package termwidth

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sliceInputs は速い道に乗る行 (ASCII / SGR / 記号) と乗らない行 (全角・結合・絵文字・キーキャップ) を混ぜる。
// 速い道で「収まる」と決めて ansi を呼ばない分岐と、ansi に任せる分岐の両方を通すため。
var sliceInputs = []string{
	"",
	"abcdef",
	"\x1b[31mred\x1b[0m plain \x1b[1;32mgreen\x1b[m",
	"│ ✓ 2270ab5 ─── …",
	"日本語の題名 abc",
	"\x1b[33m端末\x1b[0mの幅\x1b[2m 計算\x1b[0m",
	"áé combining",
	"👨‍💻 zwj 🇯🇵 flag",
	"x1️⃣y2️⃣z", // ansi.Truncate と Of の数え方が食い違う (issue 416)
	"\x1b[48;5;196m\x1b[38;5;231m枠\x1b[0m \x1b[53m上線\x1b[0m",
	"\x1b[31m",        // 幅 0 の行 (SGR だけ)
	"a\ufe0f\x1b[31m", // 幅 1 で切ると ansi がはみ出し、t = 0 まで下げると SGR だけが残る
	"\ta\ufe0fb",      // 同じく t = 0 でタブが残る形
}

// wantTruncate は ansi.Truncate を正本にした期待値。ansi の結果が Of で width をはみ出すとき (キーキャップ。
// issue 416) は、ansi へ渡す幅を 1 ずつ下げて Of で収まる最初の結果にする (二分探索しない素朴な参照。
// schedkeys が持っていた fitWidth と同じ形)。
func wantTruncate(s string, width int, tail string) string {
	r := ansi.Truncate(s, width, tail)
	for t := width - 1; width > 0 && Of(r) > width && t >= 0; t-- {
		r = ansi.Truncate(s, t, tail)
	}
	return r
}

// wantSlice は wantTruncate の右端で ansi.Cut と同じ切り出しをする。
func wantSlice(s string, left, right int) string {
	if right <= left {
		return ""
	}
	head := wantTruncate(s, right, "")
	if left <= 0 {
		return head
	}
	return ansi.TruncateLeft(head, left, "")
}

// TruncateMeasure は ansi.Truncate と同じ結果を返し (width <= 0 と、SGR だけの幅 0 の行も含む)、返す幅は Of(結果) と一致する。
func TestTruncateMeasureMatchesAnsi(t *testing.T) {
	for _, s := range sliceInputs {
		for w := -1; w <= Of(s)+2; w++ {
			for _, tail := range []string{"", "…"} {
				got, gw := TruncateMeasure(s, w, tail)
				if gw != Of(got) {
					t.Fatalf("TruncateMeasure(%q, %d, %q) の幅 %d が Of(結果)=%d と違う", s, w, tail, gw, Of(got))
				}
				if want := wantTruncate(s, w, tail); got != want {
					t.Fatalf("TruncateMeasure(%q, %d, %q) = %q; 参照は %q", s, w, tail, got, want)
				}
			}
		}
	}
}

// Slice / SliceFrom は ansi.Cut と同じ結果を返す (right <= left の空・左の負の値も含む)。
func TestSliceMatchesAnsiCut(t *testing.T) {
	for _, s := range sliceInputs {
		sw := Of(s)
		for l := -1; l <= sw+1; l++ {
			if got, want := SliceFrom(s, l), ansi.Cut(s, l, sw); got != want {
				t.Fatalf("SliceFrom(%q, %d) = %q; ansi.Cut は %q", s, l, got, want)
			}
			for r := l - 1; r <= sw+2; r++ {
				if got, want := Slice(s, l, r), wantSlice(s, l, r); got != want {
					t.Fatalf("Slice(%q, %d, %d) = %q; 参照は %q", s, l, r, got, want)
				}
			}
		}
	}
}

// SplitAround の左右は、差し込みの形 (左 = Cut(0, x) / 右 = Cut(x+w, 幅)) を ansi.Cut で書いたものと一致する。
// x が [0, 幅) の外なら左右は空。
func TestSplitAroundMatchesAnsiCut(t *testing.T) {
	for _, s := range sliceInputs {
		sw := Of(s)
		for x := -2; x <= sw+1; x++ {
			for w := range 4 {
				left, lw, right, total := SplitAround(s, x, w)
				if total != sw {
					t.Fatalf("SplitAround(%q, %d, %d) の total %d; Of は %d", s, x, w, total, sw)
				}
				if x < 0 || x >= sw {
					if left != "" || lw != 0 || right != "" {
						t.Fatalf("SplitAround(%q, %d, %d): 範囲外なのに左右が空でない (%q, %d, %q)", s, x, w, left, lw, right)
					}
					continue
				}
				if lw != Of(left) {
					t.Fatalf("SplitAround(%q, %d, %d) の左の幅 %d が Of(左)=%d と違う", s, x, w, lw, Of(left))
				}
				if want := ansi.Cut(s, x+w, sw); right != want {
					t.Fatalf("SplitAround(%q, %d, %d) の右 = %q; ansi.Cut は %q", s, x, w, right, want)
				}
				if want := wantSlice(s, 0, x); left != want {
					t.Fatalf("SplitAround(%q, %d, %d) の左 = %q; 参照は %q", s, x, w, left, want)
				}
			}
		}
	}
}
