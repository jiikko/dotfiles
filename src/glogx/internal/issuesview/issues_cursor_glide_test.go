package issuesview

import (
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"strings"
	"testing"
)

// cursorRow は描画されたリストのうち、カーソル記号が付いた行の「表示上の位置」を返す
// (見つからなければ -1)。窓の中に無ければカーソルは 1 行も描かれていない。
func cursorRow(lines []string) int {
	for i, ln := range lines {
		if strings.HasPrefix(ln, term.CursorGutterMark) {
			return i
		}
	}
	return -1
}

func TestIssuesListSpaceStartsCursorGlide(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	v.ListLines(renderOpts(20)) // 論理 offset を描画時の行数で収束させる (本番と同じ順序)
	v.HandleKey(" ", vp(20))
	if !v.CurGlide.Active() {
		t.Fatal("Space の半ページ移動でカーソル滑走が始まっていない")
	}
	if v.CurGlide.From() != 0 || v.Cursor == 0 {
		t.Fatalf("論理カーソルが即着地していない: from=%d cursor=%d", v.CurGlide.From(), v.Cursor)
	}
	// 滑走の途中では描画カーソルが起点と着地点の「あいだ」にいる (どちらとも違う)。
	v.AdvanceGlide()
	v.AdvanceGlide()
	got := v.dispCursor(v.offset, 20)
	if got <= v.CurGlide.From() || got >= v.Cursor {
		t.Fatalf("描画カーソルが途中位置にいない: from=%d disp=%d cursor=%d", v.CurGlide.From(), got, v.Cursor)
	}
	// 最終フレームで論理カーソルへ着地し、tick を止める。
	for range term.CursorAnimFrames {
		v.AdvanceGlide()
	}
	if v.CurGlide.Active() || v.dispCursor(v.offset, 20) != v.Cursor {
		t.Fatalf("着地していない: active=%v disp=%d cursor=%d", v.CurGlide.Active(), v.dispCursor(v.offset, 20), v.Cursor)
	}
}

func TestIssuesListLineMoveHasNoGlide(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	v.ListLines(renderOpts(20))
	for _, key := range []string{"j", "k", "G", "g"} {
		v.HandleKey(key, vp(20))
		if v.CurGlide.Active() {
			t.Fatalf("%q は滑走に載せない (1 行移動と端ジャンプは距離の意味が違う)", key)
		}
	}
}

func TestIssuesListGlideKeepsCursorInsideWindow(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(60)...)
	v.ListLines(renderOpts(20))
	v.HandleKey(" ", vp(20)) // 窓が動く距離を作る
	v.HandleKey(" ", vp(20))
	v.HandleKey(" ", vp(20))
	if !v.CurGlide.Active() {
		t.Fatal("前提: 3 回目の Space で滑走が始まっていない")
	}
	// 全フレームで「窓は表示カーソルを含む」= カーソル行が必ず 1 本描かれる。
	for f := 0; f <= term.CursorAnimFrames; f++ {
		lines := v.ListLines(renderOpts(20))
		if cursorRow(lines) < 0 {
			t.Fatalf("frame %d: 滑走中にカーソル行が描かれていない:\n%s", f, strings.Join(lines, "\n"))
		}
		v.AdvanceGlide()
	}
}

func TestIssuesListNextKeyLandsGlideImmediately(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	v.ListLines(renderOpts(20))
	v.HandleKey(" ", vp(20))
	v.AdvanceGlide()
	if !v.CurGlide.Active() {
		t.Fatal("前提: 滑走中でない")
	}
	v.HandleKey("j", vp(20)) // 次のキーは着地点に効く (描画も即座に追いつく)
	if v.CurGlide.Active() || v.dispCursor(v.offset, 20) != v.Cursor {
		t.Fatalf("次のキーで着地していない: active=%v disp=%d cursor=%d", v.CurGlide.Active(), v.dispCursor(v.offset, 20), v.Cursor)
	}
}

// 🚨 このテストが本命: カーソルが「実際に画面の上で滑って見える」ことを守る。滑走の状態
// (curGlide.Active()) や dispCursor の戻り値だけを見るテストは、描画がそれを使わなくなっても
// 緑のまま通る (rowLine のカーソル判定を論理カーソルへ戻す変異が、これを足すまで全 green だった)。
func TestIssuesListGlideMovesTheDrawnCursor(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	const page = 20
	v.ListLines(renderOpts(page))
	v.HandleKey(" ", vp(page))
	v.AdvanceGlide()
	v.AdvanceGlide()

	drawnRow := func() int {
		t.Helper()
		lines := v.ListLines(renderOpts(page))
		row := cursorRow(lines)
		if row < 0 {
			t.Fatalf("カーソル行が描かれていない:\n%s", strings.Join(lines, "\n"))
		}
		// 表示位置 → displayRows の添字 (ヘッダー行と窓の先頭を戻す)
		return row - len(v.listHeadLines(renderOpts(page).Width, false)) + v.offset
	}
	if got := drawnRow(); got == v.Cursor {
		t.Fatalf("描かれたカーソルが着地点に張り付いている (滑走が画面に出ていない): drawn=%d cursor=%d", got, v.Cursor)
	} else if got <= v.CurGlide.From() {
		t.Fatalf("描かれたカーソルが起点から進んでいない: drawn=%d from=%d", got, v.CurGlide.From())
	}

	for range term.CursorAnimFrames {
		v.AdvanceGlide()
	}
	if got := drawnRow(); got != v.Cursor {
		t.Fatalf("着地後も描かれたカーソルが着地点に来ない: drawn=%d cursor=%d", got, v.Cursor)
	}
}

// close は toggle (i) 経由だと handleKey を通らない = finishAnim が効かないので、滑走を自分で
// 止める必要がある (止めないと次に開いた一瞬だけ古い位置から滑る)。本文 pager の
// TestIssuesCloseStopsBodyGlide と対になる。
func TestIssuesCloseStopsCursorGlide(t *testing.T) {
	v := loadedView(issuesviewtest.ManyIssues(40)...)
	v.ListLines(renderOpts(20))
	v.HandleKey(" ", vp(20))
	if !v.CurGlide.Active() {
		t.Fatal("前提: 滑走が始まっていない")
	}
	v.Close()
	if v.CurGlide.Active() {
		t.Error("close でカーソルの滑走が残る")
	}
}
