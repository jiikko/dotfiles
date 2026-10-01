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
	writer := &discardWriteErrors{writer: underlying, stderr: &stderr}
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
