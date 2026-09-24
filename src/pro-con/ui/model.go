// Package ui は pro-con の TUI。状態は backend が持ち、ここは Snapshot を描くことと
// Command を送ることだけをする (画面を閉じても・落ちても何も失わない。415 要件 14)。
package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuikit/listnav"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
)

// TickInterval は Poll を呼ぶ間隔 (実時間)。fake では 1 回の Poll が模擬時間の 1 分に当たる。
const TickInterval = time.Second

type mode int

const (
	modeBoard mode = iota
	modeInput
)

type inputKind int

const (
	inputAnswer inputKind = iota
	inputOrder
	inputBtw
	inputNew
)

type tickMsg struct{}

type attachDoneMsg struct {
	cardID string
	err    error
}

type Model struct {
	be     backend.Backend
	snap   backend.Snapshot
	width  int
	height int

	// repos は config から列挙した repo。タブは global + 「ここに在り、カードも在る repo」。
	repos []backend.Repo
	// tab は選んでいる repo 名 ("" = global)。位置ではなく名前で持つ (タブの増減で別の repo へずれない)。
	tab string

	// 選択は位置ではなくカード ID で持つ (安定キー)。カードが列を移っても選択が付いていく。
	selected   string
	showDetail bool

	mode      mode
	input     []rune
	inputKind inputKind
	orderKind card.OrderKind

	flash string

	// カードの移動の演出 (motion.go)。now は時計 (テストで差し替える)
	now       func() time.Time
	prevSlots map[string]slot
	moves     map[string]*move
	framing   bool // frame の tick が回っているか (二重に回さない)

	copy func(string) error // クリップボードへ入れる (既定は pbcopy。テストは差し替える)

	// Claude Code の session の一覧 (sessions.go)
	listSessions func(context.Context) ([]agents.Session, error)
	sessions     []agents.Session
	sessErr      string
	sessFetched  bool
	showSessions bool
}

// New は repos (config から列挙した repo) をタブの候補にして画面を作る。nil なら global だけ。
func New(be backend.Backend, repos []backend.Repo) *Model {
	m := &Model{be: be, repos: repos, width: 120, height: 40, now: time.Now, copy: pbcopy,
		listSessions: func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) }}
	m.snap = be.Poll()
	m.ensureSelection()
	m.resetSlots()
	return m
}

// Notify は起動時の警告など、画面の外から通知行へ文面を出す。
func (m *Model) Notify(s string) { m.flash = s }

func tick() tea.Cmd { return tea.Tick(TickInterval, func(time.Time) tea.Msg { return tickMsg{} }) }

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.fetchSessions()) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.snap = m.be.Poll()
		tab := m.tab
		m.ensureTab()
		m.ensureSelection()
		if m.tab != tab { // タブが消えて global へ戻った: 配置が丸ごと変わるので演出しない
			m.resetSlots()
			return m, tick()
		}
		return m, tea.Batch(tick(), m.trackMoves())
	case frameMsg:
		return m, m.onFrame()
	case sessionsMsg:
		return m, m.onSessions(msg)
	case sessionsTickMsg:
		return m, m.fetchSessions()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case attachDoneMsg:
		if msg.err != nil {
			m.flash = "attach が失敗した: " + msg.err.Error()
		} else {
			m.flash = msg.cardID + " の session から戻った (session は動き続けている)"
		}
		return m, nil
	case tea.PasteMsg:
		// ペーストは入力欄にだけ入れる。ボードではキー操作として解釈しない
		if m.mode == modeInput {
			m.input = append(m.input, []rune(msg.Content)...)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.mode == modeInput {
			cmd := m.handleInputKey(msg)
			return m, tea.Batch(cmd, m.trackMoves()) // 回答などで列が変わったら演出する
		}
		return m, m.handleBoardKey(msg)
	}
	return m, nil
}

