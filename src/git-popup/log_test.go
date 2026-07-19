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
	m.height = 5 // paneRows = height-3(footer+border) = 2 行
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

func TestLogViewMinSizeGuard(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a", ShortSHA: "a", Subject: "x"}})
	m.width, m.height = 10, 3 // 最小未満: ボーダー2ペインを出さず単一行に degrade
	got := m.View()
	if strings.Contains(got, "\n") || strings.Contains(got, "╭") {
		t.Fatalf("極小端末でボーダー2ペインを出している (単一行に degrade すべき): %q", got)
	}
	if !strings.HasPrefix(stripANSI(got), "git-popup") {
		t.Fatalf("degrade メッセージでない: %q", got)
	}
	m.width, m.height = 0, 0 // サイズ未確定 (WindowSizeMsg 前) は空
	if m.View() != "" {
		t.Fatalf("サイズ未確定で空を返さない")
	}
}

func TestLogListLinesPushColor(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "p1", ShortSHA: "pushed1", Subject: "s"}, {SHA: "u1", ShortSHA: "unpush1", Subject: "s"}})
	m.unpushed = map[string]bool{"u1": true}
	lines := m.buildListLines(60, 2)
	// カーソル行 (0) は accent の ▌
	if !strings.Contains(lines[0], "▌") {
		t.Errorf("カーソル行に ▌ が無い: %q", lines[0])
	}
	// 未 push は marker_orange・push 済みは cold_gray
	if !strings.Contains(lines[1], fg("marker_orange")) {
		t.Errorf("未 push SHA が marker_orange でない: %q", lines[1])
	}
	if !strings.Contains(lines[0], fg("cold_gray")) {
		t.Errorf("push 済み SHA が cold_gray でない: %q", lines[0])
	}
}

func TestLogDetailCIOverlay(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}})
	m.preview = "diff-a\ndiff-b\ndiff-c\ndiff-d"
	m.ciJobs = "CI-1\nCI-2"
	// 一覧モード: CI は上部にオーバーレイし diff を押し下げない (top2=CI, その下は diff の続き)
	lines := m.buildDetailLines(40, 4)
	if stripANSI(lines[0]) != "CI-1" || stripANSI(lines[1]) != "CI-2" {
		t.Fatalf("CI が上部オーバーレイになっていない: %q", lines)
	}
	if stripANSI(lines[2]) != "diff-c" || stripANSI(lines[3]) != "diff-d" {
		t.Fatalf("diff の下部が押し下げられている (占有 overlay のはず): %q", lines)
	}
	// 詳細モード: オーバーレイせず detailOffset から diff を出す
	m.detailOpen = true
	m.detailOffset = 1
	d := m.buildDetailLines(40, 3)
	if stripANSI(d[0]) != "diff-b" {
		t.Fatalf("詳細スクロールが offset どおりでない: %q", d)
	}
}

func TestLogDetailEmptyPreview(t *testing.T) {
	m := newLogModel([]Commit{{SHA: "a"}})
	m.preview = "" // preview 未着
	lines := m.buildDetailLines(40, 3)
	if !strings.Contains(stripANSI(lines[0]), "no preview") {
		t.Errorf("空 preview で (no preview) を出さない: %q", lines[0])
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
	if got := m.ciMark("sha"); got != ansiDim+"·"+ansiReset {
		t.Fatalf("loading mark = %q", got)
	}
	// 色は theme (active_green) から引く。直値でなく paintFg で期待値を作り theme に追従させる
	m.ci = map[string]CIState{"sha": CISuccess}
	if got := m.ciMark("sha"); got != paintFg("active_green", "✓") {
		t.Fatalf("success mark = %q", got)
	}
	if stripANSI(m.ciMark("sha")) != "✓" {
		t.Fatalf("success mark 可視文字 = %q", stripANSI(m.ciMark("sha")))
	}
}
