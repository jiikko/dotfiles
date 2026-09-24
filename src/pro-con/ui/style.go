package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 見た目 (2026-09-24 に B「枠」で合意。issue 415): 列と詳細を角丸の罫線で囲み、選択中のカードがある列の枠だけを現在地色にする。
// 色の意味は theme/colors.yml に揃える (現在地 202 / 要対応 214 / 未 push 208 / 情報 51 / 正常 46 / 消灯 240 / 危険 196)。
// Go からは theme/colors.yml を読めないので番号の手書きコピー (glogx の ansiFrameBorder と同じ事情)。

func fg(n int) string { return "\x1b[38;5;" + strconv.Itoa(n) + "m" }

const (
	sgrSelected = "\x1b[48;5;202m\x1b[38;5;16m\x1b[1m" // 現在地 (蛍光オレンジ地に黒字)
	sgrFgReset  = "\x1b[39m\x1b[22m"                   // 前景と太字だけを戻す (帯の背景色を消さない)
)

// stateColor は列 (状態) の色。意味は theme/colors.yml に揃える。
func stateColor(st card.State) int {
	switch st {
	case card.Requested:
		return 51 // 情報: 来たばかり
	case card.Planned:
		return 250 // 待機 (中立)
	case card.Running:
		return 46 // 正常 / アクティブ
	case card.Waiting:
		return 214 // 要対応 (人間の番)
	case card.Review:
		return 208 // 未 push マーカー (まだ master に載っていない)
	case card.Done:
		return 240 // 消灯
	}
	return 250
}

// paint は s を表示幅 w に合わせ、背景色 b で行全体を塗る。s の中の全リセットの後も背景色を塗り直す。
func paint(b, s string, w int) string {
	s = fit(s, w)
	return b + strings.ReplaceAll(s, sgrReset, sgrReset+b) + sgrReset
}

// boxLine は罫線の枠の中の 1 行 (│ + 中身 + │)。
func boxLine(border, s string, w int) string {
	return border + "│" + sgrReset + fit(s, w-2) + border + "│" + sgrReset
}

func boxTop(border, title string, w int) string {
	inner := w - 2
	t := ""
	if title != "" {
		t = "─ " + title + border + " "
	}
	rest := max(0, inner-ansi.StringWidth(t))
	return border + "╭" + t + strings.Repeat("─", rest) + "╮" + sgrReset
}

func boxBottom(border string, w int) string {
	return border + "╰" + strings.Repeat("─", max(0, w-2)) + "╯" + sgrReset
}
