package pyjson

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"unicode/utf8"

	"yomiyasu/internal/pyunicode"
)

// MaxDepth は入れ子の深さの上限。これより深い入力は RecursionError にする (Python は入れ子の分だけ C のスタックを使い、
// 手元の Python 3.15.0 は、配列でも辞書でも深さ 130744 まで読めて、その先で "Stack overflow" の RecursionError になった)。
// 実際の境界は Python の実行環境のスタックの大きさで変わる。この値の近くの入力は揃うと主張しない。
const MaxDepth = 130744

// LoadsBytes は Python の json.loads(data.decode("utf-8")) と同じ。本家の 4 か所の呼び出し (hook の標準入力・check の記録と子の出力・レポート) は全部この形。
// UTF-8 として読めない入力 (サロゲートの形・過長な表現を含む) は UnicodeDecodeError にする (メッセージは Python の文面を再現しない)。
func LoadsBytes(data []byte) (Value, error) {
	if !utf8.Valid(data) {
		return nil, &Error{Class: ClassUnicodeDecodeError, Msg: "'utf-8' codec can't decode bytes: invalid UTF-8"}
	}
	return Loads(string(data))
}

// Loads は Python の json.loads(s) (s は str) と同じ。s は WTF-8 の文字列で、位置はコードポイントの添字で返す。
// 返す値は整数を int64 (収まらなければ *big.Int)・小数を float64・object を *Object (鍵の挿入順と、重複した鍵は最初の位置に最後の値、を保つ) にする。
func Loads(s string) (Value, error) {
	p := &parser{doc: pyunicode.Runes(s)}
	if len(p.doc) > 0 && p.doc[0] == 0xFEFF {
		return nil, p.errAt("Unexpected UTF-8 BOM (decode using utf-8-sig)", 0)
	}
	idx := p.skipSpace(0)
	v, end, err := p.value(idx, 0)
	if err != nil {
		return nil, err
	}
	end = p.skipSpace(end)
	if end != len(p.doc) {
		return nil, p.errAt("Extra data", end)
	}
	return v, nil
}

type parser struct{ doc []rune }

func (p *parser) errAt(msg string, pos int) *Error {
	line, col := 1, pos+1
	last := -1
	for i := 0; i < pos && i < len(p.doc); i++ {
		if p.doc[i] == '\n' {
			line++
			last = i
		}
	}
	if line > 1 {
		col = pos - last
	}
	return &Error{Class: ClassJSONDecodeError, Msg: msg, Pos: pos, Line: line, Col: col}
}

