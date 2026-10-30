package filer

import (
	"context"
	"errors"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"testwait"
)

func TestPutSGRColorsAndWidth(t *testing.T) {
	c := newCanvas(20, 1)
	c.putSGR(0, 0, "a\x1b[1;38;5;196mb\x1b[48;5;21mc\x1b[0md\x1b[38;2;1;2;3me\x1b[39mf", 5, cText)
	want := []struct {
		s    string
		fg   rgb
		bold bool
	}{{"a", cText, false}, {"b", rgb{255, 0, 0}, true}, {"c", rgb{255, 0, 0}, true}, {"d", cText, false}, {"e", rgb{1, 2, 3}, false}}
	for i, w := range want {
		p := c.at(i, 0)
		if p.s != w.s || p.fg != w.fg || p.bold != w.bold {
			t.Fatalf("%d 桁目 = %q %v bold=%v, want %q %v %v", i, p.s, p.fg, p.bold, w.s, w.fg, w.bold)
		}
		if p.bg != cBg {
			t.Fatalf("%d 桁目の背景が変わった (SGR の背景色は捨てる)", i)
		}
	}
	if p := c.at(5, 0); p.s != " " {
		t.Fatalf("maxw を超えて書いた: %q", p.s)
	}
}

func TestPutSGRDoesNotSplitWide(t *testing.T) {
	c := newCanvas(10, 1)
	c.putSGR(0, 0, "aあい", 4, cText) // a(1) あ(2) で 3 桁、い(2) は 5 桁目にはみ出す
	if c.at(1, 0).s != "あ" || c.at(3, 0).s != " " {
		t.Fatalf("全角の扱い: %q %q", c.at(1, 0).s, c.at(3, 0).s)
	}
}

func TestWrapCells(t *testing.T) {
	got := wrapCells("abあいcd", 4)
	if strings.Join(got, "|") != "abあ|いcd" {
		t.Fatalf("wrap = %q", got)
	}
	if got := wrapCells("", 4); len(got) != 1 || got[0] != "" {
		t.Fatalf("空の行 = %q", got)
	}
}

func openTileAt(t *testing.T, m *Model, rel string) *tile {
	t.Helper()
	cdTo(t, m, rel)
	m.HandleKey("enter")
	tl := m.frontTile()
	if tl == nil {
		t.Fatalf("%s のタイルが開かない", rel)
	}
	return tl
}

func writeRel(t *testing.T, m *Model, rel, body string) {
	t.Helper()
	p := filepath.Join(m.root.abs, rel)
	mustWrite(t, p, body)
	m.root.reload()
	if n := m.findNode(filepath.Join(m.root.abs, "a")); n != nil {
		n.reload()
	}
}

func TestViewHighlightsCode(t *testing.T) {
	m := newTest(t)
	tl := openTileAt(t, m, "a/two.go")
	vl := m.view(tl)
	if len(vl) != 1 || vl[0].plain != "package two" || !strings.Contains(tl.styledLine(0), "\x1b[") {
		t.Fatalf("Go の行に色が付かない: %+v", vl)
	}
	if termwidth.StripSGR(tl.styledLine(0)) != vl[0].plain {
		t.Fatalf("色を外すと元の行に戻らない: %q", tl.styledLine(0))
	}
}

func TestViewRendersMarkdown(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/doc.md", "# 見出し\n\n- 項目\n")
	tl := openTileAt(t, m, "a/doc.md")
	plain := []string{}
	for _, l := range m.view(tl) {
		plain = append(plain, l.plain)
	}
	joined := strings.Join(plain, "\n")
	if strings.Contains(joined, "# 見出し") || !strings.Contains(joined, "見出し") || strings.Contains(joined, "- 項目") {
		t.Fatalf("Markdown が整形されていない:\n%s", joined)
	}
}

func TestViewWrapSetting(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat("x", 300)+"\n")
	tl := openTileAt(t, m, "a/long.txt")
	w := m.tileTextWidth(tl)
	if n := len(m.view(tl)); n != (300+w-1)/w {
		t.Fatalf("折り返した行の数 = %d (幅 %d)", n, w)
	}
	m.set.Wrap = false
	if n := len(m.view(tl)); n != 1 {
		t.Fatalf("wrap off で行の数 = %d", n)
	}
}

