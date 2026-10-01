package runner

import (
	"io"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type brokenOutputWriter struct{ writes int }

func (w *brokenOutputWriter) Write(data []byte) (int, error) {
	w.writes++
	return 0, syscall.EPIPE
}

type countedChunkReader struct {
	left  int
	reads int
}

func (r *countedChunkReader) Read(data []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(len(data), 512, r.left)
	for i := 0; i < n; i++ {
		data[i] = 'x'
	}
	r.left -= n
	r.reads++
	return n, nil
}

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

func TestHeadlessOutputWriteErrorsAreDiscardedWithoutStoppingSupervision(t *testing.T) {
	writer := &brokenOutputWriter{}
	reader := &countedChunkReader{left: 2048}
	sink := newLogSink(writer, true)
	sink.CopyFrom(reader, true)
	if reader.left != 0 || reader.reads != 4 {
		t.Fatalf("copy stopped after output error: bytes left=%d read calls=%d", reader.left, reader.reads)
	}
	if writer.writes != 4 {
		t.Fatalf("writes = %d, want one attempt per input chunk", writer.writes)
	}
}

func TestTTYOutputPreservesEmptyLines(t *testing.T) {
	var output strings.Builder
	sink := newLogSink(&output, false)
	sink.CopyFrom(strings.NewReader("a\n\nb"), false)
	sink.Flush()
	if got, want := output.String(), "a\n\nb\n"; got != want {
		t.Fatalf("TTY output = %q, want %q", got, want)
	}
}
