package termwidth

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// 速い道の字 (受理集合) と、受理されずに x/ansi へ回る字を混ぜた部品。受理集合の境 (幅 2 になりうる曖昧幅の記号・
// SGR の前後・CSI だが SGR でない列・結合文字) を含める
var truncPieces = []string{
	"a", "Z", " ", "~",
	"\x1b[0m", "\x1b[1;32m", "\x1b[m",
	"─", "✓", "…", "⠋", "·", "│",
	"あ", "é", "\x1b[2K", "\t", "1️⃣",
}

var truncTails = []string{"", "…", "..", "\x1b[0m…", "あ"}

// 部品を最大 5 個並べた全列 × 幅 -1〜9 × tail で、x/ansi 本体と 1 byte 違わず一致することを総当たりで確かめる
// (速い道は x/ansi の規則の写しなので、本物を正解役にして突き合わせる)
func TestAnsiTruncateMatchesAnsi(t *testing.T) {
	var fast, total int
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		if _, ok := fastDispWidth(prefix); ok {
			fast++
		}
		total++
		for w := -1; w <= 9; w++ {
			for _, tail := range truncTails {
				if got, want := ansiTruncate(prefix, w, tail), ansi.Truncate(prefix, w, tail); got != want {
					t.Fatalf("ansiTruncate(%q, %d, %q) = %q, want %q", prefix, w, tail, got, want)
				}
				if got, want := ansiTruncateLeft(prefix, w, tail), ansi.TruncateLeft(prefix, w, tail); got != want {
					t.Fatalf("ansiTruncateLeft(%q, %d, %q) = %q, want %q", prefix, w, tail, got, want)
				}
			}
		}
		if depth == 0 {
			return
		}
		for _, p := range truncPieces {
			walk(prefix+p, depth-1)
		}
	}
	walk("", 4)
	// 速い道を通った列が十分にあること (受理集合が変わって全部 x/ansi へ回ると、この検査は何も比べなくなる)
	if fast < total/10 {
		t.Fatalf("速い道を通った列が %d / %d しかない (受理集合が変わった?)", fast, total)
	}
}

func FuzzAnsiTruncateMatchesAnsi(f *testing.F) {
	f.Add("\x1b[1;32m✓ pushed\x1b[0m origin/master", 10, "…")
	f.Add("│ あ ─── \x1b[m", 3, "")
	f.Fuzz(func(t *testing.T, s string, w int, tail string) {
		w %= 200
		if got, want := ansiTruncate(s, w, tail), ansi.Truncate(s, w, tail); got != want {
			t.Fatalf("ansiTruncate(%q, %d, %q) = %q, want %q", s, w, tail, got, want)
		}
		if got, want := ansiTruncateLeft(s, w, tail), ansi.TruncateLeft(s, w, tail); got != want {
			t.Fatalf("ansiTruncateLeft(%q, %d, %q) = %q, want %q", s, w, tail, got, want)
		}
	})
}

// 速い道は確保を減らすのが目的でもある: 色の無い行を tail なしで切るときは元の文字列の部分を返し、確保しない
func TestAnsiTruncateNoAllocWithoutSGR(t *testing.T) {
	s := strings.Repeat("abc ─ ", 20)
	if n := testing.AllocsPerRun(100, func() { _ = ansiTruncate(s, 30, "") }); n != 0 {
		t.Errorf("ansiTruncate の確保 = %v 回, want 0", n)
	}
	if n := testing.AllocsPerRun(100, func() { _ = ansiTruncateLeft(s, 30, "") }); n != 0 {
		t.Errorf("ansiTruncateLeft の確保 = %v 回, want 0", n)
	}
}
