package runner

import (
	"reflect"
	"testing"
)

func apply(m Model, kind EventKind, key string) (Model, []Effect) {
	return Update(m, Event{Kind: kind, Key: key})
}

func TestUpdateConfirmationKeysAndRestart(t *testing.T) {
	base := Model{State: Running, PID: 431, Generation: 7}
	m, effects := apply(base, KeyEvent, "R")
	if m.Confirm != ConfirmRestart || m.State != Running || m.PID != 431 || m.Generation != 7 || len(effects) != 0 {
		t.Fatalf("R: model=%+v effects=%+v", m, effects)
	}
	m, effects = apply(m, KeyEvent, "n")
	if m.Confirm != ConfirmNone || m.State != Running || m.PID != 431 || len(effects) != 0 {
		t.Fatalf("n: model=%+v effects=%+v", m, effects)
	}
	m, _ = apply(base, KeyEvent, "R")
	m, effects = apply(m, KeyEvent, "esc")
	if m.Confirm != ConfirmNone || m.State != Running || m.PID != 431 || len(effects) != 0 {
		t.Fatalf("Esc confirmation cancel: model=%+v effects=%+v", m, effects)
	}
	m, _ = apply(base, KeyEvent, "r")
	m, effects = apply(m, KeyEvent, "y")
	if m.State != Stopping || m.Intent != IntentRestart || m.PID != 431 || m.Confirm != ConfirmNone || !hasEffect(effects, BeginStopEffect) {
		t.Fatalf("confirm restart: model=%+v effects=%+v", m, effects)
	}

	m, _ = apply(base, KeyEvent, "Q")
	if m.Confirm != ConfirmQuit {
		t.Fatalf("Q did not confirm quit: %+v", m)
	}
	m, effects = apply(m, KeyEvent, "Y")
	if m.State != Stopping || m.Intent != IntentExit || !hasEffect(effects, BeginStopEffect) {
		t.Fatalf("confirm quit: %+v %+v", m, effects)
	}
}

func TestUpdateBuildTransitionsAndGeneration(t *testing.T) {
	m := InitialModel()
	m, _ = apply(m, KeyEvent, "R")
	if m.State != Building || m.Confirm != ConfirmNone || m.Message != "処理中" {
		t.Fatalf("R while building: %+v", m)
	}
	m, effects := Update(m, Event{Kind: ControlRestartEvent})
	if !m.BuildQueued || m.State != Building || len(effects) != 0 {
		t.Fatalf("queue control restart: %+v %+v", m, effects)
	}
	m, effects = Update(m, Event{Kind: BuildSucceededEvent})
	if m.State != Building || m.BuildQueued || !hasEffect(effects, StartBuildEffect) {
		t.Fatalf("build success after queue: %+v %+v", m, effects)
	}
	m, effects = Update(m, Event{Kind: BuildFailedEvent, Reason: "first failed"})
	if m.State != BuildFailed || m.Message != "first failed" || hasEffect(effects, StartBuildEffect) {
		t.Fatalf("build failed: %+v %+v", m, effects)
	}
	m, effects = apply(m, KeyEvent, "R")
	if m.State != Building || !hasEffect(effects, StartBuildEffect) {
		t.Fatalf("R retries failed build: %+v %+v", m, effects)
	}
	m, _ = Update(m, Event{Kind: BuildSucceededEvent})
	m, _ = Update(m, Event{Kind: ChildStartedEvent, PID: 818})
	if m.State != Running || m.PID != 818 || m.Generation != 1 {
		t.Fatalf("first run: %+v", m)
	}
	m, _ = Update(m, Event{Kind: ChildExitedEvent})
	if m.State != Exiting || m.PID != 0 {
		t.Fatalf("natural exit: %+v", m)
	}
	_, effects = Update(m, Event{Kind: ControlRestartEvent})
	if !hasEffect(effects, ControlRejectEffect) || effects[0].Reason != "runner exiting" {
		t.Fatalf("restart after exit: %+v", effects)
	}
}

