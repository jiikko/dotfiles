package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// graveyard の retention は **退避した時刻**から測ること (issue 364 の 2)。
//
// 🚨 `os.Rename` は mtime を保つので、打ち直さないと「死んだマシンが何日も放置した lock」=
// **最も記録する価値のあるもの**だけが、退避した直後の掃除で即座に消える。
// 人が `break` を打つのは「誰が握っていたのか」を知りたい場面なので、
// `Break` の doc が言う「誰が握っていたかの記録も残す」がそこでだけ成立しない。
func TestGraveyardRetentionIsMeasuredFromEviction(t *testing.T) {
	// 🚨 fixture の古さは retention (7d) を**確実に超える**ところへ置く。
	// 足りないと「打ち直さなくても残る」ので、変異を当てても緑で通る。
	aged := graveyardRetention + 24*time.Hour

	for _, tc := range []struct {
		name  string
		evict func(t *testing.T, l *Locker)
	}{
		{"break", func(t *testing.T, l *Locker) {
			if err := l.Break(); err != nil {
				t.Fatalf("Break: %v", err)
			}
		}},
		{"takeover", func(t *testing.T, l *Locker) {
			// 期限切れなので引き継げる = lock は graveyard へ退く
			if _, err := l.Acquire(time.Hour, "thief"); err != nil {
				t.Fatalf("Acquire (引き継ぎ): %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTestLocker(t)
			if _, err := l.Acquire(time.Minute, "holder"); err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			old := time.Now().Add(-aged)
			if err := os.Chtimes(l.lockPath(), old, old); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}
			// 前提が成立していることを固定する (退避する前に消えていないこと)
			if _, err := os.Stat(l.lockPath()); err != nil {
				t.Fatalf("前提が崩れている (lock が無い): %v", err)
			}

			tc.evict(t, l)

			if n := countGraveyard(t, l); n != 1 {
				t.Fatalf("退避した直後の graveyard が %d 件 (1 件を期待)", n)
			}
			if res := l.Cleanup(true); len(res.Errors) != 0 {
				t.Fatalf("Cleanup: %v", res.Errors)
			}
			if n := countGraveyard(t, l); n != 1 {
				t.Fatalf("退避した直後の掃除で記録が消えた (%d 件)。"+
					"retention が「lock が最後に打刻された時刻」から測られている", n)
			}
		})
	}
}

// 対照: 打刻した値そのものが「退避した時刻」であること (両方向)。
//
// 🚨 これが無いと **「打刻しすぎ」を 1 本も検出できない**。上のテストと
// TestGraveyardStillExpiresAfterRetention は、どちらも打刻後に自分で mtime を
// 上書きするか「消えないこと」しか見ないので、`now.Add(100*24*time.Hour)` へ
// 打つ変異が **full suite 全緑**で通っていた (実測 2026-09-19 の反証レビュー P2。
// issue 364 の本文が「対照を置いた」と書いていたのは誤りだった)。
// 打刻しすぎは graveyard が retention を無視して無限に育つ形なので、上限も見る。
func TestGraveyardStampIsTheEvictionTime(t *testing.T) {
	for _, tc := range []struct {
		name  string
		evict func(t *testing.T, l *Locker)
	}{
		{"break", func(t *testing.T, l *Locker) {
			if err := l.Break(); err != nil {
				t.Fatalf("Break: %v", err)
			}
		}},
		{"takeover", func(t *testing.T, l *Locker) {
			if _, err := l.Acquire(time.Hour, "thief"); err != nil {
				t.Fatalf("Acquire (引き継ぎ): %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTestLocker(t)
			if _, err := l.Acquire(time.Minute, "holder"); err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			// 退避対象を古くしておく (打刻が効かなければ古いままになる = 下限側の検出)
			old := time.Now().Add(-(graveyardRetention + 24*time.Hour))
			if err := os.Chtimes(l.lockPath(), old, old); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}

			before := time.Now()
			tc.evict(t, l)
			after := time.Now()

			var stamped time.Time
			forEachGraveyard(t, l, func(p string) {
				fi, err := os.Stat(p)
				if err != nil {
					t.Fatalf("Stat: %v", err)
				}
				stamped = fi.ModTime()
			})
			if stamped.IsZero() {
				t.Fatal("graveyard の記録が無い")
			}
			// 🚨 上下の**両方**を見る。下限だけだと「未来へ打つ」変異が、
			// 上限だけだと「打たない」変異が、それぞれ緑で通る。
			// 窓は退避の前後 ± 1 分 (serverNow は probe の実 I/O なので多少ずれる)。
			if stamped.Before(before.Add(-time.Minute)) {
				t.Fatalf("打刻が古すぎる: %v (退避は %v 以降)。退避時刻で打ち直していない", stamped, before)
			}
			if stamped.After(after.Add(time.Minute)) {
				t.Fatalf("打刻が新しすぎる: %v (退避は %v 以前)。未来へ打つと graveyard が無限に育つ", stamped, after)
			}
		})
	}
}

// 退避の打刻に使う時刻は、**不可逆な rename より前**に取得すること (反証レビュー P1)。
//
// 🚨 `serverNow` は probe の実 I/O (作成 + stat + 削除)。これが rename の**後ろ**にあると、
// 応答の鈍いマウントでその I/O が `--io-timeout` を食い切り、打刻が着地しないまま
// `BreakTimed` が「判定不能」で返る。`dispatch` は break のあとに必ず `Cleanup` を defer で
// 走らせるので、**旧 mtime (8 日前) のまま retention 超過と判定されて記録がその場で消える**。
// しかも人には「判定不能」という別の話が出るので、記録を失ったことが見えない。
// 実測 (A-B、8 日前の lock を break): 速いマウント → graveyard 1 件 / 鈍いマウント → 0 件。
//
// 🚨 これを壁時計で再現するテストは書かない (`avoid-wall-clock-assertions.md`)。
// 順序そのものを seam で観測する。窓は縮むだけで 0 にはならない (rename 後に残る
// `os.Chtimes` 1 本ぶんは残る) ので、ここで守るのは「時刻の取得が前に在ること」。
func TestBreakTakesTheStampTimeBeforeRenaming(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	called := false
	ready := false
	orig := breakAfterRenameHook
	breakAfterRenameHook = func(stampReady bool) { called, ready = true, stampReady }
	t.Cleanup(func() { breakAfterRenameHook = orig })

	if err := l.Break(); err != nil {
		t.Fatalf("Break: %v", err)
	}
	if !called {
		t.Fatal("前提崩れ: rename 後の seam を通っていない (この検査は何も守っていない)")
	}
	if !ready {
		t.Fatal("rename の時点で打刻用の時刻を持っていない。" +
			"serverNow の I/O が不可逆操作の後ろにあると、鈍いマウントで退避の記録が消える")
	}
}

// 対照: **本当に古い** graveyard の記録は従来どおり消えること
// (打ち直しが「常に残す」へ倒れていないこと。倒れると graveyard が無限に育つ)。
//
// 🚨 このテストは打刻後に自分で mtime を上書きするので、**打刻した値そのものは観測しない**。
// 値の正しさは TestGraveyardStampIsTheEvictionTime が持つ (ここは sweepDir の retention 判定の検査)。
func TestGraveyardStillExpiresAfterRetention(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Break(); err != nil {
		t.Fatalf("Break: %v", err)
	}
	if n := countGraveyard(t, l); n != 1 {
		t.Fatalf("graveyard が %d 件 (1 件を期待)", n)
	}
	// 退避「後」の記録そのものを古くする (= retention を過ぎた状態)
	old := time.Now().Add(-(graveyardRetention + time.Hour))
	forEachGraveyard(t, l, func(p string) {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	})
	if res := l.Cleanup(true); len(res.Errors) != 0 {
		t.Fatalf("Cleanup: %v", res.Errors)
	}
	if n := countGraveyard(t, l); n != 0 {
		t.Fatalf("retention を過ぎた記録が消えていない (%d 件)", n)
	}
}

func graveyardDir(l *Locker) string { return filepath.Join(l.metaDir, graveyardDirName) }

func countGraveyard(t *testing.T, l *Locker) int {
	t.Helper()
	entries, err := os.ReadDir(graveyardDir(l))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		t.Fatalf("ReadDir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func forEachGraveyard(t *testing.T, l *Locker, fn func(string)) {
	t.Helper()
	entries, err := os.ReadDir(graveyardDir(l))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			fn(filepath.Join(graveyardDir(l), e.Name()))
		}
	}
}

// 🚨 **鈍いマウントでも、退避の記録が消えないこと** (issue 400)。
//
// 364 の P1 は「**不可逆な操作 (`os.Rename`) のあとに残した I/O が `--io-timeout` を食い切ると、
// その I/O が前提の不変条件が黙って壊れる**」形だった (実測 A-B: 速いマウントでは graveyard に
// 1 件残り、鈍いマウント (30ms) では 0 件 = 退避の記録がその場で消える)。364 は打刻に使う時刻を
// rename の**前**で取ることで窓を縮めたが、**「鈍いマウントでも記録が残る」という振る舞い自体は
// CI で検査されていなかった** — `newTestLocker` が速いローカル APFS + 余裕のある timeout しか
// 作らないので、この退行は既存テストでは**構造的に観測できない**。
//
// 🚨 **このテストが守るのは「振る舞い」**。順序 (時刻を rename より前に取ること) は
// `TestBreakTakesTheStampTimeBeforeRenaming` が pin しているので、ここでは assert しない
// (同じ事実を 2 箇所で見ると、変異がどちらに当たったのか読めなくなる)。
//
// 決定論の作り方 (`avoid-wall-clock-assertions.md`。`sleep` で「たぶん超える」を作らない):
//
//	① rename の直後の seam で**テストが解放するまでブロック**する → `--io-timeout` を確実に食い切る
//	② その seam の中で **probe dir を壊す** → 退避の**後**に `serverNow` を呼ぶ実装なら打刻できない。
//	   rename の前に取った時刻を使う実装なら、I/O 無しで打刻が着地する
//	③ 判定は時間ではなく「**記録が残っているか**」
//
// このケースが red になる退行 (どちらも「時刻をいつ取るか」の別々の壊し方):
//   - 打刻の時刻を rename の**後ろ**で取り直す (364 以前の `Break`)
//   - `stampGraveyard` が渡された時刻を無視して `serverNow` を呼ぶ (364 以前の `stampGraveyard`)
func TestGraveyardRecordSurvivesSlowMount(t *testing.T) {
	// 🚨 timeout は**テスト側で十分小さく固定**する (マシンの速さに依存させない)
	l, err := NewLocker(t.TempDir(), 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	if _, err := l.Acquire(time.Minute, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// retention を確実に超えた古さ (この打刻が更新されないと、次の掃除で消える)
	old := time.Now().Add(-(graveyardRetention + 24*time.Hour))
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	probeDir := filepath.Join(l.metaDir, probeDirName)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	var releaseOnce sync.Once
	releaseSeam := func() { releaseOnce.Do(func() { close(release) }) }
	orig := breakAfterRenameHook
	breakAfterRenameHook = func(bool) {
		// ② 退避の**後**に I/O を必要とする実装を落とすため、ここで probe を壊す
		_ = os.Chmod(probeDir, 0o555)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // ① 解放するまで返らない = --io-timeout を必ず食い切る
	}
	t.Cleanup(func() {
		breakAfterRenameHook = orig
		releaseSeam()
		_ = os.Chmod(probeDir, metaDirMode)
	})

	done := make(chan error, 1)
	go func() { done <- l.BreakTimed() }()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("前提が作れていない: rename 後の seam に入らない")
	}
	select {
	case err := <-done:
		if !errors.Is(err, errIOTimeout) {
			t.Fatalf("鈍いマウントなのに期限切れになっていない: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("BreakTimed が 10 秒以内に戻らない (包みが無い)")
	}

	// 見捨てられた goroutine を進ませ、打刻が着地するのを**成立条件**で待つ
	releaseSeam()
	stamped := waitForCondition(t, 5*time.Second, func() bool {
		ok := false
		forEachGraveyard(t, l, func(p string) {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(old.Add(time.Hour)) {
				ok = true
			}
		})
		return ok
	})
	if !stamped {
		t.Fatalf("鈍いマウントで打刻が着地しない (退避の後に I/O が要る実装になっている)。" +
			"次の掃除で退避の記録が消える")
	}

	// 期限切れで帰った後に掃除が走る経路 (`dispatch` の defer) を再現する
	_ = os.Chmod(probeDir, metaDirMode) // 掃除は serverNow を使うので戻す
	if res := l.Cleanup(true); len(res.Errors) != 0 {
		t.Fatalf("Cleanup: %v", res.Errors)
	}
	if n := countGraveyard(t, l); n != 1 {
		t.Fatalf("鈍いマウントで退避の記録が消えた (%d 件)", n)
	}
}

// waitForCondition は成立条件を上限つきでポーリングし、成立したかを返す (壁時計で待たない)。
// 🚨 **成立しなかったことを呼び出し側が判定に使う**ので、ここでは Fatal にしない
// (「待っても成立しない」がこのテストの red そのもの)。
func waitForCondition(t *testing.T, limit time.Duration, ok func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
