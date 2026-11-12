package pyre

import "testing"

// regexp2 の誤りで本家と違う答えを返しうる形 (README の「揃わない範囲」)。パターンの文字列ではなく、pyre の構文木で数える
// (書き方 `a*+` `(?>a)` `￿` `\U0000ffff` `[￿]` のどれでも同じ節点になる)。
// 記録の本家のパターンにこの形が入ったら、ずれが起きる形かを調べ直す。
func unreliableShapes(pattern string, flags int) ([]string, error) {
	res, err := parsePattern([]rune(pattern), flags)
	if err != nil {
		return nil, err
	}
	var found []string
	visit(res.body, func(n *node) {
		switch n.op {
		case opAtomic:
			found = append(found, "atomic")
		case opRepeat:
			if n.rkind == repeatPossessive {
				found = append(found, "possessive")
			}
		case opLiteral, opNotLiteral:
			if n.r == 0xFFFF {
				found = append(found, "U+FFFF")
			}
		case opIn:
			for _, it := range n.items {
				if it.kind != itemCategory && (it.lo == 0xFFFF || it.hi == 0xFFFF) {
					found = append(found, "U+FFFF")
				}
			}
		case opAny, opAt, opSubpattern, opBranch, opAssert, opAssertNot, opFailure, opGroupRef, opGroupRefExists:
		}
	})
	return found, nil
}

func TestUnreliableShapesFindsEachWritingOfTheShape(t *testing.T) {
	for _, tc := range []struct{ pattern, want string }{
		{`(?:a?+)?a`, "possessive"},
		{`a*+`, "possessive"},
		{`a{1,2}+b`, "possessive"},
		{`(?>a?)?a`, "atomic"},
		{`x(?:(?>ab))`, "atomic"},
		{"￿a", "U+FFFF"},
		{`￿a`, "U+FFFF"},
		{`\U0000ffffa`, "U+FFFF"},
		{`[￿]a`, "U+FFFF"},
		{`[\u0000-￿]a`, "U+FFFF"},
		{`[^￿]a`, "U+FFFF"},
	} {
		got, err := unreliableShapes(tc.pattern, 0)
		if err != nil || len(got) == 0 || got[0] != tc.want {
			t.Errorf("%q: %v %v、期待 %s", tc.pattern, got, err, tc.want)
		}
	}
	for _, pattern := range []string{`(?:a?)?a`, `a*?b`, `(?:ab)+`, `[a-z]\w`, `￾[퟿]`, `(?=a)b`, `[^a]`} {
		if got, err := unreliableShapes(pattern, 0); err != nil || len(got) != 0 {
			t.Errorf("%q を誤って数えた: %v %v", pattern, got, err)
		}
	}
}
