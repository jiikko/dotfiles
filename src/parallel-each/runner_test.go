package main

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadInput(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "plain lines",
			content: "alpha\nbeta\ngamma\n",
			want:    []string{"alpha", "beta", "gamma"},
		},
		{
			name:    "skips blank and comment",
			content: "alpha\n\n# comment\n  # indented comment\nbeta\n",
			want:    []string{"alpha", "beta"},
		},
		{
			name:    "preserves leading whitespace in content lines",
			content: "  not-a-comment\nfoo\n",
			want:    []string{"  not-a-comment", "foo"},
		},
		{
			name:    "empty file",
			content: "",
			want:    nil,
		},
		{
			name:    "only comments",
			content: "# a\n# b\n\n",
			want:    nil,
		},
		{
			name:    "no trailing newline",
			content: "alpha\nbeta",
			want:    []string{"alpha", "beta"},
		},
		{
			name:    "whitespace-only line is blank",
			content: "alpha\n   \t\nbeta\n",
			want:    []string{"alpha", "beta"},
		},
		{
			name:    "hash inside line is not a comment",
			content: "https://example.com/x#frag\n",
			want:    []string{"https://example.com/x#frag"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "in.txt")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := readInput(path)
			if err != nil {
				t.Fatalf("readInput: %v", err)
			}
			if !equalStrings(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadInputMissingFile(t *testing.T) {
	_, err := readInput("/nonexistent/path/to/file")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestDigitWidth(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 3},
		{1, 3},
		{9, 3},
		{99, 3},
		{999, 3},
		{1000, 4},
		{9999, 4},
		{10000, 5},
	}
	for _, c := range cases {
		if got := digitWidth(c.in); got != c.want {
			t.Errorf("digitWidth(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEscapeFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"alpha", "alpha"},
		{"beta gamma", "beta_gamma"},
		{"https://example.com/path?q=1", "https___example.com_path_q_1"},
		{"keep.dot_and-dash", "keep.dot_and-dash"},
		{"日本語", "___"},
		{"/", "_"},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeFilename(c.in); got != c.want {
			t.Errorf("escapeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeFilenameTruncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := escapeFilename(long)
	if len(got) != 120 {
		t.Errorf("expected length 120, got %d", len(got))
	}
}

func TestBuildShellCommand(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`dm {item}`, `dm "$1"`},
		{`cp {item} backup/{item}.bak`, `cp "$1" backup/"$1".bak`},
		{`echo no placeholder`, `echo no placeholder`},
	}
	for _, c := range cases {
		if got := buildShellCommand(c.in); got != c.want {
			t.Errorf("buildShellCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Integration: run a small batch end-to-end and verify events, result.log and
// per-job logs.
func TestRunnerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Parallelism: 2,
		// Template is passed directly to sh -c; exit 0 for ok lines, exit 1 for "fail".
		Template: `echo processed {item}; test {item} != fail`,
	}
	lines := []string{"alpha", "beta_gamma", "fail", "delta"}

	r := NewRunner(cfg, lines)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	starts := 0
	ends := 0
	failures := 0
	for ev := range r.Events() {
		switch ev.Kind {
		case EventStart:
			starts++
		case EventEnd:
			ends++
			if ev.ExitCode != 0 {
				failures++
			}
		}
	}

	if starts != len(lines) {
		t.Errorf("starts = %d, want %d", starts, len(lines))
	}
	if ends != len(lines) {
		t.Errorf("ends = %d, want %d", ends, len(lines))
	}
	if failures != 1 {
		t.Errorf("failures = %d, want 1", failures)
	}

	// result.log should have 4 TSV rows.
	data, err := os.ReadFile(filepath.Join(logDir, "result.log"))
	if err != nil {
		t.Fatalf("read result.log: %v", err)
	}
	rows := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rows) != 4 {
		t.Fatalf("result.log rows = %d, want 4\n%s", len(rows), data)
	}
	gotFail := 0
	for _, row := range rows {
		cols := strings.Split(row, "\t")
		if len(cols) != 4 {
			t.Errorf("row has %d cols: %q", len(cols), row)
			continue
		}
		if cols[0] == "FAIL" {
			gotFail++
			if cols[2] != "fail" {
				t.Errorf("FAIL row for input %q, want 'fail'", cols[2])
			}
		} else if cols[0] != "ok" {
			t.Errorf("unexpected status %q", cols[0])
		}
		if !filepath.IsAbs(cols[3]) {
			t.Errorf("log path is not absolute: %q", cols[3])
		}
	}
	if gotFail != 1 {
		t.Errorf("FAIL rows = %d, want 1", gotFail)
	}

	// Per-job log for "alpha" should exist and contain header + stdout.
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	// 4 job logs + result.log = 5 entries.
	if len(entries) != 5 {
		t.Errorf("log dir has %d entries, want 5: %v", len(entries), entries)
	}
	var alphaLog string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-alpha.log") {
			alphaLog = filepath.Join(logDir, e.Name())
			break
		}
	}
	if alphaLog == "" {
		t.Fatal("no -alpha.log file found")
	}
	f, err := os.Open(alphaLog)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var header []string
	var body []string
	pastHeader := false
	for sc.Scan() {
		if !pastHeader {
			header = append(header, sc.Text())
			if sc.Text() == "---" {
				pastHeader = true
			}
			continue
		}
		body = append(body, sc.Text())
	}
	if len(header) != 4 {
		t.Errorf("header lines = %d, want 4", len(header))
	}
	if len(header) > 0 && !strings.HasPrefix(header[0], "# item:") {
		t.Errorf("first header line = %q, want # item: prefix", header[0])
	}
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "processed alpha") {
		t.Errorf("alpha log body missing expected stdout: %q", joined)
	}
}

// RequestStop prevents new jobs from starting but lets already-running jobs
// finish naturally (no SIGTERM).
func TestRunnerRequestStopGraceful(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Parallelism: 2,
		Template:    `sleep 0.6; echo {item}`,
	}
	lines := []string{"a", "b", "c", "d", "e", "f"}

	r := NewRunner(cfg, lines)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Stop shortly after start: jobs a,b should be already running and allowed
	// to finish; c-f should be dropped before starting.
	go func() {
		time.Sleep(150 * time.Millisecond)
		r.RequestStop()
	}()

	start := time.Now()
	ok := 0
	ends := 0
	for ev := range r.Events() {
		if ev.Kind == EventEnd {
			ends++
			if ev.ExitCode == 0 {
				ok++
			}
		}
	}
	elapsed := time.Since(start)

	// The running jobs must have been allowed to finish (sleep 0.6), not killed.
	if elapsed < 500*time.Millisecond {
		t.Errorf("elapsed = %v; expected >=500ms (running jobs should not have been killed)", elapsed)
	}
	// Exactly the number of parallel slots should have completed successfully.
	if ok != 2 {
		t.Errorf("ok count = %d, want 2 (running jobs finish naturally)", ok)
	}
	if ends != 2 {
		t.Errorf("end events = %d, want 2", ends)
	}
}

// ForceKill terminates running subprocesses with SIGTERM.
func TestRunnerForceKill(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5; echo {item}`}
	lines := []string{"a", "b", "c", "d"}

	r := NewRunner(cfg, lines)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		r.ForceKill()
	}()

	start := time.Now()
	for range r.Events() {
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Errorf("ForceKill too slow: %v (running procs should have been SIGTERMed)", elapsed)
	}
}

// RequestStop then ForceKill escalates from graceful to force-kill.
func TestRunnerStopThenKill(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5; echo {item}`}
	lines := []string{"a", "b", "c", "d"}

	r := NewRunner(cfg, lines)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// RequestStop first — workers still sleeping for 5s, won't finish on their own.
	go func() {
		time.Sleep(100 * time.Millisecond)
		r.RequestStop()
	}()
	// Then escalate to ForceKill.
	go func() {
		time.Sleep(400 * time.Millisecond)
		r.ForceKill()
	}()

	start := time.Now()
	for range r.Events() {
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Errorf("escalation too slow: %v", elapsed)
	}
}

