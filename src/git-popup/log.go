package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type previewMsg struct {
	sha  string // どのコミットの preview か (遅延到着で別選択を上書きしないための照合キー)
	text string
	err  error
}

type pushMsg struct{ err error }
type ciResultMsg struct{ states map[string]CIState }

type logModel struct {
	commits []Commit
	ci      map[string]CIState
	cursor  int
	offset  int
	preview string
	status  string
	width   int
	height  int
	done    bool
	confirm bool
	busy    bool
}

func newLogModel(commits []Commit) *logModel {
	return &logModel{commits: commits}
}

func (m *logModel) Init() tea.Cmd {
	return tea.Batch(m.previewCmd(), m.ciCmd())
}

func (m *logModel) previewCmd() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	return func() tea.Msg {
		text, err := loadPreview(sha)
		return previewMsg{sha: sha, text: text, err: err}
	}
}

// currentSHA はカーソル位置コミットの SHA ("" = 一覧が空)。preview 照合に使う。
func (m *logModel) currentSHA() string {
	if len(m.commits) == 0 {
		return ""
	}
	return m.commits[m.cursor].SHA
}

func (m *logModel) ciCmd() tea.Cmd {
	commits := append([]Commit(nil), m.commits...)
	return func() tea.Msg { return ciResultMsg{states: loadCI(commits)} }
}

func (m *logModel) Update(msg tea.Msg) (*logModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureCursorVisible()
	case previewMsg:
		if msg.sha != m.currentSHA() {
			break // 遅延到着した別コミットの preview は捨てる
		}
		if msg.err != nil {
			m.status = "preview error: " + msg.err.Error()
		} else {
			m.preview = msg.text
		}
	case ciResultMsg:
		m.ci = msg.states
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

func (m *logModel) handleKey(key string) (*logModel, tea.Cmd) {
	if m.confirm {
		switch strings.ToLower(key) {
		case "y":
			m.confirm = false
			m.busy = true
			return m, func() tea.Msg { return pushMsg{err: push()} }
		case "n", "esc", "ctrl+c":
			m.confirm = false
			m.status = ""
		}
		return m, nil
	}
	if key == "q" || key == "esc" || key == "ctrl+c" || key == "ctrl+g" {
		m.done = true
		return m, tea.Quit
	}
	if m.busy { // push 実行中は終了以外のキーを無視する
		return m, nil
	}
	if key == "ctrl+b" {
		m.confirm = true
		m.status = ""
		return m, nil
	}
	if len(m.commits) == 0 {
		return m, nil
	}
	old := m.cursor
	switch key {
	case "j", "down":
		m.cursor = min(m.cursor+1, len(m.commits)-1)
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	}
	if m.cursor != old {
		m.ensureCursorVisible()
		m.preview = "loading preview..."
		return m, m.previewCmd()
	}
	return m, nil
}

func (m *logModel) visibleRows() int { return max(m.height-2, 1) }

func (m *logModel) ensureCursorVisible() {
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

func (m *logModel) ciMark(sha string) string {
	if m.ci == nil {
		return "\x1b[2m·\x1b[0m"
	}
	switch m.ci[sha] {
	case CISuccess:
		return "\x1b[38;5;2m✓\x1b[0m"
	case CIFailure:
		return "\x1b[38;5;1m✗\x1b[0m"
	case CIPending:
		return "\x1b[38;5;3m●\x1b[0m"
	default:
		return " "
	}
}

func (m *logModel) View() string {
	if m.width < 1 {
		m.width = 80
	}
	if m.height < 1 {
		m.height = 24
	}
	leftWidth := max(m.width*45/100, 20)
	rightWidth := max(m.width-leftWidth-1, 10)
	rows := m.visibleRows()
	m.ensureCursorVisible()
	left := make([]string, rows)
	for i := range left {
		commitIndex := m.offset + i
		if commitIndex >= len(m.commits) {
			left[i] = ""
			continue
		}
		commit := m.commits[commitIndex]
		line := fmt.Sprintf("  %s %s %s", m.ciMark(commit.SHA), commit.ShortSHA, commit.Subject)
		if commitIndex == m.cursor {
			line = fmt.Sprintf("> %s %s %s", m.ciMark(commit.SHA), commit.ShortSHA, commit.Subject)
		}
		left[i] = clip(line, leftWidth)
	}
	right := strings.Split(strings.TrimRight(m.preview, "\n"), "\n")
	if len(right) == 1 && right[0] == "" {
		right = []string{"(no preview)"}
	}
	lines := make([]string, rows)
	for i := range lines {
		r := ""
		if i < len(right) {
			r = clip(right[i], rightWidth)
		}
		lines[i] = pad(left[i], leftWidth) + "│" + pad(r, rightWidth)
	}
	header := "git-popup  log"
	footer := "j/k or ↑/↓ move  C-b push  q/Esc/C-g quit"
	if m.confirm {
		footer = "push しますか? [y/N]"
	} else if m.busy {
		footer = "pushing..."
	} else if m.status != "" {
		footer = m.status
	}
	return header + "\n" + strings.Join(lines, "\n") + "\n" + footer
}

// ansiRe は SGR (色) エスケープ列。表示幅計算・truncate から除外するために使う。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// rw は East Asian Ambiguous (✓ ● ○ · 罫線 等) を幅 1 として扱う条件。StringWidth と
// RuneWidth を同一条件に揃えないと clip の見積り (RuneWidth) と displayWidth (StringWidth)
// がズレて 1 桁分の切り過ぎ/揃わずが出る。端末側も ✓/● を 1 桁で描くためこれで一致する。
var rw = &runewidth.Condition{EastAsianWidth: false}

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func displayWidth(s string) int { return rw.StringWidth(stripANSI(s)) }

// clip は ANSI (色) を保持したまま「見える幅」で切り詰める。素の runewidth を色付き行に
// 当てると、エスケープ列の分だけ幅を過大評価して早く切れ、さらにエスケープ途中で切ると
// 端末の色が壊れる (git show --color や CI マークで発生)。エスケープはそのまま通し、
// 可視幅だけ数える。切り詰めたら末尾に … と reset を足して色残りを断つ。
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	vis := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			if loc := ansiRe.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				b.WriteString(s[i : i+loc[1]])
				i += loc[1]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		cw := rw.RuneWidth(r)
		if vis+cw > width-1 { // 末尾の … の 1 桁を確保
			break
		}
		b.WriteString(s[i : i+size])
		vis += cw
		i += size
	}
	b.WriteString("…\x1b[0m")
	return b.String()
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-displayWidth(s), 0))
}
