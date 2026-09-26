package ui

// カードの詳細は右から滑り込む引き出し (tuikit/layout.ComposeDrawer)。カンバンの上に重ね、左にカンバンの端を残す
// (どのレーンのどこから開いたかが画面から消えない)。glogx の issues の本文と同じ形で、開いている間は移動のキーが
// 本文のスクロールに効き、J / K で開いたまま隣のカードへ送る (docs/glogx-ui-guide.md §3 / §6)。

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/anim"
	"tuikit/layout"
	"tuikit/listnav"

	"pro-con/card"
)

// glogx の issues の引き出しと同じ寸法と所要 (glogx/issues_drawer.go。変えるなら両方の見え方を揃えて決める)。
var drawerGeometry = layout.DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}

// worktreePaint は worktree の節の色 (見本で決めた形。issue 508): hash は黄、時刻と「ほか」は暗く、index に入った変更は緑・作業ツリーだけの変更は赤。
var worktreePaint = card.Paint{Hash: sgrYellow, Dim: sgrDim, Staged: fg(46), Unstaged: sgrRed, Reset: sgrReset}

const (
	drawerDuration = 112 * time.Millisecond
	drawerHeadRows = 2 // 見出しと罫線。本文だけをスクロールする
	glideFrames    = 6 // 半ページの滑走 (frameInterval 33ms × 6 ≒ 200ms)
)

// openDrawer は選択中のカードの詳細を開く。本文は先頭から。
func (m *Model) openDrawer() {
	if _, ok := m.selectedCard(); !ok {
		return
	}
	m.showDetail = true
	m.drawerCard = m.selected
	m.pager.Reset()
	m.act = activityView{loading: m.act.loading} // 前に開いたときの活動を出さない (読み直すまで「読んでいる…」)
	m.drawer.Open(m.now(), drawerDuration)
}

// closeDrawer は閉じ始める。🚨 閉じる途中も中身 (drawerCard) を残す: 逆再生のあいだ本文が見えている必要がある。
// 捨てるのは閉じ切ってから (settleDrawer)。
func (m *Model) closeDrawer() {
	m.showDetail = false
	m.drawer.Close(m.now(), drawerDuration)
}

func (m *Model) settleDrawer(now time.Time) {
	if m.drawer.Settle(now) {
		m.drawerCard = ""
	}
}

// handleDrawerKey は詳細を開いている間のキー。捌いたら handled=true。カードへの操作 (a / r / + / ? / y / Y / e / o) と
// 画面全体の操作 (ctrl+c / ctrl+r / s / c) はボードへ回す。それ以外 (レーンの移動・タブ・新しい依頼 等) は飲み込む:
// 詳細の下でカンバンの選択が動くと、開いているカードと操作の対象が食い違う。
func (m *Model) handleDrawerKey(k string) (cmd tea.Cmd, handled bool) {
	switch k {
	case "enter", "h", "left":
		m.closeDrawer()
		return m.startFrames(), true
	case "q", "esc":
		m.closeTop()
		return m.startFrames(), true
	case "D": // 取り込む先との差分の板 (issue 508。小文字の d はカードの削除なので大文字。docs/glogx-ui-guide.md の pro-con の表)
		return m.openDiff(), true
	case "J":
		m.stepCard(1)
		return nil, true
	case "K":
		m.stepCard(-1)
		return nil, true
	case "a", "r", "+", "w", "?", "y", "Y", "e", "o", "d", "ctrl+c", "ctrl+r", "s", "c":
		return nil, false
	}
	if mo := listnav.MotionOf(k); mo != listnav.None {
		body := m.drawerBody()
		m.pager.Move(mo, len(body), m.drawerBodyRows(), glideFrames)
		return m.startFrames(), true
	}
	return nil, true
}

