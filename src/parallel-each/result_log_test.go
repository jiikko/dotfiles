package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestResultLog(t *testing.T) (*resultLogWriter, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "result.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return newResultLogWriter(f, path), path
}

func TestResultLogAppendWritesTSVRows(t *testing.T) {
	w, path := newTestResultLog(t)
	w.append("ok", 0, "item-a", "/log/a")
	w.append("FAIL", 2, "item-b", "/log/b")
	w.close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "ok\t0\titem-a\t/log/a\nFAIL\t2\titem-b\t/log/b\n"
	if string(data) != want {
		t.Errorf("result.log =\n%q\nwant\n%q", data, want)
	}
}

func TestResultLogAppendNoopAfterClose(t *testing.T) {
	w, path := newTestResultLog(t)
	w.append("ok", 0, "before", "/log/a")
	w.close()
	w.append("ok", 0, "after", "/log/b") // handle closed → dropped
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "after") {
		t.Errorf("append after close was written: %q", data)
	}
}

func TestResultLogRewriteExcludingDropsMatchingRows(t *testing.T) {
	w, path := newTestResultLog(t)
	w.append("ok", 0, "keep-1", "/log/1")
	w.append("FAIL", 1, "drop", "/log/2")
	w.append("ok", 0, "keep-2", "/log/3")

	if err := w.rewriteExcluding("drop"); err != nil {
		t.Fatal(err)
	}
	// append still works after rewrite (handle reopened by the deferred reopen)
	w.append("ok", 0, "keep-3", "/log/4")
	w.close()

	s := string(mustRead(t, path))
	if strings.Contains(s, "drop") {
		t.Errorf("excluded row still present:\n%s", s)
	}
	for _, k := range []string{"keep-1", "keep-2", "keep-3"} {
		if !strings.Contains(s, k) {
			t.Errorf("%s missing after rewrite:\n%s", k, s)
		}
	}
}

// column-3 (the line) is the match key; a value appearing in another column
// (e.g. exit code or log path) must NOT trigger removal.
func TestResultLogRewriteMatchesOnlyColumn3(t *testing.T) {
	w, path := newTestResultLog(t)
	w.append("ok", 0, "real-item", "/log/target") // col3=real-item, col4=/log/target
	w.append("ok", 0, "target", "/log/x")         // col3=target
	if err := w.rewriteExcluding("target"); err != nil {
		t.Fatal(err)
	}
	w.close()
	s := string(mustRead(t, path))
	// real-item's col4 (/log/target) contains "target" but its col3 does not,
	// so it must survive; the row whose col3 IS "target" must be removed.
	if !strings.Contains(s, "real-item") {
		t.Errorf("row whose col4 merely contained the key was wrongly removed:\n%s", s)
	}
	if strings.Contains(s, "\ttarget\t") {
		t.Errorf("col3==target row not removed:\n%s", s)
	}
}

func TestResultLogRewriteMissingFileIsNoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.log") // never created
	w := newResultLogWriter(nil, path)
	if err := w.rewriteExcluding("x"); err != nil {
		t.Errorf("rewrite on missing file should be nil, got %v", err)
	}
}

func TestResultLogCloseIdempotent(t *testing.T) {
	w, _ := newTestResultLog(t)
	w.close()
	w.close() // must not panic / must be harmless
}

// Serialization smoke test: concurrent appends and rewrites must not corrupt
// the file or panic. Run under -race to catch missing locking.
func TestResultLogConcurrentAppendAndRewrite(t *testing.T) {
	w, path := newTestResultLog(t)
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w.append("ok", 0, fmt.Sprintf("item-%d", n), "/log")
		}(i)
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.rewriteExcluding("item-0")
		}()
	}
	wg.Wait()
	w.close()
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("result.log unreadable after concurrent access: %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
