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
	"syscall"
	"testing"
	"time"

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
	var presenter Presenter
	if keyPath := os.Getenv("RESTARTABLE_TEST_KEY_SOCKET"); keyPath != "" {
		var err error
		presenter, err = newIntegrationPresenter(keyPath, os.Getenv("RESTARTABLE_TEST_PRESENTER_STATE"))
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code, err := Run(Config{
		BuildCommand:       os.Getenv("RESTARTABLE_TEST_BUILD"),
		RunArgs:            []string{"/bin/sh", "-c", os.Getenv("RESTARTABLE_TEST_RUN")},
		StopCommand:        os.Getenv("RESTARTABLE_TEST_STOP"),
		StopCommandTimeout: envDuration("RESTARTABLE_TEST_STOP_TIMEOUT", 250*time.Millisecond),
		TermGrace:          envDuration("RESTARTABLE_TEST_TERM_GRACE", 60*time.Millisecond),
		IDEnv:              os.Getenv("RESTARTABLE_TEST_ID_ENV"),
		ControlPath:        os.Getenv("RESTARTABLE_TEST_SOCKET"),
		Stdin:              strings.NewReader(""), Stdout: os.Stdout, Stderr: os.Stderr, Headless: true,
		Presenter: presenter,
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

type testRunner struct {
	cmd     *exec.Cmd
	path    string
	stdout  string
	stderr  string
	done    chan struct{}
	waitErr error
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
	dir, err := os.MkdirTemp("/tmp", "rnt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "runner.sock")
	stdout := filepath.Join(t.TempDir(), "stdout.log")
	stderr := filepath.Join(t.TempDir(), "stderr.log")
	var out, pipeReader *os.File
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

func shellQuote(s string) string  { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func readFile(path string) string { data, _ := os.ReadFile(path); return string(data) }

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
	dir, err := os.MkdirTemp("/tmp", "rkey-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
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
		if got.Model.State != Stopping || got.Model.Confirm != ConfirmNone || got.Model.Message != "終了待ち (Esc で取り消し / Ctrl-C で強制終了)" {
			t.Fatalf("key %q changed stopping model: %+v", key, got.Model)
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
	keyDir, err := os.MkdirTemp("/tmp", "rkey-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(keyDir) })
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
		return got.Model.State == Stopping && got.Model.Message == "child exited; waiting for stop result"
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
	proc, err := startProcess([]string{"/bin/sleep", "30"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), true)
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
	proc, err := startProcess([]string{"/bin/sleep", "30"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), true)
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
	build := fmt.Sprintf(`n=0; [ ! -f %s ] || n=$(cat %s); n=$((n+1)); echo "$n" > %s`, shellQuote(count), shellQuote(count), shellQuote(count))
	r := startTestRunner(t, map[string]string{"RESTARTABLE_TEST_BUILD": build, "RESTARTABLE_TEST_RUN": "exec /bin/sleep 30"})
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
	waitFor(t, time.Second, "restart commit", func() bool {
		status, err := r.status()
		return err == nil && status.State == string(Stopping) && status.RestartPending
	})
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "committed restart despite disconnect", func() bool {
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

func TestFinishedMarkerRejectsRestartBeforeExitEventIsConsumed(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rfin-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
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
	request := <-server.Requests
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
	for n := 0; n < 2100; n++ {
		fmt.Fprintf(&input, "middle-%d\n", n)
	}
	input.WriteString("tail\n")
	sink.CopyFrom(strings.NewReader(input.String()), false)
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
	dir, err := os.MkdirTemp("/tmp", "rbuild-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
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
	request := <-server.Requests
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
	proc, err := startProcess([]string{"/bin/sh", "-c", "exit 23"}, false, os.Environ(), strings.NewReader(""), newLogSink(io.Discard, true), true)
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
	keyDir, err := os.MkdirTemp("/tmp", "rkey-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(keyDir) })
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
		"RESTARTABLE_TEST_TERM_GRACE": "1h",
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

func TestGroupStopEscalatesWhenLeaderExitsBeforeTermGrace(t *testing.T) {
	grandchildFile := filepath.Join(t.TempDir(), "grandchild.pid")
	run := fmt.Sprintf(`(trap '' TERM; while :; do /bin/sleep 1; done) & echo $! > %s; wait`, shellQuote(grandchildFile))
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_RUN":        run,
		"RESTARTABLE_TEST_TERM_GRACE": "1h",
	})
	initial := r.waitStatus(t, string(Running))
	if initial.PID == nil {
		t.Fatal("runner has no child PID")
	}
	waitFor(t, time.Second, "grandchild PID", func() bool { return strings.TrimSpace(readFile(grandchildFile)) != "" })
	t.Cleanup(func() { _ = signalGroup(*initial.PID, syscall.SIGKILL) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatalf("restart did not proceed after group leader exited: %v", err)
	}
	if !response.OK || response.Generation != 2 {
		t.Fatalf("restart response = %+v", response)
	}
	waitFor(t, time.Second, "grandchild group cleanup after leader exit", func() bool { return !groupExists(*initial.PID) })
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestQuitDuringBuildRejectsControlRestartWhileForceKillRuns(t *testing.T) {
	dir := t.TempDir()
	keyDir, err := os.MkdirTemp("/tmp", "rkey-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(keyDir) })
	keyPath := filepath.Join(keyDir, "keys.sock")
	statePath := filepath.Join(dir, "presenter.json")
	r := startTestRunner(t, map[string]string{
		"RESTARTABLE_TEST_BUILD":           `trap '' TERM; while :; do /bin/sleep 1; done`,
		"RESTARTABLE_TEST_RUN":             "exec /bin/sleep 30",
		"RESTARTABLE_TEST_TERM_GRACE":      "2s",
		"RESTARTABLE_TEST_KEY_SOCKET":      keyPath,
		"RESTARTABLE_TEST_PRESENTER_STATE": statePath,
	})
	waitPresenterSnapshot(t, statePath, "initial building state", func(got presenterSnapshot) bool { return got.Model.State == Building })
	type callResult struct {
		response control.Response
		err      error
	}
	pendingResult := make(chan callResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		response, err := control.Call(ctx, r.path, control.Restart)
		pendingResult <- callResult{response: response, err: err}
	}()
	waitFor(t, time.Second, "restart request queued during build", func() bool {
		status, err := r.status()
		return err == nil && status.RestartPending
	})
	sendKeyAndWaitRender(t, keyPath, statePath, "Q")
	confirmed := sendKeyAndWaitRender(t, keyPath, statePath, "y")
	if confirmed.Model.State != Exiting {
		confirmed = waitPresenterSnapshot(t, statePath, "quit confirmed while build is being killed", func(got presenterSnapshot) bool {
			return got.Model.State == Exiting || (got.Model.State == Stopping && got.Model.Intent == IntentExit)
		})
	}
	if confirmed.Model.State != Exiting && (confirmed.Model.State != Stopping || confirmed.Model.Intent != IntentExit) {
		t.Fatalf("unexpected state after build quit confirmation: %+v", confirmed.Model)
	}
	select {
	case result := <-pendingResult:
		if result.err != nil {
			t.Fatalf("pending restart after quit confirmation: %v", result.err)
		}
		if result.response.OK || result.response.Reason != "runner exiting" {
			t.Fatalf("pending restart after quit confirmation = %+v", result.response)
		}
	case <-time.After(time.Second):
		t.Fatal("pending build restart was not rejected after quit confirmation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	response, err := control.Call(ctx, r.path, control.Restart)
	if err != nil {
		t.Fatalf("control restart during quit kill: %v", err)
	}
	if response.OK || response.Reason != "runner exiting" {
		t.Fatalf("control restart during quit kill = %+v", response)
	}
	if confirmed.Model.State != Exiting {
		t.Fatalf("quit during build remained stoppable: %+v", confirmed.Model)
	}
	if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
		t.Fatalf("runner exit code = %d, want 143", code)
	}
}

func TestRunnerSignalHandlingAndSIGPIPEIgnore(t *testing.T) {
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

	t.Run("broken-stdout", func(t *testing.T) {
		r := startTestRunnerWithBrokenStdout(t, map[string]string{
			"RESTARTABLE_TEST_RUN": `printf 'output to a closed pipe\n'; exec /bin/sleep 30`,
		})
		status := r.waitStatus(t, string(Running))
		if status.PID == nil {
			t.Fatal("runner has no child PID after stdout broke")
		}
		if code := r.signalAndWait(t, syscall.SIGTERM); code != 143 {
			t.Fatalf("runner exit code = %d, want 143", code)
		}
	})
}
