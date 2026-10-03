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
	BuildCommand        string
	RunArgs             []string
	StopCommand         string
	StopCommandTimeout  time.Duration
	TermGrace           time.Duration
	ReadyCommand        string
	ReadyTimeout        time.Duration
	ReadyInterval       time.Duration
	ReadyAttemptTimeout time.Duration
	ReadyCleanupGrace   time.Duration
	// OutputDrainTimeout は終了時に子の出力が閉じるのを待つ上限。0 なら 500ms (テストが手順の窓を広げるための差し込み口)。
	OutputDrainTimeout time.Duration
	IDEnv              string
	ControlPath        string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	Headless           bool
	StdinIsTerminal    bool
	// StdoutIsTerminal は UI の有無と別に、子の出力を無害化するかを決める (logSink.CopyFrom)。
	StdoutIsTerminal bool
	Presenter        Presenter
}

type actorEventKind string

const (
	buildDoneEvent           actorEventKind = "build-done"
	runDoneEvent             actorEventKind = "run-done"
	stopDoneEvent            actorEventKind = "stop-done"
	forceDoneEvent           actorEventKind = "force-done"
	keyEvent                 actorEventKind = "key"
	readyProbeDoneEvent      actorEventKind = "ready-probe-done"
	readyProbeTimeoutEvent   actorEventKind = "ready-probe-timeout"
	readyOverallTimeoutEvent actorEventKind = "ready-overall-timeout"
	readyIntervalEvent       actorEventKind = "ready-interval"
	readyCleanupDoneEvent    actorEventKind = "ready-cleanup-done"
	crashCleanupDoneEvent    actorEventKind = "crash-cleanup-done"
)

type actorEvent struct {
	kind       actorEventKind
	proc       *process
	result     processResult
	err        error
	forced     bool
	generation uint64
	token      uint64
	outcome    readyCleanupOutcome
}

type readyCleanupOutcome string

const (
	readyCleanupCompleted readyCleanupOutcome = "completed"
	readyCleanupTimedOut  readyCleanupOutcome = "timed-out"
	readyCleanupCancelled readyCleanupOutcome = "cancelled"
)

type readyCleanupRequest struct {
	proc       *process
	token      uint64
	generation uint64
	outcome    readyCleanupOutcome
}

// Presenter is the boundary for the next milestone's Bubble Tea terminal UI.
// It receives model snapshots and key events; process management never draws.
type Presenter interface {
	Render(Model)
	Keys() <-chan string
	Close() error
}

type presenterStarter interface{ Start() error }
type linePrinter interface{ Println(string) }

type headlessPresenter struct{}

func (headlessPresenter) Render(Model)        {}
func (headlessPresenter) Keys() <-chan string { return nil }
func (headlessPresenter) Close() error        { return nil }

type actor struct {
	cfg                  Config
	model                Model
	id                   string
	server               *control.Server
	sink                 *logSink
	presenter            Presenter
	events               chan actorEvent
	build                *process
	child                *process
	stop                 *process
	allProcesses         []*process
	queuedRequests       []*control.Request
	pendingRequests      []*control.Request
	childResult          *processResult
	childProcessed       bool
	stopAccepted         bool
	stopRunning          bool
	forceRunning         bool
	forcedTermination    bool
	groupStopRunning     bool
	outputDrainTimeout   time.Duration
	forceCode            int
	finished             bool
	retCode              int
	flusherDone          chan struct{}
	actorDone            chan struct{}
	signals              <-chan os.Signal
	flusherWG            sync.WaitGroup
	readyProbe           *process
	readyCleaningProc    *process
	readyCleanupQueue    []readyCleanupRequest
	readyToken           uint64
	readyGeneration      uint64
	readyStarted         time.Time
	readyDeadlineTimer   *time.Timer
	readyAttemptTimer    *time.Timer
	readyIntervalTimer   *time.Timer
	readyExpired         bool
	readyFinishPending   bool
	readyFinishCode      int
	childExitPending     bool
	readyDeferredEffects []Effect
	readyResumePending   bool
	timer                transitionTimer
	// crashedChild は Crashed に入った子。再ビルドの前にそのプロセスグループの残り (孫) を片付ける。
	// Q で終えるときは片付けない (自然終了で孫を消さないのと同じ。issue 586)
	crashedChild           *process
	crashCleanupRunning    bool
	buildAfterCrashCleanup bool
}

