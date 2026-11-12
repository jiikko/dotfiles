package pyjson

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"ja-writing/internal/pyunicode"
)

// Separators は json.dumps の separators=(item, key)。
type Separators struct{ Item, Key string }

// Options は json.dumps の引数 (ensure_ascii は常に True)。零値は json.dumps(v, ensure_ascii=True) と同じ。
type Options struct {
	// Indented が真のとき indent=Indent で整形する (Python の indent=None が偽。Indent=0 は改行だけを入れる)。
	Indented bool
	Indent   int
	SortKeys bool
	// Separators が nil のとき、Python と同じく indent があれば (",", ": ")、なければ (", ", ": ")。
	Separators *Separators
}

// Dumps は json.dumps(v, ensure_ascii=True, **opts) と同じ文字列を返す。
// 数の桁数が上限 (4300 桁) を超える整数と、Value の型でないものは Python と同じ種類のエラーにする。
// 循環した値 (自分を要素に持つ []Value・*Object) は受けない: 検出せず再帰が戻らない。Python は ValueError("Circular reference detected") にするが、
// 循環した値を作る経路が無い (本家は json.dumps に循環した値を渡さず、Value は移植したコードが木として組み立てる)。作る経路を足すなら、
// 今の経路にある容器を覚えて再入で ClassValueError を返す検出 (共有しているだけの容器は拒否しない) を同じ変更で足す。
func Dumps(v Value, opts Options) (string, error) {
	e := encoder{opts: opts}
	e.item, e.key = ", ", ": "
	if opts.Indented {
		e.item = ","
	}
	if opts.Separators != nil {
		e.item, e.key = opts.Separators.Item, opts.Separators.Key
	}
	if opts.Indented {
		e.indent = strings.Repeat(" ", max(opts.Indent, 0))
	}
	if err := e.encode(v, 0); err != nil {
		return "", err
	}
	return e.b.String(), nil
}

type encoder struct {
	opts      Options
	item, key string
	indent    string
	b         strings.Builder
}

func (e *encoder) newline(level int) {
	e.b.WriteByte('\n')
	for range level {
		e.b.WriteString(e.indent)
	}
}

func (e *encoder) encode(v Value, level int) error {
	switch x := v.(type) {
	case nil:
		e.b.WriteString("null")
	case bool:
		if x {
			e.b.WriteString("true")
		} else {
			e.b.WriteString("false")
		}
	case string:
		writeString(&e.b, x)
	case float64:
		e.b.WriteString(floatJSON(x))
	case []Value:
		return e.encodeList(x, level)
	case *Object:
		return e.encodeObject(x, level)
	default:
		return e.encodeInt(v)
	}
	return nil
}

func (e *encoder) encodeInt(v Value) error {
	var s string
	switch x := v.(type) {
	case int:
		s = strconv.FormatInt(int64(x), 10)
	case int64:
		s = strconv.FormatInt(x, 10)
	case int32:
		s = strconv.FormatInt(int64(x), 10)
	case uint:
		s = strconv.FormatUint(uint64(x), 10)
	case uint64:
		s = strconv.FormatUint(x, 10)
	case *big.Int:
		if x == nil {
			return &Error{Class: ClassTypeError, Msg: "Object of type NoneType is not JSON serializable"}
		}
		if digitsExceeded(bigDigits(x)) {
			return &Error{Class: ClassValueError, Msg: "Exceeds the limit (4300 digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit"}
		}
		s = x.String()
	default:
		return &Error{Class: ClassTypeError, Msg: fmt.Sprintf("Object of type %T is not JSON serializable", v)}
	}
	e.b.WriteString(s)
	return nil
}

func (e *encoder) encodeList(list []Value, level int) error {
	if len(list) == 0 {
		e.b.WriteString("[]")
		return nil
	}
	e.b.WriteByte('[')
	if e.opts.Indented {
		e.newline(level + 1)
	}
	for i, x := range list {
		if i > 0 {
			e.b.WriteString(e.item)
			if e.opts.Indented {
				e.newline(level + 1)
			}
		}
		if err := e.encode(x, level+1); err != nil {
			return err
		}
	}
	if e.opts.Indented {
		e.newline(level)
	}
	e.b.WriteByte(']')
	return nil
}

func (e *encoder) encodeObject(o *Object, level int) error {
	if o == nil || o.Len() == 0 {
		e.b.WriteString("{}")
		return nil
	}
	keys := o.Keys()
	if e.opts.SortKeys {
		// WTF-8 のバイト列の順は、コードポイントの順 (Python の str の比較) と同じ。
		sort.Strings(keys)
	}
	e.b.WriteByte('{')
	if e.opts.Indented {
		e.newline(level + 1)
	}
	for i, k := range keys {
		if i > 0 {
			e.b.WriteString(e.item)
			if e.opts.Indented {
				e.newline(level + 1)
			}
		}
		writeString(&e.b, k)
		e.b.WriteString(e.key)
		val, _ := o.Get(k)
		if err := e.encode(val, level+1); err != nil {
			return err
		}
	}
	if e.opts.Indented {
		e.newline(level)
	}
	e.b.WriteByte('}')
	return nil
}

// writeString は ensure_ascii=True の文字列の書き方 (ASCII の印字文字以外は \uXXXX の小文字の 16 進。BMP の外はサロゲートの組)。
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, n := pyunicode.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r >= ' ' && r <= '~':
			b.WriteByte(byte(r))
		case r > 0xFFFF:
			r -= 0x10000
			fmt.Fprintf(b, `\u%04x\u%04x`, 0xD800|(r>>10)&0x3FF, 0xDC00|r&0x3FF)
		default:
			fmt.Fprintf(b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
}

func floatJSON(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return FloatRepr(f)
}

// FloatRepr は Python の repr(float) (str(float) も同じ)。最短で元の値に戻る桁を使い、指数が -4 未満か 16 以上なら指数の形 ("1e-05"・"1e+16") にする。
// inf と nan は "inf"・"nan" (json.dumps は別に "Infinity"・"NaN" と書く)。
func FloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	sign := ""
	if math.Signbit(f) {
		sign = "-"
		f = -f
	}
	if f == 0 {
		return sign + "0.0"
	}
	// 'e' の最短表現は "d.ddde±XX"。仮数の数字と、小数点の位置 (decpt: 0.DIGITS × 10^decpt) に直す。
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	decpt := exp + 1
	switch {
	case decpt > 16 || decpt < -3:
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		expSign := "+"
		if exp < 0 {
			expSign, exp = "-", -exp
		}
		return fmt.Sprintf("%s%se%s%02d", sign, out, expSign, exp)
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}
