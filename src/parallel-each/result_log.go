package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// resultLogWriter is a thread-safe writer for the TAB-delimited result.log.
// Extracted from Runner so the "hold the mutex across close→rewrite→reopen"
// invariant lives in one place and race reasoning is local (the bubbletea
// goroutine can call rewriteExcluding via the 'd' key concurrently with a
// worker's append and with teardown's close — all serialized by w.mu here).
//
// The initial open (which honours cfg.Fresh's append-vs-truncate choice) stays
// in Runner.Start; this type wraps the already-open handle plus its path. The
// internal reopen after a rewrite is always append-mode (create if missing),
// matching the original behaviour.
type resultLogWriter struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// newResultLogWriter wraps an already-open result.log handle and its path.
func newResultLogWriter(f *os.File, path string) *resultLogWriter {
	return &resultLogWriter{file: f, path: path}
}

// append writes one TSV row. No-op once the handle has been closed (teardown).
func (w *resultLogWriter) append(tag string, exitCode int, line, logAbs string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	fmt.Fprintf(w.file, "%s\t%d\t%s\t%s\n", tag, exitCode, line, logAbs)
}

// close closes the handle and nils it so a racing reopen (from rewriteExcluding)
// and a double close are both harmless.
func (w *resultLogWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
}

// rewriteExcluding rewrites result.log dropping every row whose column-3 (the
// line) equals the argument. Uses a tmp + rename so the replacement is atomic on
// crash. The active handle is swapped out for the duration and reopened for
// append at the end (even on error paths) so append still works afterwards.
func (w *resultLogWriter) rewriteExcluding(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	defer func() {
		if w.file == nil {
			w.file, _ = os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		}
	}()

	src, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var kept []byte
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		text := sc.Text()
		cols := strings.Split(text, "\t")
		if len(cols) >= 4 && cols[2] == line {
			continue
		}
		kept = append(kept, []byte(text)...)
		kept = append(kept, '\n')
	}
	scanErr := sc.Err()
	src.Close()
	if scanErr != nil {
		return scanErr
	}

	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, kept, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, w.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
