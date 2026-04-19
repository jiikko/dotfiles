package main

import (
	"context"
	"fmt"
	"sort"
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
}

type model struct {
	cfg       Config
	total     int
	completed int
	failed    int
	startedAt time.Time
	slots     map[int]slotState
	recent    []recentEntry
	maxRecent int

	// Tails captured from each active slot's log file.
	tails          map[int][]string // slotID -> last N lines
	tailPerSlot    int              // inline tail lines in overview (feature A)
	focusSlot      int              // slotID under focus; 0 = overview (feature B)
	focusTailLines int              // tail lines in focus view

	// Resume state: how many input rows were skipped because they were already
	// present in result.log. Purely informational — the total count already
	// excludes these.
	skipped int

	width    int
	height   int
	events   <-chan Event
	done     bool
	runner   *Runner
	stopping bool // true once a graceful stop has been requested
}

func newModel(cfg Config, total int, events <-chan Event, runner *Runner, skipped int) model {
	return model{
		cfg:            cfg,
		total:          total,
		startedAt:      time.Now(),
		slots:          make(map[int]slotState),
		maxRecent:      20,
		tails:          make(map[int][]string),
		tailPerSlot:    2,
		focusTailLines: 20,
		events:         events,
		runner:         runner,
		width:          100,
		skipped:        skipped,
	}
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
		case "0":
			m.focusSlot = 0
			return m, nil
		case "esc":
			// In focus mode, esc exits focus rather than triggering shutdown.
			if m.focusSlot != 0 {
				m.focusSlot = 0
				return m, nil
			}
			fallthrough
		case "q", "ctrl+c":
			if !m.stopping {
				m.stopping = true
				m.runner.RequestStop()
			} else {
				m.runner.ForceKill()
			}
			return m, nil
		}
		return m, nil

	case tickMsg:
		if m.done {
			return m, nil
		}
		// Refresh tails for active slots, then schedule the next tick.
		lines := m.tailPerSlot
		if m.focusSlot != 0 {
			lines = m.focusTailLines
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
			}
			m.recent = append([]recentEntry{entry}, m.recent...)
			if len(m.recent) > m.maxRecent {
				m.recent = m.recent[:m.maxRecent]
			}
		}
		return m, waitForEvent(m.events)

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
	elapsed := time.Since(m.startedAt).Round(time.Second)
	okCount := m.completed - m.failed
	running := len(m.slots)
	etaStr := "—"
	if eta := m.eta(); eta > 0 {
		etaStr = formatDur(eta)
	}
	b.WriteString(fmt.Sprintf("  %s %d/%d   %s %d   %s %d   %s %d   %s %s   %s %s\n",
		styleHeader.Render("done:"), m.completed, m.total,
		styleOK.Render("ok:"), okCount,
		styleFail.Render("fail:"), m.failed,
		styleRunning.Render("running:"), running,
		styleDim.Render("elapsed:"), elapsed,
		styleDim.Render("eta:"), etaStr,
	))
	if m.skipped > 0 {
		b.WriteString(fmt.Sprintf("  %s %d already in result.log (use --fresh to rerun all)\n",
			styleDim.Render("resumed — skipped:"), m.skipped))
	}
	b.WriteString("\n")

	if m.focusSlot != 0 {
		b.WriteString(m.renderFocus())
	} else {
		b.WriteString(m.renderOverview())
	}

	b.WriteString("\n")
	if m.stopping {
		if running > 0 {
			b.WriteString(styleFail.Render(fmt.Sprintf(
				"  stopping… waiting for %d running job(s). press q / ctrl-c again to force-kill.", running)))
		} else {
			b.WriteString(styleFail.Render("  stopping…"))
		}
	} else if m.focusSlot != 0 {
		b.WriteString(styleKey.Render("  esc / 0: back to overview   q / ctrl-c: stop"))
	} else {
		b.WriteString(styleKey.Render("  1-9: focus slot   q / ctrl-c: stop (twice = force-kill)"))
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

// visibleLen returns the visible column width of s, ignoring ANSI escape
// sequences from lipgloss styling.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
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

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func formatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

// runTUI drives the TUI and blocks until jobs are done (or user quits).
func runTUI(ctx context.Context, cfg Config, lines []string, skipped int) int {
	r := NewRunner(cfg, lines)
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
