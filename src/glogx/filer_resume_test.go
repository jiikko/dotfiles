package main

import (
	"path/filepath"
	"strings"
	"testing"

	"glogx/internal/app"
	"glogx/internal/filerview"
	"glogx/internal/gitdata"
	"glogx/internal/ratelimitview"
	"glogx/internal/testenv"
	"treefiler/filer"
)

// F を出したまま終える経路 (C-g と F の q) で覚え、F を閉じてから終えたら消す。
func TestRememberFilerScreenOnQuit(t *testing.T) {
	for _, key := range []string{"ctrl+g", "q"} {
		t.Run(key, func(t *testing.T) {
			now := testenv.IsolateScreenMemory(t)
			m := newFilerBrowse(t)
			m.handleKey("F")
			m.handleKey("j") // カーソルを動かしてから閉じる (場所を覚えたことを見るため)
			want := m.filerV.Fixture().Model().Place()
			if _, cmd := m.handleKey(key); !isQuitCmd(cmd) {
				t.Fatalf("%s で終わらない", key)
			}
			if key == "q" && m.filerV.Visible() {
				t.Error("q で F を閉じていない (hide = 見張りの停止と Remember place の保存が走らない)")
			}
			got, ok := filerview.LoadFilerScreen(now, app.CurrentDir())
			if !ok || got.Cursor != want.Cursor {
				t.Fatalf("F を出したまま終えたのに覚えていない: %+v ok=%v (want cursor %s)", got, ok, want.Cursor)
			}

			m2 := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
			m2.quit()
			if _, ok := filerview.LoadFilerScreen(now, app.CurrentDir()); ok {
				t.Error("F を閉じて終えたのに記憶が残った (次の起動で蘇る)")
			}
		})
	}
}

// 起動時: 同じ dir の記憶があれば、F を開いた状態で、開いていたフォルダとカーソルが戻る。
func TestInitRestoresFilerScreen(t *testing.T) {
	now := testenv.IsolateScreenMemory(t)
	m := newFilerBrowse(t)
	dir := app.CurrentDir()
	sub := filepath.Join(dir, "sub")
	if err := filerview.FixtureSaveScreen(filer.Place{Start: dir, Opened: []string{sub}, Cursor: filepath.Join(sub, "c.txt")}, now); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.activeFullScreen() != FullScreenFiler {
		t.Fatalf("記憶があるのに F が開いていない: active=%v", m.activeFullScreen())
	}
	if got := m.filerV.Fixture().Model().Place().Cursor; got != filepath.Join(sub, "c.txt") {
		t.Errorf("カーソルが戻らない: %s", got)
	}
	if view := strings.Join(m.filerV.Lines(100, 29), "\n"); !strings.Contains(view, "c.txt") {
		t.Errorf("開いていたフォルダの中身が描かれていない:\n%s", view)
	}
}

// 他の全画面の記憶が当たったら F は開かない (全画面は同時に 1 枚。F の優先は最下位)。記憶が無ければ一覧から始まる。
func TestInitFilerRestoreYieldsToOtherScreens(t *testing.T) {
	now := testenv.IsolateScreenMemory(t)
	m := newFilerBrowse(t)
	if err := filerview.FixtureSaveScreen(filer.Place{Start: app.CurrentDir()}, now); err != nil {
		t.Fatal(err)
	}
	if err := ratelimitview.FixtureSaveScreen(now); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.filerV.Visible() {
		t.Error("ratelimit の記憶があるのに F も開いた")
	}

	filerview.FixtureRemoveScreen()
	ratelimitview.FixtureRemoveScreen()
	m2 := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	_ = m2.Init()
	if m2.activeFullScreen() != FullScreenNone {
		t.Errorf("記憶が無いのに全画面が開いた: %v", m2.activeFullScreen())
	}
}
