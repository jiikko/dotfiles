package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"glogx/issues"
)

// jumpEnv は repo root + issues/ + 実在するファイルを持つ本文を開いた viewer。
type jumpEnv struct {
	v    *issuesView
	root string
}

func (e *jumpEnv) press(key string) { e.v.handleKey(key, vp(10)) }

func (e *jumpEnv) path(rel string) string { return filepath.Join(e.root, rel) }

// newJumpEnv は body を本文に持つ issue を開く。body 中の {filler} は窓より長い埋め草に置き換える。
func newJumpEnv(t *testing.T, body string) *jumpEnv {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"src/a.go", "src/b.go", "docs/spec.md"} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "issues")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "001-feat-jump.md"
	path := filepath.Join(dir, rel)
	body = strings.ReplaceAll(body, "{filler}", strings.Repeat("埋め草の行。\n\n", 30))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: rel, Number: "001", Category: "feat"})
	v.root = root
	v.handleKey("enter", vp(10))
	if v.open == nil || v.body == nil {
		t.Fatal("本文モードに入れていない")
	}
	v.drawer.finish()
	v.lines(renderOpts(20))
	return &jumpEnv{v: v, root: root}
}

// selected は選択中のリンク (モード外なら ok=false)。
func (e *jumpEnv) selected(t *testing.T) (issues.FileLink, bool) {
	t.Helper()
	if !e.v.linkJump.active {
		return issues.FileLink{}, false
	}
	fl := e.v.jumpLinks(vp(10))
	i := e.v.linkJump.reanchor(fl)
	if i < 0 {
		t.Fatal("モード中なのに選択が一覧に無い")
	}
	return fl[i], true
}

const jumpBody = "# 001 feat: jump\n\n`src/a.go:7` と `src/none.go` と [仕様](../docs/spec.md) と `src/b.go`\n"

func TestLinkJumpMovesOverExistingPathsOnly(t *testing.T) {
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	got := make([]string, 0, 4)
	for range 4 { // 3 個 + 巻いて先頭
		l, ok := e.selected(t)
		if !ok {
			t.Fatal("ジャンプモードに入っていない")
		}
		got = append(got, l.Path)
		e.press("j")
	}
	want := []string{e.path("src/a.go"), e.path("docs/spec.md"), e.path("src/b.go"), e.path("src/a.go")}
	if !slices.Equal(got, want) {
		t.Fatalf("j で辿る順 %q (want %q。実在しない src/none.go は飛ばす・末尾で先頭へ巻く)", got, want)
	}
	e.press("g")
	e.press("k")
	if l, _ := e.selected(t); l.Path != e.path("src/b.go") {
		t.Fatalf("先頭の k が末尾へ巻かない: %q", l.Path)
	}
	e.press("g")
	if l, _ := e.selected(t); l.Path != e.path("src/a.go") {
		t.Fatalf("g で先頭へ: %q", l.Path)
	}
	e.press("G")
	if l, _ := e.selected(t); l.Path != e.path("src/b.go") {
		t.Fatalf("G で末尾へ: %q", l.Path)
	}
}

func TestLinkJumpEnterOpensReadonly(t *testing.T) {
	cmds := stubEditorCapture(t)
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	if cmd := e.v.handleKey("enter", vp(10)); cmd == nil {
		t.Fatal("Enter が Cmd を返さない")
	}
	if len(*cmds) != 1 {
		t.Fatalf("起動 %d 回", len(*cmds))
	}
	if args, want := (*cmds)[0].Args, []string{"nvim", "-R", "+7", "--", e.path("src/a.go")}; !slices.Equal(args, want) {
		t.Fatalf("起動コマンド %q (want %q)", args, want)
	}
	if !e.v.linkJump.active || e.v.open == nil {
		t.Fatal("開いた後もジャンプモードと本文は残る (戻ってきて次のリンクを開けるように)")
	}
	// 行番号の無いリンクは +N を付けない
	e.press("j")
	e.v.handleKey("enter", vp(10))
	if args, want := (*cmds)[1].Args, []string{"nvim", "-R", "--", e.path("docs/spec.md")}; !slices.Equal(args, want) {
		t.Fatalf("起動コマンド %q (want %q)", args, want)
	}
}

// e / y は選択中のリンクへ効く (issue 自体を開く・コピーしない)。
func TestLinkJumpActionKeysTargetLink(t *testing.T) {
	cmds := stubEditorCapture(t)
	copied := stubClipboard(t)
	pinFallbackEditor(t)
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	e.press("j")
	e.press("y")
	if *copied != e.path("docs/spec.md") {
		t.Fatalf("y がリンクのパスでない: %q", *copied)
	}
	e.v.handleKey("e", vp(10))
	if len(*cmds) != 1 || !slices.Equal((*cmds)[0].Args, []string{"nvim", e.path("docs/spec.md")}) {
		t.Fatalf("e がリンクを $EDITOR で開かない: %v", *cmds)
	}
}

func TestLinkJumpExitAndFallThrough(t *testing.T) {
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	e.press("esc")
	if e.v.linkJump.active || e.v.open == nil {
		t.Fatalf("Esc はモードだけ抜けて本文は残す: active=%v open=%v", e.v.linkJump.active, e.v.open != nil)
	}
	// 捌かないキーはモードを抜けて通常どおり処理する (u は URL ピッカー。URL が無いので案内が出る)
	e.press("tab")
	e.v.takeNotice()
	e.press("u")
	if e.v.linkJump.active {
		t.Fatal("u でモードを抜けていない")
	}
	if msg, _ := e.v.takeNotice(); !strings.Contains(msg, "URL") {
		t.Fatalf("抜けた後に u が通常のキーとして処理されていない: %q", msg)
	}
	// 隣の issue へ行く操作 (openIssue) はモードを捨てる
	e.press("tab")
	e.v.openIssue(e.v.open)
	if e.v.linkJump.active {
		t.Fatal("本文を開き直してもモードが残った")
	}
}

