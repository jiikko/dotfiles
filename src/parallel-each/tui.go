package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- styles ---------------------------------------------------------------

var (
	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleBarBg   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	styleKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// --- messages -------------------------------------------------------------

type eventMsg Event
type doneMsg struct{}
type tickMsg time.Time
type tailsMsg map[int][]string

func waitForEvent(ch <-chan Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return eventMsg(ev)
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// --- model ----------------------------------------------------------------

type slotState struct {
	Active      bool
	JobIndex    int
	Line        string
	Started     time.Time // current attempt start
	LogPath     string
	Attempt     int
	MaxAttempts int
}

type recentEntry struct {
	JobIndex int
	Line     string
	ExitCode int
	Duration time.Duration
	LogPath  string
}

type model struct {
	cfg       Config
	total     int
	completed int
	failed    int
	slots     map[int]slotState
	recent    []recentEntry
	maxRecent int

	// elapsed tracking: accumulates only while there is pending or running
	// work. When the queue empties, the timer freezes until more items are
	// added. activeStart is zero while idle.
	activeStart time.Time
	accumActive time.Duration

	// Current target parallelism (mirror of runner).
	par int

	// Parallelism change state: 'p' opens an input prompt for a new value;
	// Enter moves to a confirmation step; Enter there applies the change.
	parInputMode   bool
	parInputBuf    []rune
	parConfirmMode bool
	parPending     int

	// "Other" menu & export-wrapper sub-flow.
	otherMenu      bool
	exportInput    bool
	exportBuf      []rune
	exportTargetDir string // where the exported wrapper will be written

	// Tails captured from each active slot's log file.
	tails          map[int][]string // slotID -> last N lines
	tailPerSlot    int              // inline tail lines in overview (feature A)
	focusSlot      int              // slotID under focus; 0 = overview (feature B)
	focusTailLines int              // tail lines in focus view

	// Resume state: how many input rows were skipped because they were already
	// present in result.log. Purely informational — the total count already
	// excludes these.
	skipped int

	// Interactive add-to-queue state.
	inputMode    bool
	inputPrepend bool // if true, submissions go to the HEAD of the queue
	inputBuf     []rune
	flashMsg     string
	flashErr     bool
	flashUntil   time.Time
	addedLive    int // count of items successfully Enqueued

	// Full recent-view state.
	recentMode bool
	recentList listState

	// Queue view state (pending items).
	queueMode     bool
	queueList     listState
	queueSnapshot []string

	width    int
	height   int
	events   <-chan Event
	done     bool
	runner   *Runner
	paused   bool // 1st-press state: pause dispatching (reversible via 'c')
	stopping bool // 2nd-press state: graceful stop is final

	// Force-kill confirmation window: set when the 3rd press arms the
	// confirmation; press q / Ctrl-C again before it expires to actually
	// force-kill, otherwise the window auto-dismisses.
	forceKillConfirmUntil time.Time
}

func newModel(cfg Config, total int, events <-chan Event, runner *Runner, skipped int) model {
	m := model{
		cfg:            cfg,
		total:          total,
		slots:          make(map[int]slotState),
		maxRecent:      10000,
		tails:          make(map[int][]string),
		tailPerSlot:    2,
		focusTailLines: 20,
		// maxRecent caps in-memory history. Overview shows a height-limited
		// subset; 'r' opens a scrollable full view.
		events:  events,
		runner:  runner,
		width:   100,
		skipped: skipped,
	}
	// If we start with pending work, the clock is already running.
	if total > 0 {
		m.activeStart = time.Now()
	}
	if runner != nil {
		m.par = runner.Parallelism()
	} else {
		m.par = cfg.Parallelism
	}
	return m
}

// forceKillConfirmActive reports whether the 3rd-stage shutdown confirmation
// window is currently open.
func (m model) forceKillConfirmActive() bool {
	return !m.forceKillConfirmUntil.IsZero() && time.Now().Before(m.forceKillConfirmUntil)
}

// updateActiveState transitions between "running" and "idle" when the
// pending-work predicate (completed < total) changes. Must be called after
// any mutation to total or completed.
func (m *model) updateActiveState() {
	hasWork := m.completed < m.total
	running := !m.activeStart.IsZero()
	now := time.Now()
	switch {
	case hasWork && !running:
		m.activeStart = now
	case !hasWork && running:
		m.accumActive += now.Sub(m.activeStart)
		m.activeStart = time.Time{}
	}
}

// elapsed returns the total active processing time, excluding idle intervals,
// rounded to seconds for display.
func (m model) elapsed() time.Duration {
	return m.elapsedRaw().Round(time.Second)
}

// elapsedRaw returns the un-rounded accumulated active time. Used by tests.
func (m model) elapsedRaw() time.Duration {
	d := m.accumActive
	if !m.activeStart.IsZero() {
		d += time.Since(m.activeStart)
	}
	return d
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.events), tickEvery(200*time.Millisecond))
}

