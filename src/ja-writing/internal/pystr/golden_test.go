package pystr

import (
	"encoding/json"
	"slices"
	"testing"

	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
)

const goldenDir = "../../testdata/golden"

// decode は記録の文字列 (JSON の文字列か、サロゲートを含むときの {"ws": [cp...]}) を WTF-8 の文字列にする。
func decode(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var ws struct {
		WS []rune `json:"ws"`
	}
	if err := json.Unmarshal(raw, &ws); err != nil || ws.WS == nil {
		t.Fatalf("記録の文字列を読めません: %s", raw)
	}
	return pyunicode.FromRunes(ws.WS)
}

func decodeAll(t *testing.T, raws []json.RawMessage) []string {
	out := make([]string, len(raws))
	for i, r := range raws {
		out[i] = decode(t, r)
	}
	return out
}

func TestMatchesPythonStr(t *testing.T) {
	var g oracle.PystrGolden
	if err := oracle.ReadGolden(goldenDir, oracle.PystrFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	if len(g.Cases) < 1000 {
		t.Fatalf("記録が %d 件しかない", len(g.Cases))
	}
	var withChars, withSurrogate, multiLine int
	for _, c := range g.Cases {
		s := decode(t, c.S)
		q := func(name string) string { return name + "(" + string(c.S) + ")" }
		if got := Len(s); got != c.Len {
			t.Fatalf("%s = %d、Python は %d", q("Len"), got, c.Len)
		}
		for _, p := range []struct {
			name      string
			got, want bool
		}{
			{"IsSpace", IsSpace(s), c.IsSpace}, {"IsAlpha", IsAlpha(s), c.IsAlpha}, {"IsAlnum", IsAlnum(s), c.IsAlnum},
			{"IsDecimal", IsDecimal(s), c.IsDecimal}, {"IsDigit", IsDigit(s), c.IsDigit}, {"IsNumeric", IsNumeric(s), c.IsNumeric},
		} {
			if p.got != p.want {
				t.Fatalf("%s = %v、Python は %v", q(p.name), p.got, p.want)
			}
		}
		for _, p := range []struct {
			name      string
			got, want string
		}{{"Strip", Strip(s), decode(t, c.Strip)}, {"LStrip", LStrip(s), decode(t, c.LStrip)}, {"RStrip", RStrip(s), decode(t, c.RStrip)}} {
			if p.got != p.want {
				t.Fatalf("%s = %q、Python は %q", q(p.name), p.got, p.want)
			}
		}
		if got, want := SplitLines(s, false), decodeAll(t, c.Lines); !slices.Equal(got, want) {
			t.Fatalf("%s = %q、Python は %q", q("SplitLines"), got, want)
		}
		if got, want := SplitLines(s, true), decodeAll(t, c.LinesKeep); !slices.Equal(got, want) {
			t.Fatalf("%s = %q、Python は %q", q("SplitLines(keepends)"), got, want)
		}
		if len(c.Lines) > 1 {
			multiLine++
		}
		if len(pyunicode.Runes(s)) != c.Len {
			t.Fatalf("記録の len %d と文字列の字数 %d が違う", c.Len, len(pyunicode.Runes(s)))
		}
		for _, r := range pyunicode.Runes(s) {
			if r >= 0xD800 && r <= 0xDFFF {
				withSurrogate++
				break
			}
		}
		if c.Chars != nil {
			withChars++
			chars := decode(t, c.Chars)
			for _, p := range []struct {
				name      string
				got, want string
			}{
				{"StripChars", StripChars(s, chars), decode(t, c.StripChars)},
				{"LStripChars", LStripChars(s, chars), decode(t, c.LStripChars)},
				{"RStripChars", RStripChars(s, chars), decode(t, c.RStripChars)},
			} {
				if p.got != p.want {
					t.Fatalf("%s(%q) = %q、Python は %q", q(p.name), chars, p.got, p.want)
				}
			}
		}
	}
	// 記録が空振り (全部が 1 行・chars なし・サロゲートなし) でないこと
	if withChars < 500 || withSurrogate < 20 || multiLine < 100 {
		t.Fatalf("記録の偏り: chars つき %d・サロゲートを含む %d・複数行 %d", withChars, withSurrogate, multiLine)
	}
}

func TestSplitLinesSeparators(t *testing.T) {
	// \x1f は isspace だが行の区切りではなく、\x1c〜\x1e は区切り (Python の str.splitlines の定義)。
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"a\x1fb", []string{"a\x1fb"}}, {"a\x1cb", []string{"a", "b"}}, {"a\r\nb", []string{"a", "b"}}, {"a\n\rb", []string{"a", "", "b"}},
		{"", nil}, {"\n", []string{""}}, {"a\n", []string{"a"}}, {"a\u2028b\u2029c\u0085d", []string{"a", "b", "c", "d"}},
	} {
		if got := SplitLines(c.in, false); !slices.Equal(got, c.want) {
			t.Errorf("SplitLines(%q) = %q、期待 %q", c.in, got, c.want)
		}
	}
}
