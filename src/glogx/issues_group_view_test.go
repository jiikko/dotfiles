package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/issues"
)

func fakeEpicIssue(dir, group, number, slug string, status issues.Status) *issues.Issue {
	base := fmt.Sprintf("%s-feat-%s.md", number, slug)
	rel := filepath.Join(issues.EpicDirName, group, base)
	// 状態の正本はパスなので、fake も本物と同じ置き場所を作る (next / done / pending)。
	// 直下に置いたまま Status だけ差し替えると、production では存在しない組み合わせを
	// テストが前提にしてしまう
	if sub := epicStatusDir(status); sub != "" {
		rel = filepath.Join(issues.EpicDirName, group, sub, base)
	}
	groupKey := filepath.Join(dir, issues.EpicDirName, group)
	return &issues.Issue{
		Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: number,
		Category: "feat", Slug: slug, Status: status, Group: group,
		GroupKind: issues.GroupEpic, GroupKey: groupKey,
	}
}

// epicStatusDir は epic group 内で status に対応する状態ディレクトリ名 (open は直下なので "")。
// 名前は issues.EpicChildStatus を逆引きして得る (テスト側に第 2 の列挙を作らない)。
func epicStatusDir(status issues.Status) string {
	for _, name := range []string{issues.NextDirName, "done", "pending"} {
		if got, ok := issues.EpicChildStatus(name); ok && got == status {
			return name
		}
	}
	return ""
}

func TestIssuesViewGroupsUseSeparateDisplayRowsAndToggle(t *testing.T) {
	global := fakeIssue("711", "feat", "global", issues.StatusOpen)
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	beta := fakeEpicIssue("/repo/issues", "beta", "710", "beta", issues.StatusOpen)
	betaChild := fakeEpicIssue("/repo/issues", "beta", "700", "beta-child", issues.StatusOpen)
	v := loadedView(betaChild, global, beta, alpha)

	if len(v.rows) != 4 {
		t.Fatalf("子 issue 数が変わった: %d", len(v.rows))
	}
	if len(v.displayRows) != 3 {
		t.Fatalf("既定で group が折り畳まれていない: displayRows=%d", len(v.displayRows))
	}
	if v.displayRows[0].kind != displayRowIssue || v.displayRows[0].issue != global {
		t.Fatalf("単独 issue の表示順が壊れた: %+v", v.displayRows[0])
	}
	if v.displayRows[1].kind != displayRowGroup || v.displayRows[1].groupName != "alpha" ||
		v.displayRows[2].groupName != "beta" {
		t.Fatalf("最大番号同値の group が GroupKey 順でない: %+v", v.displayRows)
	}
	out := strings.Join(v.listLines(renderOpts(10)), "\n")
	if !strings.Contains(out, "▸ alpha (1)") || !strings.Contains(out, "▸ beta (2)") ||
		strings.Contains(out, "beta-child") {
		t.Fatalf("折り畳み表示が違う:\n%s", out)
	}

	v.handleKey("j", vp(10)) // alpha 親行
	if v.current() != nil || !v.currentIsGroup() {
		t.Fatalf("親行が issue として扱われた: current=%+v", v.current())
	}
	v.handleKey("enter", vp(10))
	if !v.expandedGroups[alpha.GroupKey] || len(v.displayRows) != 4 {
		t.Fatalf("Enter で group が展開されない: expanded=%v rows=%d", v.expandedGroups, len(v.displayRows))
	}
	if v.displayRows[2].kind != displayRowIssue || v.displayRows[2].issue != alpha {
		t.Fatalf("展開後に子 issue が親の直下にない: %+v", v.displayRows)
	}
	v.handleKey(" ", vp(10))
	if v.expandedGroups[alpha.GroupKey] || len(v.displayRows) != 3 {
		t.Fatalf("Space で group が折り畳まれない: expanded=%v rows=%d", v.expandedGroups, len(v.displayRows))
	}
}

func TestIssuesViewGroupParentActionsAreNoopWithNotice(t *testing.T) {
	group := fakeEpicIssue("/repo/issues", "cloud", "710", "drive", issues.StatusOpen)
	v := loadedView(group)
	if !v.currentIsGroup() {
		t.Fatal("前提: 初期カーソルが group 親行にない")
	}
	for _, key := range []string{"shift+down", "y", "p", "Y", "N", "o", "n"} {
		before := v.cursor
		v.handleKey(key, vp(10))
		if v.cursor != before || v.open != nil || v.markNext.active {
			t.Fatalf("親行で %q が issue 操作として効いた: cursor=%d open=%v mark=%v", key, v.cursor, v.open != nil, v.markNext.active)
		}
		if v.notice == "" {
			t.Fatalf("親行で %q が no-op notice を出していない", key)
		}
		v.notice = ""
	}
}

