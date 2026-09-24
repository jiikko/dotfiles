// Package ui は pro-con の TUI。状態は backend が持ち、ここは Snapshot を描くことと
// Command を送ることだけをする (画面を閉じても・落ちても何も失わない。415 要件 14)。
package ui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

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

	// 選択は位置ではなくカード ID で持つ (安定キー)。カードが列を移っても選択が付いていく。
	selected   string
	showDetail bool

	mode      mode
	input     []rune
	inputKind inputKind
	orderKind card.OrderKind

	flash string
}

func New(be backend.Backend) *Model {
	m := &Model{be: be, width: 120, height: 40}
	m.snap = be.Poll()
	m.ensureSelection()
	return m
}

func tick() tea.Cmd { return tea.Tick(TickInterval, func(time.Time) tea.Msg { return tickMsg{} }) }

func (m *Model) Init() tea.Cmd { return tick() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.snap = m.be.Poll()
		m.ensureSelection()
		return m, tick()
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
			return m, m.handleInputKey(msg)
		}
		return m, m.handleBoardKey(msg)
	}
	return m, nil
}

// columns は Snapshot をカンバンの列に分ける。列の中は Snapshot の並び順のまま。
func (m *Model) columns() [][]card.Card {
	cols := make([][]card.Card, len(card.Columns))
	for _, c := range m.snap.Cards {
		for i, st := range card.Columns {
			if c.State == st {
				cols[i] = append(cols[i], c)
			}
		}
	}
	return cols
}

// position は選択中のカードの (列, 行)。見つからなければ ok=false。
func (m *Model) position() (col, row int, ok bool) {
	for i, cs := range m.columns() {
		for j, c := range cs {
			if c.ID == m.selected {
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

func (m *Model) moveRow(delta int) {
	col, row, ok := m.position()
	if !ok {
		return
	}
	cs := m.columns()[col]
	if r := row + delta; r >= 0 && r < len(cs) {
		m.selected = cs[r].ID
	}
}

func (m *Model) handleBoardKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "left", "h":
		m.moveCol(-1)
	case "right", "l":
		m.moveCol(1)
	case "up", "k":
		m.moveRow(-1)
	case "down", "j":
		m.moveRow(1)
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
	}
	return nil
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
