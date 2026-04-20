package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const logDir = "parallel-each-log"

// readInput loads non-blank, non-comment lines.
func readInput(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func runDryRun(cfg Config, lines []string) {
	width := digitWidth(len(lines))
	for i, line := range lines {
		resolved := strings.ReplaceAll(cfg.Template, "{item}", line)
		fmt.Printf("%0*d %s\n", width, i+1, resolved)
	}
}

func digitWidth(n int) int {
	w := 0
	for n > 0 {
		n /= 10
		w++
	}
	if w < 3 {
		w = 3
	}
	return w
}

// findDuplicates returns duplicate values in the input with their counts,
// sorted lexicographically. Returns nil if all values are unique.
func findDuplicates(lines []string) []string {
	counts := make(map[string]int, len(lines))
	for _, l := range lines {
		counts[l]++
	}
	var dupes []string
	for line, n := range counts {
		if n > 1 {
			dupes = append(dupes, fmt.Sprintf("%s (x%d)", line, n))
		}
	}
	sort.Strings(dupes)
	return dupes
}

// loadProcessedLines reads an existing result.log and returns the set of
// input values (column 3 of TSV). Missing file is not an error and returns
// an empty map. Malformed rows are silently skipped.
func loadProcessedLines(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	defer f.Close()

	set := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 4 {
			continue
		}
		set[cols[2]] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

// filterProcessed returns lines whose value is not in the processed set,
// preserving original order.
func filterProcessed(lines []string, processed map[string]struct{}) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if _, ok := processed[l]; ok {
			continue
		}
		out = append(out, l)
	}
	return out
}

var escapeRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func escapeFilename(s string) string {
	out := escapeRe.ReplaceAllString(s, "_")
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// buildShellCommand replaces each {item} with "$1" so the actual line can be
// passed as a safely-quoted positional argument to sh -c.
func buildShellCommand(template string) string {
	return strings.ReplaceAll(template, "{item}", `"$1"`)
}

// Runner orchestrates job execution with bounded parallelism.
//
// Shutdown is two-stage:
//   - RequestStop: stop dispatching new jobs and let already-running jobs
//     finish naturally. Queued-but-not-yet-started jobs are dropped.
//   - ForceKill:   additionally send SIGTERM to any still-running sh
//     subprocesses (via exec.CommandContext cancellation).
//
// Calling ForceKill implies RequestStop; both are idempotent.
//
// Live mode (SetLive(true)) keeps the dispatcher alive after the original
// input list is exhausted so that Enqueue can push additional items at
// runtime (used by the TUI).
type Runner struct {
	cfg        Config
	lines      []string
	events     chan Event
	resultMu   sync.Mutex
	resultLog  *os.File
	logDirAbs  string
	width      int
	stopCtx    context.Context
	stopCancel context.CancelFunc
	killCtx    context.Context
	killCancel context.CancelFunc

	adds       chan string
	live       bool
	queuedMu   sync.Mutex
	queued     map[string]struct{}
	addedCount int // tracks items added via Enqueue (read under queuedMu)

	// Snapshot of items that have not started running yet. Items are added
	// when registered (original list on Start, or Enqueue) and removed when
	// a worker picks them up. Used by the TUI 'queue' view.
	pendingMu sync.Mutex
	pending   []string

	// Dynamic worker pool state.
	jobs          chan runnerJob
	wg            sync.WaitGroup
	workerMu      sync.Mutex
	targetPar     int // desired worker count (>=1)
	activeWorkers int // currently running worker goroutines
	nextSlotID    int // monotonic slot id for new workers
}

// runnerJob is passed to workers via r.jobs.
type runnerJob struct {
	index int
	line  string
}

func NewRunner(cfg Config, lines []string) *Runner {
	queued := make(map[string]struct{}, len(lines))
	for _, l := range lines {
		queued[l] = struct{}{}
	}
	return &Runner{
		cfg:    cfg,
		lines:  lines,
		events: make(chan Event, 64),
		width:  digitWidth(len(lines)),
		adds:   make(chan string, 256),
		queued: queued,
	}
}

// SetLive toggles "live" dispatch: after the original input list is drained,
// the dispatcher blocks waiting for Enqueue instead of closing the job channel.
// Must be called before Start; ignored afterwards.
func (r *Runner) SetLive(live bool) {
	r.live = live
}

// Enqueue pushes a new item into the live queue. Returns an error if the
// runner is not live, if the item was already processed/queued, or if the
// runner has been asked to stop. Thread-safe.
//
// On successful enqueue the line is also appended to the -F input file so
// the on-disk list stays a faithful record of everything that was processed.
// An input-append failure is non-fatal (logged via the returned warning,
// but the in-memory enqueue still succeeds).
func (r *Runner) Enqueue(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return fmt.Errorf("empty input")
	}
	if !r.live {
		return fmt.Errorf("runner not in live mode")
	}
	if r.stopCtx != nil && r.stopCtx.Err() != nil {
		return fmt.Errorf("runner stopping")
	}

	r.queuedMu.Lock()
	if _, exists := r.queued[line]; exists {
		r.queuedMu.Unlock()
		return fmt.Errorf("duplicate: %q already queued or processed", line)
	}
	r.queued[line] = struct{}{}
	r.addedCount++
	r.queuedMu.Unlock()

	select {
	case r.adds <- line:
		// Track the new item in the pending snapshot for the queue view.
		r.addPending(line)
		// Best-effort append to the -F input file.
		switch err := r.appendToInputFile(line); {
		case err == nil:
		case errors.Is(err, errInputLineExists):
			fmt.Fprintf(os.Stderr,
				"warning: %q is already a line in %s; skipped input-file append\n",
				line, r.cfg.File)
		default:
			fmt.Fprintf(os.Stderr, "warning: could not append to %s: %v\n", r.cfg.File, err)
		}
		return nil
	default:
		r.queuedMu.Lock()
		delete(r.queued, line)
		r.addedCount--
		r.queuedMu.Unlock()
		return fmt.Errorf("queue full; try again shortly")
	}
}

