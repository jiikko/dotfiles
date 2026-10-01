package runner

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
)

const (
	defaultOutputLines = 2000
	defaultOutputBytes = 1 << 20
	maxOutputLineBytes = 64 << 10
)

const OutputFailureMessage = "出力先に書けなくなったので以後のログを捨てる"

// OutputBuffer keeps the first line and the newest tail while bounding memory.
// The omission marker is inserted between them when Drain is called.
type OutputBuffer struct {
	mu       sync.Mutex
	maxLines int
	maxBytes int
	first    string
	hasFirst bool
	// tail は先頭行より後の新しい行。古い行は先頭を切り落として捨てる (詰め直すと上限の行数に比例する)。
	tail    []string
	bytes   int
	dropped uint64
}

func NewOutputBuffer(maxLines, maxBytes int) *OutputBuffer {
	if maxLines < 2 {
		maxLines = 2
	}
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &OutputBuffer{maxLines: maxLines, maxBytes: maxBytes}
}

func (b *OutputBuffer) AddLine(line string) {
	line = strings.TrimSuffix(line, "\n")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bytes += len(line)
	if !b.hasFirst {
		b.first, b.hasFirst = line, true
	} else {
		b.tail = append(b.tail, line)
	}
	// The first line is retained even if it alone exceeds the byte bound.
	for len(b.tail) > 0 && (1+len(b.tail) > b.maxLines || b.bytes > b.maxBytes) {
		b.bytes -= len(b.tail[0])
		b.tail[0] = "" // release the string; the backing array keeps the slot until append reallocates
		b.tail = b.tail[1:]
		b.dropped++
	}
}

func (b *OutputBuffer) Drain() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := make([]string, 0, len(b.tail)+2)
	if !b.hasFirst {
		return lines
	}
	lines = append(lines, b.first)
	if b.dropped > 0 && len(b.tail) > 0 {
		lines = append(lines, omissionLine(b.dropped))
	}
	lines = append(lines, b.tail...)
	b.first, b.hasFirst = "", false
	b.tail = nil
	b.bytes = 0
	b.dropped = 0
	return lines
}

func omissionLine(n uint64) string {
	return "… (restartable: " + itoa(n) + " 行を表示せずに落とした)"
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// FailOnceWriter writes to the wrapped writer until the first failed or short
// write, then calls onFailure once and discards every later write. Write always
// reports success so a copier (io.Copy, Bubble Tea's renderer) keeps draining
// its source instead of blocking the child on a full pipe.
type FailOnceWriter struct {
	writer    io.Writer
	onFailure func()
	mu        sync.Mutex
	failed    bool
}

func NewFailOnceWriter(w io.Writer, onFailure func()) *FailOnceWriter {
	return &FailOnceWriter{writer: w, onFailure: onFailure}
}

func (w *FailOnceWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	if w.failed {
		w.mu.Unlock()
		return len(data), nil
	}
	n, err := w.writer.Write(data)
	if err == nil && n == len(data) {
		w.mu.Unlock()
		return len(data), nil
	}
	w.failed = true
	w.mu.Unlock()
	if w.onFailure != nil {
		w.onFailure()
	}
	return len(data), nil
}

// Writer returns the wrapped writer (for capabilities such as Fd).
func (w *FailOnceWriter) Writer() io.Writer { return w.writer }

type logSink struct {
	output   *FailOnceWriter
	headless bool
	// terminalOutput は stdout が端末か。UI が無くても端末へ出すなら無害化する (CopyFrom)。
	terminalOutput  bool
	buffer          *OutputBuffer
	printLine       func(string)
	onOutputFailure func()
}

func newLogSink(w io.Writer, headless bool, printLines ...func(string)) *logSink {
	var printLine func(string)
	if len(printLines) > 0 {
		printLine = printLines[0]
	}
	s := &logSink{headless: headless, buffer: NewOutputBuffer(defaultOutputLines, defaultOutputBytes), printLine: printLine}
	s.output = NewFailOnceWriter(w, func() {
		if s.onOutputFailure != nil {
			s.onOutputFailure()
		}
	})
	return s
}

// CopyFrom is the one place that decides whether child output is sanitized.
// Only output bound for a non-terminal (pipe / file) without the UI is copied
// byte for byte. Anything that reaches a terminal is split into lines and
// sanitized the same way: buffered for the TTY UI, written at once otherwise.
func (s *logSink) CopyFrom(r io.Reader, headless bool) {
	headless = headless || s.headless
	if headless && !s.terminalOutput {
		_, _ = io.Copy(s.output, r)
		return
	}
	emit := s.addSafeLine
	if headless {
		emit = func(line string) { s.writeOutput([]byte(safeLine(line) + "\n")) }
	}
	var current []byte
	previousWasCR := false
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			if c == '\r' {
				// A CR completes a frame only when the current line has text.
				// Leading or repeated CRs rewrite an empty frame and must not
				// introduce blank lines; CRLF still collapses to one boundary.
				if len(current) > 0 {
					emit(string(current))
					current = current[:0]
				}
				previousWasCR = true
				continue
			}
			if c == '\n' {
				if !previousWasCR {
					emit(string(current))
					current = current[:0]
				}
				previousWasCR = false
				continue
			}
			previousWasCR = false
			current = append(current, c)
			if len(current) >= maxOutputLineBytes {
				emit(string(current))
				current = current[:0]
			}
		}
		if err != nil {
			if len(current) > 0 {
				emit(string(current))
			}
			return
		}
	}
}

// safeLine strips terminal control sequences except SGR, and resets SGR around
// a line that sets it so a child's colour cannot leak into later lines.
func safeLine(line string) string {
	line = termsafe.DetailLine(line)
	if strings.Contains(line, "\x1b[") {
		line = "\x1b[0m" + line + "\x1b[0m"
	}
	return line
}

func (s *logSink) addSafeLine(line string) { s.buffer.AddLine(safeLine(line)) }

func (s *logSink) writeOutput(data []byte) { _, _ = s.output.Write(data) }

func (s *logSink) Flush() {
	lines := s.buffer.Drain()
	if len(lines) == 0 {
		return
	}
	if !s.headless && s.printLine != nil {
		for _, line := range lines {
			s.printLine(line)
		}
		return
	}
	for _, line := range lines {
		s.writeOutput([]byte(line + "\n"))
	}
}

func reportOutputFailure(stderr io.Writer) {
	if stderr == nil {
		stderr = io.Discard
	}
	_, _ = io.WriteString(stderr, OutputFailureMessage+"\n")
}

func (s *logSink) runFlusher(done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	s.runFlusherTicks(done, ticker.C)
}

// runFlusherTicks takes ticks from a clock seam so batching can be tested
// without sleeping for wall-clock time.
func (s *logSink) runFlusherTicks(done <-chan struct{}, ticks <-chan time.Time) {
	for {
		select {
		case <-ticks:
			s.Flush()
		case <-done:
			s.Flush()
			return
		}
	}
}

func (s *logSink) hasPrinter() bool { return !s.headless && s.printLine != nil }
