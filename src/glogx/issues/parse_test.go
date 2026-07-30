package issues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture は状態ディレクトリつきの issue ディレクトリを作って返す。
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "030-feat-new-thing.md", "028-refactor-box-args.md",
		"resource-leaks-2025-12-25.md", // 番号なしの issue (末尾に並ぶ)
		"README.md")                    // issue ではない付随ファイル (除外される)
	mkFiles(t, filepath.Join(dir, "pending"), "004-docs-later.md")
	mkFiles(t, filepath.Join(dir, "done"), "001-feat-old.md", "002-bug-fixed.md")
	mkFiles(t, filepath.Join(dir, "mid-long-term"), "003-research-horizon.md")
	return dir
}

func TestScanReadsStatusFromDirectory(t *testing.T) {
	issues, warns := Scan([]string{fixture(t)})
	if len(warns) != 0 {
		t.Fatalf("想定外の警告: %q", warns)
	}
	got := make(map[string]Status, len(issues))
	for _, iss := range issues {
		got[filepath.Base(iss.Rel)] = iss.Status
	}
	want := map[string]Status{
		"030-feat-new-thing.md":        StatusOpen,
		"028-refactor-box-args.md":     StatusOpen,
		"resource-leaks-2025-12-25.md": StatusOpen,
		"004-docs-later.md":            StatusPending,
		"001-feat-old.md":              StatusDone,
		"002-bug-fixed.md":             StatusDone,
		"003-research-horizon.md":      StatusUnknown, // 未知のサブディレクトリは状態へ写像しない
	}
	if _, ok := got["README.md"]; ok {
		t.Fatal("README.md を issue として拾ってしまった")
	}
	if len(got) != len(want) {
		t.Fatalf("件数が違う: got %d want %d (%v)", len(got), len(want), got)
	}
	for name, st := range want {
		if got[name] != st {
			t.Fatalf("%s の状態: want %v got %v", name, st, got[name])
		}
	}
}

func TestScanKeepsUnknownSubdirNameAsGroup(t *testing.T) {
	issues, _ := Scan([]string{fixture(t)})
	for _, iss := range issues {
		if filepath.Base(iss.Rel) == "003-research-horizon.md" {
			if iss.Group != "mid-long-term" {
				t.Fatalf("未知サブディレクトリ名が残っていない: %q", iss.Group)
			}
			return
		}
	}
	t.Fatal("mid-long-term のファイルが拾えていない")
}

func TestNewIssueParsesNamingSchemes(t *testing.T) {
	cases := []struct {
		name     string
		number   string
		category string
		slug     string
	}{
		{"028-refactor-glogx-box.md", "028", "refactor", "glogx-box"},
		{"014-research-keyboard-2026-07-12.md", "014", "research", "keyboard-2026-07-12"},
		{"042-videoviewmodel-proxy-properties.md", "042", "videoviewmodel", "proxy-properties"},
		{"UI-005-remove-memo.md", "005", "ui", "remove-memo"}, // 第 2 の ID スキーム
		{"PERF-003-thumbnail.md", "003", "perf", "thumbnail"}, // 同上
		{"083-handover.md", "083", "", "handover"},            // 単一トークン = カテゴリ扱いしない
		{"README.md", "", "", "README"},                       // 非 issue ファイル
		// 番号なしのファイルはカテゴリを取らない (先頭語をタブにすると意味のないタブが増える)
		{"resource-leaks-2025-12-25.md", "", "", "resource-leaks-2025-12-25"},
	}
	for _, c := range cases {
		iss := newIssue("/d", c.name)
		if iss.Number != c.number || iss.Category != c.category || iss.Slug != c.slug {
			t.Fatalf("%s: got (%q,%q,%q) want (%q,%q,%q)",
				c.name, iss.Number, iss.Category, iss.Slug, c.number, c.category, c.slug)
		}
	}
}

func TestSortIssuesNumberDescendingNumberlessLast(t *testing.T) {
	issues, _ := Scan([]string{fixture(t)})
	order := make([]string, 0, len(issues))
	for _, iss := range issues {
		order = append(order, filepath.Base(iss.Rel))
	}
	if order[0] != "030-feat-new-thing.md" || order[1] != "028-refactor-box-args.md" {
		t.Fatalf("番号の降順になっていない: %q", order)
	}
	if order[len(order)-1] != "resource-leaks-2025-12-25.md" {
		t.Fatalf("番号なしが末尾でない: %q", order)
	}
}