// 先へ読み進めたときは増えた行だけを足す。足した結果は最初から作り直したものと同じ。
func TestViewGrowsIncrementally(t *testing.T) {
	m := newTest(t)
	var b strings.Builder
	for i := range 30000 {
		b.WriteString("line " + itoa(i) + " " + strings.Repeat("y", i%90) + "\n")
	}
	writeRel(t, m, "a/big.txt", b.String())
	tl := openTileAt(t, m, "a/big.txt")
	first := len(m.view(tl))
	m.HandleKey("G")
	grown := append([]viewLine(nil), m.view(tl)...)
	if len(grown) <= first {
		t.Fatalf("読み進めても行が増えない: %d → %d", first, len(grown))
	}
	tl.vlines, tl.vkey, tl.vsrc = nil, viewKey{}, 0
	fresh := m.view(tl)
	if len(fresh) != len(grown) {
		t.Fatalf("足した行の数 %d と作り直した数 %d が違う", len(grown), len(fresh))
	}
	for i := range fresh {
		if fresh[i] != grown[i] {
			t.Fatalf("%d 行目が違う: %q / %q", i, grown[i].plain, fresh[i].plain)
		}
	}
}

// fakeDiff は gitDiffCommand を差し替える。HEAD 無し (unborn=false が失敗) も模せる。
func fakeDiff(t *testing.T, noHead bool, out string) *[]bool {
	t.Helper()
	var mu sync.Mutex
	calls := []bool{}
	orig := gitDiffCommand
	gitDiffCommand = func(_ context.Context, _, _ string, unborn bool) ([]byte, error) {
		mu.Lock()
		calls = append(calls, unborn)
		mu.Unlock()
		if noHead && !unborn {
			return nil, errors.New("bad revision 'HEAD'")
		}
		return []byte(out), nil
	}
	origHead := gitHeadExists
	gitHeadExists = func(context.Context, string) (bool, error) { return !noHead, nil }
	t.Cleanup(func() { gitDiffCommand, gitHeadExists = orig, origHead })
	return &calls
}

// HEAD が在るのに HEAD との diff が失敗したら、index との差に落とさず失敗を出す (未 stage の変更を隠さない)。
func TestDiffHeadFailureIsNotTakenAsUnborn(t *testing.T) {
	orig, origHead := gitDiffCommand, gitHeadExists
	t.Cleanup(func() { gitDiffCommand, gitHeadExists = orig, origHead })
	gitDiffCommand = func(_ context.Context, _, _ string, unborn bool) ([]byte, error) {
		if !unborn {
			return nil, errors.New("signal: killed")
		}
		return []byte("+index だけの差\n"), nil
	}
	gitHeadExists = func(context.Context, string) (bool, error) { return true, nil }
	m := newTest(t)
	markModified(t, m, "c.txt")
	tl := openTileAt(t, m, "c.txt")
	m.HandleKey("d")
	waitDiff(t, m)
	if got := m.view(tl)[0].plain; !strings.Contains(got, "diff を取れません") {
		t.Fatalf("HEAD が在るのに index だけの差を出した: %q", got)
	}
	gitHeadExists = func(context.Context, string) (bool, error) { return false, errors.New("rev-parse: killed") } // 判定そのものが失敗しても落とさない
	m.HandleKey("d")
	m.HandleKey("d")
	waitDiff(t, m)
	if got := m.view(tl)[0].plain; !strings.Contains(got, "diff を取れません") {
		t.Fatalf("HEAD の有無を判定できないのに別の差を出した: %q", got)
	}
}

// HEAD が無い repo (最初の commit の前) でも、作業ツリーの未 stage の変更が出る (空の tree との差。index との差では出ない)。
func TestUnbornDiffShowsUnstagedChanges(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	initRepo(t, dir, "main")
	mustWrite(t, filepath.Join(dir, "f"), "staged\n")
	gitIn(t, dir, "add", "f")
	mustWrite(t, filepath.Join(dir, "f"), "staged\nunstaged\n")
	out := strings.Join(fetchDiff(context.Background(), filepath.Join(dir, "f")), "\n")
	if !strings.Contains(out, "unstaged") {
		t.Fatalf("HEAD が無い repo で未 stage の変更が出ない:\n%s", out)
	}
}

