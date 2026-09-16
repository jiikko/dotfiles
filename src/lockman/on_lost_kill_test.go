package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// `--on-lost kill` (既定) の経路。着手時点でテストは **1 本も無かった** (issue 356)。
// 091:282 は「子プロセスを**止められる**ようにする」と書いているが、実装は SIGTERM を
// 1 回撃つだけで、trap / 無視する子には効かなかった。

// 🚨 子が TERM を無視しても止まること。ここが経路 2 の本体。
// 昇格が無いと runWith は子が自然に終わるまで返らず、**他者が既に引き継いでいる状態が
// 無期限に続く**ので、安全網の上限で赤にする。
func TestOnLostKillEscalatesToSigkill(t *testing.T) {
	restore := shortenOnLostGrace(t, 300*time.Millisecond)
	defer restore()

	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond // tick = 200ms
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				_ = l.Break() // 他者が引き継いだ = lease 喪失
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// TERM を無視する子。昇格が無ければ 60 秒生き続ける
	got := boundedInt(t, "runWith (TERM を無視する子)", func() int {
		return runWith(l, ttl, "", true, []string{"sh", "-c", "trap '' TERM; sleep 60"})
	})
	if got != exitWithLost {
		t.Fatalf("exit %d (期待 %d = lease 喪失)", got, exitWithLost)
	}
}

// --on-lost warn では止めない (既定を変えたい人のための逃げ道が生きていること)。
func TestOnLostWarnDoesNotKillChild(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				_ = l.Break()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	// 🚨 **終了コードでは区別できない。** outcome が renewLost なら、子が殺されていても
	// 122 が返る (childExitCode に到達しない)。敵対レビューが `if onLostKill` → `if true`
	// の変異を当てて、このテストが**緑のまま通る**ことを実測した。
	// 子が最後まで走ったことを**子自身に書かせて**観測する。
	mark := filepath.Join(t.TempDir(), "done")
	got := boundedInt(t, "runWith (--on-lost warn)", func() int {
		return runWith(l, ttl, "", false,
			[]string{"sh", "-c", "sleep 0.5; : > " + mark})
	})
	if got != exitWithLost {
		t.Fatalf("exit %d (期待 %d)", got, exitWithLost)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatalf("--on-lost warn なのに子が最後まで走っていない (印が無い): %v", err)
	}
}

// 🚨 pgid が 0 なら **呼び出し側のプロセスグループ**を撃つ。本番では呼び出し元シェル、
// テストでは go test 自身が死ぬので、撃つ前に弾けていることを固定する。
func TestKillGroupRefusesUnsafePgid(t *testing.T) {
	for _, pgid := range []int{0, 1, -1} {
		if err := killGroup(pgid, syscall.SIGTERM); err == nil {
			t.Errorf("pgid=%d を撃とうとした (弾くべき)", pgid)
		}
	}
}

// shortenOnLostGrace は猶予を縮める。仕様値なので production の既定は動かさない。
func shortenOnLostGrace(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := onLostGracePeriod
	onLostGracePeriod = d
	return func() { onLostGracePeriod = old }
}