func TestTabsDerivedFromFilenamesAndMinorityMerged(t *testing.T) {
	issues, _ := Scan([]string{fixture(t)})
	// fixture のカテゴリ: feat 2, refactor 1, docs 1, bug 1, research 1, (なし) 1 = 番号なし
	tabs := Tabs(issues, TabMinCount)
	if len(tabs) != 2 || tabs[0].Name != "feat" || tabs[0].Count != 2 {
		t.Fatalf("タブの組み立てが想定と違う: %+v", tabs)
	}
	if tabs[1].Name != OtherTab || tabs[1].Count != 5 {
		t.Fatalf("少数派が other に寄っていない: %+v", tabs)
	}
	// minCount=1 なら全カテゴリが独立タブになる
	if all := Tabs(issues, 1); len(all) != 6 {
		t.Fatalf("minCount=1 でタブ数が想定と違う: %+v", all)
	}
}

func TestFilterByTabAndDone(t *testing.T) {
	issues, _ := Scan([]string{fixture(t)})
	// 既定 (done を伏せる)
	open := Filter(issues, "", false)
	for _, iss := range open {
		if iss.Status == StatusDone {
			t.Fatalf("done が既定で混ざっている: %s", iss.Rel)
		}
	}
	if len(Filter(issues, "", true)) != len(issues) {
		t.Fatal("showDone=true で全件出ていない")
	}
	// feat タブ (done を含めると 2 件)
	if got := Filter(issues, "feat", true); len(got) != 2 {
		t.Fatalf("feat タブの件数が違う: %d", len(got))
	}
	// other タブはカテゴリ無し + 独立タブを持たないカテゴリ
	other := Filter(issues, OtherTab, true)
	for _, iss := range other {
		if iss.Category == "feat" {
			t.Fatalf("独立タブを持つカテゴリが other に混ざった: %s", iss.Rel)
		}
	}
	if len(other) != 5 {
		t.Fatalf("other の件数が違う: %d (%v)", len(other), other)
	}
}

func TestTabsAndFilterKeepLiteralOtherCategoryInOneTab(t *testing.T) {
	// カテゴリ語は任意なので、寄せ先と同綴りの "other" が実在しうる。同名タブが 2 つ並ぶと
	// tabIdx の指す先が曖昧になり、Filter がどちらでも同じ判定を通るので片方が必ず空になる。
	// さらに own から除かないと、そのカテゴリの issue がどのタブにも出ない (行が消える)。
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "010-other-misc-one.md", "009-other-misc-two.md",
		"008-feat-a.md", "007-feat-b.md", "006-perf-solo.md")
	list, _ := Scan([]string{dir})

	tabs := Tabs(list, TabMinCount)
	seen := 0
	for _, t := range tabs {
		if t.Name == OtherTab {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("other タブが %d 個ある: %+v", seen, tabs)
	}
	// other = 実カテゴリ other 2 件 + 少数派 perf 1 件
	if i := indexOfTab(tabs, OtherTab); tabs[i].Count != 3 {
		t.Fatalf("other の件数が合算されていない: %+v", tabs)
	}
	got := Filter(list, OtherTab, true)
	if len(got) != 3 {
		t.Fatalf("other タブの行数が件数と合わない: %d (%+v)", len(got), got)
	}
	// 全 issue がどこかのタブに出る (どこにも出ない行を作らない)
	total := 0
	for _, tb := range tabs {
		total += len(Filter(list, tb.Name, true))
	}
	if total != len(list) {
		t.Fatalf("タブの合計 %d が全件 %d と合わない", total, len(list))
	}
}

func TestConflictsIgnoresNonStatusSubgroupDirs(t *testing.T) {
	// 状態でないサブディレクトリ (プロダクト名・時間軸) は別の名前空間。同名ファイルがあっても
	// 二重化ではないので警告しない (誤警告を出すと本物の警告が信じられなくなる)。
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, filepath.Join(dir, "ubipay"), "001-fix-a.md")
	mkFiles(t, filepath.Join(dir, "ubiregi"), "001-fix-a.md")
	if _, warns := Scan([]string{dir}); len(warns) != 0 {
		t.Fatalf("サブグループ間の同名ファイルで警告が出た: %q", warns)
	}
}

