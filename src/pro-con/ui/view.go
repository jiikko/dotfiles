package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 見た目は style.go の冒頭の合意 (B「枠」) に従う。表示の文言と配置にはテストを書いていない (つなぎ込みだけを検査する)。

const (
	sgrReset = "\x1b[0m"
	sgrBold  = "\x1b[1m"
	sgrDim   = "\x1b[2m"
	// 下線の開始と終了 (終了だけを戻すので、帯の背景色や太字を消さない)
	sgrUnderline   = "\x1b[4m"
	sgrNoUnderline = "\x1b[24m"
	sgrRed         = "\x1b[38;5;196m"
	sgrYellow      = "\x1b[38;5;214m"
	sgrCyan        = "\x1b[38;5;51m"
	minColW        = 14
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *Model) render() string {
	w := m.width
	title := sgrBold + fg(202) + " pro-con" + sgrFgReset + sgrDim + "  mock: claude は起動しない。表示は模擬データ" + sgrReset
	out := []string{title, m.tabBar(), m.gauge(), fg(240) + strings.Repeat("─", w) + sgrReset}
	out = append(out, m.overlayMoves(m.boardLines())...)
	if m.showDetail {
		out = append(out, "")
		out = append(out, m.detailBlock()...)
	}
	if m.showSessions {
		out = append(out, "")
		out = append(out, m.sessionsBlock()...)
	}
	out = append(out, "")
	if m.mode == modeInput {
		out = append(out, m.inputLine())
	}
	if m.flash != "" {
		out = append(out, sgrCyan+" "+m.flash+sgrReset)
	}
	help := " ←→↑↓ 選択  tab repo  n 新しい依頼  y コピー  s claude 一覧  enter 詳細  a attach  r 回答  o 追加オーダー  b btw  q 終了"
	out = append(out, sgrDim+help+sgrReset)
	return strings.Join(out, "\n")
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
			label = sgrSelected + " " + label + " " + sgrReset
		} else {
			label = " " + label + " "
		}
		parts = append(parts, label)
	}
	bar := " " + strings.Join(parts, fg(240)+"│"+sgrFgReset)
	if outside := len(m.snap.Cards) - shown; outside > 0 {
		bar += sgrDim + fmt.Sprintf("   config の外の repo のカード %d 枚は global にだけ出る", outside) + sgrReset
	}
	return bar
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
		parts = append(parts, fmt.Sprintf("%s%s %d%s", fg(stateColor(st)), st.Label(), counts[st], sgrFgReset))
	}
	sep := fg(240) + " │ " + sgrFgReset
	g := " " + strings.Join(parts, "  ") + sep + fmt.Sprintf("最古の待ち %s", fmtDur(oldest)) + sep +
		fmt.Sprintf("PG %d/%d", len(m.snap.Consumers), m.snap.Limit) + sep +
		fmt.Sprintf("daemon %s前", fmtDur(m.snap.Now.Sub(m.snap.DaemonTick))) + sep + m.sessionsSummary()
	if pending > 0 {
		g += sep + sgrYellow + fmt.Sprintf("⚠ issue 化待ち %d", pending) + sgrFgReset
	}
	if stalled > 0 {
		g += sep + sgrRed + sgrBold + fmt.Sprintf("🚨 停滞 %d", stalled) + sgrFgReset
	}
	if n := len(m.snap.Violations); n > 0 {
		g += sep + sgrRed + fmt.Sprintf("不変条件の破れ %d", n) + sgrFgReset
	}
	return g
}

func (m *Model) colWidth() int {
	n := len(card.Columns)
	return max(minColW, (m.width-(n-1)*len(colSep))/n)
}

// colSep は列の枠と枠のあいだ。
const colSep = " "

// boardLines はカンバン。列ごとに枠つきの行を組んでから横に並べる。
func (m *Model) boardLines() []string {
	w := m.colWidth()
	cols := m.columns()
	selCol, _, _ := m.position()

	const headLines = 2 // 上枠 + 下枠
	room := m.height - 9 - headLines
	if m.showDetail {
		room -= 15
	}
	if m.showSessions {
		room -= len(m.sessions) + 5 // 上下の枠・見出し・エラー行・空行
	}
	// 枠の高さは画面の残りで固定する (枚数に合わせると、ある列の枚数が変わった瞬間に全部の列の枠が伸び縮みする)
	shown := max(1, room/perCardLines)

	blocks := make([][]string, len(cols))
	for i, s := range card.Columns {
		blocks[i] = m.columnBlock(i, s, cols[i], w, shown, i == selCol)
	}
	height := 0
	for _, b := range blocks {
		height = max(height, len(b))
	}
	var out []string
	for r := range height {
		var row []string
		for _, b := range blocks {
			if r < len(b) {
				row = append(row, b[r])
				continue
			}
			row = append(row, fit("", w)) // この列は他より短い (カードが少ない)
		}
		out = append(out, strings.Join(row, colSep))
	}
	return out
}

// columnBorder は列の枠の色。選択中のカードがある列だけ現在地色にする。
func columnBorder(focused bool) string {
	if focused {
		return fg(202)
	}
	return fg(240)
}

