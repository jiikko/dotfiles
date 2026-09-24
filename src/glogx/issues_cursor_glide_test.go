package main

import (
	"strings"
	"testing"

	"glogx/issues"
)

// manyIssues は滑走が観測できるだけの行数を持つ一覧を作る (窓より十分多い件数)。
func manyIssues(n int) []*issues.Issue {
	out := make([]*issues.Issue, 0, n)
	for i := n; i > 0; i-- {
		out = append(out, fakeIssue(numStr(i), "bug", "x", issues.StatusOpen))
	}
	return out
}

func numStr(i int) string {
	s := ""
	for _, d := range []int{100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}

// cursorRow は描画されたリストのうち、カーソル記号が付いた行の「表示上の位置」を返す
// (見つからなければ -1)。窓の中に無ければカーソルは 1 行も描かれていない。
func cursorRow(lines []string) int {
	for i, ln := range lines {
		if strings.HasPrefix(ln, cursorGutterMark) {
			return i
		}
	}
	return -1
}

func TestIssuesListSpaceStartsCursorGlide(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	v.listLines(renderOpts(20)) // 論理 offset を描画時の行数で収束させる (本番と同じ順序)
	v.handleKey(" ", vp(20))
	if !v.curGlide.Active() {
		t.Fatal("Space の半ページ移動でカーソル滑走が始まっていない")
	}
	if v.curGlide.From() != 0 || v.cursor == 0 {
		t.Fatalf("論理カーソルが即着地していない: from=%d cursor=%d", v.curGlide.From(), v.cursor)
	}
	// 滑走の途中では描画カーソルが起点と着地点の「あいだ」にいる (どちらとも違う)。
	v.advanceGlide()
	v.advanceGlide()
	got := v.dispCursor(v.offset, 20)
	if got <= v.curGlide.From() || got >= v.cursor {
		t.Fatalf("描画カーソルが途中位置にいない: from=%d disp=%d cursor=%d", v.curGlide.From(), got, v.cursor)
	}
	// 最終フレームで論理カーソルへ着地し、tick を止める。
	for range cursorAnimFrames {
		v.advanceGlide()
	}
	if v.curGlide.Active() || v.dispCursor(v.offset, 20) != v.cursor {
		t.Fatalf("着地していない: active=%v disp=%d cursor=%d", v.curGlide.Active(), v.dispCursor(v.offset, 20), v.cursor)
	}
}

func TestIssuesListLineMoveHasNoGlide(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	v.listLines(renderOpts(20))
	for _, key := range []string{"j", "k", "G", "g"} {
		v.handleKey(key, vp(20))
		if v.curGlide.Active() {
			t.Fatalf("%q は滑走に載せない (1 行移動と端ジャンプは距離の意味が違う)", key)
		}
	}
}

func TestIssuesListGlideKeepsCursorInsideWindow(t *testing.T) {
	v := loadedView(manyIssues(60)...)
	v.listLines(renderOpts(20))
	v.handleKey(" ", vp(20)) // 窓が動く距離を作る
	v.handleKey(" ", vp(20))
	v.handleKey(" ", vp(20))
	if !v.curGlide.Active() {
		t.Fatal("前提: 3 回目の Space で滑走が始まっていない")
	}
	// 全フレームで「窓は表示カーソルを含む」= カーソル行が必ず 1 本描かれる。
	for f := 0; f <= cursorAnimFrames; f++ {
		lines := v.listLines(renderOpts(20))
		if cursorRow(lines) < 0 {
			t.Fatalf("frame %d: 滑走中にカーソル行が描かれていない:\n%s", f, strings.Join(lines, "\n"))
		}
		v.advanceGlide()
	}
}

func TestIssuesListNextKeyLandsGlideImmediately(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	v.listLines(renderOpts(20))
	v.handleKey(" ", vp(20))
	v.advanceGlide()
	if !v.curGlide.Active() {
		t.Fatal("前提: 滑走中でない")
	}
	v.handleKey("j", vp(20)) // 次のキーは着地点に効く (描画も即座に追いつく)
	if v.curGlide.Active() || v.dispCursor(v.offset, 20) != v.cursor {
		t.Fatalf("次のキーで着地していない: active=%v disp=%d cursor=%d", v.curGlide.Active(), v.dispCursor(v.offset, 20), v.cursor)
	}
}

// 🚨 このテストが本命: カーソルが「実際に画面の上で滑って見える」ことを守る。滑走の状態
// (curGlide.Active()) や dispCursor の戻り値だけを見るテストは、描画がそれを使わなくなっても
// 緑のまま通る (rowLine のカーソル判定を論理カーソルへ戻す変異が、これを足すまで全 green だった)。
func TestIssuesListGlideMovesTheDrawnCursor(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	const page = 20
	v.listLines(renderOpts(page))
	v.handleKey(" ", vp(page))
	v.advanceGlide()
	v.advanceGlide()

	drawnRow := func() int {
		t.Helper()
		lines := v.listLines(renderOpts(page))
		row := cursorRow(lines)
		if row < 0 {
			t.Fatalf("カーソル行が描かれていない:\n%s", strings.Join(lines, "\n"))
		}
		// 表示位置 → displayRows の添字 (ヘッダー行と窓の先頭を戻す)
		return row - len(v.listHeadLines(renderOpts(page).width, false)) + v.offset
	}
	if got := drawnRow(); got == v.cursor {
		t.Fatalf("描かれたカーソルが着地点に張り付いている (滑走が画面に出ていない): drawn=%d cursor=%d", got, v.cursor)
	} else if got <= v.curGlide.From() {
		t.Fatalf("描かれたカーソルが起点から進んでいない: drawn=%d from=%d", got, v.curGlide.From())
	}

	for range cursorAnimFrames {
		v.advanceGlide()
	}
	if got := drawnRow(); got != v.cursor {
		t.Fatalf("着地後も描かれたカーソルが着地点に来ない: drawn=%d cursor=%d", got, v.cursor)
	}
}

// close は toggle (i) 経由だと handleKey を通らない = finishAnim が効かないので、滑走を自分で
// 止める必要がある (止めないと次に開いた一瞬だけ古い位置から滑る)。本文 pager の
// TestIssuesCloseStopsBodyGlide と対になる。
func TestIssuesCloseStopsCursorGlide(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	v.listLines(renderOpts(20))
	v.handleKey(" ", vp(20))
	if !v.curGlide.Active() {
		t.Fatal("前提: 滑走が始まっていない")
	}
	v.close()
	if v.curGlide.Active() {
		t.Error("close でカーソルの滑走が残る")
	}
}

// hint が案内する半ページ移動の 2 キーが、実際に逆向きに効くことを固定する。
//
// 🚨 上方向を `b` で案内しているのは shift+space が端末を選ぶため (docs/issues-viewer-spec.md
// 「半ページ移動はカーソルが滑る (窓は滑らせない)」の末尾)。案内を shift+space へ変えると、
// 非対応の端末では「押すと下へ行くキー」を上方向として案内することになる。
func TestIssuesListHintAdvertisesWorkingHalfPageKeys(t *testing.T) {
	v := loadedView(manyIssues(40)...)
	const page = 20
	v.listLines(renderOpts(page))
	if h := v.hint(testHintBudget(t)); !strings.Contains(h, "Space/b") {
		t.Fatalf("一覧の hint が半ページ移動を案内していない: %q", h)
	}
	v.handleKey(" ", vp(page))
	down := v.cursor
	if down == 0 {
		t.Fatal("案内した Space で下へ動かない")
	}
	v.handleKey("b", vp(page))
	if v.cursor >= down {
		t.Fatalf("案内した b で上へ戻らない: %d → %d", down, v.cursor)
	}
}
