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
		return centeredPanel(transitionPanel(state, width, spinnerFrame), width, height, StatusLine(state, width))
	}
	footer := make([]string, 0, 2)
	message := state.Message
	switch state.Transition.Result {
	case runner.TransitionBuildFailed:
		message = "ビルドに失敗しました"
	case runner.TransitionReadinessUnconfirmed:
		message = "起動を確認できませんでした"
	}
	if message := MessageLine(message, width); message != "" {
		footer = append(footer, message)
	}
	footer = append(footer, StatusLine(state, width))
	if prompt := confirmPrompt(state); prompt != "" {
		if width < layout.PanelMinWidth {
			return append([]string{compactConfirmLine(state.Confirm, width)}, footer...)
		}
		// 確認は横だけ中央に寄せ、縦は footer の直上に置く。inline 表示は view の上端が戻らないので、縦に寄せると
		// 取り消した後 (ログが流れていない間) に最下行が画面の途中に浮いたまま残る
		return append(centerHorizontally(confirm.Dialog("", []string{prompt}, confirm.HintYesNo, width, false), width), footer...)
	}
	return footer
}

// centerHorizontally は板を横の中央に寄せ、各行を width 桁に揃える。
func centerHorizontally(box []string, width int) []string {
	boxWidth := 0
	for _, line := range box {
		boxWidth = max(boxWidth, termwidth.Of(line))
	}
	indent := strings.Repeat(" ", max((width-boxWidth)/2, 0))
	lines := make([]string, 0, len(box))
	for _, line := range box {
		line = indent + line
		lines = append(lines, line+strings.Repeat(" ", max(width-termwidth.Of(line), 0)))
	}
	return lines
}

// centeredPanel は板を端末の中央に置き、footer (最下行のステータスなど) をその下の最下部に付ける。
// inline 表示なので板より上の行は返さない (そこはログが流れる場所)。板の下に空行を挟んで縦の位置を中央へ寄せ、
// 端末が低くて入らないときは footer の直上に寄せる。
func centeredPanel(box []string, width, height int, footer ...string) []string {
	panelHeight := len(box)
	gap := 0
	if height > panelHeight+len(footer)+2 {
		screenTop := max((height-panelHeight)/2, 0)
		gap = max(height-screenTop-panelHeight-len(footer), 0)
	}
	lines := make([]string, 0, panelHeight+gap+len(footer))
	lines = append(lines, centerHorizontally(box, width)...)
	for range gap {
		lines = append(lines, "")
	}
	return append(lines, footer...)
}

func compactTransitionLine(state runner.Model, width int) string {
	// 確認ダイアログが開いているなら、段より確認を優先して出す (板が入らない幅でも y / n を押せると分かるように)。
	if state.Confirm != runner.ConfirmNone {
		return compactConfirmLine(state.Confirm, width)
	}
	switch state.Transition.Stage {
	case runner.TransitionStop:
		return fitFirst(width, "終了", "stop")
	case runner.TransitionBuild:
		return fitFirst(width, "ビルド", "build")
	case runner.TransitionReady:
		return fitFirst(width, "確認", "ready")
	default:
		return fitFirst(width, "起動", "launch")
	}
}

// compactConfirmLine は板の入らない幅 (layout.PanelMinWidth 未満) の確認の 1 行。板の間も板の外も同じ文言にする。
func compactConfirmLine(confirmState runner.Confirm, width int) string {
	// 4 桁あれば終了と再起動を見分けられる形を残す (Q / R は確認を開いたキー)。3 桁以下は y/n を切り詰める
	if confirmState == runner.ConfirmRestart {
		return fitFirst(width, "再起動y/n", "再y/n", "Ry/n", "y/n")
	}
	return fitFirst(width, "終了y/n", "終y/n", "Qy/n", "y/n")
}

// fitFirst は width に収まる最初の候補を返す。どれも入らなければ最後の候補を切り詰める
// (全角の候補は 1 桁に入らないので、最後は ASCII にしておく)。
func fitFirst(width int, candidates ...string) string {
	for _, candidate := range candidates {
		if termwidth.Of(candidate) <= width {
			return candidate
		}
	}
	return termwidth.Truncate(candidates[len(candidates)-1], width, "")
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
	if prompt := confirmPrompt(state); prompt != "" && state.Transition.Active {
		rows = append(rows, "", prompt, confirm.HintYesNo)
	} else {
		rows = append(rows, "", transitionHint(state))
	}
	return confirm.Box("─ "+title+" ", rows, width, false)
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
	for index := range completed {
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

func confirmPrompt(state runner.Model) string {
	switch state.Confirm {
	case runner.ConfirmRestart:
		return "アプリを再起動しますか？"
	case runner.ConfirmQuit:
		if state.State == runner.Crashed {
			return "restartable を終了しますか？" // アプリはもう終わっている
		}
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
