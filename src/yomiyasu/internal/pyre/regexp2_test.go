package pyre

import "testing"

// regexp2 は、先頭が (?:\A|x) の選択肢のあとに「1 字以外」の集合が来ると後戻りを間違える。補集合の集合を 2 つのクラスに割る emitSet の迂回が効いていること。
func TestZeroWidthAlternativeBeforeComplementSet(t *testing.T) {
	for _, c := range []struct {
		pattern, text string
		span          [2]int
	}{
		{`(?:^|;)[^a]`, ";ab", [2]int{0, 1}},
		{`(?:^|;)[^,]b`, ";ab", [2]int{0, 3}},
		{`(?:^|、)([^、]+?い)、`, "でも、これは努力じゃない、仕組み", [2]int{2, 13}},
		{`(?:\Z|;).`, ";ab", [2]int{0, 2}},
	} {
		m, err := MustCompile(c.pattern, 0).Search(NewText(c.text), 0, NoEnd)
		if err != nil || m == nil {
			t.Fatalf("%q が %q に当たらない (%v)", c.pattern, c.text, err)
		}
		if s, e := m.Span(0); [2]int{s, e} != c.span {
			t.Errorf("%q: span %d,%d 期待 %v", c.pattern, s, e, c.span)
		}
	}
}
