// Package view は板を読む画面 (TUI)。左に話題の一覧、右に選んだ話題の中身 (ラベル・要点・原文) を出し、
// r で今のチャンネル、R で全部のチャンネルを読み直す (取得 → claude -p で分類 → 検査。中身は Loader に任せる)。
//
// 読み直しの間は前の板を出したままにする。結果は世代で照合し、追い越された古い結果は捨てる (r を連打しても、
// 最後に頼んだ読み直しの結果だけが残る)。
package view

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

// Loader は 1 チャンネルを読み、話題に組んだ板を返す。画面の外の goroutine で呼ばれる。
type Loader func(ctx context.Context, ch subs.Channel) (render.Page, error)

type chanState struct {
	ch       subs.Channel
	page     *render.Page // 最後に読めた板 (読み直しに失敗しても残す)
	err      error        // 最後の読み直しの誤り
	loading  bool
	gen      int // 読み直しを頼んだ回数。結果はこの値と照合する
	loadedAt time.Time
	stale    bool // page はキャッシュから出した前回の板で、この起動ではまだ読み直せていない
}

// Model は画面の状態。
type Model struct {
	ctx      context.Context
	load     Loader
	open     func(url string) error
	now      func() time.Time
	chans    []chanState
	cur      int
	list     listnav.List
	pager    listnav.Pager
	body     bool // 右の欄を操作しているか
	w, h     int
	notice   string // 1 回だけ出す知らせ (開けなかった等)
	quitting bool
}

// New は画面を作る。chans は 1 つ以上。
func New(ctx context.Context, chans []subs.Channel, load Loader, open func(string) error, now func() time.Time) *Model {
	m := &Model{ctx: ctx, load: load, open: open, now: now, w: 100, h: 30}
	for _, c := range chans {
		m.chans = append(m.chans, chanState{ch: c})
	}
	return m
}

// Preload はチャンネル i に前回の板 (キャッシュ) を出しておく。at は前回取得した時刻。読み直せるまで出し、開いたときに読み直す。
func (m *Model) Preload(i int, p render.Page, at time.Time) {
	if i < 0 || i >= len(m.chans) {
		return
	}
	c := &m.chans[i]
	c.page, c.loadedAt, c.stale = &p, at, true
}

// loadedMsg は読み直しの結果。
type loadedMsg struct {
	idx, gen int
	page     render.Page
	err      error
	at       time.Time
}

// reload はチャンネル i の読み直しを頼む。返す Cmd は画面の状態を触らない (Loader と照合用の値だけを持つ)。
func (m *Model) reload(i int) tea.Cmd {
	c := &m.chans[i]
	c.gen++
	c.loading = true
	c.err = nil // 前の失敗は、この読み直しの結果で決め直す
	gen, ch, load, ctx, now := c.gen, c.ch, m.load, m.ctx, m.now
	return func() tea.Msg {
		p, err := load(ctx, ch)
		return loadedMsg{idx: i, gen: gen, page: p, err: err, at: now()}
	}
}

// Init は最初のチャンネルだけを読む (他は開いたときに読む。起動のたびに全チャンネル分 claude を呼ばない)。
func (m *Model) Init() tea.Cmd {
	if len(m.chans) == 0 {
		return tea.Quit
	}
	return m.reload(0)
}

// Update はメッセージを処理する。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case loadedMsg:
		m.loaded(msg)
	case tea.KeyPressMsg:
		cmd := m.key(msg.String())
		if m.quitting {
			return m, tea.Quit
		}
		return m, cmd
	}
	return m, nil
}

