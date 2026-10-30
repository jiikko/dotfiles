package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jiikko/dotfiles/src/restartable/internal/runnertest"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jiikko/dotfiles/src/restartable/internal/control"

	"testwait"
)

type presenterSnapshot struct {
	Model       Model
	RenderCount int
}

type integrationPresenter struct {
	keys      chan string
	conn      *net.UnixConn
	path      string
	statePath string
	stop      chan struct{}
	pumpDone  chan struct{}
	renders   int
}

func newIntegrationPresenter(keyPath, statePath string) (*integrationPresenter, error) {
	_ = os.Remove(keyPath)
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: keyPath, Net: "unixgram"})
	if err != nil {
		return nil, err
	}
	p := &integrationPresenter{keys: make(chan string, 32), conn: conn, path: keyPath, statePath: statePath,
		stop: make(chan struct{}), pumpDone: make(chan struct{})}
	go p.readKeys()
	return p, nil
}

func (p *integrationPresenter) readKeys() {
	defer close(p.pumpDone)
	buf := make([]byte, 128)
	for {
		n, _, err := p.conn.ReadFromUnix(buf)
		if err != nil {
			return
		}
		select {
		case p.keys <- string(buf[:n]):
		case <-p.stop:
			return
		}
	}
}

func (p *integrationPresenter) Render(m Model) {
	p.renders++
	data, err := json.Marshal(presenterSnapshot{Model: m, RenderCount: p.renders})
	if err != nil {
		return
	}
	tmp := p.statePath + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		_ = os.Rename(tmp, p.statePath)
	}
}

func (p *integrationPresenter) Keys() <-chan string { return p.keys }

