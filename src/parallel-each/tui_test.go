package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// Shutdown requires the user to type the literal word "quit". The staged
// semantics are preserved:
//   1st "quit" -> paused (reversible)
//   2nd "quit" -> stopping (final)
//   3rd "quit" -> force-kill confirm armed
//   4th "quit" (in window) -> force-kill
func TestTUIModelThreeStageShutdown(t *testing.T) {
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
	typeQuit := func() {
		for _, ch := range "quit" {
			u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
			m = u.(model)
		}
	}

	// Single 'q' must NOT trigger anything.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = u.(model)
	if m.paused || m.stopping {
		t.Fatal("single 'q' should not initiate shutdown")
	}

	// Breaking the sequence resets the buffer.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = u.(model)
	if m.quitBuffer != "" {
		t.Fatalf("quitBuffer should reset after non-quit key, got %q", m.quitBuffer)
	}

	// 1st "quit" -> pause.
	typeQuit()
	if !m.paused {
		t.Fatal(`1st "quit" should set paused`)
	}

	// 2nd "quit" -> stop.
	typeQuit()
	if !m.stopping {
		t.Fatal(`2nd "quit" should set stopping`)
	}
	select {
	case <-r.stopCtx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stopCtx not cancelled after 2nd quit")
	}
	select {
	case <-r.killCtx.Done():
		t.Fatal("killCtx should NOT be cancelled yet")
	default:
	}

	// 3rd "quit" -> arm confirmation.
	typeQuit()
	if !m.forceKillConfirmActive() {
		t.Fatal(`3rd "quit" should arm force-kill`)
	}

	// 4th "quit" -> force-kill.
	typeQuit()
	select {
	case <-r.killCtx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("killCtx not cancelled after 4th quit")
	}

	select {
	case <-eventsDrained(r.Events()):
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not drain after force kill")
	}
}

// If the user waits past the 3s window, the force-kill arm is silently
// cancelled and the runner is not killed.
func TestTUIModelForceKillConfirmExpires(t *testing.T) {
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
	// Walk through pause → stop → arm confirm by typing "quit" three times.
	for i := 0; i < 3; i++ {
		for _, ch := range "quit" {
			u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
			m = u.(model)
		}
	}
	if !m.forceKillConfirmActive() {
		t.Fatal(`expected confirm to be active after 3 "quit" sequences`)
	}
	// Simulate time passing past the window.
	m.forceKillConfirmUntil = time.Now().Add(-time.Second)
	if m.forceKillConfirmActive() {
		t.Fatal("confirm should have expired")
	}
	// Trigger a tick — the expired field should be zeroed.
	u, _ := m.Update(tickMsg(time.Now()))
	m = u.(model)
	if !m.forceKillConfirmUntil.IsZero() {
		t.Errorf("tick should clear expired forceKillConfirmUntil, got %v", m.forceKillConfirmUntil)
	}
	// killCtx was never triggered.
	select {
	case <-r.killCtx.Done():
		t.Fatal("killCtx should not be cancelled on expiry")
	default:
	}
}

// 'c' cancels a pending pause and resumes dispatching.
func TestTUIModelCancelPauseWithC(t *testing.T) {
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

	// Pause via "quit".
	for _, ch := range "quit" {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	if !m.paused || !r.IsPaused() {
		t.Fatal(`paused should be true after "quit"`)
	}

	// Resume via c.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = u.(model)
	if m.paused {
		t.Fatal("paused should be false after c")
	}
	if r.IsPaused() {
		t.Fatal("runner should not be paused after c")
	}
	if m.stopping {
		t.Fatal("stopping should still be false")
	}
}

// Any non-quit keystroke resets the in-progress quit buffer.
func TestTUIModelQuitBufferResets(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 1, r.Events(), r, 0)

	send := func(s string) {
		for _, ch := range s {
			u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
			m = u.(model)
		}
	}

	send("qu")
	if m.quitBuffer != "qu" {
		t.Fatalf("buffer = %q, want %q", m.quitBuffer, "qu")
	}
	send("x") // random key resets
	if m.quitBuffer != "" {
		t.Fatalf("buffer = %q, want empty after non-quit key", m.quitBuffer)
	}
	if m.paused {
		t.Fatal("paused must not be set when only partial quit was typed")
	}
	// Now typing "quit" fully should work (buffer was reset cleanly).
	send("quit")
	if !m.paused {
		t.Fatal("full quit should pause")
	}
}