// 🚨 更新が詰まったとき、tick ごとに goroutine を積まないこと。
//
// `with` は lockman で唯一の長寿命モードなので、包んだ `Renew` が毎 tick ブロックすると
// `withTimeout` が見捨てた goroutine が溜まり続ける (回収できない)。issue 359 の項目 4 が
// 「357 を実装したら上限を置くか決めろ」と trigger を残していた箇所。
//
// 判定は**時間ではなく本数**で行う。上限が無い版はこの条件で 10 本以上積む
// (tick 200ms x 子の寿命 3s)。上限が在れば maxInFlightRenews 本で止まる。
//
// 🚨 **ここに「lease が生きているか」の assert は置けない** (issue 381 の残タスク ① は誤り)。
// この手順は FIFO を最後まで退けないので、**どの実装でも Renew は 1 度も成功しえない** —
// lease は修正版でも壊れた版でも死ぬ。恒久ラッチの検査は
// `TestLeaseSurvivesPermanentRenewBlock` (パスだけ復旧させ、古い読み手は返らないまま残す形)
// が持つ。ここが守るのは**本数の上限**だけ。
func TestRenewDoesNotPileUpGoroutinesWhenBlocked(t *testing.T) {
	// 本番値 (8) のままだと、上限チェックを外す変異 (この条件で 7〜10 本) との差が
	// 1 本ぶんしかなく tick の揺れで緑になる。2 へ縮めて桁を付ける
	defer shortenMaxInFlightRenews(t, 2)()
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond // tick = 200ms
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				// 詰まらせる (消してから作ると errNotOwner を拾う窓ができるので rename で被せる)
				tmp := l.lockPath() + ".fifo"
				if err := syscall.Mkfifo(tmp, 0o600); err != nil {
					return
				}
				_ = os.Rename(tmp, l.lockPath())
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	before := runtime.NumGoroutine()
	// --on-lost warn (kill しない) にして、子が生きているあいだ tick を回し続けさせる
	got := boundedInt(t, "runWith (詰まった renew)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 3"})
	})
	if got != exitWithInvalid {
		t.Fatalf("exit %d (期待 %d = 判定不能)", got, exitWithInvalid)
	}
	leaked := runtime.NumGoroutine() - before
	// 上限を 2 に縮めてあるので、見捨てるのは 2 本まで。余裕を見て 4 本を閾値にする
	// (上限を外すとこの条件で 7 本以上になる)
	if leaked > 4 {
		t.Fatalf("詰まった renew が %d 本の goroutine を積んだ (期待: %d 本。上限が効いていない)",
			leaked, maxInFlightRenews)
	}
}

// shortenMaxInFlightRenews は見捨てた更新の上限を縮める。仕様値なので production の既定は動かさない。
func shortenMaxInFlightRenews(t *testing.T, n int64) func() {
	t.Helper()
	old := maxInFlightRenews
	maxInFlightRenews = n
	return func() { maxInFlightRenews = old }
}

// leaseProbe は「別マシンから見て lease がどう見えるか」を 1 度に採った観測。
//
// 🚨 **`Acquire` が errBusy を返したことだけを「lease が生きている」と読まない。**
// 中身を読めない lock も errBusy になる (`readLock` が fail-closed に倒す) ので、
// 更新が止まっていても同じ緑になりうる (issue 383)。token と期限まで見て初めて
// 「保持者の lease が生きたまま」と言える。
type leaseProbe struct {
	setupErr   error // 手順そのものの失敗 (assert 以前の話)
	acquireErr error // 別マシンの Acquire の結果
	readErr    error // 保持者の lock を読み直した結果
	ours       bool  // 読めた lock が保持者のものか
	alive      bool  // その lock が期限内か
}

// probeLease は「別マシンが奪えるか」と「保持者の lease が生きているか」を同じ瞬間に採る。
// runWith の defer が lock を消すので、**観測は runWith が返る前に済ませる**必要がある。
func probeLease(l, other *Locker, ttl time.Duration, wantToken string) leaseProbe {
	var p leaseProbe
	_, p.acquireErr = other.Acquire(ttl, "other")
	m, mtime, err := l.readLock()
	p.readErr = err
	if err != nil || m == nil {
		return p
	}
	p.ours = m.Token == wantToken
	now, nerr := l.serverNow()
	if nerr != nil {
		p.readErr = nerr
		return p
	}
	p.alive = !expired(now, mtime, holderTTL(m))
	return p
}

