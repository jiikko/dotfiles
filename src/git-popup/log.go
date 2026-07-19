package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

type previewMsg struct {
	text string
	err  error
}

type pushMsg struct{ err error }

type logModel struct {
	commits []Commit
	cursor  int
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
	if len(m.commits) == 0 {
		return nil
	}
	return m.previewCmd()
}

func (m *logModel) previewCmd() tea.Cmd {
	sha := m.commits[m.cursor].SHA
	return func() tea.Msg {
		text, err := loadPreview(sha)
		return previewMsg{text: text, err: err}
	}
}

func (m *logModel) Update(msg tea.Msg) (*logModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case previewMsg:
		if msg.err != nil {
			m.status = "preview error: " + msg.err.Error()
		} else {
			m.preview = msg.text
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
		m.preview = "loading preview..."
		return m, m.previewCmd()
	}
	return m, nil
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
	rows := max(m.height-2, 1)
	left := make([]string, rows)
	for i := range left {
		if i >= len(m.commits) {
			left[i] = ""
			continue
		}
		commit := m.commits[i]
		line := fmt.Sprintf("  %s %s", commit.ShortSHA, commit.Subject)
		if i == m.cursor {
			line = "> " + commit.ShortSHA + " " + commit.Subject
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

func clip(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-runewidth.StringWidth(s), 0))
}
