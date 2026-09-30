package runner

// State is the externally visible lifecycle state of a runner.
type State string

const (
	Building    State = "building"
	Running     State = "running"
	BuildFailed State = "build-failed"
	Stopping    State = "stopping"
	Exiting     State = "exiting"
)

// Confirm is deliberately orthogonal to State: a build can finish while a quit
// confirmation is open, and control requests can dismiss either confirmation.
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

type EventKind string

const (
	KeyEvent             EventKind = "key"
	BuildSucceededEvent  EventKind = "build-succeeded"
	BuildFailedEvent     EventKind = "build-failed"
	ChildStartedEvent    EventKind = "child-started"
	ChildExitedEvent     EventKind = "child-exited"
	StopStartedEvent     EventKind = "stop-started"
	StopCommandOKEvent   EventKind = "stop-command-ok"
	StopCommandFailEvent EventKind = "stop-command-failed"
	ControlRestartEvent  EventKind = "control-restart"
	ControlStatusEvent   EventKind = "control-status"
	ForceEvent           EventKind = "force"
)

// Event is an immutable input to Update. Key uses terminal spellings such as
// "R", "enter", "esc", and "ctrl+c".
type Event struct {
	Kind       EventKind
	Key        string
	PID        int
	ExitCode   int
	SignalCode int
	Reason     string
}

type EffectKind string

const (
	StartBuildEffect    EffectKind = "start-build"
	BeginStopEffect     EffectKind = "begin-stop"
	ForceStopEffect     EffectKind = "force-stop"
	ExitEffect          EffectKind = "exit"
	ControlRejectEffect EffectKind = "control-reject"
	ControlStatusEffect EffectKind = "control-status"
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
}

func InitialModel() Model { return Model{State: Building, Confirm: ConfirmNone} }

func Update(m Model, e Event) (Model, []Effect) {
	if m.State == Exiting {
		if e.Kind == ControlRestartEvent {
			return m, []Effect{{Kind: ControlRejectEffect, Reason: "runner exiting"}}
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
			m.Message = "restart queued during build"
			return m, []Effect{{Kind: StartBuildEffect}}
		}
		m.State = Running
		return m, nil
	case BuildFailedEvent:
		if m.State != Building {
			return m, nil
		}
		if m.BuildQueued {
			m.BuildQueued = false
			m.Message = "restart queued during build"
			return m, []Effect{{Kind: StartBuildEffect}}
		}
		m.State = BuildFailed
		m.PID = 0
		m.Message = e.Reason
		return m, nil
	case ChildStartedEvent:
		if m.State != Exiting {
			m.State = Running
			m.PID = e.PID
			m.Generation++
			m.Intent = IntentNone
			m.StopAccepted = false
			m.Message = ""
		}
		return m, nil
	case ChildExitedEvent:
		m.PID = 0
		if m.State == Stopping {
			return finishStopping(m)
		}
		m.State = Exiting
		m.Confirm = ConfirmNone
		m.ExitCode = 0
		return m, []Effect{{Kind: ExitEffect}}
	case StopStartedEvent:
		m.State = Stopping
		m.Confirm = ConfirmNone
		m.Intent = IntentRestart
		return m, []Effect{{Kind: BeginStopEffect}}
	case StopCommandOKEvent:
		m.StopAccepted = true
		m.Message = "stopping — waiting for child exit"
		return m, nil
	case StopCommandFailEvent:
		if m.PID != 0 {
			m.State = Running
			m.Intent = IntentNone
			m.StopAccepted = false
			m.Message = e.Reason
			return m, nil
		}
		return finishStopping(m)
	case ControlRestartEvent:
		return updateControlRestart(m)
	case ControlStatusEvent:
		return m, []Effect{{Kind: ControlStatusEffect}}
	case ForceEvent:
		m.State = Exiting
		m.Confirm = ConfirmNone
		m.PID = 0
		m.Intent = IntentExit
		m.ExitCode = e.SignalCode
		return m, []Effect{{Kind: ForceStopEffect}, {Kind: ExitEffect}}
	}
	return m, nil
}

func updateKey(m Model, key string) (Model, []Effect) {
	if key == "ctrl+c" {
		return Update(m, Event{Kind: ForceEvent, SignalCode: 130})
	}
	if m.Confirm != ConfirmNone {
		switch key {
		case "y", "Y", "enter":
			confirm := m.Confirm
			m.Confirm = ConfirmNone
			if confirm == ConfirmRestart {
				if m.State == Running {
					m.State = Stopping
					m.Intent = IntentRestart
					return m, []Effect{{Kind: BeginStopEffect}}
				}
				return m, nil
			}
			m.Intent = IntentExit
			if m.State == Running {
				m.State = Stopping
				return m, []Effect{{Kind: BeginStopEffect}}
			}
			if m.State == Building {
				m.State = Stopping
				return m, []Effect{{Kind: ForceStopEffect}, {Kind: ExitEffect}}
			}
			m.State = Exiting
			m.ExitCode = 0
			return m, []Effect{{Kind: ExitEffect}}
		case "n", "N", "esc":
			m.Confirm = ConfirmNone
			return m, nil
		default:
			return m, nil
		}
	}
	switch key {
	case "r", "R":
		switch m.State {
		case Building:
			m.Message = "building"
			return m, []Effect{{Kind: MessageEffect, Reason: "building"}}
		case BuildFailed:
			m.State = Building
			m.Message = ""
			return m, []Effect{{Kind: StartBuildEffect}}
		case Running:
			m.Confirm = ConfirmRestart
		}
	case "q", "Q":
		m.Confirm = ConfirmQuit
	case "esc":
		if m.State == Stopping && m.StopAccepted && m.Intent == IntentRestart {
			m.State = Running
			m.Intent = IntentNone
			m.StopAccepted = false
			m.Message = "stop cancelled"
			return m, []Effect{{Kind: ControlRejectEffect, Reason: "stop cancelled"}}
		}
	}
	return m, nil
}

func updateControlRestart(m Model) (Model, []Effect) {
	m.Confirm = ConfirmNone
	switch m.State {
	case Building:
		m.BuildQueued = true
		return m, nil
	case BuildFailed:
		m.State = Building
		m.Message = ""
		return m, []Effect{{Kind: StartBuildEffect}}
	case Running:
		m.State = Stopping
		m.Intent = IntentRestart
		return m, []Effect{{Kind: BeginStopEffect}}
	case Stopping:
		if m.Intent == IntentExit {
			m.Intent = IntentRestart
		}
		return m, nil
	}
	return m, []Effect{{Kind: ControlRejectEffect, Reason: "runner unavailable"}}
}

func finishStopping(m Model) (Model, []Effect) {
	m.PID = 0
	m.StopAccepted = false
	if m.Intent == IntentRestart {
		m.State = Building
		m.Intent = IntentNone
		return m, []Effect{{Kind: StartBuildEffect}}
	}
	m.State = Exiting
	m.Confirm = ConfirmNone
	m.ExitCode = 0
	return m, []Effect{{Kind: ExitEffect}}
}