func TestUpdateControlRestartByState(t *testing.T) {
	building := InitialModel()
	building, _ = Update(building, Event{Kind: ControlRestartEvent})
	if !building.BuildQueued {
		t.Fatalf("building restart not queued: %+v", building)
	}

	failed := Model{State: BuildFailed}
	failed, effects := Update(failed, Event{Kind: ControlRestartEvent})
	if failed.State != Building || !hasEffect(effects, StartBuildEffect) {
		t.Fatalf("failed build restart: %+v %+v", failed, effects)
	}

	running := Model{State: Running, PID: 77, Generation: 4, Confirm: ConfirmQuit}
	running, effects = Update(running, Event{Kind: ControlRestartEvent})
	if running.State != Stopping || running.Intent != IntentRestart || running.Confirm != ConfirmNone ||
		running.Transition.Kind != TransitionRestart || running.Transition.Stage != TransitionStop ||
		!running.Transition.Active || !hasEffect(effects, BeginStopEffect) {
		t.Fatalf("running restart: %+v %+v", running, effects)
	}

	stopping := Model{State: Stopping, Intent: IntentExit, PID: 99}
	stopping, effects = Update(stopping, Event{Kind: ControlRestartEvent})
	if stopping.Intent != IntentRestart || len(effects) != 0 {
		t.Fatalf("control restart while stopping: %+v %+v", stopping, effects)
	}
}

func TestTransitionPanelTracksStartupRestartAndReadiness(t *testing.T) {
	m := InitialModel()
	if !m.Transition.Active || m.Transition.Kind != TransitionStartup || m.Transition.Stage != TransitionBuild {
		t.Fatalf("initial transition = %+v", m.Transition)
	}
	m, _ = Update(m, Event{Kind: BuildSucceededEvent})
	if m.Transition.Stage != TransitionLaunch {
		t.Fatalf("build success transition = %+v", m.Transition)
	}
	m, _ = Update(m, Event{Kind: ChildStartedEvent, PID: 410, ReadyCheck: true})
	if m.Generation != 1 || m.Ready || !m.Transition.Active || m.Transition.Stage != TransitionReady {
		t.Fatalf("child startup transition = %+v, model=%+v", m.Transition, m)
	}
	m, _ = Update(m, Event{Kind: ReadySucceededEvent, Generation: 1})
	if !m.Ready || m.Transition.Active {
		t.Fatalf("ready success = %+v", m)
	}

	m, _ = Update(m, Event{Kind: KeyEvent, Key: "R"})
	m, _ = Update(m, Event{Kind: KeyEvent, Key: "y"})
	if m.State != Stopping || m.Transition.Kind != TransitionRestart || m.Transition.Stage != TransitionStop {
		t.Fatalf("confirmed restart transition = %+v, model=%+v", m.Transition, m)
	}
	m, _ = Update(m, Event{Kind: ChildExitedEvent})
	if m.State != Building || m.Transition.Kind != TransitionRestart || m.Transition.Stage != TransitionBuild {
		t.Fatalf("restart build transition = %+v, model=%+v", m.Transition, m)
	}
	m, _ = Update(m, Event{Kind: BuildSucceededEvent})
	if m.Transition.Stage != TransitionLaunch {
		t.Fatalf("restart launch transition = %+v", m.Transition)
	}
	m, _ = Update(m, Event{Kind: ChildStartedEvent, PID: 411, ReadyCheck: true})
	if m.Generation != 2 || m.Ready || m.Transition.Stage != TransitionReady {
		t.Fatalf("second generation readiness transition = %+v, model=%+v", m.Transition, m)
	}
	m, _ = Update(m, Event{Kind: ReadySucceededEvent, Generation: 2})
	if !m.Ready || m.Transition.Active {
		t.Fatalf("second generation ready = %+v", m)
	}
}

func TestTransitionResultsClosePanelAndRejectStaleReadyGeneration(t *testing.T) {
	m, _ := Update(InitialModel(), Event{Kind: BuildFailedEvent, Reason: "exit 1"})
	if m.State != BuildFailed || m.Transition.Active || m.Transition.Result != TransitionBuildFailed || m.Message != "exit 1" {
		t.Fatalf("build failure result = %+v", m)
	}

	m = Model{State: Running, PID: 72, Generation: 2, Transition: Transition{Active: true, Kind: TransitionRestart, Stage: TransitionReady}}
	got, effects := Update(m, Event{Kind: ReadySucceededEvent, Generation: 1})
	if got.Ready || !got.Transition.Active || len(effects) != 0 {
		t.Fatalf("stale readiness success applied: %+v, effects=%+v", got, effects)
	}
	got, _ = Update(m, Event{Kind: ReadyFailedEvent, Generation: 2})
	if got.Ready || got.Transition.Active || got.Transition.Result != TransitionReadinessUnconfirmed || got.State != Running || got.PID != 72 {
		t.Fatalf("readiness failure should close panel and preserve child: %+v", got)
	}
}