// tabs は global ("") と、config に在り、かつカードが 1 枚以上ある repo 名。
// config に在ってもカードの無い repo は出さない (~/src の下は数十 repo あり、タブが埋まる)。
func (m *Model) tabs() []string {
	has := map[string]bool{}
	for _, c := range m.snap.Cards {
		has[c.Repo] = true
	}
	out := []string{""}
	for _, r := range m.repos {
		if has[r.Name] {
			out = append(out, r.Name)
		}
	}
	return out
}

// tabRepo は選んでいるタブの repo (global ならゼロ値)。新しい依頼のスコープになる。
func (m *Model) tabRepo() backend.Repo {
	for _, r := range m.repos {
		if r.Name == m.tab && m.tab != "" {
			return r
		}
	}
	return backend.Repo{}
}

// ensureTab は選んでいる repo のカードが無くなってタブが消えたら global へ戻す。
func (m *Model) ensureTab() {
	for _, t := range m.tabs() {
		if t == m.tab {
			return
		}
	}
	m.tab = ""
}

func (m *Model) moveTab(delta int) {
	ts := m.tabs()
	cur := 0
	for i, t := range ts {
		if t == m.tab {
			cur = i
		}
	}
	m.tab = ts[(cur+delta+len(ts))%len(ts)]
	m.selected = ""
	m.ensureSelection()
	m.resetSlots() // タブの切り替えはカードが動いたのではない
}

// visible は選んでいるタブに属するカード。global は全部 (config の外の repo のカードも含む)。
func (m *Model) visible() []card.Card {
	if m.tab == "" {
		return m.snap.Cards
	}
	var out []card.Card
	for _, c := range m.snap.Cards {
		if c.Repo == m.tab {
			out = append(out, c)
		}
	}
	return out
}