// Typing out-of-order letters (e.g., 'q' then 'i') resets the buffer but
// re-starts if the new letter is 'q' again.
func TestTUIModelQuitBufferRestartsOnQ(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r.ForceKill()

	m := newModel(cfg, 1, r.Events(), r, 0)
	send := func(s string) {
		for _, ch := range s {
			u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
			m = u.(model)
		}
	}

	send("qi") // 'q' starts, 'i' breaks the sequence
	if m.quitBuffer != "" {
		t.Errorf("buffer = %q, want empty", m.quitBuffer)
	}
	if m.paused {
		t.Fatal("partial sequence must not pause")
	}

	send("qquit") // re-starts on 'q'
	if !m.paused {
		t.Fatal("second full quit should pause")
	}
}

// Single Ctrl-C is ignored with a flash; never starts shutdown on its own.
func TestTUIModelCtrlCIgnored(t *testing.T) {
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
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = u.(model)
	if m.paused || m.stopping {
		t.Fatal("ctrl+c must not initiate shutdown")
	}
	if !strings.Contains(m.flashMsg, "ignored") {
		t.Errorf(`flash should explain the ignore, got %q`, m.flashMsg)
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

	// esc again (no focus) -> no-op (shutdown requires typing "quit")
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.paused || m.stopping {
		t.Fatal("esc outside focus should NOT initiate shutdown")
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

// 'A' enters input mode with prepend=true; Enter inserts at the queue head.
func TestTUIModelAKeyPrepends(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"orig-1", "orig-2"})
	r.SetLive(true)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	// Let the worker start on orig-1 so pending = [orig-2].
	time.Sleep(150 * time.Millisecond)

	m := newModel(cfg, 2, r.Events(), r, 0)

	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = u.(model)
	if !m.inputMode || !m.inputPrepend {
		t.Fatal("A should open input mode with prepend=true")
	}
	for _, ch := range "urgent" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = u

	got := r.PendingSnapshot()
	if len(got) == 0 || got[0] != "urgent" {
		t.Errorf("prepended item not at head: %v", got)
	}
}

// Multi-line paste in prepend mode keeps the paste's original order at the
// queue head.
func TestTUIModelMultilinePrependPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 5`}
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

	time.Sleep(100 * time.Millisecond)

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = u.(model)
	// Paste three lines in one go.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\ny\nz\n")})
	m = u.(model)

	got := r.PendingSnapshot()
	// Head of queue should be x, y, z (matching paste order).
	if len(got) < 3 {
		t.Fatalf("expected >=3 items, got %v", got)
	}
	if got[0] != "x" || got[1] != "y" || got[2] != "z" {
		t.Errorf("prepended block order broken: head=%v, want [x y z ...]", got[:3])
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

	// Down arrow moves cursor by 1.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = u.(model)
	if m.recentList.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.recentList.cursor)
	}

	// 'j' also moves cursor down.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = u.(model)
	if m.recentList.cursor != 2 {
		t.Errorf("cursor after j = %d, want 2", m.recentList.cursor)
	}

	// 'G' goes to the end.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = u.(model)
	if m.recentList.cursor != 49 {
		t.Errorf("cursor after G = %d, want 49", m.recentList.cursor)
	}

	// 'g' returns to top.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = u.(model)
	if m.recentList.cursor != 0 {
		t.Errorf("cursor after g = %d, want 0", m.recentList.cursor)
	}

	// esc exits.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.recentMode {
		t.Fatal("esc should exit recentMode")
	}
}

// Enter in recent view triggers an editor launch for the cursor row's log.
func TestTUIModelRecentEnterOpensEditor(t *testing.T) {
	var openedPath string
	orig := openEditorCmd
	openEditorCmd = func(path string) tea.Cmd {
		openedPath = path
		return nil
	}
	defer func() { openEditorCmd = orig }()

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
	m.recent = []recentEntry{
		{JobIndex: 1, Line: "alpha", ExitCode: 0, Duration: time.Second, LogPath: "/tmp/p/001-alpha.log"},
		{JobIndex: 2, Line: "beta", ExitCode: 1, Duration: time.Second, LogPath: "/tmp/p/002-beta.log"},
	}
	m.height = 30

	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = u.(model)
	// Move cursor to second row then Enter.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = u
	if openedPath != "/tmp/p/002-beta.log" {
		t.Errorf("openEditorCmd called with %q, want %q", openedPath, "/tmp/p/002-beta.log")
	}
}

// 'e' in focus mode opens the focused slot's log in the editor.
func TestTUIModelFocusEOpensEditor(t *testing.T) {
	var openedPath string
	orig := openEditorCmd
	openEditorCmd = func(path string) tea.Cmd {
		openedPath = path
		return nil
	}
	defer func() { openEditorCmd = orig }()

	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10`}
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
	m.slots[1] = slotState{
		Active:   true,
		JobIndex: 1,
		Line:     "x",
		LogPath:  "/tmp/p/001-x.log",
	}
	m.focusSlot = 1

	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_ = u
	if openedPath != "/tmp/p/001-x.log" {
		t.Errorf("openEditorCmd called with %q, want /tmp/p/001-x.log", openedPath)
	}
}

