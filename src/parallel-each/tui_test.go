package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello w…"},
		{"日本語テスト", 4, "日本語…"},
		{"abc", 0, ""},
		{"abc", 1, "a"},
		{"abc", 2, "a…"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestFormatDur(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{999 * time.Millisecond, "999ms"},
		{1 * time.Second, "1.0s"},
		{1500 * time.Millisecond, "1.5s"},
		{59 * time.Second, "59.0s"},
		{60 * time.Second, "1m00s"},
		{125 * time.Second, "2m05s"},
	}
	for _, c := range cases {
		if got := formatDur(c.in); got != c.want {
			t.Errorf("formatDur(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// First q triggers graceful stop; second q escalates to force-kill.
func TestTUIModelTwoStageShutdown(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10; echo {item}`}
	r := NewRunner(cfg, []string{"a", "b"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 2, r.Events(), r, 0)

	// Press q once: graceful stop requested.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m2 := updated.(model)
	if !m2.stopping {
		t.Fatal("after 1st q, stopping should be true")
	}
	// stopCtx should be cancelled; killCtx still live.
	select {
	case <-r.stopCtx.Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stopCtx not cancelled after 1st q")
	}
	select {
	case <-r.killCtx.Done():
		t.Fatal("killCtx should NOT be cancelled after 1st q")
	default:
	}

	// Press q again: escalate to force-kill.
	m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	select {
	case <-r.killCtx.Done():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("killCtx not cancelled after 2nd q")
	}

	// Wait for runner to finish draining.
	select {
	case <-eventsDrained(r.Events()):
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not drain after force kill")
	}
}

// Ctrl-C and esc should behave the same as q.
func TestTUIModelCtrlCEquivalent(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10; echo {item}`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 1, r.Events(), r, 0)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !updated.(model).stopping {
		t.Fatal("ctrl+c should trigger graceful stop")
	}
}

func eventsDrained(ch <-chan Event) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	return done
}

// Pressing a digit key focuses the matching active slot; pressing it again
// unfocuses; '0' / esc exits focus.
func TestTUIModelFocusKeys(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 10; echo {item}`}
	r := NewRunner(cfg, []string{"a", "b"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 2, r.Events(), r, 0)
	// Seed two active slots.
	m.slots[1] = slotState{Active: true, JobIndex: 1, Line: "a", LogPath: "/tmp/a.log"}
	m.slots[2] = slotState{Active: true, JobIndex: 2, Line: "b", LogPath: "/tmp/b.log"}

	// '1' -> focus slot 1
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if u.(model).focusSlot != 1 {
		t.Fatalf("focusSlot = %d, want 1", u.(model).focusSlot)
	}
	m = u.(model)

	// '1' again -> toggle off
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if u.(model).focusSlot != 0 {
		t.Fatalf("focusSlot = %d, want 0 after toggle", u.(model).focusSlot)
	}
	m = u.(model)

	// '2' -> focus slot 2
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = u.(model)
	if m.focusSlot != 2 {
		t.Fatalf("focusSlot = %d, want 2", m.focusSlot)
	}

	// '0' -> clear focus
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = u.(model)
	if m.focusSlot != 0 {
		t.Fatalf("focusSlot = %d, want 0 after '0'", m.focusSlot)
	}

	// Focus slot 2 again, then esc -> clear (not shutdown)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = u.(model)
	if m.focusSlot != 2 {
		t.Fatalf("pre-esc focus = %d, want 2", m.focusSlot)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.focusSlot != 0 {
		t.Fatal("esc in focus should clear focus")
	}
	if m.stopping {
		t.Fatal("esc in focus should NOT trigger shutdown")
	}

	// esc again (no focus) -> triggers shutdown
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if !m.stopping {
		t.Fatal("esc outside focus should trigger shutdown")
	}
}

// Digit for a non-existent slot is ignored.
func TestTUIModelFocusIgnoresInactive(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10; echo {item}`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 1, r.Events(), r, 0)
	m.slots[1] = slotState{Active: true, JobIndex: 1, Line: "a"}

	// Pressing '5' when only slot 1 exists should not set focus.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	if u.(model).focusSlot != 0 {
		t.Errorf("focusSlot = %d, want 0 (slot 5 doesn't exist)", u.(model).focusSlot)
	}
}

