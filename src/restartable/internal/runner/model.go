package runner

import "fmt"

// State is the externally visible lifecycle state of a runner.
type State string

const (
	Building    State = "building"
	Running     State = "running"
	BuildFailed State = "build-failed"
	Stopping    State = "stopping"
	Exiting     State = "exiting"
	// Crashed は子が rc≠0 (シグナルを含む) で自分から終わった後、R / restart か Q を待つ状態。
	// rc 0 の終了は人の Cmd+Q 等なので待たずに runner も終える (issue 586 の R7)
	Crashed State = "crashed"
)

// Confirm is deliberately orthogonal to Transition: a confirmation asks the
// user to choose an action; a transition panel shows work already in progress.
type Confirm string

const (
	ConfirmNone    Confirm = ""
	ConfirmRestart Confirm = "restart"
	ConfirmQuit    Confirm = "quit"
)

type Intent string

const (
	IntentNone    Intent = ""
	IntentRestart Intent = "restart"
	IntentExit    Intent = "quit"
)

type TransitionKind string

const (
	TransitionNone    TransitionKind = ""
	TransitionRestart TransitionKind = "restart"
	TransitionQuit    TransitionKind = "quit"
	TransitionStartup TransitionKind = "startup"
)

type TransitionStage string

const (
	TransitionStop   TransitionStage = "stop"
	TransitionBuild  TransitionStage = "build"
	TransitionLaunch TransitionStage = "launch"
	TransitionReady  TransitionStage = "ready"
)

type TransitionResult string

const (
	TransitionResultNone           TransitionResult = ""
	TransitionBuildFailed          TransitionResult = "build-failed"
	TransitionReadinessUnconfirmed TransitionResult = "readiness-unconfirmed"
)

// Transition is the model for the progress panel. Active controls its
// visibility; Kind and Stage describe its heading and current step, Result is
// kept after the panel closes for the result line, and Busy marks ignored R/Q.
type Transition struct {
	Active bool
	Kind   TransitionKind
	Stage  TransitionStage
	Result TransitionResult
	Busy   bool
}

type EventKind string

const (
	KeyEvent               EventKind = "key"
	BuildSucceededEvent    EventKind = "build-succeeded"
	BuildFailedEvent       EventKind = "build-failed"
	ChildStartedEvent      EventKind = "child-started"
	ChildExitedEvent       EventKind = "child-exited"
	StopStartedEvent       EventKind = "stop-started"
	StopCommandOKEvent     EventKind = "stop-command-ok"
	StopCommandFailEvent   EventKind = "stop-command-failed"
	ControlRestartEvent    EventKind = "control-restart"
	ForceEvent             EventKind = "force"
	LaunchStartedEvent     EventKind = "launch-started"
	ReadySucceededEvent    EventKind = "ready-succeeded"
	ReadyFailedEvent       EventKind = "ready-failed"
	ReadyCheckResumedEvent EventKind = "ready-check-resumed"
)

// Event is an immutable input to Update. Key uses terminal spellings such as
// "R", "enter", "esc", and "ctrl+c".
type Event struct {
	Kind        EventKind
	Key         string
	PID         int
	SignalCode  int
	Reason      string
	ChildExited bool
	ReadyCheck  bool
	Generation  uint64
	// ExitStatus は ChildExitedEvent の子の終了コード (シグナルは 128+番号)。
	ExitStatus int
	// NoUI はキーを受けられない (UI の無い) 起動。待っても操作できないので、子の rc≠0 の終了は子の rc で、
	// ビルドの失敗は rc 1 で、待たずに runner を終える (CI や pipe が止まったままにならないように)。
	NoUI bool
}

type EffectKind string

const (
	StartBuildEffect    EffectKind = "start-build"
	BeginStopEffect     EffectKind = "begin-stop"
	ForceStopEffect     EffectKind = "force-stop"
	ExitEffect          EffectKind = "exit"
	ControlRejectEffect EffectKind = "control-reject"
	MessageEffect       EffectKind = "message"
)

type Effect struct {
	Kind   EffectKind
	Reason string
}

// Model contains only data needed to render and decide lifecycle transitions.
// Update has no I/O and can be called directly by UI code and tests.
type Model struct {
	State        State
	Confirm      Confirm
	PID          int
	Generation   uint64
	Intent       Intent
	BuildQueued  bool
	StopAccepted bool
	ExitCode     int
	Message      string
	Transition   Transition
	Ready        bool
}

