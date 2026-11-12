package visibility

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// utf8BOM は U+FEFF の UTF-8 (ソースに字そのものを書くと見えない)。
const utf8BOM = "\xef\xbb\xbf"

// SourceInfo は DecodeSource の情報 (本家の {"bom": ..., "eol": ...})。EOL は "lf"・"crlf"・"cr"・"mixed"・"none"。
type SourceInfo struct {
	BOM bool
	EOL string
}

// DecodeSourceBytes は decode_source(bytes)。UTF-8 として厳密に解き (読めなければ UnicodeDecodeError)、先頭の BOM を 1 つ外し、CRLF と CR を LF にそろえる。
func DecodeSourceBytes(data []byte) (string, SourceInfo, error) {
	text, err := decodeUTF8Strict(data)
	if err != nil {
		return "", SourceInfo{}, err
	}
	out, info := DecodeSource(text)
	return out, info, nil
}

// DecodeSource は decode_source(str)。解く処理を飛ばし、BOM と改行の処理だけをする。
func DecodeSource(text string) (string, SourceInfo) {
	bom := strings.HasPrefix(text, utf8BOM)
	if bom {
		text = text[len(utf8BOM):]
	}
	if !strings.Contains(text, "\r") {
		eol := "none"
		if strings.Contains(text, "\n") {
			eol = "lf"
		}
		return text, SourceInfo{BOM: bom, EOL: eol}
	}
	crlf := strings.Count(text, "\r\n")
	cr := strings.Count(text, "\r") - crlf
	lf := strings.Count(text, "\n") - crlf
	var kinds []string
	for _, k := range []struct {
		name  string
		count int
	}{{"lf", lf}, {"crlf", crlf}, {"cr", cr}} {
		if k.count > 0 {
			kinds = append(kinds, k.name)
		}
	}
	eol := "mixed"
	if len(kinds) == 1 {
		eol = kinds[0]
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, SourceInfo{BOM: bom, EOL: eol}
}

// utf8Problem は CPython の UTF-8 の復号器が止まる所。Start から End (含まない) までのバイトが 1 つの誤りで、Reason が理由。
type utf8Problem struct {
	Start, End int
	Reason     string
}

func isContinuation(b byte) bool { return b&0xC0 == 0x80 }

// scanUTF8 は data[s:] の先頭の 1 字の長さを返す。読めなければ size は 0 で、誤りの範囲と理由を返す。
// 範囲と理由は CPython の Objects/stringlib/codecs.h (utf8_decode) と Objects/unicodeobject.c (unicode_decode_utf8) の規則どおり:
// 先頭が不正なら 1 バイト、継続バイトが違えば手前まで (最初の継続バイトが違えば先頭の 1 バイト)、途中で終われば残り全部。
func scanUTF8(data []byte, s int) (size int, p utf8Problem) {
	n := len(data) - s
	b0 := data[s]
	bad := func(end int, reason string) (int, utf8Problem) { return 0, utf8Problem{s, end, reason} }
	short := func() (int, utf8Problem) { return bad(len(data), "unexpected end of data") }
	switch {
	case b0 < 0x80:
		return 1, utf8Problem{}
	case b0 < 0xC2:
		return bad(s+1, "invalid start byte")
	case b0 < 0xE0:
		if n < 2 {
			return short()
		}
		if !isContinuation(data[s+1]) {
			return bad(s+1, "invalid continuation byte")
		}
		return 2, utf8Problem{}
	case b0 < 0xF0:
		if n < 3 {
			if n < 2 {
				return short()
			}
			b1 := data[s+1]
			if !isContinuation(b1) || (b0 == 0xE0 && b1 < 0xA0) || (b0 == 0xED && b1 >= 0xA0) {
				return bad(s+1, "invalid continuation byte")
			}
			return short()
		}
		b1, b2 := data[s+1], data[s+2]
		if !isContinuation(b1) || (b0 == 0xE0 && b1 < 0xA0) || (b0 == 0xED && b1 >= 0xA0) {
			return bad(s+1, "invalid continuation byte")
		}
		if !isContinuation(b2) {
			return bad(s+2, "invalid continuation byte")
		}
		return 3, utf8Problem{}
	case b0 < 0xF5:
		first := func(b1 byte) bool {
			return !isContinuation(b1) || (b0 == 0xF0 && b1 < 0x90) || (b0 == 0xF4 && b1 >= 0x90)
		}
		if n < 4 {
			if n < 2 {
				return short()
			}
			if first(data[s+1]) {
				return bad(s+1, "invalid continuation byte")
			}
			if n < 3 {
				return short()
			}
			if !isContinuation(data[s+2]) {
				return bad(s+2, "invalid continuation byte")
			}
			return short()
		}
		if first(data[s+1]) {
			return bad(s+1, "invalid continuation byte")
		}
		if !isContinuation(data[s+2]) {
			return bad(s+2, "invalid continuation byte")
		}
		if !isContinuation(data[s+3]) {
			return bad(s+3, "invalid continuation byte")
		}
		return 4, utf8Problem{}
	default:
		return bad(s+1, "invalid start byte")
	}
}

// decodeUTF8Strict は data.decode("utf-8")。読めなければ UnicodeDecodeError (文は CPython と同じ)。
func decodeUTF8Strict(data []byte) (string, *Error) {
	for i := 0; i < len(data); {
		size, p := scanUTF8(data, i)
		if size == 0 {
			return "", newError(TypeUnicodeDecodeError, decodeErrorText(data, p))
		}
		i += size
	}
	return string(data), nil
}

// decodeUTF8Replace は data.decode("utf-8", errors="replace")。誤り 1 つ (scanUTF8 の範囲) を U+FFFD 1 字にする。
func decodeUTF8Replace(data []byte) string {
	var b strings.Builder
	for i := 0; i < len(data); {
		size, p := scanUTF8(data, i)
		if size == 0 {
			b.WriteRune(utf8.RuneError)
			i = p.End
			continue
		}
		b.Write(data[i : i+size])
		i += size
	}
	return b.String()
}

// utf8ErrorStart は data が UTF-8 として読めないときの誤りの先頭のバイトの添字 (UnicodeDecodeError.start)。読めるなら -1。
func utf8ErrorStart(data []byte) int {
	for i := 0; i < len(data); {
		size, p := scanUTF8(data, i)
		if size == 0 {
			return p.Start
		}
		i += size
	}
	return -1
}

func decodeErrorText(data []byte, p utf8Problem) string {
	if p.End == p.Start+1 {
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[p.Start], p.Start, p.Reason)
	}
	return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", p.Start, p.End-1, p.Reason)
}
