package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLogKeyHandling(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a", ShortSHA: "a", Subject: "one"}, {SHA: "b", ShortSHA: "b", Subject: "two"}})
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("j cursor = %d, want 1", m.cursor)
	}
	m.handleKey("j")
	m.handleKey("k")
	m.handleKey("k")
	if m.cursor != 0 {
		t.Fatalf("clamped cursor = %d, want 0", m.cursor)
	}
	m.Update(tea.KeyMsg{})
	m.handleKey("q")
	if !m.done {
		t.Fatal("q did not set done")
	}
}
