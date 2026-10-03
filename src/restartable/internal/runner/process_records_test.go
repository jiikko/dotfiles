package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func startTrackedTestProcess(t *testing.T, script string) *process {
	t.Helper()
	proc, err := startProcess([]string{script}, true, nil, strings.NewReader(""), newLogSink(io.Discard, true), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = signalGroup(proc.pid, syscall.SIGKILL)
		<-proc.done
	})
	return proc
}

// startSettledProcess returns a record that forced shutdown and output drain
// can no longer act on: reaped leader, closed output, empty group.
func startSettledProcess(t *testing.T) *process {
	t.Helper()
	proc := startTrackedTestProcess(t, "exit 0")
	<-proc.done
	<-proc.outputEnd
	waitFor(t, 5*time.Second, "settled process group to disappear", func() bool { return !groupExists(proc.pid) })
	return proc
}

func TestTrackProcessKeepsRecordCountBoundedAcrossManyFinishedProbes(t *testing.T) {
	a := &actor{}
	for i := range 50 {
		a.trackProcess(startSettledProcess(t))
		if got := len(a.allProcesses); got != 1 {
			t.Fatalf("after %d finished processes, allProcesses has %d records, want 1", i+1, got)
		}
	}
}

func TestReadyProbeLoopDoesNotAccumulateProcessRecords(t *testing.T) {
	const probes = 60
	a := &actor{
		cfg: Config{
			ReadyCommand: "exit 1", ReadyInterval: time.Millisecond, ReadyTimeout: time.Hour,
			ReadyAttemptTimeout: time.Hour, ReadyCleanupGrace: 10 * time.Millisecond,
			Stdin: strings.NewReader(""), Stderr: io.Discard,
		},
		model: Model{State: Running, Generation: 1, Transition: Transition{Active: true, Stage: TransitionReady}},
		sink:  newLogSink(io.Discard, true), presenter: headlessPresenter{}, events: make(chan actorEvent, 16),
	}
	t.Cleanup(func() {
		a.stopReadyTimers()
		for _, proc := range a.allProcesses {
			_ = signalGroup(proc.pid, syscall.SIGKILL)
			<-proc.done
		}
	})
	a.startReadyCheck()
	maxRecords := 0
	for done := 0; done < probes; {
		select {
		case ev := <-a.events:
			if ev.kind == readyProbeDoneEvent {
				done++
			}
			a.handleEvent(ev)
		case <-time.After(10 * time.Second):
			t.Fatalf("ready probe loop stalled after %d probes", done)
		}
		maxRecords = max(maxRecords, len(a.allProcesses))
	}
	t.Logf("max process records over %d probes: %d", probes, maxRecords)
	// Without pruning the count equals the number of probes; settled probes
	// leave at most a few records whose output copy is still finishing.
	if maxRecords > probes/6 {
		t.Fatalf("ready probe loop kept up to %d process records over %d probes", maxRecords, probes)
	}
}

func TestTrackProcessKeepsReapedLeaderWhoseGroupIsAliveForForcedShutdown(t *testing.T) {
	descendant := `(trap '' TERM; while :; do /bin/sleep 1; done) >/dev/null 2>&1 & exit 0`
	live := startTrackedTestProcess(t, descendant)
	<-live.done
	<-live.outputEnd
	if !groupExists(live.pid) {
		t.Fatal("fixture: descendant group exited before tracking")
	}
	a := &actor{cfg: Config{TermGrace: 50 * time.Millisecond}, events: make(chan actorEvent, 1)}
	a.trackProcess(live)
	for range 20 {
		a.trackProcess(startSettledProcess(t))
	}
	if !slices.Contains(a.allProcesses, live) {
		t.Fatal("record of a reaped leader whose process group is alive was dropped")
	}
	a.beginForceStop()
	select {
	case ev := <-a.events:
		if ev.kind != forceDoneEvent || !ev.forced {
			t.Fatalf("unexpected event after forced stop: %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("forced stop did not finish")
	}
	// SIGKILLed orphans disappear once launchd reaps them, after forceDone.
	waitFor(t, 5*time.Second, "forced shutdown to kill the descendant process group", func() bool { return !groupExists(live.pid) })
}

func TestTrackProcessKeepsRecordWhoseOutputIsStillHeld(t *testing.T) {
	if _, err := os.Stat("/usr/bin/perl"); err != nil {
		t.Fatalf("fixture needs /usr/bin/perl to move a descendant out of the group: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "escaped.pid")
	// The descendant leaves the group but keeps the shared output pipe open.
	script := fmt.Sprintf(`/usr/bin/perl -e 'setpgrp(0,0); sleep 30' & echo $! > %s; exit 0`, shellQuote(pidFile))
	held := startTrackedTestProcess(t, script)
	<-held.done
	waitFor(t, 5*time.Second, "escaped descendant PID", func() bool { return strings.TrimSpace(readFile(pidFile)) != "" })
	escaped, err := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = signalGroup(escaped, syscall.SIGKILL) })
	waitFor(t, 5*time.Second, "leader group to empty after the descendant left it", func() bool { return !groupExists(held.pid) })
	select {
	case <-held.outputEnd:
		t.Fatal("fixture: output ended although the escaped descendant holds the pipe")
	default:
	}
	a := &actor{cfg: Config{Stderr: io.Discard}}
	a.trackProcess(held)
	for range 5 {
		a.trackProcess(startSettledProcess(t))
	}
	if !slices.Contains(a.allProcesses, held) {
		t.Fatal("record whose output pipe is still held was dropped before output drain")
	}
	a.drainOutputs()
	select {
	case <-held.outputEnd:
	default:
		t.Fatal("output drain did not close the held output")
	}
}