func TestIssuesViewNumberFilterAutoExpandsAndReanchorsChild(t *testing.T) {
	first := fakeEpicIssue("/repo/issues", "cloud", "415", "first", issues.StatusOpen)
	second := fakeEpicIssue("/repo/issues", "cloud", "141", "second", issues.StatusOpen)
	other := fakeEpicIssue("/repo/issues", "other", "050", "other", issues.StatusOpen)
	v := loadedView(first, second, other)
	groupKey := first.GroupKey
	v.handleKey("/", vp(10))
	v.handleKey("4", vp(10))
	v.handleKey("1", vp(10))
	if !v.autoExpandedGroups[groupKey] || v.expandedGroups[groupKey] {
		t.Fatalf("番号 filter の group 展開状態が分離されていない: manual=%v auto=%v", v.expandedGroups[groupKey], v.autoExpandedGroups[groupKey])
	}
	v.handleKey("enter", vp(10))
	v.handleKey("down", vp(10)) // auto 展開された親の下の 415
	want := first.Path
	if v.current() == nil || v.current().Path != want {
		t.Fatalf("filter 中の子 issue へ移れない: %+v", v.current())
	}
	v.handleKey("esc", vp(10))
	if len(v.autoExpandedGroups) != 0 {
		t.Fatalf("解除後も autoExpanded が残った: %v", v.autoExpandedGroups)
	}
	if v.current() == nil || v.current().Path != want {
		t.Fatalf("解除後に子 issue の path へ再アンカーされない: %+v display=%+v", v.current(), v.displayRows)
	}
}

func TestIssuesViewClearingNumberFilterDropsStaleCollapsedGroups(t *testing.T) {
	child := fakeEpicIssue("/repo/issues", "cloud", "415", "first", issues.StatusOpen)
	v := loadedView(child)

	v.handleKey("/", vp(10))
	v.handleKey("4", vp(10))
	v.handleKey("1", vp(10))
	v.handleKey("5", vp(10))
	v.handleKey("enter", vp(10))
	v.handleKey(" ", vp(10)) // 自動展開された group を手動で折り畳む
	if !v.collapsedGroups[child.GroupKey] {
		t.Fatal("前提: 番号 filter 中の group を折り畳めていない")
	}

	v.handleKey("/", vp(10))
	v.handleKey("ctrl+u", vp(10))
	v.handleKey("9", vp(10))
	v.handleKey("9", vp(10))
	v.handleKey("9", vp(10)) // group A を不一致にして autoExpandedGroups から外す
	v.handleKey("esc", vp(10))

	v.handleKey("/", vp(10))
	v.handleKey("4", vp(10))
	v.handleKey("1", vp(10))
	v.handleKey("5", vp(10))
	if !v.groupExpanded(child.GroupKey) || len(v.displayRows) != 2 {
		t.Fatalf("検索語を戻しても group が再展開されない: expanded=%v collapsed=%v rows=%+v",
			v.groupExpanded(child.GroupKey), v.collapsedGroups, v.displayRows)
	}
}

func TestIssuesViewRescanReanchorsGroupParentByGroupKey(t *testing.T) {
	child := fakeEpicIssue("/repo/issues", "cloud", "415", "first", issues.StatusOpen)
	v := loadedView(child)
	if !v.currentIsGroup() {
		t.Fatal("前提: 初期カーソルが group 親行にない")
	}

	newer := fakeIssue("999", "feat", "new", issues.StatusOpen)
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: []*issues.Issue{newer, child}})
	row, ok := v.currentDisplayRow()
	if !ok || row.kind != displayRowGroup || row.groupKey != child.GroupKey {
		t.Fatalf("再スキャン後に親行へ戻らない: row=%+v cursor=%d display=%+v", row, v.cursor, v.displayRows)
	}
}

func TestIssuesViewAutoExpandedGroupStillToggles(t *testing.T) {
	child := fakeEpicIssue("/repo/issues", "cloud", "415", "first", issues.StatusOpen)
	v := loadedView(child)
	v.handleKey("/", vp(10))
	v.handleKey("4", vp(10))
	v.handleKey("1", vp(10))
	if !v.autoExpandedGroups[child.GroupKey] || !v.groupExpanded(child.GroupKey) {
		t.Fatal("番号 filter で group が自動展開されていない")
	}
	v.handleKey("enter", vp(10)) // 入力を確定し、親行の toggle を通常モードで試す
	v.handleKey(" ", vp(10))
	if v.groupExpanded(child.GroupKey) || len(v.displayRows) != 1 || !v.currentIsGroup() {
		t.Fatalf("auto 展開中の Space が折り畳み toggle になっていない: expanded=%v rows=%d current=%+v",
			v.groupExpanded(child.GroupKey), len(v.displayRows), v.current())
	}
	v.handleKey("enter", vp(10))
	if !v.groupExpanded(child.GroupKey) || len(v.displayRows) != 2 {
		t.Fatalf("auto 展開中の Enter が再展開 toggle になっていない: expanded=%v rows=%d",
			v.groupExpanded(child.GroupKey), len(v.displayRows))
	}
}

func TestIssuesViewTabCountsChildrenNotGroupParents(t *testing.T) {
	v := loadedView(
		fakeEpicIssue("/repo/issues", "cloud", "710", "a", issues.StatusOpen),
		fakeEpicIssue("/repo/issues", "cloud", "709", "b", issues.StatusOpen),
		fakeIssue("708", "feat", "plain", issues.StatusOpen),
	)
	if v.allCount != 3 || len(v.rows) != 3 {
		t.Fatalf("件数が親行込みになった: all=%d rows=%d", v.allCount, len(v.rows))
	}
	if len(v.displayRows) != 2 {
		t.Fatalf("折り畳み後の表示行数が違う: %d", len(v.displayRows))
	}
	if !strings.Contains(v.tabLine(issuesRenderOpts{width: 120}), "[All 3]") {
		t.Fatalf("タブバッジが子件数でない: %q", v.tabLine(issuesRenderOpts{width: 120}))
	}
}

