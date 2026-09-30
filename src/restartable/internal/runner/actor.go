package runner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jiikko/dotfiles/src/restartable/internal/control"
	"github.com/jiikko/dotfiles/src/termsafe"
)

type Config struct {
	BuildCommand       string
	RunArgs            []string
	StopCommand        string
	StopCommandTimeout time.Duration
	TermGrace          time.Duration
	IDEnv              string
	ControlPath        string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	Headless           bool
	Presenter          Presenter
}

type actorEventKind string

const (
	buildDoneEvent actorEventKind = "build-done"
	runDoneEvent   actorEventKind = "run-done"
	stopDoneEvent  actorEventKind = "stop-done"
	forceDoneEvent actorEventKind = "force-done"
	keyEvent       actorEventKind = "key"
)

type actorEvent struct {
	kind   actorEventKind
	proc   *process
	result processResult
	err    error
	forced bool
}

// Presenter is the boundary for the next milestone's Bubble Tea terminal UI.
// It receives model snapshots and key events; process management never draws.
type Presenter interface {
	Render(Model)
	Keys() <-chan string
	Close() error
}

type headlessPresenter struct{}

func (headlessPresenter) Render(Model)        {}
func (headlessPresenter) Keys() <-chan string { return nil }
func (headlessPresenter) Close() error        { return nil }

type actor struct {
	cfg                Config
	model              Model
	id                 string
	server             *control.Server
	sink               *logSink
	presenter          Presenter
	events             chan actorEvent
	build              *process
	child              *process
	stop               *process
	allProcesses       []*process
	queuedRequests     []*control.Request
	pendingRequests    []*control.Request
	childResult        *processResult
	childProcessed     bool
	stopAccepted       bool
	stopRunning        bool
	forceRunning       bool
	groupStopRunning   bool
	outputDrainTimeout time.Duration
	forceCode          int
	finished           bool
	retCode            int
	flusherDone        chan struct{}
	flusherWG          sync.WaitGroup
}