// markModified は Git を止めて (裏の git の結果で上書きさせない)、rel を変更ありにする。
func markModified(t *testing.T, m *Model, rel string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(m.root.abs, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	setGit(m, " M "+rel+"\x00")
}

func waitDiff(t *testing.T, m *Model) {
	t.Helper()
	testwait.Until(t, "diff の取得", func() bool { return !m.diffBusy() })
}

func TestDiffToggle(t *testing.T) {
	calls := fakeDiff(t, true, "diff --git a/c.txt b/c.txt\n-old\n+new\n")
	m := newTest(t)
	markModified(t, m, "c.txt")
	tl := openTileAt(t, m, "c.txt")
	m.HandleKey("d")
	waitDiff(t, m)
	vl := m.view(tl)
	if tl.mode != modeDiff || len(vl) != 3 || vl[2].plain != "+new" {
		t.Fatalf("diff の表示: mode=%v %+v", tl.mode, vl)
	}
	if c := *calls; len(c) != 2 || c[0] || !c[1] {
		t.Fatalf("HEAD が無いとき index との差に倒れない: %v", c)
	}
	m.Advance(fixedNow) // タイルの開く動きは Advance で目標が決まる
	m.snapAll()         // 開くアニメーションを終点へ (途中は枠が小さく題が切れる)
	if !strings.Contains(strings.Join(m.View(), "\n"), "· diff") {
		t.Fatal("題に diff と出ない")
	}
	m.HandleKey("d")
	if tl.mode != modeFile || m.view(tl)[0].plain != "c" {
		t.Fatal("d で本体に戻らない")
	}
}

func TestDiffRefusedWithoutChanges(t *testing.T) {
	fakeDiff(t, false, "")
	m := newTest(t)
	m.set.Git = false
	tl := openTileAt(t, m, "c.txt")
	m.TakeNotices()
	m.HandleKey("d")
	if tl.mode != modeFile || tl.diff != nil {
		t.Fatal("変更の無いファイルで diff を取りに行った")
	}
	if ns := m.TakeNotices(); len(ns) != 1 || ns[0].OK {
		t.Fatalf("知らせ = %+v", ns)
	}
}

func TestDiffKeepsBusyWhileFetching(t *testing.T) {
	release := make(chan struct{})
	orig := gitDiffCommand
	gitDiffCommand = func(context.Context, string, string, bool) ([]byte, error) { <-release; return []byte("x\n"), nil }
	t.Cleanup(func() { gitDiffCommand = orig })
	m := newTest(t)
	markModified(t, m, "c.txt")
	openTileAt(t, m, "c.txt")
	settleBackground(t, m)
	m.HandleKey("d")
	if !m.Busy() {
		t.Fatal("diff の取得中に Busy が false")
	}
	m.snapAll()
	if !strings.Contains(strings.Join(m.View(), "\n"), "diff を取得中") {
		t.Fatal("取得中の表示が無い")
	}
	close(release)
	waitDiff(t, m)
	settleBackground(t, m)
	if m.Busy() {
		t.Fatal("取得が終わっても Busy")
	}
}

// 名前に glob の文字を含むファイルの diff に、他のファイルが混ざらない (レビューで再現 2026-10-09)。
func TestFetchDiffLiteralPathspec(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	write := func(name, body string) {
		mustWrite(t, filepath.Join(dir, name), body)
	}
	initRepo(t, dir, "main")
	write("a1.txt", "one\n")
	write("a[1].txt", "bracket\n")
	commitAll(t, dir)
	write("a1.txt", "one changed\n")
	write("a[1].txt", "bracket changed\n")
	got := strings.Join(fetchDiff(context.Background(), filepath.Join(dir, "a[1].txt")), "\n")
	if !strings.Contains(got, "+bracket changed") || strings.Contains(got, "one changed") {
		t.Fatalf("a[1].txt の diff:\n%s", got)
	}
}

// 幅を広げると折り返しの行が減る。末尾にいたタイルが行の先を指したまま空白にならない。
func TestResizeClampsTileScroll(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat(strings.Repeat("z", 200)+"\n", 40))
	m.Resize(60, 30)
	tl := openTileAt(t, m, "a/long.txt")
	m.HandleKey("G")
	if tl.scroll == 0 {
		t.Fatal("前提: 末尾へ動かない")
	}
	m.Resize(240, 30)
	if tl.scroll > m.maxScroll(tl) {
		t.Fatalf("広げた後の scroll = %d (最大 %d)", tl.scroll, m.maxScroll(tl))
	}
}