// When the context is cancelled before jobs complete, running processes should
// be killed and remaining jobs skipped.
func TestRunnerCancel(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Parallelism: 2,
		Template:    `sleep 5; echo {item}`,
	}
	lines := []string{"a", "b", "c", "d", "e", "f"}

	r := NewRunner(cfg, lines)
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Cancel quickly; workers' sleep should be killed.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	ends := 0
	for ev := range r.Events() {
		if ev.Kind == EventEnd {
			ends++
		}
	}
	dur := time.Since(start)
	if dur > 3*time.Second {
		t.Errorf("cancel took too long: %v (expected <3s with 5s sleep)", dur)
	}
	// Not all jobs should complete — at least some should be skipped.
	if ends >= len(lines) {
		t.Errorf("all %d jobs completed despite cancel", ends)
	}
}

func TestFindDuplicates(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no duplicates", []string{"a", "b", "c"}, nil},
		{"one duplicate", []string{"a", "b", "a"}, []string{"a (x2)"}},
		{"multiple duplicates sorted",
			[]string{"b", "a", "c", "b", "a", "a"},
			[]string{"a (x3)", "b (x2)"}},
		{"empty input", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findDuplicates(c.in)
			if !equalStrings(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestLoadProcessedLines(t *testing.T) {
	t.Run("missing file returns empty set", func(t *testing.T) {
		got, err := loadProcessedLines(filepath.Join(t.TempDir(), "nope.log"))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("want empty, got %v", got)
		}
	})

	t.Run("parses TSV and ignores malformed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "result.log")
		content := strings.Join([]string{
			"ok\t0\talpha\t/tmp/a.log",
			"FAIL\t1\tbeta gamma\t/tmp/b.log",
			"",                        // blank -> <4 cols -> skipped
			"malformed line",          // no tabs -> skipped
			"ok\t0\tgamma\t/tmp/g.log",
		}, "\n") + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := loadProcessedLines(path)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]struct{}{
			"alpha":      {},
			"beta gamma": {},
			"gamma":      {},
		}
		if len(got) != len(want) {
			t.Fatalf("size mismatch: got %v, want %v", got, want)
		}
		for k := range want {
			if _, ok := got[k]; !ok {
				t.Errorf("missing key %q", k)
			}
		}
	})
}

