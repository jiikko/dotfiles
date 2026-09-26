package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 演出のコマの観測は、置き場に印があるあいだだけ書く。印は tick ごとに見る (起動し直さずに入れたり切ったりできる)。
func TestFrameLogRecordsOnlyWhileMarked(t *testing.T) {
	dir := t.TempDir()
	m, _ := cursorModel(t)
	m.SetFrameLog(dir)
	logPath, onPath := filepath.Join(dir, FrameLogFile), filepath.Join(dir, FrameLogOn)
	lines := func() []string {
		b, err := os.ReadFile(logPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	frames := func() {
		for range 3 {
			m.Update(frameMsg{})
			_ = m.View()
		}
	}
	m.Update(tickMsg{})
	frames()
	if got := lines(); got != nil {
		t.Fatalf("印が無いのに書いた: %v", got)
	}
	if err := os.WriteFile(onPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	frames()
	got := lines()
	var nFrame, nView int
	for _, l := range got {
		f := strings.Split(l, "\t")
		if len(f) != 3 {
			t.Fatalf("記録の形が違う (時刻 \\t 種類 \\t µs): %q", l)
		}
		switch f[1] {
		case "frameMsg":
			nFrame++
		case "View":
			nView++
		}
	}
	if nFrame != 3 || nView != 3 {
		t.Fatalf("印を置いた後のコマ 3 回・描画 3 回を記録していない (frameMsg %d / View %d): %v", nFrame, nView, got)
	}
	if err := os.Remove(onPath); err != nil {
		t.Fatal(err)
	}
	m.Update(tickMsg{})
	n := len(lines())
	frames()
	if after := len(lines()); after != n {
		t.Fatalf("印を消した後も書いた (%d → %d 行)", n, after)
	}
}
