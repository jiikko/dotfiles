package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

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
