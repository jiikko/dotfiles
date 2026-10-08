package filer

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

var fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// fixture は決め打ちの木を一時ディレクトリに作る。mtime は fixedNow からの経過で固定する。
func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir()) // 設定はテストごとに別 (前のテストが . で書いた設定を読まない)
	root := t.TempDir()
	files := map[string]string{
		"a/one.txt":       "1\n2\n3\n",
		"a/two.go":        "package two\n",
		"a/three.md":      "see a/one.txt and b/deep/x.txt:12.\nmissing/nope.txt\n",
		"b/deep/x.txt":    "x\n",
		"c.txt":           "c\n",
		".hidden":         "h\n",
		"song.mp3":        "not really audio\n",
		"pic.png":         "not really png\n",
		"bin.dat":         "ab\x00cd",
		"file10.txt":      "",
		"file2.txt":       "",
		"name\x1b[31m.md": "ansi in name\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newTest(t *testing.T) *Model {
	t.Helper()
	m, err := New(fixture(t), Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	m.Resize(120, 40)
	m.Advance(fixedNow)
	m.snapAll() // 起動時のフェードインを終点まで進める
	return m
}

func names(ns []*node) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = n.raw
	}
	return strings.Join(s, ",")
}

func TestKidsAreNaturalSortedAndDotfilesHidden(t *testing.T) {
	m := newTest(t)
	got := names(m.kids(m.root))
	want := "a,b,bin.dat,c.txt,file2.txt,file10.txt,name\x1b[31m.md,pic.png,song.mp3"
	if got != want {
		t.Fatalf("kids = %q\nwant %q", got, want)
	}
	m.HandleKey(".")
	if !strings.Contains(names(m.kids(m.root)), ".hidden") {
		t.Fatal(". で dotfile が見えない")
	}
}

func TestNameIsSanitized(t *testing.T) {
	m := newTest(t)
	for _, k := range m.root.kids {
		if strings.ContainsRune(k.name, 0x1b) {
			t.Fatalf("表示名に ESC が残っている: %q", k.name)
		}
	}
}

// j を押しっぱなしにしても、開いたフォルダへ潜らず兄弟の中だけを動く (spec §0.1)。矢印と emacs は別名。
func TestSiblingMovesAndAliases(t *testing.T) {
	m := newTest(t)
	if m.cur.raw != "a" {
		t.Fatalf("起動時のカーソル = %q", m.cur.raw)
	}
	m.HandleKey("l") // a へ潜る → one.txt? 名前順で three.md が先
	if m.cur.parent.raw != "a" {
		t.Fatalf("l で a に潜らない: %q", m.cur.raw)
	}
	m.HandleKey("h")
	if m.cur.raw != "a" || !m.cur.expanded {
		t.Fatalf("h で親へ戻らない / 開いたままでない: %q", m.cur.raw)
	}
	for range 20 {
		m.HandleKey("j")
	}
	if m.cur.raw != "song.mp3" {
		t.Fatalf("j の押しっぱなしで兄弟の末尾に止まらない (開いた a へ潜った?): %q", m.cur.raw)
	}
	for _, k := range []string{"up", "ctrl+p", "k"} {
		before := m.cur
		m.HandleKey(k)
		if m.cur == before {
			t.Fatalf("%s が上へ動かない", k)
		}
	}
	m.HandleKey("g")
	if m.cur.raw != "a" {
		t.Fatalf("g で兄弟の先頭へ行かない: %q", m.cur.raw)
	}
	m.HandleKey("ctrl+f")
	if m.cur.parent.raw != "a" {
		t.Fatal("ctrl+f が → の別名になっていない")
	}
	m.HandleKey("ctrl+b")
	if m.cur.raw != "a" {
		t.Fatal("ctrl+b が ← の別名になっていない")
	}
	m.HandleKey("ctrl+n")
	if m.cur.raw != "b" {
		t.Fatalf("ctrl+n が ↓ の別名になっていない: %q", m.cur.raw)
	}
}

func TestSpineIsOnRowZeroAndBlocksDoNotOverlap(t *testing.T) {
	m := newTest(t)
	m.HandleKey("l")
	m.HandleKey("j") // a の 2 番目
	pos := m.layout()
	for n := range m.spine() {
		if _, ok := pos[n]; ok && pos[n].y != 0 {
			t.Fatalf("spine の %q が y=%d (選択線は y=0)", n.raw, pos[n].y)
		}
	}
	seen := map[[2]int]string{}
	for n, p := range pos {
		k := [2]int{p.x, p.y}
		if o, ok := seen[k]; ok {
			t.Fatalf("%q と %q が同じ位置 %v", o, n.raw, k)
		}
		seen[k] = n.raw
	}
}

func TestHeatStops(t *testing.T) {
	for _, s := range ember {
		if got := heat(s.age); got != s.c {
			t.Fatalf("heat(%v) = %v, want %v", s.age, got, s.c)
		}
	}
	mid := heat(1800) // 1 分と 1 時間の間
	if mid == ember[0].c || mid == ember[1].c {
		t.Fatal("区間の途中が補間されていない")
	}
}