func TestConflictsWarnsWhenSubgroupAndStatusDirShareName(t *testing.T) {
	// ⚠️ 回帰防止: サブグループを「状態でないから」と数える前に除くと、プロダクト別
	// ディレクトリで運用している repo の done 移動 (この警告が存在する唯一の理由) を黙らせる。
	// 片方でも状態を持つ配置にあるなら二重化として警告する。
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, filepath.Join(dir, "ubipay"), "001-fix-a.md")
	mkFiles(t, filepath.Join(dir, "done"), "001-fix-a.md")
	_, warns := Scan([]string{dir})
	if len(warns) != 1 {
		t.Fatalf("サブグループと done/ の二重化を警告しない: %q", warns)
	}
	for _, want := range []string{"ubipay/001-fix-a.md", "done/001-fix-a.md"} {
		if !strings.Contains(warns[0], want) {
			t.Fatalf("警告に %q が含まれない: %q", want, warns[0])
		}
	}
}

func TestIdentIncludesUpperIDPrefix(t *testing.T) {
	// 番号だけをコピーすると 005-*.md とも読める曖昧な参照になる (両スキームが同居する repo)
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "UI-005-remove-memo.md", "030-feat-new.md", "resource-leaks.md")
	list, _ := Scan([]string{dir})
	got := make(map[string]string, len(list))
	for _, iss := range list {
		got[filepath.Base(iss.Rel)] = iss.Ident()
	}
	for name, want := range map[string]string{
		"UI-005-remove-memo.md": "UI-005",
		"030-feat-new.md":       "030",
		"resource-leaks.md":     "",
	} {
		if got[name] != want {
			t.Fatalf("%s の Ident が %q (want %q)", name, got[name], want)
		}
	}
	// 数値ソートキーとしての Number は番号だけを保つ
	for _, iss := range list {
		if iss.Prefix != "" && iss.Number != "005" {
			t.Fatalf("upperID の Number が番号だけになっていない: %q", iss.Number)
		}
	}
}

func TestConflictsDetectsSameNameInTwoStatusDirs(t *testing.T) {
	// 並行セッションの git mv + pathspec commit で起きる二重化を検出できること
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "028-refactor-box.md")
	mkFiles(t, filepath.Join(dir, "done"), "028-refactor-box.md")
	_, warns := Scan([]string{dir})
	if len(warns) != 1 {
		t.Fatalf("二重化を検出できていない: %q", warns)
	}
	if !contains(warns[0], "028-refactor-box.md") {
		t.Fatalf("警告に対象ファイル名が入っていない: %q", warns[0])
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestLoadMetaReadsTitleFrontMatterAndCheckboxes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "005-feat-x.md")
	body := "---\nstatus: ongoing\n---\n# 005 feat: タイトル\n\n- [x] 済み\n- [ ] 未\n- [ ] 未\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := newIssue(dir, "005-feat-x.md")
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	if iss.Title != "005 feat: タイトル" {
		t.Fatalf("H1 が取れていない: %q", iss.Title)
	}
	if iss.Declared != "ongoing" {
		t.Fatalf("front matter の status が取れていない: %q", iss.Declared)
	}
	if iss.Checked != 1 || iss.Boxes != 3 || iss.Progress() != "1/3" {
		t.Fatalf("チェックボックスの計数が違う: %d/%d (%q)", iss.Checked, iss.Boxes, iss.Progress())
	}
	// 2 回目は読み直さない (状態が壊れない)
	if err := iss.LoadMeta(); err != nil || iss.Boxes != 3 {
		t.Fatalf("再呼び出しで壊れた: err=%v boxes=%d", err, iss.Boxes)
	}
}

func TestStatusLabelShowsContradictionInsteadOfMerging(t *testing.T) {
	iss := &Issue{Status: StatusDone, Declared: "ongoing"}
	label := iss.StatusLabel()
	if !contains(label, "done") || !contains(label, "ongoing") {
		t.Fatalf("矛盾が両方表示されていない: %q", label)
	}
	// 宣言とパスが一致していれば 1 つだけ
	if got := (&Issue{Status: StatusDone, Declared: "Done"}).StatusLabel(); got != "done" {
		t.Fatalf("一致時に余計な表示が出た: %q", got)
	}
	if got := (&Issue{Status: StatusOpen}).StatusLabel(); got != "open" {
		t.Fatalf("宣言なしの表示が違う: %q", got)
	}
}

