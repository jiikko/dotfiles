package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
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
	return ViewLinesAt(state, width, defaultHeight, 0)
}

// ViewLinesAt lays out the current view for a terminal page and spinner frame.
// An active panel starts the view; only the gap below the panel is included so
// inline log output above it keeps its viewport space.
func ViewLinesAt(state runner.Model, width, height, spinnerFrame int) []string {
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	if state.Transition.Active {
		if width < layout.PanelMinWidth {
			return []string{compactTransitionLine(state, width)}
		}
		panel := transitionPanel(state, width, spinnerFrame)
		status := StatusLine(state, width)
		panelHeight := len(panel)
		gap := 0
		if height > panelHeight+3 {
			screenTop := max((height-panelHeight)/2, 0)
			gap = max(height-screenTop-panelHeight-1, 0)
		}
		lines := make([]string, 0, panelHeight+gap+1)
		lines = append(lines, panel...)
		for range gap {
			lines = append(lines, "")
		}
		lines = append(lines, status)
		return lines
	}
	lines := make([]string, 0, 8)
	if prompt := confirmPrompt(state.Confirm); prompt != "" {
		lines = append(lines, confirm.Dialog("", []string{prompt}, confirm.HintYesNo, width, false)...)
	}
	message := state.Message
	switch state.Transition.Result {
	case runner.TransitionBuildFailed:
		message = "ビルドに失敗しました"
	case runner.TransitionReadinessUnconfirmed:
		message = "起動を確認できませんでした"
	}
	if message := MessageLine(message, width); message != "" {
		lines = append(lines, message)
	}
	lines = append(lines, StatusLine(state, width))
	return lines
}

func compactTransitionLine(state runner.Model, width int) string {
	// 確認ダイアログが開いているなら、段より確認を優先して出す (板が入らない幅でも y / n を押せると分かるように)。
	switch state.Confirm {
	case runner.ConfirmQuit:
		return termwidth.Truncate("quit? y/n", width, "")
	case runner.ConfirmRestart:
		return termwidth.Truncate("rst? y/n", width, "")
	}
	stage := "launch"
	switch state.Transition.Stage {
	case runner.TransitionStop:
		stage = "stop"
	case runner.TransitionBuild:
		stage = "build"
	case runner.TransitionReady:
		stage = "ready"
	}
	return termwidth.Truncate(stage, width, "")
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func transitionPanel(state runner.Model, width, frame int) []string {
	title := "起動中"
	switch state.Transition.Kind {
	case runner.TransitionRestart:
		title = "再起動中"
	case runner.TransitionQuit:
		title = "終了中"
	}
	frame = ((frame % len(spinnerFrames)) + len(spinnerFrames)) % len(spinnerFrames)
	current := "アプリを起動しています…"
	switch state.Transition.Stage {
	case runner.TransitionStop:
		current = "アプリを終了しています…"
	case runner.TransitionBuild:
		current = "ビルドしています…"
	case runner.TransitionReady:
		current = "起動を確認しています…"
	}
	steps := transitionSteps(state.Transition)
	rows := []string{
		spinnerFrames[frame] + " " + current,
		steps,
	}
	if state.Transition.Busy {
		rows = append(rows, "処理中")
	}
	if prompt := confirmPrompt(state.Confirm); prompt != "" && state.Transition.Active {
		rows = append(rows, "", prompt, confirm.HintYesNo)
	} else {
		rows = append(rows, "", transitionHint(state))
	}
	box := confirm.Box("─ "+title+" ", rows, width, false)
	boxWidth := 0
	for _, line := range box {
		boxWidth = max(boxWidth, termwidth.Of(line))
	}
	left := max((width-boxWidth)/2, 0)
	indent := strings.Repeat(" ", left)
	for index := range box {
		line := indent + box[index]
		box[index] = line + strings.Repeat(" ", max(width-termwidth.Of(line), 0))
	}
	return box
}

func transitionSteps(transition runner.Transition) string {
	if transition.Kind == runner.TransitionQuit {
		return "   終了"
	}

	steps := []string{"終了", "ビルド", "起動", "起動の確認"}
	completed := 0
	switch transition.Stage {
	case runner.TransitionBuild:
		completed = 1
	case runner.TransitionLaunch:
		completed = len(steps) - 2
	case runner.TransitionReady:
		completed = len(steps) - 1
	}
	for index := 0; index < completed; index++ {
		steps[index] += "✓"
	}
	return "   " + strings.Join(steps, " → ")
}

func transitionHint(state runner.Model) string {
	if state.Transition.Stage == runner.TransitionStop {
		if state.StopAccepted {
			return "Esc: 取り消し   Ctrl-C: 強制終了"
		}
		return "Ctrl-C: 強制終了"
	}
	switch state.Transition.Stage {
	case runner.TransitionBuild, runner.TransitionLaunch, runner.TransitionReady:
		return "Q: 終了   Ctrl-C: 強制終了"
	default:
		return "Ctrl-C: 強制終了"
	}
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
type spinnerTickMsg struct{}

type teaModel struct {
	state         runner.Model
	width         int
	height        int
	keys          chan<- string
	delivery      *keyDelivery
	ready         func()
	closed        <-chan struct{}
	spinnerTick   func() tea.Cmd
	spinnerActive bool
	spinnerFrame  int
}

func (m *teaModel) Init() tea.Cmd {
	return func() tea.Msg { return startedMsg{} }
}

func (m *teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case startedMsg:
		m.ready()
		if m.state.Transition.Active {
			m.spinnerActive = true
			cmd = m.nextSpinnerTick()
		}
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
		wasActive := m.state.Transition.Active
		m.state = runner.Model(msg)
		if m.state.Transition.Active && !wasActive && !m.spinnerActive {
			m.spinnerActive = true
			cmd = m.nextSpinnerTick()
		} else if !m.state.Transition.Active {
			m.spinnerActive = false
			m.spinnerFrame = 0
		}
	case spinnerTickMsg:
		if m.spinnerActive && m.state.Transition.Active {
			m.spinnerFrame++
			cmd = m.nextSpinnerTick()
		}
	case tea.KeyPressMsg:
		if key, ok := ModelKey(msg); ok {
			if m.delivery != nil {
				m.delivery.enqueue(key)
				break
			}
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
	view := tea.NewView(strings.Join(ViewLinesAt(m.state, m.width, m.height, m.spinnerFrame), "\n"))
	view.AltScreen = false
	return view
}

func (m *teaModel) nextSpinnerTick() tea.Cmd {
	if m.spinnerTick != nil {
		return m.spinnerTick()
	}
	return func() tea.Msg {
		<-time.After(100 * time.Millisecond)
		return spinnerTickMsg{}
	}
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
