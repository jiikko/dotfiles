package pyjson

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"testing"

	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
)

const goldenDir = "../../testdata/golden"

func loadJSONGolden(t *testing.T) oracle.JSONGolden {
	t.Helper()
	var g oracle.JSONGolden
	if err := oracle.ReadGolden(goldenDir, oracle.JSONFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	return g
}

// decodeTagged は記録の符号化 (oracle/py/record_json.py の docstring) を Value に戻す。
func decodeTagged(t *testing.T, raw json.RawMessage) Value {
	t.Helper()
	var any0 any
	if err := json.Unmarshal(raw, &any0); err != nil {
		t.Fatalf("記録の値を読めません: %v: %s", err, raw)
	}
	return fromAny(t, any0)
}

func fromAny(t *testing.T, a any) Value {
	t.Helper()
	switch x := a.(type) {
	case nil, bool, string:
		return x
	case []any:
		out := make([]Value, len(x))
		for i, e := range x {
			out[i] = fromAny(t, e)
		}
		return out
	case map[string]any:
		if len(x) != 1 {
			t.Fatalf("符号化の印が 1 つでない: %v", x)
		}
		for tag, body := range x {
			return fromTag(t, tag, body)
		}
	}
	t.Fatalf("記録の値の型 %T を扱えません", a)
	return nil
}

func fromTag(t *testing.T, tag string, body any) Value {
	t.Helper()
	switch tag {
	case "i":
		n, ok := new(big.Int).SetString(body.(string), 0)
		if !ok {
			t.Fatalf("整数 %v を読めません", body)
		}
		return normInt(n)
	case "f":
		var bits uint64
		if _, err := fmt.Sscanf(body.(string), "%x", &bits); err != nil {
			t.Fatalf("浮動小数点数 %v を読めません: %v", body, err)
		}
		return math.Float64frombits(bits)
	case "ws":
		return pyunicode.FromRunes(runesOf(body.([]any)))
	case "r":
		pair := body.([]any)
		var rs []rune
		for c := rune(pair[0].(float64)); c < rune(pair[1].(float64)); c++ {
			rs = append(rs, c)
		}
		return pyunicode.FromRunes(rs)
	case "o":
		obj := NewObject()
		for _, kv := range body.([]any) {
			pair := kv.([]any)
			obj.Set(fromAny(t, pair[0]).(string), fromAny(t, pair[1]))
		}
		return obj
	}
	t.Fatalf("未知の印 %q", tag)
	return nil
}

func runesOf(a []any) []rune {
	out := make([]rune, len(a))
	for i, v := range a {
		out[i] = rune(v.(float64))
	}
	return out
}

func normInt(n *big.Int) Value {
	if n.IsInt64() {
		return n.Int64()
	}
	return n
}

// equalValue は Python の == に近い比較 (nan どうしは同じとみなし、浮動小数点数はビットで比べ、dict は鍵の順も比べる)。
func equalValue(a, b Value) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (math.Float64bits(x) == math.Float64bits(y) || (math.IsNaN(x) && math.IsNaN(y)))
	case *big.Int:
		y, ok := b.(*big.Int)
		return ok && x.Cmp(y) == 0
	case []Value:
		y, ok := b.([]Value)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case *Object:
		y, ok := b.(*Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		xk, yk := x.Keys(), y.Keys()
		for i := range xk {
			if xk[i] != yk[i] {
				return false
			}
			xv, _ := x.Get(xk[i])
			yv, _ := y.Get(yk[i])
			if !equalValue(xv, yv) {
				return false
			}
		}
		return true
	}
	return a == b
}