// Markdown は G で先へ進んでも上限を超えて読まない。
func TestMarkdownReadsUpToCap(t *testing.T) {
	m := newTest(t)
	line := strings.Repeat("w", 100) + "\n"
	writeRel(t, m, "a/huge.md", strings.Repeat(line, (markdownCap+(1<<20))/len(line)))
	tl := openTileAt(t, m, "a/huge.md")
	m.HandleKey("G")
	m.view(tl)
	if tl.src.off > markdownCap+chunkBytes {
		t.Fatalf("読んだ量 = %d (上限 %d)", tl.src.off, markdownCap)
	}
	if tl.src.eof {
		t.Fatal("前提: ファイルが上限より小さい")
	}
}

// 巨大な diff は上限で読むのをやめて返る (全部を読んでから切らない。止めた git を待って詰まらない)。
func TestFetchDiffStopsAtByteCap(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	line := strings.Repeat("v", 999) + "\n" // 長い行で、行の数の上限より先にバイトの上限に当てる
	n := diffMaxBytes/len(line) + 100
	mustWrite(t, p, "x\n")
	initRepo(t, dir, "main")
	commitAll(t, dir)
	mustWrite(t, p, strings.Repeat(line, n))
	lines := fetchDiff(context.Background(), p)
	if len(lines) < 2 || len(lines) > n {
		t.Fatalf("行の数 = %d (書いた行 %d)", len(lines), n)
	}
	if body := lines[len(lines)-2]; body != "+"+strings.TrimSuffix(line, "\n") {
		t.Fatalf("途中で切れた行を出した: %d 桁", len(body))
	}
	if !strings.Contains(lines[len(lines)-1], "先頭の") {
		t.Fatalf("打ち切った知らせが無い: %q", lines[len(lines)-1])
	}
}

// d で取り直した diff が前と同じ行の数でも、新しい中身を出す (描かずに d d と押したとき)。
func TestDiffToggleShowsRefetchedContent(t *testing.T) {
	var mu sync.Mutex
	n := 0
	orig := gitDiffCommand
	gitDiffCommand = func(context.Context, string, string, bool) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return []byte("+v" + itoa(n) + "\n"), nil
	}
	t.Cleanup(func() { gitDiffCommand = orig })
	m := newTest(t)
	markModified(t, m, "c.txt")
	tl := openTileAt(t, m, "c.txt")
	m.HandleKey("d")
	waitDiff(t, m)
	if got := m.view(tl)[0].plain; got != "+v1" {
		t.Fatalf("1 回目 = %q", got)
	}
	m.HandleKey("d") // 本体へ (描かない)
	m.HandleKey("d") // diff へ取り直す
	waitDiff(t, m)
	if got := m.view(tl)[0].plain; got != "+v2" {
		t.Fatalf("取り直した diff が出ない: %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 2 { // 開くたびに 1 回だけ取りに行く (TestDiffRefetchesOnEachToggle をここへ畳んだ。issue 735)
		t.Fatalf("取りに行った回数 = %d, want 2", n)
	}
}

// スクロールバーは枠の縦線の上でなく、その 1 桁内側に描く (枠・四隅と見分けるため。ユーザー回答 2026-10-09)。
func TestScrollbarIsInsideBorder(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat("x\n", 200))
	tl := openTileAt(t, m, "a/long.txt")
	settle(m)
	c := m.draw()
	r := slotRect(tl.slot, m.w, m.canvasH())
	thumb, track := 0, 0
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		if s := c.at(r.x+r.w-1, y).s; s != "│" {
			t.Fatalf("%d 行目の枠の縦線が %q (スクロールバーが枠に重なった)", y, s)
		}
		switch c.at(r.x+r.w-2, y).s {
		case "█":
			thumb++
		case "░":
			track++
		}
	}
	if thumb == 0 || track == 0 {
		t.Fatalf("内側の列のつまみ %d・溝 %d", thumb, track)
	}
	if widthOf("█") != 1 || widthOf("░") != 1 {
		t.Fatal("スクロールバーの文字が 1 桁でない (枠がずれる)")
	}
}

// d を連打しても git を重ねない: 本体へ戻すと取得中の diff の git を止める。
func TestDiffToggleCancelsInflightFetch(t *testing.T) {
	started := make(chan struct{}, 4)
	canceled := make(chan struct{}, 4)
	orig := gitDiffCommand
	gitDiffCommand = func(ctx context.Context, _, _ string, _ bool) ([]byte, error) {
		started <- struct{}{}
		<-ctx.Done()
		canceled <- struct{}{}
		return nil, ctx.Err()
	}
	t.Cleanup(func() { gitDiffCommand = orig })
	m := newTest(t)
	markModified(t, m, "c.txt")
	openTileAt(t, m, "c.txt")
	m.HandleKey("d") // diff へ (取得は止まったまま)
	<-started
	m.HandleKey("d") // 本体へ: 取得を止める
	// hang guard (止めなかったときだけ落とす)
	testwait.Recv(t, "本体へ戻しても取得中の git を止めない", canceled, testwait.Within(10*time.Second))
	waitDiff(t, m) // 取得の goroutine が終わってから抜ける (差し替え点を戻す後始末と、HEAD が無いときの 2 回目の呼び出しを重ねない)
}