func TestFilterProcessed(t *testing.T) {
	processed := map[string]struct{}{
		"alpha": {},
		"gamma": {},
	}
	got := filterProcessed([]string{"alpha", "beta", "gamma", "delta"}, processed)
	want := []string{"beta", "delta"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Second run resumes: a fresh run processes everything, a subsequent run with
// the same input + the same log dir should process nothing new.
func TestRunnerResume(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `echo {item}`}
	lines := []string{"a", "b", "c"}

	// First run.
	r1 := NewRunner(cfg, lines)
	ctx := context.Background()
	if err := r1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	firstEnds := 0
	for ev := range r1.Events() {
		if ev.Kind == EventEnd {
			firstEnds++
		}
	}
	if firstEnds != 3 {
		t.Fatalf("first run ends = %d, want 3", firstEnds)
	}

	// Simulate what main.go does for the second run: load + filter.
	processed, err := loadProcessedLines(filepath.Join(logDir, "result.log"))
	if err != nil {
		t.Fatal(err)
	}
	remaining := filterProcessed(lines, processed)
	if len(remaining) != 0 {
		t.Errorf("remaining = %v, want empty", remaining)
	}

	// Verify result.log still has exactly 3 rows (append did not duplicate).
	data, err := os.ReadFile(filepath.Join(logDir, "result.log"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rows) != 3 {
		t.Errorf("result.log rows = %d, want 3:\n%s", len(rows), data)
	}
}

// A new run with additional items should append new rows to result.log and
// leave existing rows intact.
func TestRunnerAppendsOnResume(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `echo {item}`}

	// First run: a, b
	r1 := NewRunner(cfg, []string{"a", "b"})
	if err := r1.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range r1.Events() {
	}

	// Second run: a, b, c, d — but after filtering we only want c, d.
	processed, _ := loadProcessedLines(filepath.Join(logDir, "result.log"))
	remaining := filterProcessed([]string{"a", "b", "c", "d"}, processed)
	if !equalStrings(remaining, []string{"c", "d"}) {
		t.Fatalf("remaining = %v, want [c d]", remaining)
	}

	r2 := NewRunner(cfg, remaining)
	if err := r2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range r2.Events() {
	}

	data, _ := os.ReadFile(filepath.Join(logDir, "result.log"))
	rows := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rows) != 4 {
		t.Errorf("result.log rows = %d, want 4:\n%s", len(rows), data)
	}
	// All four inputs should appear in column 3.
	seen := make(map[string]bool)
	for _, row := range rows {
		cols := strings.Split(row, "\t")
		if len(cols) >= 4 {
			seen[cols[2]] = true
		}
	}
	for _, want := range []string{"a", "b", "c", "d"} {
		if !seen[want] {
			t.Errorf("missing %q in result.log", want)
		}
	}
}

