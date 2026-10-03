package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

type failingUIWriter struct{ writes int }

func (w *failingUIWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, syscall.EPIPE
}

func TestStatusLineFitsWidthsAndShowsEveryState(t *testing.T) {
	states := []runner.State{runner.Building, runner.Running, runner.BuildFailed, runner.Stopping}
	for _, width := range []int{40, 80, 120} {
		for _, state := range states {
			for _, pid := range []int{0, 12345} {
				name := fmt.Sprintf("width=%d/state=%s/pid=%d", width, state, pid)
				t.Run(name, func(t *testing.T) {
					line := StatusLine(runner.Model{State: state, PID: pid}, width)
					if got := termwidth.Of(line); got != width {
						t.Fatalf("display width = %d, want %d: %q", got, width, line)
					}
					wantStatus := string(state)
					if state == runner.Running && pid > 0 {
						wantStatus = fmt.Sprintf("running (pid %d)", pid)
					}
					if !strings.HasSuffix(line, wantStatus) {
						t.Fatalf("line %q does not end in %q", line, wantStatus)
					}
				})
			}
		}
	}
	for _, width := range []int{80, 120} {
		line := StatusLine(runner.Model{State: runner.Running, PID: 12345}, width)
		if !strings.HasPrefix(line, fullActions) {
			t.Errorf("width %d should retain the agreed action prefix: %q", width, line)
		}
	}
}

func TestViewPlacesConfirmAboveStatusAndMessageOnPreviousLine(t *testing.T) {
	state := runner.Model{State: runner.Running, PID: 12345, Confirm: runner.ConfirmRestart, Message: "終了待ち"}
	lines := ViewLines(state, 80)
	if len(lines) < 3 {
		t.Fatalf("expected dialog, message, and status rows, got %q", lines)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "アプリを再起動しますか？") {
		t.Fatalf("confirm body missing: %q", joined)
	}
	if !strings.Contains(joined, confirm.HintYesNo) {
		t.Fatalf("confirm hint missing: %q", joined)
	}
	if got, want := lines[len(lines)-2], "終了待ち"; got != want {
		t.Fatalf("message line = %q, want %q immediately above status", got, want)
	}
	if got, want := lines[len(lines)-1], StatusLine(state, 80); got != want {
		t.Fatalf("last line = %q, want status line %q", got, want)
	}
	if strings.Index(joined, "アプリを再起動しますか？") > strings.LastIndex(joined, lines[len(lines)-1]) {
		t.Fatal("confirm dialog must appear above the final status line")
	}
}

func TestZeroWindowSizeUsesDefaultDimensionsAndResizesRenderer(t *testing.T) {
	model := &teaModel{width: defaultWidth, height: defaultHeight, closed: make(chan struct{})}
	updated, resize := model.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	got := updated.(*teaModel)
	if got.width != defaultWidth || got.height != defaultHeight {
		t.Fatalf("normalized dimensions = %dx%d, want %dx%d", got.width, got.height, defaultWidth, defaultHeight)
	}
	if resize == nil {
		t.Fatal("zero dimensions did not request a renderer resize")
	}
	msg, ok := resize().(tea.WindowSizeMsg)
	if !ok || msg.Width != defaultWidth || msg.Height != defaultHeight {
		t.Fatalf("resize command = %#v, want WindowSizeMsg{%d, %d}", msg, defaultWidth, defaultHeight)
	}
}

func TestQuitConfirmUsesAgreedPrompt(t *testing.T) {
	lines := ViewLines(runner.Model{State: runner.BuildFailed, Confirm: runner.ConfirmQuit}, 80)
	if !strings.Contains(strings.Join(lines, "\n"), "アプリを終了しますか？") {
		t.Fatalf("quit confirmation missing from view: %q", lines)
	}
}

