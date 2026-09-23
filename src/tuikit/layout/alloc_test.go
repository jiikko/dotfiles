package layout

import (
	"strings"
	"testing"

	"tuikit/sgr"
)

// 板とスクロールバーは毎フレーム全可視行で走るので、1 行あたりの確保を増やさないことを固定する。
//
// 🚨 これは glogx の ruleguard (空白の連結は termwidth.PadSpaces を使う = 無確保) の代わり。tuikit には
// ruleguard が無く、字句で止める代わりに**効果 (確保の回数)** を測る。
//
// 🚨 幅 250 のケースを消さないこと。Go 1.26 の strings.Repeat(" ", n) は n <= 128 なら確保しない
// (PadSpaces は 256 まで確保しない)。差が出るのは埋め草が 129〜256 桁の板 = 広い端末の最外周の枠
// だけなので、幅 80 だけで測ると埋め草を strings.Repeat へ戻す退行が緑のまま通る (実測)。
//
// 上限は実測 (2026-09-24, go1.26.0): 色ありの Panel = 行数 + 14、Scrollbar = 行数 + 1 (幅 80 / 250 で
// 同じ)。1 行あたり 1 回は行の文字列の連結そのもの。上限を緩めるなら、増えた確保が何かを先に
// 説明できること。
func TestPanelAndScrollbarAllocsPerRow(t *testing.T) {
	for _, n := range []int{10, 40} {
		rows := make([]string, n)
		for i := range rows {
			rows[i] = "\x1b[31m行の中身 " + strings.Repeat("x", i%30) + "\x1b[0m" // 埋め草が要る短い行を混ぜる
		}
		st := PanelStyle{Border: BorderLight, Color: sgr.Dim, Indent: 1}
		for _, width := range []int{80, 250} {
			if got, limit := testing.AllocsPerRun(50, func() { Panel("title", rows, width, true, st) }), float64(n+14); got > limit {
				t.Errorf("rows=%d width=%d: Panel の確保 %.0f 回 > 上限 %.0f (1 行あたりの確保が増えた)", n, width, got, limit)
			}
			if got, limit := testing.AllocsPerRun(50, func() { Scrollbar(rows, width, n*3, 5, true) }), float64(n+1); got > limit {
				t.Errorf("rows=%d width=%d: Scrollbar の確保 %.0f 回 > 上限 %.0f (1 行あたりの確保が増えた)", n, width, got, limit)
			}
		}
	}
}
