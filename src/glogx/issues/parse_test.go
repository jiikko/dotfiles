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
	// human は fixture に無いが、0 件でも先頭に席がある (HumanTab の契約)
	tabs := Tabs(issues, TabMinCount)
	if len(tabs) != 3 || tabs[0].Name != HumanTab || tabs[0].Count != 0 {
		t.Fatalf("human タブが先頭に無い: %+v", tabs)
	}
	if tabs[1].Name != "feat" || tabs[1].Count != 2 {
		t.Fatalf("タブの組み立てが想定と違う: %+v", tabs)
	}
	if tabs[2].Name != OtherTab || tabs[2].Count != 5 {
		t.Fatalf("少数派が other に寄っていない: %+v", tabs)
	}
	// minCount=1 なら全カテゴリが独立タブになる (+ human の席)
	if all := Tabs(issues, 1); len(all) != 7 {
		t.Fatalf("minCount=1 でタブ数が想定と違う: %+v", all)
	}
}

func TestFilterByTabAndDone(t *testing.T) {
	issues, _ := Scan([]string{fixture(t)})
	// 既定 (FilterOpen) は open だけ。pending / done は伏せる
	open := Filter(issues, "", FilterOpen)
	for _, iss := range open {
		if iss.Status == StatusDone || iss.Status == StatusPending {
			t.Fatalf("既定で %s が混ざっている: %s", iss.Status, iss.Rel)
		}
	}
	if len(Filter(issues, "", FilterAll)) != len(issues) {
		t.Fatal("FilterAll で全件出ていない")
	}
	// feat タブ (done を含めると 2 件)
	if got := Filter(issues, "feat", FilterAll); len(got) != 2 {
		t.Fatalf("feat タブの件数が違う: %d", len(got))
	}
	// other タブはカテゴリ無し + 独立タブを持たないカテゴリ
	other := Filter(issues, OtherTab, FilterAll)
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
	got := Filter(list, OtherTab, FilterAll)
	if len(got) != 3 {
		t.Fatalf("other タブの行数が件数と合わない: %d (%+v)", len(got), got)
	}
	// 全 issue がどこかのタブに出る (どこにも出ない行を作らない)
	total := 0
	for _, tb := range tabs {
		total += len(Filter(list, tb.Name, FilterAll))
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
	// 🚨 回帰防止: サブグループを「状態でないから」と数える前に除くと、プロダクト別
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

func TestLoadMetaReadsTitleAndFrontMatter(t *testing.T) {
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
	// 2 回目は読み直さない (状態が壊れない)
	if err := iss.LoadMeta(); err != nil || iss.Title != "005 feat: タイトル" {
		t.Fatalf("再呼び出しで壊れた: err=%v title=%q", err, iss.Title)
	}
}

// LoadMeta が H1 で**読むのをやめている**ことを主張する。
//
// なぜ scanner のトークン上限を観測点にするか: 「H1 の後ろに壊れた内容を置いて読まずに
// 済んでいることを見る」だけでは vacuous になる — h1Re はゴミバイトでエラーにならないので、
// EOF まで読んでも外から見える差が出ず、打ち切りを外しても green のままになる。時間で
// 測るのは flaky。LoadMeta は sc.Buffer(64KB, 1MB) を張って sc.Err() を返すので、
// **H1 の後ろに改行なしの 2MB の 1 行**を置けば「EOF まで読む実装は
// bufio.Scanner: token too long を返す / 打ち切る実装はその行に到達しない」で決定論的に分かれる。
func TestLoadMetaStopsAtH1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "099-feat-probe.md")
	// 🚨 上限は loadMetaMaxLine から組む。リテラル (2MB 等) で書くと、上限を上げた変更で
	// このテストが無言で恒真になる (R3 レビューで実証済み)
	huge := strings.Repeat("x", loadMetaMaxLine+1)
	body := "---\nstatus: open\n---\n# 099 feat: probe\n" + huge
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := newIssue(dir, "099-feat-probe.md")
	err := iss.LoadMeta()
	if err != nil {
		t.Fatalf("H1 の先を読んでいる (打ち切りが効いていない): %v", err)
	}
	if iss.Title != "099 feat: probe" {
		t.Fatalf("H1 が取れていない: %q", iss.Title)
	}
	if iss.Declared != "open" {
		t.Fatalf("front matter の status を取り落とした: %q", iss.Declared)
	}
}

