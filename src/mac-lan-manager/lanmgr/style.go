package lanmgr

// 見た目の部品 (色・枠・影・ピル)。見た目の決定は issue 747 の「配色」「枠の見た目とキー」「charm 版」節が正本。
// 色は truecolor で書く (bubbletea が端末に合わせて 256 色へ落とす)。意味は docs/theme-colors.md に合わせる。

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	cBg      = lipgloss.Color("#1F1E1D") // 端末の地 (ClaudeWarm)
	cSel     = lipgloss.Color("#3E3346") // 選んだ行の地
	cCode    = lipgloss.Color("#171615") // コマンドの面 (地より一段暗い)
	cShadow  = lipgloss.Color("#0B0A09") // 落ち影
	cText    = lipgloss.Color("#EBDBBC")
	cDim     = lipgloss.Color("#8F857A")
	cMuted   = lipgloss.Color("#5C554F")
	cAccent  = lipgloss.Color("#FF7A2F")
	cOK      = lipgloss.Color("#A9B665")
	cOKBg    = lipgloss.Color("#3A4026")
	cWarn    = lipgloss.Color("#E3A84E")
	cWarnBg  = lipgloss.Color("#4A3A1E")
	cInfo    = lipgloss.Color("#7DAEA3")
	cPink    = lipgloss.Color("#FF6FA8")
	cPurple  = lipgloss.Color("#B79CFF")
	cTeal    = lipgloss.Color("#5FD7C9")
	cYellow  = lipgloss.Color("#FFD36E")
	cTabIdle = lipgloss.Color("#332C38")

	// タイルの面 (左右で色味を変え、それぞれの枠のグラデーションの色に寄せる)
	surfList   = "#2A2430"
	surfDetail = "#1E2B2E"
	surfPanel  = "#2B2530"
	surfModal  = "#30293F"

	// タブごとの色 (管理対象 = 水色 / 未管理 = ピンク)。一覧のカードの枠もこの色から始まる
	tabColors = []color.Color{cTeal, cPink}
)

var (
	sText   = lipgloss.NewStyle().Foreground(cText)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sOK     = lipgloss.NewStyle().Foreground(cOK)
	sKeycap = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6DAFF")).Background(lipgloss.Color("#4A3A6E")).Bold(true).Padding(0, 1)
	sCode   = lipgloss.NewStyle().Foreground(cText).Background(cCode).Padding(0, 1)

	pillOn  = lipgloss.NewStyle().Foreground(cOK).Background(cOKBg).Padding(0, 1)
	pillOff = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
	pillWrn = lipgloss.NewStyle().Foreground(cWarn).Background(cWarnBg).Padding(0, 1).Bold(true)
	// 機能ごとに色を分ける (ssh = 水色 / 画面共有 = 紫 / smb = 黄)
	pillSSH = lipgloss.NewStyle().Foreground(cTeal).Background(lipgloss.Color("#1C3B39")).Padding(0, 1).Bold(true)
	pillVNC = lipgloss.NewStyle().Foreground(cPurple).Background(lipgloss.Color("#2E2850")).Padding(0, 1).Bold(true)
	pillSMB = lipgloss.NewStyle().Foreground(cYellow).Background(lipgloss.Color("#3D3420")).Padding(0, 1).Bold(true)
)

func keycap(k, label string) string { return sKeycap.Render(k) + " " + sDim.Render(label) }