func TestIssuesViewMoveReanchorsCursorAndMarkToReturnedPaths(t *testing.T) {
	dir := t.TempDir()
	groupDir := filepath.Join(dir, issues.EpicDirName, "cloud")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := []*issues.Issue{
		fakeEpicIssue(dir, "cloud", "002", "a", issues.StatusOpen),
		fakeEpicIssue(dir, "cloud", "001", "b", issues.StatusOpen),
	}
	for _, iss := range old {
		if err := os.WriteFile(iss.Path, []byte("# "+iss.Number+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := loadedView(old...)
	v.cwd = dir
	v.handleKey("enter", vp(10)) // group を展開
	v.handleKey("j", vp(10))     // 002
	v.handleKey("J", vp(10))     // 002..001 を選択
	if len(v.selectedRows()) != 2 {
		t.Fatal("前提: 2 件の選択が作れていない")
	}
	v.handleKey("n", vp(10))
	cmd := v.handleKey("y", vp(10))
	if cmd == nil || v.pendingCursorPath == "" || v.pendingMarkPath == "" {
		t.Fatalf("移動後の再アンカーが予約されていない: cmd=%v cursor=%q mark=%q", cmd != nil, v.pendingCursorPath, v.pendingMarkPath)
	}

	// claim は symlink の目印なので Path は変わらない (issue 263)。再走査の結果は同じ Path に
	// Status=Next / NextLink が付いた形になる
	fresh := make([]*issues.Issue, 0, len(old))
	for _, iss := range old {
		base := filepath.Base(iss.Path)
		fresh = append(fresh, &issues.Issue{
			Path: iss.Path, Dir: dir, Rel: iss.Rel,
			Number: iss.Number, Category: iss.Category, Slug: iss.Slug, Status: issues.StatusNext,
			Group: "cloud", GroupKind: issues.GroupEpic, GroupKey: groupDir,
			NextLink: filepath.Join(groupDir, issues.NextDirName, base),
		})
	}
	v.receive(issuesScanMsg{dirs: []string{dir}, issues: fresh})
	if v.current() == nil || v.current().Path != filepath.Join(groupDir, "001-feat-b.md") {
		t.Fatalf("cursor が move の戻り値へ再アンカーされない: %+v", v.current())
	}
	if lo, hi, ok := v.selection(); !ok || hi-lo != 1 {
		t.Fatalf("mark が move 後に再アンカーされない: lo=%d hi=%d ok=%v", lo, hi, ok)
	}
}

func TestIssuesViewUserCursorActionsCancelPendingMoveAnchors(t *testing.T) {
	type action struct {
		name string
		run  func(*issuesView)
	}
	actions := []action{
		{name: "moveCursor", run: func(v *issuesView) { v.moveCursor(1, 10) }},
		{name: "setCursor", run: func(v *issuesView) { v.setCursor(1, 10) }},
		{name: "g", run: func(v *issuesView) { v.handleKey("g", vp(10)) }},
		{name: "G", run: func(v *issuesView) { v.handleKey("G", vp(10)) }},
		{name: "tab", run: func(v *issuesView) { v.handleKey("tab", vp(10)) }},
		{name: "anchorCursor", run: func(v *issuesView) { v.anchorCursor(v.rows[0].Path) }},
	}
	for _, tc := range actions {
		t.Run(tc.name, func(t *testing.T) {
			v := loadedView(sampleIssues()...)
			v.pendingCursorPath, v.pendingMarkPath = "/moved/cursor", "/moved/mark"
			tc.run(v)
			if v.pendingCursorPath != "" || v.pendingMarkPath != "" {
				t.Fatalf("%s 後も pending anchor が残る: cursor=%q mark=%q", tc.name, v.pendingCursorPath, v.pendingMarkPath)
			}
		})
	}

	t.Run("anchorGroup", func(t *testing.T) {
		child := fakeEpicIssue("/repo/issues", "cloud", "415", "first", issues.StatusOpen)
		v := loadedView(child)
		v.pendingCursorPath, v.pendingMarkPath = "/moved/cursor", "/moved/mark"
		v.anchorGroup(child.GroupKey)
		if v.pendingCursorPath != "" || v.pendingMarkPath != "" {
			t.Fatalf("anchorGroup 後も pending anchor が残る: cursor=%q mark=%q", v.pendingCursorPath, v.pendingMarkPath)
		}
	})
}

func TestIssuesViewPendingMoveAnchorsExpireAfterFreshMissingScan(t *testing.T) {
	// 🚨 done/ の issue を使う: 直下の issue の claim は symlink の目印で Path が変わらず (issue 263)、
	// 「移動先が消える」状況が作れない。done/ からの claim は従来どおり rename なので、宛先の消失を再現できる
	iss := realDoneIssue(t)
	v := loadedView(iss)
	v.filter = issues.FilterAll // done は既定の一覧に出ないので見せる
	v.refresh()
	v.cwd = iss.Dir
	if v.scanCmd(v.cwd) == nil {
		t.Fatal("前提: stale scan を飛行させられない")
	}
	v.handleKey("n", vp(10))
	if v.handleKey("y", vp(10)) != nil || v.pendingCursorPath == "" {
		t.Fatal("前提: move 後の pending cursor anchor がない")
	}
	if err := os.Remove(filepath.Join(iss.Dir, issues.NextDirName, filepath.Base(iss.Rel))); err != nil {
		t.Fatal(err)
	}

	stale := issuesScanMsg{dirs: []string{iss.Dir}, issues: []*issues.Issue{iss}}
	if cmd := v.receive(stale); cmd == nil {
		t.Fatal("stale receive 後の authoritative scan が予約されない")
	}
	if v.pendingCursorPath == "" {
		t.Fatal("stale receive だけで pending anchor を消費した")
	}
	v.receive(stale)
	if v.pendingCursorPath != "" || v.pendingMarkPath != "" {
		t.Fatalf("宛先が消えた fresh receive 後も pending が残る: cursor=%q mark=%q",
			v.pendingCursorPath, v.pendingMarkPath)
	}
}

// round 2 の敵対レビュー: 親行のカーソルは state に GroupKey で残り、復元で親行へ戻る。
func TestIssuesScreenRoundTripsGroupCursor(t *testing.T) {
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	global := fakeIssue("711", "feat", "global", issues.StatusOpen)
	v := loadedView(global, alpha)
	v.shown, v.root = true, "/repo" // screen は開いている viewer だけを覚える
	v.handleKey("j", vp(10))        // alpha の親行
	if !v.currentIsGroup() {
		t.Fatal("前提: 親行にカーソルがない")
	}
	s, ok := v.screen(time.Now())
	if !ok || s.CursorGroup != alpha.GroupKey || s.Cursor != "" {
		t.Fatalf("親行のカーソルが state に残らない: ok=%v %+v", ok, s)
	}
	w := loadedView(global, alpha)
	w.applyScreen(s)
	if !w.currentIsGroup() || w.currentGroupKey() != alpha.GroupKey {
		t.Fatalf("復元で親行に戻らない: cursor=%d group=%q", w.cursor, w.currentGroupKey())
	}
}

// 走査結果に無い GroupKey (rename / 削除された group) は復元時に落とす。同名 group を作り直した
// とき、死にキーが「畳んだつもり」を無視して展開する事故を防ぐ。
func TestIssuesViewApplyScreenPrunesDeadGroupKeys(t *testing.T) {
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	v := loadedView(alpha)
	v.applyScreen(issuesScreen{Groups: map[string]bool{alpha.GroupKey: true, "/repo/issues/epic/gone": true}})
	if !v.expandedGroups[alpha.GroupKey] {
		t.Fatal("生きている GroupKey まで落とした")
	}
	if v.expandedGroups["/repo/issues/epic/gone"] {
		t.Fatalf("死にキーが残っている: %v", v.expandedGroups)
	}
}

// move の再アンカー予約は、カーソルを動かさない操作 (a の filter 切替 / Esc の番号 filter 解除)
// では保持する。捨てるのは j/k/g/G/tab 等のカーソル移動だけ。
func TestIssuesViewPendingMoveAnchorSurvivesNonCursorKeys(t *testing.T) {
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	global := fakeIssue("711", "feat", "global", issues.StatusOpen)
	v := loadedView(global, alpha)
	v.pendingCursorPath = "/repo/issues/epic/alpha/next/710-feat-alpha.md"
	v.handleKey("a", vp(10))
	if v.pendingCursorPath == "" {
		t.Fatal("a (filter 切替) で move の再アンカー予約が消えた")
	}
	v.handleKey("/", vp(10))
	v.handleKey("7", vp(10))
	v.handleKey("esc", vp(10))
	if v.pendingCursorPath == "" {
		t.Fatal("番号 filter の解除で move の再アンカー予約が消えた")
	}
	v.handleKey("j", vp(10))
	if v.pendingCursorPath != "" {
		t.Fatal("j (カーソル移動) で move の再アンカー予約が捨てられない")
	}
}

// 開いたまま group を消して同名で作り直しても、死にキーで勝手に展開しない (再スキャン経路でも prune する)。
func TestIssuesViewReceivePrunesDeadGroupKeys(t *testing.T) {
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	beta := fakeEpicIssue("/repo/issues", "beta", "709", "beta", issues.StatusOpen)
	v := loadedView(alpha, beta)
	v.handleKey("enter", vp(10)) // alpha を展開
	if !v.expandedGroups[alpha.GroupKey] {
		t.Fatal("前提: alpha が展開されていない")
	}
	v.receive(issuesScanMsg{dirs: []string{"/repo/issues"}, issues: []*issues.Issue{beta}}) // alpha が消えた
	if v.expandedGroups[alpha.GroupKey] {
		t.Fatalf("消えた group の展開キーが残っている: %v", v.expandedGroups)
	}
}

// 親行で番号 filter を Esc したときは親行へ戻る (filter 中の添字を持ち越さない)。
func TestIssuesViewClearNumberFilterOnGroupRowReanchorsGroup(t *testing.T) {
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	g1 := fakeIssue("712", "feat", "a", issues.StatusOpen)
	g2 := fakeIssue("711", "feat", "b", issues.StatusOpen)
	v := loadedView(g1, g2, alpha)
	v.handleKey("/", vp(10))
	v.handleKey("7", vp(10))
	v.handleKey("1", vp(10))
	v.handleKey("0", vp(10)) // alpha の子だけがヒット → 親行 + 子行
	v.handleKey("up", vp(10))
	v.handleKey("up", vp(10)) // 先頭 = alpha の親行
	if !v.currentIsGroup() {
		t.Fatalf("前提: filter 中の先頭が親行でない: %+v", v.displayRows)
	}
	v.handleKey("esc", vp(10))
	if !v.currentIsGroup() || v.currentGroupKey() != alpha.GroupKey {
		t.Fatalf("Esc 後に親行へ戻らない: cursor=%d rows=%+v", v.cursor, v.displayRows)
	}
}

// realDoneIssue は done/ に実ファイルを持つ issue (claim が rename になる側の配置)。
func realDoneIssue(t *testing.T) *issues.Issue {
	t.Helper()
	dir := t.TempDir()
	rel := filepath.Join("done", "001-feat-real.md")
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# 001 feat: real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &issues.Issue{Path: path, Dir: dir, Rel: rel, Number: "001", Category: "feat", Status: issues.StatusDone}
}

// group を展開したとき、子 issue の行は親行より右 (groupChildIndent ぶん) に寄り、単独 issue と
// 同じ桁に並ばない。親と同じ桁だと「開いたのに所属が読めない」(2026-09-05 のユーザー要望)。
func TestIssuesViewGroupChildrenAreIndentedUnderParent(t *testing.T) {
	global := fakeIssue("711", "feat", "global", issues.StatusOpen)
	alpha := fakeEpicIssue("/repo/issues", "alpha", "710", "alpha", issues.StatusOpen)
	v := loadedView(global, alpha)
	v.handleKey("j", vp(10)) // alpha 親行
	v.handleKey("enter", vp(10))
	v.handleKey("k", vp(10)) // カーソルを global へ戻し、子行を非カーソル行として描く
	lines := v.listLines(renderOpts(10))
	var globalLine, childLine string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "711"):
			globalLine = l
		case strings.Contains(l, "710"):
			childLine = l
		}
	}
	if globalLine == "" || childLine == "" {
		t.Fatalf("行が見つからない:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(childLine, cursorGutterBlank+groupChildIndent+"710") {
		t.Fatalf("子 issue が親の下でインデントされていない: %q", childLine)
	}
	if strings.HasPrefix(globalLine, cursorGutterMark+"711") == false {
		t.Fatalf("単独 issue の桁が動いた (インデントは group の子だけ): %q", globalLine)
	}
}

// TestIssuesViewGroupParentIssueMergesIntoHeaderRow は group 名と同じ番号の issue を親行へ
// 統合する挙動を固定する (合成の親行 + 同じ番号の子行、の 2 行に割れていたのを 1 行にする)。
func TestIssuesViewGroupParentIssueMergesIntoHeaderRow(t *testing.T) {
	dir := "/repo/issues"
	parent := fakeEpicIssue(dir, "467", "467", "asset-library", issues.StatusOpen)
	child := fakeEpicIssue(dir, "467", "460", "ui-design", issues.StatusOpen)
	v := loadedView(parent, child)

	if len(v.displayRows) != 1 {
		t.Fatalf("親 issue が親行へ統合されていない: %+v", v.displayRows)
	}
	head := v.displayRows[0]
	if head.kind != displayRowIssue || head.issue != parent || !head.groupHead ||
		head.groupKey != parent.GroupKey || head.childCount != 1 {
		t.Fatalf("親行が統合された issue 行になっていない: %+v", head)
	}
	out := strings.Join(v.listLines(renderOpts(10)), "\n")
	if !strings.Contains(out, "▸ (1) 467") || strings.Contains(out, "▸ 467 (") {
		t.Fatalf("親行の描画が違う:\n%s", out)
	}

	// 統合行では issue 操作が生きている (合成の親行では弾かれていたもの)
	if v.currentIsGroup() || v.current() != parent {
		t.Fatalf("統合行が issue として扱われない: current=%+v", v.current())
	}
	// Enter / Space はどちらも子 issue の展開 toggle (本文は o で開く)
	v.handleKey("enter", vp(10))
	if !v.expandedGroups[parent.GroupKey] || len(v.displayRows) != 2 {
		t.Fatalf("Enter で子リストが開かない: expanded=%v rows=%d", v.expandedGroups, len(v.displayRows))
	}
	if v.displayRows[1].issue != child || !v.displayRows[1].inGroup {
		t.Fatalf("子行が親の下に並ばない: %+v", v.displayRows[1])
	}
	if v.open != nil {
		t.Fatalf("Enter が本文を開いた (子リストの展開が優先されるべき)")
	}
	v.handleKey(" ", vp(10)) // Space でも同じ toggle
	if v.expandedGroups[parent.GroupKey] || len(v.displayRows) != 1 {
		t.Fatalf("Space で畳めない: expanded=%v rows=%d", v.expandedGroups, len(v.displayRows))
	}
}

// TestIssuesViewGroupParentShowsDoneCount は親行の括弧が「子の件数 + done の件数」を出すことと、
// done な子が既定 (状態フィルタ open) でも group の中に見えることを固定する (issue 291)。
func TestIssuesViewGroupParentShowsDoneCount(t *testing.T) {
	dir := "/repo/issues"
	open1 := fakeEpicIssue(dir, "alpha", "710", "open-one", issues.StatusOpen)
	next1 := fakeEpicIssue(dir, "alpha", "709", "claimed", issues.StatusNext)
	held := fakeEpicIssue(dir, "alpha", "708", "held", issues.StatusPending)
	done1 := fakeEpicIssue(dir, "alpha", "707", "done-one", issues.StatusDone)
	done2 := fakeEpicIssue(dir, "alpha", "706", "done-two", issues.StatusDone)
	fresh := fakeEpicIssue(dir, "beta", "705", "no-progress", issues.StatusOpen)
	globalDone := fakeIssue("711", "feat", "global-done", issues.StatusDone)
	v := loadedView(open1, next1, held, done1, done2, fresh, globalDone)

	if v.filter != issues.FilterOpen {
		t.Fatalf("既定の状態フィルタが open でない: %v", v.filter)
	}
	out := strings.Join(v.listLines(renderOpts(20)), "\n")
	if !strings.Contains(out, "▸ alpha (5 ✓2)") {
		t.Fatalf("親行に子の件数と done の件数が出ていない:\n%s", out)
	}
	if !strings.Contains(out, "▸ beta (1)") {
		t.Fatalf("done が 0 件の group で従来の (N) 表示が壊れた:\n%s", out)
	}
	if strings.Contains(out, "global-done") {
		t.Fatalf("global の done が既定で見えている (epic だけの例外のはず):\n%s", out)
	}

	if !v.currentIsGroup() {
		t.Fatalf("先頭行が alpha の親行でない: %+v", v.displayRows[0])
	}
	v.handleKey("enter", vp(20)) // alpha を展開
	out = strings.Join(v.listLines(renderOpts(20)), "\n")
	// 番号 + バッジで見る (タイトルはスラッグを整形するので、状態が読めない)
	for _, want := range []string{"707 ✓", "706 ✓", "708 ⏸", "709 ▶", "710 ○"} {
		if !strings.Contains(out, want) {
			t.Fatalf("epic を展開しても %q が既定で見えない:\n%s", want, out)
		}
	}
}

// TestIssuesViewGroupHeadRowShowsDoneCount は group 名と同じ番号の issue を親行へ統合した行でも
// 進捗が出ることを固定する (統合行は groupLine ではなく rowLine が描くので経路が別)。
func TestIssuesViewGroupHeadRowShowsDoneCount(t *testing.T) {
	dir := "/repo/issues"
	parent := fakeEpicIssue(dir, "467", "467", "asset-library", issues.StatusOpen)
	child := fakeEpicIssue(dir, "467", "460", "ui-design", issues.StatusOpen)
	doneChild := fakeEpicIssue(dir, "467", "459", "spike", issues.StatusDone)
	v := loadedView(parent, child, doneChild)

	out := strings.Join(v.listLines(renderOpts(20)), "\n")
	if !strings.Contains(out, "(2 ✓1)") {
		t.Fatalf("統合した親行に進捗が出ていない:\n%s", out)
	}
}

// strayIn は group 内の予約外ディレクトリ (`epic/<name>/closed/`) に居る迷子。
// GroupKind は Unknown だが GroupKey は持つ (移動の宛先はこれで決まる)。
func strayIn(dir, group, number string) *issues.Issue {
	base := number + "-feat-stray.md"
	rel := filepath.Join(issues.EpicDirName, group, "closed", base)
	return &issues.Issue{
		Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: number,
		Category: "feat", Slug: "stray", Status: issues.StatusUnknown,
		Group: filepath.Join(group, "closed"), GroupKind: issues.GroupUnknown,
		GroupKey: filepath.Join(dir, issues.EpicDirName, group),
	}
}

// TestIssuesViewAnchorOpensCollapsedGroupForMovedIssue は、移動で group の中へ入った issue に
// カーソルを置き直すとき、畳んだ親行を開いて対象を見せることを固定する
// (2026-09-06 の敵対的レビュー 2 周目: 「移動しました」と言った直後に対象が画面から消えていた)。
//
// 🚨 並び順を 2 通り回す。「最初に見つけた GroupKey の group を開く」誤実装は、無関係な group が
// 対象より後ろに居ると正解と同じ group に当たって素通りする。production の並びは番号降順
// (issues.sortIssues) なので、片方の並びだけで書くと守れているつもりで守れていない (4 周目)。
//
// 🚨 その変異を当てると **「無関係な group が先」のケースだけが red** になる (もう一方は
// 構造上その誤実装を検出できない)。ケースを減らさないこと — 減らすと検出力が 0 になる。
func TestIssuesViewAnchorOpensCollapsedGroupForMovedIssue(t *testing.T) {
	dir := "/repo/issues"
	for _, tc := range []struct {
		name   string
		before bool // 無関係な group を対象より前に置くか
	}{
		{"無関係な group が先", true},
		{"無関係な group が後 (番号降順 = production の並び)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			global := fakeIssue("900", "feat", "global", issues.StatusOpen)
			child := fakeEpicIssue(dir, "cloud", "702", "moved-in", issues.StatusNext)
			other := fakeEpicIssue(dir, "alpha", "701", "unrelated", issues.StatusOpen)
			list := []*issues.Issue{global, other, child}
			if !tc.before {
				list = []*issues.Issue{global, child, other}
			}
			v := loadedView(list...)
			if v.groupExpanded(child.GroupKey) {
				t.Fatalf("前提が違う: group が既に展開されている")
			}

			v.anchorCursorInternal(child.Path)

			if !v.groupExpanded(child.GroupKey) {
				t.Fatalf("畳んだ group が開かれていない: %+v", v.expandedGroups)
			}
			if v.groupExpanded(other.GroupKey) {
				t.Fatalf("無関係な group まで開いた: %+v", v.expandedGroups)
			}
			row, ok := v.currentDisplayRow()
			if !ok || row.kind != displayRowIssue || row.issue != child {
				t.Fatalf("カーソルが移動先の issue にない: ok=%v row=%+v", ok, row)
			}
		})
	}
}