func TestTransitionViewKeepsPanelBelowLogAreaAtHeight24And40(t *testing.T) {
	state := runner.Model{
		State:      runner.Stopping,
		Transition: runner.Transition{Active: true, Kind: runner.TransitionRestart, Stage: runner.TransitionBuild},
	}
	for _, height := range []int{24, 40} {
		lines := ViewLinesAt(state, 80, height, 0)
		panelTop := -1
		panelWidth := 0
		left := 0
		for index, line := range lines {
			if strings.Contains(line, "┌") && strings.Contains(line, "再起動中") {
				panelTop = index
				left = termwidth.Of(strings.SplitN(line, "┌", 2)[0])
				panelWidth = 44
				break
			}
		}
		if panelTop < 0 {
			t.Fatalf("height %d has no restart panel: %q", height, lines)
		}
		panelHeight := 7 // four body rows + top/bottom/low-shadow rows from confirm.Box
		wantScreenTop := (height - panelHeight) / 2
		wantGap := max(height-wantScreenTop-panelHeight-1, 0)
		if panelTop != 0 {
			t.Fatalf("height %d view starts with %d rows above the panel; logs must own that area: %q", height, panelTop, lines[:panelTop])
		}
		if got := len(lines); got != panelHeight+wantGap+1 {
			t.Fatalf("height %d view has %d rows, want panel %d + gap %d + status", height, got, panelHeight, wantGap)
		}
		if screenTop := height - len(lines); screenTop != wantScreenTop {
			t.Fatalf("height %d panel screen top = %d, want about %d", height, screenTop, wantScreenTop)
		}
		if left != (80-panelWidth)/2 {
			t.Fatalf("height %d panel left edge = %d, want %d", height, left, (80-panelWidth)/2)
		}
		for _, row := range lines[panelTop : panelTop+panelHeight] {
			if got := termwidth.Of(row); got != 80 {
				t.Fatalf("height %d panel row width = %d, want 80: %q", height, got, row)
			}
		}
		if lines[len(lines)-1] != StatusLine(state, 80) {
			t.Fatalf("height %d final row is not the pinned status: %q", height, lines[len(lines)-1])
		}
	}
}

func TestTransitionPanelFitsOrFallsBackAtWidthsOneThroughNine(t *testing.T) {
	// 日本語の段名が入る幅ではそれを、入らない幅 (全角 1 文字も入らない 1 桁など) では ASCII を切り詰めて出す。
	stages := []struct {
		stage runner.TransitionStage
		label string
		ascii string
	}{
		{stage: runner.TransitionStop, label: "終了", ascii: "stop"},
		{stage: runner.TransitionBuild, label: "ビルド", ascii: "build"},
		{stage: runner.TransitionLaunch, label: "起動", ascii: "launch"},
		{stage: runner.TransitionReady, label: "確認", ascii: "ready"},
	}
	for width := 1; width <= 9; width++ {
		for _, stage := range stages {
			state := runner.Model{State: runner.Running, Transition: runner.Transition{
				Active: true, Kind: runner.TransitionRestart, Stage: stage.stage,
			}}
			lines := ViewLinesAt(state, width, 24, 0)
			if len(lines) != 1 {
				t.Fatalf("width %d/stage %s produced %d rows, want one compact stage row: %q", width, stage.stage, len(lines), lines)
			}
			if got := termwidth.Of(lines[0]); got > width {
				t.Fatalf("width %d/stage %s compact row wraps at %d cells: %q", width, stage.stage, got, lines[0])
			}
			want := stage.label
			if termwidth.Of(want) > width {
				want = stage.ascii[:min(width, len(stage.ascii))]
			}
			if lines[0] != want {
				t.Fatalf("width %d/stage %s row = %q, want stage label %q", width, stage.stage, lines[0], want)
			}
			if strings.ContainsAny(lines[0], "┌┐└┘") {
				t.Fatalf("width %d/stage %s still draws an oversized panel: %q", width, stage.stage, lines[0])
			}
		}
	}
}