func TestQuitTransitionEscCancellationAndChildExit(t *testing.T) {
	base := Model{State: Running, PID: 90, Generation: 3, Ready: true}
	m, _ := Update(base, Event{Kind: KeyEvent, Key: "Q"})
	m, _ = Update(m, Event{Kind: KeyEvent, Key: "y"})
	if m.State != Stopping || m.Transition.Kind != TransitionQuit || !m.Transition.Active {
		t.Fatalf("quit transition = %+v", m)
	}
	m, _ = Update(m, Event{Kind: StopCommandOKEvent})
	m, effects := Update(m, Event{Kind: KeyEvent, Key: "esc"})
	if m.State != Running || m.Transition.Active || m.PID != base.PID || !m.Ready || m.StopAccepted || !hasEffect(effects, ControlRejectEffect) {
		t.Fatalf("quit cancel = %+v, effects=%+v", m, effects)
	}

	m, _ = Update(base, Event{Kind: KeyEvent, Key: "Q"})
	m, _ = Update(m, Event{Kind: KeyEvent, Key: "y"})
	m, effects = Update(m, Event{Kind: ChildExitedEvent})
	if m.State != Exiting || m.Transition.Active || !hasEffect(effects, ExitEffect) {
		t.Fatalf("quit child exit = %+v, effects=%+v", m, effects)
	}
}

func TestReadyCheckCanResumeAfterRejectedStop(t *testing.T) {
	m := Model{State: Running, PID: 92, Generation: 4}
	m, _ = Update(m, Event{Kind: ReadyCheckResumedEvent})
	if !m.Transition.Active || m.Transition.Kind != TransitionStartup || m.Transition.Stage != TransitionReady || m.Ready {
		t.Fatalf("resumed readiness state = %+v", m)
	}
}

func TestUpdateStopCommandCancellationAndForceCodes(t *testing.T) {
	m := Model{State: Stopping, PID: 212, Generation: 3, Intent: IntentRestart}
	m, _ = Update(m, Event{Kind: StopCommandOKEvent})
	if !m.StopAccepted {
		t.Fatalf("stop command success was not recorded: %+v", m)
	}
	m, effects := apply(m, KeyEvent, "esc")
	if m.State != Running || m.Intent != IntentNone || m.PID != 212 || m.StopAccepted || !hasEffect(effects, ControlRejectEffect) || effects[0].Reason != "stop cancelled" {
		t.Fatalf("Esc: %+v %+v", m, effects)
	}

	m, effects = apply(Model{State: Running, PID: 11}, KeyEvent, "ctrl+c")
	if m.State != Exiting || m.ExitCode != 130 || !hasEffect(effects, ForceStopEffect) {
		t.Fatalf("Ctrl-C: %+v %+v", m, effects)
	}
	m, _ = Update(Model{State: Running, PID: 11}, Event{Kind: ForceEvent, SignalCode: 143})
	if m.State != Exiting || m.ExitCode != 143 {
		t.Fatalf("SIGTERM: %+v", m)
	}
}

func TestUpdateRestartAndQuitIgnoredWhileStoppingOrExiting(t *testing.T) {
	withOpenDialog, _ := Update(Model{State: Stopping, PID: 212, Intent: IntentRestart, Confirm: ConfirmQuit}, Event{Kind: StopCommandOKEvent})
	if withOpenDialog.Confirm != ConfirmNone {
		t.Fatalf("stop acceptance left confirmation open: %+v", withOpenDialog)
	}
	for _, state := range []State{Stopping, Exiting} {
		for _, key := range []string{"R", "Q"} {
			t.Run(string(state)+"/"+key, func(t *testing.T) {
				m := Model{State: state, PID: 212, Intent: IntentRestart, Confirm: ConfirmQuit, StopAccepted: true}
				got, effects := apply(m, KeyEvent, key)
				wantMessage := "処理中"
				wantReason := "処理中"
				if got.State != state || got.PID != 212 || got.Intent != IntentRestart || got.Confirm != ConfirmNone || got.Message != wantMessage {
					t.Fatalf("key %q changed lifecycle while %s: model=%+v", key, state, got)
				}
				if len(effects) != 1 || effects[0] != (Effect{Kind: MessageEffect, Reason: wantReason}) {
					t.Fatalf("key %q effects while %s = %+v", key, state, effects)
				}
			})
		}
	}
}