// A job that fails N times then succeeds should end up with ok and a single
// FAIL-free row in result.log, while the per-job log records all attempts.
func TestRunnerRetriesThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create a counter file. The template increments the counter and fails
	// until the counter reaches 3, then succeeds.
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Parallelism: 1,
		Retries:     5,
		Template: `n=$(cat ` + counter + `); n=$((n+1)); echo $n > ` + counter +
			`; echo attempt=$n item={item}; [ $n -ge 3 ]`,
	}

	r := NewRunner(cfg, []string{"alpha"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	starts := 0
	var finalEnd Event
	for ev := range r.Events() {
		switch ev.Kind {
		case EventStart:
			starts++
		case EventEnd:
			finalEnd = ev
		}
	}

	if starts != 3 {
		t.Errorf("EventStart count = %d, want 3", starts)
	}
	if finalEnd.ExitCode != 0 {
		t.Errorf("final ExitCode = %d, want 0", finalEnd.ExitCode)
	}
	if finalEnd.Attempt != 3 || finalEnd.MaxAttempts != 6 {
		t.Errorf("final Attempt/MaxAttempts = %d/%d, want 3/6", finalEnd.Attempt, finalEnd.MaxAttempts)
	}

	// Log file should contain retry separators for attempts 2 and 3.
	entries, _ := os.ReadDir(logDir)
	var logPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-alpha.log") {
			logPath = filepath.Join(logDir, e.Name())
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "=== retry 1/5") {
		t.Errorf("log missing 'retry 1/5' separator:\n%s", s)
	}
	if !strings.Contains(s, "=== retry 2/5") {
		t.Errorf("log missing 'retry 2/5' separator")
	}
	if !strings.Contains(s, "attempt=3") {
		t.Errorf("log missing final attempt output")
	}

	// result.log records only the final outcome.
	resData, _ := os.ReadFile(filepath.Join(logDir, "result.log"))
	rows := strings.Split(strings.TrimRight(string(resData), "\n"), "\n")
	if len(rows) != 1 {
		t.Errorf("result.log rows = %d, want 1", len(rows))
	}
	if !strings.HasPrefix(rows[0], "ok\t0\t") {
		t.Errorf("result.log row = %q", rows[0])
	}
}

