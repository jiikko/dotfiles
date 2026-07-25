package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildPanelBoxWidths(t *testing.T) {
	lines := buildPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false)
	if len(lines) != 4 {
		t.Fatalf("枠 + 2 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := dispWidth(l); w != 40 {
			t.Errorf("パネル行の幅 = %d; want 40: %q", w, l)
		}
	}
}

func TestBuildShadowPanelBoxWidths(t *testing.T) {
	lines := buildShadowPanelBox(" title ", []string{"row", strings.Repeat("x", 200)}, 40, false, ansiDim)
	// 枠 (top/bottom) + 2 行 + 下端の落ち影 1 行 = 5 行。影を足しても footprint 幅は 40 のまま
	if len(lines) != 5 {
		t.Fatalf("枠 + 2 行 + 影 1 行のはずが %d 行", len(lines))
	}
	for _, l := range lines {
		if w := dispWidth(l); w != 40 {
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
		if w := dispWidth(l); w != 40 {
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
	if w := dispWidth(lines[0]); w != 40 {
		t.Errorf("タイトル行の幅 = %d; want 40: %q", w, lines[0])
	}
}

// 落ち影は前景ブロック (█ 本体 / ▓ フェザー) で描き、bg ベタ塗り (旧 233) は使わない。
// 端末 bg が透けて penumbra になり縁が柔らかくなる。footprint 幅は据え置き。
func TestShadowForegroundBlocksAndFeather(t *testing.T) {
	// colored: 前景ブロック + フェザー、旧 bg 塗りは無い
	lines := buildShadowPanelBox(" t ", []string{"a", "b"}, 20, true, ansiDim)
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
		if w := dispWidth(l); w != 20 {
			t.Errorf("colored パネル行の幅 = %d; want 20: %q", w, l)
		}
	}
	// NO_COLOR: 近黒 fg が使えないため ▒ 本体 + ░ フェザーの階調で代用、ANSI は含まない
	mono := buildShadowPanelBox(" t ", []string{"a", "b"}, 20, false, ansiDim)
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
	// 上辺は二重罫線 ╔…╗ (ユーザー要望 2026-07-24: フレームを二重に)
	if !strings.Contains(out[1], "╔") || !strings.Contains(out[1], "╗") {
		t.Fatalf("2 行目が上辺 ╔…╗ でない: %q", out[1])
	}
	for i, l := range out {
		if w := dispWidth(l); w > termW {
			t.Errorf("行 %d の幅 = %d > termW %d: %q", i, w, termW, l)
		}
	}
	// NO_COLOR は ▒/░ の影グリフ
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "▒") || !strings.Contains(joined, "░") {
		t.Errorf("NO_COLOR の影グリフ (▒/░) が無い:\n%s", joined)
	}
	// colored は近黒 fg の █ (本体)
	cj := strings.Join(wrapWindowFrame(content, termW, true), "\n")
	if !strings.Contains(cj, ansiShadowFg+"█") {
		t.Errorf("colored で影本体 █ が無い")
	}
	// 罫線は scratch と同じマゼンタで染まる。影は中立 dim のまま (染めない)
	if !strings.Contains(cj, ansiFrameBorder+"╔") {
		t.Errorf("colored で上辺が枠色に染まっていない:\n%s", cj)
	}
	if strings.Contains(cj, ansiFrameBorder+"█") || strings.Contains(cj, ansiFrameBorder+"▓") {
		t.Errorf("落ち影まで枠色で染まっている (影は中立のままにする):\n%s", cj)
	}
	// NO_COLOR では色を出さない (paint が素通しする)
	if nj := strings.Join(wrapWindowFrame(content, termW, false), "\n"); strings.Contains(nj, ansiFrameBorder) {
		t.Errorf("NO_COLOR で枠色の SGR が出ている:\n%s", nj)
	}
}

// withScrollbar: 収まるときは無改変、溢れるときは thumb が offset に追従し末尾で下端に接地する。
func TestWithScrollbar(t *testing.T) {
	rows := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "x"
		}
		return out
	}
	// 全体が収まる → 列を作らない (本文幅を戻す)
	in := rows(5)
	if got := withScrollbar(in, 40, 5, 0, false); !reflect.DeepEqual(got, in) {
		t.Fatalf("total <= view で改変された: %q", got)
	}
	// thumb 位置: 先頭 / 中間 / 末尾
	thumbAt := func(offset int) []int {
		var idx []int
		for i, l := range withScrollbar(rows(10), 40, 100, offset, false) {
			if strings.HasSuffix(l, scrollbarThumbGlyph) {
				idx = append(idx, i)
			} else if !strings.HasSuffix(l, scrollbarTrackGlyph) {
				t.Fatalf("行 %d にバー列が無い: %q", i, l)
			}
		}
		if len(idx) == 0 {
			t.Fatalf("offset %d で thumb が無い", offset)
		}
		return idx
	}
	if got := thumbAt(0); got[0] != 0 {
		t.Errorf("offset 0 の thumb 開始 = %d, want 0", got)
	}
	last := thumbAt(90)
	if last[len(last)-1] != 9 {
		t.Errorf("末尾 offset で thumb が下端 (9) に接地していない: %v", last)
	}
	if mid := thumbAt(45); mid[0] == 0 || mid[len(mid)-1] == 9 {
		t.Errorf("中間 offset の thumb が端に張り付いている: %v", mid)
	}
	// 幅: バー列を足しても buildPanelBox の本文幅を超えない。幅は描画側と同じ dispWidth
	// (ansi.StringWidth) で測る — … / █ は runewidth では 2 桁扱い (ambiguous) になり食い違う。
	for _, l := range withScrollbar([]string{strings.Repeat("a", 100)}, 40, 100, 0, false) {
		if w := dispWidth(l); w > 40-4 {
			t.Errorf("本文行の幅 = %d > inner %d: %q", w, 40-4, l)
		}
	}
}