// refreshTailsCmd reads the tail of every active slot's log file in a
// goroutine and returns the aggregated result as a single tailsMsg. We
// snapshot slot log paths before launching IO to avoid map races.
func refreshTailsCmd(slots map[int]slotState, lines int) tea.Cmd {
	// Snapshot under caller's goroutine (this is called from Update, serial).
	type snap struct {
		slotID int
		path   string
	}
	var snaps []snap
	for id, s := range slots {
		if s.Active && s.LogPath != "" {
			snaps = append(snaps, snap{id, s.LogPath})
		}
	}
	return func() tea.Msg {
		out := make(tailsMsg, len(snaps))
		for _, s := range snaps {
			out[s.slotID] = readTail(s.path, lines)
		}
		return out
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Modal handlers take priority over the regular key map.
		if m.inputMode {
			return m.handleInputKey(msg)
		}
		if m.parConfirmMode {
			return m.handleParConfirmKey(msg)
		}
		if m.parInputMode {
			return m.handleParInputKey(msg)
		}
		if m.exportInput {
			return m.handleExportKey(msg)
		}
		if m.otherMenu {
			return m.handleOtherMenuKey(msg)
		}
		if m.recentMode {
			return m.handleRecentKey(msg)
		}
		if m.queueMode {
			return m.handleQueueKey(msg)
		}
		s := msg.String()
		// Digit keys 1-9 toggle focus on the corresponding slot, if active.
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			id := int(s[0] - '0')
			if m.focusSlot == id {
				m.focusSlot = 0
			} else if st, ok := m.slots[id]; ok && st.Active {
				m.focusSlot = id
			}
			return m, nil
		}
		switch s {
		case "a":
			if m.focusSlot == 0 && m.runner != nil && !m.stopping {
				m.inputMode = true
				m.inputPrepend = false
				m.inputBuf = m.inputBuf[:0]
				m.flashMsg = ""
			}
			return m, nil
		case "A":
			if m.focusSlot == 0 && m.runner != nil && !m.stopping {
				m.inputMode = true
				m.inputPrepend = true
				m.inputBuf = m.inputBuf[:0]
				m.flashMsg = ""
			}
			return m, nil
		case "p":
			// Enter parallelism-change mode.
			if m.runner != nil && !m.stopping && m.focusSlot == 0 {
				m.parInputMode = true
				m.parInputBuf = m.parInputBuf[:0]
			}
			return m, nil
		case "o":
			if m.focusSlot == 0 && !m.stopping {
				m.otherMenu = true
			}
			return m, nil
		case "r":
			if m.focusSlot == 0 && len(m.recent) > 0 {
				m.recentMode = true
				m.recentList.reset()
			}
			return m, nil
		case "l":
			if m.focusSlot == 0 && m.runner != nil {
				m.queueMode = true
				m.queueList.reset()
				m.queueSnapshot = m.runner.PendingSnapshot()
			}
			return m, nil
		case "e":
			// In focus mode, open the focused slot's log in $EDITOR.
			if m.focusSlot != 0 {
				if s, ok := m.slots[m.focusSlot]; ok && s.LogPath != "" {
					return m, openEditorCmd(s.LogPath)
				}
			}
			return m, nil
		case "0":
			m.focusSlot = 0
			return m, nil
		case "c":
			// Cancel a pending pause and resume normal dispatching.
			if m.paused && !m.stopping {
				m.paused = false
				m.runner.Resume()
				m.setFlash("resumed", false)
			}
			return m, nil
		case "esc":
			if m.focusSlot != 0 {
				m.focusSlot = 0
				return m, nil
			}
			fallthrough
		case "q", "ctrl+c":
			switch {
			case !m.paused:
				// 1st press: reversible pause.
				m.paused = true
				m.runner.Pause()
			case !m.stopping:
				// 2nd press: commit to graceful stop (final).
				m.stopping = true
				m.runner.RequestStop()
			case m.forceKillConfirmActive():
				// 4th press within the window: confirmed → force-kill.
				m.forceKillConfirmUntil = time.Time{}
				m.runner.ForceKill()
			default:
				// 3rd press: arm 3-second confirmation.
				m.forceKillConfirmUntil = time.Now().Add(3 * time.Second)
			}
			return m, nil
		}
		return m, nil

	case tickMsg:
		if m.done {
			return m, nil
		}
		// Expire the force-kill confirmation window after its deadline.
		if !m.forceKillConfirmUntil.IsZero() && time.Now().After(m.forceKillConfirmUntil) {
			m.forceKillConfirmUntil = time.Time{}
		}
		// Refresh tails for active slots; also refresh queue snapshot if
		// the queue view is open.
		lines := m.tailPerSlot
		if m.focusSlot != 0 {
			lines = m.focusTailLines
		}
		if m.queueMode && m.runner != nil {
			m.queueSnapshot = m.runner.PendingSnapshot()
			m.queueList.cursor = clampListIdx(m.queueList.cursor, len(m.queueSnapshot))
			m.queueList.ensureVisible(len(m.queueSnapshot), m.recentPageSize())
		}
		return m, tea.Batch(
			refreshTailsCmd(m.slots, lines),
			tickEvery(200*time.Millisecond),
		)

	case tailsMsg:
		// Replace tails for the keys present in the message; drop others.
		fresh := make(map[int][]string, len(msg))
		for id, lines := range msg {
			fresh[id] = lines
		}
		m.tails = fresh
		return m, nil

	case eventMsg:
		ev := Event(msg)
		switch ev.Kind {
		case EventStart:
			m.slots[ev.SlotID] = slotState{
				Active:      true,
				JobIndex:    ev.JobIndex,
				Line:        ev.Line,
				Started:     ev.Started,
				LogPath:     ev.LogPath,
				Attempt:     ev.Attempt,
				MaxAttempts: ev.MaxAttempts,
			}
		case EventEnd:
			m.completed++
			if ev.ExitCode != 0 {
				m.failed++
			}
			delete(m.slots, ev.SlotID)
			delete(m.tails, ev.SlotID)
			if m.focusSlot == ev.SlotID {
				m.focusSlot = 0
			}
			entry := recentEntry{
				JobIndex: ev.JobIndex,
				Line:     ev.Line,
				ExitCode: ev.ExitCode,
				Duration: ev.Ended.Sub(ev.Started),
				LogPath:  ev.LogPath,
			}
			m.recent = append([]recentEntry{entry}, m.recent...)
			if len(m.recent) > m.maxRecent {
				m.recent = m.recent[:m.maxRecent]
			}
			m.updateActiveState()
		}
		return m, waitForEvent(m.events)

	case editorDoneMsg:
		if msg.err != nil {
			m.setFlash("✗ editor error: "+msg.err.Error(), true)
		} else {
			m.setFlash("✓ closed "+filepath.Base(msg.path), false)
		}
		return m, nil

	case doneMsg:
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m model) View() string {
	if m.width <= 0 {
		m.width = 100
	}

	var b strings.Builder

	// Header.
	b.WriteString(styleTitle.Render("parallel-each"))
	b.WriteString(styleDim.Render(fmt.Sprintf("   %s", truncate(m.cfg.Template, maxInt(20, m.width-20)))))
	b.WriteString("\n")

	// Progress bar.
	barW := m.width - 40
	if barW < 10 {
		barW = 10
	}
	b.WriteString(m.renderProgress(barW))
	b.WriteString("\n")

	// Summary line.
	elapsed := m.elapsed()
	okCount := m.completed - m.failed
	running := len(m.slots)
	etaStr := "—"
	if eta := m.eta(); eta > 0 {
		etaStr = formatDur(eta)
	}
	b.WriteString(fmt.Sprintf("  %s %d/%d   %s %d   %s %d   %s %d   %s %d   %s %s   %s %s\n",
		styleHeader.Render("done:"), m.completed, m.total,
		styleOK.Render("ok:"), okCount,
		styleFail.Render("fail:"), m.failed,
		styleRunning.Render("running:"), running,
		styleDim.Render("par:"), m.par,
		styleDim.Render("elapsed:"), elapsed,
		styleDim.Render("eta:"), etaStr,
	))
	if m.skipped > 0 {
		b.WriteString(fmt.Sprintf("  %s %d already in result.log (use --fresh to rerun all)\n",
			styleDim.Render("resumed — skipped:"), m.skipped))
	}
	b.WriteString("\n")

	if m.recentMode {
		b.WriteString(m.renderRecentFull())
	} else if m.queueMode {
		b.WriteString(m.renderQueue())
	} else if m.focusSlot != 0 {
		b.WriteString(m.renderFocus())
	} else {
		b.WriteString(m.renderOverview())
	}

	b.WriteString("\n")

	// Transient flash message (e.g., add confirmation / error).
	if m.flashMsg != "" && time.Now().Before(m.flashUntil) {
		if m.flashErr {
			b.WriteString("  " + styleFail.Render(m.flashMsg))
		} else {
			b.WriteString("  " + styleOK.Render(m.flashMsg))
		}
		b.WriteString("\n")
	}

	// Input prompt when adding a new item.
	if m.inputMode {
		label := "add item (append to tail):"
		if m.inputPrepend {
			label = "add item (PREPEND to head):"
		}
		b.WriteString("  ")
		b.WriteString(styleHeader.Render(label))
		b.WriteString(" ")
		b.WriteString(string(m.inputBuf))
		b.WriteString(styleRunning.Render("▌"))
		b.WriteString("\n")
		b.WriteString(styleDim.Render("    tip: paste multiple lines to enqueue them at once"))
		b.WriteString("\n")
		b.WriteString(styleKey.Render("  enter: submit   esc: cancel   ctrl-u: clear"))
		b.WriteString("\n")
		return b.String()
	}

	// Parallelism change: step 1 (type number).
	if m.parInputMode {
		b.WriteString("  ")
		b.WriteString(styleHeader.Render(fmt.Sprintf("set parallelism (current: %d):", m.par)))
		b.WriteString(" ")
		b.WriteString(string(m.parInputBuf))
		b.WriteString(styleRunning.Render("▌"))
		b.WriteString("\n")
		b.WriteString(styleKey.Render("  enter: continue   esc: cancel"))
		b.WriteString("\n")
		return b.String()
	}

	// Other menu.
	if m.otherMenu {
		b.WriteString("  ")
		b.WriteString(styleHeader.Render("other actions:"))
		b.WriteString("\n")
		b.WriteString("    1) Export wrapper script → ")
		b.WriteString(styleDim.Render(resolveExportDir() + "/"))
		b.WriteString("\n\n")
		b.WriteString(styleKey.Render("  select a number, or esc to close"))
		b.WriteString("\n")
		return b.String()
	}

	// Export wrapper: filename input.
	if m.exportInput {
		b.WriteString("  ")
		b.WriteString(styleHeader.Render(fmt.Sprintf("wrapper filename → %s/", m.exportTargetDir)))
		b.WriteString("\n  ")
		b.WriteString(string(m.exportBuf))
		b.WriteString(styleRunning.Render("▌"))
		b.WriteString("\n")
		b.WriteString(styleDim.Render("    the wrapper bakes in -P, --attempt-timeout, -F (absolute), and template; extra args forward via $@"))
		b.WriteString("\n")
		b.WriteString(styleKey.Render("  enter: write   esc: cancel"))
		b.WriteString("\n")
		return b.String()
	}

	// Parallelism change: step 2 (confirm).
	if m.parConfirmMode {
		delta := m.parPending - m.par
		arrow := "↑"
		tail := "A new worker will start immediately."
		if delta < 0 {
			arrow = "↓"
			tail = "Excess workers retire gracefully after their current job — running jobs are never interrupted."
		}
		b.WriteString("  ")
		b.WriteString(styleHeader.Render("confirm parallelism change:"))
		b.WriteString(fmt.Sprintf(" %s  %d → %d\n", arrow, m.par, m.parPending))
		b.WriteString("    ")
		b.WriteString(styleDim.Render(tail))
		b.WriteString("\n")
		b.WriteString(styleKey.Render("  enter: apply   esc: cancel"))
		b.WriteString("\n")
		return b.String()
	}

	if m.stopping {
		if m.forceKillConfirmActive() {
			remaining := int(time.Until(m.forceKillConfirmUntil).Round(time.Second) / time.Second)
			if remaining < 1 {
				remaining = 1
			}
			b.WriteString(styleFail.Render(fmt.Sprintf(
				"  ⚠ force-kill? press q / ctrl-c again within %ds to confirm (running: %d).",
				remaining, running)))
		} else if running > 0 {
			b.WriteString(styleFail.Render(fmt.Sprintf(
				"  stopping… waiting for %d running job(s). press q / ctrl-c to force-kill.", running)))
		} else {
			b.WriteString(styleFail.Render("  stopping…"))
		}
	} else if m.paused {
		b.WriteString(styleFail.Render(fmt.Sprintf(
			"  paused (running: %d)  —  c: resume   q/ctrl-c: stop for good (→ force-kill)", running)))
	} else if m.recentMode {
		b.WriteString(styleKey.Render("  ↑/↓ or j/k: move   pgup/pgdown: page   g/G: top/bottom   enter: open log   esc/r: back"))
	} else if m.queueMode {
		b.WriteString(styleKey.Render("  ↑/↓ or j/k: move   pgup/pgdown: page   g/G: top/bottom   esc/l: back"))
	} else if m.focusSlot != 0 {
		b.WriteString(styleKey.Render("  esc / 0: back   e: open log in $EDITOR   a: add   q: stop"))
	} else if m.completed >= m.total && running == 0 {
		b.WriteString(styleKey.Render("  all done — a/A: add/prepend   p: par   r: recent   l: queue   o: other   q: exit"))
	} else {
		b.WriteString(styleKey.Render("  1-9: focus   a/A: add/prepend   p: par   r: recent   l: queue   o: other   q: stop"))
	}
	b.WriteString("\n")

	return b.String()
}

