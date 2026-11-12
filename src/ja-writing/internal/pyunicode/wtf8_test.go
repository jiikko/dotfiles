package pyunicode

import (
	"slices"
	"testing"
	"unicode/utf8"
)

func TestSurrogatesRoundTripThroughWTF8(t *testing.T) {
	for _, r := range []rune{0xD7FF, 0xD800, 0xDBFF, 0xDC00, 0xDFFF, 0xE000, 0x10000, 0x10FFFF, 'a', 0} {
		b := AppendRune(nil, r)
		got, n := DecodeRuneInString(string(b))
		if got != r || n != len(b) {
			t.Errorf("U+%04X を書いて読むと U+%04X (%d バイト中 %d)", r, got, len(b), n)
		}
		last, ln := DecodeLastRuneInString(string(b))
		if last != r || ln != len(b) {
			t.Errorf("U+%04X を末尾から読むと U+%04X (%d バイト中 %d)", r, last, len(b), ln)
		}
	}
	if got := AppendRune(nil, 0xD800); string(got) != "\xed\xa0\x80" {
		t.Errorf("U+D800 の書き方 % x", got)
	}
}

func TestRunesTreatsLoneSurrogatesAsSingleCharacters(t *testing.T) {
	s := FromRunes([]rune{'a', 0xD83D, 0xDE00, 'b'})
	if utf8.ValidString(s) {
		t.Fatal("単独のサロゲートが厳密な UTF-8 として通った")
	}
	if ValidUTF8(s) {
		t.Error("ValidUTF8 がサロゲートの形を通した")
	}
	if got := Runes(s); !slices.Equal(got, []rune{'a', 0xD83D, 0xDE00, 'b'}) {
		t.Errorf("Runes = %U (2 つの単独のサロゲートは 1 つの絵文字に合わさらない)", got)
	}
	if RuneCount(s) != 4 {
		t.Errorf("RuneCount = %d、期待 4", RuneCount(s))
	}
	if RuneCount("日本語😀") != 4 {
		t.Errorf("RuneCount(日本語😀) = %d", RuneCount("日本語😀"))
	}
}

func TestInvalidBytesAreReadOneByteAtATime(t *testing.T) {
	for _, s := range []string{"\xff", "\xed\xa0", "\xed\xa0\x40", "\xc0\xaf", "\xf4\x90\x80\x80"} {
		r, n := DecodeRuneInString(s)
		if r != utf8.RuneError || n != 1 {
			t.Errorf("% x を読むと U+%04X・%d バイト", s, r, n)
		}
	}
	if r, n := DecodeRuneInString(""); r != utf8.RuneError || n != 0 {
		t.Errorf("空文字列を読むと %U・%d", r, n)
	}
}

func TestNormalizeKeepsLoneSurrogates(t *testing.T) {
	s := FromRunes([]rune{0xD800, 'A', 0x0300, 0xDC00})
	want := FromRunes([]rune{0xD800, 0xC0, 0xDC00})
	if got := Normalize(NFC, s); got != want {
		t.Errorf("Normalize(NFC) = %U、期待 %U", Runes(got), Runes(want))
	}
}