// TestIssuesViewAnchorIgnoresIssuesOutsideVisibleRows は、今の一覧に居ない issue のために
// group を開かないことを固定する。走査先を v.rows から v.all へ広げると、Next タブで目印を
// 外した直後 (対象が可視集合から外れる) に「見えない issue のために group が開き、その展開が
// screen に保存されて再起動を跨ぐ」形になる (2026-09-06 の敵対的レビュー 4 周目の変異 M7)。
func TestIssuesViewAnchorIgnoresIssuesOutsideVisibleRows(t *testing.T) {
	dir := "/repo/issues"
	child := fakeEpicIssue(dir, "cloud", "702", "done-child", issues.StatusDone)
	v := loadedView(fakeIssue("900", "feat", "global", issues.StatusOpen), child)
	v.setRows(nil) // 可視集合から外れた状態 (タブ切り替え・フィルタで起きる)
	v.rebuildDisplayRows()

	v.anchorCursorInternal(child.Path)

	if v.groupExpanded(child.GroupKey) {
		t.Fatalf("一覧に居ない issue のために group を開いた: %+v", v.expandedGroups)
	}
}

// TestIssuesViewAnchorOverridesExplicitCollapse は、明示的に畳んだ (collapsedGroups) group でも
// 移動先を見せるために開くこと、そしてその展開が **再スキャンを跨いで残る** ことを固定する。
// 展開先を autoExpandedGroups にすると refresh のたびに捨てられ、次の再スキャンで畳み直される
// (2026-09-06 の敵対的レビュー 3 周目 P3-3 / 4 周目 P3-4 の変異 M2)。
func TestIssuesViewAnchorOverridesExplicitCollapse(t *testing.T) {
	dir := "/repo/issues"
	child := fakeEpicIssue(dir, "cloud", "702", "moved-in", issues.StatusNext)
	v := loadedView(fakeIssue("900", "feat", "global", issues.StatusOpen), child)
	v.collapsedGroups = map[string]bool{child.GroupKey: true}
	v.autoExpandedGroups = map[string]bool{child.GroupKey: true} // 番号フィルタが開けた分を人が畳んだ形

	v.anchorCursorInternal(child.Path)

	if v.collapsedGroups[child.GroupKey] {
		t.Fatalf("明示的な畳みが残ったまま: %+v", v.collapsedGroups)
	}
	if !v.groupExpanded(child.GroupKey) {
		t.Fatalf("group が開いていない: expanded=%+v collapsed=%+v", v.expandedGroups, v.collapsedGroups)
	}
	row, ok := v.currentDisplayRow()
	if !ok || row.kind != displayRowIssue || row.issue != child {
		t.Fatalf("カーソルが移動先の issue にない: ok=%v row=%+v", ok, row)
	}

	v.refresh() // 再スキャン相当 (autoExpandedGroups はここで捨てられる)
	if !v.groupExpanded(child.GroupKey) {
		t.Fatalf("refresh で group が畳み直された (展開先が autoExpandedGroups になっている)")
	}
}

