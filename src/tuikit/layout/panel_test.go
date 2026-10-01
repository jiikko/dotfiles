package layout

import (
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 行数は中身 + 3、各行の表示幅は width + Indent (中身に全角・SGR・幅超えが混ざっても罫線が崩れない)。
func TestPanelGeometry(t *testing.T) {
	rows := []string{"短い", "\x1b[31m赤い中身が板より長く長く長く長く続く\x1b[0m", "", "ascii"}
	for _, colored := range []bool{false, true} {
		for _, indent := range []int{0, 1, 3} {
			for width := range 61 {
				st := PanelStyle{Border: BorderLight, Color: sgr.Dim, Indent: indent}
				out := Panel("\x1b[1mタイトル\x1b[0m", rows, width, colored, st)
				if len(out) != len(rows)+3 {
					t.Fatalf("行数 %d, want %d", len(out), len(rows)+3)
				}
				want := max(width, PanelMinWidth) + indent
				for i, l := range out {
					if got := termwidth.Of(l); got != want {
						t.Fatalf("colored=%v indent=%d width=%d 行 %d: 幅 %d, want %d: %q", colored, indent, width, i, got, want, l)
					}
				}
			}
		}
	}
}

// 中身の幅と PanelChrome を足した幅なら、中身は切られずに収まる。
func TestPanelChromeFitsContent(t *testing.T) {
	content := "ちょうど収まる中身"
	out := Panel("", []string{content}, termwidth.Of(content)+PanelChrome, false, PanelStyle{Border: BorderLight})
	if !strings.Contains(out[1], content) || strings.Contains(out[1], "…") {
		t.Fatalf("PanelChrome ぶん足した幅で中身が切られた: %q", out[1])
	}
}

// 色なしでは中身の SGR を通さない (閉じていない SGR が後続の行まで属性を引きずらないように)。
// タイトルは色あり・なしに関係なく SGR を落とす (幅の計算がずれて罫線が崩れるため)。
func TestPanelStripsSGRWhenUncolored(t *testing.T) {
	out := Panel("\x1b[31mtitle", []string{"\x1b[31mopen red"}, 30, false, PanelStyle{Border: BorderLight})
	for i, l := range out {
		if strings.Contains(l, "\x1b") {
			t.Fatalf("色なしの行 %d に SGR が残っている: %q", i, l)
		}
	}
	colored := Panel("\x1b[31mtitle", nil, 30, true, PanelStyle{Border: BorderLight, Color: sgr.Dim})
	if strings.Contains(colored[0], "\x1b[31m") {
		t.Fatalf("タイトルの SGR が残っている: %q", colored[0])
	}
}

// 影は右と下に落ち、色なしでは陰影文字になる。影の色の既定は ShadowNearBlack。
func TestPanelShadow(t *testing.T) {
	out := Panel("", []string{"a", "b"}, 20, false, PanelStyle{Border: BorderLight})
	if !strings.HasSuffix(out[1], ShadowMonoEdge) || !strings.HasSuffix(out[2], ShadowMono) {
		t.Fatalf("右の影が縁 → 本体になっていない: %q / %q", out[1], out[2])
	}
	last := out[len(out)-1]
	if !strings.HasPrefix(last, "  "+ShadowMonoEdge) {
		t.Fatalf("下端の影が右へずれて縁から始まっていない: %q", last)
	}
	colored := Panel("", []string{"a"}, 20, true, PanelStyle{Border: BorderLight})
	if !strings.Contains(colored[1], ShadowNearBlack+ShadowFeather) {
		t.Fatalf("影の色の既定が ShadowNearBlack でない: %q", colored[1])
	}
	custom := Panel("", []string{"a"}, 20, true, PanelStyle{Border: BorderLight, Shadow: "\x1b[90m"})
	if !strings.Contains(custom[1], "\x1b[90m"+ShadowFeather) {
		t.Fatalf("指定した影の色が使われていない: %q", custom[1])
	}
}

// 中央に浮かせた板は、左右の背景を残し (色も持ち越し)、行の幅を変えない。
func TestOverlayCenteredKeepsBackground(t *testing.T) {
	bg := strings.Repeat("x", 30)
	window := []string{bg, "\x1b[32m" + bg + "\x1b[0m", bg, bg, bg}
	box := []string{"[box]", "[box]"}
	out := OverlayCentered(append([]string(nil), window...), box, 30, 5, true)
	for i, l := range out {
		if got := termwidth.Of(l); got != 30 {
			t.Fatalf("行 %d の幅 %d, want 30: %q", i, got, l)
		}
	}
	// 5 行の中央 = 1..2 行目、30 桁の中央 = 左に 12 桁残る
	plain := termwidth.StripSGR(out[1])
	if plain != strings.Repeat("x", 12)+"[box]"+strings.Repeat("x", 13) {
		t.Fatalf("左右の背景が残っていない: %q", plain)
	}
	if !strings.Contains(out[1], "\x1b[32m") || !strings.HasSuffix(out[1], "\x1b[0m") {
		t.Fatalf("右の背景の色が持ち越されていない: %q", out[1])
	}
	if out[0] != bg || out[3] != bg {
		t.Fatalf("板の無い行が変わった: %q / %q", out[0], out[3])
	}
}

// 行ごと置き換える Overlay は、下に収まらなければ page の中に収まる位置まで引き上げる。
func TestOverlayPullsUpToFit(t *testing.T) {
	window := []string{"0", "1", "2", "3", "4"}
	out := Overlay(append([]string(nil), window...), []string{"A", "B"}, 4, 5)
	if strings.Join(out, "") != "012AB" {
		t.Fatalf("末尾で引き上げない: %q", out)
	}
}

// PanelContentWidth は Panel が中身に使う幅そのもの: その幅ちょうどの行は切られず、1 桁でも長い行は切られる
// (最小幅で押し上げられる狭い width も含む)。正解役は Panel 自身。
func TestPanelContentWidthMatchesPanel(t *testing.T) {
	for width := range 61 {
		cw := PanelContentWidth(width)
		fit := strings.Repeat("x", cw)
		over := strings.Repeat("x", cw+1)
		out := Panel("", []string{fit, over}, width, false, PanelStyle{Border: BorderLight})
		if !strings.Contains(out[1], fit) || strings.Contains(out[1], "…") {
			t.Fatalf("width=%d: 幅 %d の行が切られた: %q", width, cw, out[1])
		}
		if !strings.Contains(out[2], "…") {
			t.Fatalf("width=%d: 幅 %d の行が切られていない (中身の幅を 1 桁広く数えている): %q", width, cw+1, out[2])
		}
	}
}

// 右寄せの重ね: 左の背景は残り、各行の幅は窓の幅ちょうど、箱は右端に揃う。箱が窓より広い行は箱を窓の幅で切る。
// 窓の外 (base が負・下にはみ出す) の行は捨てる。色ありなら箱の直前で reset する (背景の開いた色を箱へ滲ませない)。
func TestOverlayRight(t *testing.T) {
	bg := func() []string {
		return []string{"0123456789abcdefghij", "\x1b[31m赤い背景のa行がつづく", "あいうえおかきくけこ", "tail"}
	}
	box := []string{"[box1]", "[箱２]"}

	got := OverlayRight(bg(), box, 20, true, 1)
	for i, want := range []string{"0123456789abcdefghij", "\x1b[31m赤い背景のa行" + sgr.Reset + " [box1]", "あいうえおかき" + sgr.Reset + "[箱２]", "tail"} {
		if got[i] != want {
			t.Fatalf("行 %d = %q, want %q", i, got[i], want)
		}
	}
	for i := 1; i <= 2; i++ {
		if w := termwidth.Of(got[i]); w != 20 {
			t.Fatalf("行 %d の幅 %d, want 20", i, w)
		}
	}

	wide := OverlayRight(bg(), []string{strings.Repeat("箱", 15)}, 20, false, 0)
	if w := termwidth.Of(wide[0]); w > 20 || !strings.HasPrefix(wide[0], "箱") {
		t.Fatalf("窓より広い箱: %q (幅 %d)", wide[0], w)
	}

	edge := OverlayRight(bg(), box, 20, false, -1)
	if edge[0] != "0123456789abcd[箱２]" {
		t.Fatalf("base=-1: 2 行目の箱だけが 0 行目に来るはず: %q", edge[0])
	}
	if out := OverlayRight(bg(), box, 20, false, 3); len(out) != 4 || out[3] != "tail          [box1]" {
		t.Fatalf("下にはみ出す: %q", out)
	}
}