func (m *Model) loaded(msg loadedMsg) {
	if msg.idx < 0 || msg.idx >= len(m.chans) {
		return
	}
	c := &m.chans[msg.idx]
	if msg.gen != c.gen {
		return // 後から頼んだ読み直しに追い越された
	}
	c.loading = false
	if msg.err != nil {
		c.err = msg.err
		return
	}
	var keep string
	if msg.idx == m.cur {
		if t, ok := m.selected(); ok {
			keep = t.Units[0].ID
		}
	}
	p := msg.page
	c.page, c.err, c.loadedAt, c.stale = &p, nil, msg.at, false
	if msg.idx == m.cur {
		// 読み直しで話題が並び替わっても、見ていた話題 (最初の単位で同一視する) にカーソルを戻す。無くなっていたら先頭へ
		m.list = listnav.List{}
		for i, t := range p.Topics {
			if slices.ContainsFunc(t.Units, func(u board.Unit) bool { return u.ID == keep }) {
				m.list.Cursor = i
				break
			}
		}
		m.list.Fit(len(p.Topics), m.listRows())
		m.pager.Reset()
	}
}

// Page はチャンネル i の最後に読めた板 (まだ読めていなければ ok が偽)。
func (m *Model) Page(i int) (render.Page, bool) {
	if i < 0 || i >= len(m.chans) || m.chans[i].page == nil {
		return render.Page{}, false
	}
	return *m.chans[i].page, true
}

func (m *Model) topics() []board.Topic {
	if p := m.chans[m.cur].page; p != nil {
		return p.Topics
	}
	return nil
}

func (m *Model) selected() (board.Topic, bool) {
	ts := m.topics()
	if len(ts) == 0 {
		return board.Topic{}, false
	}
	return ts[min(m.list.Cursor, len(ts)-1)], true
}

func (m *Model) key(k string) tea.Cmd {
	m.notice = ""
	if len(m.chans) == 0 {
		m.quitting = true
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		m.quitting = true
		return nil
	case "r":
		if m.chans[m.cur].loading {
			return nil // 走っている読み直しを待つ (連打で claude を何本も起こさない)
		}
		return m.reload(m.cur)
	case "R":
		var cmds []tea.Cmd
		for i := range m.chans {
			if !m.chans[i].loading {
				cmds = append(cmds, m.reload(i))
			}
		}
		return tea.Batch(cmds...)
	case "tab", "shift+tab":
		d := 1
		if k == "shift+tab" {
			d = -1
		}
		return m.switchTo(listnav.Cycle(m.cur, d, len(m.chans)))
	case "o":
		m.openSelected()
		return nil
	}
	if m.body {
		switch k {
		case "h", "left", "esc":
			m.body = false
			return nil
		}
		if mo := listnav.MotionOf(k); mo != listnav.None {
			m.pager.Move(mo, len(m.bodyLines()), m.bodyRows(), 0)
		}
		return nil
	}
	switch k {
	case "l", "right", "enter":
		if _, ok := m.selected(); ok {
			m.body = true
		}
		return nil
	}
	if mo := listnav.MotionOf(k); mo != listnav.None {
		if m.list.Move(mo, len(m.topics()), m.listRows(), 0) {
			m.pager.Reset()
		}
	}
	return nil
}

func (m *Model) switchTo(i int) tea.Cmd {
	if i == m.cur {
		return nil
	}
	m.cur, m.body = i, false
	m.list = listnav.List{}
	m.pager.Reset()
	c := &m.chans[i]
	if (c.page == nil || c.stale) && !c.loading && c.err == nil {
		return m.reload(i) // まだ読んでいない (キャッシュの板しか無い) チャンネルは開いたときに読む
	}
	return nil
}

// openSelected は選んだ話題の最後の発言を Slack で開く。
func (m *Model) openSelected() {
	t, ok := m.selected()
	p := m.chans[m.cur].page
	if !ok || p == nil || m.open == nil {
		return
	}
	last := t.Units[0]
	for _, u := range t.Units {
		if board.TsBefore(last.Last(), u.Last()) {
			last = u
		}
	}
	url := p.URL(last.Last(), last)
	if url == "" {
		m.notice = "リンクを組めない (ワークスペースの URL が分からない)"
		return
	}
	if err := m.open(url); err != nil {
		m.notice = "開けなかった: " + err.Error()
	}
}

