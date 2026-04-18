package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTailHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	content := strings.Join([]string{
		"# item: alpha",
		"# cmd: dm alpha",
		"# time: 2026-04-19T00:00:00+0900",
		"---",
		"step 1",
		"step 2",
		"step 3",
		"step 4",
		"step 5",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readTail(path, 3)
	want := []string{"step 3", "step 4", "step 5"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadTailStripsHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	content := "# meta1\n# meta2\n---\nhello\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readTail(path, 10)
	if !equalStrings(got, []string{"hello"}) {
		t.Errorf("got %v", got)
	}
}

func TestReadTailMissingFile(t *testing.T) {
	if got := readTail(filepath.Join(t.TempDir(), "nope"), 5); got != nil {
		t.Errorf("want nil for missing file, got %v", got)
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readTail(path, 5); got != nil {
		t.Errorf("want nil for empty file, got %v", got)
	}
}

func TestReadTailLargeFileKeepsLastLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	var sb strings.Builder
	sb.WriteString("---\n")
	// Generate enough lines to exceed the 8KB read window.
	for i := 0; i < 5000; i++ {
		sb.WriteString("line ")
		sb.WriteString(string(rune('A' + (i % 26))))
		sb.WriteByte('\n')
	}
	sb.WriteString("final line\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readTail(path, 3)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %v", len(got), got)
	}
	if got[len(got)-1] != "final line" {
		t.Errorf("last line = %q, want %q", got[len(got)-1], "final line")
	}
}
