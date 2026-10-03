package runner

import (
	"testing"
	"time"
)

// fakeClock は呼ばれるたびに進めた時刻を返す (実時間を待たない)。
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func timedActor(clock *fakeClock, m Model) *actor {
	a := &actor{model: m}
	a.timer.now = clock.now
	return a
}

func TestRestartSummaryReportsEachStage(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	a := timedActor(clock, Model{State: Running, PID: 5})
	a.transition(Event{Kind: ControlRestartEvent}) // 終了の段で板が開く
	clock.advance(400 * time.Millisecond)
	a.transition(Event{Kind: ChildExitedEvent}) // 停止が済んでビルドの段へ
	clock.advance(8200 * time.Millisecond)
	a.transition(Event{Kind: BuildSucceededEvent})
	clock.advance(100 * time.Millisecond)
	a.transition(Event{Kind: ChildStartedEvent, PID: 6, ReadyCheck: true})
	clock.advance(1100 * time.Millisecond)
	a.transition(Event{Kind: ReadySucceededEvent, Generation: a.model.Generation})
	want := "再起動しました (計 9.8s: 終了 0.4s / ビルド 8.2s / 起動 0.1s / 起動の確認 1.1s)"
	if a.model.Message != want {
		t.Fatalf("summary = %q, want %q", a.model.Message, want)
	}
}

func TestStartupSummaryWithoutReadyCheck(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	a := timedActor(clock, InitialModel())
	a.timer.begin()
	clock.advance(2 * time.Second)
	a.transition(Event{Kind: BuildSucceededEvent})
	clock.advance(300 * time.Millisecond)
	a.transition(Event{Kind: ChildStartedEvent, PID: 6})
	if want := "起動しました (計 2.3s: ビルド 2.0s / 起動 0.3s)"; a.model.Message != want {
		t.Fatalf("summary = %q, want %q", a.model.Message, want)
	}
}

// 板が成功以外で閉じたときは、その理由の message を残す (所要時間で上書きしない)。
func TestNoSummaryWhenPanelClosesWithoutSuccess(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	cases := []struct {
		name  string
		setup func(a *actor)
		close Event
		want  string
	}{
		{"stop cancelled with Esc", func(a *actor) {
			a.transition(Event{Kind: ControlRestartEvent})
			a.transition(Event{Kind: StopCommandOKEvent})
		}, Event{Kind: KeyEvent, Key: "esc"}, "停止を取り消しました"},
		{"stop command failed", func(a *actor) {
			a.transition(Event{Kind: ControlRestartEvent})
		}, Event{Kind: StopCommandFailEvent, Reason: "stop-cmd failed: boom"}, "stop-cmd failed: boom"},
		{"readiness unconfirmed", func(a *actor) {
			a.transition(Event{Kind: ControlRestartEvent})
			a.transition(Event{Kind: ChildExitedEvent})
			a.transition(Event{Kind: BuildSucceededEvent})
			a.transition(Event{Kind: ChildStartedEvent, PID: 6, ReadyCheck: true})
		}, Event{Kind: ReadyFailedEvent, Generation: 1}, "起動を確認できませんでした"},
	}
	for _, tc := range cases {
		a := timedActor(clock, Model{State: Running, PID: 5})
		tc.setup(a)
		clock.advance(time.Second)
		a.transition(tc.close)
		if a.model.Transition.Active || a.model.Message != tc.want {
			t.Fatalf("%s: message = %q (panel active %v), want %q", tc.name, a.model.Message, a.model.Transition.Active, tc.want)
		}
	}
}