func InitialModel() Model {
	return Model{
		State: Building, Confirm: ConfirmNone,
		Transition: Transition{Active: true, Kind: TransitionStartup, Stage: TransitionBuild},
	}
}

func Update(m Model, e Event) (Model, []Effect) {
	if m.State == Stopping {
		m.Confirm = ConfirmNone
	}
	if m.State == Exiting {
		if e.Kind == ControlRestartEvent {
			return m, []Effect{{Kind: ControlRejectEffect, Reason: "runner exiting"}}
		}
		if e.Kind == KeyEvent && isRestartOrQuitKey(e.Key) {
			m.Confirm = ConfirmNone
			m.Message = "処理中"
			if m.Transition.Active {
				m.Transition.Busy = true
			}
			return m, []Effect{{Kind: MessageEffect, Reason: "処理中"}}
		}
		return m, nil
	}
	switch e.Kind {
	case KeyEvent:
		return updateKey(m, e.Key)
	case BuildSucceededEvent:
		if m.State != Building {
			return m, nil
		}
		if m.BuildQueued {
			m.BuildQueued = false
			m.Message = "ビルド中に再起動の要求があったのでビルドし直します"
			if m.Transition.Active {
				m.Transition.Stage = TransitionBuild
			}
			return m, []Effect{{Kind: StartBuildEffect}}
		}
		m.State = Running
		if m.Transition.Active {
			m.Transition.Stage = TransitionLaunch
		}
		return m, nil
	case BuildFailedEvent:
		if m.State != Building {
			return m, nil
		}
		if m.BuildQueued {
			m.BuildQueued = false
			m.Message = "ビルド中に再起動の要求があったのでビルドし直します"
			if m.Transition.Active {
				m.Transition.Stage = TransitionBuild
			}
			return m, []Effect{{Kind: StartBuildEffect}}
		}
		m.State = BuildFailed
		m.PID = 0
		m.Ready = false
		m.Message = e.Reason
		m.Transition.Active = false
		m.Transition.Busy = false
		m.Transition.Result = TransitionBuildFailed
		if e.NoUI {
			// 止めた古い子の残り (stop-cmd が親だけを終わらせた場合の孫など) も、Ctrl-C と同じく片付けてから終える
			m.State = Exiting
			m.Confirm = ConfirmNone
			m.ExitCode = 1
			return m, []Effect{{Kind: ForceStopEffect}, {Kind: ExitEffect}}
		}
		return m, nil
	case LaunchStartedEvent:
		if m.Transition.Active {
			m.Transition.Stage = TransitionLaunch
		}
		return m, nil
	case ChildStartedEvent:
		if m.State != Exiting {
			m.State = Running
			m.PID = e.PID
			m.Generation++
			m.Ready = !e.ReadyCheck
			if m.Transition.Active {
				if e.ReadyCheck {
					m.Transition.Stage = TransitionReady
				} else {
					m.Transition.Active = false
				}
			}
			m.Transition.Result = TransitionResultNone
			m.Transition.Busy = false
			m.Intent = IntentNone
			m.StopAccepted = false
			m.Message = ""
		}
		return m, nil
	case ReadySucceededEvent:
		if m.Generation != e.Generation || m.State != Running || !m.Transition.Active || m.Transition.Stage != TransitionReady {
			return m, nil
		}
		m.Ready = true
		m.Transition.Active = false
		m.Transition.Busy = false
		m.Transition.Result = TransitionResultNone
		m.Message = ""
		return m, nil
	case ReadyFailedEvent:
		if m.Generation != e.Generation || m.State != Running || !m.Transition.Active || m.Transition.Stage != TransitionReady {
			return m, nil
		}
		m.Ready = false
		m.Transition.Active = false
		m.Transition.Busy = false
		m.Transition.Result = TransitionReadinessUnconfirmed
		m.Message = "起動を確認できませんでした"
		return m, nil
	case ReadyCheckResumedEvent:
		if m.State != Running || m.Ready {
			return m, nil
		}
		m.Transition = Transition{Active: true, Kind: TransitionStartup, Stage: TransitionReady}
		m.Message = ""
		return m, nil
	case ChildExitedEvent:
		m.PID = 0
		m.Ready = false
		m.Transition.Active = false
		m.Transition.Busy = false
		if m.State == Stopping {
			return finishStopping(m)
		}
		m.Confirm = ConfirmNone
		m.Intent = IntentNone
		m.StopAccepted = false
		if e.ExitStatus != 0 && !e.NoUI {
			m.State = Crashed
			m.Transition.Result = TransitionResultNone // 前の「起動を確認できませんでした」が view で message を上書きしないように
			m.Message = fmt.Sprintf("アプリが終了しました (rc %d)。R で再ビルド / Q で終了", e.ExitStatus)
			return m, nil
		}
		m.State = Exiting
		m.ExitCode = e.ExitStatus
		return m, []Effect{{Kind: ExitEffect}}
	case StopStartedEvent:
		m.State = Stopping
		m.Confirm = ConfirmNone
		kind := TransitionRestart
		if m.Intent == IntentExit {
			kind = TransitionQuit
		} else {
			m.Intent = IntentRestart
		}
		m.Transition = Transition{Active: true, Kind: kind, Stage: TransitionStop}
		m.Message = "終了処理中 (Ctrl-C で強制終了)"
		return m, []Effect{{Kind: BeginStopEffect}}
	case StopCommandOKEvent:
		m.StopAccepted = true
		m.Message = "終了待ち (Esc で取り消し / Ctrl-C で強制終了)"
		return m, nil
	case StopCommandFailEvent:
		if e.ChildExited {
			// A failed stop command cannot be treated as the cause of the child's
			// exit. In particular, a pending restart must not turn a human Cmd+Q
			// into a restart after the application exits on its own.
			m.Intent = IntentNone
			m.Transition.Active = false
			m.Transition.Busy = false
			return finishStopping(m)
		}
		if m.PID != 0 {
			m.State = Running
			m.Intent = IntentNone
			m.StopAccepted = false
			m.Message = e.Reason
			m.Transition.Active = false
			m.Transition.Busy = false
			return m, nil
		}
		return finishStopping(m)
	case ControlRestartEvent:
		return updateControlRestart(m)
	case ForceEvent:
		m.State = Exiting
		m.Confirm = ConfirmNone
		m.PID = 0
		m.Intent = IntentExit
		m.Ready = false
		m.Transition.Active = false
		m.Transition.Busy = false
		m.ExitCode = e.SignalCode
		return m, []Effect{{Kind: ForceStopEffect}, {Kind: ExitEffect}}
	}
	return m, nil
}

