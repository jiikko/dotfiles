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
	if status == issues.StatusNext {
		rel = filepath.Join(issues.EpicDirName, group, issues.NextDirName, base)
	}
	groupKey := filepath.Join(dir, issues.EpicDirName, group)
	return &issues.Issue{
		Path: filepath.Join(dir, rel), Dir: dir, Rel: rel, Number: number,
		Category: "feat", Slug: slug, Status: status, Group: group,
		GroupKind: issues.GroupEpic, GroupKey: groupKey,
	}
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
	// Space は展開の toggle、Enter は本文を開く
	v.handleKey(" ", vp(10))
	if !v.expandedGroups[parent.GroupKey] || len(v.displayRows) != 2 {
		t.Fatalf("Space で展開されない: expanded=%v rows=%d", v.expandedGroups, len(v.displayRows))
	}
	if v.displayRows[1].issue != child || !v.displayRows[1].inGroup {
		t.Fatalf("子行が親の下に並ばない: %+v", v.displayRows[1])
	}
	if v.toggleGroupAtCursor(false) {
		t.Fatalf("Enter (includeHead=false) が統合行で toggle した")
	}
}