// After exhausting retries the job should be marked FAIL.
func TestRunnerRetriesExhausted(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Retries: 2, Template: `echo fail; exit 7`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	starts := 0
	var finalEnd Event
	for ev := range r.Events() {
		switch ev.Kind {
		case EventStart:
			starts++
		case EventEnd:
			finalEnd = ev
		}
	}
	if starts != 3 {
		t.Errorf("starts = %d, want 3 (1 + 2 retries)", starts)
	}
	if finalEnd.ExitCode != 7 {
		t.Errorf("final ExitCode = %d, want 7", finalEnd.ExitCode)
	}
	if finalEnd.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3", finalEnd.Attempt)
	}
}

// A job exceeding the per-attempt timeout is killed and retried.
func TestRunnerTimeoutPerAttempt(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Parallelism:    1,
		Retries:        1,
		AttemptTimeout: 300 * time.Millisecond,
		Template:       `sleep 5`,
	}

	r := NewRunner(cfg, []string{"a"})
	start := time.Now()
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	starts := 0
	var finalEnd Event
	for ev := range r.Events() {
		if ev.Kind == EventStart {
			starts++
		} else {
			finalEnd = ev
		}
	}
	elapsed := time.Since(start)

	// Should have attempted twice (initial + 1 retry), each killed at ~300ms.
	if starts != 2 {
		t.Errorf("starts = %d, want 2", starts)
	}
	if !finalEnd.TimedOut {
		t.Errorf("TimedOut = false, want true")
	}
	if finalEnd.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero")
	}
	// Sanity: total runtime should be roughly 2 * 300ms, not 2 * 5s.
	if elapsed > 3*time.Second {
		t.Errorf("elapsed = %v; timeout didn't fire", elapsed)
	}
}

// Live mode: items Enqueue'd after Start are picked up and processed. Non-live
// mode rejects Enqueue.
func TestRunnerLiveEnqueue(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"a", "b"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Drain events in the background to keep the dispatcher moving.
	var seenLines []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range r.Events() {
			if ev.Kind == EventEnd {
				mu.Lock()
				seenLines = append(seenLines, ev.Line)
				mu.Unlock()
			}
		}
	}()

	// Wait briefly then enqueue two more items.
	time.Sleep(200 * time.Millisecond)
	if err := r.Enqueue("c"); err != nil {
		t.Fatalf("Enqueue(c): %v", err)
	}
	if err := r.Enqueue("d"); err != nil {
		t.Fatalf("Enqueue(d): %v", err)
	}
	// Duplicate should fail.
	if err := r.Enqueue("c"); err == nil {
		t.Error("expected duplicate Enqueue to fail")
	}
	// Empty should fail.
	if err := r.Enqueue("  "); err == nil {
		t.Error("expected empty Enqueue to fail")
	}

	// Give the added items time to flow through, then stop.
	time.Sleep(300 * time.Millisecond)
	r.RequestStop()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{"a", "b", "c", "d"} {
		found := false
		for _, got := range seenLines {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing processed item %q (got %v)", want, seenLines)
		}
	}
	if r.AddedCount() != 2 {
		t.Errorf("AddedCount = %d, want 2", r.AddedCount())
	}
}