// renderOverview renders the active slot list (with inline tail lines) plus a
// trailing "recent completions" list.
func (m model) renderOverview() string {
	var b strings.Builder

	b.WriteString(styleHeader.Render("  active slots:"))
	b.WriteString("\n")

	if len(m.slots) == 0 {
		b.WriteString(styleDim.Render("    (idle)"))
		b.WriteString("\n")
	} else {
		ids := sortedSlotIDs(m.slots)
		now := time.Now()
		for _, id := range ids {
			s := m.slots[id]
			dur := now.Sub(s.Started).Round(100 * time.Millisecond)
			retryTag := ""
			if s.Attempt > 1 {
				retryTag = "  " + styleFail.Render(fmt.Sprintf("retry %d/%d", s.Attempt-1, s.MaxAttempts-1))
			}
			available := m.width - 22 - visibleLen(retryTag)
			if available < 10 {
				available = 10
			}
			b.WriteString(fmt.Sprintf("    %s %s %s%s\n",
				styleRunning.Render(fmt.Sprintf("▶ [%d]", id)),
				styleDim.Render(fmt.Sprintf("%6s", formatDur(dur))),
				truncate(s.Line, available),
				retryTag,
			))
			// Inline tail (feature A).
			lines := m.tails[id]
			if m.tailPerSlot > 0 && len(lines) > 0 {
				n := m.tailPerSlot
				if len(lines) < n {
					n = len(lines)
				}
				tailWidth := m.width - 10
				if tailWidth < 20 {
					tailWidth = 20
				}
				for _, ln := range lines[len(lines)-n:] {
					b.WriteString("        ")
					b.WriteString(styleDim.Render("│ "))
					b.WriteString(truncate(ln, tailWidth))
					b.WriteString("\n")
				}
			}
		}
	}
	b.WriteString("\n")

	b.WriteString(styleHeader.Render("  recent:"))
	b.WriteString("\n")
	if len(m.recent) == 0 {
		b.WriteString(styleDim.Render("    (none)"))
		b.WriteString("\n")
	} else {
		limit := m.maxRecent
		if m.height > 0 {
			// Rough estimate of lines already consumed above.
			slotRows := len(m.slots)
			if slotRows == 0 {
				slotRows = 1
			}
			tailRows := 0
			for id := range m.slots {
				if n := len(m.tails[id]); n > 0 {
					if n > m.tailPerSlot {
						n = m.tailPerSlot
					}
					tailRows += n
				}
			}
			used := 8 + slotRows + tailRows + 3
			remaining := m.height - used
			if remaining < 3 {
				remaining = 3
			}
			if remaining < limit {
				limit = remaining
			}
		}
		for i := 0; i < len(m.recent) && i < limit; i++ {
			e := m.recent[i]
			mark := styleOK.Render("✓")
			tail := ""
			if e.ExitCode != 0 {
				mark = styleFail.Render("✗")
				tail = styleFail.Render(fmt.Sprintf(" (exit=%d)", e.ExitCode))
			}
			available := m.width - 24
			if available < 10 {
				available = 10
			}
			b.WriteString(fmt.Sprintf("    %s %s %s %s%s\n",
				mark,
				styleDim.Render(fmt.Sprintf("%04d", e.JobIndex)),
				styleDim.Render(fmt.Sprintf("%6s", formatDur(e.Duration))),
				truncate(e.Line, available),
				tail,
			))
		}
	}

	return b.String()
}