// TestIssuesViewGroupProgressCountsOnlyVisibleChildren は親行の `(N ✓M)` が
// 「今その一覧に並ぶ子」を数えることを固定する (spec 3 節。タブを切り替えると両方縮む)。
func TestIssuesViewGroupProgressCountsOnlyVisibleChildren(t *testing.T) {
	dir := "/repo/issues"
	bugChild := func(number, slug string, status issues.Status) *issues.Issue {
		base := number + "-bug-" + slug + ".md"
		rel := filepath.Join(issues.EpicDirName, "alpha", base)
		if sub := epicStatusDir(status); sub != "" {
			rel = filepath.Join(issues.EpicDirName, "alpha", sub, base)
		}
		return &issues.Issue{
			Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: number,
			Category: "bug", Slug: slug, Status: status, Group: "alpha",
			GroupKind: issues.GroupEpic, GroupKey: filepath.Join(dir, issues.EpicDirName, "alpha"),
		}
	}
	v := loadedView(
		fakeEpicIssue(dir, "alpha", "710", "feat-open", issues.StatusOpen),
		fakeEpicIssue(dir, "alpha", "709", "feat-done", issues.StatusDone),
		bugChild("708", "open", issues.StatusOpen),
		bugChild("707", "done", issues.StatusDone),
	)

	if out := strings.Join(v.listLines(renderOpts(10)), "\n"); !strings.Contains(out, "▸ alpha (4 ✓2)") {
		t.Fatalf("All タブで全子を数えていない:\n%s", out)
	}
	v.tabIdx = tabIndexOf(v.tabs, "bug")
	if v.currentTab() != "bug" {
		t.Fatalf("bug タブが無い: %+v", v.tabs)
	}
	v.refresh()
	out := strings.Join(v.listLines(renderOpts(10)), "\n")
	if !strings.Contains(out, "▸ alpha (2 ✓1)") {
		t.Fatalf("タブ filter 後の集合を数えていない:\n%s", out)
	}
}