func TestUpdateEscCancelsAcceptedQuitStopAndRejectsControlRestart(t *testing.T) {
	m := Model{State: Stopping, PID: 212, Generation: 3, Intent: IntentExit, StopAccepted: true}
	m, _ = Update(m, Event{Kind: StopCommandOKEvent})
	m, effects := apply(m, KeyEvent, "esc")
	if m.State != Running || m.Intent != IntentNone || m.PID != 212 || m.StopAccepted || m.Message != "停止を取り消しました" {
		t.Fatalf("Esc after accepted quit: model=%+v", m)
	}
	if len(effects) != 1 || effects[0] != (Effect{Kind: ControlRejectEffect, Reason: "stop cancelled"}) {
		t.Fatalf("Esc effects = %+v", effects)
	}
}

func TestUpdateStopCommandFailureAfterChildExitExitsRegardlessOfIntent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		intent     Intent
		wantState  State
		wantEffect EffectKind
	}{
		{name: "restart", intent: IntentRestart, wantState: Exiting, wantEffect: ExitEffect},
		{name: "quit", intent: IntentExit, wantState: Exiting, wantEffect: ExitEffect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{State: Stopping, PID: 212, Intent: tc.intent}
			got, effects := Update(m, Event{Kind: StopCommandFailEvent, Reason: "stop-cmd failed", ChildExited: true})
			if got.State != tc.wantState || got.PID != 0 || got.Intent != IntentNone {
				t.Fatalf("stop failure after child exit: model=%+v, want state %s", got, tc.wantState)
			}
			if !hasEffect(effects, tc.wantEffect) {
				t.Fatalf("effects=%+v, want %s", effects, tc.wantEffect)
			}
		})
	}
}

func TestQuitConfirmationDuringBuildStopsBuildOnYes(t *testing.T) {
	m, effects := Update(InitialModel(), Event{Kind: KeyEvent, Key: "Q"})
	if m.Confirm != ConfirmQuit || !m.Transition.Active || m.State != Building || len(effects) != 0 {
		t.Fatalf("Q during build should open quit confirmation: %+v, effects=%+v", m, effects)
	}
	m, effects = Update(m, Event{Kind: KeyEvent, Key: "y"})
	if m.State != Exiting || m.Confirm != ConfirmNone || m.ExitCode != 0 || !hasEffect(effects, ForceStopEffect) || !hasEffect(effects, ExitEffect) {
		t.Fatalf("confirmed quit during build should stop build and exit: %+v, effects=%+v", m, effects)
	}
	if !m.Transition.Active || m.Transition.Kind != TransitionQuit {
		t.Fatalf("quit panel should remain visible while build is stopped: %+v", m.Transition)
	}
}

func TestQuitConfirmationDuringReadyUsesChildStopPath(t *testing.T) {
	m := Model{State: Running, PID: 410, Generation: 2, Transition: Transition{
		Active: true, Kind: TransitionRestart, Stage: TransitionReady,
	}}
	m, effects := Update(m, Event{Kind: KeyEvent, Key: "Q"})
	if m.Confirm != ConfirmQuit || !m.Transition.Active || len(effects) != 0 {
		t.Fatalf("Q during readiness should open quit confirmation: %+v, effects=%+v", m, effects)
	}
	m, effects = Update(m, Event{Kind: KeyEvent, Key: "y"})
	if m.State != Stopping || m.PID != 410 || m.Intent != IntentExit || m.Transition.Kind != TransitionQuit || m.Transition.Stage != TransitionStop || !hasEffect(effects, BeginStopEffect) {
		t.Fatalf("confirmed quit during readiness should stop the running child: %+v, effects=%+v", m, effects)
	}

	stopping, effects := Update(m, Event{Kind: KeyEvent, Key: "Q"})
	if stopping.State != Stopping || !stopping.Transition.Busy || stopping.Confirm != ConfirmNone || !hasEffect(effects, MessageEffect) {
		t.Fatalf("Q during stop stage should be ignored as busy: %+v, effects=%+v", stopping, effects)
	}
}

