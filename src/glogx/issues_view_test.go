package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
)

// issuesView は browseModel を参照しないので、モデルを組まずに直接駆動できる
// (この独立性が「結合を最後にする」を成立させている。壊れたら結合コストが跳ね上がる)。

// fakeIssue はファイルシステムを使わずに Issue を作る (一覧の表示・フィルタのテスト用)。
func fakeIssue(number, category, slug string, st issues.Status) *issues.Issue {
	rel := number + "-" + category + "-" + slug + ".md"
	if st != issues.StatusOpen {
		rel = st.String() + "/" + rel
	}
	return &issues.Issue{
		Path:     "/repo/issues/" + rel,
		Dir:      "/repo/issues",
		Rel:      rel,
		Number:   number,
		Category: category,
		Slug:     slug,
		Status:   st,
	}
}

// loadedView は receive 済みの viewer を返す。
func loadedView(list ...*issues.Issue) *issuesView {
	v := newIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: list})
	return &v
}

func sampleIssues() []*issues.Issue {
	return []*issues.Issue{
		fakeIssue("030", "feat", "a", issues.StatusOpen),
		fakeIssue("029", "feat", "b", issues.StatusOpen),
		fakeIssue("028", "refactor", "c", issues.StatusPending),
		fakeIssue("027", "refactor", "d", issues.StatusDone),
		fakeIssue("026", "bug", "e", issues.StatusDone),
	}
}

func renderOpts(page int) issuesRenderOpts {
	return issuesRenderOpts{width: 80, page: page, colored: false}
}

func TestIssuesViewToggleScansOnlyOnce(t *testing.T) {
	v := newIssuesView()
	if cmd := v.toggle("/repo"); cmd == nil {
		t.Fatal("初回の toggle でスキャンの Cmd が返らない")
	}
	if !v.visible() || !v.loading() {
		t.Fatalf("表示・スキャン中になっていない: shown=%v scanning=%v", v.visible(), v.loading())
	}
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: sampleIssues()})
	if v.loading() {
		t.Fatal("receive 後もスキャン中のまま")
	}
	v.toggle("/repo") // 閉じる
	if v.visible() {
		t.Fatal("2 回目の toggle で閉じない")
	}
	if cmd := v.toggle("/repo"); cmd != nil {
		t.Fatal("再表示で再スキャンしてしまった (結果は保持しているはず)")
	}
}

func TestIssuesViewTabsAndDoneFilter(t *testing.T) {
	v := loadedView(sampleIssues()...)
	// 既定は done を伏せる: open 2 + pending 1 = 3 行
	if len(v.rows) != 3 {
		t.Fatalf("既定の行数が違う: %d", len(v.rows))
	}
	// タブは feat 2 / refactor 2 / (bug 1 は other へ)
	if len(v.tabs) != 3 || v.tabs[0].Name != "feat" {
		t.Fatalf("タブの組み立てが違う: %+v", v.tabs)
	}
	v.handleKey("a", 10)
	if len(v.rows) != 5 {
		t.Fatalf("a で done を含めていない: %d", len(v.rows))
	}
	// Tab 巡回: All → feat → refactor → other → All
	names := []string{"feat", "refactor", "other", ""}
	for _, want := range names {
		v.handleKey("tab", 10)
		if got := v.currentTab(); got != want {
			t.Fatalf("タブ巡回が想定と違う: want %q got %q", want, got)
		}
	}
	// feat タブでは feat の 2 件だけ
	v.handleKey("tab", 10)
	if len(v.rows) != 2 {
		t.Fatalf("feat タブの行数が違う: %d", len(v.rows))
	}
	for _, iss := range v.rows {
		if iss.Category != "feat" {
			t.Fatalf("feat タブに %s が混ざった", iss.Rel)
		}
	}
}

func TestIssuesViewLinesAlwaysExactlyPageRows(t *testing.T) {
	for _, page := range []int{3, 5, 20, 40} {
		for _, v := range []*issuesView{loadedView(sampleIssues()...), loadedView(), {shown: true, scanning: true}} {
			got := v.lines(renderOpts(page))
			if len(got) != page {
				t.Fatalf("page=%d なのに %d 行返った", page, len(got))
			}
			for i, ln := range got {
				if w := dispWidth(ln); w > 80 {
					t.Fatalf("page=%d 行 %d が幅を超えた (w=%d): %q", page, i, w, ln)
				}
			}
		}
	}
}

func TestIssuesViewRowShowsNumberBadgeCategoryTitle(t *testing.T) {
	v := loadedView(sampleIssues()...)
	lines := v.lines(renderOpts(20))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"030", "○", "feat", "028", "⏸", "refactor"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("一覧に %q が出ない:\n%s", want, joined)
		}
	}
	// 矢印はカーソル行だけに 1 本出る (先頭行 = 030)
	marked := make([]string, 0, 1)
	for _, ln := range lines {
		if strings.HasPrefix(ln, cursorGutterMark) {
			marked = append(marked, ln)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "030") {
		t.Fatalf("カーソル行の矢印が想定と違う: %q", marked)
	}
}