func TestTweenEaseOutAndSnap(t *testing.T) {
	var tw tween
	tw.step(100, 0.5, 0)
	tw.step(100, 0.5, 0.065) // 全体の 13%
	if tw.v < 49 {
		t.Fatalf("初動が遅い: 13%% の時点で %.1f (ease-out quint なら約 50)", tw.v)
	}
	tw.snap()
	if tw.v != 100 {
		t.Fatalf("snap で終点へ飛ばない: %v", tw.v)
	}
}

func cdTo(t *testing.T, m *Model, rel string) {
	t.Helper()
	m.cur = m.root
	for part := range strings.SplitSeq(rel, "/") {
		m.ensureLoaded(m.cur)
		var next *node
		for _, k := range m.cur.kids {
			if k.raw == part {
				next = k
			}
		}
		if next == nil {
			t.Fatalf("%s が無い", rel)
		}
		m.cur.expanded = true
		m.setCur(next)
	}
}

func TestRefusedFilesShowNoticeNotTile(t *testing.T) {
	m := newTest(t)
	for _, f := range []string{"song.mp3", "pic.png", "bin.dat"} {
		cdTo(t, m, f)
		m.HandleKey("enter")
		if len(m.tiles) != 0 {
			t.Fatalf("%s でタイルが開いた", f)
		}
		ns := m.TakeNotices()
		if len(ns) != 1 || ns[0].OK {
			t.Fatalf("%s の知らせ = %+v (失敗の toast を 1 つ)", f, ns)
		}
	}
}

func TestTileSlotsRotateAndCloseKeys(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "c.txt")
	for i := range 6 {
		m.openTile(m.cur, rect{})
		if got := m.tiles[i].slot; got != i%4 {
			t.Fatalf("%d 枚目の置き場所 = %d, want %d", i+1, got, i%4)
		}
	}
	for _, k := range []string{"q", "enter", "esc", "h"} {
		before := len(m.tiles)
		m.HandleKey(k)
		m.HandleKey("zzz-unknown") // 次のキーで閉じかけを捨てる
		if len(m.tiles) != before-1 {
			t.Fatalf("%s でタイルが 1 枚閉じない (%d → %d)", k, before, len(m.tiles))
		}
	}
	if r := slotRect(0, 200, 55); r.x != 40 || r.y != 8 {
		t.Fatalf("1 枚目が中央でない: %+v (spec §0.2 の 200×55 で (40, 8))", r)
	}
}

func TestTileJKReplacesAndStopsAtEnd(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	m.HandleKey("enter")
	m.HandleKey("J")
	if m.frontTile().n.raw != "three.md" || m.cur.raw != "three.md" {
		t.Fatalf("J で隣のファイルへ差し替わらない / カーソルが追従しない: tile=%q cur=%q", m.frontTile().n.raw, m.cur.raw)
	}
	m.HandleKey("J")
	m.HandleKey("J")
	ns := m.TakeNotices()
	if len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "最後") {
		t.Fatalf("末尾で知らせが出ない: %+v", ns)
	}
}

func TestLinksResolveOnlyExistingFiles(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/three.md")
	m.HandleKey("enter")
	ls := m.tileLinks(m.frontTile(), 10)
	got := make([]string, 0, len(ls))
	for _, l := range ls {
		got = append(got, l.text)
	}
	if strings.Join(got, ",") != "a/one.txt,b/deep/x.txt" {
		t.Fatalf("links = %v (実在するものだけ。:12 と末尾の . は外す)", got)
	}
	m.HandleKey("tab")
	m.HandleKey("j")
	m.HandleKey("enter")
	if len(m.tiles) != 2 || m.frontTile().n.raw != "x.txt" {
		t.Fatalf("ジャンプでタイルが重ならない: %d 枚", len(m.tiles))
	}
	if m.frontTile().slot != 1 {
		t.Fatalf("2 枚目の置き場所 = %d", m.frontTile().slot)
	}
}

func TestLazyTextReadsHeadThenMore(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := range 50000 {
		b.WriteString(strings.Repeat("x", 30))
		b.WriteString("\t")
		b.WriteRune(rune('a' + i%26))
		b.WriteString("\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openText(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.eof || len(s.lines) >= 50000 {
		t.Fatalf("先頭だけでなく全部読んだ: %d 行", len(s.lines))
	}
	if strings.Contains(s.lines[0], "\t") {
		t.Fatal("タブが展開されていない")
	}
	n := len(s.lines)
	s.ensure(n + 1000)
	if len(s.lines) < n+1000 {
		t.Fatalf("続きを読まない: %d → %d", n, len(s.lines))
	}
	s.ensure(1 << 30)
	if !s.eof || len(s.lines) != 50000 {
		t.Fatalf("末尾まで読めない: eof=%v lines=%d", s.eof, len(s.lines))
	}
}

func TestViewPaintsCursorBackground(t *testing.T) {
	m := newTest(t)
	c := m.draw()
	found := false
	for _, p := range c.cells {
		if p.bg == cCursorBg && p.s == "a" {
			found = true
		}
	}
	if !found {
		t.Fatal("カーソルの名前の背景が glogx のカーソル色になっていない")
	}
	if !strings.Contains(c.plain()[m.h-1], "q quit") {
		t.Fatal("ステータスバーに抜ける手段の案内が無い")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := newTest(t)
		if m.HandleKey(k) != Quit {
			t.Fatalf("木の上の %s で Quit にならない", k)
		}
	}
}

// FIFO は開かない (os.Open が書き手を待って画面が永久に固まる。レビューで再現 2026-10-08)。
func TestFIFOIsRefusedWithoutBlocking(t *testing.T) {
	m := newTest(t)
	p := filepath.Join(m.root.path(), "pipe")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Fatal(err)
	}
	m.root.load()
	cdTo(t, m, "pipe")
	done := make(chan struct{})
	go func() {
		m.HandleKey("enter")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second): // hang guard (合否は下の知らせで見る)
		t.Fatal("FIFO を開こうとして返らない")
	}
	if len(m.tiles) != 0 || len(m.TakeNotices()) != 1 {
		t.Fatal("FIFO でタイルが開いた / 知らせが出ない")
	}
}

