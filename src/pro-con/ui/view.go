package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 見た目 (列・語・記号) は未確定。issue 415 の段階 1 でサンプルとして見てもらい、合意してから固める
// (decide-layout-in-sample-renderer-first)。表示の文言にはテストを書いていない。

const (
	sgrReset   = "\x1b[0m"
	sgrBold    = "\x1b[1m"
	sgrDim     = "\x1b[2m"
	sgrReverse = "\x1b[7m"
	sgrRed     = "\x1b[31m"
	sgrYellow  = "\x1b[33m"
	minColW    = 14
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *Model) render() string {
	var b strings.Builder
	b.WriteString(sgrBold + "pro-con (mock)" + sgrReset + sgrDim + "  claude は起動しない。表示は模擬データ" + sgrReset + "\n")
	b.WriteString(m.tabBar() + "\n")
	b.WriteString(m.gauge() + "\n\n")
	lines := m.boardLines()
	b.WriteString(strings.Join(lines, "\n") + "\n")
	if m.showDetail {
		b.WriteString("\n" + m.detail())
	}
	b.WriteString("\n")
	if m.mode == modeInput {
		b.WriteString(m.inputLine() + "\n")
	}
	if m.flash != "" {
		b.WriteString(sgrYellow + m.flash + sgrReset + "\n")
	}
	b.WriteString(sgrDim + "←→↑↓ 選択  enter 詳細  a attach  r 回答  o 追加オーダー  b btw  q 終了" + sgrReset)
	return b.String()
}

// tabBar は global と repo のタブ。config の外の repo のカードは global にだけ出るので、その枚数も添える。
func (m *Model) tabBar() string {
	count := map[string]int{}
	for _, c := range m.snap.Cards {
		count[c.Repo]++
	}
	var parts []string
	shown := 0
	for _, t := range m.tabs() {
		label := fmt.Sprintf("global %d", len(m.snap.Cards))
		if t != "" {
			label = fmt.Sprintf("%s %d", t, count[t])
			shown += count[t]
		}
		if t == m.tab {
			label = sgrReverse + " " + label + " " + sgrReset
		} else {
			label = " " + label + " "
		}
		parts = append(parts, label)
	}
	bar := strings.Join(parts, "│")
	if outside := len(m.snap.Cards) - shown; outside > 0 {
		bar += sgrDim + fmt.Sprintf("   (config の外の repo のカード %d 枚は global にだけ出る)", outside) + sgrReset
	}
	return bar + sgrDim + "   tab / shift+tab で切り替え" + sgrReset
}

// gauge は溜まり具合の 1 行 (要件 4)。件数は選んでいるタブの分、PG の数と上限は全体。
func (m *Model) gauge() string {
	counts := map[card.State]int{}
	var oldest time.Duration
	pending, stalled := 0, 0
	for _, c := range m.visible() {
		counts[c.State]++
		if c.State != card.Done && c.State != card.Running {
			if d := m.snap.Now.Sub(c.Since); d > oldest {
				oldest = d
			}
		}
		if c.Ending == card.EndPendingIssue {
			pending++
		}
		if c.Stalled {
			stalled++
		}
	}
	var parts []string
	for _, st := range card.Columns {
		parts = append(parts, fmt.Sprintf("%s %d", st.Label(), counts[st]))
	}
	g := strings.Join(parts, "  ")
	g += fmt.Sprintf("  │ 最古の待ち %s │ PG %d/%d │ daemon %s", fmtDur(oldest), len(m.snap.Consumers), m.snap.Limit,
		fmtDur(m.snap.Now.Sub(m.snap.DaemonTick))+"前")
	if pending > 0 {
		g += " │ " + sgrYellow + fmt.Sprintf("⚠ issue 化待ち %d", pending) + sgrReset
	}
	if stalled > 0 {
		g += " │ " + sgrRed + fmt.Sprintf("🚨 停滞 %d", stalled) + sgrReset
	}
	if n := len(m.snap.Violations); n > 0 {
		g += " │ " + sgrRed + fmt.Sprintf("不変条件の破れ %d", n) + sgrReset
	}
	return g
}

func (m *Model) colWidth() int {
	n := len(card.Columns)
	return max(minColW, (m.width-(n-1))/n)
}

// boardLines はカンバン。1 枚のカードは 2 行 (タイトル行 + バッジ行)。
func (m *Model) boardLines() []string {
	w := m.colWidth()
	cols := m.columns()
	var header []string
	for i, st := range card.Columns {
		header = append(header, sgrBold+fit(fmt.Sprintf("%s (%d)", st.Label(), len(cols[i])), w)+sgrReset)
	}
	out := []string{strings.Join(header, " ")}
	maxRows := 0
	for _, cs := range cols {
		maxRows = max(maxRows, len(cs))
	}
	// 詳細を開いているときは下に場所を空ける
	room := m.height - 8
	if m.showDetail {
		room -= 14
	}
	shown := max(1, min(maxRows, room/2))
	for r := range shown {
		var l1, l2 []string
		for _, cs := range cols {
			if r >= len(cs) {
				l1, l2 = append(l1, fit("", w)), append(l2, fit("", w))
				continue
			}
			if r == shown-1 && len(cs) > shown {
				l1, l2 = append(l1, fit(sgrDim+fmt.Sprintf("… 他 %d 枚", len(cs)-shown+1)+sgrReset, w)), append(l2, fit("", w))
				continue
			}
			t, bdg := m.cardCell(cs[r], w)
			l1, l2 = append(l1, t), append(l2, bdg)
		}
		out = append(out, strings.Join(l1, " "), strings.Join(l2, " "))
	}
	return out
}