func TestTransitionPanelFlushesAgainstStatusOnTenRowTerminal(t *testing.T) {
	state := runner.Model{
		State:      runner.Running,
		PID:        1,
		Transition: runner.Transition{Active: true, Kind: runner.TransitionStartup, Stage: runner.TransitionReady},
	}
	lines := ViewLinesAt(state, 80, 10, 0)
	if len(lines) != 8 {
		t.Fatalf("height 10 produced %d rows, want 7 panel rows plus status", len(lines))
	}
	if !strings.Contains(lines[6], "░") || lines[7] != StatusLine(state, 80) {
		t.Fatalf("panel is not flush to the final status row: %q", lines)
	}
	if !strings.Contains(lines[0], "┌") || !strings.Contains(lines[1], "起動を確認しています") {
		t.Fatalf("ready stage missing from short terminal panel: %q", lines)
	}
	panelScreenTop := 10 - len(lines)
	panelCenter := panelScreenTop + 3
	if panelScreenTop != 2 || panelCenter != 5 {
		t.Fatalf("short terminal panel position = top %d, center %d; want top 2, center 5", panelScreenTop, panelCenter)
	}
}

func TestTransitionPanelShowsCompletedStagesAndStageSpecificHints(t *testing.T) {
	cases := []struct {
		name       string
		stage      runner.TransitionStage
		steps      string
		wantHint   string
		wantNoHint string
	}{
		{"stop-command-running", runner.TransitionStop, "終了 → ビルド → 起動 → 起動の確認", "Ctrl-C: 強制終了", "Esc: 取り消し"},
		{"stop-wait", runner.TransitionStop, "終了 → ビルド → 起動 → 起動の確認", "Esc: 取り消し   Ctrl-C: 強制終了", "Q: 終了"},
		{"build", runner.TransitionBuild, "終了✓ → ビルド → 起動 → 起動の確認", "Q: 終了   Ctrl-C: 強制終了", "Esc: 取り消し"},
		{"launch", runner.TransitionLaunch, "終了✓ → ビルド✓ → 起動 → 起動の確認", "Q: 終了   Ctrl-C: 強制終了", "Esc: 取り消し"},
		{"ready", runner.TransitionReady, "終了✓ → ビルド✓ → 起動✓ → 起動の確認", "Q: 終了   Ctrl-C: 強制終了", "Esc: 取り消し"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := runner.Model{State: runner.Running, Transition: runner.Transition{
				Active: true, Kind: runner.TransitionRestart, Stage: tc.stage,
			}}
			if tc.name == "stop-wait" {
				state.State = runner.Stopping
				state.StopAccepted = true
			}
			joined := strings.Join(ViewLinesAt(state, 80, 24, 0), "\n")
			if !strings.Contains(joined, tc.steps) {
				t.Fatalf("completed stage row missing %q: %q", tc.steps, joined)
			}
			if !strings.Contains(joined, tc.wantHint) || strings.Contains(joined, tc.wantNoHint) {
				t.Fatalf("stage hint does not match accepted keys (want %q, exclude %q): %q", tc.wantHint, tc.wantNoHint, joined)
			}
		})
	}
	startup := ViewLinesAt(runner.Model{State: runner.Building, Transition: runner.Transition{
		Active: true, Kind: runner.TransitionStartup, Stage: runner.TransitionBuild,
	}}, 80, 24, 0)
	if !strings.Contains(strings.Join(startup, "\n"), "終了✓ → ビルド → 起動 → 起動の確認") {
		t.Fatalf("startup stage row should mark the skipped stop step complete: %q", startup)
	}
}

func TestQuitConfirmationDuringTransitionIsDrawnInsidePanel(t *testing.T) {
	state := runner.Model{State: runner.Running, Confirm: runner.ConfirmQuit, Transition: runner.Transition{
		Active: true, Kind: runner.TransitionRestart, Stage: runner.TransitionReady,
	}}
	lines := ViewLinesAt(state, 80, 24, 0)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "アプリを終了しますか？") || !strings.Contains(joined, confirm.HintYesNo) {
		t.Fatalf("quit confirmation missing from active transition panel: %q", lines)
	}
	if got := strings.Count(joined, "┌"); got != 1 {
		t.Fatalf("confirmation should be contained in one progress panel, found %d boxes: %q", got, lines)
	}
	if !strings.Contains(joined, "起動を確認しています") {
		t.Fatalf("confirmation obscured the active stage: %q", lines)
	}
	state.Transition.Busy = true
	short := ViewLinesAt(state, 80, 10, 0)
	if len(short) != 10 || short[len(short)-1] != StatusLine(state, 80) || !strings.Contains(strings.Join(short, "\n"), confirm.HintYesNo) {
		t.Fatalf("busy confirmation does not fit above the final row on height 10: %q", short)
	}
}

