package main

import "time"

type EventKind int

const (
	EventStart EventKind = iota
	EventEnd
)

type Event struct {
	Kind        EventKind
	SlotID      int
	JobIndex    int
	Total       int
	Line        string
	Started     time.Time
	Ended       time.Time
	ExitCode    int
	LogPath     string
	Err         error
	Attempt     int // 1-based; 1 = initial run, 2+ = retries
	MaxAttempts int // initial + retries
	TimedOut    bool
}