// 画面の割り振り: 1 行目はチャンネルと状態、2 行目は注記、最後の行はキーの案内、残りが 2 つの枠 (上下の枠線で 2 行)。
func (m *Model) paneRows() int  { return max(m.h-3, 3) }
func (m *Model) listRows() int  { return max(m.paneRows()-2, 1) }
func (m *Model) bodyRows() int  { return max(m.paneRows()-2, 1) }
func (m *Model) leftWidth() int { return min(max(m.w*3/10, 24), 44) }
func (m *Model) bodyWidth() int { return max(m.w-m.leftWidth()-4, 10) }

// bodyLines は右の欄の行 (枠の幅で折り返した後)。
func (m *Model) bodyLines() []string {
	c := m.chans[m.cur]
	if c.page == nil {
		switch {
		case c.err != nil:
			return wrap(termsafe.PlainLine("読めなかった: "+c.err.Error()), m.bodyWidth())
		default:
			return []string{"読んでいます…"}
		}
	}
	t, ok := m.selected()
	if !ok {
		return []string{"期間内の発言はありません。"}
	}
	var out []string
	for _, l := range c.page.Body(t, false) {
		out = append(out, wrap(l, m.bodyWidth())...)
	}
	return out
}

// wrap は行を幅 w で折り返す。2 行目からは元の行の字下げに 2 桁足して揃える (原文の続きが左端へ戻って、誰の発言か追えなくならない)。
func wrap(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	body := strings.TrimLeft(s, " ")
	indent := len(s) - len(body)
	first := termwidth.Wrap(s, w, false)
	if len(first) <= 1 || indent+2 >= w/2 {
		return first
	}
	head := termwidth.Truncate(s, w, "")
	rest := strings.TrimPrefix(s, head)
	pad := strings.Repeat(" ", indent+2)
	out := []string{head}
	for _, l := range termwidth.Wrap(rest, w-len(pad), true) {
		out = append(out, pad+l)
	}
	return out
}

// MinWidth / MinHeight より小さい端末では枠を描かず、案内の 1 行だけを出す (枠の最小の幅・高さを割ると行がはみ出す)。
const (
	MinWidth  = 40
	MinHeight = 8
)

// Lines は画面の行。
func (m *Model) Lines() []string {
	if len(m.chans) == 0 {
		return []string{"読むチャンネルがありません"}
	}
	if m.w < MinWidth || m.h < MinHeight {
		return []string{termwidth.Truncate(fmt.Sprintf("端末が小さすぎる (%dx%d 以上にする)", MinWidth, MinHeight), max(m.w, 1), "")}
	}
	lines := []string{m.header(), m.noteLine()}
	left := m.leftLines()
	right := m.rightLines()
	for i := range m.paneRows() {
		lines = append(lines, left[i]+right[i])
	}
	return append(lines, termwidth.Truncate(m.help(), m.w, "…"))
}

// header はチャンネルのタブと、今のチャンネルの状態。幅に収まらないときは今のタブと状態を残し、今のタブから遠いタブを「…」に畳む
// (行の右端で切ると、後ろの方のチャンネルを開いたときに、どれを見ているかも読み込みの状態も消える)。
func (m *Model) header() string {
	names := make([]string, len(m.chans))
	for i, c := range m.chans {
		names[i] = "#" + termsafe.PlainLine(c.ch.Name)
	}
	tail := ""
	if s := m.state(); s != "" {
		tail = "   " + s
	}
	// 状態が長くて今のタブが読めなくなるときは、タブに minTab 桁を残して状態の方を詰める (どれを見ているかを先に守る)
	room := m.w - termwidth.Of(tail)
	if room < minTab {
		room = min(minTab, m.w)
	}
	return termwidth.Truncate(tabWindow(names, m.cur, room)+tail, m.w, "…")
}

