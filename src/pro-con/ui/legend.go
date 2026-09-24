package ui

// ? で出すレーンの意味の表。説明の正本は card.State.Meaning (ここは並べて見せるだけ)。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"
	"tuikit/sgr"

	"pro-con/card"
)

// handleLegendKey は表を出している間のキー。? / q / esc で閉じ、ctrl+c は終了の手続きへ。それ以外は飲み込む
// (表の下でカンバンが動くと、閉じたときに居場所が変わって見える)。
func (m *Model) handleLegendKey(key string) tea.Cmd {
	switch key {
	case "?", "q", "esc":
		m.legend = false
	case "ctrl+c":
		m.legend = false
		return m.requestQuit()
	}
	return nil
}

// overlayLegend は表を画面の中央に重ねる。
func (m *Model) overlayLegend(screen []string) []string {
	if !m.legend {
		return screen
	}
	width, inner := legendSize(m.width)
	rows := legendRows(inner)
	box := layout.Panel(" レーンの意味 ", rows, width, true, layout.PanelStyle{Border: layout.BorderLight, Color: sgr.Dim})
	return layout.OverlayCentered(screen, box, m.width, len(screen), true)
}

// legendSize は画面の幅 total に対する表の板の幅と、中身の幅。
// 🚨 Panel の width は右の影 1 桁込みなので、中身の幅は width-1 の枠から数える (1 桁広く折り返すと行末が … で切れる)
func legendSize(total int) (width, inner int) {
	width = min(total-4, 76)
	return width, layout.PanelInnerWidth(width - 1)
}

// legendRows は表の中身の行 (どの行も幅 inner に収まるよう折り返す)。
func legendRows(inner int) []string {
	var rows []string
	for i, s := range card.Columns {
		rows = append(rows, fg(stateColor(s))+sgrBold+fmt.Sprintf("%d %s", i+1, s.Label())+sgrReset)
		for _, l := range strings.Split(ansi.Hardwrap(s.Meaning(), max(inner-2, 10), true), "\n") {
			rows = append(rows, "  "+l)
		}
	}
	return rows
}
