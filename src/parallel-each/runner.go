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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// defaultLogDir is used when Config.LogDir is empty. Override via --log-dir
// to isolate concurrent parallel-each runs (e.g. two processes pointing at
// disjoint input files): each gets its own result.log and per-job log files.
const defaultLogDir = "parallel-each-log"

// forceKillGrace bounds how long we wait for a subprocess to exit after its
// context is cancelled (timeout or force-kill) and cmd.Cancel has SIGTERMed
// its process group. If it is still alive after this, exec sends SIGKILL and
// cmd.Run() returns. Without it, a job that ignores SIGTERM would block its
// worker forever, defeating both --attempt-timeout and force-kill and hanging
// shutdown (wg.Wait never returns, locks never released). A var (not const)
// so tests can shorten it.
var forceKillGrace = 10 * time.Second

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
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// result.log is TAB-delimited; an item containing a tab would inject a
		// bogus column and make resume/dedup match the wrong input next run.
		// Reject at the boundary rather than corrupt result.log silently.
		if strings.ContainsRune(line, '\t') {
			return nil, fmt.Errorf("%s:%d: item contains a tab (result.log is TAB-delimited; a tab corrupts resume) — remove it: %q",
				path, lineNo, line)
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

// dedupError renders a specific reason for the duplicate rejection depending
// on whether the item is in the current queue, was a prior success, or was a
// prior failure. Recognised by tests via the "duplicate" prefix.
func dedupError(line, status, logDir string) error {
	switch status {
	case "":
		return fmt.Errorf("duplicate: %q is already in the current queue", line)
	case "ok":
		return fmt.Errorf("duplicate: %q already succeeded in a previous run (ok in %s/result.log)",
			line, logDir)
	case "FAIL":
		return fmt.Errorf("duplicate: %q previously failed — remove its row from %s/result.log to retry",
			line, logDir)
	default:
		return fmt.Errorf("duplicate: %q already seen (status=%s)", line, status)
	}
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

// ProcessedEntry is one row loaded from result.log.
type ProcessedEntry struct {
	Status   string // "ok" or "FAIL"
	ExitCode int
	Input    string
	LogPath  string
}

// loadProcessedEntries reads an existing result.log and returns rows in file
// order (oldest first). Missing file is not an error and returns nil.
// Malformed rows are silently skipped.
func loadProcessedEntries(path string) ([]ProcessedEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []ProcessedEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 4 {
			continue
		}
		exit, _ := strconv.Atoi(cols[1])
		out = append(out, ProcessedEntry{
			Status:   cols[0],
			ExitCode: exit,
			Input:    cols[2],
			LogPath:  cols[3],
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// loadProcessedLines is a convenience wrapper returning input -> status.
// If a line appears more than once the LAST row wins. Kept for tests and
// callers that don't need per-row detail.
func loadProcessedLines(path string) (map[string]string, error) {
	entries, err := loadProcessedEntries(path)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Input] = e.Status
	}
	return m, nil
}

// filterProcessed returns lines whose value is not a key in the processed
// map, preserving original order.
func filterProcessed(lines []string, processed map[string]string) []string {
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
	cfg       Config
	lines     []string
	events    chan Event
	resultMu  sync.Mutex
	resultLog *os.File
	// lockFile holds an exclusive flock on <LogDir>/.lock for the entire
	// lifetime of Start..cleanup. Released on Close so a subsequent run
	// against the same LogDir can acquire it.
	lockFile *os.File
	// inputLockFile holds an exclusive flock on cfg.File itself (advisory,
	// affects only other parallel-each processes — editors / cat / etc.
	// are unaffected). Catches the "same -F, different --log-dir" pattern
	// that the log-dir lock alone would not detect.
	inputLockFile *os.File
	logDirAbs     string
	width         int
	stopCtx       context.Context
	stopCancel    context.CancelFunc
	killCtx       context.Context
	killCancel    context.CancelFunc

	// Pause state: when true, the dispatcher blocks before submitting new
	// jobs. Reversible via Resume. Independent of stopCtx; used for the
	// TUI's "undo graceful shutdown" flow.
	pauseMu sync.Mutex
	paused  bool
	pauseCh chan struct{}

	live     bool
	queuedMu sync.Mutex
	// queued value is "" for items that are part of this run's queue, or the
	// status column from result.log ("ok" / "FAIL") for seeded entries.
	queued     map[string]string
	addedCount int // tracks items added via Enqueue (read under queuedMu)

	// Unified dispatch queue. Protected by queueMu. Dispatch pops from the
	// head; Enqueue appends to the tail; EnqueueFront inserts at the head.
	// Also serves as the snapshot source for the TUI's queue view.
	queueMu   sync.Mutex
	queue     []runnerJob
	queueWake chan struct{} // buffered 1; signals queue has items or state changed
	nextIndex int           // monotonic job index (protected by queueMu)

	// Dynamic worker pool state.
	jobs          chan runnerJob
	wg            sync.WaitGroup
	workerMu      sync.Mutex
	targetPar     int // desired worker count (>=1)
	activeWorkers int // currently running worker goroutines
	// closing is set (under workerMu) by the dispatcher's teardown right
	// before it calls r.wg.Wait(). SetParallelism checks it under the same
	// lock and stops spawning: wg.Add must never run concurrently with
	// wg.Wait (the runtime panics on that), and a worker spawned after the
	// job channel is closed is pointless. workerMu thus serialises Add
	// against the Wait barrier.
	closing bool
	slotIDs map[int]bool // set of slot ids currently in use (1-based)
	// 各 worker に「retire 通知」用の cancel を持たせる。slot id -> cancel。
	// SetParallelism shrink で対象 slot の cancel を呼ぶと:
	//   - idle (次の job 待ちで select 中) なら即座に retire (subprocess を
	//     殺すわけではなく、まだ何も走らせていないので無害に exit)
	//   - in-flight (runOne 中) なら現 job を最後まで走らせ、終わってから
	//     ctx.Err() != nil を見て retire (graceful)
	// runOne 自体は引き続き r.killCtx を親に subprocess を起動するので、
	// shrink ではなく ForceKill を呼んだ場合のみ subprocess に SIGTERM が飛ぶ。
	workerRetire map[int]context.CancelFunc
}

// runnerJob is passed to workers via r.jobs.
type runnerJob struct {
	index int
	line  string
}

func NewRunner(cfg Config, lines []string) *Runner {
	// Normalise LogDir once so all downstream code can rely on cfg.LogDir
	// being non-empty (callers that omit it get the default).
	if cfg.LogDir == "" {
		cfg.LogDir = defaultLogDir
	}
	queued := make(map[string]string, len(lines))
	for _, l := range lines {
		queued[l] = "" // "" marks an item that belongs to this run's pending queue
	}
	return &Runner{
		cfg:    cfg,
		lines:  lines,
		events: make(chan Event, 64),
		width:  digitWidth(len(lines)),
		queued: queued,
	}
}

// SetLive toggles "live" dispatch: after the original input list is drained,
// the dispatcher blocks waiting for Enqueue instead of closing the job channel.
// Must be called before Start; ignored afterwards.
func (r *Runner) SetLive(live bool) {
	r.live = live
}

// Enqueue pushes a new item to the tail of the live queue (appended after
// any currently pending items). See EnqueueFront for "prepend" semantics.
func (r *Runner) Enqueue(line string) error {
	return r.enqueueInternal(line, false)
}

// EnqueueFront inserts a new item at the HEAD of the live queue so that it
// will be dispatched before any other pending items. Useful for injecting
// an urgent job interactively.
func (r *Runner) EnqueueFront(line string) error {
	return r.enqueueInternal(line, true)
}

// EnqueueForce retries a line that was previously processed (status "ok" /
// "FAIL" in result.log) by first clearing its dedup entry and result.log row,
// then enqueueing it like Enqueue. Items currently pending in the live queue
// are NOT re-enqueued — force is only meaningful for finished items.
func (r *Runner) EnqueueForce(line string) error {
	return r.enqueueInternalForce(line, false)
}

// EnqueueFrontForce is the HEAD-insert counterpart of EnqueueForce.
func (r *Runner) EnqueueFrontForce(line string) error {
	return r.enqueueInternalForce(line, true)
}

// errItemHasTab rejects an item containing a tab: result.log is TAB-delimited,
// so a tab splits the row into a bogus column and makes resume/dedup/forget
// match the wrong (or a different) input on the next run.
var errItemHasTab = errors.New("item contains a tab (result.log is TAB-delimited; a tab corrupts resume) — remove it")

func (r *Runner) enqueueInternalForce(line string, front bool) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return errors.New("empty input")
	}
	if strings.ContainsRune(line, '\t') {
		return errItemHasTab
	}
	r.queuedMu.Lock()
	status, exists := r.queued[line]
	r.queuedMu.Unlock()
	// Currently pending: force doesn't help — the item will run anyway.
	if exists && status == "" {
		return fmt.Errorf("%q is already pending — force not applicable", line)
	}
	if exists {
		if err := r.ForgetLine(line); err != nil {
			return fmt.Errorf("force: ForgetLine failed: %w", err)
		}
	}
	return r.enqueueInternal(line, front)
}

// enqueueInternal performs the common dedup + queue insert + input-file
// append logic for both Enqueue (tail) and EnqueueFront (head).
//
// Under --input-type=url, the line is also passed through a static URL
// syntax check (urlLooksValid). Reachability is NOT verified — real
// sites behind Cloudflare etc. routinely block curl-style probes, so a
// network pre-check would produce false rejections.
func (r *Runner) enqueueInternal(line string, front bool) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return errors.New("empty input")
	}
	if strings.ContainsRune(line, '\t') {
		return errItemHasTab
	}
	if !r.live {
		return errors.New("runner not in live mode")
	}
	if r.stopCtx != nil && r.stopCtx.Err() != nil {
		return errors.New("runner stopping")
	}
	if r.cfg.InputType == "url" {
		if ok, reason := urlLooksValid(line); !ok {
			return fmt.Errorf("URL syntax check failed: %s", reason)
		}
	}

	r.queuedMu.Lock()
	if status, exists := r.queued[line]; exists {
		r.queuedMu.Unlock()
		return dedupError(line, status, r.cfg.LogDir)
	}
	r.queued[line] = ""
	r.addedCount++
	r.queuedMu.Unlock()

	r.pushQueue(line, front)

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
}

