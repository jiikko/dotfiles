package pystr

import (
	"strings"
	"testing"
)

// ParseInt が Python 3.15.0 の int(s) と同じ値を返し、同じ入力を断る (期待値は python3.15 で int(s) を呼んだ結果。断ったものは "")。
func TestParseIntMatchesPython(t *testing.T) {
	cases := []struct{ in, want string }{
		{" 3 ", "3"}, {"+3", "3"}, {"-0", "0"}, {"\uff13", "3"}, {"1_000", "1000"}, {"_1", ""}, {"1_", ""}, {"1__0", ""},
		{"0_0", "0"}, {"007", "7"}, {"", ""}, {"  ", ""}, {"+", ""}, {"- 1", ""}, {"1 2", ""}, {"\u0663", "3"},
		{"\u30001\u3000", "1"}, {"\t5\n", "5"}, {"x", ""}, {"1.0", ""}, {"+_1", ""}, {"0x10", ""}, {"\uff11_\uff12", "12"},
		{"\u20281", "1"}, {"\u00851", "1"}, {"-12_3", "-123"}, {"00", "0"}, {"1\u0300", ""},
		{strings.Repeat("0", 4301), ""}, {"1" + strings.Repeat("_1", 4299), "1" + strings.Repeat("1", 4299)}, {"1" + strings.Repeat("_1", 4300), ""},
	}
	for _, c := range cases {
		n, ok := ParseInt(c.in)
		got := ""
		if ok {
			got = n.String()
		}
		if got != c.want {
			t.Errorf("ParseInt(%q) = %q (ok=%v)、Python は %q", c.in, got, ok, c.want)
		}
	}
}
