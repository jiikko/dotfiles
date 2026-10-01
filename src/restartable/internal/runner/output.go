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

// OutputBuffer keeps the first line and the newest tail while bounding memory.
// The omission marker is inserted between them when Drain is called.
type OutputBuffer struct {
	mu        sync.Mutex
	maxLines  int
	maxBytes  int
	lines     []string
	bytes     int
	dropped   uint64
	firstSeen bool
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
	if !b.firstSeen {
		b.lines = append(b.lines, line)
		b.bytes += len(line)
		b.firstSeen = true
	} else {
		b.lines = append(b.lines, line)
		b.bytes += len(line)
	}
	for len(b.lines) > b.maxLines || b.bytes > b.maxBytes {
		if len(b.lines) <= 1 { // the initial line is retained, even if it is long
			break
		}
		// Preserve the first line and discard the oldest line after it.
		b.bytes -= len(b.lines[1])
		copy(b.lines[1:], b.lines[2:])
		b.lines = b.lines[:len(b.lines)-1]
		b.dropped++
	}
}

func (b *OutputBuffer) Drain() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := make([]string, 0, len(b.lines)+1)
	if len(b.lines) == 0 {
		return lines
	}
	if b.dropped > 0 && len(b.lines) > 1 {
		lines = append(lines, b.lines[0], omissionLine(b.dropped))
		lines = append(lines, b.lines[1:]...)
	} else {
		lines = append(lines, b.lines...)
	}
	b.lines = nil
	b.bytes = 0
	b.dropped = 0
	b.firstSeen = false
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

type logSink struct {
	writer    io.Writer
	headless  bool
	buffer    *OutputBuffer
	printLine func(string)
	mu        sync.Mutex
}

func newLogSink(w io.Writer, headless bool, printLines ...func(string)) *logSink {
	var printLine func(string)
	if len(printLines) > 0 {
		printLine = printLines[0]
	}
	return &logSink{writer: w, headless: headless, buffer: NewOutputBuffer(defaultOutputLines, defaultOutputBytes), printLine: printLine}
}

// CopyFrom preserves bytes and ordering in headless mode. TTY logs are
// normalized and sanitized before they reach the terminal.
func (s *logSink) CopyFrom(r io.Reader, headless bool) {
	if headless || s.headless {
		_, _ = io.Copy(synchronizedWriter{s: s}, r)
		return
	}
	var current []byte
	previousWasCR := false
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			if c == '\r' {
				s.addSafeLine(string(current))
				current = current[:0]
				previousWasCR = true
				continue
			}
			if c == '\n' {
				if !previousWasCR {
					s.addSafeLine(string(current))
					current = current[:0]
				}
				previousWasCR = false
				continue
			}
			previousWasCR = false
			current = append(current, c)
			if len(current) >= maxOutputLineBytes {
				s.addSafeLine(string(current))
				current = current[:0]
			}
		}
		if err != nil {
			if len(current) > 0 {
				s.addSafeLine(string(current))
			}
			return
		}
	}
}

func (s *logSink) addSafeLine(line string) {
	line = termsafe.DetailLine(line)
	if strings.Contains(line, "\x1b[") {
		line = "\x1b[0m" + line + "\x1b[0m"
	}
	s.buffer.AddLine(line)
}

type synchronizedWriter struct{ s *logSink }

func (w synchronizedWriter) Write(data []byte) (int, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	_, _ = w.s.writer.Write(data)
	return len(data), nil
}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range lines {
		_, _ = io.WriteString(s.writer, line+"\n")
	}
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