// H1 がファイルの深い位置にあっても取れること (打ち切りに行数上限を設けていないこと)。
//
// 🚨 「行数上限は設けない」は issue 050 の明示的な決定。実データに H1 が 239 行目にある
// issue が存在し (別 repo の 559 行のファイル)、上限を入れるとそのタイトルが無言で消える。
// 決定を守るテストが無いと、後から「先頭 N 行だけ読む」最適化で静かに壊れる
// (R3 レビューで上限 5 行の変異が全 green だったため追加)。
func TestLoadMetaFindsH1DeepInFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "097-feat-deep.md")
	var b strings.Builder
	b.WriteString("---\nstatus: open\n---\n")
	for range 300 {
		b.WriteString("前置きの本文行\n")
	}
	b.WriteString("# 097 feat: 深い位置の H1\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := newIssue(dir, "097-feat-deep.md")
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	if iss.Title != "097 feat: 深い位置の H1" {
		t.Fatalf("深い位置の H1 を取れていない (行数上限が入った?): %q", iss.Title)
	}
	if iss.Declared != "open" {
		t.Fatalf("front matter の status を取り落とした: %q", iss.Declared)
	}
}

// H1 が無いファイルは打ち切り条件が成立しないので EOF まで読む (意図した挙動)。
// front matter だけは取れること。
func TestLoadMetaWithoutH1StillReadsFrontMatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "098-feat-noh1.md")
	body := "---\nstatus: pending\n---\n本文だけで H1 が無い\n\n## 見出し 2 は H1 ではない\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	iss := newIssue(dir, "098-feat-noh1.md")
	if err := iss.LoadMeta(); err != nil {
		t.Fatal(err)
	}
	if iss.Title != "" {
		t.Fatalf("H1 が無いのにタイトルが付いた: %q", iss.Title)
	}
	if iss.Declared != "pending" {
		t.Fatalf("front matter の status が取れていない: %q", iss.Declared)
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

// epic/<name>/ の 2 段は open の issue (Group = <name>)。group 内 next は claim として残し、
// done/pending は規約外の迷子として Unknown のまま見せる。epic/ 直下も Unknown。
func TestScanReadsEpicSubdirsAsOpenGroups(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, dir, "001-feat-a.md")
	mkFiles(t, filepath.Join(dir, "epic", "google-drive"), "README.md", "393-feat-gd-phase3.md", "casa-assessment.md")
	mkFiles(t, filepath.Join(dir, "epic", "google-drive", "next"), "394-feat-gd-claim.md")
	mkFiles(t, filepath.Join(dir, "epic", "google-drive", "done"), "395-feat-gd-lost.md")
	mkFiles(t, filepath.Join(dir, "epic", "google-drive", "pending"), "396-feat-gd-pending-lost.md")
	mkFiles(t, filepath.Join(dir, "epic", "cloud"), "700-design-backend.md")
	mkFiles(t, filepath.Join(dir, "epic"), "999-bug-lost.md")
	got, warns := Scan([]string{dir})
	if len(warns) != 0 {
		t.Fatalf("想定外の警告: %q", warns)
	}
	type want struct {
		status Status
		group  string
		kind   GroupKind
		key    string
	}
	wants := map[string]want{
		"001-feat-a.md": {StatusOpen, "", GroupNone, ""},
		"epic/google-drive/393-feat-gd-phase3.md":               {StatusOpen, "google-drive", GroupEpic, filepath.Join(dir, "epic", "google-drive")},
		"epic/google-drive/casa-assessment.md":                  {StatusOpen, "google-drive", GroupEpic, filepath.Join(dir, "epic", "google-drive")},
		"epic/google-drive/next/394-feat-gd-claim.md":           {StatusNext, "google-drive", GroupEpic, filepath.Join(dir, "epic", "google-drive")},
		"epic/google-drive/done/395-feat-gd-lost.md":            {StatusUnknown, filepath.Join("google-drive", "done"), GroupUnknown, filepath.Join(dir, "epic", "google-drive")},
		"epic/google-drive/pending/396-feat-gd-pending-lost.md": {StatusUnknown, filepath.Join("google-drive", "pending"), GroupUnknown, filepath.Join(dir, "epic", "google-drive")},
		"epic/cloud/700-design-backend.md":                      {StatusOpen, "cloud", GroupEpic, filepath.Join(dir, "epic", "cloud")},
		"epic/999-bug-lost.md":                                  {StatusUnknown, "epic", GroupUnknown, ""},
	}
	if len(got) != len(wants) {
		names := make([]string, 0, len(got))
		for _, iss := range got {
			names = append(names, iss.Rel)
		}
		t.Fatalf("件数が違う: got %d want %d (%q)", len(got), len(wants), names)
	}
	for _, iss := range got {
		w, ok := wants[filepath.ToSlash(iss.Rel)]
		if !ok {
			t.Fatalf("想定外の issue: %q", iss.Rel)
		}
		if iss.Status != w.status || iss.Group != w.group || iss.GroupKind != w.kind || iss.GroupKey != w.key {
			t.Errorf("%s: status=%v group=%q kind=%v key=%q, want status=%v group=%q kind=%v key=%q", iss.Rel, iss.Status, iss.Group, iss.GroupKind, iss.GroupKey, w.status, w.group, w.kind, w.key)
		}
	}
}

