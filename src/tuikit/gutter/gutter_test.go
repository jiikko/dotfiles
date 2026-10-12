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
