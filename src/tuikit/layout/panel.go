package layout

import (
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// Border は枠の罫線 (上角 + 横 + 縦)。下辺は影に接地させる低ブロック ▖▁▗ 固定なので下角の字形は
// 持たない。どれも表示幅 1 の box-drawing 文字なので、幅計算は罫線の種類に依らない。
type Border struct{ TL, TR, H, V string }

var (
	BorderLight  = Border{TL: "┌", TR: "┐", H: "─", V: "│"} // 小さい板 (モーダル・パネル)
	BorderDouble = Border{TL: "╔", TR: "╗", H: "═", V: "║"} // 画面の最外周
)

// ShadowNearBlack は落ち影の既定の色 (256 色の近黒)。前景のブロック文字で描くので、グリフの
// 隙間から端末の地色が透けて半影になる (bg のベタ塗りではない)。
const ShadowNearBlack = "\x1b[38;5;232m"

// PanelStyle は板の見た目。
type PanelStyle struct {
	Border Border // 罫線の字形 (BorderLight / BorderDouble)
	Color  string // 罫線の SGR (colored のときだけ使う。通常は sgr.Dim)
	Shadow string // 落ち影の SGR (colored のときだけ使う。空なら ShadowNearBlack)
	// Fill は枠の内側に敷く地の色 (bg の SGR。colored のときだけ使う。空なら端末の地色のまま)。
	// 外 (影・余白) は塗らない: 影は近黒なので、地を暗い色で塗った上に落とすと見えなくなる
	Fill string
	// Indent は全行の頭に付ける空白の桁数 (0 = 付けない)。
	//
	// 🚨 見た目の都合ではなく確保を削るために持っている。呼び出し側で `" " + line` を全行に掛けると
	// 可視行ぶんの文字列を丸ごともう 1 部作る (glogx の実測で 1 フレーム 40 KB のうち 8.3 KB)。
	// 組み立てに織り込めば既にある連結の一部になる。
	Indent int
}

const (
	// PanelMinWidth は板の最小幅 (これ未満の width は Panel がここまで押し上げる)。
	PanelMinWidth = 10
	// PanelChrome は Panel が内容幅に足す固定分 ("│ " + " │" + 右影 1 桁 = 5)。内容の幅から
	// 板の幅を決めるときは content + PanelChrome を渡す。
	PanelChrome = 5
	// shadowBottomOffset は下端の影の左端を板の左端から右へずらす桁数 (右下へ落とす影)。
	shadowBottomOffset = 2
)

// PanelInnerWidth は罫線の幅 frameWidth (右影を除いた板の幅) のうち、中身に使える幅。
func PanelInnerWidth(frameWidth int) int { return frameWidth - 4 }

// PanelContentWidth は Panel に渡す width (右影 1 桁込み) のとき、中身の行に使える幅。Panel と同じく
// PanelMinWidth 未満の width は押し上げてから数える (中身の幅を決めてから行を折る呼び出し側は、これを使えば
// 影の 1 桁と最小幅を知らなくてよい)。
func PanelContentWidth(width int) int { return PanelInnerWidth(max(width, PanelMinWidth) - 1) }

// 落ち影のグリフ。色なし (NO_COLOR) では近黒が使えず █ だと地色に対して明るく浮くので、陰影文字で代用する。
const (
	ShadowFull     = "█" // 本体 (最も濃い)
	ShadowFeather  = "▓" // 縁 (一段淡い)
	ShadowMono     = "▒" // 色なしの本体
	ShadowMonoEdge = "░" // 色なしの縁
)

// Panel は右下に落ち影の付いた板を組む。返す行数は len(rows) + 3 (上辺 + 下辺 + 下端の影)、
// 各行の表示幅は width + Indent (width は右影 1 桁込み。罫線は width-1 に収まる)。
//
// タイトルは SGR を落としてから幅で切る (色が残ると幅の計算がずれて罫線が崩れる)。中身の行は
// 板の内側の幅で切り、足りない分を埋める。
//
// 🚨 色なし (colored=false) では中身の SGR も落とす。色なしでは reset を 1 つも出さないので、
// 閉じていない SGR が 1 行にあると、埋め草・罫線・後続の行まで属性が続いて画面が壊れる。
func Panel(title string, rows []string, width int, colored bool, st PanelStyle) []string {
	width = max(width, PanelMinWidth)
	shadowColor := st.Shadow
	if shadowColor == "" {
		shadowColor = ShadowNearBlack
	}
	b := st.Border
	fw := width - 1 // 罫線の幅 (残り 1 桁が右の影)
	inner := PanelInnerWidth(fw)
	lines := make([]string, 0, len(rows)+3)
	title = termwidth.Truncate(termwidth.StripSGR(title), fw-2, "…")
	top := b.TL + title + strings.Repeat(b.H, max(fw-2-termwidth.Of(title), 0)) + b.TR
	pre := termwidth.PadSpaces(st.Indent)
	// 最上段だけ影なし (影は右上角の 1 つ下から始まるのが自然な落ち影)
	lines = append(lines, pre+paint(top, st.Color, colored)+" ")
	// 行ごとに変わらない断片はループの外で 1 度だけ組む (毎フレーム全可視行が通る)。
	// 右影の上端 (最初の中身の行) だけ縁で柔らかく始め、以降は本体にする。
	leftEdge, rightEdge := paint(b.V+" ", st.Color, colored), paint(" "+b.V, st.Color, colored)
	fill := colored && st.Fill != ""
	if fill {
		leftEdge, rightEdge = paint(b.V, st.Color, colored), paint(b.V, st.Color, colored)
	}
	shadeFirst := shadowEdge(colored, shadowColor)
	shadeRest := shadowRun(1, colored, shadowColor)
	for i, row := range rows {
		if !colored {
			row = termwidth.StripSGR(row)
		}
		content, cw := termwidth.ClipMeasure(row, inner)
		shade := shadeRest
		if i == 0 {
			shade = shadeFirst
		}
		pad := termwidth.PadSpaces(max(inner-cw, 0))
		if fill {
			lines = append(lines, pre+leftEdge+st.Fill+" "+sgr.ReapplyAfterReset(content, st.Fill)+pad+" "+sgr.Reset+rightEdge+shade)
			continue
		}
		lines = append(lines, pre+leftEdge+content+pad+rightEdge+shade)
	}
	// 下辺はセル最下段の低ブロック (▖ + ▁ + ▗) で、下の落ち影に隙間なく接地させる。─ だと
	// 影との間に半セルの余白ができ、└┘ の角だけ中央高だと横線との間に段差が出る。
	bottom := paint("▖"+strings.Repeat("▁", fw-2)+"▗", st.Color, colored)
	if fill { // 低ブロックの上の空きは内側なので地を敷く (敷かないと中身と下辺の間に端末の地色の筋が出る)
		bottom = st.Color + st.Fill + "▖" + strings.Repeat("▁", fw-2) + "▗" + sgr.Reset
	}
	lines = append(lines, pre+bottom+shadowRun(1, colored, shadowColor)) // 右下角の影は最も深い
	return append(lines, shadowBottom(pre, width, colored, shadowColor))
}

// shadowBottom は幅 width (右影 1 桁込み) の板の下端の影 (頭に pre を付ける)。左端を shadowBottomOffset 桁ずらして
// 右下に落とし、左端は縁で溶かし、右端は右影の列と揃える。pre を同じ連結に入れるのは確保を 1 回に保つため (Panel は毎フレーム通る)
func shadowBottom(pre string, width int, colored bool, color string) string {
	return pre + termwidth.PadSpaces(shadowBottomOffset) + shadowEdge(colored, color) + shadowRun(width-1-shadowBottomOffset, colored, color)
}

// DropShadow は組み終わったブロック (罫線を自分で描いた板) に、Panel と同じ形の落ち影を付ける (幅と高さが 1 ずつ増える)。
// 右は 1 桁 (最上段は空け、2 段目は縁、以降は本体)、下は右へ shadowBottomOffset 桁ずらした 1 行。行の幅が揃っていなければ
// 一番広い行に合わせて空白で埋める。color は影の SGR (空なら ShadowNearBlack)。
func DropShadow(block []string, colored bool, color string) []string {
	if color == "" {
		color = ShadowNearBlack
	}
	w := 0
	for _, l := range block {
		w = max(w, termwidth.Of(l))
	}
	out := make([]string, 0, len(block)+1)
	edge, run := shadowEdge(colored, color), shadowRun(1, colored, color)
	for i, l := range block {
		l += termwidth.PadSpaces(max(w-termwidth.Of(l), 0))
		switch i {
		case 0:
			l += " "
		case 1:
			l += edge
		default:
			l += run
		}
		out = append(out, l)
	}
	return append(out, shadowBottom("", w+1, colored, color))
}

func paint(s, color string, colored bool) string {
	if !colored {
		return s
	}
	return color + s + sgr.Reset
}

func shadowRun(n int, colored bool, color string) string {
	if n <= 0 {
		return ""
	}
	if !colored {
		return strings.Repeat(ShadowMono, n)
	}
	return color + strings.Repeat(ShadowFull, n) + sgr.Reset
}

func shadowEdge(colored bool, color string) string {
	if !colored {
		return ShadowMonoEdge
	}
	return color + ShadowFeather + sgr.Reset
}

// Overlay は box を window の anchor 行へ重ねる (行ごと置き換える)。下に収まらなければ、
// page 行の中に収まる位置まで引き上げる。
//
// 🚨 window を**その場で書き換えて**返す (ComposeDrawer / SlideIn と違い、新しいスライスを
// 作らない)。キャッシュした行や別の画面と共有している行を渡すと、重ねた板が
// 元の行に残る (閉じたはずのモーダルが次のフレームにも描かれる)。共有しているならコピーを渡す。
func Overlay(window, box []string, anchor, page int) []string {
	start := max(min(anchor, max(page-len(box), 0)), 0)
	for i, p := range box {
		pos := start + i
		if pos < len(window) {
			window[pos] = p
		} else if len(window) < page {
			window = append(window, p)
		}
	}
	return window
}

// OverlayRight は box を window の右端に寄せ、base 行目から重ねる (左側の背景は残し、箱の直前で reset して
// 背景の開いた色が箱へ滲まないようにする)。箱の行が width 以上なら、その行は箱を width で切って置き換える。
//
// 🚨 window を**その場で書き換えて**返す (Overlay と同じ)。共有している行を渡すならコピーを渡す。
func OverlayRight(window, box []string, width int, colored bool, base int) []string {
	if len(window) == 0 || width <= 0 || len(box) == 0 {
		return window
	}
	reset := ""
	if colored {
		reset = sgr.Reset
	}
	for i, row := range box {
		pos := base + i
		if pos < 0 {
			continue
		}
		if pos >= len(window) {
			break
		}
		bw := termwidth.Of(row)
		if bw >= width {
			window[pos] = termwidth.Clip(row, width)
			continue
		}
		left, lw := termwidth.CutMeasure(window[pos], width-bw)
		window[pos] = left + reset + termwidth.PadSpaces(width-bw-lw) + row
	}
	return window
}

// OverlayCentered は box を画面の中央に「浮かせて」重ねる。Overlay が行を塗り潰すのに対し、
// 各行で box が占める列だけを差し替え、左右の背景は残す。垂直は page 行の中で中央に置く。
//
// 左の背景は Cut で残し、右の背景は DropColumns で box の右端以降を復元する。box の前後に
// reset を挟み、背景の色が box に、box の色が右の背景に滲まないようにする。
//
// 🚨 Overlay と同じく window をその場で書き換える (共有している行を渡さない。理由は Overlay の doc)。
func OverlayCentered(window, box []string, width, page int, colored bool) []string {
	if len(box) == 0 || len(window) == 0 || width <= 0 {
		return window
	}
	reset := ""
	if colored {
		reset = sgr.Reset
	}
	bw := 0
	for _, r := range box {
		bw = max(bw, termwidth.Of(r))
	}
	leftGap := max((width-bw)/2, 0)
	leftPad := termwidth.PadSpaces(leftGap)
	start := min(max((page-len(box))/2, 0), max(page-len(box), 0))
	for i, boxRow := range box {
		pos := start + i
		if pos >= page {
			break
		}
		if pos >= len(window) {
			if len(window) < page {
				window = append(window, leftPad+boxRow) // 背景の行が無いところは素の余白 + box
			}
			continue
		}
		bg := window[pos]
		left := termwidth.Cut(bg, leftGap)
		left += termwidth.PadSpaces(max(leftGap-termwidth.Of(left), 0))
		right := termwidth.DropColumns(bg, leftGap+termwidth.Of(boxRow))
		window[pos] = left + reset + boxRow + reset + right
	}
	return window
}
