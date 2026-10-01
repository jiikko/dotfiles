package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jiikko/dotfiles/src/restartable/internal/control"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
)

// TestPTYRunnerHelper is the child process used by the real-terminal tests.
func TestPTYRunnerHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_PTY_HELPER") != "1" {
		return
	}
	presenter := New(os.Stdin, os.Stdout)
	var configuredPresenter runner.Presenter = presenter
	if statePath := os.Getenv("RESTARTABLE_PTY_PANEL_STATE"); statePath != "" {
		configuredPresenter = &recordingPresenter{Presenter: presenter, statePath: statePath}
	}
	code, err := runner.Run(runner.Config{
		RunArgs:         []string{"/bin/sh", "-c", os.Getenv("RESTARTABLE_PTY_RUN")},
		ReadyCommand:    os.Getenv("RESTARTABLE_PTY_READY"),
		ReadyTimeout:    envDuration("RESTARTABLE_PTY_READY_TIMEOUT", 120*time.Second),
		ReadyInterval:   envDuration("RESTARTABLE_PTY_READY_INTERVAL", 500*time.Millisecond),
		ControlPath:     os.Getenv("RESTARTABLE_PTY_CONTROL"),
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		Headless:        false,
		StdinIsTerminal: true,
		Presenter:       configuredPresenter,
		TermGrace:       100 * time.Millisecond,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

// TestPTYDetachedOutputHelper creates a process that leaves the runner's
// process group while keeping the shared output pipe open. The test releases
// it after SIGTERM so output draining overlaps a controlled key burst.
func TestPTYDetachedOutputHelper(t *testing.T) {
	switch os.Getenv("RESTARTABLE_PTY_DESCENDANT_MODE") {
	case "leader":
		detached := exec.Command(os.Args[0], "-test.run=^TestPTYDetachedOutputHelper$")
		detached.Env = append(os.Environ(), "RESTARTABLE_PTY_DESCENDANT_MODE=detached")
		detached.Stdin, detached.Stdout, detached.Stderr = os.Stdin, os.Stdout, os.Stderr
		detached.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := detached.Start(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("RESTARTABLE_PTY_DESCENDANT_READY"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		holdRead, holdWrite, err := os.Pipe()
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer func() { _ = holdRead.Close(); _ = holdWrite.Close() }()
		var hold [1]byte
		_, _ = holdRead.Read(hold[:])
	case "detached":
		release := os.Getenv("RESTARTABLE_PTY_DESCENDANT_RELEASE")
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(release); err == nil {
				_, _ = fmt.Fprintln(os.Stdout, "delayed-output-line")
				return
			}
			<-ticker.C
		}
	}
}

type ptyRunner struct {
	cmd    *exec.Cmd
	master *os.File
	path   string
	done   chan struct{}
	err    error

	outputMu sync.Mutex
	output   bytes.Buffer
}

type recordingPresenter struct {
	*Presenter
	statePath string
}

func (p *recordingPresenter) Render(state runner.Model) {
	active := "active=false"
	if state.Transition.Active {
		active = "active=true"
	}
	_ = os.WriteFile(p.statePath, []byte(active), 0600)
	p.Presenter.Render(state)
}

func startPTYRunner(t *testing.T, run string) *ptyRunner {
	return startPTYRunnerWithEnvAndSize(t, run, nil)
}

func startPTYRunnerWithEnv(t *testing.T, run string, extraEnv ...string) *ptyRunner {
	return startPTYRunnerWithEnvAndSize(t, run, nil, extraEnv...)
}

func startPTYRunnerWithEnvAndSize(t *testing.T, run string, size *pty.Winsize, extraEnv ...string) *ptyRunner {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rpty-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "runner.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPTYRunnerHelper$")
	cmd.Env = append(os.Environ(),
		"RESTARTABLE_PTY_HELPER=1",
		"RESTARTABLE_PTY_CONTROL="+path,
		"RESTARTABLE_PTY_RUN="+run,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	if size == nil {
		size = &pty.Winsize{Rows: 24, Cols: 100}
	}
	master, err := pty.StartWithSize(cmd, size)
	if err != nil {
		t.Fatal(err)
	}
	r := &ptyRunner{cmd: cmd, master: master, path: path, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = cmd.Wait()
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := master.Read(buf)
			if n > 0 {
				r.outputMu.Lock()
				_, _ = r.output.Write(buf[:n])
				r.outputMu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		select {
		case <-r.done:
		default:
			_ = r.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-r.done:
			case <-time.After(4 * time.Second):
				_ = r.cmd.Process.Kill()
				<-r.done
			}
		}
		_ = r.master.Close()
	})
	return r
}

func (r *ptyRunner) text() string {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	return r.output.String()
}

func TestPTYReadyPanelStaysVisibleUntilReadyThenCloses(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "ready")
	statePath := filepath.Join(dir, "panel-state")
	r := startPTYRunnerWithEnv(t, "exec /bin/sleep 30",
		"RESTARTABLE_PTY_READY=test -e "+ptyShellQuote(gate),
		"RESTARTABLE_PTY_READY_TIMEOUT=5s",
		"RESTARTABLE_PTY_READY_INTERVAL=5ms",
		"RESTARTABLE_PTY_PANEL_STATE="+statePath,
	)
	waitPTYOutput(t, r, "startup panel with readiness stage", func(output string) bool {
		return strings.Contains(output, "起動中") && strings.Contains(output, "起動を確認しています")
	})
	waitPTYCondition(t, r, "active startup panel state", func() bool {
		state, err := os.ReadFile(statePath)
		return err == nil && string(state) == "active=true"
	})
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitPTYCondition(t, r, "ready status and closed progress panel", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		status, err := control.Call(ctx, r.path, control.Status)
		state, stateErr := os.ReadFile(statePath)
		return err == nil && stateErr == nil && status.Ready && string(state) == "active=false"
	})
	if code := signalPTYRunner(t, r); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func ptyShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func waitPTYOutput(t *testing.T, r *ptyRunner, description string, predicate func(string) bool) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		if output := r.text(); predicate(output) {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s; PTY output: %q", description, r.text())
		case <-r.done:
			t.Fatalf("runner exited before %s: %v; PTY output: %q", description, r.err, r.text())
		}
	}
}