// 'l' opens the queue view, populates snapshot, cursor/scroll work.
func TestTUIModelQueueView(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	items := make([]string, 50)
	for i := range items {
		items[i] = fmt.Sprintf("item-%02d", i+1)
	}
	cfg := Config{Parallelism: 1, Template: `sleep 10`}
	r := NewRunner(cfg, items)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, len(items), r.Events(), r, 0)
	m.height = 30

	// 'l' opens queue view and loads snapshot.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = u.(model)
	if !m.queueMode {
		t.Fatal("expected queueMode=true after 'l'")
	}
	if len(m.queueSnapshot) == 0 {
		t.Fatal("queueSnapshot should be populated")
	}

	// Down arrow moves cursor.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = u.(model)
	if m.queueList.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m.queueList.cursor)
	}

	// 'G' goes to end.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = u.(model)
	want := len(m.queueSnapshot) - 1
	if m.queueList.cursor != want {
		t.Errorf("cursor after G = %d, want %d", m.queueList.cursor, want)
	}

	// Esc closes.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.queueMode {
		t.Fatal("esc should close queue view")
	}
}

// '/' in recent view enters filter mode; typing narrows the list live.
func TestTUIModelRecentFilter(t *testing.T) {
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
	m.recent = []recentEntry{
		{JobIndex: 1, Line: "alpha"},
		{JobIndex: 2, Line: "beta"},
		{JobIndex: 3, Line: "gamma"},
		{JobIndex: 4, Line: "alphabet"},
	}
	m.height = 30

	// Open recent, then /
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = u.(model)
	if !m.recentFilterMode {
		t.Fatal("expected recentFilterMode after '/'")
	}

	// Type "alp" — should match alpha and alphabet.
	for _, ch := range "alp" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	if m.recentFilter != "alp" {
		t.Errorf("recentFilter = %q, want %q", m.recentFilter, "alp")
	}
	got := m.filteredRecent()
	if len(got) != 2 || got[0].Line != "alpha" || got[1].Line != "alphabet" {
		t.Errorf("filteredRecent = %v", got)
	}

	// Enter commits (exits input mode, filter stays).
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.recentFilterMode {
		t.Error("Enter should exit filter input mode")
	}
	if m.recentFilter != "alp" {
		t.Errorf("filter cleared unexpectedly: %q", m.recentFilter)
	}

	// Esc clears filter.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.recentFilter != "" {
		t.Errorf("Esc should clear filter, got %q", m.recentFilter)
	}
}