// renderFocus renders a large tail view for the focused slot.
func (m model) renderFocus() string {
	var b strings.Builder
	s, ok := m.slots[m.focusSlot]
	if !ok || !s.Active {
		// Focused slot is no longer active; fall back to overview.
		return m.renderOverview()
	}
	dur := time.Since(s.Started).Round(100 * time.Millisecond)

	b.WriteString(styleHeader.Render(fmt.Sprintf("  focus: slot %d", m.focusSlot)))
	b.WriteString("  ")
	b.WriteString(styleDim.Render(fmt.Sprintf("elapsed %s", formatDur(dur))))
	b.WriteString("\n")
	b.WriteString("    ")
	b.WriteString(styleRunning.Render("▶ "))
	b.WriteString(truncate(s.Line, m.width-8))
	b.WriteString("\n")
	b.WriteString("    ")
	b.WriteString(styleDim.Render(truncate(s.LogPath, m.width-8)))
	b.WriteString("\n\n")

	// Determine how many tail lines fit.
	reservedAbove := 9 // header/progress/summary/focus header lines (approx)
	reservedBelow := 3 // footer
	avail := m.focusTailLines
	if m.height > 0 {
		want := m.height - reservedAbove - reservedBelow
		if want < 3 {
			want = 3
		}
		if want < avail {
			avail = want
		}
	}

	lines := m.tails[m.focusSlot]
	if len(lines) == 0 {
		b.WriteString(styleDim.Render("    (no output yet)"))
		b.WriteString("\n")
		return b.String()
	}
	if len(lines) > avail {
		lines = lines[len(lines)-avail:]
	}
	tailWidth := m.width - 6
	if tailWidth < 20 {
		tailWidth = 20
	}
	for _, ln := range lines {
		b.WriteString("    ")
		b.WriteString(styleDim.Render("│ "))
		b.WriteString(truncate(ln, tailWidth))
		b.WriteString("\n")
	}
	return b.String()
}

