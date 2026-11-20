package pycli

import (
	"testing"

	"ja-writing/internal/pyfs"
)

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

// 近い候補は Python 3.15.0 の difflib.get_close_matches(値, choices, 1) の実測と同じもの (無ければ ""。同じ比なら字の順で後のもの)。
func TestCloseMatchMatchesPython(t *testing.T) {
	dcff := string(pyfs.AppendSurrogate(nil, 0xDCFF))
	host := []string{"claude-code", "codex"}
	cases := []struct {
		word    string
		choices []string
		want    string
	}{
		{"Codex", host, "codex"}, {"codex ", host, "codex"}, {"claude", host, "claude-code"}, {"claud-code", host, "claude-code"},
		{"x", host, ""}, {"", host, ""}, {"codexx", host, "codex"}, {"CLAUDE-CODE", host, ""}, {"code", host, "codex"}, {"cod", host, "codex"},
		{"odex", host, "codex"}, {"clau", host, ""}, {dcff, host, ""}, {"c" + dcff + "odex", host, "codex"}, {"ｃｏｄｅｘ", host, ""},
		{"de", host, ""}, {"claude-codex", host, "claude-code"},
		{"abc", []string{"abe", "abd"}, "abe"}, {"abc", []string{"abd", "abe"}, "abe"},
	}
	for _, c := range cases {
		got, ok := closeMatch(c.word, c.choices)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("closeMatch(%q, %q) = %q, %v、Python は %q", c.word, c.choices, got, ok, c.want)
		}
	}
}

// choices に無い値の誤りは argparse の文 (近い候補があれば maybe you meant)。Python 3.15.0 の argparse の実測。
func TestCheckChoiceMatchesArgparse(t *testing.T) {
	host := []string{"claude-code", "codex"}
	if err := CheckChoice("--host", "codex", host); err != nil {
		t.Fatalf("choices の値を断った: %v", err)
	}
	for value, want := range map[string]string{
		"Codex": "argument --host: invalid choice: 'Codex', maybe you meant 'codex'? (choose from 'claude-code', 'codex')",
		"x":     "argument --host: invalid choice: 'x' (choose from 'claude-code', 'codex')",
	} {
		if err := CheckChoice("--host", value, host); err == nil || err.Error() != want {
			t.Errorf("CheckChoice(%q) = %v、argparse は %q", value, err, want)
		}
	}
}
