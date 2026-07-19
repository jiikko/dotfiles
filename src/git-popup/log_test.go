package main

import (
	"strings"
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

func TestLogScrollKeepsCursorVisible(t *testing.T) {
	commits := make([]Commit, 5)
	for i := range commits {
		commits[i] = Commit{SHA: string(rune('a' + i)), ShortSHA: "sha", Subject: "subject"}
	}
	m := newLogModel(commits)
	m.height = 4 // two list rows
	m.handleKey("j")
	m.handleKey("j")
	if m.cursor != 2 || m.offset != 1 {
		t.Fatalf("after moving down: cursor=%d offset=%d, want 2/1", m.cursor, m.offset)
	}
	m.handleKey("k")
	m.handleKey("k")
	if m.cursor != 0 || m.offset != 0 {
		t.Fatalf("after moving up: cursor=%d offset=%d, want 0/0", m.cursor, m.offset)
	}
}

func TestClipANSIAware(t *testing.T) {
	// 色エスケープは可視幅に数えない: "✓ hello" (可視 7 桁) は width 7 でそのまま返る
	colored := "\x1b[38;5;2m✓\x1b[0m hello"
	if got := clip(colored, 7); got != colored {
		t.Errorf("色付きで幅内なのに truncate された: %q", got)
	}
	// 幅を超えたら可視幅で切り、… と reset を付ける (エスケープ途中で切らない)
	got := clip(colored, 4)
	if displayWidth(got) > 4 {
		t.Errorf("clip 後の可視幅 %d > 4: %q", displayWidth(got), got)
	}
	if !strings.HasSuffix(got, "…\x1b[0m") {
		t.Errorf("truncate 時に …+reset が付いていない: %q", got)
	}
	if strings.Count(got, "\x1b[38;5;2m") != 1 {
		t.Errorf("先頭の色エスケープが保持されていない: %q", got)
	}
}

func TestLogCIMark(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "sha"}})
	if got := m.ciMark("sha"); got != "\x1b[2m·\x1b[0m" {
		t.Fatalf("loading mark = %q", got)
	}
	m.ci = map[string]CIState{"sha": CISuccess}
	if got := m.ciMark("sha"); got != "\x1b[38;5;2m✓\x1b[0m" {
		t.Fatalf("success mark = %q", got)
	}
}
