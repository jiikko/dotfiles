package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"

	"pro-con/backend"
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
	v.Cursor = m.caret()
	return v
}

// caret は入力欄のキャレットに置く端末のカーソル。入力欄が無ければ nil (カーソルを隠す)。
// 🚨 IME は変換中の文字を端末のカーソルの位置に出す。カーソルを置かないと、描画の差分を書き終えた位置 (毎回変わる) に出て、
// 日本語の変換中に入力欄から外れる。位置は render と同じ行の並び (ヘッダ + 領域 + PG の一覧 + 入力欄) から数える
func (m *Model) caret() *tea.Cursor {
	if m.mode != modeInput || m.stopping {
		return nil
	}
	y := m.inputRow // render が数えた入力欄の行
	head, before, _ := m.inputParts()
	x := min(ansi.StringWidth(head+before), m.width-1) // 幅は表示のセル数 (全角は 2)
	if y < 0 || y >= m.height || x < 0 {
		return nil
	}
	c := tea.NewCursor(x, y)
	c.Shape = tea.CursorBar
	return c
}

// headerRows はタイトル・タブ・ゲージ・罫線の行数。
const headerRows = 4

func (m *Model) render() string {
	w := m.width
	title := sgrBold + fg(202) + " pro-con" + sgrFgReset + " (producer-consumer)" + sgrReset
	if d, ok := m.be.(backend.Describer); ok {
		title += sgrDim + "  " + d.Describe() + sgrReset
	}
	header := []string{title, m.tabBar(), m.gauge(), fg(240) + strings.Repeat("─", w) + sgrReset}
	var region []string
	if m.picker.open {
		region = m.pickerBlock()
	} else {
		region = m.overlayMoves(m.overlayCursor(m.boardLines()))
	}
	// PG の一覧・入力欄・案内は画面の下端へ吸着させ、ボードとの間を空行で埋める。
	// カードの詳細はこの領域 (ヘッダと下端の群のあいだ) に右から重ねる
	foot := m.footGroup()
	region = layout.PadTo(region, max(len(region)+1, m.height-len(header)-len(foot)))
	m.inputRow = len(header) + len(region) + len(foot) - len(m.footLines()) // 入力欄は最下段の群の先頭 (caret が使う)
	region = m.dimWhileTyping(m.overlayDrawer(region))
	return strings.Join(m.overlayQuit(m.overlayLegend(append(append(header, region...), foot...))), "\n")
}

// footGroup は下端に吸着させる群 (PG の一覧 + 最下段)。
func (m *Model) footGroup() []string {
	foot := m.panelLines(panelPG, m.pgBlock)
	return append(foot, m.footLines()...)
}

