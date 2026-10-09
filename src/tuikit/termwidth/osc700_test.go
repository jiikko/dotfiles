package termwidth

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ESC のシーケンスの読み方は Of と同じ x/ansi のパーサ (OSC 8 のリンクは英字で終わらないので、「英字まで」の自前の走査は
// URL の途中で切って中身を文字として残した。issue 700 の 1)。
func TestEscapesReadLikeAnsi(t *testing.T) {
	s := "\x1b]8;;https://example.com/a\x1b\\link\x1b]8;;\x1b\\ tail \x1b[31mred\x1b[0m"
	if got, want := StripSGR(s), ansi.Strip(s); got != want {
		t.Fatalf("StripSGR = %q、x/ansi は %q", got, want)
	}
	if Of(StripSGR(s)) != Of(s) {
		t.Fatalf("Of(StripSGR(s)) = %d、Of(s) = %d", Of(StripSGR(s)), Of(s))
	}
	for n := 1; n < Of(s); n++ {
		got := DropColumns(s, n)
		if Of(got) != Of(s)-n {
			t.Fatalf("DropColumns(s, %d) の幅 %d (want %d): %q", n, Of(got), Of(s)-n, got)
		}
		if plain, want := ansi.Strip(got), ansi.Strip(s); plain != want[len(want)-len(plain):] {
			t.Fatalf("DropColumns(s, %d) の文字 %q が元の末尾と食い違う", n, plain)
		}
	}
}
