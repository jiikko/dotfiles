package pyunicode

import (
	"fmt"
	"unicode/utf8"
)

// DecodeRuneInString は s の先頭の 1 字と、その字のバイト数を返す。utf8.DecodeRuneInString と同じだが、
// 単独のサロゲート (U+D800..U+DFFF を 3 バイトで書いたもの。WTF-8) も 1 字として読む。
// どちらの形でもない並びは (utf8.RuneError, 1) を返す (Python の str には無い入力なので、1 バイトずつ U+FFFD として読む)。s が空なら (utf8.RuneError, 0)。
func DecodeRuneInString(s string) (rune, int) {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && n == 1 && len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return rune(0xD000) | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return r, n
}

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
		return append(b, 0xED, 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
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

// EncodeError は Python の str.encode("utf-8", "surrogateescape") が投げる UnicodeEncodeError (start・end はコードポイントの添字)。
type EncodeError struct {
	Start, End int
	Char       rune // Start の字 (End-Start が 1 のときに文に出す)
}

// Error は UnicodeEncodeError の文 (str(e))。1 字なら字と位置、2 字以上なら位置の範囲を出す。
func (e *EncodeError) Error() string {
	if e.End-e.Start == 1 {
		return fmt.Sprintf("'utf-8' codec can't encode character '\\u%04x' in position %d: surrogates not allowed", e.Char, e.Start)
	}
	return fmt.Sprintf("'utf-8' codec can't encode characters in position %d-%d: surrogates not allowed", e.Start, e.End-1)
}

// EncodeSurrogateEscape は Python の os.fsencode(s) (macOS では s.encode("utf-8", "surrogateescape")) と同じバイト列を返す。
// s は WTF-8 の文字列。U+DC80..U+DCFF のサロゲートは 1 バイト (下位 8 ビット) に戻し、ほかのサロゲートは *EncodeError にする。
// 位置は、続いたサロゲートの並びのうち、戻せない最初の字から並びの終わりまで (CPython の UTF-8 の encoder が error handler に渡す範囲)。
// UTF-8 としても WTF-8 としても読めないバイトは、そのバイトを surrogateescape で読んだ字 (U+DC80..U+DCFF) とみなして、そのまま出す
// (OS から受けた値を Python が os.fsdecode で読んで os.fsencode で戻すのと同じ。WTF-8 のサロゲートの形 (ED A0..BF xx) の生のバイトは
// サロゲートとして読むので、本家と答えが違う。macOS のパスはこの形を持てない)。
func EncodeSurrogateEscape(s string) (string, error) {
	type char struct {
		r   rune
		raw string // 出すバイト (サロゲートでない字と、読めないバイト)
	}
	var chars []char
	for i := 0; i < len(s); {
		r, n := DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			r = 0xDC00 | rune(s[i])
		}
		chars = append(chars, char{r, s[i : i+n]})
		i += n
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(chars); {
		if !IsSurrogate(chars[i].r) {
			out = append(out, chars[i].raw...)
			i++
			continue
		}
		end := i
		for end < len(chars) && IsSurrogate(chars[end].r) {
			end++
		}
		for ; i < end; i++ {
			if r := chars[i].r; r < 0xDC80 || r > 0xDCFF {
				return "", &EncodeError{Start: i, End: end, Char: r}
			}
			out = append(out, byte(chars[i].r))
		}
	}
	return string(out), nil
}