// pushQueue appends (front=false) or prepends (front=true) the line to the
// dispatch queue, assigning it a fresh monotonic index, and wakes the
// dispatcher goroutine.
func (r *Runner) pushQueue(line string, front bool) {
	r.queueMu.Lock()
	r.nextIndex++
	j := runnerJob{index: r.nextIndex, line: line}
	if front {
		r.queue = append([]runnerJob{j}, r.queue...)
	} else {
		r.queue = append(r.queue, j)
	}
	r.queueMu.Unlock()
	r.wakeQueue()
}

// peekQueue blocks until the queue has an item or the runner is stopped /
// ends (live=false and queue empty). Returns the head job WITHOUT removing
// it. The caller must call commitJob(j.index) after the job is actually sent
// to a worker so that PendingSnapshot continues to reflect the item as
// pending until the moment it's picked up.
func (r *Runner) peekQueue() (runnerJob, bool) {
	for {
		r.queueMu.Lock()
		if len(r.queue) > 0 {
			j := r.queue[0]
			r.queueMu.Unlock()
			return j, true
		}
		empty := !r.live
		r.queueMu.Unlock()
		if empty {
			return runnerJob{}, false
		}
		select {
		case <-r.queueWake:
		case <-r.stopCtx.Done():
			return runnerJob{}, false
		}
	}
}