// recvProbe は観測が返るのを**上限つきで**待つ。
// 🚨 奪う側の `Acquire` は `--io-timeout` に包まれていない生の呼び出しなので、詰まった
// 経路に入ると永久に返らない。上限が無いとパッケージ全体が panic し、**赤ではなく
// 「どの assert が落ちたか分からない」**になる (boundedInt と同じ安全網)。
func recvProbe(t *testing.T, ch <-chan leaseProbe) leaseProbe {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(20 * time.Second):
		t.Fatalf("lease の観測が 20s 以内に戻らない (奪う側の Acquire が詰まった可能性)")
		return leaseProbe{}
	}
}

// assertLeaseHeld は「保持者の lease が生きたままだった」を検査する。
func assertLeaseHeld(t *testing.T, what string, p leaseProbe, withExit int) {
	t.Helper()
	if p.setupErr != nil {
		t.Fatalf("%s: 手順が成立しなかった: %v", what, p.setupErr)
	}
	if !errors.Is(p.acquireErr, errBusy) {
		t.Fatalf("%s: 別マシンが lease を奪えた (= 更新が止まっていた): err=%v / with の exit=%d",
			what, p.acquireErr, withExit)
	}
	if p.readErr != nil || !p.ours || !p.alive {
		t.Fatalf("%s: 奪えなかったが lease が保持者のものとして生きていない "+
			"(readErr=%v ours=%v alive=%v)。errBusy の理由が違う (issue 383)",
			what, p.readErr, p.ours, p.alive)
	}
}

// waitForLockToken は lock が置かれるのを待ち、保持者の token と生の中身を返す。
func waitForLockToken(l *Locker) (string, []byte, error) {
	for range 400 {
		if b, err := os.ReadFile(l.lockPath()); err == nil && len(b) > 0 {
			var m Meta
			if err := json.Unmarshal(b, &m); err != nil {
				return "", nil, err
			}
			return m.Token, b, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", nil, errors.New("lock が置かれなかった")
}

// 🚨 上限 (maxInFlightRenews) は**詰まりが 1 度も無くても**効く。枠を戻すのは「返った更新」
// なので、戻す側 (`timeout.go` の `defer inFlight.Add(-1)`) が壊れると上限は「累計の更新回数」
// になり、**健全なマウントで上限 tick 目に更新が永久に止まる**。既定値 (TTL 30m / tick 10m /
// 上限 8) なら約 80 分走った `with` が lease を失う — 381 のバグより悪い。
//
// 詰まりを一切作らない列で固定する。敵対レビュー 2026-09-16 (観点② false green) の指摘で、
// 「上限と**成功する**更新の相互作用」を見ているテストが 1 本も無かった。
func TestLeaseSurvivesLongHealthyRun(t *testing.T) {
	// 上限 tick 目を早く来させる。健全なら枠は毎 tick 戻るので、正しい実装は上限に触れない。
	// 🚨 **2 まで縮めない**: 枠を埋めるのは「まだ返っていない更新」なので、負荷の高い CI で
	// 200ms (io-timeout) を超える更新が同時に 2 本あるだけで上限に当たり、実装が正しくても
	// 赤くなる。3 にして当たりにくくしてある (production の 8 に依存させないために縮めてはいる)。
	defer shortenMaxInFlightRenews(t, 3)()
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	// 🚨 TTL は既存で CI を通ってきた実績のある 900ms 以上にする。600ms だと lease の寿命が
	// 更新 3 回ぶんしかなく、2 core の runner で -race を回すと取りこぼしで偽の赤が出る
	// (壁時計に依存するテストは「上限を詰める」方向へ動かさない)。
	const ttl = 900 * time.Millisecond // tick = 300ms
	other, err := NewLocker(l.dir, testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	ch := make(chan leaseProbe, 1)
	go func() {
		tok, _, err := waitForLockToken(l)
		if err != nil {
			ch <- leaseProbe{setupErr: err}
			return
		}
		// 上限 (3) の 9 倍の tick を通してから観測する。枠が戻らない実装は 3 tick 目
		// (900ms) で更新をやめるので、lease は 1800ms には死んでいる (余裕 900ms)
		time.Sleep(2700 * time.Millisecond)
		ch <- probeLease(l, other, ttl, tok)
	}()

	got := boundedInt(t, "runWith (健全なマウントでの長期走行)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 4"})
	})
	assertLeaseHeld(t, "健全なマウントで上限 tick を超えて走行", recvProbe(t, ch), got)
}