func show(v Value) string {
	s, err := Dumps(v, Options{})
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func optionsOf(o oracle.DumpOpts) Options {
	opts := Options{SortKeys: o.SortKeys}
	if o.Indent != nil {
		opts.Indented, opts.Indent = true, *o.Indent
	}
	if len(o.Separators) == 2 {
		opts.Separators = &Separators{Item: o.Separators[0], Key: o.Separators[1]}
	}
	return opts
}

func checkError(t *testing.T, what string, got error, want *oracle.PyError) {
	t.Helper()
	var e *Error
	if !errors.As(got, &e) {
		t.Fatalf("%s: エラーが %v (Python は %s)", what, got, want.Type)
	}
	if e.Class != want.Type {
		t.Fatalf("%s: エラーの種類 %s: %s (Python は %s: %s)", what, e.Class, e, want.Type, want.Msg)
	}
	switch want.Type {
	case ClassRecursionError, ClassUnicodeDecodeError:
		// メッセージに Python の C のスタックの使用量や、バイトの位置の説明が入るので種類だけを比べる。
	case ClassJSONDecodeError:
		if e.Msg != want.JMsg || e.Pos != want.Pos || e.Line != want.Line || e.Col != want.Col || e.Error() != want.Msg {
			t.Fatalf("%s: JSONDecodeError %q (pos %d line %d col %d)、Python は %q (pos %d line %d col %d)",
				what, e.Error(), e.Pos, e.Line, e.Col, want.Msg, want.Pos, want.Line, want.Col)
		}
	default:
		if e.Msg != want.Msg {
			t.Fatalf("%s: %s %q、Python は %q", what, e.Class, e.Msg, want.Msg)
		}
	}
}

func TestDumpsMatchesPython(t *testing.T) {
	g := loadJSONGolden(t)
	var ok, errs, digests int
	for i, c := range g.Dumps {
		v := decodeTagged(t, c.Value)
		what := fmt.Sprintf("dumps #%d (%s) opts %+v", i, show(v), c.Opts)
		got, err := Dumps(v, optionsOf(c.Opts))
		switch {
		case c.Error != nil:
			if err == nil {
				t.Fatalf("%s: Python は %s だが、%q を返した", what, c.Error.Type, got)
			}
			checkError(t, what, err, c.Error)
			errs++
		case err != nil:
			t.Fatalf("%s: エラー %v、Python は %q", what, err, *c.Out)
		case c.Out != nil:
			if got != *c.Out {
				t.Fatalf("%s:\n got  %q\n want %q", what, got, *c.Out)
			}
			ok++
		default:
			sum := sha256.Sum256([]byte(got))
			if hex.EncodeToString(sum[:]) != c.Digest || len(got) != c.Len {
				t.Fatalf("%s: 出力の SHA-256 か長さ (%d) が Python (%d) と違う", what, len(got), c.Len)
			}
			digests++
		}
	}
	if ok < 3000 || errs < 1 || digests < 200 {
		t.Fatalf("記録の偏り: 本文 %d・エラー %d・digest %d", ok, errs, digests)
	}
}

func inputOf(t *testing.T, c oracle.LoadCase) []byte {
	t.Helper()
	switch {
	case c.Text != nil:
		return []byte(*c.Text)
	case c.Hex != nil:
		b, err := hex.DecodeString(*c.Hex)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var b strings.Builder
	for _, p := range c.Rep {
		b.WriteString(strings.Repeat(p.Unit, p.N))
	}
	return []byte(b.String())
}

// summarize は深い入れ子の結果を、記録と同じ数え方 (節点・最大の深さ・葉の型ごとの数) で縮める。再帰しない。
func summarize(v Value) oracle.Summary {
	s := oracle.Summary{Leaves: map[string]int{}}
	type item struct {
		v Value
		d int
	}
	stack := []item{{v, 1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		s.Nodes++
		s.Depth = max(s.Depth, it.d)
		switch x := it.v.(type) {
		case []Value:
			for _, e := range x {
				stack = append(stack, item{e, it.d + 1})
			}
		case *Object:
			for _, k := range x.Keys() {
				e, _ := x.Get(k)
				stack = append(stack, item{e, it.d + 1})
			}
		case nil:
			s.Leaves["NoneType"]++
		case bool:
			s.Leaves["bool"]++
		case int64, *big.Int:
			s.Leaves["int"]++
		case float64:
			s.Leaves["float"]++
		case string:
			s.Leaves["str"]++
		}
	}
	return s
}

func TestLoadsMatchesPython(t *testing.T) {
	g := loadJSONGolden(t)
	var ok, errs, deep int
	classes := map[string]int{}
	for i, c := range g.Loads {
		in := inputOf(t, c)
		show := string(in)
		if len(show) > 60 {
			show = show[:60] + "…"
		}
		what := fmt.Sprintf("loads #%d (%q)", i, show)
		got, err := LoadsBytes(in)
		if c.Error != nil {
			if err == nil {
				t.Fatalf("%s: Python は %s (%s) だが、%s を返した", what, c.Error.Type, c.Error.Msg, show2(got))
			}
			checkError(t, what, err, c.Error)
			classes[c.Error.Type]++
			errs++
			continue
		}
		if err != nil {
			t.Fatalf("%s: エラー %v、Python は成功", what, err)
		}
		if c.Summary != nil {
			if want, have := *c.Summary, summarize(got); fmt.Sprint(want) != fmt.Sprint(have) {
				t.Fatalf("%s: 形の要約 %+v、Python は %+v", what, have, want)
			}
			deep++
			continue
		}
		if want := decodeTagged(t, c.V); !equalValue(got, want) {
			t.Fatalf("%s:\n got  %s\n want %s", what, show2(got), show2(want))
		}
		ok++
	}
	if ok < 1000 || errs < 150 || deep < 5 {
		t.Fatalf("記録の偏り: 成功 %d・失敗 %d・深い入れ子 %d", ok, errs, deep)
	}
	for _, class := range []string{ClassJSONDecodeError, ClassValueError, ClassUnicodeDecodeError, ClassRecursionError} {
		if classes[class] == 0 {
			t.Errorf("失敗の種類 %s の記録が 1 件も無い", class)
		}
	}
}

func show2(v Value) string {
	s, err := Dumps(v, Options{})
	if err != nil {
		return err.Error()
	}
	return s
}

func TestRoundMatchesPython(t *testing.T) {
	g := loadJSONGolden(t)
	var rounded, errs int
	for i, c := range g.Round {
		x := decodeTagged(t, json.RawMessage(fmt.Sprintf(`{"f":%q}`, c.X))).(float64)
		what := fmt.Sprintf("round #%d (%v, %d)", i, x, c.N)
		got, err := Round(x, c.N)
		if c.Error != nil {
			if err == nil {
				t.Fatalf("%s: Python は %s だが、%v を返した", what, c.Error.Type, got)
			}
			checkError(t, what, err, c.Error)
			errs++
			continue
		}
		if err != nil {
			t.Fatalf("%s: エラー %v", what, err)
		}
		want := decodeTagged(t, c.Result)
		if !equalValue(got, want) {
			t.Fatalf("%s = %v (%016x)、Python は %v (%016x)", what, got, math.Float64bits(got), want, math.Float64bits(want.(float64)))
		}
		rounded++
	}
	if rounded < 3000 || errs < 1 {
		t.Fatalf("記録の偏り: 成功 %d・失敗 %d", rounded, errs)
	}
}

func TestRoundIntMatchesPython(t *testing.T) {
	g := loadJSONGolden(t)
	var ok, errs int
	for i, c := range g.RoundInt {
		x := decodeTagged(t, json.RawMessage(fmt.Sprintf(`{"f":%q}`, c.X))).(float64)
		what := fmt.Sprintf("round_int #%d (%v)", i, x)
		got, err := RoundInt(x)
		if c.Error != nil {
			if err == nil {
				t.Fatalf("%s: Python は %s だが、%v を返した", what, c.Error.Type, got)
			}
			checkError(t, what, err, c.Error)
			errs++
			continue
		}
		if err != nil {
			t.Fatalf("%s: エラー %v", what, err)
		}
		if want := decodeTagged(t, c.Result); !equalValue(normInt(got), want) {
			t.Fatalf("%s = %v、Python は %v", what, got, want)
		}
		ok++
	}
	if ok < 100 || errs < 2 {
		t.Fatalf("記録の偏り: 成功 %d・失敗 %d", ok, errs)
	}
}