// stepCard は詳細を開いたまま、同じレーンの隣のカードへ送る。本文は差し替えて先頭へ戻す。端では止めて知らせる (巻かない)。
func (m *Model) stepCard(delta int) {
	col, row, ok := m.position()
	if !ok {
		return
	}
	cs := m.columns()[col]
	next := row + delta
	if next < 0 || next >= len(cs) {
		if delta > 0 {
			m.info("これがこのレーンの最後のカードです")
		} else {
			m.info("これがこのレーンの最初のカードです")
		}
		return
	}
	m.selected = cs[next].ID
	m.drawerCard = m.selected
	m.pager.Reset()
}

// dropVanishedDrawer は開いているカードが Snapshot から消えたら (片付け等) 引き出しを即座に閉じる。
func (m *Model) dropVanishedDrawer() {
	if m.drawerCard == "" {
		return
	}
	if _, ok := m.drawerCardData(); !ok {
		m.showDetail, m.drawerCard = false, ""
		m.diff = diffView{} // 差分の板もそのカードのもの
		m.drawer = anim.Transition{}
	}
}

// drawerShown は引き出しが画面に入っているか (開いている・開閉の途中)。
func (m *Model) drawerShown() bool { return m.drawerCard != "" }

// drawerCardData は引き出しに出しているカード。片付け等で消えていたら false。
func (m *Model) drawerCardData() (card.Card, bool) {
	for _, c := range m.snap.Cards {
		if c.ID == m.drawerCard {
			return c, true
		}
	}
	return card.Card{}, false
}

// drawerRegionRows は引き出しを重ねる領域 (ヘッダと下端の群のあいだ) の行数。
func (m *Model) drawerRegionRows() int { return max(m.height-headerRows-len(m.footGroup()), 1) }

func (m *Model) drawerBodyRows() int { return max(m.drawerRegionRows()-drawerHeadRows, 1) }

// drawerTextWidth は本文を組む幅 (区切り線 1 桁・左の余白 1 桁・スクロールバーを引いた残り)。
func (m *Model) drawerTextWidth() int {
	return max(drawerGeometry.Target(m.width)-2-layout.ScrollbarWidth, 10)
}

// drawerBody は本文の全行 (開ききった幅で折り返し済み)。履歴と出力 (活動を読める backend では活動) は切り出さずに全部出す。
func (m *Model) drawerBody() []string {
	c, ok := m.drawerCardData()
	if !ok {
		return nil
	}
	w := m.drawerTextWidth()
	var out []string
	// add は素の文字列を折り返し、色を各行へ掛け直す (折り返した先の行で色が抜けないように)
	add := func(style, text string) {
		for _, l := range strings.Split(ansi.Hardwrap(text, w, true), "\n") {
			if style != "" {
				l = style + l + sgrReset
			}
			out = append(out, l)
		}
	}
	add("", fmt.Sprintf("状態: %s (%s)  担当: %s  repo: %s  session: %s", c.State.Label(), fmtDur(m.snap.Now.Sub(c.Since)),
		m.assignee(c), c.Repo, orDash(c.Session)))
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
	add("", "issue: "+link+"   親: "+orDash(c.ParentID))
	if c.Purpose == card.ForQuestion {
		add("", "種類: "+card.QuestionPurposeText)
	}
	if pts := card.PointsLabel(c); pts != "" {
		add("", "見積もり: "+pts+" (PM が付けた)")
	}
	add("", "依頼の原文: 「"+c.Request+"」")
	if c.Prompt != "" {
		add(sgrDim, "PG への指示: "+c.Prompt)
	}
	if len(c.After) > 0 {
		add("", "順番: "+strings.Join(c.After, ", ")+" の後 (PM が付けた。完了するまで起動しない)")
	}
	if w := c.WaitingOn(m.snap.Cards, m.snap.Roles); w != "" { // 何を待っているか (issue 469。誰の番かは 452 の Turn を読む)
		style := ""
		if m.humansTurn(c) {
			style = sgrYellow
		}
		add(style, "今の待ち: "+w)
	}
	if c.Wait.Question != "" {
		add(sgrYellow, "質問: "+c.Wait.Question)
	}
	for _, o := range c.Orders {
		st := "未達"
		if o.Delivered {
			st = "届いた"
		}
		add("", fmt.Sprintf("追加オーダー (%s・%s): %s", o.Kind.Label(), st, o.Text))
	}
	if ls := c.WorktreeLines(m.snap.Now, fmtDur, worktreePaint, sgrDim+"  (D で開く)"+sgrReset); len(ls) > 0 { // PG の worktree の場所と git (issue 508)
		out = append(out, "")
		add(sgrDim, card.WorktreeHead(c, m.snap.Now, fmtDur))
		for _, l := range ls {
			out = append(out, hangWrap("  "+l, w)...)
		}
	}
	if ls := c.ProgressLines(m.snap.Now, fmtDur); len(ls) > 0 { // どこまで進んだか (issue 469。dispatcher と見張りが集めたもの)
		out = append(out, "")
		add(sgrDim, card.ProgressHead(c, m.snap.Now, fmtDur))
		for _, l := range ls {
			add("", "  "+l)
		}
	}
	if ls := c.DoingLines(m.snap.Now, fmtDur); len(ls) > 0 { // PG が今走らせているもの (issue 473)
		out = append(out, "")
		add(sgrDim, card.DoingHead(c, m.snap.Now, fmtDur))
		for _, l := range ls {
			add("", "  "+l)
		}
	}
	if len(c.Attachments) > 0 {
		out = append(out, "")
		add(sgrDim, "添付")
		out = append(out, m.attachmentLines(c, w)...)
	}
	out = append(out, "")
	add(sgrDim, "履歴")
	for _, e := range c.History {
		line := "  " + e.At.Local().Format("15:04") + " " + e.Text // 記録の時刻の時間帯は書いた側による (transcript 由来は UTC)
		if e.Screen != "" {
			line += sgrDim + " (画面 " + e.Screen + ")" + sgrReset
		}
		add("", line)
	}
	out = append(out, "")
	if m.activityReader() != nil {
		m.addActivity(add)
		return out
	}
	add(sgrDim, "出力")
	for _, s := range c.Log {
		add("", "  "+s)
	}
	return out
}

