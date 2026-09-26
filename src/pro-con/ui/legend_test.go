package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"

	"pro-con/card"
)

// ? で全レーンの名前と意味の表が出て、? / q / esc で閉じる。出している間はカンバンが動かない。
func TestLegendShowsEveryLane(t *testing.T) {
	for _, closeKey := range []string{"?", "q", "esc"} {
		m := New(newSpy(), nil)
		m.width, m.height = 120, 40
		sel := m.selected
		press(m, "?")
		out := ansi.Strip(m.render())
		for _, s := range card.Columns {
			// 説明は折り返すので、先頭の 8 文字で探す
			if !strings.Contains(out, s.Label()) || !strings.Contains(out, string([]rune(s.Meaning())[:8])) {
				t.Fatalf("表に %s の名前か意味が無い:\n%s", s.Label(), out)
			}
		}
		press(m, "j", "l")
		if m.selected != sel {
			t.Fatalf("表の下でカンバンが動いた: %s → %s", sel, m.selected)
		}
		press(m, closeKey)
		if m.legend || strings.Contains(ansi.Strip(m.render()), "レーンと役の意味") {
			t.Fatalf("%s で表が閉じない", closeKey)
		}
	}
}

// どのレーンにも説明がある (空の説明は表で名前だけになる)。
func TestEveryStateHasMeaning(t *testing.T) {
	for _, s := range card.Columns {
		if s.Meaning() == "" {
			t.Fatalf("%s に説明が無い", s.Label())
		}
	}
}

// 表の行は板の中身の幅に収まり、説明は切れずに全部入る (1 桁でも溢れると行末が … になり、続きの語が消える)。
func TestLegendRowsFitPanel(t *testing.T) {
	for _, total := range []int{44, 120} {
		width, inner := legendSize(total)
		box := layout.Panel(" レーンの意味 ", legendRows(inner), width, false, layout.PanelStyle{Border: layout.BorderLight})
		if joined := strings.Join(box, "\n"); strings.Contains(joined, "…") {
			t.Fatalf("画面の幅 %d の表で行が … に切れた:\n%s", total, joined)
		}
		rows := legendRows(inner)
		var text strings.Builder
		for _, r := range rows {
			if w := ansi.StringWidth(r); w > inner {
				t.Fatalf("画面の幅 %d で %d 桁の行 (中身は %d 桁まで): %q", total, w, inner, ansi.Strip(r))
			}
			text.WriteString(strings.TrimPrefix(ansi.Strip(r), "  "))
		}
		for _, s := range card.Columns {
			if !strings.Contains(text.String(), s.Meaning()) {
				t.Fatalf("画面の幅 %d で %s の説明が欠けた", total, s.Label())
			}
		}
	}
}

// 表に役 (プロセス) ごとの仕事が出る。画面より長ければ j で送って全部を読め、題に送りの位置が出る。背の高い画面では送らずに全部が出る。
func TestLegendShowsEveryRole(t *testing.T) {
	m := New(newSpy(), nil)
	m.width, m.height = 120, 30
	press(m, "?")
	first := ansi.Strip(m.render())
	if !strings.Contains(first, "j / k で送る") {
		t.Fatalf("画面より長い表で、送れることを題に出さない:\n%s", first)
	}
	seen := first
	for range 60 {
		press(m, "j")
		seen += ansi.Strip(m.render())
	}
	for _, r := range roleMeanings {
		if !strings.Contains(seen, r[0]) || !strings.Contains(seen, string([]rune(r[1])[:8])) {
			t.Fatalf("表を送っても役 %s の仕事が出ない", r[0])
		}
	}
	press(m, "g")
	if got := ansi.Strip(m.render()); !strings.Contains(got, card.Columns[0].Label()) {
		t.Fatalf("g で表の頭に戻らない:\n%s", got)
	}
	tall := New(newSpy(), nil)
	tall.width, tall.height = 120, 120
	press(tall, "?")
	out := ansi.Strip(tall.render())
	if strings.Contains(out, "j / k で送る") || !strings.Contains(out, roleMeanings[len(roleMeanings)-1][0]) {
		t.Fatalf("背の高い画面で全部を出さない / 送りの案内を出す:\n%s", out)
	}
}