// handleInputKey processes keystrokes while the add-item prompt is active.
//
// Enter submits the current buffer as a single item and exits input mode.
// KeyRunes containing newline characters (from a multi-line paste) are split
// on each '\n' / '\r' and submitted one at a time; input mode stays open so
// the user can paste or type more.
func (m model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		line := strings.TrimSpace(string(m.inputBuf))
		m.inputBuf = m.inputBuf[:0]
		m.inputMode = false
		if line == "" {
			return m, nil
		}
		m.submitLine(line, false)
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.inputMode = false
		m.inputBuf = m.inputBuf[:0]
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if n := len(m.inputBuf); n > 0 {
			m.inputBuf = m.inputBuf[:n-1]
		}
		return m, nil
	case tea.KeyCtrlU:
		m.inputBuf = m.inputBuf[:0]
		return m, nil
	case tea.KeyRunes:
		if !containsNewline(msg.Runes) {
			m.inputBuf = append(m.inputBuf, msg.Runes...)
			return m, nil
		}
		m = m.consumePastedRunes(msg.Runes)
		return m, nil
	case tea.KeySpace:
		m.inputBuf = append(m.inputBuf, ' ')
		return m, nil
	}
	return m, nil
}

func containsNewline(rs []rune) bool {
	for _, r := range rs {
		if r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}

// consumePastedRunes splits the input on '\n' / '\r', submitting each non-
// empty segment via Enqueue / EnqueueFront. The final segment (after the
// last newline) stays in inputBuf so the user can finish typing it. Input
// mode is preserved. In prepend mode the completed lines are submitted in
// REVERSE order so the final queue head matches the paste's original order.
func (m model) consumePastedRunes(rs []rune) model {
	var completed []string
	for _, r := range rs {
		if r == '\n' || r == '\r' {
			line := strings.TrimSpace(string(m.inputBuf))
			m.inputBuf = m.inputBuf[:0]
			if line != "" {
				completed = append(completed, line)
			}
		} else {
			m.inputBuf = append(m.inputBuf, r)
		}
	}

	order := completed
	if m.inputPrepend {
		order = make([]string, len(completed))
		for i, s := range completed {
			order[len(completed)-1-i] = s
		}
	}

	var ok, dup, fail int
	for _, line := range order {
		switch m.submitLine(line, true) {
		case submitOK:
			ok++
		case submitDuplicate:
			dup++
		case submitError:
			fail++
		}
	}
	return m.flashBatch(ok, dup, fail)
}

type submitResult int

const (
	submitOK submitResult = iota
	submitDuplicate
	submitError
)

// submitLine enqueues one line into the runner. If batch is true the caller
// aggregates flash messages itself; otherwise an individual flash is set.
// Uses runner.EnqueueFront when the input mode was opened for prepend.
func (m *model) submitLine(line string, batch bool) submitResult {
	var err error
	if m.inputPrepend {
		err = m.runner.EnqueueFront(line)
	} else {
		err = m.runner.Enqueue(line)
	}
	if err == nil {
		m.addedLive++
		m.total++
		m.updateActiveState()
		if !batch {
			verb := "added"
			if m.inputPrepend {
				verb = "prepended"
			}
			m.setFlash(fmt.Sprintf("✓ %s: %s", verb, truncate(line, 60)), false)
		}
		return submitOK
	}
	dup := strings.Contains(err.Error(), "duplicate")
	if !batch {
		m.setFlash("✗ "+err.Error(), true)
	}
	if dup {
		return submitDuplicate
	}
	return submitError
}

func (m model) flashBatch(ok, dup, fail int) model {
	if ok+dup+fail == 0 {
		return m
	}
	parts := []string{}
	if ok > 0 {
		parts = append(parts, fmt.Sprintf("✓ added %d", ok))
	}
	if dup > 0 {
		parts = append(parts, fmt.Sprintf("↻ duplicate %d", dup))
	}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("✗ failed %d", fail))
	}
	m.setFlash(strings.Join(parts, ", "), fail > 0)
	return m
}

