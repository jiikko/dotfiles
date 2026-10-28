package report

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortDoc(t *testing.T) {
	long := strings.Repeat("あ", 40) // 120 byte
	for _, c := range []struct {
		name string
		doc  string
		max  int
		want string
	}{
		{"縮めない (0)", "A は B する。C は D。", 0, "A は B する。C は D。"},
		{"縮めない (負)", "A は B する。C は D。", -1, "A は B する。C は D。"},
		{"最初の文まで (。)", "New は Sim を返す。start は起点。", 100, "New は Sim を返す。"},
		{"最初の文まで (. + 大文字)", "Foo returns bar. It also baz.", 100, "Foo returns bar."},
		{"末尾の . は文の終わり", "Foo returns bar.", 100, "Foo returns bar."},
		{"略語 (e.g. + 小文字) の途中では切らない", "Foo handles x, e.g. the y case.", 100, "Foo handles x, e.g. the y case."},
		{"v1.2 の . は切らない", "Supports v1.2 and later", 100, "Supports v1.2 and later"},
		{"上限内ならそのまま", "短い説明", 100, "短い説明"},
		{"上限で切ると … を付ける (rune の途中で切らない)", long, 100, strings.Repeat("あ", 33) + "…"},
		{"上限ちょうどの境界 (3 byte 文字)", long, 99, strings.Repeat("あ", 33) + "…"},
		{"文の終わりが上限内なら … を付けない", "短い。" + long, 100, "短い。"},
		{"略語 e.g. の後ろでは切らない", "e.g. Foo bar. Baz", 100, "e.g. Foo bar."},
		{"略語 Dr. の後ろでは切らない", "Dr. Smith does X. Then Y.", 100, "Dr. Smith does X."},
		{"上限が最初の文字より小さくても 1 文字は残す", "あいう", 2, "あ…"},
		{"空", "", 100, ""},
	} {
		got := ShortDoc(c.doc, c.max)
		if got != c.want {
			t.Errorf("%s: ShortDoc(%q, %d) = %q, want %q", c.name, c.doc, c.max, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: invalid UTF-8: %q", c.name, got)
		}
	}
}

func TestTruncationHeader(t *testing.T) {
	h := TruncationHint{Label: "該当", Pattern: "Foo", Ignore: true, Globs: []string{"*.go", "*.swift"}, Dirs: []string{"src", "my dir", "it's"}}
	want := "該当 14 件 (3 件表示 / 全部で約 2.4 KB)。絞るか、文字列 (コメント含む) で探すなら: rg -n -i -e Foo -g '*.go' -g '*.swift' -- src 'my dir' 'it'\\''s'"
	if got := h.header(3, 14, 2400); got != want {
		t.Errorf("header =\n%q\nwant\n%q", got, want)
	}
	if got := (TruncationHint{Label: "呼び出し", Pattern: `Foo\(`}).header(20, 25, 999); got != `呼び出し 25 件 (20 件表示 / 全部で約 999 B)。絞るか、文字列 (コメント含む) で探すなら: rg -n -e 'Foo\(' -- .` {
		t.Errorf("default dir and quoting: %q", got)
	}
	if got := (TruncationHint{Label: "該当"}).header(1, 2, 10); got != "該当 2 件 (1 件表示 / 全部で約 10 B)" {
		t.Errorf("no pattern must not suggest rg: %q", got)
	}
}

func TestShortDocNeverPanicsOrBreaksUTF8(t *testing.T) {
	for _, doc := range []string{"", "\xff\xfe broken", "あ\xe3\x81", "😀😀😀 emoji", "e\u0301\u0301 combining", "..."} {
		for max := -1; max <= 12; max++ {
			got := ShortDoc(doc, max)
			if utf8.ValidString(doc) && !utf8.ValidString(got) {
				t.Errorf("ShortDoc(%q, %d) = %q: invalid UTF-8 from valid input", doc, max, got)
			}
		}
	}
}