// TestUnmarkDestLabelCoversMixedSelection は「next を外す」確認モーダルの戻り先の呼び名が
// 選択の中身で決まることを固定する。先頭 1 件で決めていたため、global が先頭で group が
// 後続の並びだと「issues 直下」と嘘をついていた (敵対レビュー 3 周目 P2-1)。
func TestUnmarkDestLabelCoversMixedSelection(t *testing.T) {
	dir := "/repo/issues"
	global := fakeIssue("900", "feat", "global", issues.StatusNext)
	child := fakeEpicIssue(dir, "cloud", "702", "child", issues.StatusNext)
	for _, tc := range []struct {
		name    string
		targets []*issues.Issue
		want    string
	}{
		{"global のみ", []*issues.Issue{global}, "issues 直下"},
		{"group のみ", []*issues.Issue{child}, "group 直下"},
		{"混在 (group が先頭)", []*issues.Issue{child, global}, "元の group / issues 直下"},
		{"混在 (global が先頭)", []*issues.Issue{global, child}, "元の group / issues 直下"},
		{"0 件", nil, "issues 直下"},
		// 予約外ディレクトリの迷子は GroupKind が Unknown でも group の中に居る
		// (MoveToSubdir と同じ判定材料 = GroupKey で決める。GroupKind で判定すると素通りする)
		{"迷子 (GroupUnknown + GroupKey)", []*issues.Issue{strayIn(dir, "cloud", "703")}, "group 直下"},
	} {
		if got := unmarkDestLabel(tc.targets); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}

	// 箱の文面としても出ること (helper だけ直して呼び出し側が固定文言に戻る変異を弾く)。
	// 併せて助詞の前後に空白が入っていることも見る (「から」と地の文がくっついていた)
	v := loadedView(global, child)
	// 箱は幅を詰めるので、短い方 (group のみ) で全文を見る
	// 🚨 箱は 44 桁で頭打ち (centerBox) なので、**実在する長さのファイル名**で見る。
	// 短い fixture だけで見ていたため、宛先が丸ごと切り落とされているのを 3 周目まで
	// 検出できていなかった (2026-09-06 の敵対的レビュー 4 周目)
	long := fakeEpicIssue(dir, "cloud", "292", "audit-forge-should-hand-each-agent-its-own-worktree", issues.StatusNext)
	v.markNext = issuesMarkConfirm{active: true, unmark: true, targets: []*issues.Issue{long}}
	box := strings.Join(v.markNextBox(80, false), "\n")
	if !strings.Contains(box, "group 直下 へ戻します") {
		t.Fatalf("長い名前だと宛先が箱から切れる:\n%s", box)
	}
	v.markNext = issuesMarkConfirm{active: true, unmark: true, targets: []*issues.Issue{global, long}}
	if box = strings.Join(v.markNextBox(80, false), "\n"); !strings.Contains(box, "元の group / issues 直下 へ戻します") {
		t.Fatalf("混在選択で宛先が読めない:\n%s", box)
	}
}

// TestIssuesViewTabLineShowsBypassBadge はタブ行の配線を固定する (純関数の VisibleBadges を
// 呼んでいるか。計算が正しくても呼び出し側が段階だけを出していれば嘘は直っていない)。
func TestIssuesViewTabLineShowsBypassBadge(t *testing.T) {
	dir := "/repo/issues"
	v := loadedView(
		fakeIssue("900", "feat", "global", issues.StatusOpen),
		fakeEpicIssue(dir, "alpha", "710", "open", issues.StatusOpen),
		fakeEpicIssue(dir, "alpha", "709", "done", issues.StatusDone),
	)
	if v.filter != issues.FilterOpen {
		t.Fatalf("前提が違う: 既定の段階が open でない")
	}
	line := v.tabLine(renderOpts(10))
	if !strings.Contains(line, "○(✓)") {
		t.Fatalf("タブ行が迂回を示していない: %q", line)
	}
	// a を 1 段進めても ✓ は括弧の中のまま (段階は pending までしか見せていない)
	v.handleKey("a", vp(10))
	if line = v.tabLine(renderOpts(10)); !strings.Contains(line, "○⏸(✓)") {
		t.Fatalf("a を進めた後のバッジが違う: %q", line)
	}
}