func updateKey(m Model, key string) (Model, []Effect) {
	if key == "ctrl+c" {
		return Update(m, Event{Kind: ForceEvent, SignalCode: 130})
	}
	if m.State == Stopping {
		// A stop request is already in flight. Only Esc can undo an accepted
		// stop, and Ctrl-C above remains the explicit force path.
		m.Confirm = ConfirmNone
		switch key {
		case "r", "R", "q", "Q":
			m.Transition.Busy = true
			m.Message = "処理中"
			return m, []Effect{{Kind: MessageEffect, Reason: "処理中"}}
		case "esc":
			if m.StopAccepted {
				m.State = Running
				m.Intent = IntentNone
				m.StopAccepted = false
				m.Transition.Active = false
				m.Transition.Busy = false
				m.Message = "停止を取り消しました"
				return m, []Effect{{Kind: ControlRejectEffect, Reason: "stop cancelled"}}
			}
		}
		return m, nil
	}
	if m.Transition.Active && isRestartOrQuitKey(key) {
		if key == "Q" || key == "q" {
			if m.Confirm == ConfirmNone && transitionAcceptsQuit(m.Transition) {
				m.Confirm = ConfirmQuit
				m.Transition.Busy = false
				m.Message = ""
				return m, nil
			}
		}
		m.Transition.Busy = true
		m.Message = "処理中"
		return m, []Effect{{Kind: MessageEffect, Reason: "処理中"}}
	}
	if m.Confirm != ConfirmNone {
		// 実行のキーは tuikit の confirm.IsYes と同じ y / Y / Enter だが、寄せない: confirm は y / Enter 以外をすべて取り消すのに対し、
		// ここは n / N / Esc だけが取り消しで、ほかのキーでは確認を開いたままにする (誤打鍵で確認を閉じない)。実行の集合だけを
		// 寄せると 1 つの switch に 2 つの規則が混ざり、状態遷移の層 (runner) が UI の部品を import する。規則を揃えるときに一緒に見直す
		switch key {
		case "y", "Y", "enter":
			confirm := m.Confirm
			m.Confirm = ConfirmNone
			if confirm == ConfirmRestart {
				if m.State == Running {
					m.State = Stopping
					m.Intent = IntentRestart
					m.Transition = Transition{Active: true, Kind: TransitionRestart, Stage: TransitionStop}
					return m, []Effect{{Kind: BeginStopEffect}}
				}
				return m, nil
			}
			m.Intent = IntentExit
			m.Transition = Transition{Active: true, Kind: TransitionQuit, Stage: TransitionStop}
			if m.State == Running {
				m.State = Stopping
				return m, []Effect{{Kind: BeginStopEffect}}
			}
			if m.State == Building {
				m.State = Exiting
				m.ExitCode = 0
				return m, []Effect{{Kind: ForceStopEffect}, {Kind: ExitEffect}}
			}
			m.State = Exiting
			m.ExitCode = 0
			return m, []Effect{{Kind: ExitEffect}}
		case "n", "N", "esc":
			m.Confirm = ConfirmNone
			if m.Transition.Active {
				m.Transition.Busy = false
				m.Message = ""
			}
			return m, nil
		default:
			return m, nil
		}
	}
	switch key {
	case "r", "R":
		switch m.State {
		case Building:
			m.Message = "ビルド中"
			return m, []Effect{{Kind: MessageEffect, Reason: "ビルド中"}}
		case BuildFailed, Crashed:
			return rebuildAfterFailure(m)
		case Running:
			m.Confirm = ConfirmRestart
		}
	case "q", "Q":
		m.Confirm = ConfirmQuit
	}
	return m, nil
}

