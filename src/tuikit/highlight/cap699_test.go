package highlight

import (
	"strings"
	"testing"
)

// 上限を超える 1 行は色を付けずに素通しする (chroma の lexer が 2 乗で伸びる言語がある。issue 699 の 1)。上限ちょうどまでは色が付く。
func TestLongLineIsPassedThrough(t *testing.T) {
	short := "const a = 1;"
	if got := Lang("typescript", short); !strings.Contains(got, "\x1b[") {
		t.Fatalf("短い行に色が付かない: %q", got)
	}
	atCap := short + strings.Repeat(" ", maxLineBytes-len(short))
	if got := Lang("typescript", atCap); !strings.Contains(got, "\x1b[") {
		t.Fatal("上限ちょうどの行に色が付かない (上限の境界がずれている)")
	}
	long := short + strings.Repeat("あ", maxLineBytes)
	if got := Lang("typescript", long); got != long {
		t.Fatal("上限を超える行に lexer を通した")
	}
}

// 上限ちょうどの行の所要 (傾向を見るためのもの。合否は見ない)。
func BenchmarkTSJapaneseAtCap(b *testing.B) {
	line := "const s = \"" + strings.Repeat("あ", (maxLineBytes-20)/3) + "\";"
	for b.Loop() {
		_ = Lang("typescript", line)
	}
}