func TestIssuesViewCursorScrollsWithinWindow(t *testing.T) {
	many := make([]*issues.Issue, 0, 40)
	for i := 40; i > 0; i-- {
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", issues.StatusOpen))
	}
	v := loadedView(many...)
	const rows = 10
	for range 20 {
		v.handleKey("j", rows)
	}
	if v.cursor != 20 {
		t.Fatalf("カーソルが動いていない: %d", v.cursor)
	}
	if v.offset == 0 || v.cursor < v.offset || v.cursor >= v.offset+rows {
		t.Fatalf("カーソルが画面内に収まっていない: cursor=%d offset=%d", v.cursor, v.offset)
	}
	v.handleKey("G", rows)
	if v.cursor != len(v.rows)-1 {
		t.Fatalf("G で末尾へ行かない: %d", v.cursor)
	}
	v.handleKey("g", rows)
	if v.cursor != 0 || v.offset != 0 {
		t.Fatalf("g で先頭へ戻らない: cursor=%d offset=%d", v.cursor, v.offset)
	}
	// 端で止まる (負のインデックスにならない)
	v.handleKey("k", rows)
	if v.cursor != 0 {
		t.Fatalf("先頭で k がはみ出した: %d", v.cursor)
	}
}

func TestIssuesViewBodyModeOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	body := "# 028 refactor: タイトル\n\n本文の段落。\n\n- 箇条書き\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := &issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor", Slug: "x"}
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	v := loadedView(iss)
	v.handleKey("enter", 10)
	if v.open == nil || v.body == nil {
		t.Fatal("Enter で本文モードに入らない")
	}
	out := strings.Join(v.lines(renderOpts(20)), "\n")
	for _, want := range []string{"028-refactor-x.md", "タイトル", "本文の段落", "• 箇条書き"} {
		if !strings.Contains(out, want) {
			t.Fatalf("本文表示に %q が出ない:\n%s", want, out)
		}
	}
	v.handleKey("h", 10)
	if v.open != nil {
		t.Fatal("h で一覧へ戻らない")
	}
}

func TestIssuesViewBodyScrollClampsToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "段落 %d\n\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	v.handleKey("enter", 10)
	// 描画で行数が確定してからスクロールする (Body は幅ごとに整形結果をキャッシュする)
	v.lines(renderOpts(20))
	for range 100 {
		v.handleKey("ctrl+d", 17)
	}
	if v.bodyOff != max(v.body.Len()-17, 0) {
		t.Fatalf("末尾を超えてスクロールした: off=%d len=%d", v.bodyOff, v.body.Len())
	}
	v.handleKey("g", 17)
	if v.bodyOff != 0 {
		t.Fatalf("g で先頭へ戻らない: %d", v.bodyOff)
	}
}

func TestIssuesViewCopyPathAndEditor(t *testing.T) {
	origCopy, origEditor := copyToClipboard, runEditorCmd
	t.Cleanup(func() { copyToClipboard, runEditorCmd = origCopy, origEditor })
	var copied string
	copyToClipboard = func(text string) error { copied = text; return nil }
	editorCalled := false
	runEditorCmd = func(cmd *exec.Cmd) tea.Cmd {
		editorCalled = true
		return func() tea.Msg { return editorClosedMsg{} }
	}

	v := loadedView(sampleIssues()...)
	v.handleKey("y", 10)
	if copied != v.rows[0].Path {
		t.Fatalf("カーソル行のパスがコピーされていない: %q", copied)
	}
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "コピーしました") {
		t.Fatal("コピーの結果が画面に出ない")
	}
	if cmd := v.handleKey("v", 10); cmd == nil || !editorCalled {
		t.Fatalf("v でエディタ起動の Cmd が返らない: cmd=%v called=%v", cmd != nil, editorCalled)
	}
}

func TestIssuesViewRescanReturnsCmd(t *testing.T) {
	v := loadedView(sampleIssues()...)
	cmd := v.handleKey("r", 10)
	if cmd == nil {
		t.Fatal("r で再スキャンの Cmd が返らない")
	}
	if !v.loading() {
		t.Fatal("r の後にスキャン中になっていない")
	}
	// Cmd を実行すると探索結果の msg が返る (cwd が空でもクラッシュしない)
	if msg := cmd(); msg == nil {
		t.Fatal("再スキャンの Cmd が msg を返さない")
	}
}

func TestIssuesViewEmptyStates(t *testing.T) {
	// ディレクトリが 1 つも無い
	v := newIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{})
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "見つかりません") {
		t.Fatal("issues ディレクトリ不在の案内が出ない")
	}
	// タブに未完了が無い (done だけ)
	v2 := loadedView(fakeIssue("001", "feat", "a", issues.StatusDone))
	if !strings.Contains(strings.Join(v2.lines(renderOpts(10)), "\n"), "a: done も表示") {
		t.Fatal("done だけのときの案内が出ない")
	}
}

func TestIssuesViewShowsScanWarning(t *testing.T) {
	v := newIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{
		dirs:     []string{"/repo/issues"},
		issues:   sampleIssues(),
		warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "同じファイル名") {
		t.Fatal("スキャンの警告が表示されない")
	}
}

func TestIssuesViewHintChangesByMode(t *testing.T) {
	v := loadedView(sampleIssues()...)
	if !strings.Contains(v.hint(), "カテゴリ") {
		t.Fatalf("一覧の hint が想定と違う: %q", v.hint())
	}
	v.open = v.rows[0]
	if !strings.Contains(v.hint(), "一覧へ") {
		t.Fatalf("本文の hint が想定と違う: %q", v.hint())
	}
}
