package main

import "sync"

// pauseGate is a reversible dispatch gate. When paused, the dispatcher blocks in
// waitUntilResumed until resume() is called or the supplied done channel fires.
// Extracted from Runner (which is a 32-field concurrency orchestrator) so the
// pause state machine is a self-contained, unit-testable primitive: it touches
// none of Runner's other state. The stop signal is injected as a channel to
// waitUntilResumed rather than held here, keeping this type free of stopCtx.
type pauseGate struct {
	mu     sync.Mutex
	paused bool
	// ch (buffered 1) wakes a blocked waitUntilResumed whenever the paused
	// state changes or the caller wants to re-evaluate (e.g. on stop). Buffered
	// so wake() never blocks and a single pending wake is enough to re-loop.
	ch chan struct{}
}

func newPauseGate() *pauseGate {
	return &pauseGate{ch: make(chan struct{}, 1)}
}

// pause blocks further dispatching. Safe to call many times.
func (g *pauseGate) pause() {
	g.mu.Lock()
	g.paused = true
	g.mu.Unlock()
	g.wake()
}

// resume lifts a pause. No-op if not paused.
func (g *pauseGate) resume() {
	g.mu.Lock()
	g.paused = false
	g.mu.Unlock()
	g.wake()
}

// isPaused reports the current paused state.
func (g *pauseGate) isPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// wake nudges a blocked waitUntilResumed to re-check state. Non-blocking.
func (g *pauseGate) wake() {
	select {
	case g.ch <- struct{}{}:
	default:
	}
}

// waitUntilResumed blocks until pause is lifted or done fires. Returns true if
// the caller should continue dispatching, false if it should exit (done fired
// while paused). done is typically stopCtx.Done(); a closed done makes the
// select's done case win, so this returns false promptly even if wake() also
// raced (the loop re-checks isPaused and converges on done).
func (g *pauseGate) waitUntilResumed(done <-chan struct{}) bool {
	for g.isPaused() {
		select {
		case <-done:
			return false
		case <-g.ch:
		}
	}
	return true
}