func TestTransitionPanelTextAndFullWidthMeasurements(t *testing.T) {
	cases := []struct {
		name, title, phrase string
		kind                runner.TransitionKind
		stage               runner.TransitionStage
	}{
		{"restart-stop", "再起動中", "アプリを終了しています", runner.TransitionRestart, runner.TransitionStop},
		{"restart-build", "再起動中", "ビルドしています", runner.TransitionRestart, runner.TransitionBuild},
		{"restart-launch", "再起動中", "アプリを起動しています", runner.TransitionRestart, runner.TransitionLaunch},
		{"restart-ready", "再起動中", "起動を確認しています", runner.TransitionRestart, runner.TransitionReady},
		{"quit", "終了中", "アプリを終了しています", runner.TransitionQuit, runner.TransitionStop},
		{"startup", "起動中", "アプリを起動しています", runner.TransitionStartup, runner.TransitionLaunch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := ViewLinesAt(runner.Model{State: runner.Running, Transition: runner.Transition{
				Active: true, Kind: tc.kind, Stage: tc.stage,
			}}, 80, 24, 1)
			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, tc.title) || !strings.Contains(joined, tc.phrase) || !strings.Contains(joined, "⠙") {
				t.Fatalf("panel text missing title/stage/spinner: %q", joined)
			}
			if tc.kind != runner.TransitionQuit && (!strings.Contains(joined, "→") || !strings.Contains(joined, "起動の確認")) {
				t.Fatalf("restart/startup stage list missing: %q", joined)
			}
			for _, line := range lines {
				if line == "" {
					continue
				}
				if got := termwidth.Of(line); got != 80 {
					t.Fatalf("full-width panel row measures %d cells, want 80: %q", got, line)
				}
			}
		})
	}
	failed := ViewLinesAt(runner.Model{State: runner.BuildFailed, Transition: runner.Transition{Result: runner.TransitionBuildFailed}}, 80, 24, 0)
	if !strings.Contains(strings.Join(failed, "\n"), "ビルドに失敗しました") {
		t.Fatalf("build failure result line missing: %q", failed)
	}
	unconfirmed := ViewLinesAt(runner.Model{State: runner.Running, Transition: runner.Transition{Result: runner.TransitionReadinessUnconfirmed}}, 80, 24, 0)
	if !strings.Contains(strings.Join(unconfirmed, "\n"), "起動を確認できませんでした") {
		t.Fatalf("readiness failure result line missing: %q", unconfirmed)
	}
}

func TestSpinnerTickUsesInjectedCommand(t *testing.T) {
	ticks := 0
	model := &teaModel{
		state: runner.InitialModel(), width: 80, height: 24,
		ready: func() {}, closed: make(chan struct{}),
		spinnerTick: func() tea.Cmd {
			ticks++
			return func() tea.Msg { return spinnerTickMsg{} }
		},
	}
	_, cmd := model.Update(startedMsg{})
	if cmd == nil || ticks != 1 {
		t.Fatalf("initial spinner command = %v, tick factory calls=%d", cmd != nil, ticks)
	}
	msg := cmd()
	_, next := model.Update(msg)
	if next == nil || model.spinnerFrame != 1 || ticks != 2 {
		t.Fatalf("spinner tick was not advanced through injected clock: frame=%d calls=%d", model.spinnerFrame, ticks)
	}
	model.Update(snapshotMsg(runner.Model{State: runner.Running}))
	if model.spinnerActive || model.spinnerFrame != 0 {
		t.Fatalf("spinner stayed active after transition closed: %+v", model)
	}
}