// columnBlock は 1 列分の行 (上枠に見出し + カード + 下枠)。列の高さは shown で揃える。
func (m *Model) columnBlock(col int, s card.State, cs []card.Card, w, shown int, focused bool) []string {
	label := fmt.Sprintf("%s (%d)", s.Label(), len(cs))
	border := columnBorder(focused)
	inner := w - 2
	out := []string{boxTop(border, fg(stateColor(s))+sgrBold+label+sgrReset, w)}
	cells := m.columnCells(col, cs, inner)
	var body []string
	for r := range min(shown, len(cells)) {
		if r == shown-1 && len(cells) > shown {
			body = append(body, fit(sgrDim+fmt.Sprintf("… 他 %d 枚", len(cells)-shown+1)+sgrReset, inner))
			break
		}
		body = append(body, cells[r][0], cells[r][1])
	}
	for len(body) < shown*perCardLines {
		body = append(body, "")
	}
	for _, l := range body {
		out = append(out, boxLine(border, l, w))
	}
	return append(out, boxBottom(border, w))
}

const perCardLines = 2

// columnCells は列の中身を 1 枚 2 行のセルで並べる。移動中のカード (motion.go) は、移動先では空けて待ち、
// 移動元には点線の枠を残す (どちらも着地まで。周りのカードが途中で詰まってずれないように)。
func (m *Model) columnCells(col int, cs []card.Card, inner int) [][2]string {
	var cells [][2]string
	for _, c := range cs {
		if mv, ok := m.moves[c.ID]; ok && mv.to.col == col {
			cells = append(cells, [2]string{fit("", inner), fit("", inner)})
			continue
		}
		t, b := m.cardCell(c, inner)
		cells = append(cells, [2]string{t, b})
	}
	for _, mv := range m.moves {
		if mv.from.col != col {
			continue
		}
		g := ghostLines(inner)
		at := min(mv.from.row, len(cells))
		cells = append(cells[:at], append([][2]string{{g[0], g[1]}}, cells[at:]...)...)
	}
	return cells
}

// cardCell はカード 1 枚 (2 行)。地の色はカードごとに固有 (cardColor)。列を移っても同じ色なので目で追える。
// 選択中は左右の両端に現在地色の ▌ ▐ を立て、タイトルを太字 + 下線にする (2026-09-24 に 3 案から a で合意)。
// 地は塗り替えない (カード固有の色が消えると、列を移ったときに目で追えなくなる)。完了は文字を dim にする。
func (m *Model) cardCell(c card.Card, w int) (string, string) {
	base := bg(cardColor(c.ID)) + fg(252)
	title := c.ID + " " + c.Title
	badge := m.badgeColored(c)
	if c.ID == m.selected {
		l, r := fg(202)+"▌"+fg(231), base+fg(202)+"▐"+sgrReset
		return paint(base, l+sgrBold+sgrUnderline+title+sgrNoUnderline, w-1) + r, paint(base, l+sgrBold+badge, w-1) + r
	}
	if c.State == card.Done {
		title, badge = sgrDim+title, sgrDim+m.badge(c)
	}
	return paint(base, " "+title, w), paint(base, " "+badge, w)
}

// badgeColored は badge の待ちの理由に色を付ける (停滞 = 危険 / 質問 = 要対応 / issue 化待ち = 要対応)。
func (m *Model) badgeColored(c card.Card) string {
	b := m.badge(c)
	switch {
	case c.Stalled:
		return sgrRed + sgrBold + b + sgrFgReset
	case c.Wait.Kind.NeedsAnswer(), c.Ending == card.EndPendingIssue:
		return sgrYellow + b + sgrFgReset
	}
	return sgrDim + b + sgrReset
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

// detailBlock は詳細欄。現在地色の枠で囲む (いま操作の対象になっているカード)。
func (m *Model) detailBlock() []string {
	c, ok := m.selectedCard()
	if !ok {
		return nil
	}
	head, body := m.detailLines(c)
	w := m.width
	out := []string{boxTop(fg(202), sgrBold+head+sgrReset, w)}
	for _, l := range body {
		out = append(out, boxLine(fg(202), " "+l, w))
	}
	return append(out, boxBottom(fg(202), w))
}

func (m *Model) detailLines(c card.Card) (string, []string) {
	head := c.ID + "  " + c.Title
	var l []string
	l = append(l, fmt.Sprintf("状態: %s%s%s (%s)  担当: %s  repo: %s  session: %s", fg(stateColor(c.State)), c.State.Label(), sgrFgReset,
		fmtDur(m.snap.Now.Sub(c.Since)), c.Owner, c.Repo, orDash(c.Session)))
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
	l = append(l, "issue: "+link+"   親: "+orDash(c.ParentID))
	l = append(l, "依頼の原文: 「"+c.Request+"」")
	if c.Prompt != "" {
		l = append(l, sgrDim+"PM に渡した指示: "+strings.ReplaceAll(c.Prompt, "\n", " ")+sgrReset)
	}
	if c.Wait.Question != "" {
		l = append(l, sgrYellow+"質問: "+c.Wait.Question+sgrFgReset)
	}
	for _, o := range c.Orders {
		st := "未達"
		if o.Delivered {
			st = "届いた"
		}
		l = append(l, fmt.Sprintf("追加オーダー (%s・%s): %s", o.Kind.Label(), st, o.Text))
	}
	l = append(l, sgrDim+"履歴"+sgrReset)
	for _, e := range tail(c.History, 4) {
		l = append(l, "  "+e.At.Format("15:04")+" "+e.Text)
	}
	l = append(l, sgrDim+"出力の末尾"+sgrReset)
	for _, s := range tail(c.Log, 3) {
		l = append(l, "  "+s)
	}
	return head, l
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
	case inputNew:
		label = "新しい依頼 (global: repo 未指定。PM が判断する)"
		if r := m.tabRepo(); r.Name != "" {
			label = "新しい依頼 (スコープ: " + r.Name + " の中だけ)"
		}
	}
	return " " + sgrBold + fg(202) + label + ": " + sgrReset + string(m.input) + "▏"
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