func TestLinkJumpNoLinksNotifies(t *testing.T) {
	e := newJumpEnv(t, "# 001\n\n`src/none.go` と `make test`\n")
	e.press("tab")
	if e.v.linkJump.active {
		t.Fatal("開けるリンクが無いのにモードに入った")
	}
	if msg, _ := e.v.takeNotice(); !strings.Contains(msg, "開けるファイルパスはありません") {
		t.Fatalf("案内が無い: %q", msg)
	}
}

// 入った時点では今見えている窓の最初のリンクを選び、窓の外のリンクを選んだら本文を送って見せる。
func TestLinkJumpStartsInWindowAndScrolls(t *testing.T) {
	e := newJumpEnv(t, "# 001\n\n`src/a.go`\n\n{filler}`src/b.go`\n\n{filler}`docs/spec.md`\n")
	fl := e.v.jumpLinks(vp(10))
	if len(fl) != 3 {
		t.Fatalf("前提: リンク 3 個 (got %d)", len(fl))
	}
	rows := e.v.visibleRows(vp(10))
	e.v.bodyPager.Offset = fl[1].Top - 1 // 2 個目だけが窓に入る位置
	e.press("tab")
	if l, _ := e.selected(t); l.Path != e.path("src/b.go") {
		t.Fatalf("窓の最初のリンクを選ばない: %q", l.Path)
	}
	e.press("j")
	off := e.v.bodyPager.Offset
	if top := fl[2].Top; top < off || top >= off+rows {
		t.Fatalf("選んだリンク (行 %d) が窓 [%d, %d) の外", top, off, off+rows)
	}
}

// 選択中のリンクは反転、他の開けるリンクは下線で描かれ、ヘッダーに開く先が出る。
func TestLinkJumpRendersHighlightAndHeader(t *testing.T) {
	e := newJumpEnv(t, jumpBody)
	o := renderOpts(20)
	o.colored = true
	before := e.v.lines(o)
	e.press("tab")
	e.press("j")
	out := strings.Join(e.v.lines(o), "\n")
	if !strings.Contains(out, "\x1b[7m\x1b[1m仕様") {
		t.Fatalf("選択中のリンクが反転していない:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[4msrc/a.go:7") || !strings.Contains(out, "\x1b[4msrc/b.go") {
		t.Fatalf("開けるリンクに下線が無い:\n%s", out)
	}
	if strings.Contains(out, "\x1b[4msrc/none.go") {
		t.Fatalf("実在しないパスに下線が付いた:\n%s", out)
	}
	if !strings.Contains(out, "パス 2/3: docs/spec.md") {
		t.Fatalf("ヘッダーに開く先が出ない:\n%s", out)
	}
	if after := e.v.lines(o); len(after) != len(before) {
		t.Fatalf("ジャンプで画面の行数が変わった: %d → %d", len(before), len(after))
	}
}

// 本文が読み直されても (エディタから戻った・見張り)、選択は同じリンクを指し続ける。
func TestLinkJumpSurvivesBodyReload(t *testing.T) {
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	e.press("j") // docs/spec.md
	// 先頭に別のリンクを足す = 添字で持っていたら 1 つずれる
	if err := os.WriteFile(e.v.open.Path, []byte("# 001\n\n`src/b.go` を先に\n\n"+jumpBody), 0o644); err != nil {
		t.Fatal(err)
	}
	e.v.reloadAfterEdit()
	if l, ok := e.selected(t); !ok || l.Path != e.path("docs/spec.md") {
		t.Fatalf("読み直しで選択がずれた: %+v ok=%v", l, ok)
	}
	// 選んでいたリンクが消えたらモードを抜ける
	if err := os.WriteFile(e.v.open.Path, []byte("# 001\n\n`src/a.go`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.v.reloadAfterEdit()
	e.v.lines(renderOpts(20))
	if e.v.linkJump.active {
		t.Fatal("選択していたリンクが消えたのにモードが残った")
	}
}

// 開く直前に消えていたら開かずに知らせる。
func TestLinkJumpOpenRechecksFile(t *testing.T) {
	cmds := stubEditorCapture(t)
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	if err := os.Remove(e.path("src/a.go")); err != nil {
		t.Fatal(err)
	}
	e.v.handleKey("enter", vp(10))
	if len(*cmds) != 0 {
		t.Fatalf("消えたファイルを開いた: %v", *cmds)
	}
	if msg, _ := e.v.takeNotice(); !strings.Contains(msg, "見つかりません") {
		t.Fatalf("案内が無い: %q", msg)
	}
}

// 同じパスが 2 回出ても別々に止まる (1 つに潰すと 2 個目の位置へ行けない)。
func TestLinkJumpRepeatedDestStopsTwice(t *testing.T) {
	e := newJumpEnv(t, "# 001\n\n`src/a.go`\n\n{filler}`src/a.go`\n")
	e.press("tab")
	first := e.v.bodyPager.Offset
	e.press("j")
	fl := e.v.jumpLinks(vp(10))
	if len(fl) != 2 || e.v.linkJump.reanchor(fl) != 1 {
		t.Fatalf("2 個目の出現に止まらない: n=%d cur=%d", len(fl), e.v.linkJump.reanchor(fl))
	}
	if e.v.bodyPager.Offset == first {
		t.Fatal("2 個目の出現まで本文が送られていない")
	}
}
