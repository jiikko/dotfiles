package pyunicode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"ja-writing/oracle"
)

func loadCasemap(t *testing.T) oracle.CasemapGolden {
	t.Helper()
	var g oracle.CasemapGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CasemapFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	return g
}

// 全コードポイントで、Go の変換が Python の結果 (記録の digest と、元と違う字の一覧) と同じ。
func TestCaseMapsMatchPythonForEveryCodePoint(t *testing.T) {
	g := loadCasemap(t)
	for _, c := range []struct {
		name string
		fn   func(string) string
		rows []oracle.CaseRow
	}{{"casefold", Casefold, g.Casefold}, {"lower", Lower, g.Lower}, {"swapcase", Swapcase, g.Swapcase}} {
		if len(c.rows) < 1000 {
			t.Fatalf("%s の行が %d 行しかない (記録が空振り)", c.name, len(c.rows))
		}
		changed := map[rune]string{}
		for _, row := range c.rows {
			changed[row.CP] = string(row.To)
		}
		var lines strings.Builder
		for r := rune(0); r <= MaxRune; r++ {
			if isSurrogate(r) {
				continue
			}
			got := c.fn(string(r))
			want, ok := changed[r]
			if !ok {
				want = string(r)
			}
			if got != want {
				t.Fatalf("%s(U+%04X) = %q、Python は %q", c.name, r, got, want)
			}
			fmt.Fprintf(&lines, "%x:", r)
			for i, x := range got {
				if i > 0 {
					lines.WriteString(" ")
				}
				fmt.Fprintf(&lines, "%x", x)
			}
			lines.WriteString("\n")
		}
		sum := sha256.Sum256([]byte(lines.String()))
		if got := hex.EncodeToString(sum[:]); got != g.Digests[c.name] {
			t.Fatalf("%s の全コードポイントの digest %s、記録 %s", c.name, got, g.Digests[c.name])
		}
	}
}

func TestCaseMapsMatchPythonOnStrings(t *testing.T) {
	g := loadCasemap(t)
	var withSigma int
	for _, c := range g.Cases {
		s := FromRunes(c.S)
		if got, want := Casefold(s), FromRunes(c.Casefold); got != want {
			t.Errorf("Casefold(%q) = %q、Python は %q", s, got, want)
		}
		if got, want := Swapcase(s), FromRunes(c.Swapcase); got != want {
			t.Errorf("Swapcase(%q) = %q、Python は %q", s, got, want)
		}
		if got, want := Lower(s), FromRunes(c.Lower); got != want {
			t.Errorf("Lower(%q) = %q、Python は %q", s, got, want)
		}
		if strings.ContainsRune(s, capitalSigma) {
			withSigma++
		}
	}
	// 複数字への展開 (ß→ss) と、Unicode 15 の表にない折りたたみを持つこと
	for in, want := range map[string]string{"ß": "ss", "ẞ": "ss", "ﬃ": "ffi", "İ": "i̇", "ς": "σ"} {
		if got := Casefold(in); got != want {
			t.Errorf("Casefold(%q) = %q、期待 %q", in, got, want)
		}
	}
	if withSigma < 15 {
		t.Fatalf("語末の Σ の規則を試す文字列が %d 件 (記録が空振り)", withSigma)
	}
	// 語末の Σ だけが ς になり、語中・語頭・読み飛ばす字を挟んだ語末の判定が Python と同じ向き
	for in, want := range map[string]string{"ΑΣ": "ας", "ΑΣΑ": "ασα", "Σ": "σ", "Α.Σ": "α.ς", "ΑΣ.Α": "ασ.α"} {
		if got := Lower(in); got != want {
			t.Errorf("Lower(%q) = %q、期待 %q", in, got, want)
		}
	}
}
