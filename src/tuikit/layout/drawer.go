// Package layout は端末 UI の画面合成 (行の配列 → 行の配列)。描画フレームワークに依存しない。
//
// 入出力はすべて「1 要素 = 1 行」の []string で、各行は ANSI SGR を含んでよい。幅はすべて表示幅
// (termwidth.Of) で数える。
package layout

import (
	"math"

	"tuikit/termwidth"
)

const (
	sgrDim   = "\x1b[2m"
	sgrReset = "\x1b[0m"
)

// DrawerGeometry は引き出し (右から滑り込む詳細パネル) の寸法の決め方。
//
// 左に一覧の端を残すのが引き出しの目的: 全画面で置き換えると「どの一覧のどこから開いたか」が
// 画面から消える。残す幅は比率で決めた上で [MinList, MaxPeek] に収める。
type DrawerGeometry struct {
	// Ratio は開ききったときに本文が占める幅の割合 (例: 0.8)。
	Ratio float64
	// Extra は比率に上乗せする桁数。比率を上げるのでなく固定の上乗せにするのは、狭い端末でも
	// 一覧側が同じ桁数だけ残るようにするため。
	Extra int
	// MinList は左に残す一覧の最小幅。本文が画面を食い切って「どこから開いたか」が消えるのを防ぐ。
	// 画面幅が MinList*2 以下なら、覗き見の余地が無いので全幅を本文に使う。
	MinList int
	// MaxPeek は左に残す一覧の最大幅。🚨 比率だけで決めると画面が広いほど一覧が場所を食う
	// (覗き見に要るのは「どの行から開いたか」が分かる幅だけ)。0 は上限なし。
	MaxPeek int
}

// Target は画面幅 total で開ききったときの本文の幅。
func (g DrawerGeometry) Target(total int) int {
	if total <= g.MinList*2 {
		return total // 覗き見の余地が無い狭さでは全幅を本文に使う (中途半端に削らない)
	}
	ratioOnly := int(math.Round(float64(total) * g.Ratio))
	peek := max(total-(ratioOnly+g.Extra), g.MinList)
	if g.MaxPeek > 0 {
		peek = max(min(peek, g.MaxPeek), g.MinList)
	}
	return max(total-peek, ratioOnly)
}

// DrawerWidth は開き具合 openness (0..1。anim.Transition.Openness) のときの本文の幅。
//
// 🚨 幅を 0 から伸ばす見え方にしない: それは「ページが開く」動きで「飛び出してくる」動きに
// ならない。ComposeDrawer は板を最終幅のまま位置だけ動かすので、ここが返すのは「画面に
// 入っている幅」であって板の幅ではない。
func DrawerWidth(target int, openness float64) int {
	return int(math.Round(float64(target) * openness))
}

// ComposeDrawer は一覧 base の上に、右端から w 桁だけ入ってきた本文 panel を重ねる。
// total は画面幅。w <= 0 なら base をそのまま返す。
//
// 板と一覧の境目に 1 桁の縦線を引く (colored なら薄く塗る)。
//
// 🚨 panel は**開ききった幅で整形済み**の行を渡すこと (演出中は切るだけ)。途中の幅で整形し直すと
// 毎フレーム折り返しが変わって文字が踊る。板の左端から w-1 桁だけが見える = 板が右外から
// 位置だけを変えて入ってくる見え方になる。
func ComposeDrawer(base, panel []string, w, total int, colored bool) []string {
	if w <= 0 {
		return base
	}
	w = min(w, total)
	left := total - w // 板の左辺 = 一覧が見えている幅
	sep := "▏"
	if colored {
		sep = sgrDim + sep + sgrReset
	}
	out := make([]string, len(base))
	for i, b := range base {
		var p string
		if i < len(panel) {
			p = panel[i]
		}
		line := cut(b, left)
		if colored {
			line += sgrReset
		}
		line += termwidth.PadSpaces(max(left-termwidth.Of(line), 0))
		if left < total {
			line += sep
		}
		vis := cut(p, max(w-1, 0))
		if colored {
			vis += sgrReset
		}
		out[i] = line + vis + termwidth.PadSpaces(max(w-1-termwidth.Of(vis), 0))
	}
	return out
}

// cut は s を表示幅 width へ切る (SGR は残し、印は付けない)。
func cut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return termwidth.Truncate(s, width, "")
}

// PadTo は行数を n へ揃える (足りなければ空行、多ければ切る)。一覧と本文のように行数の違う
// 2 枚を重ねる前に揃えるのに使う。
func PadTo(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}