// footLines は画面の最下段の群 (入力欄・確認・sticky・flash・案内) を返す。
func (m *Model) footLines() []string {
	var out []string
	switch m.mode {
	case modeInput:
		out = append(out, m.inputLine())
	case modeConfirm:
		out = append(out, " "+sgrBold+sgrYellow+m.confirmText+sgrReset)
	case modeBoard:
	}
	if m.sticky != "" {
		out = append(out, sgrYellow+" "+m.sticky+sgrReset)
	}
	if m.flash != "" {
		out = append(out, sgrCyan+" "+m.flash+sgrReset)
	}
	return append(out, hintLine(m.hints(), m.width)+sgrReset)
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
		fmt.Sprintf("dispatcher %s前", fmtDur(m.snap.Now.Sub(m.snap.DispatcherTick)))
	if u := m.upgradeSummary(); u != "" {
		g += sep + u
	}
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
	selCol := m.col

	shown := m.shownCards()

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

// shownCards は 1 列に見せるカードの枚数。枠の高さは画面の残りで固定する
// (枚数に合わせると、ある列の枚数が変わった瞬間に全部の列の枠が伸び縮みする)。
func (m *Model) shownCards() int {
	const headLines = 2 // 上枠 + 下枠
	room := m.height - 9 - headLines
	if m.reserved(panelPG) {
		room -= len(m.snap.Consumers) + 5 // 上下の枠・見出し・ほかの session の行・空行
	}
	return max(1, (room-cardGap)/perCardLines) // 末尾の空き 1 行を引いてから割る
}

// columnBlock は 1 列分の行 (上枠に見出し + カード + 下枠)。列の高さは shown で揃える。
func (m *Model) columnBlock(col int, s card.State, cs []card.Card, w, shown int, focused bool) []string {
	// 先頭の数字は 1〜6 でそのレーンへ飛ぶキー。狭いときは名前を削り、キーと枚数は残す (boxTop は後ろから切る)
	head, tail := fmt.Sprintf("%d ", col+1), fmt.Sprintf(" (%d)", len(cs))
	if focused {
		head = "▶ " + head
	} else {
		head = "  " + head // ▶ の幅を空けておく (レーンを移るたびに名前が 2 桁ずれないように)
	}
	border := fg(m.laneColor(col))
	inner := w - 2
	name := ansi.Truncate(s.Label(), max(inner-3-ansi.StringWidth(head+tail), 1), "…")
	label := head + name + tail
	out := []string{boxTop(border, fg(stateColor(s))+sgrBold+label+sgrReset, w)}
	cells := m.columnCells(col, cs, inner)
	// カードの上下に 1 行ずつ空ける (空行・カード・空行・…・空行)。選択の枠と移動中のカードの枠はこの空行の上に描くので、
	// 隣のカードを隠さない (2026-09-24 のユーザー提案。1 列に入る枚数は 2 行詰めの約 2/3 になる)
	var body []string
	for r := range min(shown, len(cells)) {
		body = append(body, "")
		if r == shown-1 && len(cells) > shown {
			body = append(body, fit(sgrDim+fmt.Sprintf("… 他 %d 枚", len(cells)-shown+1)+sgrReset, inner))
			break
		}
		body = append(body, cells[r][0], cells[r][1])
	}
	for len(body) < shown*perCardLines+cardGap {
		body = append(body, "")
	}
	for _, l := range body {
		out = append(out, boxLine(border, l, w))
	}
	return append(out, boxBottom(border, w))
}

const (
	cardLines    = 2                   // カード 1 枚の行数
	cardGap      = 1                   // カードの上下の空き (枠を描く行)
	perCardLines = cardLines + cardGap // 1 枚あたりの縦の送り
)

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
// 選択中はタイトルを太字 + 下線にする。選択の目印は周りの枠 (cursor.go)。
// 地は塗り替えない (カード固有の色が消えると、列を移ったときに目で追えなくなる)。完了は文字を dim にする。
func (m *Model) cardCell(c card.Card, w int) (string, string) {
	base := bg(cardColor(c.ID)) + fg(252)
	title := c.ID + issueTag(c) + " " + c.Title // issue に紐づくカードは 1 行目に番号を出す (バッジ行は待ちの理由と時間)
	badge := m.badgeColored(c)
	if c.ID == m.selected { // 目印は周りの枠 (cursor.go)。中は太字と下線だけ
		return paint(base, " "+fg(231)+sgrBold+sgrUnderline+title+sgrNoUnderline, w), paint(base, " "+fg(231)+sgrBold+badge, w)
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
	case c.Wait.Kind == card.WaitCrashed:
		parts = append(parts, "🚨落ちた")
	case c.Wait.Kind == card.WaitResource:
		parts = append(parts, fmt.Sprintf("…%s %d番目", c.Wait.Resource, c.Wait.Position))
	case c.Wait.Kind == card.WaitQuota:
		parts = append(parts, "…枠待ち")
	}
	if e := c.Exec; e.Active() {
		cmd := e.Command
		if e.Resource != "" {
			cmd = e.Resource + ": " + cmd
		}
		parts = append(parts, "▶ "+cmd+" "+fmtDur(m.snap.Now.Sub(e.Since)))
	} else if c.State != card.Done {
		parts = append(parts, fmtDur(m.snap.Now.Sub(c.Since)))
	}
	switch {
	case len(c.Issues) > 0: // 番号は 1 行目 (issueTag) に出している
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

func (m *Model) inputHead() string {
	var label string
	switch m.inputKind {
	case inputAnswer:
		label = m.selected + " へ回答"
	case inputOrder:
		label = fmt.Sprintf("%s へ追加オーダー [%s] (tab で種類を切り替え)", m.selected, m.orderKind.Label())
	case inputBtw:
		label = m.selected + " に btw (PG は止めない)"
	case inputIssue:
		label = "これをやる"
		if t := m.picker.target; t != nil {
			label = fmt.Sprintf("#%03d %s をやる", t.Number, t.Title)
			if t.Epic != "" {
				label = fmt.Sprintf("epic %s (未完了の子 %d) をやる", t.Epic, len(t.Children))
			}
		}
		label += " — 補足があれば (空のまま enter でよい)"
	case inputQuit:
		label = m.quitLabel()
	case inputNew:
		label = "新しい依頼 (global: repo 未指定。PM が判断する)"
		if r := m.tabRepo(); r.Name != "" {
			label = "新しい依頼 (スコープ: " + r.Name + " の中だけ)"
		}
	}
	return " " + sgrBold + fg(202) + label + ": " + sgrReset + fg(231)
}

// inputParts は入力欄の見出し・キャレットの前・後 (キャレットは caret が端末のカーソルで出す)。
func (m *Model) inputParts() (head, before, after string) {
	s, cur := []rune(m.line.String()), m.line.Cursor()
	return m.inputHead(), string(s[:cur]), string(s[cur:])
}

func (m *Model) inputLine() string {
	head, before, after := m.inputParts()
	// 入力欄に居ることが一目で分かるよう、行の全幅に地の色を敷く (カンバンは dimWhileTyping で沈める)
	return paint(bg(236), head+before+after, m.width)
}

// dimWhileTyping は入力欄・y/N 確認を出している間、カンバンの領域を色を抜いた暗い灰色で描く。
// キーは入力に取られ、カードは動かせない。その状態を画面の側で見せる (2026-09-24 の要望)。
func (m *Model) dimWhileTyping(region []string) []string {
	if m.mode == modeBoard {
		return region
	}
	out := make([]string, len(region))
	for i, l := range region {
		out[i] = fg(239) + ansi.Strip(l) + sgrReset
	}
	return out
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

// hints は最下行の案内。**今の状態で押して効くキーだけ**を出す (入力中にボードの案内を残すと、
// 載せた文字が全部入力に化ける。docs/glogx-ui-guide.md §5)。最後の項目が抜ける手段。
func (m *Model) hints() []string {
	if m.stopping {
		return []string{"ctrl+c 待たずに閉じる"}
	}
	if m.legend {
		return []string{"? / q / esc 閉じる"}
	}
	switch m.mode {
	case modeInput:
		h := []string{"enter 送信", "ctrl+h 1 文字消す", "ctrl+w 1 語消す", "ctrl+u 前を消す", "ctrl+k 後ろを消す", "ctrl+a / ctrl+e 先頭 / 末尾"}
		if m.inputKind == inputOrder {
			h = append([]string{"tab 種類"}, h...)
		}
		return append(h, "esc 取り消し")
	case modeConfirm:
		return []string{"y / enter 実行", "他のキー 取り消し"}
	case modeBoard:
	}
	if m.picker.open {
		return []string{"j / k 選択", "enter これをやる", "i / q / esc 閉じる"}
	}
	c, has := m.selectedCard()

	// カードへの操作は、選んでいるカードで効くかどうかを色で出す (効かないものは暗く。押すと理由が flash に出る)
	cardOps := []string{
		avail("a attach", has && c.Session != ""), // backend は session の無いカードを ErrNoSession で拒否する
		avail("r 回答", has && c.Answerable() && m.accepts(backend.OpAnswer)),
		avail("+ 追加オーダー", has && m.accepts(backend.OpOrder)), // 完了のカードでも「別件」は受け付ける
		avail("w btw", has && m.accepts(backend.OpBtw)),
		avail("e issue を開く", has && len(c.Issues) > 0), // md が実在するかは押したときに探す (描画のたびには探さない)
		avail("y パス", has && len(c.Issues) > 0),
		avail("Y 内容", has),
	}
	if m.showDetail {
		return append(append([]string{"j / k スクロール", "J / K 隣のカード"}, cardOps...), "q / esc 閉じる")
	}
	back := "Q 終了"
	if m.showSessions {
		back = "q / esc 閉じる"
	}
	h := append([]string{"hjkl 選択", "tab repo", avail("n 新しい依頼", m.accepts(backend.OpNew)), avail("i issue から", m.accepts(backend.OpNew)), avail("enter 詳細", has)}, cardOps...)
	return append(h, "s PG 一覧", avail("x 完了を片付け", m.doneInTab() > 0 && m.accepts(backend.OpClear)), "? レーンの意味", back)
}

// avail は案内の 1 項目を、今押して効くなら明るく、効かないなら暗く出す。
func avail(text string, ok bool) string {
	if ok {
		return text
	}
	return fg(239) + text + sgrFgReset
}

// hintLine は案内を幅 w に収める。入らなければ後ろから落とすが、最後の項目 (抜ける手段) は必ず残す (§5)。
func hintLine(items []string, w int) string {
	last := items[len(items)-1]
	rest := items[:len(items)-1]
	for {
		line := " " + strings.Join(append(append([]string{}, rest...), last), "  ")
		if ansi.StringWidth(line) <= w || len(rest) == 0 {
			return line
		}
		rest = rest[:len(rest)-1]
	}
}

// accepts は backend が操作 op を受けるか (backend.Accepter を持たない backend は全部受ける)。
func (m *Model) accepts(op backend.Op) bool {
	a, ok := m.be.(backend.Accepter)
	return !ok || a.Accepts(op)
}