// Successful Enqueue appends the line to the -F input file.
func TestRunnerEnqueueAppendsToInputFile(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	inputPath := filepath.Join(dir, "urls.txt")
	initial := "original-1\noriginal-2\n"
	if err := os.WriteFile(inputPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, File: inputPath, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"original-1", "original-2"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	if err := r.Enqueue("added-1"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := r.Enqueue("added 2 with spaces"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := initial + "added-1\n" + "added 2 with spaces\n"
	if got != want {
		t.Errorf("input file mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

// If another process / the user added the same line to the input file after
// startup, Enqueue still succeeds (in-memory dedupe didn't know about it)
// but skips the append step so the file isn't duplicated.
func TestRunnerEnqueueSkipsAppendWhenLineAlreadyInFile(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	inputPath := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(inputPath, []byte("orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, File: inputPath, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"orig"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	// Simulate an external write of a line that is NOT yet in our queued set.
	if err := os.WriteFile(inputPath, []byte("orig\nextern\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := r.Enqueue("extern"); err != nil {
		t.Fatalf("Enqueue(extern): %v", err)
	}

	data, _ := os.ReadFile(inputPath)
	if string(data) != "orig\nextern\n" {
		t.Errorf("file was modified despite pre-existing line: %q", string(data))
	}
	if r.AddedCount() != 1 {
		t.Errorf("AddedCount = %d, want 1 (enqueue still succeeds)", r.AddedCount())
	}
}

// Duplicate Enqueue does NOT append (no-op for on-disk file too).
func TestRunnerEnqueueDuplicateDoesNotAppend(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	inputPath := filepath.Join(dir, "urls.txt")
	initial := "original\n"
	if err := os.WriteFile(inputPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, File: inputPath, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"original"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	if err := r.Enqueue("original"); err == nil {
		t.Fatal("expected duplicate Enqueue to fail")
	}
	data, _ := os.ReadFile(inputPath)
	if string(data) != initial {
		t.Errorf("file was modified on duplicate: %q", string(data))
	}
}

// Non-live runner: Enqueue returns an error.
func TestRunnerEnqueueRejectedWhenNotLive(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.RequestStop()
		for range r.Events() {
		}
	}()

	if err := r.Enqueue("b"); err == nil {
		t.Error("expected Enqueue to fail on non-live runner")
	}
}

// Enqueue is rejected after RequestStop.
func TestRunnerEnqueueRejectedAfterStop(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"a"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Drain events in background.
	go func() {
		for range r.Events() {
		}
	}()

	r.RequestStop()
	time.Sleep(50 * time.Millisecond)
	if err := r.Enqueue("b"); err == nil {
		t.Error("expected Enqueue to fail after RequestStop")
	}
}

// Items already in result.log (added to queued set at NewRunner) are also
// rejected as duplicates via Enqueue.
func TestRunnerEnqueueRejectsExistingOriginal(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"a", "b"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	if err := r.Enqueue("a"); err == nil {
		t.Error("expected duplicate-of-original Enqueue to fail")
	}
}

// When the parent context hits its total deadline, everything is force-killed
// regardless of per-attempt timeout or retries.
func TestRunnerTotalTimeoutForceKills(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Parallelism:    2,
		Retries:        10, // plenty of retries; total-timeout should cut it short
		AttemptTimeout: 10 * time.Second,
		Template:       `sleep 30`,
	}
	lines := []string{"a", "b", "c", "d"}

	parent, cancelParent := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancelParent()

	r := NewRunner(cfg, lines)
	start := time.Now()
	if err := r.Start(parent); err != nil {
		t.Fatal(err)
	}

	// Mirror what main.go does: force-kill when parent expires.
	go func() {
		<-parent.Done()
		r.ForceKill()
	}()

	for range r.Events() {
	}
	elapsed := time.Since(start)

	// Should finish well within the per-attempt timeout * parallelism.
	if elapsed > 3*time.Second {
		t.Errorf("total-timeout did not fire promptly: elapsed=%v", elapsed)
	}
}

// --fresh truncates existing result.log.
func TestRunnerFreshTruncates(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Pre-populate result.log.
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := "ok\t0\tstale-item\t/tmp/old.log\n"
	if err := os.WriteFile(filepath.Join(logDir, "result.log"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`, Fresh: true}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range r.Events() {
	}

	data, _ := os.ReadFile(filepath.Join(logDir, "result.log"))
	if strings.Contains(string(data), "stale-item") {
		t.Errorf("--fresh did not truncate result.log:\n%s", data)
	}
	rows := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rows) != 1 {
		t.Errorf("rows after fresh = %d, want 1", len(rows))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