// Run supervises one foreground process. It returns a process-style exit code.
func Run(cfg Config) (int, error) {
	if len(cfg.RunArgs) == 0 {
		return 1, errors.New("missing command after --")
	}
	if cfg.StopCommandTimeout <= 0 {
		cfg.StopCommandTimeout = 5 * time.Second
	}
	if cfg.TermGrace < 0 {
		return 1, errors.New("--term-grace cannot be negative")
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.ControlPath == "" {
		return 1, errors.New("control socket path is empty")
	}
	server, err := control.Listen(cfg.ControlPath)
	if err != nil {
		return 1, err
	}
	id, err := newID()
	if err != nil {
		_ = server.Close()
		return 1, fmt.Errorf("create instance id: %w", err)
	}
	presenter := cfg.Presenter
	if presenter == nil {
		presenter = headlessPresenter{}
	}
	return (&actor{cfg: cfg, model: InitialModel(), id: id, server: server,
		sink: newLogSink(cfg.Stdout, cfg.Headless), presenter: presenter,
		events: make(chan actorEvent, 128), flusherDone: make(chan struct{}),
		outputDrainTimeout: 500 * time.Millisecond}).run()
}

func newID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (a *actor) run() (int, error) {
	defer func() {
		a.drainOutputs()
		close(a.flusherDone)
		a.flusherWG.Wait()
		a.sink.Flush()
		_ = a.presenter.Close()
		_ = a.server.Close()
	}()
	if !a.cfg.Headless {
		a.flusherWG.Add(1)
		go func() { defer a.flusherWG.Done(); a.sink.runFlusher(a.flusherDone) }()
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	requests := a.server.Requests
	go func() {
		for key := range a.presenter.Keys() {
			a.events <- actorEvent{kind: keyEvent, result: processResult{Err: errors.New(key)}}
		}
	}()
	if a.cfg.BuildCommand != "" {
		if err := a.startBuild(); err != nil {
			return 1, err
		}
	} else if err := a.startRun(); err != nil {
		return 1, err
	}
	a.presenter.Render(a.model)
	for !a.finished {
		select {
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			if req != nil {
				a.handleControl(req)
			}
		case ev := <-a.events:
			a.handleEvent(ev)
		case sig := <-signals:
			switch sig {
			case syscall.SIGINT:
				a.beginForce(130)
			case syscall.SIGTERM:
				a.beginForce(143)
			}
		}
	}
	return a.retCode, nil
}

func (a *actor) env() []string {
	env := os.Environ()
	if a.cfg.IDEnv != "" {
		env = append(env, a.cfg.IDEnv+"="+a.id)
	}
	return env
}

func (a *actor) startBuild() error {
	a.model.State = Building
	if a.cfg.BuildCommand == "" {
		return a.startRun()
	}
	proc, err := startProcess([]string{a.cfg.BuildCommand}, true, a.env(), a.cfg.Stdin, a.sink, a.cfg.Headless)
	if err != nil {
		a.transition(Event{Kind: BuildFailedEvent, Reason: err.Error()})
		a.report("build failed: " + err.Error())
		a.failRequests(err.Error())
		return nil
	}
	a.build = proc
	a.allProcesses = append(a.allProcesses, proc)
	go func() { a.events <- actorEvent{kind: buildDoneEvent, proc: proc, result: <-proc.done} }()
	a.presenter.Render(a.model)
	return nil
}

func (a *actor) startRun() error {
	proc, err := startProcess(a.cfg.RunArgs, false, a.env(), a.cfg.Stdin, a.sink, a.cfg.Headless)
	if err != nil {
		a.report("run failed: " + err.Error())
		a.failRequests(err.Error())
		a.finished = true
		a.retCode = 1
		return nil
	}
	a.child = proc
	a.allProcesses = append(a.allProcesses, proc)
	a.childResult = nil
	a.childProcessed = false
	go func() { a.events <- actorEvent{kind: runDoneEvent, proc: proc, result: <-proc.done} }()
	a.transition(Event{Kind: ChildStartedEvent, PID: proc.pid})
	a.presenter.Render(a.model)
	a.succeedRequests()
	return nil
}

func (a *actor) handleEvent(ev actorEvent) {
	switch ev.kind {
	case buildDoneEvent:
		if ev.proc != a.build {
			return
		}
		a.build = nil
		if a.model.BuildQueued {
			alive := a.liveQueued()
			if len(alive) == 0 {
				a.model.BuildQueued = false
			} else {
				a.pendingRequests = append(a.pendingRequests, alive...)
				a.queuedRequests = nil
			}
		}
		if ev.result.Code == 0 {
			_, effects := a.transition(Event{Kind: BuildSucceededEvent})
			if containsEffect(effects, StartBuildEffect) {
				if err := a.startBuild(); err != nil {
					a.failRequests(err.Error())
				}
			} else if a.model.State == Running {
				if err := a.startRun(); err != nil {
					a.failRequests(err.Error())
				}
			}
		} else {
			_, effects := a.transition(Event{Kind: BuildFailedEvent, Reason: fmt.Sprintf("build exited with status %d", ev.result.Code)})
			if containsEffect(effects, StartBuildEffect) {
				if err := a.startBuild(); err != nil {
					a.failRequests(err.Error())
				}
			} else {
				a.report(fmt.Sprintf("build failed (exit %d)", ev.result.Code))
				a.failRequests(fmt.Sprintf("build failed (exit %d)", ev.result.Code))
			}
		}
		a.presenter.Render(a.model)
	case runDoneEvent:
		if ev.proc != a.child || a.childProcessed {
			return
		}
		a.childResult = &ev.result
		a.childProcessed = true
		if a.model.State == Stopping {
			if a.stopAccepted {
				a.completeChildExit()
			} else {
				a.model.Message = "child exited; waiting for stop result"
				a.presenter.Render(a.model)
			}
			return
		}
		a.completeChildExit()
	case stopDoneEvent:
		a.stop = nil
		a.stopRunning = false
		if ev.err != nil {
			reason := "stop-cmd failed: " + ev.err.Error()
			a.observeChildExit()
			a.report(reason)
			if a.childResult != nil {
				_, effects := a.transition(Event{Kind: StopCommandFailEvent, Reason: reason, ChildExited: true})
				a.handleEffects(effects)
			} else {
				a.transition(Event{Kind: StopCommandFailEvent, Reason: reason})
				a.failRequests(reason)
			}
		} else {
			a.stopAccepted = true
			a.transition(Event{Kind: StopCommandOKEvent})
			if a.childResult != nil {
				a.completeChildExit()
			}
		}
		a.presenter.Render(a.model)
	case forceDoneEvent:
		if ev.forced {
			a.forceRunning = false
			if ev.err != nil {
				a.report("failed to stop process group: " + ev.err.Error())
			}
			if a.model.State == Exiting {
				a.finish(a.forceCode)
			}
			if a.model.State == Stopping && a.model.Intent == IntentExit {
				if a.child == nil {
					a.model.State = Exiting
					a.finish(0)
				} else {
					a.stopAccepted = true
					if a.childResult != nil {
						a.completeChildExit()
					}
				}
			}
		} else {
			a.groupStopRunning = false
			if a.model.State == Stopping {
				a.stopAccepted = true
				if a.childResult != nil {
					a.completeChildExit()
				}
			}
		}
	case keyEvent:
		a.handleKey(ev.result.Err.Error())
	}
}

func (a *actor) handleKey(key string) {
	if key == "ctrl+c" {
		a.beginForce(130)
		return
	}
	wasStopping := a.model.State == Stopping && a.model.StopAccepted && key == "esc"
	_, effects := a.transition(Event{Kind: KeyEvent, Key: key})
	if wasStopping && a.model.State == Running {
		a.stopAccepted = false
	}
	a.handleEffects(effects)
	a.presenter.Render(a.model)
}

func (a *actor) handleEffects(effects []Effect) {
	for _, effect := range effects {
		switch effect.Kind {
		case StartBuildEffect:
			if err := a.startBuild(); err != nil {
				a.failRequests(err.Error())
			}
		case BeginStopEffect:
			a.beginStop()
		case ForceStopEffect:
			a.beginForceStop()
		case ExitEffect:
			if !a.forceRunning {
				a.finish(a.model.ExitCode)
			}
		case ControlRejectEffect:
			a.failRequests(effect.Reason)
		case MessageEffect:
			a.report(effect.Reason)
		}
	}
}

func (a *actor) transition(e Event) (Model, []Effect) {
	var effects []Effect
	a.model, effects = Update(a.model, e)
	return a.model, effects
}

func (a *actor) handleControl(req *control.Request) {
	if req.Command == control.Status {
		req.Respond(a.statusResponse())
		return
	}
	if a.model.State == Exiting || a.finished {
		req.Respond(control.Response{OK: false, Reason: "runner exiting"})
		return
	}
	if a.model.State == Running && a.child != nil && a.child.finished.Load() && !a.childProcessed {
		a.consumeChildExit()
	}
	if a.model.State == Exiting || !req.Alive() {
		if a.model.State == Exiting {
			req.Respond(control.Response{OK: false, Reason: "child has exited"})
		}
		return
	}
	switch a.model.State {
	case Building:
		a.queuedRequests = append(a.queuedRequests, req)
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
	case BuildFailed:
		if !req.Alive() {
			return
		}
		a.pendingRequests = append(a.pendingRequests, req) // build start is the commit point
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
		if containsEffect(effects, StartBuildEffect) {
			if err := a.startBuild(); err != nil {
				a.failRequests(err.Error())
			}
		}
	case Running:
		if a.child != nil && a.child.finished.Load() && !a.childProcessed {
			a.consumeChildExit()
			if a.model.State == Exiting {
				req.Respond(control.Response{OK: false, Reason: "child has exited"})
				return
			}
		}
		if !req.Alive() {
			return
		}
		a.pendingRequests = append(a.pendingRequests, req) // stop-cmd / TERM is the commit point
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
		if containsEffect(effects, BeginStopEffect) {
			a.presenter.Render(a.model)
		}
	case Stopping:
		if !req.Alive() {
			return
		}
		a.pendingRequests = append(a.pendingRequests, req)
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
		a.presenter.Render(a.model)
	default:
		req.Respond(control.Response{OK: false, Reason: "runner unavailable"})
	}
}

func (a *actor) statusResponse() control.Response {
	queuedAlive := false
	for _, req := range a.queuedRequests {
		if req.Alive() {
			queuedAlive = true
			break
		}
	}
	response := control.Response{
		OK: true, ID: a.id, State: string(a.model.State), PID: nil,
		Generation:     a.model.Generation,
		RestartPending: queuedAlive || len(a.pendingRequests) > 0 || (a.model.State == Stopping && a.model.Intent == IntentRestart),
	}
	if a.model.State == Running && a.model.PID > 0 {
		pid := a.model.PID
		response.PID = &pid
	}
	return response
}

func (a *actor) liveQueued() []*control.Request {
	live := make([]*control.Request, 0, len(a.queuedRequests))
	for _, req := range a.queuedRequests {
		if req.Alive() {
			live = append(live, req)
		} else {
			req.Respond(control.Response{OK: false, Reason: "request disconnected before build"})
		}
	}
	return live
}

func (a *actor) beginStop() {
	if a.model.PID == 0 || a.child == nil {
		if a.model.Intent == IntentRestart {
			a.transition(Event{Kind: ChildExitedEvent})
		} else {
			a.finish(0)
		}
		return
	}
	if a.cfg.StopCommand != "" {
		proc, err := startProcess([]string{a.cfg.StopCommand}, true, a.env(), a.cfg.Stdin, a.sink, a.cfg.Headless)
		if err != nil {
			a.transition(Event{Kind: StopCommandFailEvent, Reason: err.Error()})
			a.report("stop-cmd failed: " + err.Error())
			a.failRequests("stop-cmd failed: " + err.Error())
			return
		}
		a.stop = proc
		a.allProcesses = append(a.allProcesses, proc)
		a.stopRunning = true
		go func() { a.events <- actorEvent{kind: stopDoneEvent, err: a.waitStopCommand(proc)} }()
		return
	}
	a.beginGroupStop(a.child)
}

func (a *actor) beginGroupStop(p *process) {
	if p == nil {
		return
	}
	if err := signalGroup(p.pid, syscall.SIGTERM); err != nil {
		a.report("SIGTERM: " + err.Error())
	}
	a.groupStopRunning = true
	go func() {
		if a.cfg.TermGrace > 0 {
			timer := time.NewTimer(a.cfg.TermGrace)
			<-timer.C
			timer.Stop()
		}
		var killErr error
		if groupExists(p.pid) {
			killErr = signalGroup(p.pid, syscall.SIGKILL)
		}
		<-p.done
		a.events <- actorEvent{kind: forceDoneEvent, proc: p, err: killErr}
	}()
}

func (a *actor) waitStopCommand(proc *process) error {
	timer := time.NewTimer(a.cfg.StopCommandTimeout)
	defer timer.Stop()
	select {
	case result := <-proc.done:
		if result.Code != 0 {
			return fmt.Errorf("exit status %d", result.Code)
		}
		return nil
	case <-timer.C:
		_ = signalGroup(proc.pid, syscall.SIGTERM)
		grace := time.NewTimer(a.cfg.TermGrace)
		<-grace.C
		grace.Stop()
		if groupExists(proc.pid) {
			_ = signalGroup(proc.pid, syscall.SIGKILL)
		}
		<-proc.done
		return errors.New("timeout")
	}
}

func (a *actor) beginForceStop() {
	// Used for a pending build canceled by Q, or another process being shut down.
	procs := make([]*process, 0, 3)
	for _, p := range []*process{a.build, a.child, a.stop} {
		if p != nil {
			procs = append(procs, p)
		}
	}
	a.forceRunning = true
	if len(procs) == 0 {
		a.events <- actorEvent{kind: forceDoneEvent, forced: true}
		return
	}
	for _, p := range procs {
		_ = signalGroup(p.pid, syscall.SIGTERM)
	}
	go func() {
		if a.cfg.TermGrace > 0 {
			timer := time.NewTimer(a.cfg.TermGrace)
			<-timer.C
			timer.Stop()
		}
		for _, p := range procs {
			if groupExists(p.pid) {
				_ = signalGroup(p.pid, syscall.SIGKILL)
			}
		}
		for _, p := range procs {
			<-p.done
		}
		a.events <- actorEvent{kind: forceDoneEvent, forced: true}
	}()
}

func (a *actor) beginForce(code int) {
	if a.finished {
		return
	}
	if a.forceRunning {
		a.forceCode = code
		a.model, _ = Update(a.model, Event{Kind: ForceEvent, SignalCode: code})
		a.failRequests("runner interrupted")
		return
	}
	a.forceCode = code
	a.model, _ = Update(a.model, Event{Kind: ForceEvent, SignalCode: code})
	a.failRequests("runner interrupted")
	a.queuedRequests = nil
	a.beginForceStop()
	a.presenter.Render(a.model)
}

func (a *actor) consumeChildExit() {
	if a.child == nil || a.childProcessed {
		return
	}
	result := a.child.result
	a.childResult = &result
	a.childProcessed = true
	a.completeChildExit()
}

// observeChildExit synchronizes with the Wait goroutine's finished marker and
// captures the result before stop-cmd completion is interpreted.
func (a *actor) observeChildExit() {
	if a.child == nil || a.childProcessed || !a.child.finished.Load() {
		return
	}
	result := a.child.result
	a.childResult = &result
	a.childProcessed = true
}

func (a *actor) completeChildExit() {
	if a.child == nil || !a.childProcessed {
		return
	}
	a.transition(Event{Kind: ChildExitedEvent})
	a.stopAccepted = false
	switch a.model.State {
	case Building:
		if err := a.startBuild(); err != nil {
			a.failRequests(err.Error())
		}
	case Exiting:
		a.finish(a.model.ExitCode)
	}
	a.presenter.Render(a.model)
}

func (a *actor) succeedRequests() {
	for _, req := range a.pendingRequests {
		req.Respond(control.Response{OK: true, ID: a.id, State: string(Running), PID: intPointer(a.model.PID), Generation: a.model.Generation})
	}
	a.pendingRequests = nil
	for _, req := range a.queuedRequests {
		req.Respond(control.Response{OK: true, ID: a.id, State: string(Running), PID: intPointer(a.model.PID), Generation: a.model.Generation})
	}
	a.queuedRequests = nil
}

func (a *actor) failRequests(reason string) {
	for _, req := range a.pendingRequests {
		req.Respond(control.Response{OK: false, Reason: reason})
	}
	a.pendingRequests = nil
	for _, req := range a.queuedRequests {
		req.Respond(control.Response{OK: false, Reason: reason})
	}
	a.queuedRequests = nil
}

func (a *actor) finish(code int) {
	if a.finished {
		return
	}
	if a.child != nil && !a.child.finished.Load() {
		a.report("子が生きたまま終了しようとしたため、runner の終了を拒否しました")
		a.model.State = Running
		a.model.PID = a.child.pid
		a.model.Intent = IntentNone
		a.model.StopAccepted = false
		a.model.Confirm = ConfirmNone
		a.model.Message = "child is still running; exit refused"
		a.stopAccepted = false
		a.failRequests("runner exit refused while child is alive")
		a.presenter.Render(a.model)
		return
	}
	a.finished = true
	a.retCode = code
	if code == 0 {
		a.model.ExitCode = 0
	}
}

func (a *actor) report(message string) {
	if message == "" {
		return
	}
	_, _ = fmt.Fprintln(a.cfg.Stderr, termsafe.DetailLine(message))
}

func (a *actor) drainOutputs() {
	for _, proc := range a.allProcesses {
		select {
		case <-proc.outputEnd:
		case <-time.After(a.outputDrainTimeout):
			// A descendant can keep the shared pipe open after the leader exits.
			// Only kill descendants after their group leader has been reaped. A
			// live leader is an application process and must never be killed by
			// output cleanup.
			if !proc.finished.Load() {
				a.report("子が生きたまま終了しようとしたため、出力回収による kill を拒否しました")
				continue
			}
			_ = signalGroup(proc.pid, syscall.SIGKILL)
			proc.closeOutput()
			select {
			case <-proc.outputEnd:
			case <-time.After(time.Second):
				a.report("timed out collecting child output")
			}
		}
	}
}

func intPointer(n int) *int { return &n }

func containsEffect(effects []Effect, kind EffectKind) bool {
	for _, effect := range effects {
		if effect.Kind == kind {
			return true
		}
	}
	return false
}
