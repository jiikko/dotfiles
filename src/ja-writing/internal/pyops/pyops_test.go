package pyops

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
)

func huge(t *testing.T, neg bool) pyjson.Value {
	t.Helper()
	s := "1" + strings.Repeat("0", 400)
	if neg {
		s = "-" + s
	}
	v, err := pyjson.Loads(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(*big.Int); !ok {
		t.Fatalf("10**400 が *big.Int にならない: %T", v)
	}
	return v
}

func outcome(f func() pyjson.Value) (got pyjson.Value, err error) {
	defer Catch(&err)
	return f(), nil
}

// int と float の比べ方と、float に収まらない int を float に変える所の例外が本家と同じ (Python 3.15.0 で確かめた値。E は例外の型)。
func TestNumbersMatchPython(t *testing.T) {
	h, nh := huge(t, false), huge(t, true)
	big1, _ := new(big.Int).SetString("1"+strings.Repeat("0", 399), 10)
	inf := math.Inf(1)
	for _, c := range []struct {
		name string
		f    func() pyjson.Value
		want string
	}{
		{"9007199254740993 == 9007199254740992.0", func() pyjson.Value { return Equal(int64(9007199254740993), 9007199254740992.0) }, "false"},
		{"9007199254740992 == 9007199254740992.0", func() pyjson.Value { return Equal(int64(9007199254740992), 9007199254740992.0) }, "true"},
		{"9007199254740992.0 < 9007199254740993", func() pyjson.Value { return Less(9007199254740992.0, int64(9007199254740993)) }, "true"},
		{"10**400 < inf", func() pyjson.Value { return Less(h, inf) }, "true"},
		{"10**400 == inf", func() pyjson.Value { return Equal(h, inf) }, "false"},
		{"-inf < -10**400", func() pyjson.Value { return Less(math.Inf(-1), nh) }, "true"},
		{"3 >= 9007199254740993", func() pyjson.Value { return GreaterEq(int64(3), int64(9007199254740993)) }, "false"},
		{"3 >= 3.0", func() pyjson.Value { return GreaterEq(int64(3), 3.0) }, "true"},
		{"'b' >= 'b'", func() pyjson.Value { return GreaterEq("b", "b") }, "true"},
		{"10**400 + 1.5", func() pyjson.Value { return Add(h, 1.5) }, "E:OverflowError"},
		{"10**400 + 'a'", func() pyjson.Value { return Add(h, "a") }, "E:TypeError"},
		{"1.5 - 10**400", func() pyjson.Value { return Sub(1.5, h) }, "E:OverflowError"},
		{"100.0 * 10**400", func() pyjson.Value { return Mul(100.0, h) }, "E:OverflowError"},
		{"10**400 / 10**399", func() pyjson.Value { return Div(h, big1) }, "10.0"},
		{"10**400 / 2", func() pyjson.Value { return Div(h, int64(2)) }, "E:OverflowError"},
		{"1 / 10**400", func() pyjson.Value { return Div(int64(1), h) }, "0.0"},
		{"1 / -10**400", func() pyjson.Value { return Div(int64(1), nh) }, "-0.0"},
		{"0 / -2", func() pyjson.Value { return Div(int64(0), int64(-2)) }, "-0.0"},
		{"10**400 / 2.0", func() pyjson.Value { return Div(h, 2.0) }, "E:OverflowError"},
		{"10**400 / 0.0", func() pyjson.Value { return Div(h, 0.0) }, "E:OverflowError"},
		{"format(10**400, '.2f')", func() pyjson.Value { return FormatFixed(h, 2) }, "E:OverflowError"},
		{"format(10**400, '.1%')", func() pyjson.Value { return FormatPercent(h, 1) }, "E:OverflowError"},
		{"format(True, '.1%')", func() pyjson.Value { return FormatPercent(true, 1) }, "100.0%"},
		{"sum([10**400, 1.5])", func() pyjson.Value { return Sum([]pyjson.Value{h, 1.5}) }, "E:OverflowError"},
	} {
		v, err := outcome(c.f)
		var got string
		switch x := v.(type) {
		case bool:
			got = map[bool]string{true: "true", false: "false"}[x]
		case float64:
			got = pyjson.FloatRepr(x)
		case string:
			got = x
		}
		var e *Exception
		if errors.As(err, &e) {
			got = "E:" + e.Type
		}
		if got != c.want {
			t.Errorf("%s = %s (本家 %s)", c.name, got, c.want)
		}
	}
}

// 切り出しと + の例外が本家と同じ (Python 3.15.0 の実測。dict の切り出しは 3.12 から slice を鍵にした KeyError)。
func TestSliceAndConcatErrorsMatchPython(t *testing.T) {
	dict := pyjson.NewObject()
	dict.Set("a", int64(1))
	for _, c := range []struct {
		name, typ, msg string
		f              func() pyjson.Value
	}{
		{"{'a': 1}[1:]", "KeyError", "slice(1, None, None)", func() pyjson.Value { return Slice(dict, 1, -1) }},
		{"{'a': 1}[0:3]", "KeyError", "slice(0, 3, None)", func() pyjson.Value { return Slice(dict, 0, 3) }},
		{"{'a': 1}[-50:]", "KeyError", "slice(-50, None, None)", func() pyjson.Value { return Tail(dict, 50) }},
		{"5[-50:]", "TypeError", "'int' object is not subscriptable", func() pyjson.Value { return Tail(int64(5), 50) }},
		{"None[-50:]", "TypeError", "'NoneType' object is not subscriptable", func() pyjson.Value { return Tail(nil, 50) }},
		{"'abc' + 5", "TypeError", `can only concatenate str (not "int") to str`, func() pyjson.Value { return Add("abc", int64(5)) }},
		{"[1] + 'x'", "TypeError", `can only concatenate list (not "str") to list`, func() pyjson.Value { return Add([]pyjson.Value{int64(1)}, "x") }},
		{"None + '.json'", "TypeError", "unsupported operand type(s) for +: 'NoneType' and 'str'", func() pyjson.Value { return Add(nil, ".json") }},
	} {
		_, err := outcome(c.f)
		var e *Exception
		if !errors.As(err, &e) || e.Type != c.typ || e.Msg != c.msg {
			t.Errorf("%s: %v、本家は %s: %s", c.name, err, c.typ, c.msg)
		}
	}
	for _, c := range []struct {
		v    pyjson.Value
		want string
	}{{"abc", "bc"}, {[]pyjson.Value{int64(1), int64(2), int64(3)}, "[2, 3]"}} {
		got, err := outcome(func() pyjson.Value { return Tail(c.v, 2) })
		if text, _ := pyjson.Dumps(got, pyjson.Options{}); err != nil || (text != `"`+c.want+`"` && text != c.want) {
			t.Errorf("Tail(%v, 2) = %v %v、本家は %s", c.v, got, err, c.want)
		}
	}
}
