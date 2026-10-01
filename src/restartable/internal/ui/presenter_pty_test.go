package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jiikko/dotfiles/src/restartable/internal/runner"
)

// TestPTYRunnerHelper is the child process used by the real-terminal tests.
func TestPTYRunnerHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_PTY_HELPER") != "1" {
		return
	}
	code, err := runner.Run(runner.Config{
		RunArgs:     []string{"/bin/sh", "-c", os.Getenv("RESTARTABLE_PTY_RUN")},
		ControlPath: os.Getenv("RESTARTABLE_PTY_CONTROL"),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Headless:    false,
		Presenter:   New(os.Stdin, os.Stdout),
		TermGrace:   100 * time.Millisecond,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
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

func startPTYRunner(t *testing.T, run string) *ptyRunner {
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
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
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

func TestPTYRunnerHandlesInputBurstBeyondEventAndKeyBuffers(t *testing.T) {
	r := startPTYRunner(t, "exec /bin/sleep 30")
	waitPTYOutput(t, r, "initial rendered status line", func(output string) bool {
		return strings.Contains(output, "[R] 再起動") && strings.Contains(output, "running (pid ")
	})
	if _, err := r.master.Write([]byte(strings.Repeat("r", 4096))); err != nil {
		t.Fatal(err)
	}
	if code := signalPTYRunner(t, r); code != 143 {
		t.Fatalf("runner exit code after input burst = %d, want 143", code)
	}
}

func signalPTYRunner(t *testing.T, r *ptyRunner) int {
	t.Helper()
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
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
	case <-time.After(5 * time.Second):
		t.Fatalf("runner did not exit after SIGTERM; PTY output: %q", r.text())
	}
	return -1
}