// commitJob removes the queued job with the given index. Safe to call for an
// index that is no longer present (e.g. already removed). We match by index,
// not position, because a concurrent EnqueueFront might have inserted a new
// head between peekQueue and commitJob.
func (r *Runner) commitJob(idx int) {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	for i, j := range r.queue {
		if j.index == idx {
			r.queue = append(r.queue[:i], r.queue[i+1:]...)
			return
		}
	}
}

func (r *Runner) wakeQueue() {
	if r.queueWake == nil {
		return
	}
	select {
	case r.queueWake <- struct{}{}:
	default:
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

// RemovePending drops a line from the pending dispatch queue so it will never
// be executed. Also clears the dedup entry so the line becomes eligible for
// re-enqueue via Enqueue / EnqueueFront.
//
// Returns an error if the line is not currently pending (either never queued,
// already dispatched, or already running — RemovePending does NOT cancel
// running jobs; use ForceKill / RequestStop for that).
func (r *Runner) RemovePending(line string) error {
	r.queueMu.Lock()
	idx := -1
	for i, j := range r.queue {
		if j.line == line {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.queueMu.Unlock()
		return fmt.Errorf("line not pending: %q", line)
	}
	r.queue = append(r.queue[:idx], r.queue[idx+1:]...)
	r.queueMu.Unlock()

	r.queuedMu.Lock()
	delete(r.queued, line)
	r.queuedMu.Unlock()
	return nil
}

// ForgetLine removes a line from both the in-memory dedup set AND the
// on-disk result.log (all matching rows, identified by column 3). After
// ForgetLine returns nil, the line is eligible to be Enqueue'd again.
// Per-job log files under parallel-each-log/ are NOT deleted.
func (r *Runner) ForgetLine(line string) error {
	r.queuedMu.Lock()
	delete(r.queued, line)
	r.queuedMu.Unlock()
	return r.rewriteResultLogExcluding(line)
}

// rewriteResultLogExcluding rewrites parallel-each-log/result.log with all
// rows whose column-3 equals line filtered out. Uses a tmp + rename so the
// replacement is atomic on crash; hold resultMu for the duration so no
// concurrent appendResult interleaves.
func (r *Runner) rewriteResultLogExcluding(line string) error {
	r.resultMu.Lock()
	defer r.resultMu.Unlock()

	path := filepath.Join(r.cfg.LogDir, "result.log")
	// Swap the active file handle out before reading/rewriting, and reopen
	// for append at the end (even on error paths) so appendResult still works.
	if r.resultLog != nil {
		r.resultLog.Close()
		r.resultLog = nil
	}
	defer func() {
		if r.resultLog == nil {
			r.resultLog, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		}
	}()

	src, err := os.Open(path)
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

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, kept, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SeedDedup marks each line as "already seen" so future Enqueue calls reject
// it as a duplicate. The status string ("ok" or "FAIL", as recorded in
// result.log) is used to produce a specific error message on rejection.
// Typically called right after NewRunner.
func (r *Runner) SeedDedup(entries map[string]string) {
	r.queuedMu.Lock()
	defer r.queuedMu.Unlock()
	for line, status := range entries {
		// Don't overwrite a "" (currently-queued) entry with a seeded one —
		// active queue takes precedence for the error wording.
		if _, exists := r.queued[line]; exists {
			continue
		}
		r.queued[line] = status
	}
}

// PendingSnapshot returns a copy of the lines currently in the dispatch
// queue (original input that has not been picked up yet, plus any items
// pushed via Enqueue / EnqueueFront). Order matches dispatch order.
func (r *Runner) PendingSnapshot() []string {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	out := make([]string, len(r.queue))
	for i, j := range r.queue {
		out[i] = j.line
	}
	return out
}

// PendingCount returns the number of not-yet-dispatched items.
func (r *Runner) PendingCount() int {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	return len(r.queue)
}

func (r *Runner) Events() <-chan Event { return r.events }

// RequestStop signals the runner to stop dispatching new jobs. Jobs currently
// executing are left to finish; queued jobs are dropped. Safe to call many times.
func (r *Runner) RequestStop() {
	if r.stopCancel != nil {
		r.stopCancel()
	}
	r.wakeQueue()
	r.wakePause()
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
	// Wake any dispatcher waiting on pause / an empty queue.
	r.wakePause()
	r.wakeQueue()
}

// Pause blocks further dispatching without stopping the runner. Running jobs
// continue to completion. Safe to call many times; reversible via Resume.
func (r *Runner) Pause() {
	r.pauseMu.Lock()
	r.paused = true
	r.pauseMu.Unlock()
	r.wakePause()
}

// Resume lifts a pause. If Pause has never been called (or stopCtx already
// cancelled) this is a no-op.
func (r *Runner) Resume() {
	r.pauseMu.Lock()
	r.paused = false
	r.pauseMu.Unlock()
	r.wakePause()
}

// IsPaused reports whether the runner is currently paused.
func (r *Runner) IsPaused() bool {
	r.pauseMu.Lock()
	defer r.pauseMu.Unlock()
	return r.paused
}

func (r *Runner) wakePause() {
	if r.pauseCh == nil {
		return
	}
	select {
	case r.pauseCh <- struct{}{}:
	default:
	}
}

// waitForUnpause blocks until pause is lifted or stopCtx fires. Returns true
// if the dispatcher should continue, false if it should exit.
func (r *Runner) waitForUnpause() bool {
	for r.IsPaused() {
		if r.stopCtx != nil && r.stopCtx.Err() != nil {
			return false
		}
		select {
		case <-r.pauseCh:
		case <-r.stopCtx.Done():
			return false
		}
	}
	return true
}

// Start launches workers. It returns immediately; results are delivered via Events().
// parent is used as the root context for both cancellation paths.
func (r *Runner) Start(parent context.Context) error {
	if err := os.MkdirAll(r.cfg.LogDir, 0o755); err != nil {
		return fmt.Errorf("mkdir log dir: %w", err)
	}
	abs, err := filepath.Abs(r.cfg.LogDir)
	if err != nil {
		return err
	}
	r.logDirAbs = abs

	// Acquire an exclusive flock on <LogDir>/.lock to detect concurrent runs
	// sharing the same output directory. Two parallel-each processes
	// against the same LogDir would otherwise race on result.log appends
	// and clobber each other's NNNN-<line>.log job logs. Lock is held for
	// the lifetime of the run; OS releases it on process exit (defensive
	// against crashes), but cleanup also closes it explicitly on the
	// graceful path.
	lockPath := filepath.Join(r.cfg.LogDir, ".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Read the holder's PID (best-effort) so the user knows who has it.
		holder := "another parallel-each"
		if data, rerr := os.ReadFile(lockPath); rerr == nil {
			if pid := strings.TrimSpace(string(data)); pid != "" {
				holder = "another parallel-each (PID " + pid + ")"
			}
		}
		lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("%s is already running with --log-dir %s; use a different --log-dir or stop the other run first (lock at %s)",
				holder, r.cfg.LogDir, lockPath)
		}
		return fmt.Errorf("acquire log-dir lock %s: %w", lockPath, err)
	}
	r.lockFile = lockFile
	// Record our PID so a future blocked instance can identify the holder.
	lockFile.Truncate(0)
	lockFile.Seek(0, 0)
	fmt.Fprintf(lockFile, "%d\n", os.Getpid())

	// Also lock the input file itself. Catches the "same -F, different
	// --log-dir" case (two parallel-each processes against the same input
	// file would both dispatch every line, doubling work and racing on
	// appendToInputFile). flock is advisory and per-fd, so editors / cat /
	// our own readInput + appendToInputFile (which open separate fds and
	// don't flock) are unaffected — only another parallel-each trying the
	// same syscall is blocked.
	//
	// A missing input file is tolerated: main.go pre-checks existence for
	// real invocations, so the only path here is test scaffolding with a
	// stub File. With no file there is nothing to share with a concurrent
	// run, so skipping the lock is safe.
	if r.cfg.File != "" {
		if ilf, err := os.OpenFile(r.cfg.File, os.O_RDONLY, 0); err == nil {
			if ferr := syscall.Flock(int(ilf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); ferr != nil {
				ilf.Close()
				r.lockFile.Close()
				r.lockFile = nil
				if errors.Is(ferr, syscall.EWOULDBLOCK) {
					return fmt.Errorf("another parallel-each is already running with -F %s; stop the other run first",
						r.cfg.File)
				}
				return fmt.Errorf("acquire input-file lock: %w", ferr)
			}
			r.inputLockFile = ilf
		} else if !os.IsNotExist(err) {
			r.lockFile.Close()
			r.lockFile = nil
			return fmt.Errorf("open input file for lock: %w", err)
		}
	}

	resultPath := filepath.Join(r.cfg.LogDir, "result.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if r.cfg.Fresh {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	rf, err := os.OpenFile(resultPath, flags, 0o644)
	if err != nil {
		// Release locks so we don't leak them on a Start failure.
		if r.inputLockFile != nil {
			r.inputLockFile.Close()
			r.inputLockFile = nil
		}
		r.lockFile.Close()
		r.lockFile = nil
		return fmt.Errorf("open result.log: %w", err)
	}
	r.resultLog = rf

	r.stopCtx, r.stopCancel = context.WithCancel(parent)
	r.killCtx, r.killCancel = context.WithCancel(parent)
	r.pauseCh = make(chan struct{}, 1)

	initialPar := r.cfg.Parallelism
	if initialPar <= 0 {
		initialPar = len(r.lines)
	}
	if initialPar < 1 {
		initialPar = 1
	}

	r.jobs = make(chan runnerJob)
	r.queueWake = make(chan struct{}, 1)

	// Seed the queue with the original input list.
	r.queueMu.Lock()
	for _, line := range r.lines {
		r.nextIndex++
		r.queue = append(r.queue, runnerJob{index: r.nextIndex, line: line})
	}
	r.queueMu.Unlock()

	r.SetParallelism(initialPar)

	go func() {
		defer func() {
			close(r.jobs)
			// Barrier so SetParallelism stops calling wg.Add: adding to a
			// WaitGroup concurrently with the Wait below is a runtime panic.
			// Set under workerMu, the same lock that guards wg.Add.
			r.workerMu.Lock()
			r.closing = true
			r.workerMu.Unlock()
			r.wg.Wait()
			// Close result.log under resultMu — the bubbletea goroutine can
			// still reach rewriteResultLogExcluding (ForgetLine via the 'd'
			// key or a force re-enqueue) during this teardown window, and it
			// touches r.resultLog under the same lock. Nil it so a racing
			// reopen and a double close are both harmless.
			r.resultMu.Lock()
			if r.resultLog != nil {
				r.resultLog.Close()
				r.resultLog = nil
			}
			r.resultMu.Unlock()
			if r.inputLockFile != nil {
				r.inputLockFile.Close() // releases input-file flock
				r.inputLockFile = nil
			}
			if r.lockFile != nil {
				r.lockFile.Close() // releases log-dir flock
				r.lockFile = nil
			}
			close(r.events)
		}()
		for {
			if !r.waitForUnpause() {
				return
			}
			j, ok := r.peekQueue()
			if !ok {
				return
			}
			select {
			case <-r.stopCtx.Done():
				return
			case r.jobs <- j:
				r.commitJob(j.index)
			}
		}
	}()

	return nil
}

// SetParallelism adjusts the target number of concurrent workers. Minimum 1.
// Growing spawns new worker goroutines with the smallest available slot ID.
// Shrinking signals the highest-numbered (current - n) workers to retire:
//   - if a target worker is idle (waiting on the job channel), it exits
//     immediately (no SIGTERM since no subprocess is running)
//   - if a target worker is in-flight, it finishes the current job
//     gracefully (no SIGTERM) and exits before picking up the next job
//
// Use ForceKill to also SIGTERM in-flight subprocesses across all workers.
// Safe to call any time.
//
// Grow/shrink target math uses len(workerRetire) — the count of workers that
// are still serving (not yet told to retire) — NOT activeWorkers.
// activeWorkers also counts workers that were cancelled by a previous shrink
// but are still draining an in-flight job (their decrement is deferred to
// workerLoop's exit, which under --attempt-timeout can be far away). Driving
// the math off activeWorkers would double-count those and leave a shrink→grow
// under target (spawning nothing), or let a repeated shrink over-cancel every
// live worker down to zero and stall the dispatcher on the unbuffered r.jobs.
func (r *Runner) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	if r.slotIDs == nil {
		r.slotIDs = make(map[int]bool)
	}
	if r.workerRetire == nil {
		r.workerRetire = make(map[int]context.CancelFunc)
	}
	r.targetPar = n
	// Runner is tearing down (dispatcher closed r.jobs and is in wg.Wait()):
	// record the intent but don't spawn — a wg.Add here would race the Wait
	// (runtime panic) and the worker would land on a closed job channel.
	if r.closing {
		return
	}
	// Grow: spawn new workers, each with its own retire-signal context.
	// Note: this ctx is NOT used for the subprocess (runOne keeps killCtx);
	// it only nudges the worker goroutine to exit either right away (idle)
	// or after the current job completes (in-flight).
	for len(r.workerRetire) < r.targetPar {
		slotID := r.allocSlotIDLocked()
		ctx, cancel := context.WithCancel(r.killCtx)
		r.workerRetire[slotID] = cancel
		r.activeWorkers++
		r.wg.Add(1)
		go r.workerLoop(slotID, ctx)
	}
	// Shrink: pick highest-numbered slots (so 1..n stays stable) and cancel
	// their retire-signal ctx. The actual exit timing depends on the worker
	// being idle vs in-flight (handled in workerLoop).
	for excess := len(r.workerRetire) - r.targetPar; excess > 0; excess-- {
		var pickID int
		for id := range r.workerRetire {
			if id > pickID {
				pickID = id
			}
		}
		if pickID == 0 {
			break
		}
		if cancel := r.workerRetire[pickID]; cancel != nil {
			cancel()
		}
		// Don't decrement activeWorkers / release slot here — workerLoop's
		// deferred cleanup handles that to keep the accounting in one place.
		delete(r.workerRetire, pickID)
	}
}

// allocSlotIDLocked returns the smallest unused 1-based slot id, marking it
// in-use. Caller must hold workerMu.
func (r *Runner) allocSlotIDLocked() int {
	for i := 1; ; i++ {
		if !r.slotIDs[i] {
			r.slotIDs[i] = true
			return i
		}
	}
}

// Parallelism returns the current target worker count.
func (r *Runner) Parallelism() int {
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	return r.targetPar
}

// workerLoop consumes jobs and retires when its retire-signal ctx is
// cancelled (typically by SetParallelism shrinking). The slot id is
// released on retirement so the next SetParallelism grow can reuse it
// (keeps slot numbers compact 1..P).
//
// Idle vs in-flight retirement:
//   - Idle (waiting on r.jobs in the select): the retire ctx Done branch
//     wins → exit immediately (no subprocess to clean up).
//   - In-flight (inside runOne): the current job runs to completion as if
//     no retire happened (graceful). After runOne returns we check the
//     retire ctx and exit before picking up the next job.
//
// runOne itself is invoked with r.killCtx, NOT the retire ctx, so shrink
// never SIGTERMs the subprocess. ForceKill (cancelling killCtx) is the
// only path that interrupts in-flight work.
func (r *Runner) workerLoop(slotID int, retireCtx context.Context) {
	defer r.wg.Done()
	defer func() {
		r.workerMu.Lock()
		delete(r.slotIDs, slotID)
		delete(r.workerRetire, slotID)
		r.activeWorkers--
		r.workerMu.Unlock()
	}()
	for {
		select {
		case <-retireCtx.Done():
			// idle 時の即時 retire (まだ何も走らせていないので無害に終了)。
			return
		case j, ok := <-r.jobs:
			if !ok {
				// r.jobs が close された (shutdown 経路)。後段の cleanup へ。
				return
			}
			if r.stopCtx.Err() != nil {
				continue
			}
			// graceful: 現 job は最後まで走らせる (subprocess は SIGTERM しない)。
			r.runOne(r.killCtx, slotID, j.index, j.line)
			// 走っている間に shrink で retire 通知が来ていたら、ここで exit。
			if retireCtx.Err() != nil {
				return
			}
		}
	}
}

func (r *Runner) runOne(_ context.Context, slotID, index int, line string) {
	overallStart := time.Now()
	idxStr := fmt.Sprintf("%0*d", r.width, index)
	safe := escapeFilename(line)
	logPath := filepath.Join(r.cfg.LogDir, idxStr+"-"+safe+".log")
	logAbs := filepath.Join(r.logDirAbs, filepath.Base(logPath))

	resolved := strings.ReplaceAll(r.cfg.Template, "{item}", line)
	shellCmd := buildShellCommand(r.cfg.Template)

	maxAttempts := max(r.cfg.Retries+1, 1)

	// Create log file once; retries append to the same file.
	logFile, err := os.Create(logPath)
	if err != nil {
		r.emit(Event{
			Kind: EventEnd, SlotID: slotID, JobIndex: index, Total: len(r.lines),
			Line: line, Started: overallStart, Ended: time.Now(),
			ExitCode: -1, LogPath: logAbs, Err: err,
			Attempt: 1, MaxAttempts: maxAttempts,
		})
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
		r.emit(Event{
			Kind:        EventStart,
			SlotID:      slotID,
			JobIndex:    index,
			Total:       len(r.lines),
			Line:        line,
			Started:     attemptStart,
			LogPath:     logAbs,
			Attempt:     attempt,
			MaxAttempts: maxAttempts,
		})

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
		// Escalate to SIGKILL if the process ignores the SIGTERM from Cancel.
		cmd.WaitDelay = forceKillGrace

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

	r.emit(Event{
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
	})
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
	if r.resultLog == nil {
		// Runner already torn down (dispatcher closed and nil'd the handle).
		// No worker reaches here after wg.Wait(); guard defensively anyway.
		return
	}
	fmt.Fprintf(r.resultLog, "%s\t%d\t%s\t%s\n", tag, exitCode, line, logAbs)
}

// emit delivers an event to consumers, but abandons the send if the runner is
// force-killed (killCtx cancelled). Without this, a worker blocks forever on
// the buffered r.events channel once it fills — which happens when the TUI's
// event loop is suspended running $EDITOR (tea.ExecProcess). A wedged worker
// never reaches wg.Done(), so the dispatcher's wg.Wait() never returns and the
// result.log FD + flocks are never released. Graceful stop (stopCtx) does NOT
// drop events: those jobs finish and their completions must still be reported.
//
// Trade-off: a completion racing force-kill may be dropped here, so the TUI /
// plain summary COUNTS are approximate at the moment of a force-kill. result.log
// stays accurate (appendResult runs before emit), and nothing hangs or leaks.
func (r *Runner) emit(ev Event) {
	select {
	case r.events <- ev:
	case <-r.killCtx.Done():
	}
}
