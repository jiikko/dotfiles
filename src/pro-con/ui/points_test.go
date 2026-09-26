package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 見積もりはカードの 1 行目の右端に薄く出る。タイトルは 1 行目で切らずに 2 行目へ続く。見積もり無しは出さない。
// 1 行目のタイトルが minTitleHead より短くなる幅なら出さない (issue 490)。
func TestCardCellPoints(t *testing.T) {
	m := New(newSpy(), nil)
	c := card.Card{ID: "C-047", State: card.Planned, Title: "カードの右上に見積もりのポイントを出す", Points: 3}
	lines := m.cardCell(c, 30)
	head := ansi.Strip(lines[0])
	if ansi.StringWidth(head) != 30 || !strings.HasSuffix(head, " 3pt") || !strings.Contains(lines[0], sgrDim+"3pt") {
		t.Fatalf("1 行目の右端に薄い 3pt が無い: %q", lines[0])
	}
	title := strings.TrimSpace(strings.TrimSuffix(head, "3pt")) + strings.TrimSpace(ansi.Strip(lines[1]))
	if strings.Contains(title, "…") || !strings.HasSuffix(title, "ポイントを出す") {
		t.Fatalf("タイトルが 2 行に続いていない: %q", title)
	}
	c.Points = 0
	if l := ansi.Strip(m.cardCell(c, 30)[0]); strings.Contains(l, "pt") {
		t.Fatalf("見積もり無しで出た: %q", l)
	}
	c.Points = 8
	// 中の幅 w-1 から「 8pt」の 4 桁を引いて minTitleHead ちょうどなら出し、1 桁狭いと出さない
	if l := ansi.Strip(m.cardCell(c, 1+minTitleHead+4)[0]); !strings.HasSuffix(l, " 8pt") {
		t.Fatalf("タイトルの 1 行目が %d 桁残るのに出していない: %q", minTitleHead, l)
	}
	if l := ansi.Strip(m.cardCell(c, minTitleHead+4)[0]); strings.Contains(l, "8pt") {
		t.Fatalf("タイトルの 1 行目を %d 桁より短くして出した: %q", minTitleHead, l)
	}
}

// ? の印とポイントのタブにポイントの意味が全行出る (正本は card.PointsMeaning)。
func TestLegendHasPointsMeaning(t *testing.T) {
	text := ansi.Strip(strings.Join(legendRows(legendMarks, 200), "\n"))
	for _, m := range card.PointsMeaning {
		if !strings.Contains(text, m) {
			t.Errorf("? の表に無い: %q", m)
		}
	}
}
