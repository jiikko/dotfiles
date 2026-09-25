// Package layout は端末 UI の画面合成 (行の配列 → 行の配列)。描画フレームワークに依存しない。
//
// 入出力はすべて「1 要素 = 1 行」の []string で、各行は ANSI SGR を含んでよい。幅はすべて表示幅
// (termwidth.Of) で数える。
package layout

import (
	"math"

	"tuikit/sgr"
	"tuikit/termwidth"
)

// DrawerGeometry は引き出し (右から滑り込む詳細パネル) の寸法の決め方。
//
// 左に一覧の端を残すのが引き出しの目的: 全画面で置き換えると「どの一覧のどこから開いたか」が
// 画面から消える。残す幅は比率で決め、上乗せ (Extra) と上限 (MaxPeek) で本文を広げる。
//
// 🚨 本文は比率ぶん (total * Ratio) より狭くしない。MinList が抑えるのは Extra で広げる分だけで、
// 比率ぶんの本文は削らない。なので狭い画面 (glogx の寸法で total=17〜37) では、左に残る一覧は
// MinList を下回る。素直に MinList で頭打ちにすると、総幅 20 桁で本文が 16 → 12 桁と「広げたのに
// 狭くなる」本末転倒が起きた (glogx の実測。commit 51937016)。この契約は glogx の
// TestDrawerTargetWidth と、ここの TestDrawerTarget が固定している
type DrawerGeometry struct {
	// Ratio は開ききったときに本文が占める幅の割合 (例: 0.8)。
	Ratio float64
	// Extra は比率に上乗せする桁数。比率を上げるのでなく固定の上乗せにするのは、狭い端末でも
	// 一覧側が同じ桁数だけ残るようにするため。
	Extra int
	// MinList は Extra で本文を広げるときに、左に残す一覧の最小幅 (比率ぶんの本文は削らないので、
	// 狭い画面ではこれを下回る。型の doc)。画面幅が MinList*2 以下なら全幅を本文に使う。
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
	sep, reset := "▏", ""
	if colored {
		sep, reset = sgr.Dim+sep+sgr.Reset, sgr.Reset
	}
	if left >= total {
		sep = "" // total <= 0 (w も 0 以下に丸まる) では区切り線を置く桁が無い
	}
	out := make([]string, len(base))
	for i, b := range base {
		var p string
		if i < len(panel) {
			p = panel[i]
		}
		// 一覧側は覗き見の幅より長いのが普通なので、測る前に切る。ansi.Truncate は冒頭で全体の
		// 幅を測るので、先に Of で測ると長い行の走査が 1 回増える。
		line, lw := termwidth.CutMeasure(b, left)
		vis, vw := cutMeasure(p, max(w-1, 0))
		// 1 行 1 回の連結にする (毎フレーム全行で走る。+= で継ぎ足すと 1 行あたり 5 回確保していた)
		out[i] = line + reset + termwidth.PadSpaces(max(left-lw, 0)) + sep +
			vis + reset + termwidth.PadSpaces(max(w-1-vw, 0))
	}
	return out
}

// cutMeasure は termwidth.Cut と同じ切り詰めを行い、結果の表示幅も返す。
//
// 本文側用: 本文は開ききった幅で整形済みなので、開いている間は必ず収まる。先に測って収まれば
// 切り詰めを省く (ansi.Truncate は収まる行でも冒頭で全体の幅を測るので、Cut してから Of で
// 測り直すと同じ行を 2 回走査する)。収まらない行 (開閉の途中) だけ切って測り直す。
//
// ⚠️ この近道はテストで固定されていない。収まる行では ansi.Truncate も確保せずに返すので確保回数に
// 差が出ず、効果は時間にしか現れない。消すと BenchmarkComposeDrawer の open が 1.5 倍前後に戻る。
func cutMeasure(s string, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	if w := termwidth.Of(s); w <= width {
		return s, w
	}
	return termwidth.CutMeasure(s, width)
}

// PadTo は行数を n へ揃える (足りなければ空行、多ければ切る)。一覧と本文のように行数の違う
// 2 枚を重ねる前に揃えるのに使う。
//
// 🚨 lines と裏の配列を共有したスライスを返す (append は容量が余っていれば lines の配列へ書く)。
// 返り値を Overlay に渡すと lines 側の行も書き換わるので、lines を使い回すならコピーを渡す。
func PadTo(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}