func (p *parser) skipSpace(i int) int {
	for i < len(p.doc) {
		switch p.doc[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func (p *parser) at(i int) rune {
	if i < len(p.doc) {
		return p.doc[i]
	}
	return -1
}

func (p *parser) hasPrefix(i int, lit string) bool {
	if i+len(lit) > len(p.doc) {
		return false
	}
	for k := range len(lit) {
		if p.doc[i+k] != rune(lit[k]) {
			return false
		}
	}
	return true
}

// value は idx から 1 つの値を読み、次の位置を返す (Python の scan_once)。値が無いときは "Expecting value" (位置は値のあるはずだった位置)。
func (p *parser) value(idx, depth int) (Value, int, error) {
	if idx >= len(p.doc) {
		return nil, 0, p.errAt("Expecting value", idx)
	}
	switch p.doc[idx] {
	case '"':
		return p.stringValue(idx)
	case '{':
		return p.object(idx+1, depth+1)
	case '[':
		return p.array(idx+1, depth+1)
	case 'n':
		if p.hasPrefix(idx, "null") {
			return nil, idx + 4, nil
		}
	case 't':
		if p.hasPrefix(idx, "true") {
			return true, idx + 4, nil
		}
	case 'f':
		if p.hasPrefix(idx, "false") {
			return false, idx + 5, nil
		}
	case 'N':
		if p.hasPrefix(idx, "NaN") {
			return math.NaN(), idx + 3, nil
		}
	case 'I':
		if p.hasPrefix(idx, "Infinity") {
			return math.Inf(1), idx + 8, nil
		}
	case '-':
		if p.hasPrefix(idx, "-Infinity") {
			return math.Inf(-1), idx + 9, nil
		}
	}
	return p.number(idx)
}

func (p *parser) stringValue(idx int) (Value, int, error) {
	s, next, err := p.str(idx + 1)
	if err != nil {
		return nil, 0, err
	}
	return s, next, nil
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// number は JSON の数 (Python の _match_number_unicode)。"1." や "1e" のように後ろが数の形でないときは、読めたところまでを数とする。
func (p *parser) number(start int) (Value, int, error) {
	idx := start
	if p.at(idx) == '-' {
		idx++
	}
	switch c := p.at(idx); {
	case c >= '1' && c <= '9':
		for isDigit(p.at(idx)) {
			idx++
		}
	case c == '0':
		idx++
	default:
		return nil, 0, p.errAt("Expecting value", start)
	}
	isFloat := false
	if p.at(idx) == '.' && isDigit(p.at(idx+1)) {
		isFloat = true
		idx += 2
		for isDigit(p.at(idx)) {
			idx++
		}
	}
	if c := p.at(idx); c == 'e' || c == 'E' {
		eStart := idx
		idx++
		if c := p.at(idx); c == '-' || c == '+' {
			idx++
		}
		for isDigit(p.at(idx)) {
			idx++
		}
		if isDigit(p.at(idx - 1)) {
			isFloat = true
		} else {
			idx = eStart
		}
	}
	text := string(p.doc[start:idx])
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil && !isRangeErr(err) {
			return nil, 0, fmt.Errorf("数 %q を読めません: %w", text, err)
		}
		return f, idx, nil
	}
	digits := idx - start
	if text[0] == '-' {
		digits--
	}
	if digitsExceeded(digits) {
		return nil, 0, &Error{Class: ClassValueError, Msg: fmt.Sprintf(
			"Exceeds the limit (4300 digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", digits)}
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n, idx, nil
	}
	big, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, 0, fmt.Errorf("整数 %q を読めません", text)
	}
	return big, idx, nil
}

func isRangeErr(err error) bool {
	return errors.Is(err, strconv.ErrRange)
}

func hexVal(r rune) (rune, bool) {
	switch {
	case r >= '0' && r <= '9':
		return r - '0', true
	case r >= 'a' && r <= 'f':
		return r - 'a' + 10, true
	case r >= 'A' && r <= 'F':
		return r - 'A' + 10, true
	}
	return 0, false
}

// hex4 は idx からの 4 字を 16 進として読む。
func (p *parser) hex4(idx int) (rune, bool) {
	var c rune
	for k := range 4 {
		d, ok := hexVal(p.doc[idx+k])
		if !ok {
			return 0, false
		}
		c = c<<4 | d
	}
	return c, true
}

// str は end (開きの引用符の次) から文字列を読み、閉じの引用符の次の位置を返す (Python の scanstring_unicode)。
func (p *parser) str(end int) (string, int, error) {
	begin := end - 1
	n := len(p.doc)
	var out []byte
	for {
		next := end
		var c rune
		for ; next < n; next++ {
			d := p.doc[next]
			if d == '"' || d == '\\' {
				c = d
				break
			}
			if d <= 0x1F {
				return "", 0, p.errAt("Invalid control character at", next)
			}
		}
		if c != '"' && c != '\\' {
			return "", 0, p.errAt("Unterminated string starting at", begin)
		}
		for _, r := range p.doc[end:next] {
			out = pyunicode.AppendRune(out, r)
		}
		next++
		if c == '"' {
			return string(out), next, nil
		}
		if next == n {
			return "", 0, p.errAt("Unterminated string starting at", begin)
		}
		c = p.doc[next]
		if c != 'u' {
			end = next + 1
			switch c {
			case '"', '\\', '/':
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			default:
				return "", 0, p.errAt("Invalid \\escape", end-2)
			}
		} else {
			next++
			end = next + 4
			if end >= n {
				return "", 0, p.errAt("Invalid \\uXXXX escape", next-1)
			}
			hi, ok := p.hex4(next)
			if !ok {
				return "", 0, p.errAt("Invalid \\uXXXX escape", end-5)
			}
			c = hi
			if hi >= 0xD800 && hi <= 0xDBFF && end+6 < n && p.doc[end] == '\\' && p.doc[end+1] == 'u' {
				end += 6
				lo, ok := p.hex4(end - 4)
				if !ok {
					return "", 0, p.errAt("Invalid \\uXXXX escape", end-5)
				}
				if lo >= 0xDC00 && lo <= 0xDFFF {
					c = 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
				} else {
					end -= 6
				}
			}
		}
		out = pyunicode.AppendRune(out, c)
	}
}

func (p *parser) array(idx, depth int) (Value, int, error) {
	if depth > MaxDepth {
		return nil, 0, &Error{Class: ClassRecursionError, Msg: "Stack overflow while decoding a JSON array from a unicode string"}
	}
	list := []Value{}
	idx = p.skipSpace(idx)
	if p.at(idx) == ']' {
		return list, idx + 1, nil
	}
	for {
		v, next, err := p.value(idx, depth)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, v)
		idx = p.skipSpace(next)
		if p.at(idx) == ']' {
			return list, idx + 1, nil
		}
		if p.at(idx) != ',' {
			return nil, 0, p.errAt("Expecting ',' delimiter", idx)
		}
		comma := idx
		idx = p.skipSpace(idx + 1)
		if p.at(idx) == ']' {
			return nil, 0, p.errAt("Illegal trailing comma before end of array", comma)
		}
	}
}

func (p *parser) object(idx, depth int) (Value, int, error) {
	if depth > MaxDepth {
		return nil, 0, &Error{Class: ClassRecursionError, Msg: "Stack overflow while decoding a JSON object from a unicode string"}
	}
	obj := NewObject()
	idx = p.skipSpace(idx)
	if p.at(idx) == '}' {
		return obj, idx + 1, nil
	}
	for {
		if p.at(idx) != '"' {
			return nil, 0, p.errAt("Expecting property name enclosed in double quotes", idx)
		}
		key, next, err := p.str(idx + 1)
		if err != nil {
			return nil, 0, err
		}
		idx = p.skipSpace(next)
		if p.at(idx) != ':' {
			return nil, 0, p.errAt("Expecting ':' delimiter", idx)
		}
		idx = p.skipSpace(idx + 1)
		v, next, err := p.value(idx, depth)
		if err != nil {
			return nil, 0, err
		}
		obj.Set(key, v)
		idx = p.skipSpace(next)
		if p.at(idx) == '}' {
			return obj, idx + 1, nil
		}
		if p.at(idx) != ',' {
			return nil, 0, p.errAt("Expecting ',' delimiter", idx)
		}
		comma := idx
		idx = p.skipSpace(idx + 1)
		if p.at(idx) == '}' {
			return nil, 0, p.errAt("Illegal trailing comma before end of object", comma)
		}
	}
}
