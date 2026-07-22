package main

import (
	"testing"
	"time"
)

func TestPauseGateBasic(t *testing.T) {
	g := newPauseGate()
	if g.isPaused() {
		t.Fatal("new gate should not be paused")
	}
	g.pause()
	if !g.isPaused() {
		t.Fatal("pause() should set paused")
	}
	g.pause() // idempotent
	if !g.isPaused() {
		t.Fatal("double pause() should stay paused")
	}
	g.resume()
	if g.isPaused() {
		t.Fatal("resume() should clear paused")
	}
}

func TestPauseGateWaitReturnsTrueWhenNotPaused(t *testing.T) {
	g := newPauseGate()
	done := make(chan struct{})
	if !g.waitUntilResumed(done) {
		t.Fatal("waitUntilResumed should return true (continue) when not paused")
	}
}

func TestPauseGateWaitUnblocksOnResume(t *testing.T) {
	g := newPauseGate()
	g.pause()
	done := make(chan struct{})
	result := make(chan bool, 1)
	go func() { result <- g.waitUntilResumed(done) }()

	select {
	case <-result:
		t.Fatal("waitUntilResumed returned while still paused")
	case <-time.After(20 * time.Millisecond):
	}

	g.resume()
	select {
	case cont := <-result:
		if !cont {
			t.Fatal("want true (continue) after resume")
		}
	case <-time.After(time.Second):
		t.Fatal("waitUntilResumed did not return after resume")
	}
}

func TestPauseGateWaitExitsWhenDoneFires(t *testing.T) {
	g := newPauseGate()
	g.pause()
	done := make(chan struct{})
	result := make(chan bool, 1)
	go func() { result <- g.waitUntilResumed(done) }()

	select {
	case <-result:
		t.Fatal("returned while paused and done still open")
	case <-time.After(20 * time.Millisecond):
	}

	close(done) // stop signal fires while paused
	select {
	case cont := <-result:
		if cont {
			t.Fatal("want false (exit) when done fires while paused")
		}
	case <-time.After(time.Second):
		t.Fatal("waitUntilResumed did not return after done closed")
	}
}

// wake() must never block, even with no waiter and repeated calls (buffered 1).
func TestPauseGateWakeNonBlocking(t *testing.T) {
	g := newPauseGate()
	done := make(chan struct{})
	go func() {
		for range 10 {
			g.wake()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wake() blocked")
	}
}
