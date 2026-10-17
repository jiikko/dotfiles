package main

import (
	"strings"
	"testing"

	"glogx/internal/issuesview/issuesviewtest"
)

// hint が案内する半ページ移動の 2 キーが、実際に逆向きに効くことを固定する。
//
// 🚨 上方向を `b` で案内しているのは shift+space が端末を選ぶため (docs/issues-viewer-spec.md
// 「半ページ移動はカーソルが滑る (窓は滑らせない)」の末尾)。案内を shift+space へ変えると、
// 非対応の端末では「押すと下へ行くキー」を上方向として案内することになる。
func TestIssuesListHintAdvertisesWorkingHalfPageKeys(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	const page = 20
	v.ListLines(renderOpts(page))
	if h := v.Hint(testHintBudget(t)); !strings.Contains(h, "Space/b") {
		t.Fatalf("一覧の hint が半ページ移動を案内していない: %q", h)
	}
	v.HandleKey(" ", vp(page))
	down := v.Cursor
	if down == 0 {
		t.Fatal("案内した Space で下へ動かない")
	}
	v.HandleKey("b", vp(page))
	if v.Cursor >= down {
		t.Fatalf("案内した b で上へ戻らない: %d → %d", down, v.Cursor)
	}
}