func TestMessageLineMeasuresFullWidthText(t *testing.T) {
	message := "ビルド中のため再起動を待っています"
	line := MessageLine(message, 12)
	if got := termwidth.Of(line); got > 12 {
		t.Fatalf("message display width = %d, want <= 12: %q", got, line)
	}
	if line == message || !strings.Contains(line, "…") {
		t.Fatalf("full-width message should be clipped with an ellipsis: %q", line)
	}
}

func TestKeyPressMsgMapsToRunnerKeys(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"r", tea.KeyPressMsg{Code: 'r', Text: "r"}, "r"},
		{"R", tea.KeyPressMsg{Code: 'R', Text: "R"}, "R"},
		{"q", tea.KeyPressMsg{Code: 'q', Text: "q"}, "q"},
		{"Q", tea.KeyPressMsg{Code: 'Q', Text: "Q"}, "Q"},
		{"y", tea.KeyPressMsg{Code: 'y', Text: "y"}, "y"},
		{"Y", tea.KeyPressMsg{Code: 'Y', Text: "Y"}, "Y"},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{"n", tea.KeyPressMsg{Code: 'n', Text: "n"}, "n"},
		{"N", tea.KeyPressMsg{Code: 'N', Text: "N"}, "N"},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, "esc"},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ModelKey(test.msg)
			if !ok || got != test.want {
				t.Fatalf("ModelKey(%q) = %q, %v; want %q, true", test.msg.String(), got, ok, test.want)
			}
		})
	}
	if _, ok := ModelKey(tea.KeyPressMsg{Code: 'x', Text: "x"}); ok {
		t.Fatal("unhandled key should not reach the runner model")
	}
}

func TestCtrlCKeyUsesForceStopModelPath(t *testing.T) {
	key, ok := ModelKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !ok {
		t.Fatal("Ctrl-C was not mapped")
	}
	state, effects := runner.Update(runner.InitialModel(), runner.Event{Kind: runner.KeyEvent, Key: key})
	if state.State != runner.Exiting || state.ExitCode != 130 {
		t.Fatalf("Ctrl-C state = %+v, want exiting with 130", state)
	}
	if len(effects) == 0 || effects[0].Kind != runner.ForceStopEffect {
		t.Fatalf("Ctrl-C effects = %+v, want forced stop first", effects)
	}
}

func TestTTYOutputWriterReportsBrokenPipeOnceAndStopsWriting(t *testing.T) {
	underlying := &failingUIWriter{}
	var stderr bytes.Buffer
	writer := newDiscardWriteErrors(underlying, &stderr)
	data := []byte("terminal output")
	for range 3 {
		n, err := writer.Write(data)
		if err != nil || n != len(data) {
			t.Fatalf("TTY write = (%d, %v), want (%d, nil)", n, err, len(data))
		}
	}
	if underlying.writes != 1 {
		t.Fatalf("failed TTY output writes = %d, want one", underlying.writes)
	}
	if got := strings.Count(stderr.String(), runner.OutputFailureMessage); got != 1 {
		t.Fatalf("TTY output warning count = %d, want one; stderr=%q", got, stderr.String())
	}
}

func TestTeaModelDropsKeyWhenRunnerQueueIsFull(t *testing.T) {
	keys := make(chan string, 32)
	for range cap(keys) {
		keys <- "queued"
	}
	model := &teaModel{keys: keys, closed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("key update blocked when the runner key queue was full")
	}
}