// 🚨 判定不能 (125) を報告した後に確実な喪失 (errNotOwner) を知っても、終了コードは 125 の
// まま。091:398-399 が 122 と 125 を分けているが、**判定不能だった窓があった事実は後から
// 消えない**ので上書きしない (issue 381 で意図的に据え置いた決定)。
//
// 更新を張り直すようになって、この順序 (判定不能 → 確実な喪失) は毎 tick 起こりうる常態に
// なった。決めた以上は固定する。敵対レビュー 2026-09-16 (観点② false green) の指摘で、
// 既存の TestWithSeparatesLostLeaseFromIndeterminate は 1 run に 1 種類の失敗しか起こさず、
// この順序を誰も作っていなかった。
func TestIndeterminateThenLostKeepsIndeterminateExit(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout) // io-timeout = 200ms
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 900 * time.Millisecond // tick = 300ms
	setup := make(chan error, 1)
	go func() {
		if _, _, err := waitForLockToken(l); err != nil {
			setup <- err
			return
		}
		// ① FIFO を被せて判定不能を作る (tick 300ms → 期限 500ms で 1 回目の報告)
		fifo := l.lockPath() + ".fifo"
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			setup <- err
			return
		}
		if err := os.Rename(fifo, l.lockPath()); err != nil {
			setup <- err
			return
		}
		// ② 期限切れの報告が済んだ後、lock ごと退ける。**詰まった読み手は解放しない**。
		//    以後の更新は readLock が「存在しない」を返すので errNotOwner = 確実な喪失
		time.Sleep(700 * time.Millisecond)
		setup <- os.Rename(l.lockPath(), fifo)
	}()

	got := boundedInt(t, "runWith (判定不能 → 確実な喪失)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 3"}) // --on-lost warn
	})
	if err := <-setup; err != nil {
		t.Fatalf("手順が成立しなかった: %v", err)
	}
	if got != exitWithInvalid {
		t.Fatalf("exit %d (期待 %d = 判定不能のまま)。確実な喪失で上書きしていないか", got, exitWithInvalid)
	}
}

// 🚨 更新が**一過性**に詰まっただけなら、復旧後も lease を持ち続けること。
//
// 最初の失敗で更新をやめる実装 (`ticker.Stop()`) は、これを**本物の lease 喪失**に変える:
// 更新が二度と走らないので lease は実際に期限切れになり、他マシンが正当に引き継ぐ —
// 子はまだ走っているので二重実行になる。091 が「最も現実的な事故経路」と呼ぶ形そのもの。
// 敵対レビュー 2026-09-15 が A-B で実測したのを、そのままテストに落とす。
func TestLeaseSurvivesTransientRenewBlock(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout) // io-timeout = 200ms
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 900 * time.Millisecond // tick = 300ms。更新が止まれば 900ms で lease が死ぬ
	// 別マシン役。lease が切れていれば取得できてしまう
	other, err := NewLocker(l.dir, testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	stolen := make(chan leaseProbe, 1)
	go func() {
		tok, orig, err := waitForLockToken(l)
		if err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		// ① 詰まらせる (FIFO を被せる。消してから作ると errNotOwner を拾う窓ができる)
		fifo := l.lockPath() + ".fifo"
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		if err := os.Rename(fifo, l.lockPath()); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		// ② 詰まりを **最初の tick (300ms) + 期限 (200ms) より十分長く**保つ。
		//    ここが短いと、更新が始まる前 / 期限が来る前に復旧してしまい、
		//    「判定不能を 1 回報告した後も更新を続けるか」という検査したい状態に入らない
		//    (最初に 400ms で書いて、退行を当てても緑のまま通った)
		time.Sleep(900 * time.Millisecond)
		// ③ 復旧させる。**順番が要る**: 先に FIFO を退かして本物を戻し、
		//    その後で FIFO へ書いて詰まっている読み手を解放する。逆順だと、解放された
		//    Renew が続けて打つ書き込み用 open がまた FIFO に当たって詰まる
		if err := os.Rename(l.lockPath(), fifo); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		real := l.lockPath() + ".real"
		if err := os.WriteFile(real, orig, 0o600); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		if err := os.Rename(real, l.lockPath()); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			_, _ = f.Write(orig)
			_ = f.Close()
		}
		_ = os.Remove(fifo)
		// ④ lease が切れているはずの時刻を十分に過ぎてから、別マシンが奪えるか試す
		time.Sleep(1500 * time.Millisecond)
		stolen <- probeLease(l, other, ttl, tok)
	}()

	got := boundedInt(t, "runWith (一過性の詰まり)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 3"}) // --on-lost warn
	})
	assertLeaseHeld(t, "一過性の詰まりからの復旧", recvProbe(t, stolen), got)
}