// '/' in queue view enters filter mode; substring filter is case-insensitive.
func TestTUIModelQueueFilter(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 1, Template: `sleep 10`}
	items := []string{"Apple", "banana", "APRICOT", "cherry"}
	r := NewRunner(cfg, items)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, len(items), r.Events(), r, 0)
	m.height = 30

	// Open queue, filter by "ap".
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = u.(model)
	for _, ch := range "ap" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	got := m.filteredQueue()
	// Case-insensitive: should match Apple and APRICOT, but not banana or cherry.
	want := map[string]bool{"Apple": true, "APRICOT": true}
	if len(got) != len(want) {
		t.Fatalf("filter results = %v, want %v", got, want)
	}
	for _, line := range got {
		if !want[line] {
			t.Errorf("unexpected filtered line %q", line)
		}
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

// 'o' opens the other menu; '1' transitions into the export prompt;
// typing a name + Enter writes a wrapper.
func TestTUIModelExportWrapperFlow(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Override writeWrapperFn to capture the output instead of writing to disk.
	var capturedPath, capturedContent string
	orig := writeWrapperFn
	writeWrapperFn = func(path, content string) error {
		capturedPath = path
		capturedContent = content
		return nil
	}
	defer func() { writeWrapperFn = orig }()

	cfg := Config{
		Parallelism:    4,
		File:           "urls.txt",
		Template:       "dm {item}",
		AttemptTimeout: time.Hour,
		Retries:        5,
	}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)

	// 'o' opens the menu.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = u.(model)
	if !m.otherMenu {
		t.Fatal("expected otherMenu=true after 'o'")
	}

	// '1' transitions to export input.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = u.(model)
	if m.otherMenu {
		t.Error("otherMenu should close after selection")
	}
	if !m.exportInput {
		t.Fatal("expected exportInput=true")
	}

	// Type a filename.
	for _, ch := range "my-dm" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}

	// Enter writes the wrapper.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.exportInput {
		t.Error("exportInput should be false after Enter")
	}
	if capturedPath == "" {
		t.Fatal("writeWrapperFn was not called")
	}
	canonDir, _ := filepath.EvalSymlinks(dir)
	wantPath := filepath.Join(canonDir, "bin", "my-dm")
	if capturedPath != wantPath {
		t.Errorf("captured path = %q, want %q (under <cwd>/bin)", capturedPath, wantPath)
	}
	if !strings.Contains(capturedContent, "-P 4") {
		t.Errorf("wrapper missing -P flag: %q", capturedContent)
	}
	if !strings.Contains(capturedContent, "--attempt-timeout 1h0m0s") {
		t.Errorf("wrapper missing attempt-timeout: %q", capturedContent)
	}
	if !strings.Contains(capturedContent, "'dm {item}'") {
		t.Errorf("wrapper missing template: %q", capturedContent)
	}
	if !strings.Contains(capturedContent, `"$@"`) {
		t.Errorf("wrapper missing $@ forwarding: %q", capturedContent)
	}
	if m.flashErr {
		t.Errorf("unexpected error flash: %q", m.flashMsg)
	}
}

// Filename with '/' characters is stripped at input time.
func TestTUIModelExportStripsSlashes(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	called := false
	orig := writeWrapperFn
	writeWrapperFn = func(path, content string) error {
		called = true
		return nil
	}
	defer func() { writeWrapperFn = orig }()

	cfg := Config{Parallelism: 1, File: "x", Template: "echo {item}", AttemptTimeout: time.Second, Retries: 5}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = u.(model)

	for _, ch := range "../escape" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	if strings.ContainsAny(string(m.exportBuf), "/\\") {
		t.Errorf("buffer should not contain slashes: %q", string(m.exportBuf))
	}

	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if !called {
		t.Error("wrapper should have been written with the sanitized name")
	}
}

