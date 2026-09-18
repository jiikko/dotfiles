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

// 対照: **本当に古い** graveyard の記録は従来どおり消えること
// (打ち直しが「常に残す」へ倒れていないこと。倒れると graveyard が無限に育つ)。
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
