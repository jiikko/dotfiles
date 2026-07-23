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

// 落ち影は前景ブロック (█ 本体 / ▓ フェザー) で描き、bg ベタ塗り (旧 233) は使わない。
// 端末 bg が透けて penumbra になり縁が柔らかくなる。footprint 幅は据え置き。
func TestShadowForegroundBlocksAndFeather(t *testing.T) {
	// colored: 前景ブロック + フェザー、旧 bg 塗りは無い
	lines := buildShadowPanelBox(" t ", []string{"a", "b"}, 20, true)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "\x1b[48;5;233m") {
		t.Error("旧 bg ベタ塗り (256色 233) が残っている")
	}
	if !strings.Contains(joined, ansiShadowFg+"█") {
		t.Error("影本体 █ (近黒前景) が使われていない")
	}
	if !strings.Contains(joined, ansiShadowFg+"▓") {
		t.Error("縁のフェザー ▓ が使われていない")
	}
	for _, l := range lines {
		if w := runewidth.StringWidth(stripANSI(l)); w != 20 {
			t.Errorf("colored パネル行の幅 = %d; want 20: %q", w, l)
		}
	}
	// NO_COLOR: 近黒 fg が使えないため ▒ 本体 + ░ フェザーの階調で代用、ANSI は含まない
	mono := buildShadowPanelBox(" t ", []string{"a", "b"}, 20, false)
	mj := strings.Join(mono, "\n")
	if strings.ContainsRune(mj, '\x1b') {
		t.Error("NO_COLOR 出力に ANSI が混入している")
	}
	if !strings.Contains(mj, "▒") || !strings.Contains(mj, "░") {
		t.Error("NO_COLOR の濃淡 (▒ 本体 / ░ フェザー) が出ていない")
	}
}

// wrapWindowFrame は content を 上余白 + 枠 + 右下ドロップシャドウで包む (issue 025)。
func TestWrapWindowFrame(t *testing.T) {
	content := []string{"line one", "line two"}
	const termW = 40
	out := wrapWindowFrame(content, termW, false)
	// 行数 = content + 4 (上余白 + 上辺 + 下辺 + 下影)
	if len(out) != len(content)+4 {
		t.Fatalf("行数 = %d; want %d", len(out), len(content)+4)
	}
	if strings.TrimSpace(out[0]) != "" {
		t.Fatalf("先頭は上余白 (空行) のはず: %q", out[0])
	}
	if !strings.Contains(out[1], "┌") || !strings.Contains(out[1], "┐") {
		t.Fatalf("2 行目が上辺 ┌…┐ でない: %q", out[1])
	}
	for i, l := range out {
		if w := runewidth.StringWidth(stripANSI(l)); w > termW {
			t.Errorf("行 %d の幅 = %d > termW %d: %q", i, w, termW, l)
		}
	}
	// NO_COLOR は ▒/░ の影グリフ
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "▒") || !strings.Contains(joined, "░") {
		t.Errorf("NO_COLOR の影グリフ (▒/░) が無い:\n%s", joined)
	}
	// colored は近黒 fg の █ (本体)
	if cj := strings.Join(wrapWindowFrame(content, termW, true), "\n"); !strings.Contains(cj, ansiShadowFg+"█") {
		t.Errorf("colored で影本体 █ が無い")
	}
}
