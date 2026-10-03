package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
		StopCommandTimeout:  envDuration("RESTARTABLE_TEST_STOP_TIMEOUT", 250*time.Millisecond),
		TermGrace:           envDuration("RESTARTABLE_TEST_TERM_GRACE", 60*time.Millisecond),
		ReadyCommand:        os.Getenv("RESTARTABLE_TEST_READY"),
		ReadyTimeout:        envDuration("RESTARTABLE_TEST_READY_TIMEOUT", 120*time.Second),
		ReadyInterval:       envDuration("RESTARTABLE_TEST_READY_INTERVAL", 500*time.Millisecond),
		ReadyAttemptTimeout: envDuration("RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT", 5*time.Second),
		ReadyCleanupGrace:   envDuration("RESTARTABLE_TEST_READY_CLEANUP_GRACE", 30*time.Millisecond),
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

func envDuration(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return value
}

type testRunner struct {
	cmd     *exec.Cmd
	path    string
	stdout  string
	stderr  string
	done    chan struct{}
	waitErr error
}

type ptyStdinRunner struct {
	cmd    *exec.Cmd
	master *os.File
	path   string
	done   chan struct{}
	err    error

	outputMu sync.Mutex
	output   bytes.Buffer
}

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
	r := &ptyStdinRunner{cmd: cmd, master: master, path: socketPath, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = cmd.Wait()
	}()
	go func() {
		defer func() { _ = stdoutRead.Close() }()
		buf := make([]byte, 4096)
		for {
			n, readErr := stdoutRead.Read(buf)
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

func (r *ptyStdinRunner) outputText() string {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	return r.output.String()
}

func (r *ptyStdinRunner) waitOutput(t *testing.T, want string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		if strings.Contains(r.outputText(), want) {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %q; output=%q", want, r.outputText())
		case <-r.done:
			t.Fatalf("runner exited before %q: %v; output=%q", want, r.err, r.outputText())
		}
	}
}

func (r *ptyStdinRunner) signalAndWait(t *testing.T, sig syscall.Signal) int {
	t.Helper()
	if err := r.cmd.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
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
	case <-time.After(4 * time.Second):
		t.Fatalf("runner did not exit; output=%q", r.outputText())
	}
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
	waitFor(t, 3*time.Second, description, func() bool {
		got, ok := readPresenterSnapshot(path)
		if !ok {
			return false
		}
		snapshot = got
		return predicate(got)
	})
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
	dir := shortTempDir(t, "rnt-")
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
		waitFor(t, 3*time.Second, "runner control socket", func() bool {
			select {
			case <-r.done:
				t.Fatalf("runner exited before opening control socket: %v; stderr: %s", r.waitErr, readFile(stderr))
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			response, err := control.Call(ctx, path, control.Status)
			return err == nil && response.OK
		})
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
	waitFor(t, 3*time.Second, "runner state "+want, func() bool {
		got, err := r.status()
		if err != nil {
			return false
		}
		response = got
		return got.State == want
	})
	return response
}

func (r *testRunner) signalAndWait(t *testing.T, sig syscall.Signal) int {
	t.Helper()
	_ = r.cmd.Process.Signal(sig)
	select {
	case <-r.done:
		err := r.waitErr
		if err == nil {
			return 0
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		t.Fatalf("wait runner: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatalf("runner did not exit after signal %v; stderr: %s", sig, readFile(r.stderr))
	}
	return -1
}

func waitFor(t *testing.T, limit time.Duration, description string, predicate func() bool) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

// shortTempDir は unix socket を置く一時ディレクトリ。socket の path は 103 byte までなので、
// 長い t.TempDir() (macOS では /var/folders/...) ではなく /tmp の直下に作り、テストの終わりに消す。
func shortTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func shellQuote(s string) string  { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func readFile(path string) string { data, _ := os.ReadFile(path); return string(data) }

func TestHeadlessRunnerWithTerminalStdinDoesNotPassTTYToChild(t *testing.T) {
	dir := shortTempDir(t, "rstin-")
	socketPath := filepath.Join(dir, "runner.sock")
	stopMarker := filepath.Join(dir, "stop-child")
	run := fmt.Sprintf(`rm -f %s; if read value; then echo run-stdin-read; else echo run-stdin-eof; fi; while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0`, shellQuote(stopMarker), shellQuote(stopMarker))
	stop := `if read value; then echo stop-stdin-read; else echo stop-stdin-eof; fi; touch ` + shellQuote(stopMarker)
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
		t.Fatalf("control restart: %v; child output=%q", err, r.outputText())
	}
	if !response.OK || response.Generation != 2 {
		t.Fatalf("control restart response = %+v, want successful generation 2", response)
	}
	r.waitOutput(t, "stop-stdin-eof")
	waitFor(t, 3*time.Second, "second build with null stdin", func() bool {
		return strings.Count(r.outputText(), "build-stdin-eof") >= 2
	})
	if output := r.outputText(); strings.Contains(output, "stdin-read") {
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
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; if [ "$n" -eq 1 ]; then echo stale > %s; exit 1; fi; echo fresh > %s`, shellQuote(count), shellQuote(count), shellQuote(count), shellQuote(artifact), shellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, shellQuote(artifact), shellQuote(launches), shellQuote(launches))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
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
	waitFor(t, time.Second, "new run to record artifact", func() bool { return strings.TrimSpace(readFile(launches)) != "" })
	if got := strings.TrimSpace(readFile(launches)); got != "fresh" {
		t.Fatalf("run used artifact %q, want fresh", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestBuildFailureIsReturnedToControlRestart(t *testing.T) {
	r := startTestRunner(t, map[string]string{
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
	run := fmt.Sprintf(`echo "$RUNNER_INSTANCE" > %s; exec /bin/sleep 30`, shellQuote(runID))
	stop := fmt.Sprintf(`echo "$RUNNER_INSTANCE" > %s; exit 9`, shellQuote(stopID))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_STOP": stop,
		"RESTARTABLE_TEST_ID_ENV": "RUNNER_INSTANCE",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil {
		t.Fatalf("running response has no pid: %+v", status)
	}
	waitFor(t, time.Second, "run id env", func() bool { return readFile(runID) != "" })
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
	dir := shortTempDir(t, "rkey-")
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
		select {
		case result := <-resultCh:
			if result.err != nil || result.response.OK || result.response.Reason != "stop cancelled" {
				t.Fatalf("restart response %d after Esc = %+v, err=%v", i+1, result.response, result.err)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("restart request %d was not rejected after Esc", i+1)
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
	stop := fmt.Sprintf(`touch %s; read ignored < %s; exit 9`, shellQuote(started), shellQuote(fifo))
	keyDir := shortTempDir(t, "rkey-")
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
	waitFor(t, 2*time.Second, "stop-cmd start marker", func() bool {
		_, err := os.Stat(started)
		return err == nil
	})
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
	select {
	case result := <-restartResult:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.response.OK || !strings.Contains(result.response.Reason, "stop-cmd failed") {
			t.Fatalf("restart response after child exit = %+v", result.response)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("failed stop-cmd request was not rejected after child exit")
	}
	select {
	case <-r.done:
		if r.waitErr != nil {
			t.Fatalf("runner did not exit naturally after stop failure: %v; stderr: %s", r.waitErr, readFile(r.stderr))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner kept running after stop-cmd failed and child exited")
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
	waitFor(t, time.Second, "TERM-ignoring child removal", func() bool { return errors.Is(syscall.Kill(*status.PID, 0), syscall.ESRCH) })
}

func TestSIGTERMStopsGrandchildInChildProcessGroup(t *testing.T) {
	grandchildFile := filepath.Join(t.TempDir(), "grandchild-pid")
	run := fmt.Sprintf(`/bin/sleep 30 & echo $! > %s; wait`, shellQuote(grandchildFile))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run})
	status := r.waitStatus(t, string(Running))
	waitFor(t, time.Second, "grandchild pid", func() bool { return strings.TrimSpace(readFile(grandchildFile)) != "" })
	grandchild, err := strconv.Atoi(strings.TrimSpace(readFile(grandchildFile)))
	if err != nil {
		t.Fatal(err)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	waitFor(t, time.Second, "grandchild process group cleanup", func() bool { return errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH) })
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
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; echo "$n" >> %s; while [ ! -e %s.$n ]; do /bin/sleep 0.01; done; echo "$n" > %s`, shellQuote(count), shellQuote(count), shellQuote(count), shellQuote(starts), shellQuote(filepath.Join(dir, "gate")), shellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, shellQuote(artifact), shellQuote(launches), shellQuote(launches))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
	waitFor(t, 2*time.Second, "initial build start", func() bool { return strings.TrimSpace(readFile(starts)) == "1" })
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
	waitFor(t, time.Second, "control request accepted", func() bool {
		status, err := r.status()
		return err == nil && status.RestartPending
	})
	if err := os.WriteFile(filepath.Join(dir, "gate.1"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "required second build start", func() bool { return strings.Contains(readFile(starts), "2\n") })
	if got := readFile(launches); got != "" {
		t.Fatalf("first build artifact launched before required rebuild: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.2"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !result.response.OK || result.response.Generation != 1 {
			t.Fatalf("restart response = %+v", result.response)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("restart response timed out")
	}
	waitFor(t, time.Second, "new run to record artifact", func() bool { return strings.TrimSpace(readFile(launches)) != "" })
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
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s; echo "$n" >> %s; while [ ! -e %s.$n ]; do /bin/sleep 0.01; done; echo "$n" > %s`, shellQuote(count), shellQuote(count), shellQuote(count), shellQuote(starts), shellQuote(gateBase), shellQuote(artifact))
	run := fmt.Sprintf(`cat %s >> %s; echo >> %s; exec /bin/sleep 30`, shellQuote(artifact), shellQuote(launches), shellQuote(launches))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": run})
	waitFor(t, 2*time.Second, "initial build start", func() bool { return strings.TrimSpace(readFile(starts)) == "1" })
	conn, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(struct {
		Command string `json:"command"`
	}{Command: "restart"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "pre-commit request accepted", func() bool { status, err := r.status(); return err == nil && status.RestartPending })
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "pre-commit disconnect observed", func() bool { status, err := r.status(); return err == nil && !status.RestartPending })
	if err := os.WriteFile(gateBase+".1", nil, 0600); err != nil {
		t.Fatal(err)
	}
	status := r.waitStatus(t, string(Running))
	waitFor(t, time.Second, "current build launch", func() bool { return strings.TrimSpace(readFile(launches)) == "1" })
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
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s`, shellQuote(count), shellQuote(count), shellQuote(count))
	// 子は release が置かれるまで TERM で終わらない。終わると Stopping+RestartPending の窓が一瞬で閉じ、負荷が高いと
	// ポーリングがその窓を取り逃がして落ちていた。窓を開けたまま切断し、切断の後で release して再起動の続きを見る
	run := fmt.Sprintf(`trap 'while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0' TERM; while :; do /bin/sleep 0.01; done`, shellQuote(release))
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
	waitFor(t, 10*time.Second, "restart commit", func() bool {
		status, err := r.status()
		return err == nil && status.State == string(Stopping) && status.RestartPending
	})
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "committed restart despite disconnect", func() bool {
		status, err := r.status()
		return err == nil && status.State == string(Running) && status.Generation == 2 && status.PID != nil
	})
	if got := strings.TrimSpace(readFile(count)); got != "2" {
		t.Fatalf("build count after committed disconnect = %q", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestChildExitWinsOverLaterControlRestart(t *testing.T) {
	r := startTestRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 0"}, false)
	select {
	case <-r.done:
		err := r.waitErr
		if err != nil {
			t.Fatalf("natural runner exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		status, err := r.status()
		t.Fatalf("runner did not exit after child: status=%+v statusErr=%v stderr=%s", status, err, readFile(r.stderr))
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
	dir := shortTempDir(t, "rsts-")
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
	command := fmt.Sprintf("(read value < %s; echo alive > %s; exec /bin/sleep 30) & echo $! > %s; exit 0", shellQuote(gate), shellQuote(marker), shellQuote(pidFile))
	proc, err := startProcess([]string{"/bin/sh", "-c", command}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	<-proc.done
	waitFor(t, time.Second, "descendant PID", func() bool { return strings.TrimSpace(readFile(pidFile)) != "" })
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

	waitFor(t, time.Second, "natural-exit descendant to wait at the named pipe", func() bool {
		file, err := os.OpenFile(gate, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return false
		}
		gateWriter = file
		return true
	})
	if _, err := gateWriter.WriteString("continue\n"); err != nil {
		t.Fatal(err)
	}
	_ = gateWriter.Close()
	waitFor(t, time.Second, "natural-exit descendant to continue after output drain", func() bool { return strings.TrimSpace(readFile(marker)) == "alive" })
	if !strings.Contains(stderr.String(), fmt.Sprintf("子孫が出力を握ったまま残っている (pgid %d)", proc.pid)) {
		t.Fatalf("missing descendant output warning: %q", stderr.String())
	}
}

func TestFinishedMarkerRejectsRestartBeforeExitEventIsConsumed(t *testing.T) {
	dir := shortTempDir(t, "rfin-")
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
	select {
	case <-r.done:
		err := r.waitErr
		if err != nil {
			t.Fatalf("runner exit: %v; stderr: %s", err, readFile(r.stderr))
		}
	case <-time.After(3 * time.Second):
		status, err := r.status()
		t.Fatalf("runner did not exit: status=%+v statusErr=%v stderr=%s", status, err, readFile(r.stderr))
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
	dir := shortTempDir(t, "rbuild-")
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
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("beginForceStop deadlocked sending to its own unbuffered event channel")
	}
}

func TestForcedShutdownKillsStopCommandGroupAfterChildExits(t *testing.T) {
	dir := t.TempDir()
	stopPIDFile := filepath.Join(dir, "stop.pid")
	stop := fmt.Sprintf(`trap '' TERM; echo $$ > %s; while :; do /bin/sleep 1; done`, shellQuote(stopPIDFile))
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
	waitFor(t, time.Second, "stop command to start", func() bool { return strings.TrimSpace(readFile(stopPIDFile)) != "" })
	stopPID, err := strconv.Atoi(strings.TrimSpace(readFile(stopPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = signalGroup(stopPID, syscall.SIGKILL) })
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("control call did not finish during forced shutdown")
	}
	waitFor(t, time.Second, "stop command group cleanup", func() bool { return !groupExists(stopPID) })
}

func TestForcedShutdownIncludesGroupsWhoseLeadersWereReaped(t *testing.T) {
	dir := t.TempDir()
	stopPIDFile := filepath.Join(dir, "stop-group.pid")
	stop := fmt.Sprintf(`echo $$ > %s; (trap '' TERM; while :; do /bin/sleep 1; done) >/dev/null 2>&1 & exit 0`, shellQuote(stopPIDFile))
	keyDir := shortTempDir(t, "rkey-")
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
	waitFor(t, time.Second, "stop group leader PID", func() bool { return strings.TrimSpace(readFile(stopPIDFile)) != "" })
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
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("pending restart call did not finish during forced shutdown")
	}
	waitFor(t, time.Second, "reaped stop command group cleanup", func() bool { return !groupExists(stopPID) })
}

func TestForcedShutdownKillsGrandchildAfterLeaderExits(t *testing.T) {
	grandchildFile := filepath.Join(t.TempDir(), "grandchild.pid")
	run := fmt.Sprintf(`(trap '' TERM; while :; do /bin/sleep 1; done) & echo $! > %s; wait`, shellQuote(grandchildFile))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "100ms",
	})
	status := r.waitStatus(t, string(Running))
	if status.PID == nil {
		t.Fatal("runner has no child PID")
	}
	waitFor(t, time.Second, "grandchild PID", func() bool { return strings.TrimSpace(readFile(grandchildFile)) != "" })
	t.Cleanup(func() { _ = signalGroup(*status.PID, syscall.SIGKILL) })
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
	waitFor(t, time.Second, "grandchild group cleanup", func() bool { return !groupExists(*status.PID) })
}

func TestGroupStopWaitsForDescendantAfterLeaderExit(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "descendant-ready")
	leaderExited := filepath.Join(dir, "leader-exited")
	release := filepath.Join(dir, "release-descendant")
	gracefulExit := filepath.Join(dir, "descendant-exited-gracefully")
	childPIDPath := filepath.Join(dir, "descendant.pid")
	run := fmt.Sprintf(`(trap 'echo ready > %s; while [ ! -e %s ]; do /bin/sleep 0.01; done; echo graceful > %s; exit 0' TERM; while :; do /bin/sleep 0.01; done) & echo $! > %s; trap 'while [ ! -e %s ]; do /bin/sleep 0.01; done; echo exited > %s; exit 0' TERM; wait`,
		shellQuote(ready), shellQuote(release), shellQuote(gracefulExit), shellQuote(childPIDPath), shellQuote(ready), shellQuote(leaderExited))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "30s",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no child PID")
	}
	waitFor(t, time.Second, "descendant PID", func() bool { return strings.TrimSpace(readFile(childPIDPath)) != "" })
	t.Cleanup(func() { _ = signalGroup(*initial.PID, syscall.SIGKILL) })
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
	waitFor(t, 10*time.Second, "descendant to receive SIGTERM", func() bool { return strings.TrimSpace(readFile(ready)) != "" })
	waitFor(t, 10*time.Second, "group leader to finish before its descendant", func() bool {
		return errors.Is(syscall.Kill(*initial.PID, 0), syscall.ESRCH) && strings.TrimSpace(readFile(leaderExited)) != ""
	})
	if !groupExists(*initial.PID) {
		t.Fatal("process group disappeared while its descendant was still handling SIGTERM")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "descendant to complete graceful termination", func() bool { return strings.TrimSpace(readFile(gracefulExit)) != "" })
	select {
	case result := <-restartDone:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !result.response.OK || result.response.Generation != 2 {
			t.Fatalf("restart response = %+v", result.response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not finish after the process group exited")
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestQuitConfirmationDuringBuildStopsBuildAndExits(t *testing.T) {
	dir := t.TempDir()
	keyDir := shortTempDir(t, "rkey-")
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
	select {
	case <-r.done:
	case <-time.After(4 * time.Second):
		t.Fatal("runner did not exit after confirmed build quit")
	}
	if r.waitErr != nil {
		t.Fatalf("runner exit = %v, want code 0", r.waitErr)
	}
}

func TestQuitConfirmationDuringReadinessUsesStopCommand(t *testing.T) {
	dir := t.TempDir()
	keyDir := shortTempDir(t, "rkey-")
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(t.TempDir(), "presenter.json")
	childPIDPath := filepath.Join(dir, "child.pid")
	stopMarkerPath := filepath.Join(dir, "stop-called")
	stopReleasePath := filepath.Join(dir, "stop-release")
	if err := syscall.Mkfifo(stopReleasePath, 0600); err != nil {
		t.Fatal(err)
	}
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   fmt.Sprintf("printf '%%s' \"$$\" > %s; exec /bin/sleep 30", shellQuote(childPIDPath)),
		"RESTARTABLE_TEST_READY":                 "exec /bin/sleep 30",
		"RESTARTABLE_TEST_READY_TIMEOUT":         "30s",
		"RESTARTABLE_TEST_READY_INTERVAL":        "5ms",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "30s",
		"RESTARTABLE_TEST_STOP_TIMEOUT":          "30s",
		"RESTARTABLE_TEST_STOP":                  fmt.Sprintf("printf started > %s; read release < %s; kill -TERM \"$(cat %s)\"; printf done > %s", shellQuote(stopMarkerPath), shellQuote(stopReleasePath), shellQuote(childPIDPath), shellQuote(stopMarkerPath)),
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
	waitFor(t, 3*time.Second, "stop-cmd to run for a quit during readiness", func() bool {
		return readFile(stopMarkerPath) == "started"
	})
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
	waitFor(t, 3*time.Second, "stop-cmd to stop the running child", func() bool {
		return readFile(stopMarkerPath) == "done"
	})
	select {
	case <-r.done:
	case <-time.After(4 * time.Second):
		t.Fatal("runner did not exit after stop-cmd stopped the child")
	}
	if r.waitErr != nil {
		t.Fatalf("runner exit = %v, want code 0", r.waitErr)
	}
}

func TestQuitStopCancellationResumesUnconfirmedReadyCheck(t *testing.T) {
	dir := t.TempDir()
	keyDir := shortTempDir(t, "rkey-")
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
		shellQuote(attemptPath), shellQuote(attemptPath), shellQuote(attemptPath), shellQuote(firstReadyGate), shellQuote(secondReadyGate), shellQuote(attemptResultPath))
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
	waitFor(t, 3*time.Second, "first readiness command to block on its gate", func() bool {
		return strings.TrimSpace(readFile(attemptPath)) == "1"
	})
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
	waitFor(t, 3*time.Second, "readiness command to restart after Esc", func() bool {
		return strings.TrimSpace(readFile(attemptPath)) == "2"
	})
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
		shellQuote(cleanupStarted), shellQuote(cleanupRelease), shellQuote(descendantStarted), shellQuote(descendantStarted))
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
	waitFor(t, 3*time.Second, "old cleanup process to enter its gated TERM handler", func() bool {
		_, err := os.Stat(cleanupStarted)
		return err == nil
	})
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
	waitFor(t, 3*time.Second, "queued ready process group to be cleaned", func() bool {
		return !groupExists(newProc.pid)
	})
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
			keyDir := shortTempDir(t, "rkey-")
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
			select {
			case <-r.done:
			case <-time.After(4 * time.Second):
				t.Fatalf("runner did not exit after %s", tc.name)
			}
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
	ready := fmt.Sprintf(`n=$(cat %s 2>/dev/null || echo 0); n=$((n+1)); printf '%%s' "$n" > %s; [ "$n" -ge 3 ] && [ "$OBAKET_DEV_LOOP" = "$(cat %s)" ]`, shellQuote(countPath), shellQuote(countPath), shellQuote(runIDPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":            fmt.Sprintf(`printf '%%s' "$OBAKET_DEV_LOOP" > %s; exec /bin/sleep 30`, shellQuote(runIDPath)),
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
	waitFor(t, 3*time.Second, "ready command to succeed on its third attempt", func() bool {
		got, err := r.status()
		if err != nil {
			return false
		}
		readyStatus = got
		return got.Ready
	})
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
	ready := fmt.Sprintf(`echo $$ >> %s; trap '' TERM; while :; do /bin/sleep 1; done`, shellQuote(pidPath))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   fmt.Sprintf(`while [ ! -e %s ]; do /bin/sleep 0.01; done; touch %s; exec /bin/sleep 30`, shellQuote(releaseChild), shellQuote(childSurvived)),
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
	waitFor(t, 3*time.Second, "ready timeout result", func() bool {
		return strings.Contains(readFile(r.stderr), "起動を確認できませんでした")
	})
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
	waitFor(t, 2*time.Second, "application continues after readiness timeout", func() bool {
		_, err := os.Stat(childSurvived)
		return err == nil
	})
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
		waitFor(t, 2*time.Second, fmt.Sprintf("ready probe process group %d cleanup", pid), func() bool {
			return !groupExists(pid)
		})
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestChildCanExitBeforeReadyProbeAndProbeGroupIsReaped(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "probe-started")
	pidPath := filepath.Join(dir, "probe-pid")
	run := fmt.Sprintf(`while [ ! -e %s ]; do /bin/sleep 0.01; done; exit 0`, shellQuote(started))
	ready := fmt.Sprintf(`echo $$ > %s; touch %s; trap '' TERM; while :; do /bin/sleep 1; done`, shellQuote(pidPath), shellQuote(started))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   run,
		"RESTARTABLE_TEST_READY":                 ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":         "5s",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "2s",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE":   "20ms",
	})
	select {
	case <-r.done:
		if r.waitErr != nil {
			t.Fatalf("runner exit after child finished first: %v; stderr=%s", r.waitErr, readFile(r.stderr))
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("runner did not exit after its child; stderr=%s", readFile(r.stderr))
	}
	if got := strings.TrimSpace(readFile(r.stderr)); got != "起動を確認できませんでした" {
		t.Fatalf("headless child-exit-before-ready result = %q", got)
	}
	pidText := strings.TrimSpace(readFile(pidPath))
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("ready probe pid = %q: %v", pidText, err)
	}
	waitFor(t, 2*time.Second, "ready probe group cleanup after child exit", func() bool { return !groupExists(pid) })
}

func TestControlRestartDuringReadyDiscardsOldGenerationResult(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "attempts")
	firstGate := filepath.Join(dir, "first-gate")
	secondGate := filepath.Join(dir, "second-gate")
	ready := fmt.Sprintf(`n=$(cat %s 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > %s; if [ "$n" -eq 1 ]; then trap '' TERM; gate=%s; else gate=%s; fi; while [ ! -e "$gate" ]; do /bin/sleep 0.01; done; exit 0`, shellQuote(countPath), shellQuote(countPath), shellQuote(firstGate), shellQuote(secondGate))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":                   "exec /bin/sleep 30",
		"RESTARTABLE_TEST_READY":                 ready,
		"RESTARTABLE_TEST_READY_TIMEOUT":         "5s",
		"RESTARTABLE_TEST_READY_INTERVAL":        "5ms",
		"RESTARTABLE_TEST_READY_ATTEMPT_TIMEOUT": "4s",
		"RESTARTABLE_TEST_READY_CLEANUP_GRACE":   "20ms",
		"RESTARTABLE_TEST_TERM_GRACE":            "60ms",
	})
	waitFor(t, 3*time.Second, "first generation readiness probe", func() bool {
		return strings.TrimSpace(readFile(countPath)) == "1"
	})
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
	waitFor(t, 3*time.Second, "second generation readiness probe", func() bool {
		return strings.TrimSpace(readFile(countPath)) == "2"
	})
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
	waitFor(t, 3*time.Second, "second generation readiness", func() bool {
		got, err := r.status()
		return err == nil && got.Generation == 2 && got.Ready
	})
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
			waitFor(t, time.Second, "child group cleanup", func() bool { return !groupExists(*status.PID) })
		})
	}

	t.Run("SIGPIPE-is-default-in-exec-child", func(t *testing.T) {
		r := startTestRunnerMode(t, map[string]string{
			"RESTARTABLE_TEST_RUN": `yes | head -c1`,
		}, false)
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			t.Fatal("runner did not exit after yes piped into head")
		}
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
		waitFor(t, time.Second, "one broken-output warning", func() bool {
			return strings.Contains(readFile(r.stderr), "出力先に書けなくなったので以後のログを捨てる")
		})
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
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not handle SIGTERM delivered during presenter startup")
	}
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
	waitFor(t, time.Second, "key forwarder to block on the full actor queue", func() bool { return len(keys) == cap(keys)-1 })
	keys <- "q"
	close(done)
	select {
	case <-forwarded:
	case <-time.After(time.Second):
		t.Fatal("key forwarder did not stop after actor shutdown")
	}
}

// startInteractiveRunner はキーを受ける (UI のある) runner を起動する。子が rc≠0 で落ちたときに待つのはこの形だけ。
func startInteractiveRunner(t *testing.T, options map[string]string) (r *testRunner, keyPath string) {
	t.Helper()
	keyPath = filepath.Join(shortTempDir(t, "rcr-"), "keys.sock")
	options["RESTARTABLE_TEST_INTERACTIVE"] = "1"
	options["RESTARTABLE_TEST_KEY_SOCKET"] = keyPath
	options["RESTARTABLE_TEST_PRESENTER_STATE"] = filepath.Join(t.TempDir(), "presenter.json")
	return startTestRunner(t, options), keyPath
}

func TestInteractiveChildCrashWaitsAndControlRestartRebuilds(t *testing.T) {
	launches := filepath.Join(t.TempDir(), "launches")
	// 1 回目の起動は rc 3 で落ち、2 回目は生き続ける
	run := fmt.Sprintf(`echo x >> %s; [ "$(wc -l < %s)" -ge 2 ] && exec /bin/sleep 30; exit 3`, shellQuote(launches), shellQuote(launches))
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
	run := fmt.Sprintf(`echo x >> %s; exit 5`, shellQuote(launches))
	r, keyPath := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": "true", "RESTARTABLE_TEST_RUN": run})
	r.waitStatus(t, string(Crashed))
	sendIntegrationKey(t, keyPath, "R")
	waitFor(t, 3*time.Second, "R to launch again", func() bool { return strings.Count(readFile(launches), "x") >= 2 })
	r.waitStatus(t, string(Crashed))
	sendIntegrationKey(t, keyPath, "Q")
	sendIntegrationKey(t, keyPath, "y")
	select {
	case <-r.done:
		if r.waitErr != nil {
			t.Fatalf("Q after crash: runner exit = %v, want rc 0", r.waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("runner did not exit after Q y; stderr: %s", readFile(r.stderr))
	}
}

// rc 0 の終了は人の Cmd+Q 等なので、UI があっても待たずに runner を終える (issue 586 の R7)。
func TestInteractiveChildCleanExitStillEndsRunner(t *testing.T) {
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 0"})
	select {
	case <-r.done:
		if r.waitErr != nil {
			t.Fatalf("clean child exit: runner exit = %v, want rc 0", r.waitErr)
		}
	case <-time.After(3 * time.Second):
		status, _ := r.status()
		t.Fatalf("runner kept running after the child exited with rc 0: %+v", status)
	}
}

// UI の無い起動では待っても操作できないので、子の rc をそのまま runner の rc にして終える。
func TestHeadlessChildCrashExitsWithChildCode(t *testing.T) {
	r := startTestRunnerMode(t, map[string]string{"RESTARTABLE_TEST_RUN": "exit 7"}, false)
	select {
	case <-r.done:
		var exit *exec.ExitError
		if !errors.As(r.waitErr, &exit) || exit.ExitCode() != 7 {
			t.Fatalf("headless crash: runner exit = %v, want rc 7", r.waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("headless runner did not exit after the child crashed; stderr: %s", readFile(r.stderr))
	}
}

// crashed からの再ビルドは、落ちた子のプロセスグループの残り (孫) を片付けてから次を起動する。
func TestCrashedRebuildStopsLeftoverGroupBeforeNextLaunch(t *testing.T) {
	dir := t.TempDir()
	launches, marker, armed := filepath.Join(dir, "launches"), filepath.Join(dir, "grandchild-term"), filepath.Join(dir, "grandchild-armed")
	// 1 回目: TERM を受けたら印を書く孫を残して rc 3 で落ちる (孫が trap を張り終えるのを待ってから落ちる。張る前に
	// TERM が届くと印を書かずに死に、片付けが効いていても dirty になる)。2 回目: 起動した時点で印があるかを記録する
	run := fmt.Sprintf(`if [ ! -f %[1]s ]; then echo first > %[1]s; /bin/sh -c 'trap "echo term > %[2]s; exit 0" TERM; : > %[3]s; while :; do sleep 0.05; done' & while [ ! -f %[3]s ]; do sleep 0.01; done; exit 3; fi; if [ -f %[2]s ]; then echo clean >> %[1]s; else echo dirty >> %[1]s; fi; exec /bin/sleep 30`,
		shellQuote(launches), shellQuote(marker), shellQuote(armed))
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "2s"})
	r.waitStatus(t, string(Crashed))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err != nil || !response.OK {
		t.Fatalf("restart from crashed = %+v, %v", response, err)
	}
	waitFor(t, 3*time.Second, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 })
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
	keyPath := filepath.Join(shortTempDir(t, "rst-"), "keys.sock")
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
		shellQuote(launches), shellQuote(armed), shellQuote(gcPID))
	return run, launches
}

// TERM を無視する孫も、KILL の後にグループが消えてから次を起動する。
func TestCrashedRebuildKillsTERMIgnoringLeftoverBeforeNextLaunch(t *testing.T) {
	run, launches := crashedTERMIgnoringGrandchildRun(t.TempDir())
	r, _ := startInteractiveRunner(t, map[string]string{"RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "200ms"})
	r.waitStatus(t, string(Crashed))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if response, err := control.Call(ctx, r.path, control.Restart); err != nil || !response.OK {
		t.Fatalf("restart from crashed = %+v, %v", response, err)
	}
	waitFor(t, 3*time.Second, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 })
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
		"RESTARTABLE_TEST_BUILD": "echo b >> " + shellQuote(builds), "RESTARTABLE_TEST_RUN": run, "RESTARTABLE_TEST_TERM_GRACE": "1s",
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
	waitFor(t, 3*time.Second, "second launch to record", func() bool { return len(strings.Fields(readFile(launches))) >= 2 })
	if got := strings.Fields(readFile(launches)); len(got) != 2 || got[1] != "clean" {
		t.Fatalf("launches = %q", got)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}
