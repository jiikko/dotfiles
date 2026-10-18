package gutter

import "testing"

func TestWidth(t *testing.T) {
	for _, c := range []struct{ lines, want int }{
		{0, 3}, {1, 3}, {99, 3}, {100, 4}, {9999, 5}, {10000, 6},
	} {
		if got := Width(c.lines); got != c.want {
			t.Errorf("Width(%d) = %d, want %d", c.lines, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		src, width int
		want       string
	}{
		{7, 4, "  7 "},
		{123, 4, "123 "},
		{0, 4, "    "}, // 続き行は空白だけ
		{-1, 3, "   "}, // 0 以下は全部 番号なし
		{5, 0, ""},
	} {
		if got := Format(c.src, c.width); got != c.want {
			t.Errorf("Format(%d, %d) = %q, want %q", c.src, c.width, got, c.want)
		}
	}
}

// Number は番号の部分 (区切りを除いた桁・右寄せ。番号 0 は空白) を返す。Format は Number + Separator と定義しているので
// 両者の一致を見ても何も検査しない — 値はリテラルで固定する。
func TestNumber(t *testing.T) {
	for _, c := range []struct {
		src, width int
		want       string
	}{
		{7, 4, "  7"}, {0, 4, "   "}, {42, 3, "42"}, {12345, 4, "12345"}, {7, 1, "7"}, {7, 0, "7"},
	} {
		if got := Number(c.src, c.width); got != c.want {
			t.Errorf("Number(%d,%d) = %q, want %q", c.src, c.width, got, c.want)
		}
	}
}
