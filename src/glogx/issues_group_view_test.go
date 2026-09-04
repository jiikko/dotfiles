package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	fresh := make([]*issues.Issue, 0, len(old))
	for _, iss := range old {
		base := filepath.Base(iss.Path)
		path := filepath.Join(groupDir, issues.NextDirName, base)
		fresh = append(fresh, &issues.Issue{
			Path: path, Dir: dir, Rel: filepath.Join(issues.EpicDirName, "cloud", issues.NextDirName, base),
			Number: iss.Number, Category: iss.Category, Slug: iss.Slug, Status: issues.StatusNext,
			Group: "cloud", GroupKind: issues.GroupEpic, GroupKey: groupDir,
		})
	}
	v.receive(issuesScanMsg{dirs: []string{dir}, issues: fresh})
	if v.current() == nil || v.current().Path != filepath.Join(groupDir, issues.NextDirName, "001-feat-b.md") {
		t.Fatalf("cursor が move の戻り値へ再アンカーされない: %+v", v.current())
	}
	if lo, hi, ok := v.selection(); !ok || hi-lo != 1 {
		t.Fatalf("mark が move 後に再アンカーされない: lo=%d hi=%d ok=%v", lo, hi, ok)
	}
}