func (m *Model) cardCell(c card.Card, w int) (string, string) {
	title := fit(c.ID+" "+c.Title, w)
	badge := fit(m.badge(c), w)
	if c.ID == m.selected {
		return sgrReverse + title + sgrReset, sgrReverse + badge + sgrReset
	}
	if c.State == card.Done {
		return sgrDim + title + sgrReset, sgrDim + badge + sgrReset
	}
	return title, badge
}

// badge は待ちの理由と、issue との紐づき (要件 10)。
func (m *Model) badge(c card.Card) string {
	var parts []string
	switch {
	case c.Stalled:
		parts = append(parts, "🚨停滞")
	case c.Wait.Kind == card.WaitQuestion:
		parts = append(parts, "?質問")
	case c.Wait.Kind == card.WaitPermission:
		parts = append(parts, "?権限")
	case c.Wait.Kind == card.WaitResource:
		parts = append(parts, fmt.Sprintf("…%s %d番目", c.Wait.Resource, c.Wait.Position))
	case c.Wait.Kind == card.WaitQuota:
		parts = append(parts, "…枠待ち")
	}
	if c.State != card.Done {
		parts = append(parts, fmtDur(m.snap.Now.Sub(c.Since)))
	}
	switch {
	case len(c.Issues) > 0:
		var refs []string
		for _, r := range c.Issues {
			refs = append(refs, fmt.Sprintf("#%03d", r.Number))
		}
		parts = append(parts, strings.Join(refs, " "))
	case c.Ending == card.EndPendingIssue:
		parts = append(parts, "⚠issue化待ち")
	case c.Ending != card.EndNone:
		parts = append(parts, "issueなし:"+c.Ending.Label())
	}
	if n := undelivered(c); n > 0 {
		parts = append(parts, fmt.Sprintf("+追加%d未達", n))
	}
	return strings.Join(parts, " ")
}

func undelivered(c card.Card) int {
	n := 0
	for _, o := range c.Orders {
		if !o.Delivered {
			n++
		}
	}
	return n
}

func (m *Model) detail() string {
	c, ok := m.selectedCard()
	if !ok {
		return ""
	}
	var b strings.Builder
	w := m.width
	line := func(s string) { b.WriteString(fit(s, w) + "\n") }
	line(sgrBold + c.ID + "  " + c.Title + sgrReset)
	line(fmt.Sprintf("状態: %s (%s)  担当: %s  repo: %s  session: %s", c.State.Label(), fmtDur(m.snap.Now.Sub(c.Since)), c.Owner, c.Repo, orDash(c.Session)))
	var refs []string
	for _, r := range c.Issues {
		refs = append(refs, r.String()+" ("+r.Status+")")
	}
	link := strings.Join(refs, ", ")
	if link == "" {
		link = "なし"
		if c.Ending != card.EndNone {
			link += " / 終わり方: " + c.Ending.Label()
		}
	}
	line("issue: " + link + "   親: " + orDash(c.ParentID))
	line("依頼の原文: 「" + c.Request + "」")
	if c.Wait.Question != "" {
		line(sgrYellow + "質問: " + c.Wait.Question + sgrReset)
	}
	for _, o := range c.Orders {
		st := "未達"
		if o.Delivered {
			st = "届いた"
		}
		line(fmt.Sprintf("追加オーダー (%s・%s): %s", o.Kind.Label(), st, o.Text))
	}
	line(sgrDim + "履歴" + sgrReset)
	for _, e := range tail(c.History, 4) {
		line("  " + e.At.Format("15:04") + " " + e.Text)
	}
	line(sgrDim + "出力の末尾" + sgrReset)
	for _, l := range tail(c.Log, 3) {
		line("  " + l)
	}
	return b.String()
}

func (m *Model) inputLine() string {
	var label string
	switch m.inputKind {
	case inputAnswer:
		label = m.selected + " へ回答"
	case inputOrder:
		label = fmt.Sprintf("%s へ追加オーダー [%s] (tab で種類を切り替え)", m.selected, m.orderKind.Label())
	case inputBtw:
		label = m.selected + " に btw (PG は止めない)"
	}
	return sgrBold + label + ": " + sgrReset + string(m.input) + "▏"
}

// fit は表示幅 w に切り詰め、足りなければ空白で埋める (全角を含む行を表示幅で揃える)。
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d秒", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d分", int(d/time.Minute))
	}
	return fmt.Sprintf("%d時間%d分", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}