func TestPTYRunnerRendersStatusAndConfirmAndUsesNullStdin(t *testing.T) {
	r := startPTYRunner(t, `read value; echo restartable-stdin-status=$?; exec /bin/sleep 30`)
	waitPTYOutput(t, r, "rendered status line and non-interactive child stdin", func(output string) bool {
		return strings.Contains(output, "[R] 再起動") && strings.Contains(output, "running (pid ") && strings.Contains(output, "restartable-stdin-status=1")
	})

	if _, err := r.master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	waitPTYOutput(t, r, "quit confirmation dialog", func(output string) bool {
		return strings.Contains(output, "アプリを終了しますか？")
	})
	if _, err := r.master.Write([]byte("n")); err != nil {
		t.Fatal(err)
	}
	if code := signalPTYRunner(t, r); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestPTYRunnerRendersStatusWithZeroByZeroTerminalSize(t *testing.T) {
	r := startPTYRunnerWithEnvAndSize(t, "exec /bin/sleep 30", &pty.Winsize{Rows: 0, Cols: 0})
	waitPTYOutput(t, r, "status row with zero terminal dimensions", func(output string) bool {
		return strings.Contains(output, "[R] 再起動") && strings.Contains(output, "running (pid ")
	})
	if code := signalPTYRunner(t, r); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestPTYRunnerDrainsLateOutputDuringTerminationWithInputBurst(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rpty-drain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	readyPath := filepath.Join(dir, "ready")
	releasePath := filepath.Join(dir, "release")
	run := fmt.Sprintf("exec '%s' -test.run=^TestPTYDetachedOutputHelper$", strings.ReplaceAll(os.Args[0], "'", "'\\''"))
	r := startPTYRunnerWithEnv(t, run,
		"RESTARTABLE_PTY_DESCENDANT_MODE=leader",
		"RESTARTABLE_PTY_DESCENDANT_READY="+readyPath,
		"RESTARTABLE_PTY_DESCENDANT_RELEASE="+releasePath,
	)
	t.Cleanup(func() { _ = os.WriteFile(releasePath, []byte("release"), 0600) })
	waitPTYOutput(t, r, "initial rendered status line", func(output string) bool {
		return strings.Contains(output, "[R] 再起動") && strings.Contains(output, "running (pid ")
	})
	waitPTYCondition(t, r, "detached descendant holding output pipe", func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	})
	ready, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(ready)))
	if err != nil {
		t.Fatalf("detached-output leader PID: %v; value=%q", err, ready)
	}
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
	waitPTYCondition(t, r, "runner draining after its child group exited", func() bool {
		return errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH)
	})
	waitPTYCondition(t, r, "actor leaving its event loop for output draining", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		_, err := control.Call(ctx, r.path, control.Status)
		return errors.Is(err, context.DeadlineExceeded)
	})
	if _, err := r.master.Write([]byte(strings.Repeat("r", 400))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := waitPTYRunner(t, r, 5*time.Second); code != 143 {
		t.Fatalf("runner exit code after drain-window burst = %d, want 143", code)
	}
	if output := r.text(); !strings.Contains(output, "delayed-output-line") {
		t.Fatalf("late descendant output was not printed: %q", output)
	}
}

func waitPTYCondition(t *testing.T, r *ptyRunner, description string, predicate func() bool) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s; PTY output: %q", description, r.text())
		case <-r.done:
			t.Fatalf("runner exited before %s: %v; PTY output: %q", description, r.err, r.text())
		}
	}
}

func signalPTYRunner(t *testing.T, r *ptyRunner) int {
	t.Helper()
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
	return waitPTYRunner(t, r, 5*time.Second)
}

func waitPTYRunner(t *testing.T, r *ptyRunner, limit time.Duration) int {
	t.Helper()
	select {
	case <-r.done:
		if r.err == nil {
			return 0
		}
		var exit *exec.ExitError
		if errors.As(r.err, &exit) {
			return exit.ExitCode()
		}
		t.Fatalf("wait for runner: %v", r.err)
	case <-time.After(limit):
		t.Fatalf("runner did not exit after SIGTERM; PTY output: %q", r.text())
	}
	return -1
}
