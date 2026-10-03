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
	for i := range n {
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

func TestHeadlessOutputFailureWarnsOnceAndKeepsReading(t *testing.T) {
	writer := &brokenOutputWriter{}
	reader := &countedChunkReader{left: 2048}
	sink := newLogSink(writer, true)
	var stderr strings.Builder
	sink.onOutputFailure = func() { reportOutputFailure(&stderr) }
	sink.CopyFrom(reader)
	if reader.left != 0 || reader.reads != 4 {
		t.Fatalf("copy stopped after output error: bytes left=%d read calls=%d", reader.left, reader.reads)
	}
	if writer.writes != 1 {
		t.Fatalf("writes after first output error = %d, want one", writer.writes)
	}
	if got, want := stderr.String(), OutputFailureMessage+"\n"; got != want {
		t.Fatalf("broken-output notice = %q, want %q", got, want)
	}
	extra := &countedChunkReader{left: 512}
	sink.CopyFrom(extra)
	if extra.left != 0 || extra.reads != 1 || writer.writes != 1 {
		t.Fatalf("later output was not drained and discarded: left=%d reads=%d writes=%d", extra.left, extra.reads, writer.writes)
	}
}

func TestTTYOutputPreservesEmptyLines(t *testing.T) {
	var output strings.Builder
	sink := newLogSink(&output, false)
	sink.CopyFrom(strings.NewReader("a\n\nb"))
	sink.Flush()
	if got, want := output.String(), "a\n\nb\n"; got != want {
		t.Fatalf("TTY output = %q, want %q", got, want)
	}
}

func TestOutputBufferByteBoundKeepsLongFirstLineAndIsReusableAfterDrain(t *testing.T) {
	buffer := NewOutputBuffer(100, 10)
	for _, line := range []string{"first-is-long", "aaaa", "bbbb", "cc"} {
		buffer.AddLine(line)
	}
	// 先頭行だけで 13 byte (上限 10) でも先頭行は残し、後ろの行はすべて落とす。最後の行も上限を超えるので落ちる。
	if got, want := buffer.Drain(), []string{"first-is-long"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Drain() with long first line = %q, want %q", got, want)
	}
	for _, line := range []string{"x", "1234", "5678", "90"} {
		buffer.AddLine(line)
	}
	// 1+4+4+2 = 11 > 10 なので "1234" を落とす。Drain の後は先頭行と落とした数を新しく数え直す。
	if got, want := buffer.Drain(), []string{"x", omissionLine(1), "5678", "90"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Drain() after reuse = %q, want %q", got, want)
	}
	if got := buffer.Drain(); len(got) != 0 {
		t.Fatalf("Drain() of an empty buffer = %q, want none", got)
	}
}

// 上限に達した後の 1 行あたりの費用 (落とす行ごとに残りを詰め直すと、上限の行数に比例する)。
func BenchmarkOutputBufferAddLineAtCapacity(b *testing.B) {
	buffer := NewOutputBuffer(defaultOutputLines, defaultOutputBytes)
	for range defaultOutputLines {
		buffer.AddLine("warm-up line")
	}
	b.ResetTimer()
	for range b.N {
		buffer.AddLine("a log line from a burst")
	}
}

func TestTTYOutputIgnoresCarriageReturnWithoutCurrentText(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "\n\r", want: "\n"},
		{input: "\r\r\n", want: ""},
		{input: "spin\rspin\r\n", want: "spin\nspin\n"},
	} {
		var output strings.Builder
		sink := newLogSink(&output, false)
		sink.CopyFrom(strings.NewReader(test.input))
		sink.Flush()
		if got := output.String(); got != test.want {
			t.Errorf("TTY output for %q = %q, want %q", test.input, got, test.want)
		}
	}
}