// drawerPanel は引き出しの行 (開ききった幅で組む。演出中は ComposeDrawer が切るだけ)。
func (m *Model) drawerPanel() []string {
	c, ok := m.drawerCardData()
	if !ok {
		return nil
	}
	rows := m.drawerBodyRows()
	target := drawerGeometry.Target(m.width)
	body := m.drawerBody()
	m.pager.Clamp(len(body), rows)
	off := m.pager.DrawOffset(len(body), rows)
	win := make([]string, rows)
	for i := range win {
		if off+i < len(body) {
			win[i] = " " + body[off+i]
		}
	}
	head := " " + sgrBold + fg(stateColor(c.State)) + c.ID + sgrFgReset + "  " + c.Title + sgrReset
	out := []string{head, fg(240) + strings.Repeat("─", max(target-1, 0)) + sgrReset}
	return append(out, layout.Scrollbar(win, target-1, len(body), off, true)...)
}

// overlayDrawer は領域 (ヘッダと下端の群のあいだ) に引き出しを重ねる。
func (m *Model) overlayDrawer(region []string) []string {
	if !m.drawerShown() {
		return region
	}
	w := layout.DrawerWidth(drawerGeometry.Target(m.width), m.drawer.Openness(m.now(), anim.EaseOutCubic))
	return layout.ComposeDrawer(region, m.drawerPanel(), w, m.width, true)
}

// hangWrap は行頭の字下げを保って幅 w で折り返す (続きの行は 2 桁深く下げる。commit の subject が行頭へ戻って hash の列が崩れない)。
func hangWrap(text string, w int) []string {
	body := strings.TrimLeft(text, " ")
	lead := strings.Repeat(" ", len(text)-len(body))
	lines := strings.Split(ansi.Hardwrap(body, max(w-len(lead)-2, 10), true), "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = lead + l
		} else {
			lines[i] = lead + "  " + strings.TrimLeft(l, " ") // 折り返した位置の空白を続きの行の頭に残さない
		}
	}
	return lines
}