func TestTeaModelPreservesControlKeysWhenRunnerQueueIsFull(t *testing.T) {
	keysToPreserve := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, "esc"},
		{"y", tea.KeyPressMsg{Code: 'y', Text: "y"}, "y"},
		{"Y", tea.KeyPressMsg{Code: 'Y', Text: "Y"}, "Y"},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{"n", tea.KeyPressMsg{Code: 'n', Text: "n"}, "n"},
		{"N", tea.KeyPressMsg{Code: 'N', Text: "N"}, "N"},
	}
	for _, test := range keysToPreserve {
		t.Run(test.name, func(t *testing.T) {
			keys := make(chan string, 32)
			closed := make(chan struct{})
			delivery := newKeyDelivery(keys, closed)
			t.Cleanup(func() { close(closed); <-delivery.done })
			for len(keys) < cap(keys) {
				keys <- "queued"
			}
			model := &teaModel{keys: keys, delivery: delivery, closed: closed}
			model.Update(test.msg)
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			for {
				select {
				case got := <-keys:
					if got == test.want {
						return
					}
					if got != "queued" {
						t.Fatalf("delivered key = %q, want filler or %q", got, test.want)
					}
				case <-timer.C:
					t.Fatalf("control key %q was dropped while runner queue was full", test.want)
				}
			}
		})
	}
}

func TestPrintlnFallsBackToStderrAfterProgramExit(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStderr := os.Stderr
	os.Stderr = writeEnd
	defer func() { os.Stderr = previousStderr }()
	presenter := New(strings.NewReader(""), io.Discard)
	go presenter.printLoop()
	close(presenter.runDone)

	presenter.Println("final report")
	close(presenter.closed)
	select {
	case <-presenter.printDone:
	case <-time.After(time.Second):
		t.Fatal("print loop did not finish")
	}
	_ = writeEnd.Close()
	os.Stderr = previousStderr
	output, err := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(output), "final report\n"; got != want {
		t.Fatalf("fallback output = %q, want %q", got, want)
	}
}

func TestPrintlnDoesNotDuplicateAnInFlightLineAfterProgramExit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, 1)
	var fallback bytes.Buffer
	presenter := &Presenter{
		runDone: make(chan struct{}), closed: make(chan struct{}), prints: make(chan printRequest),
		fallback: &fallback, printDone: make(chan struct{}),
		programPrintln: func(line string) {
			close(started)
			<-release
			delivered <- line
		},
	}
	go presenter.printLoop()
	callDone := make(chan struct{})
	go func() {
		presenter.Println("in-flight")
		close(callDone)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("print loop did not start the in-flight line")
	}
	close(presenter.runDone)
	close(release)
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("Println did not finish after the program exited")
	}
	select {
	case line := <-delivered:
		if line != "in-flight" {
			t.Fatalf("program received %q, want in-flight", line)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight line was not delivered to the program")
	}

	presenter.Println("after-exit")
	close(presenter.closed)
	select {
	case <-presenter.printDone:
	case <-time.After(time.Second):
		t.Fatal("print loop did not finish after close")
	}
	if got, want := fallback.String(), "after-exit\n"; got != want {
		t.Fatalf("fallback output = %q, want %q", got, want)
	}
}

// 板が入らない幅でも、確認ダイアログが開いていれば y / n の案内を出す (段の名前だけだと Q の後に何を押せばよいか分からない)。
func TestCompactTransitionLineShowsConfirmationPrompt(t *testing.T) {
	for _, tc := range []struct {
		confirm runner.Confirm
		want    string
	}{{runner.ConfirmQuit, "終了y/n"}, {runner.ConfirmRestart, "再起動y/n"}} {
		state := runner.Model{State: runner.Building, Confirm: tc.confirm,
			Transition: runner.Transition{Active: true, Kind: runner.TransitionStartup, Stage: runner.TransitionBuild}}
		lines := ViewLinesAt(state, 9, 24, 0)
		if len(lines) != 1 || lines[0] != tc.want {
			t.Fatalf("confirm %q at width 9: lines = %q, want [%q]", tc.confirm, lines, tc.want)
		}
	}
}