func (p *integrationPresenter) Close() error {
	close(p.stop)
	err := p.conn.Close()
	<-p.pumpDone
	close(p.keys)
	_ = os.Remove(p.path)
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// TestRunnerHelper is re-executed as a separate process by integration tests.
func TestRunnerHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_TEST_HELPER") != "1" {
		return
	}
	stdin := io.Reader(strings.NewReader(""))
	stdinIsTerminal := os.Getenv("RESTARTABLE_TEST_STDIN_TERMINAL") == "1"
	if stdinIsTerminal {
		stdin = os.Stdin
	}
	var presenter Presenter
	if signalDuringStart := os.Getenv("RESTARTABLE_TEST_SIGNAL_DURING_START"); signalDuringStart != "" {
		presenter = signalDuringStartPresenter{markerPath: signalDuringStart}
	} else if keyPath := os.Getenv("RESTARTABLE_TEST_KEY_SOCKET"); keyPath != "" {
		var err error
		presenter, err = newIntegrationPresenter(keyPath, os.Getenv("RESTARTABLE_TEST_PRESENTER_STATE"))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code, err := Run(Config{
		BuildCommand:        os.Getenv("RESTARTABLE_TEST_BUILD"),
		RunArgs:             []string{"/bin/sh", "-c", os.Getenv("RESTARTABLE_TEST_RUN")},
		StopCommand:         os.Getenv("RESTARTABLE_TEST_STOP"),
		StopCommandTimeout:  runnertest.EnvDuration("RESTARTABLE_TEST_STOP_TIMEOUT", 250*time.Millisecond),
		TermGrace:           runnertest.EnvDuration("RESTARTABLE_TEST_TERM_GRACE", 60*time.Millisecond),
		ReadyCommand:        os.Getenv("RESTARTABLE_TEST_READY"),
		ReadyTimeout:        runnertest.EnvDuration("RESTARTABLE_TEST_READY_TIMEOUT", 120*time.Second),
		ReadyInterval:       runnertest.EnvDuration("RESTARTABLE_TEST_READY_INTERVAL", 500*time.Millisecond),
		ReadyAttemptTimeout: runnertest.EnvDuration("RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT", 5*time.Second),
		ReadyCleanupGrace:   runnertest.EnvDuration("RESTARTABLE_TEST_READY_CLEANUP_GRACE", 30*time.Millisecond),
		IDEnv:               os.Getenv("RESTARTABLE_TEST_ID_ENV"),
		ControlPath:         os.Getenv("RESTARTABLE_TEST_SOCKET"),
		Stdin:               stdin, Stdout: os.Stdout, Stderr: os.Stderr, Headless: os.Getenv("RESTARTABLE_TEST_INTERACTIVE") != "1", StdinIsTerminal: stdinIsTerminal,
		Presenter: presenter,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

type signalDuringStartPresenter struct{ markerPath string }

func (p signalDuringStartPresenter) Start() error {
	return syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

func (p signalDuringStartPresenter) Render(model Model) {
	if model.State == Exiting {
		_ = os.WriteFile(p.markerPath, []byte("handled"), 0600)
	}
}

func (signalDuringStartPresenter) Keys() <-chan string { return nil }
func (signalDuringStartPresenter) Close() error        { return nil }

type testRunner struct {
	cmd     *exec.Cmd
	path    string
	stdout  string
	stderr  string
	done    chan struct{}
	waitErr error
}

// ptyStdinRunner は標準入力が端末のままの headless runner (標準出力は pipe で集める)。
type ptyStdinRunner struct{ *runnertest.PTY }

func startPTYStdinHeadlessRunner(t *testing.T, socketPath string, extraEnv ...string) *ptyStdinRunner {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerHelper$")
	cmd.Env = append(os.Environ(),
		"RESTARTABLE_TEST_HELPER=1",
		"RESTARTABLE_TEST_SOCKET="+socketPath,
		"RESTARTABLE_TEST_RUN="+`if read value; then echo child-stdin-read; else echo child-stdin-eof; fi; exec /bin/sleep 30`,
		"RESTARTABLE_TEST_STDIN_TERMINAL=1",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdin = slave
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stdoutWrite
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	_ = stdoutWrite.Close()
	_ = slave.Close()
	t.Cleanup(func() { _ = stdoutRead.Close() })
	return &ptyStdinRunner{runnertest.Attach(t, cmd, master, stdoutRead, socketPath)}
}

func (r *ptyStdinRunner) waitOutput(t *testing.T, want string) {
	t.Helper()
	r.WaitText(t, fmt.Sprintf("%q", want), 3*time.Second, func(output string) bool { return strings.Contains(output, want) })
}

func (r *ptyStdinRunner) signalAndWait(t *testing.T, sig syscall.Signal) int {
	t.Helper()
	if err := r.Cmd.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
	testwait.Recv(t, "runner did not exit; output", r.Done, testwait.Within(4*time.Second), testwait.Diag(func() string { return fmt.Sprintf("runner did not exit; output=%q", r.Text()) }))
	if r.Err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(r.Err, &exit) {
		return exit.ExitCode()
	}
	t.Fatalf("wait for runner: %v", r.Err)
	return -1
}

func sendIntegrationKey(t *testing.T, path, key string) {
	t.Helper()
	conn, err := net.Dial("unixgram", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, key); err != nil {
		t.Fatal(err)
	}
}

func readPresenterSnapshot(path string) (presenterSnapshot, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return presenterSnapshot{}, false
	}
	var snapshot presenterSnapshot
	if json.Unmarshal(data, &snapshot) != nil {
		return presenterSnapshot{}, false
	}
	return snapshot, true
}

func waitPresenterSnapshot(t *testing.T, path, description string, predicate func(presenterSnapshot) bool) presenterSnapshot {
	t.Helper()
	var snapshot presenterSnapshot
	testwait.Until(t, description, func() bool {
		got, ok := readPresenterSnapshot(path)
		if !ok {
			return false
		}
		snapshot = got
		return predicate(got)
	}, testwait.Within(3*time.Second))

	return snapshot
}

func sendKeyAndWaitRender(t *testing.T, keyPath, statePath, key string) presenterSnapshot {
	t.Helper()
	before, ok := readPresenterSnapshot(statePath)
	if !ok {
		t.Fatal("runner has not rendered an initial snapshot")
	}
	sendIntegrationKey(t, keyPath, key)
	return waitPresenterSnapshot(t, statePath, "key "+key+" to be rendered", func(got presenterSnapshot) bool {
		return got.RenderCount > before.RenderCount
	})
}

func startTestRunner(t *testing.T, options map[string]string) *testRunner {
	return startTestRunnerMode(t, options, true)
}

func startTestRunnerMode(t *testing.T, options map[string]string, waitForStatus bool) *testRunner {
	return startTestRunnerModeWithBrokenStdout(t, options, waitForStatus, false)
}

func startTestRunnerWithBrokenStdout(t *testing.T, options map[string]string) *testRunner {
	return startTestRunnerModeWithBrokenStdout(t, options, true, true)
}

func startTestRunnerModeWithBrokenStdout(t *testing.T, options map[string]string, waitForStatus, brokenStdout bool) *testRunner {
	t.Helper()
	dir := runnertest.ShortTempDir(t, "rnt-")
	path := filepath.Join(dir, "runner.sock")
	stdout := filepath.Join(t.TempDir(), "stdout.log")
	stderr := filepath.Join(t.TempDir(), "stderr.log")
	var out, pipeReader *os.File
	var err error
	if brokenStdout {
		pipeReader, out, err = os.Pipe()
	} else {
		out, err = os.Create(stdout)
	}
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(stderr)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerHelper$")
	env := append(os.Environ(), "RESTARTABLE_TEST_HELPER=1", "RESTARTABLE_TEST_SOCKET="+path)
	for key, value := range options {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = out
	cmd.Stderr = errFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	if pipeReader != nil {
		_ = pipeReader.Close()
	}
	_ = errFile.Close()
	r := &testRunner{cmd: cmd, path: path, stdout: stdout, stderr: stderr, done: make(chan struct{})}
	go func() { r.waitErr = cmd.Wait(); close(r.done) }()
	t.Cleanup(func() {
		select {
		case <-r.done:
		default:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-r.done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-r.done
			}
		}
	})
	if waitForStatus {
		testwait.Until(t, "runner control socket", func() bool {
			select {
			case <-r.done:
				t.Fatalf("runner exited before opening control socket: %v; stderr: %s", r.waitErr, readFile(stderr))
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			response, err := control.Call(ctx, path, control.Status)
			return err == nil && response.OK
		}, testwait.Within(3*time.Second))

	}
	return r
}

func (r *testRunner) status() (control.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	return control.Call(ctx, r.path, control.Status)
}

func (r *testRunner) waitStatus(t *testing.T, want string) control.Response {
	t.Helper()
	var response control.Response
	testwait.Until(t, "runner state "+want, func() bool {
		got, err := r.status()
		if err != nil {
			return false
		}
		response = got
		return got.State == want
	}, testwait.Within(3*time.Second))

	return response
}

func (r *testRunner) signalAndWait(t *testing.T, sig syscall.Signal) int {
	t.Helper()
	_ = r.cmd.Process.Signal(sig)
	testwait.Recv(t, "runner did not exit after signal", r.done, testwait.Within(4*time.Second), testwait.Diag(func() string {
		return fmt.Sprintf("runner did not exit after signal %v; stderr: %s", sig, readFile(r.stderr))
	}))
	err := r.waitErr
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	t.Fatalf("wait runner: %v", err)
	return -1
}

// shortTempDir は unix socket を置く一時ディレクトリ。socket の path は 103 byte までなので、
// 長い t.TempDir() (macOS では /var/folders/...) ではなく /tmp の直下に作り、テストの終わりに消す。

func readFile(path string) string { data, _ := os.ReadFile(path); return string(data) }

func TestHeadlessRunnerWithTerminalStdinDoesNotPassTTYToChild(t *testing.T) {
	dir := runnertest.ShortTempDir(t, "rstin-")
	socketPath := filepath.Join(dir, "runner.sock")
	stopMarker := filepath.Join(dir, "stop-child")
	run := fmt.Sprintf(`rm -f %s; if read value; then echo run-stdin-read; else echo run-stdin-eof; fi; while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0`, runnertest.ShellQuote(stopMarker), runnertest.ShellQuote(stopMarker))
	stop := `if read value; then echo stop-stdin-read; else echo stop-stdin-eof; fi; touch ` + runnertest.ShellQuote(stopMarker)
	r := startPTYStdinHeadlessRunner(t, socketPath,
		"RESTARTABLE_TEST_BUILD=if read value; then echo build-stdin-read; else echo build-stdin-eof; fi",
		"RESTARTABLE_TEST_RUN="+run,
		"RESTARTABLE_TEST_STOP="+stop,
	)
	r.waitOutput(t, "build-stdin-eof")
	r.waitOutput(t, "run-stdin-eof")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	response, err := control.Call(ctx, socketPath, control.Restart)
	cancel()
	if err != nil {
		t.Fatalf("control restart: %v; child output=%q", err, r.Text())
	}
	if !response.OK || response.Generation != 2 {
		t.Fatalf("control restart response = %+v, want successful generation 2", response)
	}
	r.waitOutput(t, "stop-stdin-eof")
	testwait.Until(t, "second build with null stdin", func() bool {
		return strings.Count(r.Text(), "build-stdin-eof") >= 2
	}, testwait.Within(3*time.Second))

	if output := r.Text(); strings.Contains(output, "stdin-read") {
		t.Fatalf("a child received the terminal stdin: %q", output)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestBuildFailureNeverRunsStaleArtifactAndRestartWaitsForNewRun(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	artifact := filepath.Join(dir, "artifact")
	launches := filepath.Join(dir, "launches")
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; if [ "$n" -eq 1 ]; then echo stale > %s; exit 1; fi; echo fresh > %s`, runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(artifact), runnertest.ShellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, runnertest.ShellQuote(artifact), runnertest.ShellQuote(launches), runnertest.ShellQuote(launches))
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
	r.waitStatus(t, string(BuildFailed))
	if _, err := os.Stat(launches); !os.IsNotExist(err) {
		t.Fatalf("run started from failed build: %q (%v)", readFile(launches), err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Generation != 1 || response.PID == nil {
		t.Fatalf("restart response = %+v", response)
	}
	testwait.Until(t, "new run to record artifact", func() bool { return strings.TrimSpace(readFile(launches)) != "" }, testwait.Within(time.Second))
	if got := strings.TrimSpace(readFile(launches)); got != "fresh" {
		t.Fatalf("run used artifact %q, want fresh", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestBuildFailureIsReturnedToControlRestart(t *testing.T) {
	r, _ := startInteractiveRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": "echo no; exit 17",
		"RESTARTABLE_TEST_RUN":   "exec /bin/sleep 30",
	})
	r.waitStatus(t, string(BuildFailed))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || !strings.Contains(response.Reason, "build failed") {
		t.Fatalf("failed restart response = %+v", response)
	}
	if got := r.waitStatus(t, string(BuildFailed)); got.PID != nil {
		t.Fatalf("build-failed status has PID: %+v", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestStopCommandFailureKeepsChildRunningAndPassesID(t *testing.T) {
	dir := t.TempDir()
	stopID := filepath.Join(dir, "stop-id")
	runID := filepath.Join(dir, "run-id")
	run := fmt.Sprintf(`echo "$RUNNER_INSTANCE" > %s; exec /bin/sleep 30`, runnertest.ShellQuote(runID))
	stop := fmt.Sprintf(`echo "$RUNNER_INSTANCE" > %s; exit 9`, runnertest.ShellQuote(stopID))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_STOP": stop,
		"RESTARTABLE_TEST_ID_ENV": "RUNNER_INSTANCE",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil {
		t.Fatalf("running response has no pid: %+v", status)
	}
	testwait.Until(t, "run id env", func() bool { return readFile(runID) != "" }, testwait.Within(time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || !strings.Contains(response.Reason, "stop-cmd failed") {
		t.Fatalf("stop failure response = %+v", response)
	}
	if got := r.waitStatus(t, string(Running)); got.PID == nil || *got.PID != *status.PID {
		t.Fatalf("child changed after stop failure: %+v", got)
	}
	if readFile(runID) != readFile(stopID) || strings.TrimSpace(readFile(runID)) != status.ID {
		t.Fatalf("id env mismatch: run=%q stop=%q status=%q", readFile(runID), readFile(stopID), status.ID)
	}
	if err := syscall.Kill(*status.PID, 0); err != nil {
		t.Fatalf("child was signaled after stop-cmd failed: %v", err)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestStopCommandSuccessWaitsWithoutSignalingChild(t *testing.T) {
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":  "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP": "exit 0",
	})
	status := r.waitStatus(t, string(Running))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	_, err := control.Call(ctx, r.path, control.Restart)
	var networkErr net.Error
	if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &networkErr) || !networkErr.Timeout()) {
		t.Fatalf("restart call error = %v, want caller timeout while stop waits", err)
	}
	stopping := r.waitStatus(t, string(Stopping))
	if stopping.PID != nil {
		t.Fatalf("stopping status should omit child pid: %+v", stopping)
	}
	if status.PID == nil {
		t.Fatalf("initial status has no pid: %+v", status)
	}
	if err := syscall.Kill(*status.PID, 0); err != nil {
		t.Fatalf("runner signaled child after successful stop-cmd: %v", err)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("SIGTERM exit code = %d, want 143", code)
	}
	if err := syscall.Kill(*status.PID, 0); err == nil {
		t.Fatalf("child pid %d survived forced shutdown", *status.PID)
	}
}

func TestStopCommandSuccessIgnoresRestartAndQuitKeysWithoutKillingChild(t *testing.T) {
	dir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(dir, "keys.sock")
	statePath := filepath.Join(t.TempDir(), "presenter.json")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP":            "exit 0",
		"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no running child")
	}
	type callResult struct {
		response control.Response
		err      error
	}
	restartResult := make(chan callResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		restartResult <- callResult{response: response, err: err}
	}()
	waitPresenterSnapshot(t, statePath, "successful stop-cmd acceptance", func(got presenterSnapshot) bool {
		return got.Model.State == Stopping && got.Model.StopAccepted
	})
	r.waitStatus(t, string(Stopping))
	beforeSecondRestart, ok := readPresenterSnapshot(statePath)
	if !ok {
		t.Fatal("runner has not rendered a snapshot")
	}
	secondRestartResult := make(chan callResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		secondRestartResult <- callResult{response: response, err: err}
	}()
	waitPresenterSnapshot(t, statePath, "control restart accepted while stopping", func(got presenterSnapshot) bool {
		return got.RenderCount > beforeSecondRestart.RenderCount && got.Model.State == Stopping
	})

	for _, key := range []string{"R", "Q", "y", "x"} {
		got := sendKeyAndWaitRender(t, keyPath, statePath, key)
		if got.Model.State != Stopping || got.Model.Confirm != ConfirmNone {
			t.Fatalf("key %q changed stopping model: %+v", key, got.Model)
		}
		if (key == "R" || key == "Q") && (!got.Model.Transition.Busy || got.Model.Message != "処理中") {
			t.Fatalf("key %q did not show processing during stop wait: %+v", key, got.Model)
		}
		if err := syscall.Kill(*initial.PID, 0); err != nil {
			t.Fatalf("key %q killed the application: %v", key, err)
		}
		select {
		case <-r.done:
			t.Fatalf("runner exited after key %q", key)
		default:
		}
	}
	status, err := r.status()
	if err != nil || status.State != string(Stopping) || status.PID != nil {
		t.Fatalf("status during accepted stop = %+v, err=%v", status, err)
	}
	if err := syscall.Kill(*initial.PID, 0); err != nil {
		t.Fatalf("status request killed the application: %v", err)
	}

	got := sendKeyAndWaitRender(t, keyPath, statePath, "esc")
	if got.Model.State != Running || got.Model.PID != *initial.PID || got.Model.StopAccepted {
		t.Fatalf("Esc did not cancel accepted stop: %+v", got.Model)
	}
	for i, resultCh := range []<-chan callResult{restartResult, secondRestartResult} {
		result := testwait.Recv(t, "restart request", resultCh, testwait.Within(4*time.Second), testwait.Diag(func() string { return fmt.Sprintf("restart request %d was not rejected after Esc", i+1) }))
		if result.err != nil || result.response.OK || result.response.Reason != "stop cancelled" {
			t.Fatalf("restart response %d after Esc = %+v, err=%v", i+1, result.response, result.err)
		}
	}
	if err := syscall.Kill(*initial.PID, 0); err != nil {
		t.Fatalf("Esc killed the application: %v", err)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("SIGTERM exit code = %d, want 143", code)
	}
}

func TestStopCommandFailureAfterChildExitExitsAndFailsRestart(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "stop-started")
	fifo := filepath.Join(dir, "stop-gate")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	stop := fmt.Sprintf(`touch %s; read ignored < %s; exit 9`, runnertest.ShellQuote(started), runnertest.ShellQuote(fifo))
	keyDir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(dir, "presenter.json")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP":            stop,
		"RESTARTABLE_TEST_STOP_TIMEOUT":    "10s",
		"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no initial child pid")
	}
	type callResult struct {
		response control.Response
		err      error
	}
	restartResult := make(chan callResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		restartResult <- callResult{response: response, err: err}
	}()
	r.waitStatus(t, string(Stopping))
	testwait.Until(t, "stop-cmd start marker", func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, testwait.Within(2*time.Second))

	if err := syscall.Kill(*initial.PID, syscall.SIGTERM); err != nil {
		t.Fatalf("terminate fake application: %v", err)
	}
	waitPresenterSnapshot(t, statePath, "child exit observed while stop-cmd is pending", func(got presenterSnapshot) bool {
		return got.Model.State == Stopping && got.Model.Message == "アプリは終了しました。停止コマンドの結果を待っています"
	})
	gate, err := os.OpenFile(fifo, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Close(); err != nil {
		t.Fatal(err)
	}
	result := testwait.Recv(t, "failed stop-cmd request was not rejected after child exit", restartResult, testwait.Within(4*time.Second))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.response.OK || !strings.Contains(result.response.Reason, "stop-cmd failed") {
		t.Fatalf("restart response after child exit = %+v", result.response)
	}
	testwait.Recv(t, "runner kept running after stop-cmd failed and child exited", r.done, testwait.Within(3*time.Second))
	if r.waitErr != nil {
		t.Fatalf("runner did not exit naturally after stop failure: %v; stderr: %s", r.waitErr, readFile(r.stderr))
	}
}

func TestFinishRefusesWhileChildAlive(t *testing.T) {
	proc, err := startProcess([]string{"/bin/sleep", "30"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !proc.finished.Load() {
			_ = signalGroup(proc.pid, syscall.SIGTERM)
			select {
			case <-proc.done:
			case <-time.After(time.Second):
				_ = signalGroup(proc.pid, syscall.SIGKILL)
				<-proc.done
			}
		}
	})
	var stderr bytes.Buffer
	a := &actor{cfg: Config{Stderr: &stderr}, model: Model{State: Exiting, PID: proc.pid}, child: proc, presenter: headlessPresenter{}}
	a.finish(0)
	if a.finished || a.model.State != Running || a.model.PID != proc.pid {
		t.Fatalf("finish was not refused safely: finished=%v model=%+v", a.finished, a.model)
	}
	if !strings.Contains(stderr.String(), "子が生きたまま終了しようとした") {
		t.Fatalf("missing refusal log: %q", stderr.String())
	}
	if err := syscall.Kill(proc.pid, 0); err != nil {
		t.Fatalf("finish guard killed the child: %v", err)
	}
}

func TestDrainOutputsNeverKillsLiveProcessLeader(t *testing.T) {
	proc, err := startProcess([]string{"/bin/sleep", "30"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !proc.finished.Load() {
			_ = signalGroup(proc.pid, syscall.SIGTERM)
			select {
			case <-proc.done:
			case <-time.After(time.Second):
				_ = signalGroup(proc.pid, syscall.SIGKILL)
				<-proc.done
			}
		}
	})
	var stderr bytes.Buffer
	a := &actor{cfg: Config{Stderr: &stderr}, allProcesses: []*process{proc}, outputDrainTimeout: 0}
	a.drainOutputs()
	if proc.finished.Load() {
		t.Fatal("output drain killed a live process-group leader")
	}
	if err := syscall.Kill(proc.pid, 0); err != nil {
		t.Fatalf("live process was killed by output drain: %v", err)
	}
}

func TestStopCommandTimeoutKeepsChildRunning(t *testing.T) {
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":          "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP":         "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP_TIMEOUT": "40ms",
		"RESTARTABLE_TEST_TERM_GRACE":   "20ms",
	})
	status := r.waitStatus(t, string(Running))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || !strings.Contains(response.Reason, "timeout") {
		t.Fatalf("timeout response = %+v", response)
	}
	if got := r.waitStatus(t, string(Running)); got.PID == nil || status.PID == nil || *got.PID != *status.PID {
		t.Fatalf("child changed after stop timeout: %+v", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestSIGTERMKillsTERMIgnoringChildGroupAndReturns143(t *testing.T) {
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        "trap '' TERM; while :; do /bin/sleep 0.02; done",
		"RESTARTABLE_TEST_TERM_GRACE": "40ms",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil {
		t.Fatal("no child pid")
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	testwait.Until(t, "TERM-ignoring child removal", func() bool { return errors.Is(syscall.Kill(*status.PID, 0), syscall.ESRCH) }, testwait.Within(time.Second))
}

func TestSIGTERMStopsGrandchildInChildProcessGroup(t *testing.T) {
	grandchildFile := filepath.Join(t.TempDir(), "grandchild-pid")
	run := fmt.Sprintf(`/bin/sleep 30 & echo $! > %s; wait`, runnertest.ShellQuote(grandchildFile))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run})
	status := r.waitStatus(t, string(Running))
	testwait.Until(t, "grandchild pid", func() bool { return strings.TrimSpace(readFile(grandchildFile)) != "" }, testwait.Within(time.Second))
	grandchild, err := strconv.Atoi(strings.TrimSpace(readFile(grandchildFile)))
	if err != nil {
		t.Fatal(err)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	testwait.Until(t, "grandchild process group cleanup", func() bool { return errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH) }, testwait.Within(time.Second))
	if status.PID == nil {
		t.Fatal("no child pid")
	}
}

func TestControlRestartDuringBuildRequiresNextBuild(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	starts := filepath.Join(dir, "starts")
	artifact := filepath.Join(dir, "artifact")
	launches := filepath.Join(dir, "launches")
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; echo "$n" >> %s; while [ ! -e %s.$n ]; do /bin/sleep 0.01; done; echo "$n" > %s`, runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(starts), runnertest.ShellQuote(filepath.Join(dir, "gate")), runnertest.ShellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, runnertest.ShellQuote(artifact), runnertest.ShellQuote(launches), runnertest.ShellQuote(launches))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
	testwait.Until(t, "initial build start", func() bool { return strings.TrimSpace(readFile(starts)) == "1" }, testwait.Within(2*time.Second))
	responseCh := make(chan struct {
		response control.Response
		err      error
	}, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		responseCh <- struct {
			response control.Response
			err      error
		}{response, err}
	}()
	// Wait until the control request has been accepted by the socket layer while
	// the build remains gated, then let that build finish.
	testwait.Until(t, "control request accepted", func() bool {
		status, err := r.status()
		return err == nil && status.RestartPending
	}, testwait.Within(time.Second))

	if err := os.WriteFile(filepath.Join(dir, "gate.1"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "required second build start", func() bool { return strings.Contains(readFile(starts), "2\n") }, testwait.Within(2*time.Second))
	if got := readFile(launches); got != "" {
		t.Fatalf("first build artifact launched before required rebuild: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.2"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	result := testwait.Recv(t, "restart response timed out", responseCh, testwait.Within(4*time.Second))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !result.response.OK || result.response.Generation != 1 {
		t.Fatalf("restart response = %+v", result.response)
	}
	testwait.Until(t, "new run to record artifact", func() bool { return strings.TrimSpace(readFile(launches)) != "" }, testwait.Within(time.Second))
	if got := strings.TrimSpace(readFile(launches)); got != "2" {
		t.Fatalf("launched artifact sequence = %q, want only generation 2", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestControlDisconnectBeforeBuildCommitDropsRequest(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	starts := filepath.Join(dir, "starts")
	artifact := filepath.Join(dir, "artifact")
	launches := filepath.Join(dir, "launches")
	gateBase := filepath.Join(dir, "gate")
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; echo "$n" >> %s; while [ ! -e %s.$n ]; do /bin/sleep 0.01; done; echo "$n" > %s`, runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(starts), runnertest.ShellQuote(gateBase), runnertest.ShellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, runnertest.ShellQuote(artifact), runnertest.ShellQuote(launches), runnertest.ShellQuote(launches))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
	testwait.Until(t, "initial build start", func() bool { return strings.TrimSpace(readFile(starts)) == "1" }, testwait.Within(2*time.Second))
	conn, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(struct {
		Command string `json:"command"`
	}{Command: "restart"}); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "pre-commit request accepted", func() bool { status, err := r.status(); return err == nil && status.RestartPending }, testwait.Within(time.Second))
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "pre-commit disconnect observed", func() bool { status, err := r.status(); return err == nil && !status.RestartPending }, testwait.Within(time.Second))
	if err := os.WriteFile(gateBase+".1", nil, 0600); err != nil {
		t.Fatal(err)
	}
	status := r.waitStatus(t, string(Running))
	testwait.Until(t, "current build launch", func() bool { return strings.TrimSpace(readFile(launches)) == "1" }, testwait.Within(time.Second))
	if status.Generation != 1 || strings.Contains(readFile(starts), "2\n") {
		t.Fatalf("pre-commit disconnect caused rebuild: status=%+v builds=%q", status, readFile(starts))
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestControlDisconnectAfterCommitContinuesRestart(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	release := filepath.Join(dir, "release")
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s`, runnertest.ShellQuote(count), runnertest.ShellQuote(count), runnertest.ShellQuote(count))
	// 子は release が置かれるまで TERM で終わらない。終わると Stopping+RestartPending の窓が一瞬で閉じ、負荷が高いと
	// ポーリングがその窓を取り逃がして落ちていた。窓を開けたまま切断し、切断の後で release して再起動の続きを見る
	run := fmt.Sprintf(`trap 'while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0' TERM; while :; do /bin/sleep 0.01; done`, runnertest.ShellQuote(release))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD":      build,
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "30s",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.Generation != 1 || initial.PID == nil {
		t.Fatalf("initial status = %+v", initial)
	}
	conn, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(struct {
		Command string `json:"command"`
	}{Command: "restart"}); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "restart commit", func() bool {
		status, err := r.status()
		return err == nil && status.State == string(Stopping) && status.RestartPending
	}, testwait.Within(10*time.Second))

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "committed restart despite disconnect", func() bool {
		status, err := r.status()
		return err == nil && status.State == string(Running) && status.Generation == 2 && status.PID != nil
	}, testwait.Within(10*time.Second))

	if got := strings.TrimSpace(readFile(count)); got != "2" {
		t.Fatalf("build count after committed disconnect = %q", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestChildExitWinsOverLaterControlRestart(t *testing.T) {
	r := startTestRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 0"}, false)
	testwait.Recv(t, "runner did not exit after child: status", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		status, err := r.status()
		return fmt.Sprintf("runner did not exit after child: status=%+v statusErr=%v stderr=%s", status, err, readFile(r.stderr))
	}))
	err := r.waitErr
	if err != nil {
		t.Fatalf("natural runner exit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err == nil && response.OK {
		t.Fatalf("restart succeeded after child exit: %+v", response)
	}
}

func TestFinishedMarkerBeforeConfirmationKeyDoesNotRestart(t *testing.T) {
	child := &process{pid: 987654, done: make(chan struct{}), outputEnd: make(chan struct{})}
	child.result = processResult{Code: 0}
	child.finished.Store(true)
	close(child.done)
	a := &actor{
		model: Model{State: Running, Confirm: ConfirmRestart, PID: child.pid, Generation: 1},
		child: child, events: make(chan actorEvent, 4), presenter: headlessPresenter{},
		sink: newLogSink(io.Discard, true),
	}

	a.handleKey("y")

	if !a.finished || a.model.State != Exiting || a.model.Generation != 1 || len(a.allProcesses) != 0 {
		t.Fatalf("confirmation after child exit started a restart: finished=%v model=%+v processes=%d", a.finished, a.model, len(a.allProcesses))
	}
}

func TestStatusConsumesFinishedMarkerBeforeResponding(t *testing.T) {
	dir := runnertest.ShortTempDir(t, "rsts-")
	server, err := control.Listen(filepath.Join(dir, "runner.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	child := &process{pid: 987654, done: make(chan struct{}), outputEnd: make(chan struct{})}
	child.result = processResult{Code: 0}
	child.finished.Store(true)
	close(child.done)
	a := &actor{
		model: Model{State: Running, PID: child.pid, Generation: 1}, child: child,
		server: server, events: make(chan actorEvent, 4), presenter: headlessPresenter{},
		sink: newLogSink(io.Discard, true),
	}

	type statusResult struct {
		response control.Response
		err      error
	}
	got := make(chan statusResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		response, err := control.Call(ctx, server.Path(), control.Status)
		got <- statusResult{response: response, err: err}
	}()
	a.handleControl(<-server.Requests())
	result := <-got
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.response.State != string(Exiting) || result.response.PID != nil || !a.finished {
		t.Fatalf("status after finished marker = %+v; actor finished=%v model=%+v", result.response, a.finished, a.model)
	}
}

func TestDrainOutputsDoesNotKillNaturalExitDescendantHoldingPipe(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "descendant.pid")
	gate := filepath.Join(dir, "gate")
	marker := filepath.Join(dir, "descendant-alive")
	if err := syscall.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("(read value < %s; echo alive > %s; exec /bin/sleep 30) & echo $! > %s; exit 0", runnertest.ShellQuote(gate), runnertest.ShellQuote(marker), runnertest.ShellQuote(pidFile))
	proc, err := startProcess([]string{"/bin/sh", "-c", command}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	<-proc.done
	testwait.Until(t, "descendant PID", func() bool { return strings.TrimSpace(readFile(pidFile)) != "" }, testwait.Within(10*time.Second))
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err != nil {
		t.Fatal(err)
	}
	var gateWriter *os.File
	t.Cleanup(func() {
		if gateWriter != nil {
			_ = gateWriter.Close()
		}
		_ = signalGroup(proc.pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	})
	var stderr bytes.Buffer
	a := &actor{
		cfg: Config{Stderr: &stderr}, allProcesses: []*process{proc},
		outputDrainTimeout: 0, sink: newLogSink(io.Discard, true),
	}

	a.drainOutputs()

	testwait.Until(t, "natural-exit descendant to wait at the named pipe", func() bool {
		file, err := os.OpenFile(gate, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return false
		}
		gateWriter = file
		return true
	}, testwait.Within(time.Second))

	if _, err := gateWriter.WriteString("continue\n"); err != nil {
		t.Fatal(err)
	}
	_ = gateWriter.Close()
	testwait.Until(t, "natural-exit descendant to continue after output drain", func() bool { return strings.TrimSpace(readFile(marker)) == "alive" }, testwait.Within(time.Second))
	if !strings.Contains(stderr.String(), fmt.Sprintf("子孫が出力を握ったまま残っている (pgid %d)", proc.pid)) {
		t.Fatalf("missing descendant output warning: %q", stderr.String())
	}
}

func TestFinishedMarkerRejectsRestartBeforeExitEventIsConsumed(t *testing.T) {
	dir := runnertest.ShortTempDir(t, "rfin-")
	server, err := control.Listen(filepath.Join(dir, "runner.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	child := &process{pid: 987654, done: make(chan struct{}), outputEnd: make(chan struct{})}
	child.result = processResult{Code: 0}
	child.finished.Store(true)
	close(child.done)
	actor := &actor{
		cfg: Config{TermGrace: 0}, model: Model{State: Running, PID: child.pid, Generation: 1},
		child: child, server: server, events: make(chan actorEvent, 2), presenter: headlessPresenter{},
	}
	resultCh := make(chan struct {
		response control.Response
		err      error
	}, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		response, err := control.Call(ctx, server.Path(), control.Restart)
		resultCh <- struct {
			response control.Response
			err      error
		}{response, err}
	}()
	request := <-server.Requests()
	actor.handleControl(request)
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.response.OK || result.response.Reason != "child has exited" {
		t.Fatalf("response after finished marker = %+v", result.response)
	}
	if actor.model.State != Exiting || !actor.finished {
		t.Fatalf("actor did not process natural child exit first: model=%+v finished=%v", actor.model, actor.finished)
	}
}

func TestOutputBufferRetainsHeadAndTailWithDroppedCount(t *testing.T) {
	buffer := NewOutputBuffer(3, 100)
	for _, line := range []string{"first", "two", "three", "four", "last"} {
		buffer.AddLine(line)
	}
	got := buffer.Drain()
	want := []string{"first", omissionLine(2), "four", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Drain() = %q, want %q", got, want)
	}
}

func TestLargeTTYLogRetainsFirstAndLastAndReportsDrops(t *testing.T) {
	var output bytes.Buffer
	sink := newLogSink(&output, false)
	var input strings.Builder
	input.WriteString("head\n")
	for n := range 2100 {
		fmt.Fprintf(&input, "middle-%d\n", n)
	}
	input.WriteString("tail\n")
	sink.CopyFrom(strings.NewReader(input.String()))
	sink.Flush()
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if lines[0] != "head" || lines[len(lines)-1] != "tail" {
		t.Fatalf("head/tail = %q / %q", lines[0], lines[len(lines)-1])
	}
	if len(lines) != defaultOutputLines+1 {
		t.Fatalf("printed lines = %d, want %d", len(lines), defaultOutputLines+1)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "restartable: 102 行") {
		t.Fatalf("missing exact dropped count: %q", lines[1])
	}
}

func TestChildStdoutStderrOrderIsPreserved(t *testing.T) {
	run := `printf 'out-1\n'; printf 'err-1\n' >&2; printf 'out-2\n'; printf 'err-2\n' >&2; exit 0`
	r := startTestRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": run}, false)
	testwait.Recv(t, "runner did not exit: status", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		status, err := r.status()
		return fmt.Sprintf("runner did not exit: status=%+v statusErr=%v stderr=%s", status, err, readFile(r.stderr))
	}))
	err := r.waitErr
	if err != nil {
		t.Fatalf("runner exit: %v; stderr: %s", err, readFile(r.stderr))
	}
	file, err := os.Open(r.stdout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	want := []string{"out-1", "err-1", "out-2", "err-2"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("combined stream order = %q, want %q", lines, want)
	}
}

func TestBuildFailedControlRestartStartsOneBuild(t *testing.T) {
	dir := runnertest.ShortTempDir(t, "rbuild-")
	server, err := control.Listen(filepath.Join(dir, "runner.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	a := &actor{
		cfg:   Config{BuildCommand: "exec /bin/sleep 30", Stdin: strings.NewReader(""), Stderr: io.Discard},
		model: Model{State: BuildFailed}, server: server,
		sink: newLogSink(io.Discard, true), presenter: headlessPresenter{}, events: make(chan actorEvent, 8),
	}
	conn, err := net.Dial("unix", server.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(struct {
		Command string `json:"command"`
	}{Command: "restart"}); err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	request := <-server.Requests()
	a.handleControl(request)
	if got := len(a.allProcesses); got != 1 {
		_ = conn.Close()
		for _, proc := range a.allProcesses {
			_ = signalGroup(proc.pid, syscall.SIGKILL)
		}
		for _, proc := range a.allProcesses {
			<-proc.done
		}
		t.Fatalf("one control restart launched %d builds, want 1", got)
	}
	_ = conn.Close()
	for _, proc := range a.allProcesses {
		_ = signalGroup(proc.pid, syscall.SIGKILL)
	}
	for _, proc := range a.allProcesses {
		<-proc.done
	}
}

func TestStartBuildRefusesWhenBuildAlreadyRunning(t *testing.T) {
	var stderr bytes.Buffer
	a := &actor{
		cfg:   Config{BuildCommand: "exec /bin/sleep 30", Stdin: strings.NewReader(""), Stderr: &stderr},
		model: Model{State: Building}, sink: newLogSink(io.Discard, true), presenter: headlessPresenter{}, events: make(chan actorEvent, 8),
	}
	if err := a.startBuild(); err != nil {
		t.Fatal(err)
	}
	first := a.build
	if err := a.startBuild(); err != nil {
		t.Fatal(err)
	}
	if a.build != first || len(a.allProcesses) != 1 {
		for _, proc := range a.allProcesses {
			_ = signalGroup(proc.pid, syscall.SIGKILL)
		}
		for _, proc := range a.allProcesses {
			<-proc.done
		}
		t.Fatalf("second build replaced the active build: active=%p first=%p count=%d", a.build, first, len(a.allProcesses))
	}
	if !strings.Contains(stderr.String(), "build start refused") {
		_ = signalGroup(first.pid, syscall.SIGKILL)
		<-first.done
		t.Fatalf("refused build attempt was not logged: %q", stderr.String())
	}
	_ = signalGroup(first.pid, syscall.SIGKILL)
	<-first.done
}

func TestProcessDoneDeliversSameResultToMultipleWaiters(t *testing.T) {
	proc, err := startProcess([]string{"/bin/sh", "-c", "exit 23"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	first := proc.wait()
	second := proc.wait()
	if first.Code != 23 || second.Code != 23 {
		t.Fatalf("process done results = %+v and %+v, want code 23 for both", first, second)
	}
}

func TestChildExitCannotFinishDuringForcedShutdown(t *testing.T) {
	child := &process{pid: 987654, done: make(chan struct{}), outputEnd: make(chan struct{})}
	child.result = processResult{Code: 143}
	child.finished.Store(true)
	close(child.done)
	a := &actor{
		model: Model{State: Exiting, PID: child.pid, ExitCode: 143},
		child: child, childProcessed: true, forceRunning: true, presenter: headlessPresenter{},
	}
	a.completeChildExit()
	if a.finished || a.model.State != Exiting {
		t.Fatalf("child exit finished the runner before forced cleanup: finished=%v model=%+v", a.finished, a.model)
	}
}

func TestFinishIsBlockedDuringForcedShutdown(t *testing.T) {
	child := &process{pid: 987654, done: make(chan struct{}), outputEnd: make(chan struct{})}
	child.result = processResult{Code: 143}
	child.finished.Store(true)
	close(child.done)
	a := &actor{model: Model{State: Exiting, PID: child.pid}, child: child, forceRunning: true}
	a.finish(143)
	if a.finished || a.retCode != 0 {
		t.Fatalf("finish crossed forced shutdown barrier: finished=%v retCode=%d", a.finished, a.retCode)
	}
}

func TestForceStopWithNoProcessesDoesNotSendToActorEventsSynchronously(t *testing.T) {
	a := &actor{
		model:  Model{State: Exiting, Intent: IntentExit},
		events: make(chan actorEvent), presenter: headlessPresenter{},
	}
	done := make(chan struct{})
	go func() { a.beginForceStop(); close(done) }()
	testwait.Recv(t, "beginForceStop deadlocked sending to its own unbuffered event channel", done, testwait.Within(time.Second))
}

func TestForcedShutdownKillsStopCommandGroupAfterChildExits(t *testing.T) {
	dir := t.TempDir()
	stopPIDFile := filepath.Join(dir, "stop.pid")
	stop := fmt.Sprintf(`trap '' TERM; echo $$ > %s; while :; do /bin/sleep 1; done`, runnertest.ShellQuote(stopPIDFile))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP":       stop,
		"RESTARTABLE_TEST_TERM_GRACE": "150ms",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no child PID")
	}
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := control.Call(ctx, r.path, control.Restart)
		result <- err
	}()
	testwait.Until(t, "stop command to start", func() bool { return strings.TrimSpace(readFile(stopPIDFile)) != "" }, testwait.Within(time.Second))
	stopPID, err := strconv.Atoi(strings.TrimSpace(readFile(stopPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = signalGroup(stopPID, syscall.SIGKILL) })
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	_ = testwait.Recv(t, "control call did not finish during forced shutdown", result, testwait.Within(time.Second))
	testwait.Until(t, "stop command group cleanup", func() bool { return !groupExists(stopPID) }, testwait.Within(time.Second))
}

func TestForcedShutdownIncludesGroupsWhoseLeadersWereReaped(t *testing.T) {
	dir := t.TempDir()
	stopPIDFile := filepath.Join(dir, "stop-group.pid")
	stop := fmt.Sprintf(`echo $$ > %s; (trap '' TERM; while :; do /bin/sleep 1; done) >/dev/null 2>&1 & exit 0`, runnertest.ShellQuote(stopPIDFile))
	keyDir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(dir, "presenter.json")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
		"RESTARTABLE_TEST_STOP":            stop,
		"RESTARTABLE_TEST_TERM_GRACE":      "150ms",
		"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
	})
	r.waitStatus(t, string(Running))
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, err := control.Call(ctx, r.path, control.Restart)
		result <- err
	}()
	waitPresenterSnapshot(t, statePath, "successful stop command", func(got presenterSnapshot) bool {
		return got.Model.State == Stopping && got.Model.StopAccepted
	})
	testwait.Until(t, "stop group leader PID", func() bool { return strings.TrimSpace(readFile(stopPIDFile)) != "" }, testwait.Within(time.Second))
	stopPID, err := strconv.Atoi(strings.TrimSpace(readFile(stopPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = signalGroup(stopPID, syscall.SIGKILL) })
	if !groupExists(stopPID) {
		t.Fatal("stop command descendant group exited before forced shutdown")
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	_ = testwait.Recv(t, "pending restart call did not finish during forced shutdown", result, testwait.Within(time.Second))
	testwait.Until(t, "reaped stop command group cleanup", func() bool { return !groupExists(stopPID) }, testwait.Within(time.Second))
}

func TestForcedShutdownKillsGrandchildAfterLeaderExits(t *testing.T) {
	grandchildFile := filepath.Join(t.TempDir(), "grandchild.pid")
	run := fmt.Sprintf(`(trap '' TERM; while :; do /bin/sleep 1; done) & echo $! > %s; wait`, runnertest.ShellQuote(grandchildFile))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "100ms",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil {
		t.Fatal("runner has no child PID")
	}
	testwait.Until(t, "grandchild PID", func() bool { return strings.TrimSpace(readFile(grandchildFile)) != "" }, testwait.Within(time.Second))
	t.Cleanup(func() { _ = signalGroup(*status.PID, syscall.SIGKILL) })
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	testwait.Until(t, "grandchild group cleanup", func() bool { return !groupExists(*status.PID) }, testwait.Within(time.Second))
}

func TestGroupStopWaitsForDescendantAfterLeaderExit(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "descendant-ready")
	leaderExited := filepath.Join(dir, "leader-exited")
	release := filepath.Join(dir, "release-descendant")
	gracefulExit := filepath.Join(dir, "descendant-exited-gracefully")
	childPIDPath := filepath.Join(dir, "descendant.pid")
	childArmed := filepath.Join(dir, "descendant-armed")
	// 🚨 合図 (pid と armed) は trap を張った**後**に書く。先に書くと、合図を見て送った SIGTERM が trap の前に届き、
	// 既定の動作で死んで leader-exited / ready を書かず、待ちが 10 秒で切れる (負荷の中で隙間が広がって落ちていた。
	// 親の trap の前に sleep 1 を挟むと毎回同じ文言で落ちることを確かめた。2026-10-15)
	run := fmt.Sprintf(`trap 'while [ ! -e %s ]; do /bin/sleep 0.01; done; echo exited > %s; exit 0' TERM; (trap 'echo ready > %s; while [ ! -e %s ]; do /bin/sleep 0.01; done; echo graceful > %s; exit 0' TERM; echo armed > %s; while :; do /bin/sleep 0.01; done) & echo $! > %s; wait`,
		runnertest.ShellQuote(ready), runnertest.ShellQuote(leaderExited), runnertest.ShellQuote(ready), runnertest.ShellQuote(release), runnertest.ShellQuote(gracefulExit), runnertest.ShellQuote(childArmed), runnertest.ShellQuote(childPIDPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "30s",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no child PID")
	}
	testwait.Until(t, "descendant PID", func() bool { return strings.TrimSpace(readFile(childPIDPath)) != "" }, testwait.Within(10*time.Second))
	t.Cleanup(func() { _ = signalGroup(*initial.PID, syscall.SIGKILL) })
	testwait.Until(t, "descendant to arm its TERM trap", func() bool { return strings.TrimSpace(readFile(childArmed)) != "" }, testwait.Within(10*time.Second))
	type restartResult struct {
		response control.Response
		err      error
	}
	restartDone := make(chan restartResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		restartDone <- restartResult{response: response, err: err}
	}()
	testwait.Until(t, "descendant to receive SIGTERM", func() bool { return strings.TrimSpace(readFile(ready)) != "" }, testwait.Within(10*time.Second))
	testwait.Until(t, "group leader to finish before its descendant", func() bool {
		return errors.Is(syscall.Kill(*initial.PID, 0), syscall.ESRCH) && strings.TrimSpace(readFile(leaderExited)) != ""
	}, testwait.Within(10*time.Second))

	if !groupExists(*initial.PID) {
		t.Fatal("process group disappeared while its descendant was still handling SIGTERM")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "descendant to complete graceful termination", func() bool { return strings.TrimSpace(readFile(gracefulExit)) != "" }, testwait.Within(2*time.Second))
	result := testwait.Recv(t, "restart did not finish after the process group exited", restartDone, testwait.Within(3*time.Second))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !result.response.OK || result.response.Generation != 2 {
		t.Fatalf("restart response = %+v", result.response)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestQuitConfirmationDuringBuildStopsBuildAndExits(t *testing.T) {
	dir := t.TempDir()
	keyDir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(dir, "presenter.json")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD":           "exec /bin/sleep 30",
		"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
		"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
	})
	waitPresenterSnapshot(t, statePath, "initial building state", func(got presenterSnapshot) bool { return got.Model.State == Building })
	busy := sendKeyAndWaitRender(t, keyPath, statePath, "R")
	if !busy.Model.Transition.Active || !busy.Model.Transition.Busy || busy.Model.Confirm != ConfirmNone || busy.Model.Message != "処理中" {
		t.Fatalf("R during build did not show busy: %+v", busy.Model)
	}
	confirm := sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	if confirm.Model.State != Building || confirm.Model.Confirm != ConfirmQuit || !confirm.Model.Transition.Active {
		t.Fatalf("Q during build did not open confirmation: %+v", confirm.Model)
	}
	sendIntegrationKey(t, keyPath, "R")
	busy = waitPresenterSnapshot(t, statePath, "busy R while quit confirmation is open", func(got presenterSnapshot) bool {
		return got.Model.Confirm == ConfirmQuit && got.Model.Transition.Busy
	})
	if busy.Model.Message != "処理中" {
		t.Fatalf("R during confirmation should show busy and keep the dialog: %+v", busy.Model)
	}
	cancelled := sendKeyAndWaitRender(t, keyPath, statePath, "n")
	if cancelled.Model.Confirm != ConfirmNone || !cancelled.Model.Transition.Active || cancelled.Model.Transition.Busy {
		t.Fatalf("n did not dismiss the quit confirmation: %+v", cancelled.Model)
	}
	confirm = sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	if confirm.Model.Confirm != ConfirmQuit {
		t.Fatalf("Q did not reopen the quit confirmation: %+v", confirm.Model)
	}
	// Transition.Active は見ない: 終了の最後の描画 (actor.go の finish) で false に戻り、状態のファイルには最後の描画しか残らない。
	// 負荷で 2 回の描画が続けて起きると、途中の描画 (Active) を読めない (make test の全体実行でだけ落ちた)
	quitting := sendKeyAndWaitRender(t, keyPath, statePath, "y")
	if quitting.Model.State != Exiting || quitting.Model.ExitCode != 0 || quitting.Model.Transition.Kind != TransitionQuit {
		t.Fatalf("confirmed Q during build did not enter graceful quit: %+v", quitting.Model)
	}
	testwait.Recv(t, "runner did not exit after confirmed build quit", r.done, testwait.Within(4*time.Second))
	if r.waitErr != nil {
		t.Fatalf("runner exit = %v, want code 0", r.waitErr)
	}
}

func TestQuitConfirmationDuringReadinessUsesStopCommand(t *testing.T) {
	dir := t.TempDir()
	keyDir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(t.TempDir(), "presenter.json")
	childPIDPath := filepath.Join(dir, "child.pid")
	stopMarkerPath := filepath.Join(dir, "stop-called")
	stopReleasePath := filepath.Join(dir, "stop-release")
	if err := syscall.Mkfifo(stopReleasePath, 0600); err != nil {
		t.Fatal(err)
	}
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   fmt.Sprintf("printf '%%s' \"$$\" > %s; exec /bin/sleep 30", runnertest.ShellQuote(childPIDPath)),
		"RESTARTABLE_TEST_READY":                 "exec /bin/sleep 30",
		"RESTARTABLE_TEST_READY_TIMEOUT":         "30s",
		"RESTARTABLE_TEST_READY_INTERVAL":        "5ms",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "30s",
		"RESTARTABLE_TEST_STOP_TIMEOUT":          "30s",
		"RESTARTABLE_TEST_STOP":                  fmt.Sprintf("printf started > %s; read release < %s; kill -TERM \"$(cat %s)\"; printf done > %s", runnertest.ShellQuote(stopMarkerPath), runnertest.ShellQuote(stopReleasePath), runnertest.ShellQuote(childPIDPath), runnertest.ShellQuote(stopMarkerPath)),
		"RESTARTABLE_TEST_KEY_SOCKET":            keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE":       statePath,
	})

	waitPresenterSnapshot(t, statePath, "child in readiness stage", func(got presenterSnapshot) bool {
		return got.Model.State == Running && got.Model.Transition.Active && got.Model.Transition.Stage == TransitionReady
	})
	confirm := sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	if confirm.Model.Confirm != ConfirmQuit || !confirm.Model.Transition.Active || confirm.Model.Transition.Stage != TransitionReady {
		t.Fatalf("Q during readiness did not open confirmation: %+v", confirm.Model)
	}
	sendKeyAndWaitRender(t, keyPath, statePath, "y")
	stopping := waitPresenterSnapshot(t, statePath, "confirmed quit waiting in stop-cmd", func(got presenterSnapshot) bool {
		return got.Model.State == Stopping && got.Model.Intent == IntentExit && got.Model.Transition.Kind == TransitionQuit && got.Model.Transition.Stage == TransitionStop
	})
	if stopping.Model.State != Stopping || stopping.Model.Intent != IntentExit || stopping.Model.Transition.Kind != TransitionQuit || stopping.Model.Transition.Stage != TransitionStop {
		t.Fatalf("confirmed quit during readiness did not enter the stop path: %+v", stopping.Model)
	}
	testwait.Until(t, "stop-cmd to run for a quit during readiness", func() bool {
		return readFile(stopMarkerPath) == "started"
	}, testwait.Within(3*time.Second))

	busy := sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	if busy.Model.State != Stopping || !busy.Model.Transition.Busy || busy.Model.Message != "処理中" {
		t.Fatalf("Q during stop-cmd should be ignored as busy: %+v", busy.Model)
	}
	stopRelease, err := os.OpenFile(stopReleasePath, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stopRelease, "continue\n"); err != nil {
		_ = stopRelease.Close()
		t.Fatal(err)
	}
	if err := stopRelease.Close(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "stop-cmd to stop the running child", func() bool {
		return readFile(stopMarkerPath) == "done"
	}, testwait.Within(3*time.Second))

	testwait.Recv(t, "runner did not exit after stop-cmd stopped the child", r.done, testwait.Within(4*time.Second))
	if r.waitErr != nil {
		t.Fatalf("runner exit = %v, want code 0", r.waitErr)
	}
}

func TestQuitStopCancellationResumesUnconfirmedReadyCheck(t *testing.T) {
	dir := t.TempDir()
	keyDir := runnertest.ShortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(dir, "presenter.json")
	attemptPath := filepath.Join(dir, "ready-attempts")
	attemptResultPath := filepath.Join(dir, "ready-results")
	firstReadyGate := filepath.Join(dir, "ready-first")
	secondReadyGate := filepath.Join(dir, "ready-second")
	for _, path := range []string{firstReadyGate, secondReadyGate} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ready := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; if [ "$n" -eq 1 ]; then gate=%s; else gate=%s; fi; read result < "$gate"; echo "$n:$result" >> %s; [ "$result" = ready ]`,
		runnertest.ShellQuote(attemptPath), runnertest.ShellQuote(attemptPath), runnertest.ShellQuote(attemptPath), runnertest.ShellQuote(firstReadyGate), runnertest.ShellQuote(secondReadyGate), runnertest.ShellQuote(attemptResultPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                 "exec /bin/sleep 30",
		"RESTARTABLE_TEST_READY":               ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":       "30s",
		"RESTARTABLE_TEST_READY_INTERVAL":      "5ms",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE": "30ms",
		"RESTARTABLE_TEST_STOP":                "exit 0",
		"RESTARTABLE_TEST_KEY_SOCKET":          keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE":     statePath,
	})
	testwait.Until(t, "first readiness command to block on its gate", func() bool {
		return strings.TrimSpace(readFile(attemptPath)) == "1"
	}, testwait.Within(3*time.Second))

	readyStatus := r.waitStatus(t, string(Running))
	if readyStatus.Ready {
		t.Fatalf("status reported ready before the gated command succeeded: %+v", readyStatus)
	}
	confirm := sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	if confirm.Model.Confirm != ConfirmQuit || confirm.Model.Transition.Stage != TransitionReady {
		t.Fatalf("Q did not open the readiness-stage quit confirmation: %+v", confirm.Model)
	}
	sendKeyAndWaitRender(t, keyPath, statePath, "y")
	waitPresenterSnapshot(t, statePath, "accepted stop command", func(got presenterSnapshot) bool {
		return got.Model.State == Stopping && got.Model.StopAccepted
	})
	cancelled := sendKeyAndWaitRender(t, keyPath, statePath, "esc")
	if cancelled.Model.State != Running || cancelled.Model.Ready {
		t.Fatalf("Esc did not return to running with readiness unconfirmed: %+v", cancelled.Model)
	}
	testwait.Until(t, "readiness command to restart after Esc", func() bool {
		return strings.TrimSpace(readFile(attemptPath)) == "2"
	}, testwait.Within(3*time.Second))

	readyGate, err := os.OpenFile(secondReadyGate, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(readyGate, "ready\n"); err != nil {
		_ = readyGate.Close()
		t.Fatal(err)
	}
	if err := readyGate.Close(); err != nil {
		t.Fatal(err)
	}
	readyDeadline := time.NewTimer(3 * time.Second)
	readyPoll := time.NewTicker(5 * time.Millisecond)
	readyConfirmed := false
	var lastStatus control.Response
	var lastStatusErr error
	for !readyConfirmed {
		lastStatus, lastStatusErr = r.status()
		readyConfirmed = lastStatusErr == nil && lastStatus.State == string(Running) && lastStatus.Ready
		if readyConfirmed {
			break
		}
		select {
		case <-readyPoll.C:
		case <-readyDeadline.C:
			readyPoll.Stop()
			t.Fatalf("status.ready remained false after resumed readiness command: status=%+v err=%v attempts=%q results=%q snapshot=%+v stderr=%q",
				lastStatus, lastStatusErr, readFile(attemptPath), readFile(attemptResultPath), func() presenterSnapshot { snapshot, _ := readPresenterSnapshot(statePath); return snapshot }(), readFile(r.stderr))
		}
	}
	readyPoll.Stop()
	_ = readyDeadline.Stop()
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestReadyCleanupQueuesCancellationWhilePreviousCleanupRuns(t *testing.T) {
	dir := t.TempDir()
	cleanupStarted := filepath.Join(dir, "cleanup-started")
	descendantStarted := filepath.Join(dir, "descendant-started")
	cleanupRelease := filepath.Join(dir, "cleanup-release")
	if err := syscall.Mkfifo(cleanupRelease, 0600); err != nil {
		t.Fatal(err)
	}
	oldCommand := fmt.Sprintf(`( trap 'printf term > %s; read release < %s; exit 0' TERM; printf ready > %s; while :; do :; done ) & while [ ! -e %s ]; do :; done; exit 0`,
		runnertest.ShellQuote(cleanupStarted), runnertest.ShellQuote(cleanupRelease), runnertest.ShellQuote(descendantStarted), runnertest.ShellQuote(descendantStarted))
	sink := newLogSink(io.Discard, true)
	oldProc, err := startProcess([]string{oldCommand}, true, os.Environ(), strings.NewReader(""), sink, false)
	if err != nil {
		t.Fatal(err)
	}
	if result := oldProc.wait(); result.Code != 0 {
		t.Fatalf("old ready command exit = %d, want 0", result.Code)
	}
	newProc, err := startProcess([]string{"/bin/sleep", "30"}, false, os.Environ(), strings.NewReader(""), sink, false)
	if err != nil {
		t.Fatal(err)
	}
	if !groupExists(newProc.pid) {
		t.Fatal("new ready process group exited before its cancellation was queued")
	}
	t.Cleanup(func() {
		_ = signalGroup(oldProc.pid, syscall.SIGKILL)
		_ = signalGroup(newProc.pid, syscall.SIGKILL)
	})
	a := &actor{cfg: Config{ReadyCleanupGrace: 30 * time.Second}, events: make(chan actorEvent, 4)}
	a.startReadyCleanup(oldProc, 1, 1, readyCleanupCompleted)
	testwait.Until(t, "old cleanup process to enter its gated TERM handler", func() bool {
		_, err := os.Stat(cleanupStarted)
		return err == nil
	}, testwait.Within(3*time.Second))

	a.readyProbe = newProc
	a.startReadyCleanup(newProc, 2, 2, readyCleanupCancelled)
	gate, err := os.OpenFile(cleanupRelease, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(gate, "continue\n"); err != nil {
		_ = gate.Close()
		t.Fatal(err)
	}
	if err := gate.Close(); err != nil {
		t.Fatal(err)
	}
	oldDone := <-a.events
	if oldDone.kind != readyCleanupDoneEvent || oldDone.proc != oldProc {
		t.Fatalf("first cleanup event = %+v, want old process cleanup", oldDone)
	}
	a.handleReadyCleanupDone(oldDone)
	testwait.Until(t, "queued ready process group to be cleaned", func() bool {
		return !groupExists(newProc.pid)
	}, testwait.Within(3*time.Second))

	newDone := <-a.events
	if newDone.kind != readyCleanupDoneEvent || newDone.proc != newProc {
		t.Fatalf("second cleanup event = %+v, want new process cleanup", newDone)
	}
	a.handleReadyCleanupDone(newDone)
	if groupExists(oldProc.pid) || groupExists(newProc.pid) {
		t.Fatalf("ready process group survived cleanup: old=%v new=%v", groupExists(oldProc.pid), groupExists(newProc.pid))
	}
	if a.readyProbe != nil || a.readyCleaningProc != nil {
		t.Fatalf("cleanup left process pointers set: probe=%p cleaning=%p", a.readyProbe, a.readyCleaningProc)
	}
	a.finish(0)
	if !a.finished {
		t.Fatal("runner could not finish after every ready process group was cleaned")
	}
}

func TestFinalPresenterRenderClosesTransitionPanelForQuitAndCtrlC(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		code int
	}{
		{name: "quit-during-build", keys: []string{"Q", "y"}, code: 0},
		{name: "ctrl-c-during-build", keys: []string{"ctrl+c"}, code: 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			keyDir := runnertest.ShortTempDir(t, "rkey-")
			keyPath := filepath.Join(keyDir, "keys.sock")
			statePath := filepath.Join(dir, "presenter.json")
			r := startTestRunner(t, map[string]string{
				"RESTARTABLE_TEST_BUILD":           "exec /bin/sleep 30",
				"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
				"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
				"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
			})
			waitPresenterSnapshot(t, statePath, "build transition", func(got presenterSnapshot) bool {
				return got.Model.State == Building && got.Model.Transition.Active
			})
			for _, key := range tc.keys {
				sendKeyAndWaitRender(t, keyPath, statePath, key)
			}
			testwait.Recv(t, "runner did not exit after", r.done, testwait.Within(4*time.Second), testwait.Diag(func() string { return "runner did not exit after " + tc.name }))
			if tc.code == 0 && r.waitErr != nil {
				t.Fatalf("runner exit = %v, want code 0", r.waitErr)
			}
			if tc.code != 0 {
				var exit *exec.ExitError
				if !errors.As(r.waitErr, &exit) || exit.ExitCode() != tc.code {
					t.Fatalf("runner exit = %v, want code %d", r.waitErr, tc.code)
				}
			}
			last, ok := readPresenterSnapshot(statePath)
			if !ok {
				t.Fatal("presenter did not record its last frame")
			}
			if last.Model.Transition.Active {
				t.Fatalf("last rendered frame still has the transition panel: %+v", last.Model)
			}
		})
	}
}

func TestReadyCommandRetriesWithInstanceIDAndReportsReadyInStatus(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "attempts")
	runIDPath := filepath.Join(dir, "run-id")
	ready := fmt.Sprintf(`n=$(cat %s 2>/dev/null || echo 0); n=$((n+1)); printf '%%s' "$n" > %s; [ "$n" -ge 3 ] && [ "$OBAKET_DEV_LOOP" = "$(cat %s)" ]`, runnertest.ShellQuote(countPath), runnertest.ShellQuote(countPath), runnertest.ShellQuote(runIDPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":            fmt.Sprintf(`printf '%%s' "$OBAKET_DEV_LOOP" > %s; exec /bin/sleep 30`, runnertest.ShellQuote(runIDPath)),
		"RESTARTABLE_TEST_READY":          ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":  "2s",
		"RESTARTABLE_TEST_READY_INTERVAL": "5ms",
		"RESTARTABLE_TEST_ID_ENV":         "OBAKET_DEV_LOOP",
		"RESTARTABLE_TEST_TERM_GRACE":     "60ms",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.Ready {
		t.Fatalf("status reported ready before the confirmation command succeeded: %+v", initial)
	}
	var readyStatus control.Response
	testwait.Until(t, "ready command to succeed on its third attempt", func() bool {
		got, err := r.status()
		if err != nil {
			return false
		}
		readyStatus = got
		return got.Ready
	}, testwait.Within(3*time.Second))

	if readyStatus.Generation != 1 || strings.TrimSpace(readFile(countPath)) != "3" {
		t.Fatalf("ready status/attempt count = %+v / %q", readyStatus, readFile(countPath))
	}
	if got := strings.TrimSpace(readFile(r.stderr)); got != "起動を確認しました" {
		t.Fatalf("headless ready result = %q, want one confirmation line", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestReadyTimeoutKillsProbeGroupsButKeepsChildAlive(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "probe-pids")
	releaseChild := filepath.Join(dir, "release-child")
	childSurvived := filepath.Join(dir, "child-survived")
	ready := fmt.Sprintf(`echo $$ >> %s; trap '' TERM; while :; do /bin/sleep 1; done`, runnertest.ShellQuote(pidPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   fmt.Sprintf(`while [ ! -e %s ]; do /bin/sleep 0.01; done; touch %s; exec /bin/sleep 30`, runnertest.ShellQuote(releaseChild), runnertest.ShellQuote(childSurvived)),
		"RESTARTABLE_TEST_READY":                 ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":         "160ms",
		"RESTARTABLE_TEST_READY_INTERVAL":        "5ms",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "35ms",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE":   "20ms",
		"RESTARTABLE_TEST_TERM_GRACE":            "60ms",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil || status.Ready {
		t.Fatalf("initial readiness status = %+v", status)
	}
	testwait.Until(t, "ready timeout result", func() bool {
		return strings.Contains(readFile(r.stderr), "起動を確認できませんでした")
	}, testwait.Within(3*time.Second))

	status, err := r.status()
	if err != nil || status.State != string(Running) || status.Ready || status.PID == nil || *status.PID == 0 {
		t.Fatalf("readiness failure changed child status: %+v, err=%v", status, err)
	}
	if err := syscall.Kill(*status.PID, 0); err != nil {
		t.Fatalf("readiness failure killed the application child: %v", err)
	}
	if err := os.WriteFile(releaseChild, nil, 0600); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "application continues after readiness timeout", func() bool {
		_, err := os.Stat(childSurvived)
		return err == nil
	}, testwait.Within(2*time.Second))

	if got := strings.Count(readFile(r.stderr), "起動を確認できませんでした"); got != 1 {
		t.Fatalf("readiness timeout result count = %d, stderr=%q", got, readFile(r.stderr))
	}
	lines := strings.Fields(readFile(pidPath))
	if len(lines) < 2 {
		t.Fatalf("attempt timeout did not run repeated probes: %q", lines)
	}
	for _, line := range lines {
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("invalid probe process id %q: %v", line, err)
		}
		testwait.Until(t, fmt.Sprintf("ready probe process group %d cleanup", pid), func() bool {
			return !groupExists(pid)
		}, testwait.Within(2*time.Second))

	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestChildCanExitBeforeReadyProbeAndProbeGroupIsReaped(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "probe-started")
	pidPath := filepath.Join(dir, "probe-pid")
	run := fmt.Sprintf(`while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0`, runnertest.ShellQuote(started))
	ready := fmt.Sprintf(`echo $$ > %s; touch %s; trap '' TERM; while :; do /bin/sleep 1; done`, runnertest.ShellQuote(pidPath), runnertest.ShellQuote(started))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   run,
		"RESTARTABLE_TEST_READY":                 ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":         "5s",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "2s",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE":   "20ms",
	})
	testwait.Recv(t, "runner did not exit after its child; stderr", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		return "runner did not exit after its child; stderr=" + readFile(r.stderr)
	}))
	if r.waitErr != nil {
		t.Fatalf("runner exit after child finished first: %v; stderr=%s", r.waitErr, readFile(r.stderr))
	}
	if got := strings.TrimSpace(readFile(r.stderr)); got != "起動を確認できませんでした" {
		t.Fatalf("headless child-exit-before-ready result = %q", got)
	}
	pidText := strings.TrimSpace(readFile(pidPath))
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("ready probe pid = %q: %v", pidText, err)
	}
	testwait.Until(t, "ready probe group cleanup after child exit", func() bool { return !groupExists(pid) }, testwait.Within(2*time.Second))
}

func TestControlRestartDuringReadyDiscardsOldGenerationResult(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "attempts")
	firstGate := filepath.Join(dir, "first-gate")
	secondGate := filepath.Join(dir, "second-gate")
	ready := fmt.Sprintf(`n=$(cat %s 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > %s; if [ "$n" -eq 1 ]; then trap '' TERM; gate=%s; else gate=%s; fi; while [ ! -e "$gate" ]; do /bin/sleep 0.01; done; exit 0`, runnertest.ShellQuote(countPath), runnertest.ShellQuote(countPath), runnertest.ShellQuote(firstGate), runnertest.ShellQuote(secondGate))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   "exec /bin/sleep 30",
		"RESTARTABLE_TEST_READY":                 ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":         "5s",
		"RESTARTABLE_TEST_READY_INTERVAL":        "5ms",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "4s",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE":   "20ms",
		"RESTARTABLE_TEST_TERM_GRACE":            "60ms",
	})
	testwait.Until(t, "first generation readiness probe", func() bool {
		return strings.TrimSpace(readFile(countPath)) == "1"
	}, testwait.Within(3*time.Second))

	first, err := r.status()
	if err != nil || first.Generation != 1 || first.Ready {
		t.Fatalf("first generation status = %+v, err=%v", first, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	response, err := control.Call(ctx, r.path, control.Restart)
	cancel()
	if err != nil || !response.OK || response.Generation != 2 || response.Ready {
		t.Fatalf("control restart response = %+v, err=%v", response, err)
	}
	testwait.Until(t, "second generation readiness probe", func() bool {
		return strings.TrimSpace(readFile(countPath)) == "2"
	}, testwait.Within(3*time.Second))

	if err := os.WriteFile(firstGate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := r.status()
	if err != nil || second.Generation != 2 || second.Ready {
		t.Fatalf("old generation result changed new readiness: %+v, err=%v", second, err)
	}
	if err := os.WriteFile(secondGate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "second generation readiness", func() bool {
		got, err := r.status()
		return err == nil && got.Generation == 2 && got.Ready
	}, testwait.Within(3*time.Second))

	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestRunnerSignalHandlingAndSIGPIPEDefaultInExecChild(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		code int
	}{
		{name: "hangup", sig: syscall.SIGHUP, code: 129},
		{name: "quit", sig: syscall.SIGQUIT, code: 131},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := startTestRunner(t, map[string]string{
				"RESTARTABLE_TEST_RUN":        `trap '' TERM; while :; do /bin/sleep 1; done`,
				"RESTARTABLE_TEST_TERM_GRACE": "100ms",
			})
			status := r.waitStatus(t, string(Running))
			if status.PID == nil {
				t.Fatal("runner has no child PID")
			}
			t.Cleanup(func() { _ = signalGroup(*status.PID, syscall.SIGKILL) })
			if code := r.signalAndWait(t, tc.sig); code != tc.code {
				t.Fatalf("runner exit code = %d, want %d", code, tc.code)
			}
			testwait.Until(t, "child group cleanup", func() bool { return !groupExists(*status.PID) }, testwait.Within(time.Second))
		})
	}

	t.Run("SIGPIPE-is-default-in-exec-child", func(t *testing.T) {
		r := startTestRunnerMode(t, map[string]string{
			"RESTARTABLE_TEST_RUN": `yes | head -c1`,
		}, false)
		testwait.Recv(t, "runner did not exit after yes piped into head", r.done, testwait.Within(3*time.Second))
		if output := readFile(r.stdout) + readFile(r.stderr); strings.Contains(output, "Broken pipe") {
			t.Fatalf("exec child inherited ignored SIGPIPE: %q", output)
		}
	})

	t.Run("broken-stdout", func(t *testing.T) {
		r := startTestRunnerWithBrokenStdout(t, map[string]string{
			"RESTARTABLE_TEST_RUN": `i=0; while [ "$i" -lt 10 ]; do echo output-$i; i=$((i+1)); done; exec /bin/sleep 30`,
		})
		status := r.waitStatus(t, string(Running))
		if status.PID == nil {
			t.Fatal("runner has no child PID after stdout broke")
		}
		testwait.Until(t, "one broken-output warning", func() bool {
			return strings.Contains(readFile(r.stderr), "出力先に書けなくなったので以後のログを捨てる")
		}, testwait.Within(time.Second))

		if got := strings.Count(readFile(r.stderr), "出力先に書けなくなったので以後のログを捨てる"); got != 1 {
			t.Fatalf("broken-output warnings = %d, want one; stderr=%q", got, readFile(r.stderr))
		}
		if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
			t.Fatalf("runner exit code = %d, want 143", code)
		}
	})
}

func TestSIGTERMDuringPresenterStartReachesRunnerSignalHandler(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "handled-signal")
	r := startTestRunnerMode(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                 "exec /bin/sleep 30",
		"RESTARTABLE_TEST_SIGNAL_DURING_START": marker,
	}, false)
	testwait.Recv(t, "runner did not handle SIGTERM delivered during presenter startup", r.done, testwait.Within(3*time.Second))
	if !strings.Contains(readFile(marker), "handled") {
		t.Fatalf("runner did not route startup SIGTERM through its handler; marker=%q exit=%v", readFile(marker), r.waitErr)
	}
	if code := processExitCodeFromError(r.waitErr); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func processExitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func TestForwardKeysExitsWhenEventAndKeyQueuesAreFull(t *testing.T) {
	done := make(chan struct{})
	events := make(chan actorEvent, 128)
	for len(events) < cap(events) {
		events <- actorEvent{kind: keyEvent}
	}
	keys := make(chan string, 32)
	for len(keys) < cap(keys) {
		keys <- "r"
	}
	forwarded := make(chan struct{})
	go func() {
		forwardKeys(done, keys, events)
		close(forwarded)
	}()
	testwait.Until(t, "key forwarder to block on the full actor queue", func() bool { return len(keys) == cap(keys)-1 }, testwait.Within(time.Second))
	keys <- "q"
	close(done)
	testwait.Recv(t, "key forwarder did not stop after actor shutdown", forwarded, testwait.Within(time.Second))
}

// startInteractiveRunner はキーを受ける (UI のある) runner を起動する。子が rc≠0 で落ちたときに待つのはこの形だけ。
func startInteractiveRunner(t *testing.T, options map[string]string) (r *testRunner, keyPath string) {
	return startInteractiveRunnerMode(t, options, true)
}

// startInteractiveRunnerMode の waitForStatus=false は、子がすぐ終わって runner が socket の確認より先に終わりうる起動に使う。
func startInteractiveRunnerMode(t *testing.T, options map[string]string, waitForStatus bool) (r *testRunner, keyPath string) {
	t.Helper()
	keyPath = filepath.Join(runnertest.ShortTempDir(t, "rcr-"), "keys.sock")
	options["RESTARTABLE_TEST_INTERACTIVE"] = "1"
	options["RESTARTABLE_TEST_KEY_SOCKET"] = keyPath
	options["RESTARTABLE_TEST_PRESENTER_STATE"] = filepath.Join(t.TempDir(), "presenter.json")
	return startTestRunnerMode(t, options, waitForStatus), keyPath
}

func TestInteractiveChildCrashWaitsAndControlRestartRebuilds(t *testing.T) {
	launches := filepath.Join(t.TempDir(), "launches")
	// 1 回目の起動は rc 3 で落ち、2 回目は生き続ける
	run := fmt.Sprintf(`echo x >> %s; [ "$(wc -l < %s)" -ge 2 ] && exec /bin/sleep 30; exit 3`, runnertest.ShellQuote(launches), runnertest.ShellQuote(launches))
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": "true", "RESTARTABLE_TEST_RUN": run})
	crashed := r.waitStatus(t, string(Crashed))
	if crashed.PID != nil || crashed.Ready {
		t.Fatalf("crashed status = %+v, want no PID and not ready", crashed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Generation != 2 || response.PID == nil {
		t.Fatalf("restart from crashed = %+v, want the second launch", response)
	}
	r.waitStatus(t, string(Running))
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestInteractiveChildCrashRestartsWithRKeyAndQuitsWithQ(t *testing.T) {
	launches := filepath.Join(t.TempDir(), "launches")
	run := fmt.Sprintf(`echo x >> %s; exit 5`, runnertest.ShellQuote(launches))
	r, keyPath := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": "true", "RESTARTABLE_TEST_RUN": run})
	r.waitStatus(t, string(Crashed))
	sendIntegrationKey(t, keyPath, "R")
	testwait.Until(t, "R to launch again", func() bool { return strings.Count(readFile(launches), "x") >= 2 }, testwait.Within(3*time.Second))
	r.waitStatus(t, string(Crashed))
	sendIntegrationKey(t, keyPath, "Q")
	sendIntegrationKey(t, keyPath, "y")
	testwait.Recv(t, "runner did not exit after Q y; stderr", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string { return "runner did not exit after Q y; stderr: " + readFile(r.stderr) }))
	if r.waitErr != nil {
		t.Fatalf("Q after crash: runner exit = %v, want rc 0", r.waitErr)
	}
}

// rc 0 の終了は人の Cmd+Q 等なので、UI があっても待たずに runner を終える。
func TestInteractiveChildCleanExitStillEndsRunner(t *testing.T) {
	r, _ := startInteractiveRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 0"}, false)
	testwait.Recv(t, "runner kept running after the child exited with rc 0", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		status, _ := r.status()
		return fmt.Sprintf("runner kept running after the child exited with rc 0: %+v", status)
	}))
	if r.waitErr != nil {
		t.Fatalf("clean child exit: runner exit = %v, want rc 0", r.waitErr)
	}
}

// UI の無い起動では待っても操作できないので、子の rc をそのまま runner の rc にして終える。
func TestHeadlessChildCrashExitsWithChildCode(t *testing.T) {
	r := startTestRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 7"}, false)
	testwait.Recv(t, "headless runner did not exit after the child crashed; stderr", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		return "headless runner did not exit after the child crashed; stderr: " + readFile(r.stderr)
	}))
	var exit *exec.ExitError
	if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("headless crash: runner exit = %v, want rc 7", r.waitErr)
	}
}

// crashed からの再ビルドは、落ちた子のプロセスグループの残り (孫) を片付けてから次を起動する。
func TestCrashedRebuildStopsLeftoverGroupBeforeNextLaunch(t *testing.T) {
	dir := t.TempDir()
	launches, marker, armed := filepath.Join(dir, "launches"), filepath.Join(dir, "grandchild-term"), filepath.Join(dir, "grandchild-armed")
	// 1 回目: TERM を受けたら印を書く孫を残して rc 3 で落ちる (孫が trap を張り終えるのを待ってから落ちる。張る前に
	// TERM が届くと印を書かずに死に、片付けが効いていても dirty になる)。2 回目: 起動した時点で印があるかを記録する
	run := fmt.Sprintf(`if [ ! -f %[1]s ]; then echo first > %[1]s; /bin/sh -c 'trap "echo term > %[2]s; exit 0" TERM; : > %[3]s; while :; do sleep 0.05; done' & while [ ! -f %[3]s ]; do sleep 0.01; done; exit 3; fi; if [ -f %[2]s ]; then echo clean >> %[1]s; else echo dirty >> %[1]s; fi; exec /bin/sleep 30`,
		runnertest.ShellQuote(launches), runnertest.ShellQuote(marker), runnertest.ShellQuote(armed))
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "2s"})
	r.waitStatus(t, string(Crashed))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err != nil || !response.OK {
		t.Fatalf("restart from crashed = %+v, %v", response, err)
	}
	testwait.Until(t, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 }, testwait.Within(3*time.Second))
	if got := strings.Fields(readFile(launches)); len(got) != 2 || got[1] != "clean" {
		t.Fatalf("launches = %q, want the second launch after the leftover group got TERM\nstderr: %s\nstdout: %s", got, readFile(r.stderr), readFile(r.stdout))
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

// 起動の板は InitialModel で開いていて transition を通らないので、run() が計測を始める配線を守る。
func TestStartupPanelReportsElapsedTime(t *testing.T) {
	state := filepath.Join(t.TempDir(), "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rst-"), "keys.sock")
	startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": "exec /bin/sleep 30", "RESTARTABLE_TEST_INTERACTIVE": "1",
		"RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	})
	waitPresenterSnapshot(t, state, "startup summary", func(got presenterSnapshot) bool {
		return strings.HasPrefix(got.Model.Message, "起動しました (計 ")
	})
}

// crashedTERMIgnoringGrandchildRun は 1 回目に TERM を無視する孫を残して rc 3 で落ち、2 回目は起動した時点で
// 孫が残っているかを launches に記録して生き続ける run コマンドを返す。
func crashedTERMIgnoringGrandchildRun(dir string) (run, launches string) {
	launches, armed, gcPID := filepath.Join(dir, "launches"), filepath.Join(dir, "armed"), filepath.Join(dir, "gc.pid")
	run = fmt.Sprintf(`if [ ! -f %[1]s ]; then echo first > %[1]s; /bin/sh -c 'trap "" TERM; echo $$ > %[3]s; : > %[2]s; while :; do sleep 0.05; done' & while [ ! -f %[2]s ]; do sleep 0.01; done; exit 3; fi; if kill -0 "$(cat %[3]s)" 2>/dev/null; then echo dirty >> %[1]s; else echo clean >> %[1]s; fi; exec /bin/sleep 30`,
		runnertest.ShellQuote(launches), runnertest.ShellQuote(armed), runnertest.ShellQuote(gcPID))
	return run, launches
}

// TERM を無視する孫も、KILL を送ってから次を起動する。
func TestCrashedRebuildKillsTERMIgnoringLeftoverBeforeNextLaunch(t *testing.T) {
	run, launches := crashedTERMIgnoringGrandchildRun(t.TempDir())
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "200ms"})
	r.waitStatus(t, string(Crashed))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err != nil || !response.OK {
		t.Fatalf("restart from crashed = %+v, %v", response, err)
	}
	testwait.Until(t, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 }, testwait.Within(3*time.Second))
	if got := strings.Fields(readFile(launches)); got[1] != "clean" {
		t.Fatalf("launches = %q, want the TERM-ignoring grandchild killed before the second launch", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

// 片付けの最中に来た restart は、まだ始まっていない片付け後のビルドに乗る (ビルドを増やさない)。
func TestRestartDuringCrashCleanupDoesNotAddBuild(t *testing.T) {
	dir := t.TempDir()
	builds := filepath.Join(dir, "builds")
	run, launches := crashedTERMIgnoringGrandchildRun(dir)
	r, _ := startInteractiveRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": "echo b >> " + runnertest.ShellQuote(builds), "RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "1s",
	})
	r.waitStatus(t, string(Crashed))
	results := make(chan control.Response, 2)
	call := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		response, _ := control.Call(ctx, r.path, control.Restart)
		results <- response
	}
	go call()
	r.waitStatus(t, string(Building)) // 片付けの間 (孫が TERM を無視するので grace の 1s は Building のまま)
	go call()
	for range 2 {
		if response := <-results; !response.OK || response.Generation != 2 {
			t.Fatalf("restart during cleanup = %+v, want both answered by the second launch", response)
		}
	}
	if got := strings.Fields(readFile(builds)); len(got) != 2 {
		t.Fatalf("builds = %q, want the initial build and one rebuild", got)
	}
	testwait.Until(t, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 }, testwait.Within(3*time.Second))
	if got := strings.Fields(readFile(launches)); len(got) != 2 || got[1] != "clean" {
		t.Fatalf("launches = %q", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

// 起動の確認の段で子が落ちたら、所要時間の行で「アプリが終了しました」を上書きしない。
func TestCrashDuringReadyKeepsCrashMessage(t *testing.T) {
	state := filepath.Join(t.TempDir(), "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rcm-"), "keys.sock")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": "sleep 0.4; exit 4", "RESTARTABLE_TEST_READY": "false", "RESTARTABLE_TEST_READY_INTERVAL": "50ms",
		"RESTARTABLE_TEST_INTERACTIVE": "1", "RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	})
	got := waitPresenterSnapshot(t, state, "crashed to be rendered", func(got presenterSnapshot) bool { return got.Model.State == Crashed })
	if !strings.HasPrefix(got.Model.Message, "アプリが終了しました (rc 4)") {
		t.Fatalf("crashed message = %q, want the crash notice", got.Model.Message)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

// processAlive は pid が生きていて zombie でもないか (kill(pid, 0) は zombie にも成功する)。
func processAlive(pid int) bool {
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	stat := strings.TrimSpace(string(out))
	return err == nil && stat != "" && !strings.HasPrefix(stat, "Z")
}

func quitWithConfirm(t *testing.T, keyPath, statePath string) {
	t.Helper()
	sendIntegrationKey(t, keyPath, "Q")
	waitPresenterSnapshot(t, statePath, "quit confirmation", func(got presenterSnapshot) bool { return got.Model.Confirm == ConfirmQuit })
	sendIntegrationKey(t, keyPath, "y")
}

// crashed から Q で終えるときは、落ちたアプリの残り (孫) に触らない (自然終了で孫を消さないのと同じ。586)。
func TestQuitFromCrashedLeavesLeftoverGroup(t *testing.T) {
	dir := t.TempDir()
	run, _ := crashedTERMIgnoringGrandchildRun(dir)
	state := filepath.Join(dir, "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rql-"), "keys.sock")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "100ms", "RESTARTABLE_TEST_INTERACTIVE": "1",
		"RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	})
	r.waitStatus(t, string(Crashed))
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(filepath.Join(dir, "gc.pid"))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	quitWithConfirm(t, keyPath, state)
	testwait.Recv(t, "runner did not exit after Q y", r.done, testwait.Within(3*time.Second))
	// 強制停止は終わるまで runner の終了を止めるので、消すなら r.done の時点で既に消えている
	if !processAlive(pid) {
		t.Fatalf("Q from crashed stopped the leftover grandchild %d", pid)
	}
}

// 片付けの最中に Q で終えたら、片付けの後に起動しない。
func TestQuitDuringCrashCleanupDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	run, launches := crashedTERMIgnoringGrandchildRun(dir)
	state := filepath.Join(dir, "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rqc-"), "keys.sock")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "1s", "RESTARTABLE_TEST_INTERACTIVE": "1",
		"RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	})
	r.waitStatus(t, string(Crashed))
	sendIntegrationKey(t, keyPath, "R")
	r.waitStatus(t, string(Building)) // 孫が TERM を無視するので grace の 1s は片付けの最中
	quitWithConfirm(t, keyPath, state)
	testwait.Recv(t, "runner did not exit after Q y during cleanup", r.done, testwait.Within(4*time.Second))
	// 起動しなかったことには待つ条件が無い。起動したなら子は起動直後に launches へ書くので、その分だけ待つ
	time.Sleep(300 * time.Millisecond) // sleep-ok: negative: 起きないことの確認
	if got := strings.Fields(readFile(launches)); len(got) != 1 {
		t.Fatalf("launches = %q, want no launch after quitting during cleanup", got)
	}
}

// UI の無い起動ではビルドの失敗で待たずに rc 1 で終える (CI や pipe が止まったままにならない)。
func TestHeadlessBuildFailureExitsWithOne(t *testing.T) {
	launches := filepath.Join(t.TempDir(), "launches")
	r := startTestRunnerMode(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": "exit 17", "RESTARTABLE_TEST_RUN": "echo x >> " + runnertest.ShellQuote(launches) + "; exec /bin/sleep 30",
	}, false)
	testwait.Recv(t, "headless runner kept waiting after the build failed; stderr", r.done, testwait.Within(3*time.Second), testwait.Diag(func() string {
		return "headless runner kept waiting after the build failed; stderr: " + readFile(r.stderr)
	}))
	var exit *exec.ExitError
	if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("headless build failure: runner exit = %v, want rc 1", r.waitErr)
	}
	if got := readFile(launches); got != "" {
		t.Fatalf("run started after a failed build: %q", got)
	}
}

// UI の無い起動で、stop-cmd が親だけを終わらせた再起動のビルドが失敗したら、古い子の孫も片付けてから rc 1 で終える。
func TestHeadlessRebuildFailureStopsOldLeftoversBeforeExit(t *testing.T) {
	dir := t.TempDir()
	count, parent, gcPID := filepath.Join(dir, "count"), filepath.Join(dir, "parent.pid"), filepath.Join(dir, "gc.pid")
	build := fmt.Sprintf(`n=0; [ ! -f %[1]s ] || n=$(cat %[1]s); n=$((n+1)); echo "$n" > %[1]s; [ "$n" -eq 1 ]`, runnertest.ShellQuote(count))
	run := fmt.Sprintf(`echo $$ > %[1]s; /bin/sleep 300 & echo $! > %[2]s; exec /bin/sleep 300`, runnertest.ShellQuote(parent), runnertest.ShellQuote(gcPID))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run,
		"RESTARTABLE_TEST_STOP": "kill -KILL $(cat " + runnertest.ShellQuote(parent) + ")",
	})
	r.waitStatus(t, string(Running))
	testwait.Until(t, "grandchild pid", func() bool { return strings.TrimSpace(readFile(gcPID)) != "" }, testwait.Within(3*time.Second))
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(gcPID)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err == nil && response.OK {
		t.Fatalf("restart with a failing rebuild succeeded: %+v", response)
	}
	testwait.Recv(t, "headless runner did not exit after the rebuild failed; stderr", r.done, testwait.Within(4*time.Second), testwait.Diag(func() string {
		return "headless runner did not exit after the rebuild failed; stderr: " + readFile(r.stderr)
	}))
	var exit *exec.ExitError
	if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("headless rebuild failure: runner exit = %v, want rc 1", r.waitErr)
	}
	// 強制停止は終わるまで runner の終了を止めるので、r.done の時点で孫は消えている
	if processAlive(pid) {
		t.Fatalf("old grandchild %d survived the headless exit", pid)
	}
}

// stop-cmd が親だけを終わらせた再起動では、次のビルドの前に古い子の残り (孫) を片付ける。ビルドが失敗して Q で終えても残らない。
func TestQuitFromBuildFailedStopsOldLeftovers(t *testing.T) {
	dir := t.TempDir()
	count, parent, gcPID := filepath.Join(dir, "count"), filepath.Join(dir, "parent.pid"), filepath.Join(dir, "gc.pid")
	build := fmt.Sprintf(`n=0; [ ! -f %[1]s ] || n=$(cat %[1]s); n=$((n+1)); echo "$n" > %[1]s; [ "$n" -eq 1 ]`, runnertest.ShellQuote(count))
	run := fmt.Sprintf(`echo $$ > %[1]s; /bin/sleep 300 & echo $! > %[2]s; exec /bin/sleep 300`, runnertest.ShellQuote(parent), runnertest.ShellQuote(gcPID))
	state := filepath.Join(dir, "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rbq-"), "keys.sock")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run,
		"RESTARTABLE_TEST_STOP":        "kill -KILL $(cat " + runnertest.ShellQuote(parent) + ")",
		"RESTARTABLE_TEST_INTERACTIVE": "1", "RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	})
	r.waitStatus(t, string(Running))
	testwait.Until(t, "grandchild pid", func() bool { return strings.TrimSpace(readFile(gcPID)) != "" }, testwait.Within(3*time.Second))
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(gcPID)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err == nil && response.OK {
		t.Fatalf("restart with a failing rebuild succeeded: %+v", response)
	}
	r.waitStatus(t, string(BuildFailed))
	quitWithConfirm(t, keyPath, state)
	testwait.Recv(t, "runner did not exit after Q y; stderr", r.done, testwait.Within(4*time.Second), testwait.Diag(func() string { return "runner did not exit after Q y; stderr: " + readFile(r.stderr) }))
	if r.waitErr != nil {
		t.Fatalf("Q from build-failed: runner exit = %v, want rc 0", r.waitErr)
	}
	// 強制停止は終わるまで runner の終了を止めるので、r.done の時点で孫は消えている
	if processAlive(pid) {
		t.Fatalf("old grandchild %d survived quitting from build-failed", pid)
	}
}

// stop-cmd が親だけを終わらせた再起動でも、古い子の孫が消えてから新しい子を起動する。
func TestStopCommandRestartStopsOldLeftoversBeforeNextLaunch(t *testing.T) {
	dir := t.TempDir()
	parent, gcPID, launches := filepath.Join(dir, "parent.pid"), filepath.Join(dir, "gc.pid"), filepath.Join(dir, "launches")
	// 2 回目の起動は、1 回目の孫が生きているか (zombie は死んでいると数える) を記録する
	run := fmt.Sprintf(`if [ -f %[2]s ]; then s=$(ps -o stat= -p "$(cat %[2]s)" 2>/dev/null); case "$s" in ""|Z*) echo clean >> %[3]s ;; *) echo dirty >> %[3]s ;; esac; exec /bin/sleep 300; fi; echo $$ > %[1]s; /bin/sleep 300 & echo $! > %[2]s; echo first >> %[3]s; exec /bin/sleep 300`,
		runnertest.ShellQuote(parent), runnertest.ShellQuote(gcPID), runnertest.ShellQuote(launches))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": "true", "RESTARTABLE_TEST_RUN": run,
		"RESTARTABLE_TEST_STOP": "kill -KILL $(cat " + runnertest.ShellQuote(parent) + ")",
	})
	r.waitStatus(t, string(Running))
	testwait.Until(t, "first launch", func() bool { return strings.TrimSpace(readFile(gcPID)) != "" }, testwait.Within(3*time.Second))
	if pid, err := strconv.Atoi(strings.TrimSpace(readFile(gcPID))); err == nil {
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err != nil || !response.OK {
		t.Fatalf("restart = %+v, %v", response, err)
	}
	testwait.Until(t, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 }, testwait.Within(3*time.Second))
	if got := strings.Fields(readFile(launches)); got[1] != "clean" {
		t.Fatalf("launches = %q, want the old grandchild gone before the second launch", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

// 強制停止の最中の Ctrl-C は、猶予 (--term-grace) を待たずに KILL して rc 130 で終える。
func TestCtrlCDuringForcedStopKillsWithoutWaitingForGrace(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "build-started")
	state := filepath.Join(dir, "presenter.json")
	keyPath := filepath.Join(runnertest.ShortTempDir(t, "rcc-"), "keys.sock")
	r := startTestRunnerMode(t, map[string]string{
		"RESTARTABLE_TEST_BUILD":       "trap '' TERM; : > " + runnertest.ShellQuote(started) + "; while :; do sleep 0.05; done",
		"RESTARTABLE_TEST_RUN":         "exec /bin/sleep 300",
		"RESTARTABLE_TEST_TERM_GRACE":  "60s",
		"RESTARTABLE_TEST_INTERACTIVE": "1", "RESTARTABLE_TEST_KEY_SOCKET": keyPath, "RESTARTABLE_TEST_PRESENTER_STATE": state,
	}, true)
	testwait.Until(t, "build to start", func() bool { _, err := os.Stat(started); return err == nil }, testwait.Within(3*time.Second))
	quitWithConfirm(t, keyPath, state) // ビルド中の Q → 強制停止 (ビルドは TERM を無視するので 60s の猶予を待つ)
	waitPresenterSnapshot(t, state, "exiting", func(got presenterSnapshot) bool { return got.Model.State == Exiting })
	sendIntegrationKey(t, keyPath, "ctrl+c")
	// hang guard (猶予の 60s より十分短い)
	testwait.Recv(t, "Ctrl-C did not cut the 60s grace short; stderr", r.done, testwait.Within(10*time.Second), testwait.Diag(func() string {
		return "Ctrl-C did not cut the 60s grace short; stderr: " + readFile(r.stderr)
	}))
	var exit *exec.ExitError
	if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 130 {
		t.Fatalf("Ctrl-C during forced stop: runner exit = %v, want rc 130", r.waitErr)
	}
}

// UI の無い起動のビルド失敗は、失敗したビルドが残したプロセスも片付けてから rc 1 で終える。
func TestHeadlessBuildFailureStopsBuildLeftovers(t *testing.T) {
	bgPID := filepath.Join(t.TempDir(), "bg.pid")
	r := startTestRunnerMode(t, map[string]string{
		"RESTARTABLE_TEST_BUILD": "/bin/sleep 300 </dev/null >/dev/null 2>&1 & echo $! > " + runnertest.ShellQuote(bgPID) + "; exit 1",
		"RESTARTABLE_TEST_RUN":   "exec /bin/sleep 300",
	}, false)
	testwait.Recv(t, "headless runner did not exit after the build failed; stderr", r.done, testwait.Within(4*time.Second), testwait.Diag(func() string {
		return "headless runner did not exit after the build failed; stderr: " + readFile(r.stderr)
	}))
	var exit *exec.ExitError
	if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("headless build failure: runner exit = %v, want rc 1", r.waitErr)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(bgPID)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	// 強制停止は終わるまで runner の終了を止めるので、r.done の時点で消えている
	if processAlive(pid) {
		t.Fatalf("background process %d left by the failed build survived the headless exit", pid)
	}
}