func TestDisplayFallsBackToSlug(t *testing.T) {
	iss := newIssue("/d", "028-refactor-box-args.md")
	if got := iss.Display(); got != "box args" {
		t.Fatalf("スラッグ由来の表示が想定と違う: %q", got)
	}
	// H1 が番号で始まるときは番号を落とす (番号は一覧の別の列に出るため)
	iss.Title = "028 refactor: 引数の整理"
	if got := iss.Display(); got != "refactor: 引数の整理" {
		t.Fatalf("H1 の先頭番号が落ちていない: %q", got)
	}
	iss.Title = "引数の整理"
	if got := iss.Display(); got != "引数の整理" {
		t.Fatalf("番号で始まらない H1 を削ってしまった: %q", got)
	}
}

func TestScanExcludesMetaFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "README.md", "INDEX.md", "TEMPLATE.md", "001-feat-a.md")
	mkFiles(t, filepath.Join(dir, "done"), "readme.md", "002-feat-b.md")
	issues, _ := Scan([]string{dir})
	if len(issues) != 2 {
		names := make([]string, 0, len(issues))
		for _, iss := range issues {
			names = append(names, iss.Rel)
		}
		t.Fatalf("付随ファイルを除外できていない: %q", names)
	}
}

func TestProgressEmptyWithoutCheckboxes(t *testing.T) {
	if got := (&Issue{}).Progress(); got != "" {
		t.Fatalf("チェックボックス無しで進捗を出した: %q", got)
	}
}

func TestScanRealRepoIssuesDirIfPresent(t *testing.T) {
	// 実データでの smoke test (この repo の issues/)。内容に依存する断定はしない
	dir := filepath.Join("..", "..", "..", "issues")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("issues/ が無い")
	}
	issues, warns := Scan([]string{dir})
	if len(issues) < 10 {
		t.Fatalf("実 repo の走査結果が少なすぎる: %d 件", len(issues))
	}
	for _, w := range warns {
		t.Logf("警告: %s", w)
	}
	for _, iss := range issues {
		if !isMarkdown(iss.Path) {
			t.Fatalf(".md 以外を拾った: %s", iss.Path)
		}
		if iss.Status == StatusUnknown {
			t.Logf("未知サブディレクトリ: %s (group=%s)", iss.Rel, iss.Group)
		}
	}
	t.Logf("タブ: %+v", Tabs(issues, TabMinCount))
}

func TestNextNumberUsesMaxAcrossAllStatusDirs(t *testing.T) {
	// 状態ディレクトリを見落とすと番号を再利用する (README の採番コマンドの失敗モード)。
	// done/ や未知サブディレクトリに最大番号があっても拾うことを固定する。
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "003-feat-a.md")
	mkFiles(t, filepath.Join(dir, "pending"), "015-feat-b.md")
	mkFiles(t, filepath.Join(dir, "done"), "028-refactor-c.md")
	mkFiles(t, filepath.Join(dir, "mid-long-term"), "031-research-d.md")
	list, _ := Scan([]string{dir})
	if got := NextNumber(list); got != "032" {
		t.Fatalf("次番号が想定と違う: %q (want 032)", got)
	}
	// 番号付きが 1 つも無ければ 001
	if got := NextNumber([]*Issue{{Slug: "no-number"}}); got != "001" {
		t.Fatalf("番号なしのみのときの次番号: %q (want 001)", got)
	}
	// 桁数は観測した最大桁に合わせる
	if got := NextNumber([]*Issue{{Number: "0042"}}); got != "0043" {
		t.Fatalf("4 桁運用の次番号: %q (want 0043)", got)
	}
}

func TestReferenceAndPathFrom(t *testing.T) {
	iss := &Issue{
		Path: "/repo/issues/pending/028-refactor-box.md", Dir: "/repo/issues",
		Rel: "pending/028-refactor-box.md", Number: "028", Category: "refactor", Slug: "box",
		Title: "028 refactor: 引数の整理",
	}
	// 番号が先頭 (rename も move も生き残る唯一安定した参照形式)、パスは repo 相対
	want := "issue 028 refactor: 引数の整理 (issues/pending/028-refactor-box.md)"
	if got := iss.Reference("/repo"); got != want {
		t.Fatalf("参照が想定と違う:\n got  %q\n want %q", got, want)
	}
	// root 不明なら絶対パスのまま
	if got := iss.PathFrom(""); got != iss.Path {
		t.Fatalf("root 空のときのパス: %q", got)
	}
	// 番号なしは "issue " を付けない
	noNum := &Issue{Path: "/repo/issues/x.md", Rel: "x.md", Slug: "resource-leaks"}
	if got := noNum.Reference("/repo"); got != "resource leaks (issues/x.md)" {
		t.Fatalf("番号なしの参照: %q", got)
	}
}