// gitHeadExists は本物の git で、最初の commit の前は false・commit の後は true を返す。
func TestGitHeadExistsOnRealRepo(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	initRepo(t, dir, "main")
	if ok, err := gitHeadExists(context.Background(), dir); err != nil || ok {
		t.Fatalf("最初の commit の前: %v %v (false, nil のはず)", ok, err)
	}
	mustWrite(t, filepath.Join(dir, "f"), "x")
	commitAll(t, dir)
	if ok, err := gitHeadExists(context.Background(), dir); err != nil || !ok {
		t.Fatalf("commit の後: %v %v (true, nil のはず)", ok, err)
	}
}

// cellsAt は canvas の (x, y) から n 桁の文字を繋げたもの。
func cellsAt(c *canvas, x, y, n int) string {
	var b strings.Builder
	for i := range n {
		if q := c.at(x+i, y); q != nil {
			b.WriteString(q.s)
		}
	}
	return b.String()
}

// タイルの本文の左にファイルの行番号を出す (nvim の number)。折り返しの続き行には出さず、本文は溝の右から始まる。
func TestTileShowsLineNumbers(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/nums.txt", "aaa\n"+strings.Repeat("x", 300)+"\nccc\n")
	tl := openTileAt(t, m, "a/nums.txt")
	settle(m)
	g := m.tileGutter(tl)
	if g != 3 {
		t.Fatalf("溝の桁 = %d (3 行なので 2 桁 + 空白 = 3 のはず)", g)
	}
	c := m.draw()
	r := slotRect(tl.slot, m.w, m.canvasH())
	w := m.tileTextWidth(tl)
	wrapped := (300 + w - 1) / w
	want := map[int]string{0: " 1 ", 1: " 2 ", 2: "   ", 1 + wrapped: " 3 "}
	for row, gw := range want {
		if got := cellsAt(c, r.x+2, r.y+1+row, g); got != gw {
			t.Errorf("%d 行目の溝 = %q, want %q", row, got, gw)
		}
	}
	if got := cellsAt(c, r.x+2+g, r.y+1, 3); got != "aaa" {
		t.Fatalf("本文が溝の右から始まらない: %q", got)
	}
	if got := cellsAt(c, r.x+2+g, r.y+1+1+wrapped, 3); got != "ccc" {
		t.Fatalf("3 行目の本文 = %q", got)
	}
	tl.mode = modeDiff
	if m.tileGutter(tl) != 0 {
		t.Fatal("diff にも溝を出している (diff の行はファイルの行ではない)")
	}
}

// 1 行の長さの上限 (maxLineBytes) で切った続きは、ファイルの行ではないので番号を数えない。
// 🚨 切れるのは改行がまだ読めていないときだけ (読んだ塊に改行があれば長くても 1 行) なので、塊 (chunkBytes) より長い行にする。
func TestSrcLineSkipsSplitContinuation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "long.txt")
	mustWrite(t, p, "a\n"+strings.Repeat("b", 4*chunkBytes)+"\nc\n")
	s, err := openText(p)
	if err != nil {
		t.Fatal(err)
	}
	s.ensure(1000)
	n := len(s.lines)
	if n < 4 || len(s.cont) != n-3 {
		t.Fatalf("行の数 = %d・続き = %d (長い行が切れていない)", n, len(s.cont))
	}
	if s.srcLine(0) != 1 || s.srcLine(1) != 2 || s.srcLine(n-1) != 3 {
		t.Fatalf("行番号 = %d %d … %d, want 1 2 … 3", s.srcLine(0), s.srcLine(1), s.srcLine(n-1))
	}
	for i := 2; i < n-1; i++ {
		if s.srcLine(i) != 0 {
			t.Fatalf("%d 番目 (続きの行) の番号 = %d (0 のはず)", i, s.srcLine(i))
		}
	}
}

