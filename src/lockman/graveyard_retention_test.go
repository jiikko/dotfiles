package main

import (
	"errors"
	"os"
	"path/filepath"
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
