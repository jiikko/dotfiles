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
	if m.State != Building || m.Confirm != ConfirmNone || m.Message != "building" {
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
	if running.State != Stopping || running.Intent != IntentRestart || running.Confirm != ConfirmNone || !hasEffect(effects, BeginStopEffect) {
		t.Fatalf("running restart: %+v %+v", running, effects)
	}

	stopping := Model{State: Stopping, Intent: IntentExit, PID: 99}
	stopping, effects = Update(stopping, Event{Kind: ControlRestartEvent})
	if stopping.Intent != IntentRestart || len(effects) != 0 {
		t.Fatalf("control restart while stopping: %+v %+v", stopping, effects)
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

	m = Model{State: Running, PID: 43}
	m, _ = Update(m, Event{Kind: ControlStatusEvent})
	if m.State != Running || m.PID != 43 {
		t.Fatalf("status changed model: %+v", m)
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
		{"status", Model{State: Running, PID: 5}, Event{Kind: ControlStatusEvent}, Model{State: Running, PID: 5}, []Effect{{Kind: ControlStatusEffect}}},
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