// When a focused slot ends, focus should auto-clear.
func TestTUIModelFocusClearsOnEnd(t *testing.T) {
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
	defer r.ForceKill()

	m := newModel(cfg, 1, r.Events(), r, 0)
	m.slots[1] = slotState{Active: true, JobIndex: 1, Line: "a"}
	m.focusSlot = 1

	u, _ := m.Update(eventMsg(Event{
		Kind: EventEnd, SlotID: 1, JobIndex: 1, Line: "a",
		Started: time.Now().Add(-time.Second), Ended: time.Now(),
	}))
	if u.(model).focusSlot != 0 {
		t.Error("focus should clear when focused slot ends")
	}
}

// Pressing 'a' enters input mode; typing + Enter Enqueues a new item.
func TestTUIModelAddItemFlow(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5; echo {item}`}
	r := NewRunner(cfg, []string{"a"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)

	// Press 'a' to enter input mode.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)
	if !m.inputMode {
		t.Fatal("expected inputMode after pressing 'a'")
	}

	// Type "new-item" character by character.
	for _, r := range "new-item" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(model)
	}
	if string(m.inputBuf) != "new-item" {
		t.Fatalf("inputBuf = %q, want %q", string(m.inputBuf), "new-item")
	}

	// Submit with Enter.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.inputMode {
		t.Fatal("inputMode should be false after Enter")
	}
	if m.addedLive != 1 {
		t.Errorf("addedLive = %d, want 1", m.addedLive)
	}
	if m.total != 2 {
		t.Errorf("total = %d, want 2 (original + added)", m.total)
	}
	if m.flashErr {
		t.Errorf("unexpected error flash: %q", m.flashMsg)
	}
	if r.AddedCount() != 1 {
		t.Errorf("runner.AddedCount = %d, want 1", r.AddedCount())
	}
}

// Backspace and Esc behave correctly in input mode.
func TestTUIModelInputEditing(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"x"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)

	for _, ch := range "abc" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}

	// Backspace
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = u.(model)
	if string(m.inputBuf) != "ab" {
		t.Errorf("after backspace: %q, want %q", string(m.inputBuf), "ab")
	}

	// Ctrl+U clears
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = u.(model)
	if len(m.inputBuf) != 0 {
		t.Errorf("after ctrl-u buf = %q, want empty", string(m.inputBuf))
	}

	// Esc cancels without submitting
	for _, ch := range "zzz" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.inputMode {
		t.Error("esc should exit input mode")
	}
	if r.AddedCount() != 0 {
		t.Errorf("nothing should be added; got %d", r.AddedCount())
	}
}

// Duplicate submission yields an error flash.
func TestTUIModelAddItemDuplicate(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"alpha"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)
	for _, ch := range "alpha" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if !m.flashErr {
		t.Error("expected error flash on duplicate")
	}
	if m.total != 1 {
		t.Errorf("total = %d, want 1 (duplicate rejected)", m.total)
	}
}

// 'r' enters the full recent view; scrolling works; esc/r exits.
func TestTUIModelRecentView(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"x"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	// Seed 50 completions.
	for i := 1; i <= 50; i++ {
		m.recent = append(m.recent, recentEntry{
			JobIndex: i, Line: "item", ExitCode: 0, Duration: time.Second,
		})
	}
	m.height = 30 // pageSize ~= 24

	// Pressing 'r' enters recent mode.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = u.(model)
	if !m.recentMode {
		t.Fatal("expected recentMode after pressing r")
	}

	// Down arrow scrolls by 1.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = u.(model)
	if m.recentScroll != 1 {
		t.Errorf("scroll after down = %d, want 1", m.recentScroll)
	}

	// 'j' also scrolls down.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = u.(model)
	if m.recentScroll != 2 {
		t.Errorf("scroll after j = %d, want 2", m.recentScroll)
	}

	// 'G' goes to the end.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = u.(model)
	if m.recentScroll != 49 {
		t.Errorf("scroll after G = %d, want 49", m.recentScroll)
	}

	// 'g' returns to top.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = u.(model)
	if m.recentScroll != 0 {
		t.Errorf("scroll after g = %d, want 0", m.recentScroll)
	}

	// esc exits.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.recentMode {
		t.Fatal("esc should exit recentMode")
	}
}

// 'r' is a no-op when there are no completions.
func TestTUIModelRecentIgnoredWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `echo {item}`}
	r := NewRunner(cfg, []string{"x"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if u.(model).recentMode {
		t.Error("r should be ignored when recent is empty")
	}
}

// Pasting multi-line text auto-submits each complete line; trailing partial
// line (without newline) stays in the buffer.
func TestTUIModelMultilinePasteSubmitsEachLine(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"existing"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	// Open input mode.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)

	// Simulate a paste with 3 newline-terminated lines + one trailing partial.
	paste := []rune("alpha\nbeta gamma\nhttps://example.com/x\npartial")
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: paste})
	m = u.(model)

	if !m.inputMode {
		t.Fatal("input mode should remain active after multi-line paste")
	}
	if string(m.inputBuf) != "partial" {
		t.Errorf("trailing partial = %q, want %q", string(m.inputBuf), "partial")
	}
	if m.addedLive != 3 {
		t.Errorf("addedLive = %d, want 3", m.addedLive)
	}
	if r.AddedCount() != 3 {
		t.Errorf("runner.AddedCount = %d, want 3", r.AddedCount())
	}
	if m.total != 4 { // 1 original + 3 added
		t.Errorf("total = %d, want 4", m.total)
	}
}

// Pasted lines mixed with duplicates report aggregate counts.
func TestTUIModelMultilinePasteMixedResults(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"existing"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)

	// "existing" is a duplicate; "" (blank between \n\n) is skipped.
	paste := []rune("new1\nexisting\n\nnew2\n")
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: paste})
	m = u.(model)

	if m.addedLive != 2 {
		t.Errorf("addedLive = %d, want 2 (new1, new2)", m.addedLive)
	}
	if !strings.Contains(m.flashMsg, "added 2") {
		t.Errorf("flash = %q, want '... added 2 ...'", m.flashMsg)
	}
	if !strings.Contains(m.flashMsg, "duplicate 1") {
		t.Errorf("flash = %q, want '... duplicate 1 ...'", m.flashMsg)
	}
}

// CRLF line endings are handled the same as LF.
func TestTUIModelMultilinePasteCRLF(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"existing"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = u.(model)

	paste := []rune("first\r\nsecond\r\n")
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: paste})
	m = u.(model)

	if r.AddedCount() != 2 {
		t.Errorf("AddedCount = %d, want 2", r.AddedCount())
	}
}

func TestContainsNewline(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hello", false},
		{"hello\nworld", true},
		{"hello\r", true},
		{"", false},
		{"\n", true},
	}
	for _, c := range cases {
		if got := containsNewline([]rune(c.in)); got != c.want {
			t.Errorf("containsNewline(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestModelETA(t *testing.T) {
	cases := []struct {
		name      string
		total     int
		completed int
		par       int
		recent    []recentEntry
		wantZero  bool
		wantRange [2]time.Duration // inclusive bounds; ignored if wantZero
	}{
		{
			name:     "no completions yet -> 0",
			total:    10,
			wantZero: true,
		},
		{
			name:      "complete -> 0",
			total:     5,
			completed: 5,
			recent:    []recentEntry{{Duration: time.Second}},
			wantZero:  true,
		},
		{
			name:      "half done, P=2, avg 1s, 5 left -> ~2.5s",
			total:     10,
			completed: 5,
			par:       2,
			recent: []recentEntry{
				{Duration: time.Second},
				{Duration: time.Second},
			},
			wantRange: [2]time.Duration{2 * time.Second, 3 * time.Second},
		},
		{
			name:      "remaining < parallelism clamps par",
			total:     10,
			completed: 9,
			par:       4,
			recent: []recentEntry{
				{Duration: 2 * time.Second},
			},
			// remaining=1, par clamped to 1 -> ETA ~= avg = 2s
			wantRange: [2]time.Duration{time.Second + 500*time.Millisecond, 2*time.Second + 500*time.Millisecond},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := model{
				cfg:       Config{Parallelism: c.par},
				total:     c.total,
				completed: c.completed,
				recent:    c.recent,
			}
			got := m.eta()
			if c.wantZero {
				if got != 0 {
					t.Errorf("want 0, got %v", got)
				}
				return
			}
			if got < c.wantRange[0] || got > c.wantRange[1] {
				t.Errorf("eta = %v, want in [%v, %v]", got, c.wantRange[0], c.wantRange[1])
			}
		})
	}
}

func TestVisibleLen(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"hello", 5},
		{"\x1b[31mred\x1b[0m", 3},
		{"\x1b[1;38;5;196mstyled\x1b[0mplain", 11},
		{"", 0},
	}
	for _, c := range cases {
		if got := visibleLen(c.in); got != c.want {
			t.Errorf("visibleLen(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMaxInt(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 2, 2},
		{5, 3, 5},
		{-1, -5, -1},
		{7, 7, 7},
	}
	for _, c := range cases {
		if got := maxInt(c.a, c.b); got != c.want {
			t.Errorf("maxInt(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
