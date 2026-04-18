package main

import (
	"context"
	"os"
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

	m := newModel(cfg, 2, r.Events(), r)

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

	m := newModel(cfg, 1, r.Events(), r)
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

	m := newModel(cfg, 2, r.Events(), r)
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

	m := newModel(cfg, 1, r.Events(), r)
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

	m := newModel(cfg, 1, r.Events(), r)
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