// errInputLineExists signals that a would-be appended line is already present
// in the -F input file, so we leave the file untouched.
var errInputLineExists = errors.New("line already in input file")

// appendToInputFile appends "<line>\n" to the -F input file if the line is
// not already present (exact match against any existing row, ignoring
// surrounding whitespace). Short writes to an O_APPEND file are atomic under
// POSIX on macOS/Linux.
func (r *Runner) appendToInputFile(line string) error {
	if r.cfg.File == "" {
		return nil
	}

	// Defensive scan: if the exact line is already in the file (e.g. the
	// file was edited externally mid-run), don't duplicate it.
	if existing, err := os.Open(r.cfg.File); err == nil {
		sc := bufio.NewScanner(existing)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		found := false
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == line {
				found = true
				break
			}
		}
		scanErr := sc.Err()
		existing.Close()
		if scanErr != nil {
			return scanErr
		}
		if found {
			return errInputLineExists
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	f, err := os.OpenFile(r.cfg.File, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// AddedCount returns the number of items pushed via Enqueue.
func (r *Runner) AddedCount() int {
	r.queuedMu.Lock()
	defer r.queuedMu.Unlock()
	return r.addedCount
}

// PendingSnapshot returns a copy of the lines that have been registered but
// not yet started by a worker. Order is insertion order (original input
// first, adds appended in the order they were Enqueue'd).
func (r *Runner) PendingSnapshot() []string {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	out := make([]string, len(r.pending))
	copy(out, r.pending)
	return out
}

// PendingCount returns the number of not-yet-started items (cheap).
func (r *Runner) PendingCount() int {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	return len(r.pending)
}

func (r *Runner) addPending(line string) {
	r.pendingMu.Lock()
	r.pending = append(r.pending, line)
	r.pendingMu.Unlock()
}

// removePending removes the first occurrence of line from the pending list.
// Pending entries are unique thanks to the dedup check, so "first" == "only".
func (r *Runner) removePending(line string) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	for i, l := range r.pending {
		if l == line {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			return
		}
	}
}

func (r *Runner) Events() <-chan Event { return r.events }

// RequestStop signals the runner to stop dispatching new jobs. Jobs currently
// executing are left to finish; queued jobs are dropped. Safe to call many times.
func (r *Runner) RequestStop() {
	if r.stopCancel != nil {
		r.stopCancel()
	}
}

// ForceKill sends SIGTERM to running subprocesses and stops dispatching.
// Safe to call many times.
func (r *Runner) ForceKill() {
	if r.stopCancel != nil {
		r.stopCancel()
	}
	if r.killCancel != nil {
		r.killCancel()
	}
}

// Start launches workers. It returns immediately; results are delivered via Events().
// parent is used as the root context for both cancellation paths.
func (r *Runner) Start(parent context.Context) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	abs, err := filepath.Abs(logDir)
	if err != nil {
		return err
	}
	r.logDirAbs = abs

	resultPath := filepath.Join(logDir, "result.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if r.cfg.Fresh {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	rf, err := os.OpenFile(resultPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open result.log: %w", err)
	}
	r.resultLog = rf

	r.stopCtx, r.stopCancel = context.WithCancel(parent)
	r.killCtx, r.killCancel = context.WithCancel(parent)

	initialPar := r.cfg.Parallelism
	if initialPar <= 0 {
		initialPar = len(r.lines)
	}
	if initialPar < 1 {
		initialPar = 1
	}

	r.jobs = make(chan runnerJob)

	// Seed the pending snapshot with the original input list.
	r.pendingMu.Lock()
	r.pending = append(r.pending, r.lines...)
	r.pendingMu.Unlock()

	r.SetParallelism(initialPar)

	go func() {
		defer func() {
			close(r.jobs)
			r.wg.Wait()
			r.resultLog.Close()
			close(r.events)
		}()
		// Phase 1: original input list.
		nextIdx := 0
		for _, line := range r.lines {
			nextIdx++
			select {
			case <-r.stopCtx.Done():
				return
			case r.jobs <- runnerJob{index: nextIdx, line: line}:
			}
		}
		// Phase 2: in live mode, wait for Enqueue. Loop until stopCtx fires.
		if !r.live {
			return
		}
		for {
			select {
			case <-r.stopCtx.Done():
				return
			case line := <-r.adds:
				nextIdx++
				select {
				case <-r.stopCtx.Done():
					return
				case r.jobs <- runnerJob{index: nextIdx, line: line}:
				}
			}
		}
	}()

	return nil
}

