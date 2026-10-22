package filerview

import (
	"glogx/internal/app"
	"glogx/internal/testenv"
	"os"
	"slices"
	"testing"
	"time"
	"treefiler/filer"
)

// 期限切れ / 未来の時刻 / 別の dir / 壊れた JSON / 時刻なしは「復元しない」に倒す。
func TestFilerScreenRejectsBadState(t *testing.T) {
	now := testenv.IsolateScreenMemory(t)
	p := filer.Place{Start: "/a", Cursor: "/a/x"}
	if _, ok := LoadFilerScreen(now, "/a"); ok {
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
		if _, ok := LoadFilerScreen(now, c.dir); ok {
			t.Errorf("%s: 復元しようとした", c.name)
		}
	}
	path, _ := filerMemory.Path()
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadFilerScreen(now, "/a"); ok {
		t.Error("壊れた JSON から復元しようとした")
	}
}

// タイルの本体か diff か・スクロールは記憶の JSON を通って残る (glogx の起動をまたぐ経路はこのファイル 1 本)。
func TestFilerScreenKeepsTiles(t *testing.T) {
	now := testenv.IsolateScreenMemory(t)
	p := filer.Place{Start: "/a", Cursor: "/a/x", Tiles: []filer.TilePlace{{Path: "/a/x", Scroll: 42}, {Path: "/a/y", Diff: true}}}
	if err := saveFilerScreen(filerScreen{Place: p, SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadFilerScreen(now, "/a")
	if !ok || !slices.Equal(got.Tiles, p.Tiles) {
		t.Fatalf("Tiles = %+v ok=%v, want %+v", got.Tiles, ok, p.Tiles)
	}
}
