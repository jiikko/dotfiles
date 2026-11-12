package pyre

import (
	"slices"
	"testing"

	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
)

func allRunes() []rune {
	rs := make([]rune, maxRune+1)
	for i := range rs {
		rs[i] = rune(i)
	}
	return rs
}

// findRanges は pattern (で字が 1 字以上続く部分) が全コードポイントの文字列のどこに当たるかを、本物の engine で調べて範囲にする。
func findRanges(t *testing.T, pattern string, flags int, text Text) rset {
	t.Helper()
	p, err := Compile(pattern, flags)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := p.Finditer(text, 0, NoEnd)
	if err != nil {
		t.Fatal(err)
	}
	var out rset
	for _, m := range ms {
		out = append(out, runeRange{rune(m.spans[0]), rune(m.spans[1] - 1)})
	}
	return out
}

func TestCategorySetsMatchPythonForEveryCodePoint(t *testing.T) {
	g := loadPyre(t).Chars
	text := Text{allRunes()}
	for _, c := range []struct {
		name    string
		pattern string
		flags   int
		cat     category
		ascii   bool
		want    []oracle.CPRange
	}{
		{"word", `\w+`, 0, catWord, false, g.Word}, {"space", `\s+`, 0, catSpace, false, g.Space}, {"digit", `\d+`, 0, catDigit, false, g.Digit},
		{"word_ascii", `\w+`, ASCII, catWord, true, g.WordASCII}, {"space_ascii", `\s+`, ASCII, catSpace, true, g.SpaceASCII},
		{"digit_ascii", `\d+`, ASCII, catDigit, true, g.DigitASCII},
	} {
		want := fromRanges(c.want)
		if len(want) == 0 {
			t.Fatalf("%s: 記録が空", c.name)
		}
		if got := categorySet(c.cat, c.ascii); !slices.Equal(got, want) {
			t.Errorf("%s: 表から作った集合が Python と違う (%d 範囲と %d 範囲)", c.name, len(got), len(want))
		}
		if got := findRanges(t, c.pattern, c.flags, text); !slices.Equal(got, want) {
			t.Errorf("%s: %s を全コードポイントに当てた結果が Python と違う (%d 範囲と %d 範囲)", c.name, c.pattern, len(got), len(want))
		}
	}
}

func TestWordBoundaryMatchesPythonForEveryCodePoint(t *testing.T) {
	g := loadPyre(t).Chars
	text := Text{allRunes()}
	for _, c := range []struct {
		flags int
		want  []int
	}{{0, g.Boundary}, {ASCII, g.BoundaryASCII}} {
		p := MustCompile(`\b`, c.flags)
		ms, err := p.Finditer(text, 0, NoEnd)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]int, len(ms))
		for i, m := range ms {
			got[i] = m.spans[0]
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("flags=%d: \\b の位置が Python と違う (%d 個と %d 個)", c.flags, len(got), len(c.want))
		}
		if len(got) < 4 {
			t.Errorf("flags=%d: \\b が 4 か所も当たらない。記録が空振りしている", c.flags)
		}
		// \B は、空でない文字列では \b の補集合 (全位置から \b の位置を引いたもの)
		nb, err := MustCompile(`\B`, c.flags).Finditer(Text{text.runes[:20000]}, 0, NoEnd)
		if err != nil {
			t.Fatal(err)
		}
		inB := map[int]bool{}
		for _, pos := range c.want {
			if pos < 20000 {
				inB[pos] = true
			}
		}
		nbBefore := 0
		for _, m := range nb {
			if m.spans[0] < 20000 {
				nbBefore++
			}
		}
		if nbBefore+len(inB) != 20000 {
			t.Errorf("flags=%d: \\B (%d) と \\b (%d) が 20001 の位置を分けていない", c.flags, len(nb), len(inB))
		}
	}
}

func candidateText(g oracle.PyreChars) Text {
	return Text{slices.Clone(g.Candidates)}
}

func hitsOf(t *testing.T, p *Pattern, text Text) []rune {
	t.Helper()
	ms, err := p.Finditer(text, 0, NoEnd)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]rune, 0, len(ms))
	for _, m := range ms {
		out = append(out, text.runes[m.spans[0]])
	}
	return out
}

func TestIgnoreCaseSingleCharactersMatchPython(t *testing.T) {
	g := loadPyre(t).Chars
	text := candidateText(g)
	cased := 0
	for _, entry := range g.IgnoreCase {
		pattern := Escape(pyunicode.FromRunes([]rune{entry.CP}))
		p, err := Compile(pattern, IGNORECASE)
		if err != nil {
			t.Fatalf("U+%04X: %v", entry.CP, err)
		}
		got := hitsOf(t, p, text)
		if !slices.Equal(got, entry.Hits) {
			t.Fatalf("U+%04X を re.I で当てた字が Python と違う\n  Go     %U\n  Python %U", entry.CP, got, entry.Hits)
		}
		if len(entry.Hits) > 1 {
			cased++
		}
		// 表から作った集合も、候補の上では同じになる
		set := caseRules{}.literalSet(entry.CP)
		var fromSet []rune
		for _, x := range g.Candidates {
			if set.contains(x) {
				fromSet = append(fromSet, x)
			}
		}
		if !slices.Equal(fromSet, entry.Hits) {
			t.Fatalf("U+%04X の集合が Python と違う", entry.CP)
		}
	}
	if len(g.IgnoreCase) < 3000 || cased < 2000 {
		t.Fatalf("記録の字が少ない (%d 字、うち大小のある字 %d)", len(g.IgnoreCase), cased)
	}
}

func TestIgnoreCaseClassesMatchPython(t *testing.T) {
	g := loadPyre(t).Chars
	text := candidateText(g)
	if len(g.Classes) < 60 {
		t.Fatalf("文字の集合の記録が %d 件しかない", len(g.Classes))
	}
	for _, c := range g.Classes {
		p, err := Compile(c.Src, c.Flags)
		if err != nil {
			t.Fatalf("%q: %v", c.Src, err)
		}
		if got := hitsOf(t, p, text); !slices.Equal(got, c.Hits) {
			t.Errorf("%q flags=%d の当たりが Python と違う (%d 字と %d 字)\n  Go のみ %U\n  Python のみ %U", c.Src, c.Flags, len(got), len(c.Hits), subtract(got, c.Hits), subtract(c.Hits, got))
		}
	}
}

func subtract(a, b []rune) []rune {
	var out []rune
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}