// 🚨 更新が**恒久的**に詰まった (返らない fd を掴んだ) 後も、更新を再開して lease を守ること。
//
// 上の transient 版との違いは 1 手だけ: **詰まった読み手を解放しない**。
// パスは実ファイルへ戻す (= 新しい Renew なら成功する) が、古い読み手は FIFO の inode に
// 残って永久に返らない。stale handle / 掴んだまま死んだマウントの形。
//
// 修正前はここで `renewCh` が nil に戻らず、以後の tick がすべて `continue` になって
// **更新が二度と走らない**。lease は実際に期限切れになり、他マシンが正当に引き継ぐ —
// 子はまだ走っているので二重実行 (091 が「最も現実的な事故経路」と呼ぶ形)。
//
// 時間の余裕は 2 つ独立に要る。どちらも 400ms 取ってある:
//
//	(a) 復旧より前に tick が 1 本 FIFO を掴む (掴まないと見捨て経路に入らず、何も検査しない)
//	(b) 復旧後の tick が 1 本、lease 期限より手前に来る
func TestLeaseSurvivesPermanentRenewBlock(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout) // io-timeout = 200ms
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 1200 * time.Millisecond // tick = 400ms
	other, err := NewLocker(l.dir, testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	stolen := make(chan leaseProbe, 1)
	go func() {
		tok, orig, err := waitForLockToken(l)
		if err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		// ① 詰まらせる (FIFO を被せる)
		fifo := l.lockPath() + ".fifo"
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		if err := os.Rename(fifo, l.lockPath()); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		// ② (a) の余裕: tick(400ms) + 期限(200ms) より 400ms 長く保つ
		time.Sleep(800 * time.Millisecond)
		// ③ パスだけ復旧させる。**FIFO へは書かない** = 詰まった読み手は永久に返らない
		if err := os.Rename(l.lockPath(), fifo); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		real := l.lockPath() + ".real"
		if err := os.WriteFile(real, orig, 0o600); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		if err := os.Rename(real, l.lockPath()); err != nil {
			stolen <- leaseProbe{setupErr: err}
			return
		}
		// ④ (b) の余裕: 復旧で打たれた mtime から ttl(1200ms) + 400ms 過ぎてから奪いに行く。
		//    更新が再開していなければ、この時点で lease は死んでいる
		time.Sleep(1600 * time.Millisecond)
		stolen <- probeLease(l, other, ttl, tok)
	}()

	got := boundedInt(t, "runWith (恒久的な詰まり)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 4"}) // --on-lost warn
	})
	assertLeaseHeld(t, "恒久的な詰まりからの更新再開", recvProbe(t, stolen), got)
}
