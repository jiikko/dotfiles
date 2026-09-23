package layout

import (
	"tuikit/sgr"
	"tuikit/termwidth"
)

// スクロールバーのグリフ。track は枠の側辺 (│) と同じ字形にして「本文の中に走る細い溝」に見せ、
// thumb だけ █ で持ち上げる。
const (
	ScrollbarTrack = "│"
	ScrollbarThumb = "█"
)

// ScrollbarWidth は Scrollbar が行の右端で消費する桁数 (バー 1 桁 + 手前の空き 1 桁)。
//
// 🚨 行を組む側が先にこの幅を引いておくこと (width - ScrollbarWidth で組む)。幅ぴったりに
// 組むと、Scrollbar のクリップで全幅の行だけ末尾 1 文字が "…" に化ける。別々の数で持つと
// どちらも「幅を超えない」のでテストの上限アサートを素通りする。
const ScrollbarWidth = 2

// Scrollbar は行列 rows の右端に 1 桁のスクロールバー列を足す。width は行が使える表示幅、
// total は全行数、offset は rows の先頭が全体の何行目か。
//
// 全体が収まる (total <= len(rows)) ときは行をそのまま返す (列を作らないので本文幅が戻る)。
// バー列すら入らない極小幅ではバーを描かず、行を width へ切るだけにする (本文 + バーが幅を
// 破らないように)。thumb の長さは表示比率、位置は offset 比率で、どちらも最低 1 行。末尾
// (offset = total - len(rows)) では thumb が下端に接地する。
func Scrollbar(rows []string, width, total, offset int, colored bool) []string {
	view := len(rows)
	if view == 0 || total <= view {
		return rows
	}
	contentW := width - ScrollbarWidth
	if contentW < 1 {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = termwidth.Clip(r, width)
		}
		return out
	}
	thumb := min(max(view*view/total, 1), view)
	maxOffset := total - view
	start := 0
	if travel := view - thumb; travel > 0 {
		start = min((offset*travel+maxOffset/2)/maxOffset, travel)
	}
	reset, track := "", ScrollbarTrack
	if colored {
		reset = sgr.Reset // 行末で色を閉じ、本文の SGR がバー列へ滲まないようにする
		track = sgr.Dim + ScrollbarTrack + sgr.Reset
	}
	out := make([]string, 0, view)
	for i, row := range rows {
		glyph := track
		if i >= start && i < start+thumb {
			glyph = ScrollbarThumb
		}
		content, cw := termwidth.ClipMeasure(row, contentW) // 切り詰めと幅を 1 回で (毎フレーム全行で走る)
		out = append(out, content+reset+termwidth.PadSpaces(contentW-cw)+" "+glyph)
	}
	return out
}