// fill は block を面の色で塗る。lipgloss は中の文字の色の後に全部を戻す SGR を出すので、戻した直後に面の色を塗り直す
// (Style.Background だけでは、中に色付きの部品がある行で地が抜ける)。
func fill(block, hex string) string {
	var r, g, b int
	_, _ = fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	seq := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		for _, reset := range []string{"\x1b[m", "\x1b[0m", "\x1b[49m"} {
			l = strings.ReplaceAll(l, reset, reset+seq)
		}
		lines[i] = seq + l + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// card は面で塗った中身を、ブロックの字形の枠で囲む (幅 width。罫線はセルの真ん中に描かれて面との間に半セルの隙間ができるので使わない):
// 左 = 右半分 ▐ / 右 = 左半分 ▌ (半セル ≒ 4px) / 上 = 下 1/4 ▂ / 下 = ▆ を字の色 = 地・背景 = 枠の色で描いて上 1/4 だけを枠の色に見せる
// (1/4 行 ≒ 4px で左右と太さが揃う)。四隅は空けて角を丸く見せる。edge は枠の色 (2 色以上で左から右へのグラデーション)。
func card(content string, width int, surf string, edge ...color.Color) string {
	if len(edge) == 1 {
		edge = append(edge, edge[0])
	}
	inner := lipgloss.NewStyle().Padding(1, 2).Width(width - 2).Render(content)
	body := strings.Split(fill(inner, surf), "\n")
	var top, bot strings.Builder
	top.WriteString(" ")
	bot.WriteString(" ")
	for _, c := range lipgloss.Blend1D(max(width-2, 1), edge...) {
		top.WriteString(lipgloss.NewStyle().Foreground(c).Render("▂"))
		bot.WriteString(lipgloss.NewStyle().Foreground(cBg).Background(c).Render("▆"))
	}
	top.WriteString(" ")
	bot.WriteString(" ")
	l := lipgloss.NewStyle().Foreground(edge[0]).Render("▐")
	r := lipgloss.NewStyle().Foreground(edge[len(edge)-1]).Render("▌")
	out := make([]string, 0, len(body)+2)
	out = append(out, top.String())
	for _, b := range body {
		out = append(out, l+b+r)
	}
	return strings.Join(append(out, bot.String()), "\n")
}

// shadow はタイルに右と下の落ち影を付ける (幅と高さが 1 ずつ増える)。右は 1 桁の暗い面、下は上半分のブロック ▀
// (セルの縦横比でも右と下の厚みが揃う)。右上の角は ▄、右下の角は ▘ で丸める。左上から光が当たる向き。
func shadow(block string) string {
	lines := strings.Split(block, "\n")
	w := lipgloss.Width(block)
	side := lipgloss.NewStyle().Background(cShadow).Render(" ")
	sh := lipgloss.NewStyle().Foreground(cShadow)
	for i, l := range lines {
		l += strings.Repeat(" ", max(w-lipgloss.Width(l), 0))
		switch i {
		case 0:
			l += " "
		case 1:
			l += sh.Render("▄")
		default:
			l += side
		}
		lines[i] = l
	}
	// 下の影: 左の 2 桁は空ける (タイルの左下の角が欠けているので)。全体で w+1 桁 (右の影の列の分)
	bottom := "  " + sh.Render(strings.Repeat("▀", max(w-2, 0))+"▘")
	return strings.Join(append(lines, bottom), "\n")
}

// gradRule は色が流れる太線。
func gradRule(w int, stops ...color.Color) string {
	var b strings.Builder
	for _, c := range lipgloss.Blend1D(max(w, 1), stops...) {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render("━"))
	}
	return b.String()
}

// pillTab は両端を半ブロックで丸めた札 (タブ)。
func pillTab(label string, bg, fg color.Color, bold bool) string {
	capS := lipgloss.NewStyle().Foreground(bg)
	return capS.Render("▐") + lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(bold).Padding(0, 1).Render(label) + capS.Render("▌")
}

// logo は 1 字ずつ色を流した名前。
func logo() string {
	var b strings.Builder
	name := []rune("mac-lan-manager")
	for i, c := range lipgloss.Blend1D(len(name), cAccent, cPink, cPurple) {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(name[i])))
	}
	return b.String()
}

// modal は確認などの板 (中央に重ねる)。
// modal は中央の板。maxW (画面の幅) より広くならないよう、中身は板の幅で折り返す (固定の幅のままだと、狭い端末で右の列と
// 「どのキーでも閉じる」などの案内が切れた。監査の指摘)。
func modal(title string, body []string, hint string, maxW int) string {
	content := sAccent.Render(title) + "\n\n" + strings.Join(body, "\n") + "\n\n" + sDim.Render(hint)
	return shadow(card(content, max(min(lipgloss.Width(content)+6, maxW-1), 12), surfModal, cPink, cYellow)) // -1 は落ち影の分
}