// renderWrapper produces a wrapper with the expected flag set and invokes
// `parallel-each` via PATH (not an absolute path).
func TestRenderWrapperIncludesNonDefaultFlags(t *testing.T) {
	cfg := Config{
		Parallelism:     8,
		File:            "/tmp/urls.txt",
		Template:        "dm {item}",
		AttemptTimeout:  30 * time.Second,
		TotalTimeout:    time.Hour,
		Retries:         3,
		Fresh:           true,
		SkipUniqueCheck: true,
	}
	out, err := renderWrapper(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"exec parallel-each",
		"-P 8",
		"--attempt-timeout 30s",
		"--total-timeout 1h0m0s",
		"--retries 3",
		"--fresh",
		"--skip-unique-txt-rows",
		"-F '/tmp/urls.txt'",
		`'dm {item}'`,
		`"$@"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("wrapper missing %q:\n%s", w, out)
		}
	}
	// Should NOT embed an absolute path to the binary.
	if strings.Contains(out, "/parallel-each") {
		t.Errorf("wrapper should not hard-code an absolute path to parallel-each:\n%s", out)
	}
}

// resolveExportDir returns <cwd>/bin.
func TestResolveExportDirUsesCwdBin(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// macOS symlinks /var → /private/var; eval on the existing dir only,
	// then re-join bin/.
	wantBase, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wantBase, "bin")
	if got := resolveExportDir(); got != want {
		t.Errorf("resolveExportDir() = %q, want %q", got, want)
	}
}

// renderWrapper omits default flag values to keep output minimal.
func TestRenderWrapperOmitsDefaults(t *testing.T) {
	cfg := Config{
		Parallelism:    4,
		File:           "urls.txt",
		Template:       "dm {item}",
		AttemptTimeout: time.Minute,
		Retries:        5, // default
	}
	out, err := renderWrapper(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "--retries") {
		t.Errorf("wrapper should not emit --retries when it equals the default:\n%s", out)
	}
	if strings.Contains(out, "--total-timeout") {
		t.Errorf("wrapper should not emit --total-timeout when unset:\n%s", out)
	}
	if strings.Contains(out, "--fresh") {
		t.Errorf("wrapper should not emit --fresh when false:\n%s", out)
	}
}

// 'p' opens parallelism input; Enter confirms twice to apply.
func TestTUIModelParallelismFlow(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"a", "b"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 2, r.Events(), r, 0)
	if m.par != 2 {
		t.Fatalf("initial par = %d, want 2", m.par)
	}

	// 'p' opens input mode.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = u.(model)
	if !m.parInputMode {
		t.Fatal("parInputMode should be true after 'p'")
	}

	// Type "6".
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})
	m = u.(model)
	if string(m.parInputBuf) != "6" {
		t.Fatalf("parInputBuf = %q, want %q", string(m.parInputBuf), "6")
	}

	// Enter → confirm step (does not apply yet).
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.parInputMode {
		t.Error("parInputMode should be false after Enter")
	}
	if !m.parConfirmMode {
		t.Fatal("parConfirmMode should be true")
	}
	if m.parPending != 6 {
		t.Errorf("parPending = %d, want 6", m.parPending)
	}
	if r.Parallelism() != 2 {
		t.Errorf("runner par changed too early: %d", r.Parallelism())
	}

	// Enter again → apply.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.parConfirmMode {
		t.Error("parConfirmMode should be false after apply")
	}
	if m.par != 6 || r.Parallelism() != 6 {
		t.Errorf("after apply: model=%d runner=%d, want 6/6", m.par, r.Parallelism())
	}
}

// Esc at either step cancels without applying.
func TestTUIModelParallelismCancel(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"a", "b"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 2, r.Events(), r, 0)

	// Open, type, then Esc at input step.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.parInputMode || m.parConfirmMode {
		t.Error("Esc at input step should exit")
	}
	if r.Parallelism() != 2 {
		t.Errorf("runner par changed on cancel: %d", r.Parallelism())
	}

	// Open, Enter, then Esc at confirm step.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if !m.parConfirmMode {
		t.Fatal("should be in confirm step")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.parConfirmMode {
		t.Error("Esc at confirm step should exit")
	}
	if r.Parallelism() != 2 {
		t.Errorf("runner par changed on confirm-cancel: %d", r.Parallelism())
	}
}

// Input validation: non-digit letters are ignored, 0 is rejected.
func TestTUIModelParallelismValidation(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Parallelism: 2, Template: `sleep 5`}
	r := NewRunner(cfg, []string{"a"})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		r.ForceKill()
		for range r.Events() {
		}
	}()

	m := newModel(cfg, 1, r.Events(), r, 0)

	// Non-digit keys are ignored in input mode.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = u.(model)
	for _, ch := range "abc1x2" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = u.(model)
	}
	if string(m.parInputBuf) != "12" {
		t.Errorf("parInputBuf = %q, want %q", string(m.parInputBuf), "12")
	}

	// Entering 0 is rejected with a flash.
	for len(m.parInputBuf) > 0 {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = u.(model)
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = u.(model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.parConfirmMode {
		t.Error("0 should not enter confirm mode")
	}
	if !m.flashErr {
		t.Error("expected error flash")
	}
}

// elapsed() freezes while queue is empty and resumes when work is added.
// Verified via internal state (avoids the second-level rounding in elapsed()).
func TestModelActiveStateTransitions(t *testing.T) {
	m := model{total: 0, completed: 0}
	m.updateActiveState()
	if !m.activeStart.IsZero() {
		t.Fatal("idle at start: activeStart should be zero")
	}

	// Transition to active via new work.
	m.total = 1
	m.updateActiveState()
	if m.activeStart.IsZero() {
		t.Fatal("after total++: activeStart should be set")
	}

	// Complete all: transition to idle, accumulate duration.
	time.Sleep(20 * time.Millisecond)
	m.completed = 1
	m.updateActiveState()
	if !m.activeStart.IsZero() {
		t.Fatal("after completed == total: activeStart should be zero")
	}
	if m.accumActive < 10*time.Millisecond {
		t.Errorf("accumActive = %v, want >=10ms", m.accumActive)
	}

	// While idle, accumActive must not grow.
	saved := m.accumActive
	time.Sleep(30 * time.Millisecond)
	if m.accumActive != saved {
		t.Errorf("accumActive changed while idle: %v -> %v", saved, m.accumActive)
	}

	// Resume by adding more work.
	m.total = 2
	m.updateActiveState()
	if m.activeStart.IsZero() {
		t.Fatal("after resume: activeStart should be set")
	}
	time.Sleep(20 * time.Millisecond)
	if el := m.elapsedRaw(); el <= saved {
		t.Errorf("elapsed should advance after resume: saved=%v el=%v", saved, el)
	}
}

// Starting with pending items arms activeStart immediately.
func TestModelElapsedStartsWithWork(t *testing.T) {
	m := newModel(Config{}, 3, nil, nil, 0)
	if m.activeStart.IsZero() {
		t.Fatal("activeStart should be set when total > 0")
	}
}

// Starting with all items already resumed keeps the timer at 0.
func TestModelElapsedStartsIdleWhenEmpty(t *testing.T) {
	m := newModel(Config{}, 0, nil, nil, 3)
	if !m.activeStart.IsZero() {
		t.Fatal("activeStart should be zero when total = 0")
	}
	time.Sleep(60 * time.Millisecond)
	if got := m.elapsed(); got != 0 {
		t.Errorf("elapsed = %v, want 0", got)
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