// 板を閉じた後の確認ダイアログも、板の入らない幅では 1 行にする (confirm.Dialog は最小 10 桁ではみ出す)。
func TestConfirmOutsideTransitionFitsAtWidthsOneThroughNine(t *testing.T) {
	want := map[runner.Confirm]map[int]string{
		runner.ConfirmQuit: {1: "y", 2: "y/", 3: "y/n", 4: "Qy/n", 5: "終y/n", 6: "終y/n",
			7: "終了y/n", 8: "終了y/n", 9: "終了y/n"},
		runner.ConfirmRestart: {1: "y", 2: "y/", 3: "y/n", 4: "Ry/n", 5: "再y/n", 6: "再y/n",
			7: "再y/n", 8: "再y/n", 9: "再起動y/n"},
	}
	for confirmState, rows := range want {
		for width := 1; width <= 9; width++ {
			state := runner.Model{State: runner.Running, PID: 7, Confirm: confirmState}
			lines := ViewLinesAt(state, width, 24, 0)
			for _, line := range lines {
				if got := termwidth.Of(line); got > width {
					t.Fatalf("confirm %q at width %d: row %q is %d cells wide; lines = %q", confirmState, width, line, got, lines)
				}
			}
			if len(lines) != 2 || lines[1] != StatusLine(state, width) {
				t.Fatalf("confirm %q at width %d: lines = %q, want the confirm row and the status row", confirmState, width, lines)
			}
			if expected := rows[width]; lines[0] != expected {
				t.Fatalf("confirm %q at width %d: confirm row = %q, want %q", confirmState, width, lines[0], expected)
			}
		}
	}
	if lines := ViewLinesAt(runner.Model{State: runner.Running, Confirm: runner.ConfirmQuit}, 10, 24, 0); len(lines) < 3 || !strings.Contains(lines[0], "┌") {
		t.Fatalf("width 10 should keep the confirm dialog: %q", lines)
	}
}

// R / Q の確認ダイアログも進捗板と同じく端末の中央に置き、メッセージとステータスは最下部に残す。
func TestConfirmDialogIsCenteredOnScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   runner.Model
		footers int
	}{
		{"quit", runner.Model{State: runner.Running, PID: 7, Confirm: runner.ConfirmQuit}, 1},
		{"restart with message", runner.Model{State: runner.Running, PID: 7, Confirm: runner.ConfirmRestart, Message: "restart requested"}, 2},
	} {
		for _, height := range []int{24, 40} {
			const width = 120
			lines := ViewLinesAt(tc.state, width, height, 0)
			if !strings.Contains(lines[0], "┌") {
				t.Fatalf("%s/height %d: view must start with the dialog (logs own the rows above): %q", tc.name, height, lines)
			}
			panelHeight := -1
			for index, line := range lines {
				if strings.Contains(line, "░") {
					panelHeight = index + 1
				}
			}
			if panelHeight < 0 {
				t.Fatalf("%s/height %d: dialog bottom not found: %q", tc.name, height, lines)
			}
			if screenTop, want := height-len(lines), (height-panelHeight)/2; screenTop != want {
				t.Fatalf("%s/height %d: dialog screen top = %d, want %d", tc.name, height, screenTop, want)
			}
			left := termwidth.Of(strings.SplitN(lines[0], "┌", 2)[0])
			dialogWidth := termwidth.Of(strings.TrimSpace(lines[0]))
			if want := (width - dialogWidth) / 2; left != want || left == 0 {
				t.Fatalf("%s/height %d: dialog left edge = %d, want %d (non-zero)", tc.name, height, left, want)
			}
			if lines[len(lines)-1] != StatusLine(tc.state, width) {
				t.Fatalf("%s/height %d: final row is not the status: %q", tc.name, height, lines[len(lines)-1])
			}
			if tc.footers == 2 && lines[len(lines)-2] != MessageLine(tc.state.Message, width) {
				t.Fatalf("%s/height %d: message is not just above the status: %q", tc.name, height, lines[len(lines)-2])
			}
		}
	}
}

// crashed ではアプリはもう終わっているので、Q の確認は runner を終えるかを聞く。
func TestQuitConfirmFromCrashedAsksToQuitRunner(t *testing.T) {
	joined := strings.Join(ViewLinesAt(runner.Model{State: runner.Crashed, Confirm: runner.ConfirmQuit}, 80, 24, 0), "\n")
	if !strings.Contains(joined, "restartable を終了しますか？") || strings.Contains(joined, "アプリを終了しますか？") {
		t.Fatalf("crashed quit confirmation = %q", joined)
	}
}