// Run supervises one foreground process. It returns a process-style exit code.
func Run(cfg Config) (int, error) {
	if len(cfg.RunArgs) == 0 {
		return 1, errors.New("missing command after --")
	}
	if cfg.StopCommandTimeout <= 0 {
		cfg.StopCommandTimeout = 5 * time.Second
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 120 * time.Second
	}
	if cfg.ReadyInterval <= 0 {
		cfg.ReadyInterval = 500 * time.Millisecond
	}
	if cfg.ReadyAttemptTimeout <= 0 {
		cfg.ReadyAttemptTimeout = 5 * time.Second
	}
	if cfg.ReadyCleanupGrace <= 0 {
		cfg.ReadyCleanupGrace = 100 * time.Millisecond
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
	if _, ok := cfg.Stdin.(*os.File); !ok {
		cfg.Stdin = &serializedReader{reader: cfg.Stdin}
	}
	if cfg.ControlPath == "" {
		return 1, errors.New("control socket path is empty")
	}
	// Register signals before the presenter starts Bubble Tea, which switches a
	// TTY to raw mode. A signal delivered during startup remains queued for actor.
	signals := make(chan os.Signal, 6)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(signals)
	pipeSignals := make(chan os.Signal, 1)
	pipeDrainDone := make(chan struct{})
	signal.Notify(pipeSignals, syscall.SIGPIPE)
	go func() {
		for {
			select {
			case <-pipeSignals:
			case <-pipeDrainDone:
				return
			}
		}
	}()
	defer signal.Stop(pipeSignals)
	defer close(pipeDrainDone)
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
	if starter, ok := presenter.(presenterStarter); ok {
		if err := starter.Start(); err != nil {
			_ = presenter.Close()
			_ = server.Close()
			return 1, fmt.Errorf("start terminal UI: %w", err)
		}
	}
	var printLine func(string)
	if printer, ok := presenter.(linePrinter); ok && !cfg.Headless {
		printLine = printer.Println
	}
	sink := newLogSink(cfg.Stdout, cfg.Headless, printLine)
	sink.terminalOutput = cfg.StdoutIsTerminal
	sink.onOutputFailure = func() { reportOutputFailure(cfg.Stderr) }
	return (&actor{cfg: cfg, model: InitialModel(), id: id, server: server,
		sink: sink, presenter: presenter,
		events: make(chan actorEvent, 128), flusherDone: make(chan struct{}), actorDone: make(chan struct{}),
		outputDrainTimeout: outputDrainTimeout(cfg.OutputDrainTimeout), signals: signals}).run()
}

func newID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (a *actor) run() (int, error) {
	defer func() { _ = a.server.Close() }()
	defer func() { _ = a.presenter.Close() }()
	defer func() {
		close(a.actorDone)
		a.drainOutputs()
		close(a.flusherDone)
		a.flusherWG.Wait()
		a.sink.Flush()
	}()
	if !a.cfg.Headless {
		a.flusherWG.Add(1)
		go func() { defer a.flusherWG.Done(); a.sink.runFlusher(a.flusherDone) }()
	}
	requests := a.server.Requests()
	go forwardKeys(a.actorDone, a.presenter.Keys(), a.events)
	if a.model.Transition.Active {
		a.timer.begin() // 起動の板は InitialModel で開いていて、transition を通らない
	}
	if a.cfg.BuildCommand != "" {
		a.handleEffects([]Effect{{Kind: StartBuildEffect}})
	} else if err := a.startRun(); err != nil {
		return 1, err
	}
	a.presenter.Render(a.model)
	for !a.finished {
		select {
		case req := <-requests: // 要求の列は閉じない (受信専用で渡るので外からも閉じられない = issue 600)
			if req != nil {
				a.handleControl(req)
			}
		case ev := <-a.events:
			a.handleEvent(ev)
		case sig := <-a.signals:
			switch sig {
			case syscall.SIGINT:
				a.beginForce(130)
			case syscall.SIGTERM:
				a.beginForce(143)
			case syscall.SIGHUP:
				a.beginForce(129)
			case syscall.SIGQUIT:
				a.beginForce(131)
			}
		}
	}
	return a.retCode, nil
}

func forwardKeys(done <-chan struct{}, keys <-chan string, events chan<- actorEvent) {
	for {
		select {
		case <-done:
			return
		case key, ok := <-keys:
			if !ok {
				return
			}
			select {
			case events <- actorEvent{kind: keyEvent, result: processResult{Err: errors.New(key)}}:
			case <-done:
				return
			}
		}
	}
}

func (a *actor) env() []string {
	env := os.Environ()
	if a.cfg.IDEnv != "" {
		env = append(env, a.cfg.IDEnv+"="+a.id)
	}
	return env
}

func (a *actor) post(ev actorEvent) {
	if a.actorDone == nil {
		a.events <- ev
		return
	}
	select {
	case a.events <- ev:
	case <-a.actorDone:
	}
}

func (a *actor) stopReadyTimers() {
	for _, timer := range []*time.Timer{a.readyDeadlineTimer, a.readyAttemptTimer, a.readyIntervalTimer} {
		if timer != nil {
			timer.Stop()
		}
	}
	a.readyDeadlineTimer = nil
	a.readyAttemptTimer = nil
	a.readyIntervalTimer = nil
}

func (a *actor) startReadyCheck() {
	a.stopReadyTimers()
	a.readyToken++
	a.readyGeneration = a.model.Generation
	a.readyStarted = time.Now()
	a.readyExpired = false
	token, generation := a.readyToken, a.readyGeneration
	a.readyDeadlineTimer = time.AfterFunc(a.cfg.ReadyTimeout, func() {
		a.post(actorEvent{kind: readyOverallTimeoutEvent, token: token, generation: generation})
	})
	a.startReadyProbe(token, generation)
}

func (a *actor) readySessionCurrent(token, generation uint64) bool {
	return token == a.readyToken && generation == a.readyGeneration &&
		a.model.Generation == generation && a.model.State == Running &&
		a.model.Transition.Active && a.model.Transition.Stage == TransitionReady
}

func (a *actor) startReadyProbe(token, generation uint64) {
	if !a.readySessionCurrent(token, generation) || a.readyExpired {
		return
	}
	if time.Since(a.readyStarted) >= a.cfg.ReadyTimeout {
		a.readyExpired = true
		a.finishReadyFailure(generation)
		return
	}
	proc, err := startProcess([]string{a.cfg.ReadyCommand}, true, a.env(), a.cfg.Stdin, a.sink, a.cfg.StdinIsTerminal)
	if err != nil {
		a.scheduleReadyProbe(token, generation)
		return
	}
	a.readyProbe = proc
	a.trackProcess(proc)
	a.readyAttemptTimer = time.AfterFunc(a.cfg.ReadyAttemptTimeout, func() {
		a.post(actorEvent{kind: readyProbeTimeoutEvent, proc: proc, token: token, generation: generation})
	})
	go func() {
		a.post(actorEvent{kind: readyProbeDoneEvent, proc: proc, result: proc.wait(), token: token, generation: generation})
	}()
}

func (a *actor) scheduleReadyProbe(token, generation uint64) {
	if !a.readySessionCurrent(token, generation) || a.readyExpired {
		return
	}
	a.readyIntervalTimer = time.AfterFunc(a.cfg.ReadyInterval, func() {
		a.post(actorEvent{kind: readyIntervalEvent, token: token, generation: generation})
	})
}

func (a *actor) startReadyCleanup(proc *process, token, generation uint64, outcome readyCleanupOutcome) {
	if proc == nil || proc == a.readyCleaningProc {
		return
	}
	if a.readyProbe == proc {
		a.readyProbe = nil
		if a.readyAttemptTimer != nil {
			a.readyAttemptTimer.Stop()
			a.readyAttemptTimer = nil
		}
	}
	for _, pending := range a.readyCleanupQueue {
		if pending.proc == proc {
			return
		}
	}
	request := readyCleanupRequest{proc: proc, token: token, generation: generation, outcome: outcome}
	if a.readyCleaningProc != nil {
		a.readyCleanupQueue = append(a.readyCleanupQueue, request)
		return
	}
	a.runReadyCleanup(request)
}

func (a *actor) runReadyCleanup(request readyCleanupRequest) {
	proc := request.proc
	if a.readyAttemptTimer != nil {
		a.readyAttemptTimer.Stop()
		a.readyAttemptTimer = nil
	}
	a.readyCleaningProc = proc
	grace := a.cfg.ReadyCleanupGrace
	go func() {
		var cleanupErr error
		if groupExists(proc.pid) {
			if err := signalGroup(proc.pid, syscall.SIGTERM); err != nil {
				cleanupErr = err
			}
			waitForTermBoundary([]*process{proc}, grace)
			if err := killRemainingGroups([]*process{proc}); err != nil && cleanupErr == nil {
				cleanupErr = err
			}
		}
		result := proc.wait()
		a.post(actorEvent{kind: readyCleanupDoneEvent, proc: proc, result: result, err: cleanupErr,
			token: request.token, generation: request.generation, outcome: request.outcome})
	}()
}

func (a *actor) startNextReadyCleanup() {
	if a.readyCleaningProc != nil || len(a.readyCleanupQueue) == 0 {
		return
	}
	request := a.readyCleanupQueue[0]
	a.readyCleanupQueue = a.readyCleanupQueue[1:]
	a.runReadyCleanup(request)
}

func (a *actor) handleReadyProbeDone(ev actorEvent) {
	if ev.proc != a.readyProbe || !a.readySessionCurrent(ev.token, ev.generation) {
		return
	}
	a.startReadyCleanup(ev.proc, ev.token, ev.generation, readyCleanupCompleted)
}

func (a *actor) handleReadyProbeTimeout(ev actorEvent) {
	if ev.proc != a.readyProbe || !a.readySessionCurrent(ev.token, ev.generation) {
		return
	}
	if ev.proc.finished.Load() {
		a.handleReadyProbeDone(actorEvent{proc: ev.proc, result: ev.proc.result, token: ev.token, generation: ev.generation})
		return
	}
	a.startReadyCleanup(ev.proc, ev.token, ev.generation, readyCleanupTimedOut)
}

func (a *actor) handleReadyOverallTimeout(ev actorEvent) {
	if !a.readySessionCurrent(ev.token, ev.generation) {
		return
	}
	a.readyExpired = true
	if a.readyIntervalTimer != nil {
		a.readyIntervalTimer.Stop()
		a.readyIntervalTimer = nil
	}
	if a.readyProbe != nil {
		a.startReadyCleanup(a.readyProbe, ev.token, ev.generation, readyCleanupTimedOut)
		return
	}
	if a.readyCleaningProc == nil {
		a.finishReadyFailure(ev.generation)
	}
}

func (a *actor) handleReadyInterval(ev actorEvent) {
	if !a.readySessionCurrent(ev.token, ev.generation) {
		return
	}
	a.readyIntervalTimer = nil
	if time.Since(a.readyStarted) >= a.cfg.ReadyTimeout {
		a.readyExpired = true
		a.finishReadyFailure(ev.generation)
		return
	}
	a.startReadyProbe(ev.token, ev.generation)
}

func (a *actor) handleReadyCleanupDone(ev actorEvent) {
	if ev.proc != a.readyCleaningProc {
		return
	}
	a.readyCleaningProc = nil
	if ev.err != nil {
		a.report("ready-cmd process group cleanup: " + ev.err.Error())
	}
	a.startNextReadyCleanup()
	if a.readySessionCurrent(ev.token, ev.generation) {
		if time.Since(a.readyStarted) >= a.cfg.ReadyTimeout {
			a.readyExpired = true
		}
		if a.readyExpired {
			a.finishReadyFailure(ev.generation)
		} else if ev.outcome == readyCleanupCompleted && ev.result.Code == 0 {
			a.finishReadySuccess(ev.generation)
		} else {
			a.scheduleReadyProbe(ev.token, ev.generation)
		}
	}
	if a.childExitPending {
		a.childExitPending = false
		effects := a.readyDeferredEffects
		a.readyDeferredEffects = nil
		a.handleEffects(effects)
		a.presenter.Render(a.model)
	}
	if a.readyResumePending && a.model.State == Running && !a.model.Ready {
		a.readyResumePending = false
		a.startReadyCheck()
		a.presenter.Render(a.model)
	}
	if a.readyFinishPending {
		code := a.readyFinishCode
		a.readyFinishPending = false
		a.finish(code)
	}
}

func (a *actor) resumeReadyCheckIfNeeded() {
	if a.cfg.ReadyCommand == "" || a.model.State != Running || a.model.Ready || a.child == nil || a.child.finished.Load() {
		return
	}
	a.transition(Event{Kind: ReadyCheckResumedEvent})
	if a.readyCleaningProc != nil {
		a.readyResumePending = true
		return
	}
	a.startReadyCheck()
	a.presenter.Render(a.model)
}

func (a *actor) finishReadySuccess(generation uint64) {
	a.stopReadyTimers()
	a.transition(Event{Kind: ReadySucceededEvent, Generation: generation})
	if a.cfg.Headless {
		a.reportStderr("起動を確認しました")
	}
	a.presenter.Render(a.model)
}

func (a *actor) finishReadyFailure(generation uint64) {
	if a.readyDeadlineTimer != nil {
		a.readyDeadlineTimer.Stop()
		a.readyDeadlineTimer = nil
	}
	if a.readyIntervalTimer != nil {
		a.readyIntervalTimer.Stop()
		a.readyIntervalTimer = nil
	}
	a.transition(Event{Kind: ReadyFailedEvent, Generation: generation})
	if a.cfg.Headless {
		a.reportStderr("起動を確認できませんでした")
	}
	a.presenter.Render(a.model)
}

func (a *actor) cancelReady() {
	a.stopReadyTimers()
	oldToken := a.readyToken
	a.readyToken++
	a.readyExpired = false
	a.readyResumePending = false
	if a.readyProbe != nil {
		a.startReadyCleanup(a.readyProbe, oldToken, a.readyGeneration, readyCleanupCancelled)
	}
}

func (a *actor) invalidateReadyForForce() {
	a.stopReadyTimers()
	a.readyToken++
	a.readyExpired = false
}

func (a *actor) startBuild() error {
	if a.build != nil {
		a.report("build start refused: a build is already running")
		return nil
	}
	if a.cfg.BuildCommand == "" {
		return a.startRun()
	}
	proc, err := startProcess([]string{a.cfg.BuildCommand}, true, a.env(), a.cfg.Stdin, a.sink, a.cfg.StdinIsTerminal)
	if err != nil {
		a.model.BuildQueued = false
		a.transition(Event{Kind: BuildFailedEvent, Reason: err.Error()})
		a.report("build failed: " + err.Error())
		a.failRequests(err.Error())
		return nil
	}
	a.build = proc
	a.trackProcess(proc)
	go func() { a.events <- actorEvent{kind: buildDoneEvent, proc: proc, result: proc.wait()} }()
	a.presenter.Render(a.model)
	return nil
}

func (a *actor) startRun() error {
	a.transition(Event{Kind: LaunchStartedEvent})
	// startProcess は exec の成否まで待つ (作り直した実行ファイルは macOS の初回検査で待つことがある)。その間「起動」の段を見せる。
	a.presenter.Render(a.model)
	proc, err := startProcess(a.cfg.RunArgs, false, a.env(), a.cfg.Stdin, a.sink, a.cfg.StdinIsTerminal)
	if err != nil {
		a.report("run failed: " + err.Error())
		a.failRequests(err.Error())
		a.model.Transition.Active = false
		a.finished = true
		a.retCode = 1
		return nil
	}
	a.child = proc
	a.trackProcess(proc)
	a.childResult = nil
	a.childProcessed = false
	go func() { a.events <- actorEvent{kind: runDoneEvent, proc: proc, result: proc.wait()} }()
	a.transition(Event{Kind: ChildStartedEvent, PID: proc.pid, ReadyCheck: a.cfg.ReadyCommand != ""})
	a.presenter.Render(a.model)
	a.succeedRequests()
	if a.cfg.ReadyCommand != "" {
		a.startReadyCheck()
		a.presenter.Render(a.model)
	}
	return nil
}

func (a *actor) handleEvent(ev actorEvent) {
	switch ev.kind {
	case buildDoneEvent:
		if ev.proc != a.build {
			return
		}
		a.build = nil
		if a.model.State == Exiting {
			// A build killed by a confirmed human quit is not a build failure.
			return
		}
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
				a.handleEffects(effects)
			} else if a.model.State == Running {
				if err := a.startRun(); err != nil {
					a.failRequests(err.Error())
				}
			}
		} else {
			_, effects := a.transition(Event{Kind: BuildFailedEvent, Reason: fmt.Sprintf("build exited with status %d", ev.result.Code)})
			if containsEffect(effects, StartBuildEffect) {
				a.handleEffects(effects)
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
				a.model.Message = "アプリは終了しました。停止コマンドの結果を待っています"
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
				a.failRequests(reason)
				a.handleEffects(effects)
			} else {
				a.transition(Event{Kind: StopCommandFailEvent, Reason: reason})
				a.failRequests(reason)
				a.resumeReadyCheckIfNeeded()
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
		a.handleForceDone(ev)
	case crashCleanupDoneEvent:
		a.handleCrashCleanupDone(ev)
	case readyProbeDoneEvent:
		a.handleReadyProbeDone(ev)
	case readyProbeTimeoutEvent:
		a.handleReadyProbeTimeout(ev)
	case readyOverallTimeoutEvent:
		a.handleReadyOverallTimeout(ev)
	case readyIntervalEvent:
		a.handleReadyInterval(ev)
	case readyCleanupDoneEvent:
		a.handleReadyCleanupDone(ev)
	case keyEvent:
		a.handleKey(ev.result.Err.Error())
	}
}

func (a *actor) handleForceDone(ev actorEvent) {
	if ev.forced {
		a.forceRunning = false
		a.readyProbe = nil
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
		return
	}
	a.groupStopRunning = false
	if a.model.State == Stopping {
		a.stopAccepted = true
		if a.childResult != nil {
			a.completeChildExit()
		}
	}
}

func (a *actor) handleKey(key string) {
	if key == "ctrl+c" {
		a.beginForce(130)
		return
	}
	if a.model.State == Running && a.child != nil && a.child.finished.Load() && !a.childProcessed {
		a.consumeChildExit()
		if a.model.State == Exiting || a.finished {
			return
		}
	}
	wasStopping := a.model.State == Stopping && a.model.StopAccepted && key == "esc"
	_, effects := a.transition(Event{Kind: KeyEvent, Key: key})
	if wasStopping && a.model.State == Running {
		a.stopAccepted = false
		a.resumeReadyCheckIfNeeded()
	}
	a.handleEffects(effects)
	a.presenter.Render(a.model)
}

func (a *actor) handleEffects(effects []Effect) {
	for _, effect := range effects {
		switch effect.Kind {
		case StartBuildEffect:
			if a.deferBuildForCrashCleanup() {
				continue
			}
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
			if a.sink == nil || !a.sink.hasPrinter() {
				a.report(effect.Reason)
			}
		}
	}
}

func (a *actor) transition(e Event) (Model, []Effect) {
	var effects []Effect
	before := a.model.Transition
	a.model, effects = Update(a.model, e)
	if summary := a.timer.observe(before, a.model.Transition, a.model.State); summary != "" {
		a.model.Message = summary
	}
	return a.model, effects
}

func (a *actor) handleControl(req *control.Request) {
	if !req.Alive() {
		return
	}
	if req.Command == control.Status {
		if a.child != nil && a.child.finished.Load() && !a.childProcessed {
			a.consumeChildExit()
		}
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
		if a.crashCleanupRunning {
			// 片付けの後のビルドはまだ始まっていないので、次のビルドの要求は積まない (積むとビルドが 1 回増える)
			a.pendingRequests = append(a.pendingRequests, req) // build start is the commit point
			return
		}
		a.queuedRequests = append(a.queuedRequests, req)
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
	case BuildFailed, Crashed:
		if !req.Alive() {
			return
		}
		a.pendingRequests = append(a.pendingRequests, req) // build start is the commit point
		_, effects := a.transition(Event{Kind: ControlRestartEvent})
		a.handleEffects(effects)
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
		Ready:          a.model.Ready,
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
	a.cancelReady()
	if a.model.PID == 0 || a.child == nil {
		if a.model.Intent == IntentRestart {
			_, effects := a.transition(Event{Kind: ChildExitedEvent})
			a.handleEffects(effects)
		} else {
			a.finish(0)
		}
		return
	}
	if a.cfg.StopCommand != "" {
		a.model.Message = "停止コマンド実行中 (Ctrl-C で強制終了)"
		proc, err := startProcess([]string{a.cfg.StopCommand}, true, a.env(), a.cfg.Stdin, a.sink, a.cfg.StdinIsTerminal)
		if err != nil {
			a.transition(Event{Kind: StopCommandFailEvent, Reason: err.Error()})
			a.report("stop-cmd failed: " + err.Error())
			a.failRequests("stop-cmd failed: " + err.Error())
			a.resumeReadyCheckIfNeeded()
			return
		}
		a.stop = proc
		a.trackProcess(proc)
		a.stopRunning = true
		go func() { a.events <- actorEvent{kind: stopDoneEvent, err: a.waitStopCommand(proc)} }()
		return
	}
	a.model.Message = "終了待ち (Ctrl-C で強制終了)"
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
		waitForTermBoundary([]*process{p}, a.cfg.TermGrace)
		killErr := killRemainingGroups([]*process{p})
		p.wait()
		a.events <- actorEvent{kind: forceDoneEvent, proc: p, err: killErr}
	}()
}

func (a *actor) waitStopCommand(proc *process) error {
	timer := time.NewTimer(a.cfg.StopCommandTimeout)
	defer timer.Stop()
	select {
	case <-proc.done:
		result := proc.wait()
		if result.Code != 0 {
			return fmt.Errorf("exit status %d", result.Code)
		}
		return nil
	case <-timer.C:
		_ = signalGroup(proc.pid, syscall.SIGTERM)
		waitForTermBoundary([]*process{proc}, a.cfg.TermGrace)
		_ = killRemainingGroups([]*process{proc})
		proc.wait()
		return errors.New("timeout")
	}
}

// waitForTermBoundary waits until every target process group is gone or the
// configured grace period expires. A child leader may exit while descendants
// are still handling TERM, so the leader's Wait result does not end the grace.
func waitForTermBoundary(procs []*process, grace time.Duration) {
	if len(procs) == 0 {
		return
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		allStopped := true
		for _, proc := range procs {
			if groupExists(proc.pid) {
				allStopped = false
				break
			}
		}
		if allStopped {
			return
		}
		select {
		case <-timer.C:
			return
		case <-ticker.C:
		}
	}
}

func killRemainingGroups(procs []*process) error {
	var firstErr error
	for _, proc := range procs {
		if groupExists(proc.pid) {
			if err := signalGroup(proc.pid, syscall.SIGKILL); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// trackProcess is the only way a started process enters allProcesses. It first
// drops settled records so the list holds only records that forced shutdown
// or output drain can still act on (a live group, or an output pipe still held
// by a descendant that left the group), instead of one per ready-cmd probe.
// The held-pipe records are kept on purpose: dropping them loses the only
// handle drainOutputs has to close that pipe.
func (a *actor) trackProcess(p *process) {
	kept := a.allProcesses[:0]
	for _, old := range a.allProcesses {
		if old != nil && !old.settled() {
			kept = append(kept, old)
		}
	}
	clear(a.allProcesses[len(kept):])
	a.allProcesses = append(kept, p)
}

func (a *actor) beginForceStop() {
	// Include every process group started by the runner, even when its leader
	// has exited but descendants still keep the group alive.
	procs := make([]*process, 0, len(a.allProcesses))
	seen := make(map[*process]struct{}, len(a.allProcesses))
	for _, p := range a.allProcesses {
		if p == nil {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if !p.finished.Load() || groupExists(p.pid) {
			procs = append(procs, p)
		}
	}
	a.forceRunning = true
	if a.model.State == Exiting && a.model.Intent == IntentExit && a.model.ExitCode == 0 {
		a.failRequests("runner exiting")
	}
	if len(procs) == 0 {
		a.handleForceDone(actorEvent{kind: forceDoneEvent, forced: true})
		return
	}
	for _, p := range procs {
		_ = signalGroup(p.pid, syscall.SIGTERM)
	}
	go func() {
		waitForTermBoundary(procs, a.cfg.TermGrace)
		killErr := killRemainingGroups(procs)
		for _, p := range procs {
			p.wait()
		}
		a.events <- actorEvent{kind: forceDoneEvent, forced: true, err: killErr}
	}()
}

func (a *actor) beginForce(code int) {
	if a.finished {
		return
	}
	if a.forceRunning {
		a.forcedTermination = true
		a.forceCode = code
		a.model, _ = Update(a.model, Event{Kind: ForceEvent, SignalCode: code})
		a.invalidateReadyForForce()
		a.childExitPending = false
		a.readyResumePending = false
		a.readyDeferredEffects = nil
		a.readyFinishPending = false
		a.failRequests("runner interrupted")
		return
	}
	a.forcedTermination = true
	a.forceCode = code
	a.model, _ = Update(a.model, Event{Kind: ForceEvent, SignalCode: code})
	a.invalidateReadyForForce()
	a.childExitPending = false
	a.readyResumePending = false
	a.readyDeferredEffects = nil
	a.readyFinishPending = false
	a.failRequests("runner interrupted")
	a.queuedRequests = nil
	a.beginForceStop()
	a.presenter.Render(a.model)
}

func (a *actor) consumeChildExit() {
	if a.child == nil || a.childProcessed {
		return
	}
	a.handleEvent(actorEvent{kind: runDoneEvent, proc: a.child, result: a.child.result})
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
	readyEndedEarly := a.model.State == Running && a.model.Transition.Active && a.model.Transition.Stage == TransitionReady
	if readyEndedEarly && a.cfg.Headless {
		a.reportStderr("起動を確認できませんでした")
	}
	if a.readyProbe != nil {
		a.cancelReady()
	}
	a.readyResumePending = false
	// childProcessed は子の Wait が済んだ後にだけ立つので、child.result は書き込み済み
	code := a.child.result.Code
	_, effects := a.transition(Event{Kind: ChildExitedEvent, ExitStatus: code, HoldOnFailure: !a.cfg.Headless})
	a.stopAccepted = false
	if a.model.State == Crashed {
		// 落ちた時点でグループが残っている (孫がいる) ときだけ記録する。空のグループの番号は再利用されうるので、
		// Crashed で長く待った後に撃つと無関係なグループに届く。孫が後から自然に消えて番号が再利用される窓は残る
		// (強制終了の beginForceStop も allProcesses の記録へ同じ形で撃つ。pgid の同一性を確かめる手段が無い)
		if groupExists(a.child.pid) {
			a.crashedChild = a.child
		}
		a.failRequests(fmt.Sprintf("child exited (rc %d)", code))
	}
	if a.readyCleaningProc != nil {
		a.childExitPending = true
		a.readyDeferredEffects = effects
		a.presenter.Render(a.model)
		return
	}
	a.handleEffects(effects)
	a.presenter.Render(a.model)
}

func (a *actor) succeedRequests() {
	for _, req := range a.pendingRequests {
		req.Respond(control.Response{OK: true, ID: a.id, State: string(Running), PID: intPointer(a.model.PID), Generation: a.model.Generation, Ready: a.model.Ready})
	}
	a.pendingRequests = nil
	for _, req := range a.queuedRequests {
		req.Respond(control.Response{OK: true, ID: a.id, State: string(Running), PID: intPointer(a.model.PID), Generation: a.model.Generation, Ready: a.model.Ready})
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
	if a.forceRunning {
		return
	}
	if a.readyProbe != nil || a.readyCleaningProc != nil || len(a.readyCleanupQueue) > 0 {
		a.readyFinishPending = true
		a.readyFinishCode = code
		return
	}
	if a.child != nil && !a.child.finished.Load() {
		a.report("子が生きたまま終了しようとしたため、runner の終了を拒否しました")
		a.model.State = Running
		a.model.PID = a.child.pid
		a.model.Intent = IntentNone
		a.model.StopAccepted = false
		a.model.Confirm = ConfirmNone
		a.model.Message = "アプリが動いているので終了を取りやめました"
		a.stopAccepted = false
		a.failRequests("runner exit refused while child is alive")
		a.presenter.Render(a.model)
		return
	}
	a.model.Confirm = ConfirmNone
	a.model.Transition.Active = false
	a.model.Transition.Busy = false
	if a.presenter != nil {
		a.presenter.Render(a.model)
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
	line := termsafe.DetailLine(message)
	if a.sink != nil && a.sink.hasPrinter() {
		a.sink.addSafeLine(line)
		return
	}
	_, _ = fmt.Fprintln(a.cfg.Stderr, line)
}

func (a *actor) reportStderr(message string) {
	if message == "" {
		return
	}
	writer := a.cfg.Stderr
	if writer == nil {
		writer = os.Stderr
	}
	_, _ = fmt.Fprintln(writer, termsafe.DetailLine(message))
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
			if !a.forcedTermination {
				a.reportStderr(fmt.Sprintf("子孫が出力を握ったまま残っている (pgid %d)", proc.pid))
				proc.closeOutput()
				select {
				case <-proc.outputEnd:
				case <-time.After(time.Second):
					a.report("timed out collecting child output")
				}
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

func outputDrainTimeout(configured time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return 500 * time.Millisecond
}

// deferBuildForCrashCleanup は Crashed からの再ビルドで、落ちた子のプロセスグループが残っていれば
// 片付けを始めてビルドを後回しにする (true)。残った孫がポート等を握ったまま新しい子と並ぶのを防ぐ。
func (a *actor) deferBuildForCrashCleanup() bool {
	if a.crashCleanupRunning {
		a.buildAfterCrashCleanup = true
		return true
	}
	p := a.crashedChild
	a.crashedChild = nil
	if p == nil || !groupExists(p.pid) {
		return false
	}
	a.crashCleanupRunning = true
	a.buildAfterCrashCleanup = true
	grace := a.cfg.TermGrace
	go func() {
		_ = signalGroup(p.pid, syscall.SIGTERM)
		waitForTermBoundary([]*process{p}, grace)
		err := killRemainingGroups([]*process{p})
		// KILL の後もグループが消える (ポート等が解放される) まで待ってから次を起動する
		waitForTermBoundary([]*process{p}, grace)
		a.post(actorEvent{kind: crashCleanupDoneEvent, proc: p, err: err})
	}()
	return true
}

func (a *actor) handleCrashCleanupDone(ev actorEvent) {
	a.crashCleanupRunning = false
	if ev.err != nil {
		a.report("failed to stop process group: " + ev.err.Error())
	}
	build := a.buildAfterCrashCleanup
	a.buildAfterCrashCleanup = false
	// 片付けの間に終了 (Q / Ctrl-C) へ移っていたらビルドしない
	if !build || a.model.State != Building {
		return
	}
	if err := a.startBuild(); err != nil {
		a.failRequests(err.Error())
	}
	a.presenter.Render(a.model)
}