// columns は選んでいるタブのカードをカンバンの列に分ける。列の中は「その列に入った順」(Since、同時なら ID)。
// 移ってきたカードは必ず列の末尾に着地するので、既存のカードの位置は動かない (Snapshot の並び順のままだと、
// 移ってきたカードが途中に割り込んで既存のカードが一斉にずれる)。
func (m *Model) columns() [][]card.Card {
	cols := make([][]card.Card, len(card.Columns))
	for _, c := range m.visible() {
		for i, st := range card.Columns {
			if c.State == st {
				cols[i] = append(cols[i], c)
			}
		}
	}
	for _, cs := range cols {
		slices.SortStableFunc(cs, func(a, b card.Card) int {
			if c := a.Since.Compare(b.Since); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return cols
}

// position は選択中のカードの (列, 行)。見つからなければ ok=false。
func (m *Model) position() (col, row int, ok bool) { return m.positionOf(m.selected) }

func (m *Model) positionOf(id string) (col, row int, ok bool) {
	for i, cs := range m.columns() {
		for j, c := range cs {
			if c.ID == id {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

func (m *Model) ensureSelection() {
	if _, _, ok := m.position(); ok {
		return
	}
	for _, cs := range m.columns() {
		if len(cs) > 0 {
			m.selected = cs[0].ID
			return
		}
	}
	m.selected = ""
}

func (m *Model) selectedCard() (card.Card, bool) {
	for _, c := range m.snap.Cards {
		if c.ID == m.selected {
			return c, true
		}
	}
	return card.Card{}, false
}

// moveCol は左右の列へ移る。空の列は飛ばす。
func (m *Model) moveCol(delta int) {
	col, row, ok := m.position()
	if !ok {
		return
	}
	cols := m.columns()
	for i := col + delta; i >= 0 && i < len(cols); i += delta {
		if len(cols[i]) == 0 {
			continue
		}
		m.selected = cols[i][min(row, len(cols[i])-1)].ID
		return
	}
}

// moveRow は列の中で delta 枚動く。端で止める (半ページ・先頭・末尾の移動も同じ関数で端に寄せる)。
func (m *Model) moveRow(delta int) {
	col, row, ok := m.position()
	if !ok {
		return
	}
	cs := m.columns()[col]
	m.selected = cs[max(0, min(row+delta, len(cs)-1))].ID
}

func (m *Model) handleBoardKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "tab":
		m.moveTab(1)
	case "shift+tab":
		m.moveTab(-1)
	case "left", "h":
		m.moveCol(-1)
	case "right", "l", "ctrl+f": // ctrl+f は → の別名 (docs/glogx-ui-guide.md の emacs 層。ctrl+b は ← にしない)
		m.moveCol(1)
	case "enter":
		m.showDetail = !m.showDetail
	case "esc":
		m.showDetail = false
	case "a":
		return m.attach()
	case "r":
		c, ok := m.selectedCard()
		if !ok || c.State != card.Waiting {
			m.flash = "回答できるのは質問待ちのカードだけ"
			return nil
		}
		m.startInput(inputAnswer)
	case "o":
		if _, ok := m.selectedCard(); ok {
			m.orderKind = card.OrderAppend
			m.startInput(inputOrder)
		}
	case "b":
		if _, ok := m.selectedCard(); ok {
			m.startInput(inputBtw)
		}
	case "n":
		m.startInput(inputNew)
	case "y":
		m.yank()
	case "s":
		m.showSessions = !m.showSessions
	default:
		m.moveByMotion(listnav.MotionOf(k.String()))
	}
	return nil
}

// moveByMotion は上下の移動を tuikit の語彙 (listnav.MotionOf) で受ける。語彙は glogx と同じ
// (j/k/ctrl+n/ctrl+p/↑↓ = 1 枚、ctrl+d/ctrl+u/space/f/pgdn/pgup = 半ページ、g/G/home/end = 先頭・末尾)。
// 🚨 画面固有の動作キーは handleBoardKey の switch で先に捌いているので、ここへは来ない (b は btw)。
func (m *Model) moveByMotion(mo listnav.Motion) {
	half := listnav.Half(m.shownCards())
	switch mo {
	case listnav.Down:
		m.moveRow(1)
	case listnav.Up:
		m.moveRow(-1)
	case listnav.HalfDown:
		m.moveRow(half)
	case listnav.HalfUp:
		m.moveRow(-half)
	case listnav.Top:
		m.moveRow(-len(m.snap.Cards))
	case listnav.Bottom:
		m.moveRow(len(m.snap.Cards))
	case listnav.None:
	}
}

func (m *Model) startInput(k inputKind) {
	m.mode = modeInput
	m.inputKind = k
	m.input = nil
	m.flash = ""
}

func (m *Model) handleInputKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeBoard
		m.flash = "入力を取り消した"
	case "enter":
		m.submit()
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "tab":
		if m.inputKind == inputOrder {
			m.orderKind = (m.orderKind + 1) % 3
		}
	case "ctrl+c":
		return tea.Quit
	default:
		if k.Text != "" {
			m.input = append(m.input, []rune(k.Text)...)
		}
	}
	return nil
}

func (m *Model) submit() {
	text := string(m.input)
	var cmd backend.Command
	switch m.inputKind {
	case inputAnswer:
		cmd = backend.Answer{CardID: m.selected, Text: text, From: "人間"}
	case inputOrder:
		cmd = backend.AddOrder{CardID: m.selected, Kind: m.orderKind, Text: text}
	case inputBtw:
		cmd = backend.Btw{CardID: m.selected, Question: text}
	case inputNew:
		cmd = backend.NewRequest{Repo: m.tabRepo(), Text: text}
	}
	res, err := m.be.Apply(cmd)
	if err != nil {
		m.flash = "失敗: " + err.Error()
		if errors.Is(err, backend.ErrEmptyText) {
			return // 入力欄を開いたまま書き直させる
		}
	} else {
		m.flash = res
	}
	m.mode = modeBoard
	m.input = nil
	m.snap = m.be.Snapshot()
	m.ensureTab()
	m.ensureSelection()
}

func (m *Model) attach() tea.Cmd {
	c, ok := m.selectedCard()
	if !ok {
		return nil
	}
	cmd, err := m.be.AttachCommand(c.Session)
	if err != nil {
		m.flash = "attach できない: " + err.Error()
		return nil
	}
	id := c.ID
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return attachDoneMsg{cardID: id, err: err} })
}
