package main

import "sync"

// dispatchQueue is the ordered list of jobs that have been accepted but not yet
// handed to a worker. Extracted from Runner (a 30+ field concurrency
// orchestrator) so the two invariants below live in one place and race
// reasoning about them is local — every mutation of the list goes through this
// type's mutex, instead of 12 lock sites scattered through runner.go.
//
// Invariants owned here:
//  1. commit-by-index: peek returns the head WITHOUT removing it, and commit
//     removes by index (not position). A concurrent pushFront may insert a new
//     head between the two, so position would remove the wrong job. Keeping the
//     item in the list until it is actually sent to a worker is what makes
//     snapshot() (the TUI's queue view) show it as pending the whole time.
//  2. index monotonicity: nextIndex only ever grows, so indexes stay unique for
//     the lifetime of the run even as items are removed.
//
// Deliberately NOT owned here (keeping the boundary at "queue operations"):
//   - the dedup set (Runner.queued) and the -F input file append: those are
//     admission-control concerns, and Runner sequences them around push
//   - the "send to worker" select and the stop context: the dispatcher stays in
//     Runner, so this type does not touch the worker pool. peek takes the stop
//     signal as a channel argument (same shape as pauseGate.waitUntilResumed)
//     rather than holding a context.
type dispatchQueue struct {
	mu        sync.Mutex
	items     []runnerJob
	nextIndex int // monotonic job index (guarded by mu)
	// live mirrors Runner's "live mode": when true, peek keeps blocking after
	// the list drains (waiting for Enqueue) instead of reporting "done".
	// Written only by setLive, whose contract is "before Start" (see SetLive).
	live bool
	// wake (buffered 1) nudges a blocked peek whenever the list or the stop
	// state changed. Buffered so nudge() never blocks and one pending wake is
	// enough for the blocked peek to re-loop.
	wake chan struct{}
}

func newDispatchQueue() *dispatchQueue {
	return &dispatchQueue{wake: make(chan struct{}, 1)}
}

// setLive selects blocking (live) vs draining behaviour for peek. Must be
// called before the dispatcher starts (Runner.SetLive enforces that contract).
func (q *dispatchQueue) setLive(live bool) {
	q.mu.Lock()
	q.live = live
	q.mu.Unlock()
}

// isLive reports the current mode (used by Runner to reject Enqueue when the
// runner is not live).
func (q *dispatchQueue) isLive() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.live
}

// seed appends the original input list, assigning each line an index. Used once
// from Start before the dispatcher goroutine exists.
func (q *dispatchQueue) seed(lines []string) {
	q.mu.Lock()
	for _, line := range lines {
		q.nextIndex++
		q.items = append(q.items, runnerJob{index: q.nextIndex, line: line})
	}
	q.mu.Unlock()
}

// push appends (front=false) or prepends (front=true) a line with a fresh
// index and wakes a blocked peek.
func (q *dispatchQueue) push(line string, front bool) {
	q.mu.Lock()
	q.nextIndex++
	j := runnerJob{index: q.nextIndex, line: line}
	if front {
		q.items = append([]runnerJob{j}, q.items...)
	} else {
		q.items = append(q.items, j)
	}
	q.mu.Unlock()
	q.nudge()
}

// peek blocks until the list has an item, or until it should stop waiting:
// done fires (stop requested), or the list is empty and the queue is not live.
// Returns the head WITHOUT removing it; the caller must call commit(j.index)
// once the job has actually been handed to a worker (invariant 1).
func (q *dispatchQueue) peek(done <-chan struct{}) (runnerJob, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			j := q.items[0]
			q.mu.Unlock()
			return j, true
		}
		drained := !q.live
		q.mu.Unlock()
		if drained {
			return runnerJob{}, false
		}
		select {
		case <-q.wake:
		case <-done:
			return runnerJob{}, false
		}
	}
}

// commit removes the job with the given index. A no-op for an index that is no
// longer present (already removed by removeLine, or committed twice).
func (q *dispatchQueue) commit(idx int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.items {
		if j.index == idx {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return
		}
	}
}

// removeLine drops the first pending job with the given line and reports
// whether one was found. Used by RemovePending (the TUI's 'd' on a queued row);
// the caller clears the dedup entry so the line can be re-enqueued.
func (q *dispatchQueue) removeLine(line string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.items {
		if j.line == line {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return true
		}
	}
	return false
}

// snapshot copies the pending lines in dispatch order (the TUI's queue view).
func (q *dispatchQueue) snapshot() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, len(q.items))
	for i, j := range q.items {
		out[i] = j.line
	}
	return out
}

// length is the number of not-yet-dispatched items.
func (q *dispatchQueue) length() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// nudge wakes a blocked peek so it re-evaluates (used on stop / kill, where the
// list did not change but the caller must stop waiting). Non-blocking.
func (q *dispatchQueue) nudge() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
