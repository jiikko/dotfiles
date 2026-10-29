package report

import (
	"strings"
	"testing"
)

// 確かに言えない出どころ (L3 の形・クロージャの順番だけで当てた相手) は名指しを弱める。クロージャは鍵が同じでも「同じ単位」と言わない。
func TestFromNoteWeakensUnsureAndClosureOrigins(t *testing.T) {
	x := UnitRef{Key: "a.go||x()", File: "a.go", Line: 3, Kind: "func", Name: "x", Signature: "x()"}
	n := UnitRef{Key: "b.go||n()", File: "b.go", Line: 5, Kind: "func", Name: "n", Signature: "n()"}
	got := fromNote(n, []OriginJSON{{Key: n.Key, From: x, BodyChanged: true, Sure: false}})
	if got != " (出どころは確かでない: base の a.go:3 x() に形が近い)" {
		t.Errorf("確かでない出どころは名指しを弱める: %q", got)
	}
	c := UnitRef{Key: "a.go|f|{closure}", File: "a.go", Line: 9, Kind: "closure", OwnerName: "f"}
	got = fromNote(c, []OriginJSON{{Key: c.Key, From: c, BodyChanged: true, Sure: true}})
	if strings.Contains(got, "同じ単位") || !strings.Contains(got, "から。本体を変えた") {
		t.Errorf("クロージャは鍵が同じでも同じ単位と言わない: %q", got)
	}
	if got := fromNote(n, nil); got != "" {
		t.Errorf("出どころの無い単位には付けない: %q", got)
	}
}
