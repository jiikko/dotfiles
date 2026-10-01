package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

const (
	defaultWidth  = 80
	defaultHeight = 24
	fullActions   = "── [R] 再起動  [Q] 終了 "
	shortActions  = "[R]再起動 [Q]終了 "
	minActions    = "[R][Q] "
)

// StatusLine renders the pinned inline status row at exactly width display cells.
func StatusLine(state runner.Model, width int) string {
	if width <= 0 {
		width = defaultWidth
	}
	suffix := string(state.State)
	if state.State == runner.Running && state.PID > 0 {
		suffix = fmt.Sprintf("running (pid %d)", state.PID)
	}
	suffix = " " + suffix

	actions := fullActions
	if termwidth.Of(actions)+termwidth.Of(suffix) > width {
		actions = shortActions
	}
	if termwidth.Of(actions)+termwidth.Of(suffix) > width {
		actions = minActions
	}
	if termwidth.Of(actions) > width {
		actions = termwidth.Truncate(actions, width, "")
	}
	if termwidth.Of(actions)+termwidth.Of(suffix) > width {
		suffix = termwidth.Truncate(suffix, max(width-termwidth.Of(actions), 0), "…")
	}
	ruleWidth := max(width-termwidth.Of(actions)-termwidth.Of(suffix), 0)
	line := actions + strings.Repeat("─", ruleWidth) + suffix
	return line + strings.Repeat(" ", max(width-termwidth.Of(line), 0))
}

// MessageLine returns a safe, single-line message clipped to the view width.
func MessageLine(message string, width int) string {
	if width <= 0 {
		width = defaultWidth
	}
	message = strings.Join(strings.Fields(termsafe.PlainLine(message)), " ")
	if message == "" {
		return ""
	}
	return termwidth.Truncate(message, width, "…")
}

// ViewLines composes the optional confirm panel and message above the final row.
func ViewLines(state runner.Model, width int) []string {
	if width <= 0 {
		width = defaultWidth
	}
	lines := make([]string, 0, 8)
	if prompt := confirmPrompt(state.Confirm); prompt != "" {
		lines = append(lines, confirm.Dialog("", []string{prompt}, confirm.HintYesNo, width, false)...)
	}
	if message := MessageLine(state.Message, width); message != "" {
		lines = append(lines, message)
	}
	lines = append(lines, StatusLine(state, width))
	return lines
}

func confirmPrompt(confirmState runner.Confirm) string {
	switch confirmState {
	case runner.ConfirmRestart:
		return "アプリを再起動しますか？"
	case runner.ConfirmQuit:
		return "アプリを終了しますか？"
	default:
		return ""
	}
}

type snapshotMsg runner.Model
type startedMsg struct{}

type teaModel struct {
	state  runner.Model
	width  int
	height int
	keys   chan<- string
	ready  func()
	closed <-chan struct{}
}

func (m *teaModel) Init() tea.Cmd {
	return func() tea.Msg { return startedMsg{} }
}

func (m *teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case startedMsg:
		m.ready()
	case tea.WindowSizeMsg:
		width, height := msg.Width, msg.Height
		if width <= 0 {
			width = defaultWidth
		}
		if height <= 0 {
			height = defaultHeight
		}
		m.width, m.height = width, height
		if width != msg.Width || height != msg.Height {
			cmd = func() tea.Msg { return tea.WindowSizeMsg{Width: width, Height: height} }
		}
	case snapshotMsg:
		m.state = runner.Model(msg)
	case tea.KeyPressMsg:
		if key, ok := ModelKey(msg); ok {
			select {
			case m.keys <- key:
			case <-m.closed:
			default:
			}
		}
	}
	return m, cmd
}

func (m *teaModel) View() tea.View {
	view := tea.NewView(strings.Join(ViewLines(m.state, m.width), "\n"))
	view.AltScreen = false
	return view
}

// ModelKey maps only keys understood by runner.Update. In particular Ctrl-C is
// an ordinary model key here; Bubble Tea's signal handler is disabled.
func ModelKey(key tea.KeyPressMsg) (string, bool) {
	switch key.String() {
	case "r", "R", "q", "Q", "y", "Y", "enter", "n", "N", "esc", "ctrl+c":
		return key.String(), true
	default:
		return "", false
	}
}