// ansiFrameBorder の色番号が theme/colors.yml の blink_magenta と一致することを機械検証する。
//
// なぜテストで守るか: この色は dotfiles のテーマ意味マップ (docs/theme-colors.md) の
// 「点滅/scratch アイデンティティ」で、tmux の scratch popup 枠と同じ色であることに意味がある
// (glogx も「ふだんの pane とは別の一時的な板」だと色で示す)。単一ソースは theme/colors.yml だが、
// 生成器 (scripts/gen_theme_colors.sh) の Go 出力先は src/git-popup 固定なので glogx は
// 手書きコピーになる。放置すると yml を変えたときに無言でずれるため、ここで突き合わせる。
func TestFrameBorderMatchesThemeYML(t *testing.T) {
	const role = "blink_magenta"
	data, err := os.ReadFile(filepath.Join("..", "..", "theme", "colors.yml"))
	if err != nil {
		t.Skipf("theme/colors.yml を読めないのでスキップ (glogx 単体で切り出された場合): %v", err)
	}
	// role の直後に続く最初の "cterm: <n>" を取る
	lines := strings.Split(string(data), "\n")
	want := ""
	for i, l := range lines {
		if !strings.HasPrefix(l, role+":") {
			continue
		}
		for _, next := range lines[i+1:] {
			if s := strings.TrimSpace(next); strings.HasPrefix(s, "cterm:") {
				want = strings.TrimSpace(strings.TrimPrefix(s, "cterm:"))
				break
			}
		}
		break
	}
	if want == "" {
		t.Fatalf("theme/colors.yml に %s の cterm が見つからない", role)
	}
	if got := "\x1b[38;5;" + want + "m"; ansiFrameBorder != got {
		t.Errorf("ansiFrameBorder = %q; theme/colors.yml の %s (cterm %s) は %q\n"+
			"→ yml を変えたなら render.go の ansiFrameBorder も揃えること", ansiFrameBorder, role, want, got)
	}
}
