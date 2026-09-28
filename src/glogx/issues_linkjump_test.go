package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"glogx/issues"
	"tuikit/anim"
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
	// 実体で持つ (macOS の /var は /private/var への symlink。開く対象は解いた実体になる)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	e.press("j") // src/b.go (docs/spec.md は .md なので viewer 内で開く。別のテスト)
	e.v.handleKey("enter", vp(10))
	if args, want := (*cmds)[1].Args, []string{"nvim", "-R", "--", e.path("src/b.go")}; !slices.Equal(args, want) {
		t.Fatalf("起動コマンド %q (want %q)", args, want)
	}
}

// .md は viewer の本文 pager に積んで開き、h/Esc で元の本文・位置・選択へ戻る (nvim は起動しない)。
func TestLinkJumpMarkdownOpensInViewerAndPops(t *testing.T) {
	cmds := stubEditorCapture(t)
	e := newJumpEnv(t, "# 001\n\n{filler}[仕様](../docs/spec.md) と `src/a.go`\n")
	if err := os.WriteFile(e.path("docs/spec.md"), []byte("# spec\n\n`src/b.go` を見る\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.press("tab")
	before := e.v.bodyPager.Offset
	issuePath := e.v.open.Path
	e.v.handleKey("enter", vp(10))
	if len(*cmds) != 0 {
		t.Fatalf(".md で nvim を起動した: %v", (*cmds)[0].Args)
	}
	if e.v.open.Path != e.path("docs/spec.md") || len(e.v.docStack) != 1 || e.v.linkJump.active {
		t.Fatalf("doc が積まれていない: open=%q stack=%d jump=%v", e.v.open.Path, len(e.v.docStack), e.v.linkJump.active)
	}
	if out := strings.Join(e.v.lines(renderOpts(20)), "\n"); !strings.Contains(out, "001-feat-jump.md へ戻る") {
		t.Fatalf("ヘッダーに戻り先が出ない:\n%s", out)
	}
	// doc の中でもジャンプできる (インラインコードの基準は元の issue のプロジェクト root)
	e.press("tab")
	if l, ok := e.selected(t); !ok || l.Path != e.path("src/b.go") {
		t.Fatalf("doc の中のジャンプ: %+v ok=%v", l, ok)
	}
	e.press("esc") // ジャンプを抜ける
	e.press("h")   // doc から戻る
	if e.v.open == nil || e.v.open.Path != issuePath || len(e.v.docStack) != 0 {
		t.Fatalf("元の本文へ戻らない: open=%v stack=%d", e.v.open, len(e.v.docStack))
	}
	if e.v.bodyPager.Offset != before || !e.v.linkJump.active {
		t.Fatalf("戻ったとき位置・選択が戻らない: offset %d→%d jump=%v", before, e.v.bodyPager.Offset, e.v.linkJump.active)
	}
	if l, _ := e.selected(t); l.Path != e.path("docs/spec.md") {
		t.Fatalf("戻ったときの選択が違う: %q", l.Path)
	}
	e.press("esc")
	e.press("h") // もう戻る段が無いので本文を閉じる
	if e.v.drawer.phase() != anim.Closing && e.v.open != nil {
		t.Fatal("底の本文で h を押しても閉じない")
	}
}

// doc を開いたまま再スキャン (見張り) が来ても、底の issue で照合するので畳まれない。画面の記憶も底の issue。
func TestLinkJumpDocSurvivesRescanAndScreenSavesRoot(t *testing.T) {
	e := newJumpEnv(t, "# 001\n\n[仕様](../docs/spec.md)\n")
	issuePath := e.v.open.Path
	e.press("tab")
	e.v.handleKey("enter", vp(10))
	if len(e.v.docStack) != 1 {
		t.Fatal("前提: doc が積まれていない")
	}
	e.v.rebindOpen(issuePath)
	if e.v.open == nil || e.v.open.Path != e.path("docs/spec.md") || e.v.docStack[0].open.Path != issuePath {
		t.Fatalf("再スキャンで doc が畳まれた / 底がずれた: open=%v", e.v.open)
	}
	if s, ok := e.v.screen(time.Now()); !ok || s.Open != issuePath {
		t.Fatalf("画面の記憶が底の issue でない: %+v ok=%v", s, ok)
	}
}

// #L12 で開いた doc はその行を窓に入れる。ディレクトリは nvim -R で開く。
func TestLinkJumpDocLineAndDirectory(t *testing.T) {
	cmds := stubEditorCapture(t)
	e := newJumpEnv(t, "# 001\n\n[仕様](../docs/spec.md#L40) と `src/`\n")
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		b.WriteString("行 " + strconv.Itoa(i) + "\n\n")
	}
	if err := os.WriteFile(e.path("docs/spec.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	e.press("tab")
	e.v.handleKey("enter", vp(10))
	out := strings.Join(e.v.lines(renderOpts(20)), "\n")
	if !strings.Contains(out, "行 20") || strings.Contains(out, "行 1\n") {
		t.Fatalf("#L40 の行へ送られていない:\n%s", out)
	}
	e.press("h") // 戻るとジャンプの選択 (spec.md) も戻る
	e.press("j") // src/
	e.v.handleKey("enter", vp(10))
	if len(*cmds) != 1 || !slices.Equal((*cmds)[0].Args, []string{"nvim", "-R", "--", e.path("src")}) {
		t.Fatalf("ディレクトリを nvim -R で開かない: %v", *cmds)
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

// 同じ dest の出現が減ったら、残った出現へ寄せてモードを保つ (消えた扱いにしない)。
func TestLinkJumpReanchorWhenOccurrencesShrink(t *testing.T) {
	// 3 個 → 2 個に減らし、3 個目を選んでいた状態から「最後に残った出現」(2 個目) へ寄せる
	// (最初の出現へ寄せる実装と区別するため、残りを 2 個にする)
	e := newJumpEnv(t, "# 001\n\n`src/a.go` と `src/a.go` と `src/a.go` と `src/b.go`\n")
	e.press("tab")
	e.press("j")
	e.press("j") // 3 個目の src/a.go
	if err := os.WriteFile(e.v.open.Path, []byte("# 001\n\n`src/a.go` と `src/a.go` と `src/b.go`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.v.reloadAfterEdit()
	e.v.lines(renderOpts(20))
	fl := e.v.jumpLinks(vp(10))
	if cur := e.v.linkJump.reanchor(fl); cur != 1 {
		t.Fatalf("最後に残った出現 (添字 1) へ寄せない: cur=%d", cur)
	}
}

// 窓の先頭行ちょうどにあるリンクは「見えている」ので、入ったときにそれを選ぶ。
func TestLinkJumpStartsAtWindowTopBoundary(t *testing.T) {
	e := newJumpEnv(t, "# 001\n\n`src/a.go`\n\n{filler}`src/b.go`\n\n{filler}`docs/spec.md`\n")
	fl := e.v.jumpLinks(vp(10))
	e.v.bodyPager.Offset = fl[1].Top
	e.press("tab")
	if l, _ := e.selected(t); l.Path != e.path("src/b.go") {
		t.Fatalf("窓の先頭行のリンクを選ばない: %q", l.Path)
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

// 一覧を作った後に repo の外への symlink へ差し替わったら (git pull 等)、開く直前の再確認で止める。
func TestLinkJumpOpenRechecksEscape(t *testing.T) {
	cmds := stubEditorCapture(t)
	e := newJumpEnv(t, jumpBody)
	e.press("tab")
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.path("src/a.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, e.path("src/a.go")); err != nil {
		t.Fatal(err)
	}
	e.v.handleKey("enter", vp(10))
	if len(*cmds) != 0 {
		t.Fatalf("repo の外へ差し替わったファイルを開いた: %v", (*cmds)[0].Args)
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