// minTab は見出しの行で今のタブに必ず残す幅 (「#」とチャンネル名の頭の数文字が読める幅)。
const minTab = 12

func (m *Model) state() string {
	c := m.chans[m.cur]
	switch {
	case c.loading && c.stale:
		return "読み直し中… (" + c.loadedAt.Format("01/02 15:04") + " のキャッシュを出している)"
	case c.loading && c.page != nil:
		return "読み直し中… (前の板を出している)"
	case c.loading:
		return "読んでいます…"
	case c.err != nil && c.stale:
		return "🚨 読み直しに失敗 (" + c.loadedAt.Format("01/02 15:04") + " のキャッシュを出している)"
	case c.err != nil && c.page != nil:
		return "🚨 読み直しに失敗 (前の板を出している)"
	case c.err != nil:
		return "🚨 読めなかった (r で読み直す)"
	case c.stale:
		return c.loadedAt.Format("01/02 15:04") + " のキャッシュ"
	case c.page != nil:
		s := "最終 " + c.loadedAt.Format("15:04")
		// 新しい発言が無くて前回の分類を使い回したときは、分類した時刻を添える (ラベルや見出しが古いと分かるように)
		if at := c.page.ClassifiedAt; !at.IsZero() && at.Before(c.loadedAt.Add(-time.Minute)) {
			s += " (分類は " + at.Format("01/02 15:04") + ")"
		}
		return s
	}
	return ""
}

const (
	tabGap  = "  "
	tabMore = "…"
)

// tabWindow は names を tabGap でつないだ行で、cur を反転して出す。幅 room に収まらなければ cur を含む連続した範囲だけを出し、
// 外した側に tabMore を置く。cur だけでも収まらないときは、印を付けずに cur の名前を room に縮める
// (印の分だけ cur が押し出され、呼び出し側が行の右端で切ると状態が消える)。
func tabWindow(names []string, cur, room int) string {
	width := func(lo, hi int) int {
		w := termwidth.Of(tabGap) * (hi - lo)
		for _, n := range names[lo : hi+1] {
			w += termwidth.Of(n)
		}
		if lo > 0 {
			w += termwidth.Of(tabMore + tabGap)
		}
		if hi < len(names)-1 {
			w += termwidth.Of(tabGap + tabMore)
		}
		return w
	}
	lo, hi := cur, cur
	if width(lo, hi) > room {
		return "\x1b[7m" + termwidth.Truncate(names[cur], max(room, 1), "…") + "\x1b[27m"
	}
	for grew := true; grew; {
		grew = false
		if hi+1 < len(names) && width(lo, hi+1) <= room {
			hi, grew = hi+1, true
		}
		if lo > 0 && width(lo-1, hi) <= room {
			lo, grew = lo-1, true
		}
	}
	var parts []string
	if lo > 0 {
		parts = append(parts, tabMore)
	}
	for i := lo; i <= hi; i++ {
		if i == cur {
			parts = append(parts, "\x1b[7m"+names[i]+"\x1b[27m")
		} else {
			parts = append(parts, names[i])
		}
	}
	if hi < len(names)-1 {
		parts = append(parts, tabMore)
	}
	return strings.Join(parts, tabGap)
}

func (m *Model) noteLine() string {
	c := m.chans[m.cur]
	var s string
	switch {
	case m.notice != "":
		s = m.notice
	case c.err != nil && c.page != nil:
		s = "🚨 " + c.err.Error()
	case c.page != nil:
		// 期間の行は注記があっても隠さない (注記は後ろへ足す)
		s = fmt.Sprintf("%s 〜 %s / %s より前に始まったスレッドは見ていない",
			c.page.Since.Format("01/02 15:04"), c.page.Until.Format("01/02 15:04"), c.page.Lookback.Format("01/02"))
		if len(c.page.Notes) > 0 {
			s += " / 🚨 " + c.page.Notes[0]
			if len(c.page.Notes) > 1 {
				s += fmt.Sprintf(" (ほか %d 件)", len(c.page.Notes)-1)
			}
		}
	}
	return termwidth.Truncate(termsafe.PlainLine(s), m.w, "…")
}

