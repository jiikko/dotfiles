package commitview

import (
	"glogx/internal/term"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
)

// job 詳細ポップアップの本文にスクロールバー列が出ても枠が崩れない (全行が同じ表示幅) ことと、
// 全行が収まるときはバー列が出ないこと。term.WithScrollbar の単体テストではなく描画経路 (boxLines)
// で幅の均一性を見る (rows と len(lines) の噛み合わせが崩れる回帰を捕まえる)。
func TestJobDetailBoxLinesScrollbar(t *testing.T) {
	logLines := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "log line"
		}
		return out
	}
	const width, rows = 50, 8
	o := NewJobDetailOverlay()
	o.open = true

	// 溢れる: バー列あり + 幅は均一 + offset に応じて thumb が動く
	o.cache.Store("over", logLines(50), "over")
	o.pager.Offset = 20
	box := o.BoxLines(width, false, "", "job", "over", rows)
	uniformWidth(t, box)
	// 影付き箱: 末尾 2 行は下辺 (▖▁▗+影) と下端影なので本文から除く。本文行の行末は
	// 右影 1 桁 (NO_COLOR は ░/▒) が付く
	body := box[1 : len(box)-2]
	thumbs := 0
	for i, l := range body {
		shade := layout.ShadowMono
		if i == 0 {
			shade = layout.ShadowMonoEdge
		}
		trimmed := strings.TrimSuffix(strings.TrimSuffix(l, shade), " "+layout.BorderLight.V)
		switch {
		case strings.HasSuffix(trimmed, layout.ScrollbarThumb):
			thumbs++
		case strings.HasSuffix(trimmed, layout.ScrollbarTrack):
		default:
			t.Fatalf("本文行 %d にバー列が無い: %q", i, l)
		}
	}
	if thumbs == 0 || thumbs == len(body) {
		t.Fatalf("thumb 行数 = %d (本文 %d 行) — 比率になっていない", thumbs, len(body))
	}

	// colored でも幅は均一 (SGR 入りの影・バー列が幅計算を狂わせない)
	uniformWidth(t, o.BoxLines(width, true, "", "job", "over", rows))

	// 収まる: バー列なし (本文幅が戻る)。幅は溢れる場合と同じ (枠幅は不変)
	o.cache.Store("fit", logLines(rows-2), "fit")
	o.pager.Offset = 0
	fit := o.BoxLines(width, false, "", "job", "fit", rows)
	if w := uniformWidth(t, fit); w != term.DispWidth(term.StripANSI(box[0])) {
		t.Fatalf("収まる場合の枠幅 = %d, 溢れる場合 = %d", w, term.DispWidth(term.StripANSI(box[0])))
	}
	for i, l := range fit[1 : len(fit)-2] {
		if strings.Contains(l, layout.ScrollbarThumb) {
			t.Fatalf("収まるのに thumb が出ている (行 %d): %q", i, l)
		}
	}
}