// handleParInputKey captures digits for the new parallelism value.
// Enter validates and transitions to confirmation; Esc cancels.
func (m model) handleParInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		raw := strings.TrimSpace(string(m.parInputBuf))
		m.parInputMode = false
		m.parInputBuf = m.parInputBuf[:0]
		if raw == "" {
			return m, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			m.setFlash(fmt.Sprintf("✗ invalid parallelism: %q", raw), true)
			return m, nil
		}
		if n == m.par {
			m.setFlash(fmt.Sprintf("parallelism already %d — no change", n), false)
			return m, nil
		}
		m.parPending = n
		m.parConfirmMode = true
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.parInputMode = false
		m.parInputBuf = m.parInputBuf[:0]
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if n := len(m.parInputBuf); n > 0 {
			m.parInputBuf = m.parInputBuf[:n-1]
		}
		return m, nil
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r >= '0' && r <= '9' {
				m.parInputBuf = append(m.parInputBuf, r)
			}
		}
		return m, nil
	}
	return m, nil
}

// handleParConfirmKey asks the user to confirm (Enter) or cancel (Esc) a
// pending parallelism change.
func (m model) handleParConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		old := m.par
		m.runner.SetParallelism(m.parPending)
		m.par = m.parPending
		m.parConfirmMode = false
		if m.par > old {
			m.setFlash(fmt.Sprintf("↑ parallelism: %d → %d (new worker started)", old, m.par), false)
		} else {
			m.setFlash(fmt.Sprintf("↓ parallelism: %d → %d (excess workers retire gracefully)", old, m.par), false)
		}
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.parConfirmMode = false
		return m, nil
	}
	return m, nil
}

