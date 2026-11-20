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
