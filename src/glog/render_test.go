package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func testCommits() []Commit {
	return []Commit{
		{SHA: "a", ShortSHA: "aaaaaaa", Subject: "Fix invoice calculation", Author: "koji", RelDate: "2 hours ago", Decoration: "HEAD -> master"},
		{SHA: "b", ShortSHA: "bbbbbbb", Subject: "Update README", Author: "koji", RelDate: "1 day ago"},
	}
}

func TestRenderCommitsPlain(t *testing.T) {
	statuses := map[string]CIState{"a": StateSuccess, "b": StateFailure}
	out := RenderCommits(testCommits(), statuses, 120, false, "")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("行数 = %d; want 2:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "✓ aaaaaaa") {
		t.Errorf("成功行 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "✗ bbbbbbb") {
		t.Errorf("失敗行 = %q", lines[1])
	}
	if !strings.Contains(lines[0], "(HEAD -> master)") {
		t.Errorf("decoration がない: %q", lines[0])
	}
}

func TestRenderCommitsLoadingSpinner(t *testing.T) {
	// statuses に無い SHA は取得中としてスピナーを出す
	out := RenderCommits(testCommits(), map[string]CIState{}, 120, false, "⠋")
	if !strings.HasPrefix(out, "⠋ aaaaaaa") {
		t.Errorf("スピナー行 = %q", out)
	}
}

func TestRenderCommitsNoANSIWhenUncolored(t *testing.T) {
	// 非 TTY (colored=false) では ANSI を一切出さない (issue の完了条件)
	statuses := map[string]CIState{"a": StateSuccess, "b": StatePending}
	out := RenderCommits(testCommits(), statuses, 120, false, "")
	if strings.Contains(out, "\x1b") {
		t.Errorf("colored=false で ANSI が混入: %q", out)
	}
}

func TestRenderCommitsColored(t *testing.T) {
	statuses := map[string]CIState{"a": StateSuccess, "b": StateFailure}
	out := RenderCommits(testCommits(), statuses, 120, true, "")
	if !strings.Contains(out, ansiGreen+"✓"+ansiReset) {
		t.Errorf("成功記号に色がない: %q", out)
	}
}

func TestRenderCommitsWithBody(t *testing.T) {
	commits := testCommits()
	commits[0].Body = " file.go | 2 +-\n 1 file changed"
	statuses := map[string]CIState{"a": StateSuccess, "b": StateSuccess}
	out := RenderCommits(commits, statuses, 120, false, "")
	// CI 記号はヘッダー行にだけ付く (issue の設計)
	var glyphLines int
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "✓") {
			glyphLines++
		}
	}
	if glyphLines != 2 {
		t.Errorf("記号付き行 = %d; want 2 (ヘッダーのみ):\n%s", glyphLines, out)
	}
	if !strings.Contains(out, " file.go | 2 +-") {
		t.Errorf("本文が保持されていない:\n%s", out)
	}
}

func TestRenderCommitsTruncatesToWidth(t *testing.T) {
	commits := []Commit{{SHA: "a", ShortSHA: "aaaaaaa", Subject: strings.Repeat("x", 100), Author: "koji", RelDate: "now"}}
	out := RenderCommits(commits, map[string]CIState{"a": StateSuccess}, 40, false, "")
	for line := range strings.SplitSeq(out, "\n") {
		if w := runewidth.StringWidth(line); w > 40 {
			t.Errorf("幅 %d > 40: %q", w, line)
		}
	}
}

func TestRenderCached(t *testing.T) {
	head := &Commit{SHA: "a", ShortSHA: "aaaaaaa", Subject: "Fix invoice calculation"}
	out := RenderCached(head, StateSuccess, " 3 files changed", false, "")
	if !strings.HasPrefix(out, "HEAD CI: ✓ aaaaaaa Fix invoice calculation") {
		t.Errorf("ヘッダー = %q", out)
	}
	if !strings.Contains(out, "Staged changes:\n 3 files changed") {
		t.Errorf("staged diff がない:\n%s", out)
	}
	empty := RenderCached(head, StateNone, "", false, "")
	if !strings.Contains(empty, "staged な変更はありません") {
		t.Errorf("空 diff の表示 = %q", empty)
	}
}

func TestStatusGlyphAllStates(t *testing.T) {
	want := map[CIState]string{
		StateSuccess: "✓", StateFailure: "✗", StatePending: "●",
		StateNeutral: "⊘", StateNone: "–", StateUnknown: "?",
	}
	for state, glyph := range want {
		if got := StatusGlyph(state, false, ""); got != glyph {
			t.Errorf("StatusGlyph(%s) = %q; want %q", state, got, glyph)
		}
	}
	if got := StatusGlyph(StateLoading, false, "⠙"); got != "⠙" {
		t.Errorf("loading = %q; want spinner", got)
	}
}