// SetParallelism adjusts the target number of concurrent workers. Minimum 1.
// Growing spawns new worker goroutines; shrinking signals the excess workers
// to exit after their current job completes (does not interrupt in-flight
// work). Safe to call at any time after Start.
func (r *Runner) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	r.targetPar = n
	for r.activeWorkers < r.targetPar {
		r.nextSlotID++
		slotID := r.nextSlotID
		r.activeWorkers++
		r.wg.Add(1)
		go r.workerLoop(slotID)
	}
}

// Parallelism returns the current target worker count.
func (r *Runner) Parallelism() int {
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	return r.targetPar
}

// workerLoop consumes jobs and retires itself when the target shrinks below
// the current active count.
func (r *Runner) workerLoop(slotID int) {
	defer r.wg.Done()
	for j := range r.jobs {
		// Job has been claimed by a worker — remove from the pending view.
		r.removePending(j.line)
		if r.stopCtx.Err() != nil {
			continue
		}
		r.runOne(r.killCtx, slotID, j.index, j.line)
		r.workerMu.Lock()
		if r.activeWorkers > r.targetPar {
			r.activeWorkers--
			r.workerMu.Unlock()
			return
		}
		r.workerMu.Unlock()
	}
	r.workerMu.Lock()
	r.activeWorkers--
	r.workerMu.Unlock()
}