func TestCtrlCQuitsFromTileAndJump(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/three.md")
	m.HandleKey("enter")
	m.HandleKey("tab")
	if m.HandleKey("ctrl+c") != Quit {
		t.Fatal("タイルのジャンプモードで ctrl+c が Quit にならない")
	}
}

func TestSymlinkRootIsFollowed(t *testing.T) {
	real := fixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	m, err := New(link, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.kids(m.root)) == 0 {
		t.Fatal("symlink の root で木が空")
	}
}

func TestLongLineIsSplitAndContentSanitized(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "one-line.txt")
	body := "\x1b[31mred\x1b[0m" + strings.Repeat("あ", maxLineBytes) // 改行なし。1 文字 3 バイト
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openText(p)
	if err != nil {
		t.Fatal(err)
	}
	s.ensure(2)
	if len(s.lines) < 2 {
		t.Fatalf("1 行の上限で割られていない: %d 行", len(s.lines))
	}
	for _, l := range s.lines {
		if len(l) > maxLineBytes || !utf8.ValidString(l) {
			t.Fatalf("行が上限を超える / 文字の途中で割れた: len=%d", len(l))
		}
	}
	if strings.ContainsRune(s.lines[0], 0x1b) {
		t.Fatal("本文の ESC が残っている")
	}
}

func TestFourthSlotPosition(t *testing.T) {
	if r := slotRect(3, 200, 55); r.x != 76 || r.y != 14 {
		t.Fatalf("4 枚目の置き場所 = %+v (spec §0.2 の 200×55 で (76, 14))", r)
	}
}

func TestFoldKeysAndReroot(t *testing.T) {
	m := newTest(t)
	m.HandleKey("l") // a を開いて中へ
	m.HandleKey("h")
	m.HandleKey("space")
	if m.cur.expanded {
		t.Fatal("space で畳まれない")
	}
	m.HandleKey("tab")
	if !m.cur.expanded {
		t.Fatal("tab で開かない")
	}
	m.HandleKey("c")
	if m.cur.expanded {
		t.Fatal("c で畳まれない")
	}
	m.HandleKey("l")
	m.HandleKey("C") // カーソル (a の中のファイル) の祖先は開いたまま
	if !m.cur.parent.expanded {
		t.Fatal("C で経路の祖先まで畳まれた")
	}
	oldRoot := m.root
	m.HandleKey("-")
	if m.root == oldRoot || m.root.last != oldRoot || oldRoot.parent != m.root {
		t.Fatal("- で 1 段上へ付け替わらない")
	}
	if !m.onPath(m.cur) || m.cur.parent == nil {
		t.Fatal("付け替えでカーソルが木の外へ出た")
	}
}

func TestJKOnTileOutsideTreeSaysSo(t *testing.T) {
	m := newTest(t)
	outside := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(outside, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openTile(m.nodeFor(outside), rect{})
	m.HandleKey("J")
	ns := m.TakeNotices()
	if len(ns) != 1 || !strings.Contains(ns[0].Text, "木の外") {
		t.Fatalf("木の外のファイルの J の知らせ = %+v", ns)
	}
}

// Refresh は開いていたフォルダを読み直す。開閉は保ち、消えた項目にいたカーソルは祖先へ移る。
func TestRefreshPicksUpChangesAndKeepsState(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	if err := os.WriteFile(filepath.Join(m.root.path(), "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(m.root.path(), "b", "deep")); err != nil {
		t.Fatal(err)
	}
	b := m.cur.parent.parent
	m.Refresh()
	if !strings.Contains(names(m.kids(m.root)), "new.txt") {
		t.Fatal("新しいファイルが出ない")
	}
	if m.cur != b {
		t.Fatalf("消えた項目にいたカーソルが祖先へ移らない: %q", m.cur.raw)
	}
	if !b.expanded {
		t.Fatal("読み直しで開閉の状態が失われた")
	}
}

func writeFile(p string) error  { return os.WriteFile(p, []byte("x\n"), 0o644) }
func removeFile(p string) error { return os.Remove(p) }
