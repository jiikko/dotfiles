package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"time"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
)

// atProgress は演出の進みを p (0..1) に固定した viewer を返す (壁時計を巻き戻して作る)。
func atProgress(v *issuesView, p float64) *issuesView {
	v.animStart = time.Now().Add(-time.Duration(float64(issuesAnimDuration) * p))
	return v
}

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
	const page = 17
	for range 100 {
		v.handleKey("ctrl+d", page)
	}
	// handleKey に渡すのは画面行数 (page)。実際にスクロールに使える行数はヘッダーを
	// 差し引いた visibleRows で、これが描画側とずれると末尾に届かなくなる
	if v.bodyOff != max(v.body.Len()-v.visibleRows(page), 0) {
		t.Fatalf("末尾を超えてスクロールした: off=%d len=%d rows=%d", v.bodyOff, v.body.Len(), v.visibleRows(page))
	}
	v.handleKey("g", page)
	if v.bodyOff != 0 {
		t.Fatalf("g で先頭へ戻らない: %d", v.bodyOff)
	}
}

// hasCursorMark はカーソル行の矢印が描かれている行があるか。
func hasCursorMark(lines []string) bool {
	for _, ln := range lines {
		if strings.HasPrefix(ln, cursorGutterMark) {
			return true
		}
	}
	return false
}

