package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type changesStatusMsg struct {
	changes []Change
	err     error
}
type changesPreviewMsg struct {
	path string // どのファイルの preview か (遅延到着で別選択を上書きしないための照合キー)
	text string
	err  error
}
type changesActionMsg struct{ err error }

type inputMode int

const (
	inputNone inputMode = iota
	inputCommit
)

type changesModel struct {
	changes []Change
	cursor  int
	offset  int
	preview string
	status  string
	width   int
	height  int
	busy    bool
	confirm bool
	input   inputMode
	message []rune
}

func newChangesModel() *changesModel { return &changesModel{} }

func (m *changesModel) Init() tea.Cmd { return m.statusCmd() }

func (m *changesModel) statusCmd() tea.Cmd {
	return func() tea.Msg {
		changes, err := loadChanges()
		return changesStatusMsg{changes: changes, err: err}
	}
}

func (m *changesModel) previewCmd() tea.Cmd {
	if len(m.changes) == 0 {
		return nil
	}
	change := m.changes[m.cursor]
	return func() tea.Msg {
		text, err := loadChangePreview(change)
		return changesPreviewMsg{path: change.Path, text: text, err: err}
	}
}

// currentPath はカーソル位置ファイルのパス ("" = 一覧が空)。preview 照合に使う。
func (m *changesModel) currentPath() string {
	if len(m.changes) == 0 {
		return ""
	}
	return m.changes[m.cursor].Path
}

func (m *changesModel) actionCmd(action func() error) tea.Cmd {
	return func() tea.Msg { return changesActionMsg{err: action()} }
}

func (m *changesModel) Update(msg tea.Msg) (*changesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureCursorVisible()
	case changesStatusMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "status error: " + msg.err.Error()
		} else {
			m.changes = msg.changes
			m.cursor = min(m.cursor, max(len(m.changes)-1, 0))
			m.ensureCursorVisible()
			m.preview = ""
			if len(m.changes) > 0 {
				m.preview = "loading preview..."
				return m, m.previewCmd()
			}
		}
	case changesPreviewMsg:
		if msg.path != m.currentPath() {
			break // 遅延到着した別ファイルの preview は捨てる (カーソルは既に動いている)
		}
		if msg.err != nil {
			m.status = "preview error: " + msg.err.Error()
		} else {
			m.preview = msg.text
		}
	case changesActionMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "git error: " + msg.err.Error()
		} else {
			m.status = ""
			return m, m.statusCmd()
		}
	case pushMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "push failed: " + msg.err.Error()
		} else {
			m.status = "push completed"
		}
	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m *changesModel) handleKey(key string) (*changesModel, tea.Cmd) {
	if m.input == inputCommit {
		switch key {
		case "enter":
			if len(strings.TrimSpace(string(m.message))) == 0 {
				return m, nil
			}
			message := string(m.message)
			m.input = inputNone
			m.message = nil
			m.busy = true
			return m, m.actionCmd(func() error { return commitChanges(message) })
		case "esc":
			m.input, m.message = inputNone, nil
		case "backspace":
			if len(m.message) > 0 {
				m.message = m.message[:len(m.message)-1]
			}
		default:
			if len([]rune(key)) == 1 {
				m.message = append(m.message, []rune(key)...)
			}
		}
		return m, nil
	}
	if m.confirm {
		switch strings.ToLower(key) {
		case "y":
			m.confirm, m.busy = false, true
			return m, func() tea.Msg { return pushMsg{err: push()} }
		case "n", "esc", "ctrl+c":
			m.confirm, m.status = false, ""
		}
		return m, nil
	}
	if key == "q" || key == "esc" || key == "ctrl+c" || key == "ctrl+g" {
		return m, tea.Quit
	}
	// git 操作 (stage/add/commit/push) 実行中は終了以外のキーを無視する。並行実行すると
	// 後着の status 再読込が状態を上書きし、git add/commit が競合するため。
	if m.busy {
		return m, nil
	}
	if key == "ctrl+b" {
		m.confirm, m.status = true, ""
		return m, nil
	}
	if key == "ctrl+a" {
		m.busy = true
		return m, m.actionCmd(stageAll)
	}
	if key == "ctrl+o" {
		m.input, m.message = inputCommit, nil
		m.status = ""
		return m, nil
	}
	if len(m.changes) == 0 {
		return m, nil
	}
	old := m.cursor
	switch key {
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.changes)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "space", "enter":
		m.busy = true
		change := m.changes[m.cursor]
		return m, m.actionCmd(func() error { return stageChange(change) })
	}
	if old != m.cursor {
		m.ensureCursorVisible()
		m.preview = "loading preview..."
		return m, m.previewCmd()
	}
	return m, nil
}

// markColor は XY ステータス 2 桁を自前で着色する (porcelain -z は無色のため)。
// git 既定に倣い staged(index 列)=緑・worktree 列=赤・untracked(?)=赤。空白はそのまま。
// 色は単一ソース theme/colors.yml (active_green / error_red) から引く。
func markColor(c Change) string {
	col := func(b byte, role string) string {
		switch b {
		case ' ':
			return " "
		case '?':
			return paintFg("error_red", "?")
		default:
			return paintFg(role, string(b))
		}
	}
	return col(c.Index, "active_green") + col(c.Worktree, "error_red")
}

func (m *changesModel) visibleRows() int { return max(m.height-2, 1) }
func (m *changesModel) ensureCursorVisible() {
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *changesModel) View() string {
	// WindowSizeMsg 到着前は描かない (log.View と同じ理由: 残像防止)。
	if m.width < 1 || m.height < 1 {
		return ""
	}
	leftWidth := max(m.width*45/100, 20)
	rightWidth := max(m.width-leftWidth-1, 10)
	rows := m.visibleRows()
	m.ensureCursorVisible()
	left := make([]string, rows)
	for i := range left {
		index := m.offset + i
		if index < len(m.changes) {
			c := m.changes[index]
			prefix := "  "
			if index == m.cursor {
				prefix = "> "
			}
			left[i] = clip(prefix+markColor(c)+" "+c.Path, leftWidth)
		}
	}
	right := strings.Split(strings.TrimRight(m.preview, "\n"), "\n")
	if len(right) == 1 && right[0] == "" {
		right = []string{"(no changes)"}
	}
	lines := make([]string, rows)
	for i := range lines {
		r := ""
		if i < len(right) {
			r = clip(right[i], rightWidth)
		}
		lines[i] = pad(left[i], leftWidth) + "│" + pad(r, rightWidth)
	}
	header := "git-popup  changes"
	footer := "j/k or ↑/↓ move  space/Enter stage  C-a add all  C-o commit  C-b push  C-l log  q/Esc/C-g quit"
	if m.input == inputCommit {
		footer = "commit message: " + string(m.message) + "  (Enter commit, Esc cancel)"
	} else if m.confirm {
		footer = "push しますか? [y/N]"
	} else if m.busy {
		footer = "working..."
	} else if m.status != "" {
		footer = m.status
	} else if len(m.changes) == 0 {
		footer = "no changes  C-l log"
	}
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer
}