func transitionAcceptsQuit(transition Transition) bool {
	switch transition.Stage {
	case TransitionBuild, TransitionLaunch, TransitionReady:
		return true
	default:
		return false
	}
}

func isRestartOrQuitKey(key string) bool {
	switch key {
	case "r", "R", "q", "Q":
		return true
	default:
		return false
	}
}

func updateControlRestart(m Model) (Model, []Effect) {
	m.Confirm = ConfirmNone
	switch m.State {
	case Building:
		m.BuildQueued = true
		return m, nil
	case BuildFailed, Crashed:
		return rebuildAfterFailure(m)
	case Running:
		m.State = Stopping
		m.Intent = IntentRestart
		m.Transition = Transition{Active: true, Kind: TransitionRestart, Stage: TransitionStop}
		m.Message = "終了処理中 (Ctrl-C で強制終了)"
		return m, []Effect{{Kind: BeginStopEffect}}
	case Stopping:
		if m.Intent == IntentExit {
			m.Intent = IntentRestart
			m.Transition = Transition{Active: true, Kind: TransitionRestart, Stage: TransitionStop}
		}
		return m, nil
	}
	return m, []Effect{{Kind: ControlRejectEffect, Reason: "runner unavailable"}}
}

// rebuildAfterFailure starts a new build from build-failed or crashed (R key and
// control restart). From build-failed the panel reopens only if one was shown for
// the failed build; from crashed it always reopens as a restart.
func rebuildAfterFailure(m Model) (Model, []Effect) {
	if m.State == Crashed {
		m.Transition.Kind = TransitionRestart // 動いていたアプリを立ち上げ直すので、初回の起動の後でも「再起動」
	}
	m.State = Building
	m.Message = ""
	if m.Transition.Kind != TransitionNone {
		m.Transition.Active = true
		m.Transition.Stage = TransitionBuild
		m.Transition.Result = TransitionResultNone
		m.Transition.Busy = false
	}
	return m, []Effect{{Kind: StartBuildEffect}}
}

func finishStopping(m Model) (Model, []Effect) {
	m.PID = 0
	m.StopAccepted = false
	m.Message = ""
	if m.Intent == IntentRestart {
		m.State = Building
		m.Intent = IntentNone
		m.Ready = false
		m.Transition = Transition{Active: true, Kind: TransitionRestart, Stage: TransitionBuild}
		return m, []Effect{{Kind: StartBuildEffect}}
	}
	m.State = Exiting
	m.Intent = IntentNone
	m.Confirm = ConfirmNone
	m.Ready = false
	m.Transition.Active = false
	m.Transition.Busy = false
	m.ExitCode = 0
	return m, []Effect{{Kind: ExitEffect}}
}
