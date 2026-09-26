package ui

// ? で出すレーンの意味の表。説明の正本は card.State.Meaning とポイントの card.PointsMeaning (ここは並べて見せるだけ)。

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
	// explain は説明の文を幅に収まるよう折り返し、見出しの下に 2 桁下げて足す
	explain := func(text string) {
		for _, l := range strings.Split(ansi.Hardwrap(text, max(inner-2, 10), true), "\n") {
			rows = append(rows, "  "+l)
		}
	}
	for i, s := range card.Columns {
		rows = append(rows, fg(stateColor(s))+sgrBold+fmt.Sprintf("%d %s", i+1, s.Label())+sgrReset)
		explain(s.Meaning())
	}
	rows = append(rows, "", humanTag(humanMark+"の番")) // 印の意味 (452)。どれが人の番かの正本は card.Turn
	explain(humansTurnMeaning)
	rows = append(rows, "", sgrBold+"右上の 3pt = 見積もりのポイント"+sgrReset) // カードの右上の数 (490)。意味の正本は card.PointsMeaning
	for _, m := range card.PointsMeaning {
		explain(m)
	}
	return rows
}

const humansTurnMeaning = "人が操作しないと進まない (権限の確認・落ち続けて止めた PG・PM か取り込みの係が人に回したもの・" +
	"起こさない設定の役の仕事)。黄の字はこれだけに使う"
