package pyunicode

import (
	"unicode/utf8"

	"ja-writing/internal/pyfs"
)

// DecodeRuneInString は s の先頭の 1 字と、その字のバイト数を返す。utf8.DecodeRuneInString と同じだが、
// 単独のサロゲート (U+D800..U+DFFF を 3 バイトで書いたもの。WTF-8) も 1 字として読む。
// どちらの形でもない並びは (utf8.RuneError, 1) を返す (Python の str には無い入力なので、1 バイトずつ U+FFFD として読む)。s が空なら (utf8.RuneError, 0)。
func DecodeRuneInString(s string) (rune, int) { return pyfs.DecodeRune(s) }

// DecodeLastRuneInString は s の末尾の 1 字と、その字のバイト数を返す (DecodeRuneInString の末尾版)。
func DecodeLastRuneInString(s string) (rune, int) {
	r, n := utf8.DecodeLastRuneInString(s)
	if r == utf8.RuneError && n == 1 && len(s) >= 3 {
		if r3, n3 := DecodeRuneInString(s[len(s)-3:]); n3 == 3 && r3 != utf8.RuneError {
			return r3, 3
		}
	}
	return r, n
}

// IsSurrogate は r がサロゲート (U+D800..U+DFFF) かを返す。
func IsSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// AppendRune は r を UTF-8 で b の後ろに足す。サロゲートも 3 バイトの形で足す (utf8.AppendRune は U+FFFD にする)。
func AppendRune(b []byte, r rune) []byte {
	if IsSurrogate(r) {
		return pyfs.AppendSurrogate(b, r)
	}
	return utf8.AppendRune(b, r)
}

// Runes は s を 1 字ずつの列にする (単独のサロゲートも 1 字)。
func Runes(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRuneInString(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// FromRunes は Runes の逆。
func FromRunes(rs []rune) string {
	b := make([]byte, 0, len(rs))
	for _, r := range rs {
		b = AppendRune(b, r)
	}
	return string(b)
}

// RuneCount は s の字数 (Python の len(str))。
func RuneCount(s string) int {
	n := 0
	for i := 0; i < len(s); {
		_, w := DecodeRuneInString(s[i:])
		i += w
		n++
	}
	return n
}

// ValidUTF8 は s が厳密な UTF-8 (サロゲートの形を含まない) かを返す。Python の bytes.decode("utf-8") が通る入力と同じ。
func ValidUTF8(s string) bool { return utf8.ValidString(s) }
