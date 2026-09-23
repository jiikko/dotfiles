package layout

import (
	"strings"
	"testing"

	"tuikit/termwidth"
)

func rowsN(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat("x", 30)
	}
	return out
}

// thumb の位置 (何行目から何行目か) と、バー列が付いているかを返す。
func thumbSpan(t *testing.T, out []string) (start, length int) {
	t.Helper()
	start = -1
	for i, l := range out {
		switch {
		case strings.HasSuffix(l, ScrollbarThumb):
			if start < 0 {
				start = i
			}
			length++
		case strings.HasSuffix(l, ScrollbarTrack):
		default:
			t.Fatalf("行 %d にバー列が無い: %q", i, l)
		}
	}
	return start, length
}

func TestScrollbarFitsWithoutBar(t *testing.T) {
	rows := rowsN(10)
	if out := Scrollbar(rows, 20, 10, 0, false); strings.Join(out, "|") != strings.Join(rows, "|") {
		t.Fatalf("全体が収まるのにバーを足した: %q", out)
	}
}

// thumb は先頭で上端、末尾で下端に接地し、長さは表示比率。
func TestScrollbarThumbPosition(t *testing.T) {
	const view, total = 10, 40
	if s, n := thumbSpan(t, Scrollbar(rowsN(view), 20, total, 0, false)); s != 0 || n != view*view/total {
		t.Fatalf("先頭: thumb=%d+%d, want 0+%d", s, n, view*view/total)
	}
	s, n := thumbSpan(t, Scrollbar(rowsN(view), 20, total, total-view, false))
	if s+n != view {
		t.Fatalf("末尾で下端に接地しない: thumb=%d+%d (view %d)", s, n, view)
	}
	// 途中は上端と下端の間
	if s, _ := thumbSpan(t, Scrollbar(rowsN(view), 20, total, (total-view)/2, false)); s <= 0 || s >= view-n {
		t.Fatalf("中ほどの thumb が端に居る: start=%d", s)
	}
	// 表示比率が極小でも最低 1 行
	if _, n := thumbSpan(t, Scrollbar(rowsN(view), 20, 10000, 0, false)); n != 1 {
		t.Fatalf("極小比率の thumb = %d 行, want 1", n)
	}
}

// どの行も幅ちょうど (本文は ScrollbarWidth ぶん詰める。色の SGR がバーへ滲まない)。
func TestScrollbarKeepsWidth(t *testing.T) {
	rows := []string{"\x1b[31m赤い行が長く長く長く続く\x1b[0m", "短い", ""}
	for _, colored := range []bool{false, true} {
		for w := 1; w <= 30; w++ {
			for i, l := range Scrollbar(rows, w, 10, 3, colored) {
				if got := termwidth.Of(l); got > w || (w > ScrollbarWidth && got != w) {
					t.Fatalf("colored=%v w=%d 行 %d: 幅 %d: %q", colored, w, i, got, l)
				}
			}
		}
	}
	// バー列すら入らない幅ではバーを描かない
	for _, l := range Scrollbar(rows, ScrollbarWidth, 10, 0, false) {
		if strings.HasSuffix(l, ScrollbarThumb) || strings.HasSuffix(l, ScrollbarTrack) {
			t.Fatalf("極小幅でバーを描いた: %q", l)
		}
	}
}
