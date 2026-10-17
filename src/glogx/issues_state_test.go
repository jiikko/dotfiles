package main

import (
	"testing"

	"glogx/internal/app"
	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
)

// 終了時に覚えるのは viewer を出していたときだけ。一覧 (git log) から終了したら記憶を消す
// (残すと「一覧を見て閉じたのに次の起動で viewer が出る」)。
func TestQuitRemembersOnlyWhenViewerVisible(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m.issuesOv = *loadedView(issuesviewtest.SampleIssues()...)
	m.issuesOv.Fixture().SetRoot("/repo")
	m.quit()
	// 読むのは保存より後の時刻で (SavedAt が読み取り時刻より未来なら時計のずれとして捨てる仕様)
	s, ok := issuesview.LoadIssuesScreen(app.TimeNow())
	if !ok {
		t.Fatal("viewer を出したまま終了したのに記憶が無い")
	}
	if s.Root != "/repo" {
		t.Fatalf("記憶の root が違う: %q", s.Root)
	}

	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m2.quit() // 一覧から終了
	if _, ok := issuesview.LoadIssuesScreen(app.TimeNow()); ok {
		t.Fatal("一覧から終了したのに記憶が残っている")
	}
}