func TestScanAcceptsSkipDirNameAsEpicGroup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	mkFiles(t, filepath.Join(dir, EpicDirName, "build"), "710-feat-build.md")

	got, _ := Scan([]string{dir})
	if len(got) != 1 || got[0].Rel != filepath.Join(EpicDirName, "build", "710-feat-build.md") {
		t.Fatalf("skipDirs と同名の Epic group が走査されない: %+v", got)
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

// human タブは件数に依らず常に先頭に居る。人間待ちのタスクは件数が少ないうちに minCount で
// other へ沈むと見落とすため (見落とすのは、まさに件数が少ない = 忘れやすい局面)。
func TestTabsPinHumanFirstRegardlessOfCount(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "issues")
	// human は 1 件だけ = TabMinCount(2) 未満。feat は 2 件で独立タブになる
	mkFiles(t, dir, "010-human-verify-thing.md", "009-feat-a.md", "008-feat-b.md", "007-docs-solo.md")
	list, _ := Scan([]string{dir})

	tabs := Tabs(list, TabMinCount)
	if tabs[0].Name != HumanTab || tabs[0].Count != 1 {
		t.Fatalf("1 件の human が other へ沈んだ / 先頭でない: %+v", tabs)
	}
	// other に二重計上されていない (human は count から抜いてから寄せ集めを数える)
	for _, tb := range tabs[1:] {
		if tb.Name == HumanTab {
			t.Fatalf("human タブが 2 つある: %+v", tabs)
		}
		if tb.Name == OtherTab && tb.Count != 1 { // docs 1 件だけが other へ
			t.Fatalf("other の件数に human が混ざっている: %+v", tabs)
		}
	}
	// human タブを選ぶと human の issue だけが出る (other へ落ちていない証跡)
	if got := Filter(list, HumanTab, FilterAll); len(got) != 1 || got[0].Category != HumanTab {
		t.Fatalf("human タブの中身が違う: %+v", got)
	}
	// 0 件でも席は残る
	root2 := t.TempDir()
	dir2 := filepath.Join(root2, "issues")
	mkFiles(t, dir2, "001-feat-a.md", "002-feat-b.md")
	only, _ := Scan([]string{dir2})
	if t2 := Tabs(only, TabMinCount); t2[0].Name != HumanTab || t2[0].Count != 0 {
		t.Fatalf("human 0 件で席が消えた: %+v", t2)
	}
}

// 段階の名前は往復すること (issue 115)。
//
// 🚨 名前は保存形式そのもの。ParseStatusFilter が引けない段階は ok=false で既定 (open) へ落ち、
// 「開き直したら伏せていたはずの段階に戻っている」という、原因が保存形式だと気づけない形で出る。
func TestStatusFilterNameRoundTrip(t *testing.T) {
	// 名前そのものを literal で固定する (production の String() から作ると自己言及になる)
	for _, tc := range []struct {
		name string
		want StatusFilter
	}{
		{"open", FilterOpen},
		{"pending", FilterPending},
		{"all", FilterAll},
	} {
		got, ok := ParseStatusFilter(tc.name)
		if !ok || got != tc.want {
			t.Errorf("ParseStatusFilter(%q) = (%v, %v) (期待 (%v, true))", tc.name, got, ok, tc.want)
		}
		if s := tc.want.String(); s != tc.name {
			t.Errorf("%v.String() = %q (期待 %q)", tc.want, s, tc.name)
		}
	}
}

// 🚨 **全段階**が引けること。段階を増やしたとき、String() 側は default 無しの switch なので
// exhaustive linter が強制するが、ParseStatusFilter 側は強制されない。ここで範囲を走ることで
// 「String() には足したが引く側に足し忘れた」を検出する (issue 115 の本題)。
func TestStatusFilterEveryStageIsParseable(t *testing.T) {
	n := 0
	for f := FilterOpen; f <= FilterAll; f++ {
		got, ok := ParseStatusFilter(f.String())
		if !ok {
			t.Errorf("段階 %d (%q) を引けない (String() には在るが引く側に無い)", f, f.String())
			continue
		}
		if got != f {
			t.Errorf("段階 %d (%q) が %d として引かれた", f, f.String(), got)
		}
		n++
	}
	// 走査対象 0 件を緑にしない (範囲の書き方が壊れたら赤にする)
	if n < 3 {
		t.Fatalf("段階を %d 件しか走査していない (範囲が壊れている)", n)
	}
}

// 未知の名前は既定 (open) + ok=false へ倒す (見えすぎるより見えなさすぎる方を選ぶ契約)。
func TestStatusFilterUnknownNameFallsBackToOpen(t *testing.T) {
	got, ok := ParseStatusFilter("no-such-stage")
	if ok {
		t.Error("未知の名前を受理した")
	}
	if got != FilterOpen {
		t.Errorf("未知の名前が %v へ落ちた (期待 FilterOpen)", got)
	}
}
