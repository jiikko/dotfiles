package lineedit

import "testing"

// 文字の向きを変える制御文字と行・段落の区切りは入れない (見えている文字列と中身の並びが食い違う。issue 700 の 2)。
// ZWJ と異体字セレクタは絵文字の一部として通す。
func TestInsertDropsBidiAndLineSeparators(t *testing.T) {
	for _, r := range []rune{0x202a, 0x202e, 0x2066, 0x2069} {
		var l Line
		l.Insert("a" + string(r) + "b")
		if got := l.String(); got != "ab" {
			t.Errorf("%U が入った: %q", r, got)
		}
	}
	for _, r := range []rune{0x2028, 0x2029} { // 行・段落の区切りは改行と同じく空白にする
		var l Line
		l.Insert("a" + string(r) + "b")
		if got := l.String(); got != "a b" {
			t.Errorf("%U: %q (空白になるはず)", r, got)
		}
	}
	var l Line
	keep := "👨" + string(rune(0x200d)) + "👩" + "❤" + string(rune(0xfe0f))
	l.Insert(keep)
	if l.String() != keep {
		t.Fatalf("ZWJ / 異体字セレクタを落とした: %q", l.String())
	}
}
