package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/internal/app"
	"treefiler/filer"
)

// 記憶の置き場を隔離し、時計を固定する。
func withFilerState(t *testing.T) time.Time {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	return now
}

// 期限切れ / 未来の時刻 / 別の dir / 壊れた JSON / 時刻なしは「復元しない」に倒す。
func TestFilerScreenRejectsBadState(t *testing.T) {
	now := withFilerState(t)
	p := filer.Place{Start: "/a", Cursor: "/a/x"}
	if _, ok := loadFilerScreen(now, "/a"); ok {
		t.Fatal("記憶が無いのに復元しようとした")
	}
	for _, c := range []struct {
		name string
		s    filerScreen
		dir  string
	}{
		{"期限切れ", filerScreen{Place: p, SavedAt: now.Add(-app.ScreenMemoryTTL)}, "/a"},
		{"未来の時刻", filerScreen{Place: p, SavedAt: now.Add(time.Minute)}, "/a"},
		{"別の dir", filerScreen{Place: p, SavedAt: now}, "/a/sub"},
		{"時刻なし", filerScreen{Place: p}, "/a"},
		{"起動 dir なし", filerScreen{Place: filer.Place{Cursor: "/a/x"}, SavedAt: now}, ""},
	} {
		if err := saveFilerScreen(c.s); err != nil {
			t.Fatal(err)
		}
		if _, ok := loadFilerScreen(now, c.dir); ok {
			t.Errorf("%s: 復元しようとした", c.name)
		}
	}
	path, _ := filerMemory.Path()
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadFilerScreen(now, "/a"); ok {
		t.Error("壊れた JSON から復元しようとした")
	}
}

// F を出したまま終える経路 (C-g と F の q) で覚え、F を閉じてから終えたら消す。
func TestRememberFilerScreenOnQuit(t *testing.T) {
	for _, key := range []string{"ctrl+g", "q"} {
		t.Run(key, func(t *testing.T) {
			now := withFilerState(t)
			m := newFilerBrowse(t)
			m.handleKey("F")
			m.handleKey("j") // カーソルを動かしてから閉じる (場所を覚えたことを見るため)
			want := m.filerV.f.Place()
			if _, cmd := m.handleKey(key); !isQuitCmd(cmd) {
				t.Fatalf("%s で終わらない", key)
			}
			if key == "q" && m.filerV.visible() {
				t.Error("q で F を閉じていない (hide = 見張りの停止と Remember place の保存が走らない)")
			}
			got, ok := loadFilerScreen(now, app.CurrentDir())
			if !ok || got.Cursor != want.Cursor {
				t.Fatalf("F を出したまま終えたのに覚えていない: %+v ok=%v (want cursor %s)", got, ok, want.Cursor)
			}

			m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
			m2.quit()
			if _, ok := loadFilerScreen(now, app.CurrentDir()); ok {
				t.Error("F を閉じて終えたのに記憶が残った (次の起動で蘇る)")
			}
		})
	}
}

// 起動時: 同じ dir の記憶があれば、F を開いた状態で、開いていたフォルダとカーソルが戻る。
func TestInitRestoresFilerScreen(t *testing.T) {
	now := withFilerState(t)
	m := newFilerBrowse(t)
	dir := app.CurrentDir()
	sub := filepath.Join(dir, "sub")
	if err := saveFilerScreen(filerScreen{Place: filer.Place{Start: dir, Opened: []string{sub}, Cursor: filepath.Join(sub, "c.txt")}, SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.activeFullScreen() != app.FullScreenFiler {
		t.Fatalf("記憶があるのに F が開いていない: active=%v", m.activeFullScreen())
	}
	if got := m.filerV.f.Place().Cursor; got != filepath.Join(sub, "c.txt") {
		t.Errorf("カーソルが戻らない: %s", got)
	}
	if view := strings.Join(m.filerV.lines(100, 29), "\n"); !strings.Contains(view, "c.txt") {
		t.Errorf("開いていたフォルダの中身が描かれていない:\n%s", view)
	}
}

// 他の全画面の記憶が当たったら F は開かない (全画面は同時に 1 枚。F の優先は最下位)。記憶が無ければ一覧から始まる。
func TestInitFilerRestoreYieldsToOtherScreens(t *testing.T) {
	now := withFilerState(t)
	m := newFilerBrowse(t)
	if err := saveFilerScreen(filerScreen{Place: filer.Place{Start: app.CurrentDir()}, SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.filerV.visible() {
		t.Error("ratelimit の記憶があるのに F も開いた")
	}

	removeFilerScreen()
	removeRatelimitScreen()
	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	_ = m2.Init()
	if m2.activeFullScreen() != app.FullScreenNone {
		t.Errorf("記憶が無いのに全画面が開いた: %v", m2.activeFullScreen())
	}
}
