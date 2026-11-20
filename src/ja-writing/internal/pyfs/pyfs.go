// Package pyfs は Python の os.fsencode と os.fsdecode (macOS では utf-8 と surrogateescape) を写す。字は WTF-8 の Go の文字列で持つ
// (単独のサロゲートを 3 バイトの形で書く。pyunicode と同じ持ち方)。
//
// pyunicode に置かない: cmdenv がこの規則で環境変数を読むが、pyunicode の内部テストが oracle (→ cmdenv) を import するので、
// cmdenv が pyunicode を import するとテストの import が輪になる。このパッケージは標準ライブラリだけに依存し、WTF-8 の 1 字の読み書き
// (DecodeRune・AppendSurrogate) もここに置いて pyunicode が使う。
package pyfs

import (
	"fmt"
	"unicode/utf8"
)

// DecodeRune は s の先頭の 1 字とバイト数。utf8.DecodeRuneInString と同じだが、単独のサロゲート (U+D800..U+DFFF を 3 バイトで書いたもの。
// WTF-8) も 1 字として読む。どちらの形でもない並びは (utf8.RuneError, 1)、s が空なら (utf8.RuneError, 0)。
func DecodeRune(s string) (rune, int) {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && n == 1 && len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return rune(0xD000) | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return r, n
}

func isSurrogate(r rune) bool { return r >= 0xD800 && r <= 0xDFFF }

// AppendSurrogate はサロゲート r を WTF-8 の 3 バイトで足す (utf8.AppendRune は U+FFFD にする)。
func AppendSurrogate(b []byte, r rune) []byte {
	return append(b, 0xED, 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
}

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

// Decode は Python の os.fsdecode(b) (macOS では b.decode("utf-8", "surrogateescape")) と同じ字を WTF-8 で返す
// (Encode の逆で、どのバイト列も同じバイト列に戻る)。厳密な UTF-8 として読めないバイトは 1 つずつ U+DC00+b にする。
// WTF-8 のサロゲートの形 (ED A0..BF xx) も厳密な UTF-8 では読めないので、3 バイトとも U+DCxx になる (サロゲートの字としては読まない)。
// OS から受けた値はこれで読んでから持つ: 生のまま持つと、記録の JSON の単独のサロゲートと見分けが付かず、Encode が書き換える。
func Decode(b string) string {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRuneInString(b[i:])
		if r == utf8.RuneError && n == 1 {
			out = AppendSurrogate(out, 0xDC00|rune(b[i]))
		} else {
			out = append(out, b[i:i+n]...)
		}
		i += n
	}
	return string(out)
}

// char は WTF-8 の文字列の 1 字 (r) と、サロゲートでない字のときに出すバイト (raw)。
type char struct {
	r   rune
	raw string
}

// chars は s を字に分ける。UTF-8 としても WTF-8 としても読めないバイトは、そのバイトを surrogateescape で読んだ字 (U+DC80..U+DCFF) とみなす
// (OS から受けたバイトを Decode せずに持った値も、本家の str と同じ字に数える)。
func chars(s string) []char {
	var out []char
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		if r == utf8.RuneError && n == 1 {
			r = 0xDC00 | rune(s[i])
		}
		out = append(out, char{r, s[i : i+n]})
		i += n
	}
	return out
}

// surrogateRun は cs[i] から続くサロゲートの並びの終わり (含まない添字)。
func surrogateRun(cs []char, i int) int {
	end := i
	for end < len(cs) && isSurrogate(cs[end].r) {
		end++
	}
	return end
}

// Encode は Python の os.fsencode(s) (macOS では s.encode("utf-8", "surrogateescape")) と同じバイト列を返す。
// s は WTF-8 の文字列。U+DC80..U+DCFF のサロゲートは 1 バイト (下位 8 ビット) に戻し、ほかのサロゲートは *EncodeError にする。
// 位置は、続いたサロゲートの並びのうち、戻せない最初の字から並びの終わりまで (CPython の UTF-8 の encoder が error handler に渡す範囲)。
// OS から受けた生のバイト列を渡さない: WTF-8 のサロゲートの形 (ED A0..BF xx) はサロゲートの字として読むので、本家が同じバイトで渡す値を
// 書き換えるか断る。OS の値は Decode で読んでから渡す。
func Encode(s string) (string, error) {
	cs := chars(s)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(cs); {
		if !isSurrogate(cs[i].r) {
			out = append(out, cs[i].raw...)
			i++
			continue
		}
		end := surrogateRun(cs, i)
		for ; i < end; i++ {
			if r := cs[i].r; r < 0xDC80 || r > 0xDCFF {
				return "", &EncodeError{Start: i, End: end, Char: r}
			}
			out = append(out, byte(cs[i].r))
		}
	}
	return string(out), nil
}

// EncodeStrict は Python の s.encode("utf-8") (errors は strict) と同じバイト列を返す。サロゲートが 1 字でもあれば *EncodeError で、
// 位置は最初のサロゲートから、その並びの終わりまで。本家の標準出力 (main が reconfigure(encoding="utf-8") で errors を strict に戻す) に書く口が使う。
func EncodeStrict(s string) (string, error) {
	cs := chars(s)
	for i, c := range cs {
		if isSurrogate(c.r) {
			return "", &EncodeError{Start: i, End: surrogateRun(cs, i), Char: c.r}
		}
	}
	return s, nil
}

// BackslashReplace は Python の s.encode("utf-8", "backslashreplace") と同じバイト列を返す。サロゲートは \udcff の形の 6 字にする。
// 本家の標準エラー (_error と argparse の error が errors="backslashreplace" にしてから書く) に書く口が使う。
func BackslashReplace(s string) string {
	cs := chars(s)
	out := make([]byte, 0, len(s))
	for _, c := range cs {
		if isSurrogate(c.r) {
			out = fmt.Appendf(out, "\\u%04x", c.r)
			continue
		}
		out = append(out, c.raw...)
	}
	return string(out)
}