func TestIssuesViewCursorStaysVisibleAfterRowSetChanges(t *testing.T) {
	// ⚠️ 回帰防止: カーソルは常に窓の中にいる。行数やヘッダー高が変わる経路 (a / Tab /
	// 通知行の増加) で窓だけ先頭へ飛ぶと、カーソル行がどこにも描かれないまま
	// current() が見えない行を返し、Enter / v (nvim) / y が別の issue を対象にする。
	orig := copyToClipboard
	t.Cleanup(func() { copyToClipboard = orig })
	copyToClipboard = func(string) error { return nil }

	list := make([]*issues.Issue, 0, 50)
	for i := 50; i > 0; i-- {
		st := issues.StatusOpen
		if i <= 10 {
			st = issues.StatusDone
		}
		list = append(list, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", st))
	}
	const page = 12
	for _, key := range []string{"a", "tab", "y"} {
		v := loadedView(list...)
		v.handleKey("G", page) // カーソルを末尾へ (窓は下端に張り付く)
		if !hasCursorMark(v.lines(renderOpts(page))) {
			t.Fatalf("前提が崩れた: G の直後にカーソル行が描かれていない (key=%q)", key)
		}
		v.handleKey(key, page)
		if !hasCursorMark(v.lines(renderOpts(page))) {
			t.Fatalf("%q の後にカーソル行が窓の外へ出た:\n%s", key, strings.Join(v.lines(renderOpts(page)), "\n"))
		}
	}
}

func TestIssuesViewBodyScrollRecoversAfterWidthGrows(t *testing.T) {
	// 幅が広がると本文行数が減る。論理 bodyOff を収束させないと k / ctrl+u が
	// 「押しても画面が動かない」状態になる (上方向だけ固まる)。
	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "折り返しの対象になる長い段落 %d。ここは幅が狭いと複数行に割れ、広いと 1 行に収まる。\n\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	const page = 12
	v.handleKey("enter", page)

	narrow := issuesRenderOpts{width: 40, page: page}
	v.lines(narrow)
	v.handleKey("G", page) // 狭い幅での末尾へ
	v.lines(narrow)
	tail := v.bodyOff

	wide := issuesRenderOpts{width: 100, page: page}
	v.lines(wide) // 幅が広がって行数が減る
	if v.bodyOff >= tail {
		t.Fatalf("幅を広げても bodyOff が縮まっていない: %d -> %d", tail, v.bodyOff)
	}
	before := v.bodyOff
	v.handleKey("k", page)
	if v.bodyOff != before-1 {
		t.Fatalf("幅変更後の k が 1 行戻していない: %d -> %d", before, v.bodyOff)
	}
}

func TestIssuesViewGReachesLastLine(t *testing.T) {
	// キー操作側と描画側で使う行数が一致していることの担保 (ずれると G が末尾に届かない)。
	// 一覧と本文の両方で「G を押したら最後の行が実際に描かれる」ことを見る。
	many := make([]*issues.Issue, 0, 40)
	for i := 40; i > 0; i-- {
		many = append(many, fakeIssue(fmt.Sprintf("%03d", i), "feat", "x", issues.StatusOpen))
	}
	v := loadedView(many...)
	const page = 12
	v.handleKey("G", page)
	if out := strings.Join(v.lines(renderOpts(page)), "\n"); !strings.Contains(out, "001") {
		t.Fatalf("一覧で G が末尾に届いていない:\n%s", out)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "001-feat-long.md")
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "段落 %d\n\n", i)
	}
	b.WriteString("最終行マーカー\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "001-feat-long.md", Number: "001", Category: "feat"})
	v2.handleKey("enter", page)
	v2.lines(renderOpts(page)) // 幅ごとの整形を確定させてから G
	v2.handleKey("G", page)
	if out := strings.Join(v2.lines(renderOpts(page)), "\n"); !strings.Contains(out, "最終行マーカー") {
		t.Fatalf("本文で G が末尾に届いていない:\n%s", out)
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

func TestIssuesViewNoticeIsTransientAndDoesNotHideWarning(t *testing.T) {
	// ⚠️ 回帰防止: notice に寿命が無いと、コピーを 1 回した時点でスキャン警告
	// (同名ファイルの二重化 = conflict も error も出ない静かな内容喪失) が二度と出なくなる。
	orig := copyToClipboard
	t.Cleanup(func() { copyToClipboard = orig })
	copyToClipboard = func(string) error { return nil }

	v := newIssuesView()
	v.shown = true
	v.receive(issuesScanMsg{
		dirs:     []string{"/repo/issues"},
		issues:   sampleIssues(),
		warnings: []string{"同じファイル名が複数の状態ディレクトリにあります: 028-x.md / done/028-x.md"},
	})
	v.handleKey("y", 10)
	if !strings.Contains(strings.Join(v.lines(renderOpts(10)), "\n"), "コピーしました") {
		t.Fatal("コピーの結果が出ていない")
	}
	v.handleKey("j", 10) // 次のキーで通知は寿命を終える
	out := strings.Join(v.lines(renderOpts(10)), "\n")
	if strings.Contains(out, "コピーしました") {
		t.Fatalf("通知が次のキーでも残っている:\n%s", out)
	}
	if !strings.Contains(out, "同じファイル名") {
		t.Fatalf("通知が消えても警告が戻らない:\n%s", out)
	}
}

func TestIssuesViewBodyModeShowsCopyResult(t *testing.T) {
	// 本文モードのコピーは成功も失敗も画面に出ない (ヘッダーに通知の行が無い) 状態だった。
	// 全画面差し替えのためトーストは見えないので、受け皿はこのヘッダーしかない。
	orig := copyToClipboard
	t.Cleanup(func() { copyToClipboard = orig })
	copyToClipboard = func(string) error { return errors.New("pbcopy が見つかりません") }

	dir := t.TempDir()
	path := filepath.Join(dir, "028-refactor-x.md")
	if err := os.WriteFile(path, []byte("# 028 refactor: タイトル\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := loadedView(&issues.Issue{Path: path, Dir: dir, Rel: "028-refactor-x.md", Number: "028", Category: "refactor"})
	v.handleKey("enter", 10)
	v.handleKey("p", 10)
	if out := strings.Join(v.lines(renderOpts(10)), "\n"); !strings.Contains(out, "コピーに失敗しました") {
		t.Fatalf("本文モードでコピー失敗が画面に出ない:\n%s", out)
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

func TestIssuesViewCopyActions(t *testing.T) {
	orig := copyToClipboard
	t.Cleanup(func() { copyToClipboard = orig })
	var copied string
	copyToClipboard = func(text string) error { copied = text; return nil }

	v := loadedView(sampleIssues()...)
	v.root = "/repo"
	v.all[0].Title = "030 feat: 新機能"

	v.handleKey("p", 10)
	if copied != "030" {
		t.Fatalf("p が番号をコピーしていない: %q", copied)
	}
	if !strings.Contains(v.notice, "番号をコピーしました: 030") {
		t.Fatalf("通知が出ていない: %q", v.notice)
	}
	// Y = 番号 + タイトル + repo 相対パス (H1 の先頭番号は Display が落とす)
	v.handleKey("Y", 10)
	if copied != "issue 030 feat: 新機能 (issues/030-feat-a.md)" {
		t.Fatalf("Y の参照が想定と違う: %q", copied)
	}
	// N = 次に採番すべき番号 (fixture の最大は 030)
	v.handleKey("N", 10)
	if copied != "031" {
		t.Fatalf("N が次番号をコピーしていない: %q", copied)
	}
	// 番号なし issue ではファイル名に落として理由を通知する
	v2 := loadedView(&issues.Issue{Path: "/repo/issues/resource-leaks.md", Dir: "/repo/issues", Rel: "resource-leaks.md", Slug: "resource-leaks"})
	v2.handleKey("p", 10)
	if copied != "resource-leaks.md" || !strings.Contains(v2.notice, "番号が無い") {
		t.Fatalf("番号なしの扱いが想定と違う: copied=%q notice=%q", copied, v2.notice)
	}
}

func TestCategoryColorIsStableAndReservesRed(t *testing.T) {
	if categoryColor("bug") != catRed || categoryColor("fix") != catRed {
		t.Fatal("bug / fix が赤になっていない")
	}
	if categoryColor("feat") != catGreen {
		t.Fatal("feat が緑になっていない")
	}
	if categoryColor("") != ansiDim {
		t.Fatal("カテゴリ無しが dim になっていない")
	}
	// 表に無い語: 同じ語なら常に同じ色 (起動ごとに変わらない)
	first := categoryColor("waveform")
	for range 5 {
		if categoryColor("waveform") != first {
			t.Fatal("未知カテゴリの色が安定していない")
		}
	}
	// ⚠️ 未知語に赤を割らない (意味の無い語が「失敗」の色で出ると誤読される)
	for _, name := range []string{"waveform", "videoviewmodel", "dfs", "share", "auth", "acl", "codec", "thumbnail", "tabmanager"} {
		if categoryColor(name) == catRed {
			t.Fatalf("未知カテゴリ %q に赤を割った", name)
		}
	}
}

func TestIssuesViewRowPaintsCategoryColor(t *testing.T) {
	v := loadedView(fakeIssue("001", "bug", "x", issues.StatusOpen))
	o := renderOpts(80)
	o.colored = true
	joined := strings.Join(v.lines(o), "\n")
	if !strings.Contains(joined, catRed+"bug") {
		t.Fatalf("カテゴリ列に色が塗られていない:\n%q", joined)
	}
}

func TestIssuesViewSlideInAnimation(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.shown = true

	// 開始直後: まだほとんど画面外 (先頭行に右オフセットが入る)
	atProgress(v, 0.05)
	if !v.animating() {
		t.Fatal("演出中と判定されない")
	}
	early := v.lines(renderOpts(12))
	if len(early) != 12 {
		t.Fatalf("演出中も page 行を返すべき: %d", len(early))
	}
	shifted := false
	for _, ln := range early {
		if w := dispWidth(ln); w > 80 {
			t.Fatalf("演出中の行が幅を超えた (w=%d): %q", w, ln)
		}
		if strings.HasPrefix(ln, " ") && strings.TrimSpace(ln) != "" {
			shifted = true
		}
	}
	if !shifted {
		t.Fatalf("右からのスライドになっていない (オフセットが無い):\n%q", early)
	}

	// 進みの途中: 行ごとに開始がずれる (stagger) ので、上の行の方が先に着地している
	atProgress(v, 0.5)
	mid := v.lines(renderOpts(12))
	if offsetOf(mid[2]) > offsetOf(mid[len(mid)-1]) {
		t.Fatalf("上の行が先に着地していない: %q", mid)
	}

	// 着地後: 演出なしの静的描画と一致する
	v.finishAnim()
	if v.animating() {
		t.Fatal("finishAnim 後も演出中のまま")
	}
	want := v.lines(renderOpts(12))
	atProgress(v, 1.2) // duration を超えた進み
	if got := v.lines(renderOpts(12)); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("着地後の画面が静的描画と一致しない:\n%q\n%q", got, want)
	}
}

// offsetOf は行頭の空白の数 (スライドの残りオフセット)。空行は最大扱い。
func offsetOf(line string) int {
	if strings.TrimSpace(line) == "" {
		return math.MaxInt
	}
	return len(line) - len(strings.TrimLeft(line, " "))
}

func TestIssuesViewKeyLandsAnimationImmediately(t *testing.T) {
	v := loadedView(sampleIssues()...)
	v.shown = true
	atProgress(v, 0.1)
	v.handleKey("j", 12)
	if v.animating() {
		t.Fatal("キー入力で演出が着地していない (演出中は操作を待たせない契約)")
	}
}

func TestIssuesViewHintFitsPopupWidth(t *testing.T) {
	// hint は 1 行で、超過分は末尾から黙って切られる。popup の実幅 (84 桁) に収める
	const popupWidth = 84
	v := loadedView(sampleIssues()...)
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("一覧の hint が %d 桁に収まらない (w=%d): %q", popupWidth, w, v.hint())
	}
	v.showDone = true
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("done 表示中の hint が収まらない (w=%d): %q", w, v.hint())
	}
	v.open = v.rows[0]
	if w := dispWidth(v.hint()); w > popupWidth {
		t.Fatalf("本文の hint が収まらない (w=%d): %q", w, v.hint())
	}
}