// box は幅 w (枠線を含む)・高さ rows+2 の枠に、題 title と中身 body を入れた行。
func box(title string, body []string, w, rows int, active bool) []string {
	inner := max(w-2, 1)
	edge := func(l, r string, t string) string {
		t = termwidth.Truncate(t, max(inner-1, 0), "…")
		return l + t + strings.Repeat("─", max(inner-termwidth.Of(t), 0)) + r
	}
	top := "─"
	if active {
		top = "━"
	}
	if title != "" {
		top += " " + title + " "
	}
	out := []string{edge("┌", "┐", top)}
	for i := range rows {
		s := ""
		if i < len(body) {
			s = body[i]
		}
		s = termwidth.Truncate(s, inner, "…")
		out = append(out, "│"+s+termwidth.PadSpaces(inner-termwidth.Of(s))+"│")
	}
	return append(out, edge("└", "┘", ""))
}

func (m *Model) leftLines() []string {
	ts := m.topics()
	m.list.Fit(len(ts), m.listRows())
	var rows []string
	end := min(m.list.Offset+m.listRows(), len(ts))
	for i := m.list.Offset; i < end; i++ {
		t := ts[i]
		cur := " "
		if i == m.list.Cursor {
			cur = "▶"
		}
		count := fmt.Sprintf(" %d", render.Count(t))
		text := firstMark(t) + " " + termsafe.PlainLine(t.Title)
		room := max(m.leftWidth()-2-1-termwidth.Of(count), 1)
		text = termwidth.Truncate(text, room, "…")
		line := cur + text + termwidth.PadSpaces(room-termwidth.Of(text)) + count
		if i == m.list.Cursor && !m.body {
			line = "\x1b[7m" + line + "\x1b[27m"
		}
		rows = append(rows, line)
	}
	return box("話題", rows, m.leftWidth(), m.listRows(), !m.body)
}

// firstMark は一覧に出す印 1 つ (要対応 → 必読 → 状態の順で最初に当てはまるもの)。どの印も表示幅は 2。
func firstMark(t board.Topic) string {
	switch {
	case t.Has(board.KindAction):
		return "🔴"
	case t.Has(board.KindMustRead):
		return "📌"
	case t.Unclassified:
		return "⚪"
	case t.State == board.StateDecided:
		return "🟢"
	case t.State == board.StateOpen:
		return "🟡"
	}
	return "⚪"
}

func (m *Model) rightLines() []string {
	title := ""
	if t, ok := m.selected(); ok {
		title = termsafe.PlainLine(t.Title)
		if mk := render.Mark(t); mk != "" {
			title = mk + ": " + title
		}
	}
	lines := m.bodyLines()
	m.pager.Clamp(len(lines), m.bodyRows())
	off := m.pager.DrawOffset(len(lines), m.bodyRows())
	return box(title, lines[min(off, len(lines)):], m.w-m.leftWidth(), m.bodyRows(), m.body)
}

func (m *Model) help() string {
	if m.body {
		return " j/k スクロール  h 話題へ  o Slack で開く  r 読み直す  R 全部  Tab チャンネル  q 終わる"
	}
	return " j/k 話題  l 本文へ  o Slack で開く  r 読み直す  R 全部  Tab チャンネル  q 終わる"
}

// View は画面を出す。
func (m *Model) View() tea.View {
	v := tea.NewView(strings.Join(m.Lines(), "\n"))
	v.AltScreen = true
	return v
}

// Run は画面を開き、閉じるまで待つ。
func Run(m *Model) error {
	_, err := tea.NewProgram(m).Run()
	return err
}