func TestRestartKeyDuringEveryTransitionStageShowsBusy(t *testing.T) {
	for _, stage := range []TransitionStage{TransitionStop, TransitionBuild, TransitionLaunch, TransitionReady} {
		state := Model{State: Running, Transition: Transition{Active: true, Kind: TransitionRestart, Stage: stage}}
		if stage == TransitionStop {
			state.State = Stopping
		}
		got, effects := Update(state, Event{Kind: KeyEvent, Key: "R"})
		if !got.Transition.Active || !got.Transition.Busy || got.Message != "処理中" || len(effects) != 1 || effects[0].Reason != "処理中" {
			t.Fatalf("R during %s should show processing: %+v, effects=%+v", stage, got, effects)
		}
	}
	confirmed := Model{State: Building, Confirm: ConfirmQuit, Transition: Transition{
		Active: true, Kind: TransitionStartup, Stage: TransitionBuild,
	}}
	got, effects := Update(confirmed, Event{Kind: KeyEvent, Key: "R"})
	if got.Confirm != ConfirmQuit || !got.Transition.Busy || got.Message != "処理中" || len(effects) != 1 || effects[0].Reason != "処理中" {
		t.Fatalf("R during quit confirmation should show busy and keep confirmation: %+v, effects=%+v", got, effects)
	}

	m, effects := Update(InitialModel(), Event{Kind: KeyEvent, Key: "ctrl+c"})
	if m.State != Exiting || m.ExitCode != 130 || m.Transition.Active || !hasEffect(effects, ForceStopEffect) {
		t.Fatalf("Ctrl-C during startup transition = %+v, effects=%+v", m, effects)
	}
	m, effects = Update(m, Event{Kind: ControlRestartEvent})
	if m.State != Exiting || !hasEffect(effects, ControlRejectEffect) || effects[0].Reason != "runner exiting" {
		t.Fatalf("control restart after forced exit = %+v, effects=%+v", m, effects)
	}
}

func TestUpdateStoppingAndChildExit(t *testing.T) {
	m := Model{State: Stopping, PID: 43, Generation: 2, Intent: IntentRestart}
	m, effects := Update(m, Event{Kind: ChildExitedEvent})
	if m.State != Building || m.PID != 0 || !hasEffect(effects, StartBuildEffect) {
		t.Fatalf("restart after stop: %+v %+v", m, effects)
	}

	m = Model{State: Stopping, PID: 43, Intent: IntentExit}
	m, effects = Update(m, Event{Kind: ChildExitedEvent})
	if m.State != Exiting || m.PID != 0 || !hasEffect(effects, ExitEffect) {
		t.Fatalf("quit after stop: %+v %+v", m, effects)
	}
}

// build-failed からの再ビルドは R キーでも control restart でも同じ (rebuildAfterFailure): 板を出していたなら
// ビルドの段で開き直し、結果を消す。板を出していなかったなら開かない。
func TestRebuildFromBuildFailedIsTheSameForKeyAndControl(t *testing.T) {
	for _, entry := range []struct {
		name  string
		event Event
	}{{"key", Event{Kind: KeyEvent, Key: "R"}}, {"control", Event{Kind: ControlRestartEvent}}} {
		failed := Model{State: BuildFailed, Message: "build exited with status 2",
			Transition: Transition{Kind: TransitionRestart, Stage: TransitionBuild, Result: TransitionBuildFailed, Busy: true}}
		got, effects := Update(failed, entry.event)
		want := Model{State: Building, Transition: Transition{Active: true, Kind: TransitionRestart, Stage: TransitionBuild}}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(effects, []Effect{{Kind: StartBuildEffect}}) {
			t.Fatalf("%s from build-failed with a panel: got %+v %+v; want %+v and one build", entry.name, got, effects, want)
		}
		noPanel := Model{State: BuildFailed, Message: "build exited with status 2"}
		got, _ = Update(noPanel, entry.event)
		if got.State != Building || got.Transition.Active || got.Message != "" {
			t.Fatalf("%s from build-failed without a panel opened one or kept the message: %+v", entry.name, got)
		}
	}
}

func TestUpdateEffectsAreStable(t *testing.T) {
	cases := []struct {
		name    string
		model   Model
		event   Event
		want    Model
		effects []Effect
	}{
		{"failed build retry", Model{State: BuildFailed, Generation: 8}, Event{Kind: KeyEvent, Key: "R"}, Model{State: Building, Generation: 8}, []Effect{{Kind: StartBuildEffect}}},
		{"control after exit", Model{State: Exiting}, Event{Kind: ControlRestartEvent}, Model{State: Exiting}, []Effect{{Kind: ControlRejectEffect, Reason: "runner exiting"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, effects := Update(tc.model, tc.event)
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(effects, tc.effects) {
				t.Fatalf("got %+v %+v; want %+v %+v", got, effects, tc.want, tc.effects)
			}
		})
	}
}

func hasEffect(effects []Effect, kind EffectKind) bool { return containsEffect(effects, kind) }
