package filer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Place は、カーソルの経路の外で開いたフォルダも記録し、RestorePlace がそれを開く (カーソルの祖先は RestorePlace が
// 開くので、祖先だけを見るテストでは Opened の欠落が見えない)。
func TestPlaceRoundTripsFoldersOffCursorPath(t *testing.T) {
	m := newTest(t)
	a := m.loadPath(filepath.Join(m.root.abs, "a"))
	b := m.loadPath(filepath.Join(m.root.abs, "b"))
	if a == nil || b == nil || !a.dir || !b.dir {
		t.Fatal("fixture の a / b が無い")
	}
	a.expanded = true
	m.setCur(b) // a の外 (b は開かない)
	p := m.Place()
	if p.Start != m.startDir || p.Cursor != b.path() || !slices.Contains(p.Opened, a.path()) {
		t.Fatalf("Place = %+v (a = %s を開き、カーソルは b のはず)", p, a.path())
	}

	m2, err := New(m.root.abs, Options{Now: m.now})
	if err != nil {
		t.Fatal(err)
	}
	m2.RestorePlace(p)
	if n := m2.loadPath(a.path()); n == nil || !n.expanded {
		t.Error("カーソルの経路の外で開いていた a が開かない")
	}
	if m2.cur.path() != b.path() {
		t.Errorf("カーソル = %s, want %s", m2.cur.path(), b.path())
	}
}

// Place は開いているタイル (奥から手前・本体か diff か・スクロール) を記録し、閉じかけのタイルは含めない。
func TestPlaceRecordsOpenTiles(t *testing.T) {
	fakeDiff(t, false, "diff --git a/c.txt b/c.txt\n-old\n+new\n")
	m := newTest(t)
	markModified(t, m, "c.txt")
	writeRel(t, m, "long.txt", strings.Repeat("line\n", 300))
	long := openTileAt(t, m, "long.txt")
	long.scroll = 120
	c, msg := m.newTile(m.nodeFor(filepath.Join(m.root.abs, "c.txt")), m.cursorRect())
	if msg != "" {
		t.Fatal(msg)
	}
	m.tiles = append(m.tiles, c)
	m.HandleKey("d")
	waitDiff(t, m)
	want := []TilePlace{{Path: long.n.path(), Scroll: 120}, {Path: c.n.path(), Diff: true}}
	if got := m.Place().Tiles; !slices.Equal(got, want) {
		t.Fatalf("Tiles = %+v, want %+v", got, want)
	}
	m.HandleKey("q") // 手前のタイルを閉じ始める
	if got := m.Place().Tiles; !slices.Equal(got, want[:1]) {
		t.Fatalf("閉じかけのタイルを記録した: %+v", got)
	}
}

// RestorePlace はタイルを開き切った状態で積み直す。画面の大きさが決まる前に戻したスクロールを、決まったときの Resize が保つ
// (glogx は起動時の復元の後で描くときに Resize する)。diff は git の状態を取る前でも diff で開く。開けないファイルは知らせずに飛ばす。
func TestRestorePlaceReopensTiles(t *testing.T) {
	fakeDiff(t, false, "diff --git a/c.txt b/c.txt\n-old\n+new\n")
	dir := fixture(t)
	// 1 行を長くして、復元のときに読む分 (chunkBytes 単位) が 120 行 + 画面 1 枚に届かないようにする (届くと Resize の読み足しが見えない)
	mustWrite(t, filepath.Join(dir, "long.txt"), strings.Repeat(strings.Repeat("x", 1000)+"\n", 400))
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil { // diff は repo の中でだけ取る (fetchDiff)
		t.Fatal(err)
	}
	m := newAt(t, dir)
	m.set.Wrap = false // 折り返すと長い行が表示の行を増やし、読み足さなくても上限に届く
	m.TakeNotices()
	m.RestorePlace(Place{Start: dir, Tiles: []TilePlace{
		{Path: filepath.Join(dir, "long.txt"), Scroll: 120},
		{Path: filepath.Join(dir, "gone.txt")},
		{Path: filepath.Join(dir, "c.txt"), Diff: true},
	}})
	if ns := m.TakeNotices(); len(ns) != 0 {
		t.Errorf("復元で知らせを出した: %+v", ns)
	}
	if len(m.tiles) != 2 {
		t.Fatalf("タイル %d 枚, want 2 (消えたファイルは飛ばす)", len(m.tiles))
	}
	long, c := m.tiles[0], m.tiles[1]
	m.Resize(120, 40)
	if long.scroll != 120 || long.mode != modeFile || long.slot != 0 {
		t.Errorf("long.txt: scroll=%d mode=%v slot=%d, want 120 file 0", long.scroll, long.mode, long.slot)
	}
	if long.open.v != 1 || long.sy.v != 120 {
		t.Errorf("開くアニメ・スクロールの動きが残る: open=%v sy=%v", long.open.v, long.sy.v)
	}
	if c.mode != modeDiff || c.slot != 1 {
		t.Fatalf("c.txt: mode=%v slot=%d, want diff 1", c.mode, c.slot)
	}
	waitDiff(t, m)
	if vl := m.view(c); len(vl) != 3 || vl[2].plain != "+new" {
		t.Errorf("diff の表示: %+v", vl)
	}
}
