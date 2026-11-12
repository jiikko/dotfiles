package view

import (
	"fmt"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 画面の色は mac-lan-manager (lanmgr/style.go) の配色に揃える (ユーザー決定 2026-11-11「a で修正やって」: 欄を面の色で塗ったカードにする案)。
// 24bit の色で書き、端末が対応していなければ bubbletea が近い色へ落とす。
//
// 端末の 16 色だけを使い背景を塗らない形 (2026-11-10 まで) から変えたので、端末のテーマの明暗には追従しない。明るいテーマの端末でも
// 欄の中は暗い面のまま。欄の中の文字は端末の文字色ではなく cText で描く (端末の文字色が暗いと暗い面に沈む)。
// 青 (34)・水色 (36) の基本色は今も使わない: 背景が青い端末で沈む (ユーザー報告 2026-11-10)。
type rgb struct{ r, g, b uint8 }

func hex(s string) rgb {
	var c rgb
	_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

// sgr は 24bit の色の指定 (layer は前景 38・地 48)。
func (c rgb) sgr(layer int) string { return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", layer, c.r, c.g, c.b) }
func (c rgb) fg() string           { return c.sgr(38) }
func (c rgb) bg() string           { return c.sgr(48) }

var (
	cBg      = hex("#1F1E1D") // カードの下辺の欠けを埋める色 (mac-lan-manager の端末の地)
	cSel     = hex("#3E3346") // 操作中の欄で選んだ行の地
	cSelIdle = hex("#332C38") // 操作していない欄で選んだ行の地 (タブの地と同じ)
	cShadow  = hex("#0B0A09")
	cText    = hex("#EBDBBC")
	cDim     = hex("#8F857A")
	cMuted   = hex("#5C554F")
	cAccent  = hex("#FF7A2F")
	cOK      = hex("#A9B665")
	cPink    = hex("#FF6FA8")
	cPurple  = hex("#B79CFF")
	cTeal    = hex("#5FD7C9")
	cYellow  = hex("#FFD36E")
	cTabIdle = hex("#332C38")
	cEdgeOff = hex("#3A3440") // 操作していない欄の枠の終わりの色

	surfList   = hex("#2A2430") // 話題の一覧の面
	surfDetail = hex("#1E2B2E") // 本文の面
)

// 解除は太字・下線 (22・24) と前景だけにし、地 (面や選んだ行の色) を消さない。前景は端末の文字色 (39) でなく cText へ戻す
var (
	fgText = cText.fg()
	offFg  = sgr.BoldOff + sgr.UnderlineOff + fgText
)

func paintFg(c rgb, s string) string { return c.fg() + s + offFg }
func boldFg(c rgb, s string) string  { return sgr.Bold + c.fg() + s + offFg }
func dim(s string) string            { return paintFg(cDim, s) }
func muted(s string) string          { return paintFg(cMuted, s) }
func accent(s string) string         { return boldFg(cAccent, s) }
func pink(s string) string           { return boldFg(cPink, s) }
func boldText(s string) string       { return sgr.Bold + s + sgr.BoldOff }
func reset(s string) string          { return s + sgr.Reset }
func shadow(block []string) []string { return layout.DropShadow(block, true, cShadow.fg()) }
func keycap(k, label string) string {
	return reset(hex("#4A3A6E").bg()+sgr.Bold+hex("#E6DAFF").fg()+" "+k+" ") + " " + reset(dim(label))
}
func hintText(key, desc string) string { return pink(key+":") + " " + dim(desc) }

// blend は stops を順に線形に混ぜた n 色。
func blend(n int, stops ...rgb) []rgb {
	out := make([]rgb, n)
	if len(stops) == 1 || n == 1 {
		for i := range out {
			out[i] = stops[0]
		}
		return out
	}
	lerp := func(a, b uint8, t float64) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	for i := range out {
		pos := float64(i) / float64(n-1) * float64(len(stops)-1)
		k := min(int(pos), len(stops)-2)
		t := pos - float64(k)
		a, b := stops[k], stops[k+1]
		out[i] = rgb{lerp(a.r, b.r, t), lerp(a.g, b.g, t), lerp(a.b, b.b, t)}
	}
	return out
}

// gradient は glyph を w 個並べ、前景を stops で流す。
func gradient(w int, glyph string, stops ...rgb) string {
	var b strings.Builder
	for _, c := range blend(max(w, 1), stops...) {
		b.WriteString(c.fg() + glyph)
	}
	return reset(b.String())
}

// gradRule は色の流れる太線。
func gradRule(w int, stops ...rgb) string { return gradient(w, "━", stops...) }

// logo は色の流れる「slack-bbs」。
func logo() string {
	name := []rune("slack-bbs")
	var b strings.Builder
	b.WriteString(sgr.Bold)
	for i, c := range blend(len(name), cAccent, cPink, cPurple) {
		b.WriteString(c.fg() + string(name[i]))
	}
	return reset(b.String())
}

// pillTab は両端を丸めたタブ (幅は label + 4)。
func pillTab(label string, bg, fg rgb, bold bool) string {
	b := ""
	if bold {
		b = sgr.Bold
	}
	return reset(bg.fg()+"▐") + reset(bg.bg()+b+fg.fg()+" "+label+" ") + reset(bg.fg()+"▌")
}

// pillWidth は pillTab が label に足す幅。
const pillWidth = 4

// 枠の色: 操作中の欄は水色から紫 (本文は桃色まで) へ流し、操作していない欄は暗く沈める。
var (
	edgeListOn = []rgb{cTeal, cPurple}
	edgeBodyOn = []rgb{cTeal, cPurple, cPink}
	edgeOff    = []rgb{cMuted, cEdgeOff}
)

// cardPadX は面の内側の左右の余白。狭い端末では 1 桁にして中身の幅を残す。
func cardPadX(w int) int {
	if w >= 100 {
		return 2
	}
	return 1
}

// cardChrome は幅 width のカードの、中身に使えない桁数 (左右の縁と余白)。
func cardChrome(padX int) int { return 2 + 2*padX }

// card は mac-lan-manager の card と同じ形の板: 上辺は ▂、左右は ▐ ▌、下辺は ▆ で、枠の色を edge で横へ流し、内側を面の色 surf で塗る。
// 行は rows 行 (足りなければ空行で埋め、多ければ切る)。返す行数は rows + 2、幅は width。
//
// 中身の行の SGR の全解除 (ESC[m・ESC[0m) の後には面の色と文字色を張り直す (layout.Scrollbar の行末などが全解除を出す)。
// 下辺の ▆ の上の欠けは cBg で埋める: mac-lan-manager と同じく、端末の地が cBg から遠いと下辺の下に細い帯が見える。
// tuikit の layout.Panel に寄せない: Panel は罫線の枠と題の行を持つ別の見た目。mac-lan-manager の card とも寄せない: 見た目は同じだが、
// あちらは lipgloss の色で 1 つの文字列を組み、こちらは 24bit の色の SGR で行の配列を組む (寄せるなら両方の色の持ち方を揃えてから)。
func card(lines []string, width, rows, padX int, surf rgb, edge []rgb) []string {
	inner := max(width-cardChrome(padX), 1)
	cs := blend(max(width-2, 1), edge...)
	var top, bot strings.Builder
	top.WriteString(" ")
	bot.WriteString(" ")
	for _, c := range cs {
		top.WriteString(c.fg() + "▂")
		bot.WriteString(c.bg() + cBg.fg() + "▆")
	}
	top.WriteString(sgr.Reset + " ")
	bot.WriteString(sgr.Reset + " ")
	surfSGR := surf.bg() + fgText
	pad := termwidth.PadSpaces(padX)
	left, right := reset(edge[0].fg()+"▐")+surfSGR+pad, pad+sgr.Reset+reset(edge[len(edge)-1].fg()+"▌")
	out := make([]string, 0, rows+2)
	out = append(out, top.String())
	for i := range rows {
		s := ""
		if i < len(lines) {
			s = lines[i]
		}
		s = termwidth.Truncate(s, inner, "…")
		s = sgr.ReapplyAfterReset(s, surfSGR)
		out = append(out, left+s+surfSGR+termwidth.PadSpaces(inner-termwidth.Of(s))+right)
	}
	return append(out, bot.String())
}

// paintRow は面の上の 1 行 (幅 w) を地 bg で塗り、行の後で面 surf の色に戻す。
func paintRow(s string, w int, bg, surf rgb) string {
	s = termwidth.Truncate(s, w, "…")
	s = sgr.ReapplyAfterReset(s, bg.bg()+fgText)
	return bg.bg() + s + termwidth.PadSpaces(w-termwidth.Of(s)) + surf.bg()
}
