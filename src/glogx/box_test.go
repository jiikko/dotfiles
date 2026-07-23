package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestBuildPanelBoxWidths(t *testing.T) {
	lines := buildPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	if len(lines) != 4 {
		t.Fatalf("枠 + 2 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestBuildShadowPanelBoxWidths(t *testing.T) {
	lines := buildShadowPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	// 枠 (top/bottom) + 2 行 + 下端の落ち影 1 行 = 5 行。影を足しても footprint 幅は 40 のまま
	if len(lines) != 5 {
		t.Fatalf("枠 + 2 行 + 影 1 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestJapanesePanelBoxWidths(t *testing.T) {
	// 全角の job 名・タイトルでも罫線の幅が揃う (全角境界の切り詰め込み)
	rows := []string{
		"❯ ✓ テストジョブ (日本語)",
		"  ✗ " + strings.Repeat("長", 40), // inner を超えて全角境界で切り詰められる
	}
	lines := buildPanelBox(" CI jobs: abc1234 日本語のサブジェクトがとても長い場合の切り詰め ", rows, 40, true)
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestBuildPanelBoxTitleStripsANSI(t *testing.T) {
	// SGR 入りの job 名/subject がタイトルに載っても罫線幅と dim 塗りを崩さない
	lines := buildPanelBox(" \x1b[31mred job\x1b[0m ", []string{"row"}, 40, false)
	if strings.Contains(lines[0], "\x1b") {
		t.Errorf("タイトルに ANSI が残っている: %q", lines[0])
	}
	if w := runewidth.StringWidth(lines[0]); w != 40 {
		t.Errorf("タイトル行の幅 = %d; want 40: %q", w, lines[0])
	}
}