// handleOtherMenuKey handles the numbered menu opened with 'o'. Currently
// option 1 is the only entry (export wrapper); more can be added later.
func (m model) handleOtherMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "o", "q":
		m.otherMenu = false
		return m, nil
	case "1":
		m.otherMenu = false
		m.exportInput = true
		m.exportBuf = m.exportBuf[:0]
		m.exportTargetDir = resolveExportDir()
		return m, nil
	}
	return m, nil
}

// handleExportKey handles the filename-input prompt for the wrapper export.
func (m model) handleExportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		name := strings.TrimSpace(string(m.exportBuf))
		m.exportInput = false
		m.exportBuf = m.exportBuf[:0]
		if name == "" {
			return m, nil
		}
		if strings.ContainsAny(name, `/\`) {
			m.setFlash("✗ wrapper name cannot contain '/' or '\\'", true)
			return m, nil
		}
		path := filepath.Join(m.exportTargetDir, name)
		if err := writeWrapper(path, m.cfg); err != nil {
			m.setFlash("✗ export failed: "+err.Error(), true)
		} else {
			m.setFlash("✓ wrote "+path, false)
		}
		return m, nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.exportInput = false
		m.exportBuf = m.exportBuf[:0]
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if n := len(m.exportBuf); n > 0 {
			m.exportBuf = m.exportBuf[:n-1]
		}
		return m, nil
	case tea.KeyCtrlU:
		m.exportBuf = m.exportBuf[:0]
		return m, nil
	case tea.KeyRunes:
		// Allow basic filename-safe characters.
		for _, r := range msg.Runes {
			if r == '/' || r == '\\' || r == 0 {
				continue
			}
			m.exportBuf = append(m.exportBuf, r)
		}
		return m, nil
	case tea.KeySpace:
		// Spaces are unusual in wrapper names but allow them; the user can
		// quote the filename when invoking.
		m.exportBuf = append(m.exportBuf, ' ')
		return m, nil
	}
	return m, nil
}

// handleQueueKey processes keystrokes while the queue (pending) view is open.
func (m model) handleQueueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "l", "q":
		m.queueMode = false
		return m, nil
	case "ctrl+c":
		m.queueMode = false
		if !m.stopping {
			m.stopping = true
			m.runner.RequestStop()
		} else {
			m.runner.ForceKill()
		}
		return m, nil
	}
	m.queueList.handleNavKey(msg.String(), len(m.queueSnapshot), m.recentPageSize())
	return m, nil
}

// handleRecentKey processes keystrokes while the full recent view is open.
func (m model) handleRecentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		if len(m.recent) == 0 {
			return m, nil
		}
		e := m.recent[m.recentList.cursor]
		if e.LogPath == "" {
			m.setFlash("✗ no log path for this entry", true)
			return m, nil
		}
		return m, openEditorCmd(e.LogPath)
	}

	switch msg.String() {
	case "esc", "r", "q":
		m.recentMode = false
		m.recentList.reset()
		return m, nil
	case "ctrl+c":
		m.recentMode = false
		if !m.stopping {
			m.stopping = true
			m.runner.RequestStop()
		} else {
			m.runner.ForceKill()
		}
		return m, nil
	}
	m.recentList.handleNavKey(msg.String(), len(m.recent), m.recentPageSize())
	return m, nil
}

// recentPageSize returns how many recent rows fit in the content area of the
// full-view screen.
func (m model) recentPageSize() int {
	n := m.height - 6 // header + title + footer
	if n < 5 {
		n = 5
	}
	return n
}

// setFlash records a transient message shown above the footer. Pointer
// receiver intentionally not used — model is a value type in Bubble Tea, and
// callers re-assign the returned model.
func (m *model) setFlash(msg string, isErr bool) {
	m.flashMsg = msg
	m.flashErr = isErr
	m.flashUntil = time.Now().Add(3 * time.Second)
}

// renderRecentFull renders a scrollable list of all recorded completions
// (newest first). `recentScroll` is the index of the topmost visible row.
func (m model) renderRecentFull() string {
	var b strings.Builder
	total := len(m.recent)
	okN, failN := 0, 0
	for _, e := range m.recent {
		if e.ExitCode == 0 {
			okN++
		} else {
			failN++
		}
	}

	b.WriteString(styleHeader.Render(fmt.Sprintf("  recent (full) — %d entries", total)))
	b.WriteString("  ")
	b.WriteString(styleOK.Render(fmt.Sprintf("ok %d", okN)))
	b.WriteString("  ")
	b.WriteString(styleFail.Render(fmt.Sprintf("fail %d", failN)))
	b.WriteString("\n\n")

	if total == 0 {
		b.WriteString(styleDim.Render("    (no completions yet)"))
		b.WriteString("\n")
		return b.String()
	}

	pageSize := m.recentPageSize()
	start := m.recentList.scroll
	if start > total-1 {
		start = total - 1
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	available := m.width - 26
	if available < 10 {
		available = 10
	}

	for i := start; i < end; i++ {
		e := m.recent[i]
		mark := styleOK.Render("✓")
		tail := ""
		if e.ExitCode != 0 {
			mark = styleFail.Render("✗")
			tail = styleFail.Render(fmt.Sprintf(" (exit=%d)", e.ExitCode))
		}
		pointer := "  "
		if i == m.recentList.cursor {
			pointer = styleRunning.Render("▶ ")
		}
		b.WriteString(fmt.Sprintf("  %s%s %s %s %s%s\n",
			pointer,
			mark,
			styleDim.Render(fmt.Sprintf("%04d", e.JobIndex)),
			styleDim.Render(fmt.Sprintf("%7s", formatDur(e.Duration))),
			truncate(e.Line, available),
			tail,
		))
	}

	// Scrollbar / position indicator
	if total > 0 {
		b.WriteString("\n    ")
		b.WriteString(styleDim.Render(fmt.Sprintf("showing %d–%d of %d", start+1, end, total)))
		b.WriteString("\n")
	}

	return b.String()
}

// renderQueue renders a scrollable list of pending items.
func (m model) renderQueue() string {
	var b strings.Builder
	total := len(m.queueSnapshot)
	b.WriteString(styleHeader.Render(fmt.Sprintf("  queue (pending) — %d item(s)", total)))
	b.WriteString("\n\n")
	if total == 0 {
		b.WriteString(styleDim.Render("    (empty)"))
		b.WriteString("\n")
		return b.String()
	}
	pageSize := m.recentPageSize()
	start := m.queueList.scroll
	end := start + pageSize
	if end > total {
		end = total
	}
	available := m.width - 10
	if available < 10 {
		available = 10
	}
	for i := start; i < end; i++ {
		line := m.queueSnapshot[i]
		pointer := "  "
		if i == m.queueList.cursor {
			pointer = styleRunning.Render("▶ ")
		}
		b.WriteString(fmt.Sprintf("  %s%s %s\n",
			pointer,
			styleDim.Render(fmt.Sprintf("%04d", i+1)),
			truncate(line, available),
		))
	}
	b.WriteString("\n    ")
	b.WriteString(styleDim.Render(fmt.Sprintf("showing %d–%d of %d", start+1, end, total)))
	b.WriteString("\n")
	return b.String()
}

func sortedSlotIDs(m map[int]slotState) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// eta returns an estimate of remaining wall-clock time based on the average
// duration of recent completions. Returns 0 when no data is available.
func (m model) eta() time.Duration {
	if m.completed == 0 || m.completed >= m.total {
		return 0
	}
	var totalDur time.Duration
	count := 0
	for _, r := range m.recent {
		totalDur += r.Duration
		count++
	}
	if count == 0 {
		return 0
	}
	avg := totalDur / time.Duration(count)
	remaining := m.total - m.completed
	par := m.cfg.Parallelism
	if par <= 0 || par > remaining {
		par = remaining
	}
	if par == 0 {
		return 0
	}
	return time.Duration(remaining) * avg / time.Duration(par)
}

func (m model) renderProgress(width int) string {
	frac := 0.0
	if m.total > 0 {
		frac = float64(m.completed) / float64(m.total)
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(float64(width) * frac)
	if filled > width {
		filled = width
	}
	pct := int(frac * 100)
	return fmt.Sprintf("  %s%s %3d%%",
		styleBar.Render(strings.Repeat("█", filled)),
		styleBarBg.Render(strings.Repeat("░", width-filled)),
		pct)
}

// runTUI drives the TUI and blocks until jobs are done (or user quits).
func runTUI(ctx context.Context, cfg Config, lines []string, skipped int, processed []string) int {
	r := NewRunner(cfg, lines)
	r.SetLive(true) // enable interactive Enqueue via the TUI 'a' key
	r.SeedDedup(processed)
	if err := r.Start(ctx); err != nil {
		fmt.Printf("error: %v\n", err)
		return 1
	}

	mdl := newModel(cfg, len(lines), r.Events(), r, skipped)
	p := tea.NewProgram(mdl, tea.WithAltScreen())

	// Parent context cancel (from external SIGTERM, etc.) triggers force-kill.
	go func() {
		<-ctx.Done()
		r.ForceKill()
	}()

	finalMsg, err := p.Run()
	if err != nil {
		r.ForceKill()
		fmt.Printf("tui error: %v\n", err)
		return 1
	}

	// Drain any remaining events (should already be done, but be safe).
	for range r.Events() {
	}

	finalModel, _ := finalMsg.(model)
	if finalModel.failed > 0 {
		fmt.Printf("summary: %d/%d failed (logs: %s/)\n", finalModel.failed, finalModel.total, logDir)
		return 1
	}
	fmt.Printf("summary: all %d ok (logs: %s/)\n", finalModel.total, logDir)
	return 0
}
