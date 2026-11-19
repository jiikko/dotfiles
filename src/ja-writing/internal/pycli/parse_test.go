package pycli

import "testing"

// 期待値は Python 3.15.0 の `piece.strip().lower().replace("_", "-")` の出力をそのまま写した。
// 語末の Σ (前に cased の字があり、後ろに cased の字が続かない) と、Unicode 16 で足された字の小文字化を含める。
func TestFailOnWordMatchesPythonLower(t *testing.T) {
	cases := []struct{ in, want string }{
		{" ΑΣ ", "ας"},
		{"ΑΣ_Β", "ας-β"},
		{"İ", "i̇"},
		{"ꞈᲉ", "ꞈᲊ"},
		{"WARN", "warn"},
		{"Error_", "error-"},
	}
	for _, c := range cases {
		if got := FailOnWord(c.in); got != c.want {
			t.Errorf("FailOnWord(%q) = %q、Python は %q", c.in, got, c.want)
		}
	}
}
