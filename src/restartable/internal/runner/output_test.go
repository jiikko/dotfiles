package runner

import (
	"io"
	"reflect"
	"testing"
	"time"
)

func TestTTYFlusherBatchesUntilInjectedTickAndPrintsDropNotice(t *testing.T) {
	printed := make(chan string, 8)
	sink := newLogSink(io.Discard, false, func(line string) { printed <- line })
	sink.buffer = NewOutputBuffer(3, 1024)
	done := make(chan struct{})
	ticks := make(chan time.Time)
	flusherDone := make(chan struct{})
	go func() {
		defer close(flusherDone)
		sink.runFlusherTicks(done, ticks)
	}()

	for _, line := range []string{"line 1", "line 2", "line 3", "line 4", "line 5"} {
		sink.addSafeLine(line)
	}
	select {
	case line := <-printed:
		t.Fatalf("printed before injected tick: %q", line)
	default:
	}
	ticks <- time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	want := []string{
		"line 1",
		"… (restartable: 2 行を表示せずに落とした)",
		"line 4",
		"line 5",
	}
	got := make([]string, 0, len(want))
	for range want {
		select {
		case line := <-printed:
			got = append(got, line)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for injected flush; got %q", got)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Println lines = %q, want %q", got, want)
	}
	close(done)
	select {
	case <-flusherDone:
	case <-time.After(2 * time.Second):
		t.Fatal("flusher did not stop after done was closed")
	}
}
