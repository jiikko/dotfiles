package filer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestXterm256(t *testing.T) {
	cases := map[int]rgb{1: {205, 0, 0}, 16: {0, 0, 0}, 196: {255, 0, 0}, 231: {255, 255, 255}, 232: {8, 8, 8}, 255: {238, 238, 238}}
	for n, want := range cases {
		if got := xterm256(n); got != want {
			t.Fatalf("xterm256(%d) = %v, want %v", n, got, want)
		}
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
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.root.reload()
	if n := m.findNode(filepath.Join(m.root.abs, "a")); n != nil {
		n.reload()
	}
}

func TestViewHighlightsCode(t *testing.T) {
	m := newTest(t)
	tl := openTileAt(t, m, "a/two.go")
	vl := m.view(tl)
	if len(vl) != 1 || vl[0].plain != "package two" || !strings.Contains(vl[0].styled, "\x1b[") {
		t.Fatalf("Go の行に色が付かない: %+v", vl)
	}
	if stripSGR(vl[0].styled) != vl[0].plain {
		t.Fatalf("色を外すと元の行に戻らない: %q", vl[0].styled)
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

// fakeDiff は gitDiffCommand を差し替える。HEAD 無し (cached=false が失敗) も模せる。
func fakeDiff(t *testing.T, noHead bool, out string) *[]bool {
	t.Helper()
	var mu sync.Mutex
	calls := []bool{}
	orig := gitDiffCommand
	gitDiffCommand = func(_ context.Context, _, _ string, cached bool) ([]byte, error) {
		mu.Lock()
		calls = append(calls, cached)
		mu.Unlock()
		if noHead && !cached {
			return nil, errors.New("bad revision 'HEAD'")
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { gitDiffCommand = orig })
	return &calls
}

// markModified は Git を止めて (裏の git の結果で上書きさせない)、rel を変更ありにする。
func markModified(t *testing.T, m *Model, rel string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(m.root.abs, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.set.Git = false
	setGit(m, " M "+rel+"\x00")
}

func waitDiff(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.diffBusy() {
		if time.Now().After(deadline) {
			t.Fatal("diff の取得が終わらない")
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 裏の goroutine の完了を条件で待つループの刻み
	}
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
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("a1.txt", "one\n")
	write("a[1].txt", "bracket\n")
	git("add", ".")
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
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

// d で diff へ切り替えるたびに取り直す (開いている間の変更を出す)。
func TestDiffRefetchesOnEachToggle(t *testing.T) {
	calls := fakeDiff(t, false, "+x\n")
	m := newTest(t)
	markModified(t, m, "c.txt")
	openTileAt(t, m, "c.txt")
	for range 2 {
		m.HandleKey("d") // diff へ
		waitDiff(t, m)
		m.HandleKey("d") // 本体へ
	}
	if n := len(*calls); n != 2 {
		t.Fatalf("取りに行った回数 = %d, want 2", n)
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
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	p := filepath.Join(dir, "big.txt")
	line := strings.Repeat("v", 999) + "\n" // 長い行で、行の数の上限より先にバイトの上限に当てる
	n := diffMaxBytes/len(line) + 100
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
	if err := os.WriteFile(p, []byte(strings.Repeat(line, n)), 0o644); err != nil {
		t.Fatal(err)
	}
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

// d で取り直した diff が前と同じ行の数でも、新しい中身を出す (描かずに d d と押したとき。2 周目のレビューの指摘)。
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
}

// スクロールバーは枠の縦線の上でなく、その 1 桁内側に描く (枠・四隅と見分けるため。ユーザー回答 2026-10-09)。
func TestScrollbarIsInsideBorder(t *testing.T) {
	m := newTest(t)
	writeRel(t, m, "a/long.txt", strings.Repeat("x\n", 200))
	tl := openTileAt(t, m, "a/long.txt")
	m.Advance(fixedNow)
	m.snapAll()
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

// d を連打しても git を重ねない: 本体へ戻すと取得中の diff の git を止める (issue 693)。
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
	select {
	case <-canceled:
	case <-time.After(10 * time.Second): // hang guard (止めなかったときだけ落とす)
		t.Fatal("本体へ戻しても取得中の git を止めない")
	}
	waitDiff(t, m) // 取得の goroutine が終わってから抜ける (差し替え点を戻す後始末と、--cached の 2 回目の呼び出しを重ねない)
}
