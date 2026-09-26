package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"

	"pro-con/card"
)

// ? のレーンのタブに全レーンの名前と意味が出て、? / q / esc で閉じる。出している間はカンバンが動かない。
func TestLegendShowsEveryLane(t *testing.T) {
	for _, closeKey := range []string{"?", "q", "esc"} {
		m := New(newSpy(), nil)
		m.width, m.height = 120, 40
		sel := m.selected
		press(m, "?", "tab")
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
		if m.legend || strings.Contains(ansi.Strip(m.render()), strings.TrimSpace(legendTitle)) {
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

// どのタブの行も (タブの行も) 板の中身の幅に収まり、説明は切れずに全部入る (1 桁でも溢れると行末が … になり、続きの語が消える)。
func TestLegendRowsFitPanel(t *testing.T) {
	for _, total := range []int{44, 120} {
		width, inner := legendSize(total)
		var text strings.Builder
		for _, tab := range legendTabs {
			rows := append([]string{legendTabBar(tab, inner)}, legendRows(tab, inner)...)
			box := layout.Panel(legendTitle, rows, width, false, layout.PanelStyle{Border: layout.BorderLight})
			if joined := strings.Join(box, "\n"); strings.Contains(joined, "…") {
				t.Fatalf("画面の幅 %d の %s のタブで行が … に切れた:\n%s", total, tab.label(), joined)
			}
			for _, r := range rows {
				if w := ansi.StringWidth(r); w > inner {
					t.Fatalf("画面の幅 %d の %s のタブで %d 桁の行 (中身は %d 桁まで): %q", total, tab.label(), w, inner, ansi.Strip(r))
				}
				text.WriteString(strings.ReplaceAll(ansi.Strip(r), " ", "")) // 字下げと折り返しの位置の空白は比べない
			}
		}
		var want []string
		for _, s := range card.Columns {
			want = append(want, s.Meaning())
		}
		for _, st := range append(slices.Clone(card.MainFlow), card.FlowDetours...) {
			want = append(want, st.Who+": "+st.How)
		}
		for _, w := range append(want, card.AfterDone...) {
			if !strings.Contains(text.String(), strings.ReplaceAll(w, " ", "")) {
				t.Fatalf("画面の幅 %d で説明が欠けた: %s", total, w)
			}
		}
	}
}

// ? は流れのタブで開き、tab で次・shift+tab で前のタブへ (端で回る)。閉じて開き直すと流れのタブの頭から (issue 515)。
func TestLegendTabs(t *testing.T) {
	m := New(newSpy(), nil)
	m.width, m.height = 120, 40
	press(m, "?")
	tabOf := func() string {
		out := ansi.Strip(m.render())
		for _, tab := range legendTabs {
			if tab == m.legendTab && !strings.Contains(out, tab.label()) {
				t.Fatalf("%s のタブの名前が出ない:\n%s", tab.label(), out)
			}
		}
		return m.legendTab.label()
	}
	if got := tabOf(); got != "流れ" || !strings.Contains(ansi.Strip(m.render()), "通常の流れ") {
		t.Fatalf("? で流れのタブが開かない (%s):\n%s", got, ansi.Strip(m.render()))
	}
	var seen []string
	for range legendTabs {
		press(m, "tab")
		seen = append(seen, tabOf())
	}
	if want := []string{"レーン", "印とポイント", "役", "流れ"}; !slices.Equal(seen, want) {
		t.Fatalf("tab の順: %v (期待 %v)", seen, want)
	}
	press(m, "shift+tab")
	if got := tabOf(); got != "役" {
		t.Fatalf("shift+tab で前のタブ (役) へ戻らない: %s", got)
	}
	if !m.legend {
		t.Fatal("tab で表が閉じた")
	}
	press(m, "j", "q", "?")
	if m.legendTab != legendFlow || m.legendOff != 0 {
		t.Fatalf("開き直しで流れのタブの頭に戻らない: %s / 送り %d", m.legendTab.label(), m.legendOff)
	}
}

// 表に役 (プロセス) ごとの仕事が出る。画面より長ければ j で送って全部を読め、題に送りの位置が出る。背の高い画面では送らずに全部が出る。
func TestLegendShowsEveryRole(t *testing.T) {
	m := New(newSpy(), nil)
	m.width, m.height = 120, 20
	press(m, "?", "tab", "tab", "tab")
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
	if got := ansi.Strip(m.render()); !strings.Contains(got, roleMeanings[0][0]) {
		t.Fatalf("g で表の頭に戻らない:\n%s", got)
	}
	tall := New(newSpy(), nil)
	tall.width, tall.height = 120, 120
	press(tall, "?", "shift+tab")
	out := ansi.Strip(tall.render())
	if strings.Contains(out, "j / k で送る") || !strings.Contains(out, roleMeanings[len(roleMeanings)-1][0]) {
		t.Fatalf("背の高い画面で全部を出さない / 送りの案内を出す:\n%s", out)
	}
}
