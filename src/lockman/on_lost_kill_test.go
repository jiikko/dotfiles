package main

import (
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

	stolen := make(chan error, 1)
	go func() {
		var orig []byte
		for range 400 { // lock が置かれるのを待つ
			if b, err := os.ReadFile(l.lockPath()); err == nil && len(b) > 0 {
				orig = b
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if orig == nil {
			stolen <- errors.New("lock が置かれなかった")
			return
		}
		// ① 詰まらせる (FIFO を被せる。消してから作ると errNotOwner を拾う窓ができる)
		fifo := l.lockPath() + ".fifo"
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			stolen <- err
			return
		}
		if err := os.Rename(fifo, l.lockPath()); err != nil {
			stolen <- err
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
			stolen <- err
			return
		}
		real := l.lockPath() + ".real"
		if err := os.WriteFile(real, orig, 0o600); err != nil {
			stolen <- err
			return
		}
		if err := os.Rename(real, l.lockPath()); err != nil {
			stolen <- err
			return
		}
		if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			_, _ = f.Write(orig)
			_ = f.Close()
		}
		_ = os.Remove(fifo)
		// ④ lease が切れているはずの時刻を十分に過ぎてから、別マシンが奪えるか試す
		time.Sleep(1500 * time.Millisecond)
		_, aerr := other.Acquire(ttl, "other")
		stolen <- aerr
	}()

	got := boundedInt(t, "runWith (一過性の詰まり)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 3"}) // --on-lost warn
	})
	aerr := <-stolen
	if !errors.Is(aerr, errBusy) {
		t.Fatalf("別マシンが lease を奪えた (= 更新が止まっていた): err=%v / with の exit=%d", aerr, got)
	}
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

	stolen := make(chan error, 1)
	go func() {
		var orig []byte
		for range 400 {
			if b, err := os.ReadFile(l.lockPath()); err == nil && len(b) > 0 {
				orig = b
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if orig == nil {
			stolen <- errors.New("lock が置かれなかった")
			return
		}
		// ① 詰まらせる (FIFO を被せる)
		fifo := l.lockPath() + ".fifo"
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			stolen <- err
			return
		}
		if err := os.Rename(fifo, l.lockPath()); err != nil {
			stolen <- err
			return
		}
		// ② (a) の余裕: tick(400ms) + 期限(200ms) より 400ms 長く保つ
		time.Sleep(800 * time.Millisecond)
		// ③ パスだけ復旧させる。**FIFO へは書かない** = 詰まった読み手は永久に返らない
		if err := os.Rename(l.lockPath(), fifo); err != nil {
			stolen <- err
			return
		}
		real := l.lockPath() + ".real"
		if err := os.WriteFile(real, orig, 0o600); err != nil {
			stolen <- err
			return
		}
		if err := os.Rename(real, l.lockPath()); err != nil {
			stolen <- err
			return
		}
		// ④ (b) の余裕: 復旧で打たれた mtime から ttl(1200ms) + 400ms 過ぎてから奪いに行く。
		//    更新が再開していなければ、この時点で lease は死んでいる
		time.Sleep(1600 * time.Millisecond)
		_, aerr := other.Acquire(ttl, "other")
		stolen <- aerr
	}()

	got := boundedInt(t, "runWith (恒久的な詰まり)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 4"}) // --on-lost warn
	})
	aerr := <-stolen
	if !errors.Is(aerr, errBusy) {
		t.Fatalf("別マシンが lease を奪えた (= 詰まった後に更新が再開していない): err=%v / with の exit=%d", aerr, got)
	}
}