func (r *Runner) runOne(_ context.Context, slotID, index int, line string) {
	overallStart := time.Now()
	idxStr := fmt.Sprintf("%0*d", r.width, index)
	safe := escapeFilename(line)
	logPath := filepath.Join(logDir, idxStr+"-"+safe+".log")
	logAbs := filepath.Join(r.logDirAbs, filepath.Base(logPath))

	resolved := strings.ReplaceAll(r.cfg.Template, "{item}", line)
	shellCmd := buildShellCommand(r.cfg.Template)

	maxAttempts := r.cfg.Retries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Create log file once; retries append to the same file.
	logFile, err := os.Create(logPath)
	if err != nil {
		r.events <- Event{
			Kind: EventEnd, SlotID: slotID, JobIndex: index, Total: len(r.lines),
			Line: line, Started: overallStart, Ended: time.Now(),
			ExitCode: -1, LogPath: logAbs, Err: err,
			Attempt: 1, MaxAttempts: maxAttempts,
		}
		r.appendResult("FAIL", -1, line, logAbs)
		return
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "# item: %s\n", line)
	fmt.Fprintf(logFile, "# cmd: %s\n", resolved)
	fmt.Fprintf(logFile, "# time: %s\n", overallStart.Format(time.RFC3339))
	if r.cfg.AttemptTimeout > 0 {
		fmt.Fprintf(logFile, "# attempt-timeout: %s  retries: %d\n", r.cfg.AttemptTimeout, r.cfg.Retries)
	}
	fmt.Fprintln(logFile, "---")

	var (
		finalExitCode = -1
		finalErr      error
		finalTimedOut bool
		attempt       int
	)

	for attempt = 1; attempt <= maxAttempts; attempt++ {
		// Honour an external graceful-stop request between attempts.
		if attempt > 1 && r.stopCtx.Err() != nil {
			break
		}

		attemptStart := time.Now()
		r.events <- Event{
			Kind:        EventStart,
			SlotID:      slotID,
			JobIndex:    index,
			Total:       len(r.lines),
			Line:        line,
			Started:     attemptStart,
			LogPath:     logAbs,
			Attempt:     attempt,
			MaxAttempts: maxAttempts,
		}

		if attempt > 1 {
			fmt.Fprintf(logFile, "\n=== retry %d/%d (previous exit=%d%s) ===\n",
				attempt-1, maxAttempts-1, finalExitCode, timedOutSuffix(finalTimedOut))
		}

		// Each attempt gets its own timeout context derived from killCtx.
		var cmdCtx context.Context
		var cancelTO context.CancelFunc
		if r.cfg.AttemptTimeout > 0 {
			cmdCtx, cancelTO = context.WithTimeout(r.killCtx, r.cfg.AttemptTimeout)
		} else {
			cmdCtx, cancelTO = context.WithCancel(r.killCtx)
		}

		cmd := exec.CommandContext(cmdCtx, "sh", "-c", shellCmd, "_", line)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			return nil
		}

		runErr := cmd.Run()
		cancelTO()

		exitCode := 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				exitCode = -1
			}
		}
		// Distinguish timeout from user-initiated kill.
		timedOut := cmdCtx.Err() == context.DeadlineExceeded && r.killCtx.Err() == nil

		finalExitCode = exitCode
		finalErr = runErr
		finalTimedOut = timedOut

		// Success or externally cancelled: no more attempts.
		if exitCode == 0 {
			break
		}
		if r.killCtx.Err() != nil || r.stopCtx.Err() != nil {
			break
		}
	}

	// Cap the attempt counter at what actually ran (in case we bailed early).
	if attempt > maxAttempts {
		attempt = maxAttempts
	}

	ended := time.Now()
	tag := "ok"
	if finalExitCode != 0 {
		tag = "FAIL"
	}
	r.appendResult(tag, finalExitCode, line, logAbs)

	r.events <- Event{
		Kind:        EventEnd,
		SlotID:      slotID,
		JobIndex:    index,
		Total:       len(r.lines),
		Line:        line,
		Started:     overallStart,
		Ended:       ended,
		ExitCode:    finalExitCode,
		LogPath:     logAbs,
		Err:         finalErr,
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		TimedOut:    finalTimedOut,
	}
}

func timedOutSuffix(timedOut bool) string {
	if timedOut {
		return " TIMEOUT"
	}
	return ""
}

func (r *Runner) appendResult(tag string, exitCode int, line, logAbs string) {
	r.resultMu.Lock()
	defer r.resultMu.Unlock()
	fmt.Fprintf(r.resultLog, "%s\t%d\t%s\t%s\n", tag, exitCode, line, logAbs)
}

