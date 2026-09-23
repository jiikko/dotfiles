package layout

import (
	"math"

	"tuikit/anim"
	"tuikit/termwidth"
)

// SlideIn は窓の各行を「右から左へ流し込む」途中の姿にする (closing なら右へ抜ける途中)。
// progress は 0..1 (1 = 着地)。画面の外に居る行は空にする = 右端から現れ、右端へ消える。
//
// stagger は入ってくるときに行ごとに開始をずらす割合 (0 = 全行同時、0.35 で上から順に流れ込む)。
func SlideIn(window []string, progress float64, width int, closing bool, stagger float64) []string {
	out := make([]string, 0, len(window))
	last := max(len(window)-1, 1)
	for i, ln := range window {
		ratio := RowOffsetRatio(progress, i, last, closing, stagger)
		switch {
		case ratio <= 0:
			out = append(out, ln) // 着地済み
			continue
		case ratio >= 1 || ln == "":
			out = append(out, "") // まだ画面外 (または元から空行)
			continue
		}
		off := int(math.Round(ratio * float64(width)))
		if off >= width {
			out = append(out, "") // 端数で幅ぴったりまで押し出された (空白だけの行を残さない)
			continue
		}
		out = append(out, termwidth.PadSpaces(off)+termwidth.Clip(ln, width-off))
	}
	return out
}

// RowOffsetRatio は行 i (last = 最終行の index) の横ずれを幅に対する割合で返す
// (0 = 着地、1 = 画面外)。
//
// 入ってくる向きだけ EaseOutCubic で終端を減速させ、行ごとに開始をずらす。着地するものは
// 減速しないと「カクッ」と止まって見え、ずらしがあると上から順に流れ込んで見える。
//
// 🚨 出ていく向きにこの作法を流用しないこと。板には着地点が無いので、終端の減速は「もう画面から
// 消えているのに畳まれない時間」に化ける (easeOutCubic だと 280 桁端末で残り 100ms が真っ白)。
// ずらしも視線のある最上行を最後に回して動き出しを遅らせる。出ていくときは全行同時・等速。
func RowOffsetRatio(progress float64, i, last int, closing bool, stagger float64) float64 {
	if closing {
		return 1 - progress // 板が 1 枚まるごと等速で右へ抜ける
	}
	delay := stagger * float64(i) / float64(last)
	return 1 - anim.EaseOutCubic((progress-delay)/(1-stagger))
}