// Markdown の行番号は整形後の表示行の連番ではなく、ソース (.md) の行番号。
func TestMarkdownLineNumbersAreSourceLines(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/doc.md", "# 見出し\n\n- 項目\n")
	tl := openTileAt(t, m, "a/doc.md")
	srcOf := map[string]int{}
	for _, l := range m.view(tl) {
		srcOf[strings.TrimSpace(l.plain)] = l.src
	}
	found := 0
	for k, v := range srcOf {
		switch {
		case strings.Contains(k, "見出し") && v != 1:
			t.Fatalf("見出しの行番号 = %d (1 のはず)", v)
		case strings.Contains(k, "項目") && v != 3:
			t.Fatalf("項目の行番号 = %d (3 のはず)", v)
		case strings.Contains(k, "見出し"), strings.Contains(k, "項目"):
			found++
		}
	}
	if found != 2 {
		t.Fatalf("見出しと項目の行が見つからない: %v", srcOf)
	}
}

// ジャンプモードの下線は溝のぶんずらした本文のリンクの上に引く。
func TestJumpUnderlineShiftedByGutter(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/three.md")
	m.HandleKey("enter")
	tl := m.frontTile()
	settle(m)
	m.HandleKey("tab")
	ls := m.tileLinks(tl, 10)
	if len(ls) == 0 {
		t.Fatal("リンクが無い")
	}
	c := m.draw()
	r := slotRect(tl.slot, m.w, m.canvasH())
	l := ls[0]
	x, y := r.x+2+m.tileGutter(tl)+l.col, r.y+1+l.line-tl.scroll
	if got := cellsAt(c, x, y, widthOf(l.text)); got != l.text {
		t.Fatalf("リンクの位置の文字 = %q, want %q", got, l.text)
	}
	// 先頭・末尾に下線があり、1 桁手前 (溝ぶんずれた下線ならここに掛かる) には無い
	for _, k := range []int{0, widthOf(l.text) - 1} {
		if q := c.at(x+k, y); q == nil || !q.ul {
			t.Fatalf("リンクの %d 桁目に下線が無い", k)
		}
	}
	if q := c.at(x-1, y); q != nil && q.ul {
		t.Fatal("リンクの手前に下線が掛かっている (溝のぶんずれている)")
	}
}

// 読む上限 (Markdown の markdownCap) に達した後の ensure は、ファイルを開かずに戻る (Resize が毎フレーム呼ぶ)。
// 開きに行くと、消したファイルで読めない扱いになる。
func TestEnsureStopsAtLimitWithoutOpening(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.md")
	mustWrite(t, p, strings.Repeat("line\n", 3*chunkBytes/5)) // 1 回の読み (chunkBytes) で末尾に届かない大きさ
	src, err := openText(p)
	if err != nil {
		t.Fatal(err)
	}
	src.limit = chunkBytes
	src.ensure(1 << 30)
	if src.eof || src.off < src.limit {
		t.Fatalf("上限まで読んでいない: off=%d eof=%v", src.off, src.eof)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	src.ensure(1 << 30)
	if src.readErr || src.eof {
		t.Error("上限に達した後もファイルを開きに行った")
	}
}

// コードの色付けは描いた行にだけ付く: 3000 行を読んで末尾まで飛んでも、色付けの回数は描いた画面の行の数で済む
// (読み進めた行をまとめて色付けすると、読み足した 1 打が千行ぶんの chroma になる)。
func TestCodeHighlightOnlyForDrawnLines(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "big.go", strings.Repeat("package big // x\n", 3000))
	tl := openTileAt(t, m, "big.go")
	calls := 0
	hl := tl.hl
	tl.hl = func(s string) string { calls++; return hl(s) }
	m.HandleKey("G") // 末尾へ (読み切る)
	settle(m)
	m.View()
	if len(m.view(tl)) < 3000 {
		t.Fatalf("末尾まで読めていない: %d 行", len(m.view(tl)))
	}
	if rows := m.tileRows(tl); calls == 0 || calls > 2*rows {
		t.Fatalf("色付けが %d 回 (描いた画面は %d 行。0 は描いていない・行数を大きく超えるのは描かない行まで色付けしている)", calls, rows)
	}
	if !strings.Contains(tl.styledLine(len(tl.vlines)-1), "\x1b[") {
		t.Fatal("描いた行に色が付いていない")
	}
	before := calls
	m.View() // 同じ画面をもう一度描く: 付けた色は覚えていて、付け直さない
	if calls != before {
		t.Fatalf("同じ画面を描き直すたびに色付けし直す: %d → %d 回", before, calls)
	}
}
