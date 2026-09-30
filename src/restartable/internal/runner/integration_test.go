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

// TestRunnerHelper is re-executed as a separate process by integration tests.
func TestRunnerHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_TEST_HELPER") != "1" {
		return
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

func startTestRunner(t *testing.T, options map[string]string) *testRunner {
	return startTestRunnerMode(t, options, true)
}

func startTestRunnerMode(t *testing.T, options map[string]string, waitForStatus bool) *testRunner {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rnt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "runner.sock")
	stdout := filepath.Join(t.TempDir(), "stdout.log")
	stderr := filepath.Join(t.TempDir(), "stderr.log")
	out, err := os.Create(stdout)
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
	child := &process{pid: 987654, done: make(chan processResult), outputEnd: make(chan struct{})}
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
