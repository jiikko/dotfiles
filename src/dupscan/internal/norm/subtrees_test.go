package norm

import (
	"dupscan/internal/tree"
	"testing"
)

func TestSubtreesRetainL1ValuesAndLocalNumbering(t *testing.T) {
	unit := func(param, member, literal string) *tree.Unit {
		return &tree.Unit{Params: []string{param}, Body: &tree.Node{Kind: "block", Children: []*tree.Node{
			{Kind: "call", Action: true, Children: []*tree.Node{
				{Kind: "identifier", Role: tree.RoleName, Text: param},
				{Kind: "identifier", Role: tree.RoleMember, Text: member},
				{Kind: "literal", Role: tree.RoleLiteral, Text: literal, LitKind: "string"},
			}},
		}}}
	}
	a := Subtrees(unit("a", "run", "\"a(b)\""), 3)
	same, weight := a.Dice(Subtrees(unit("renamed", "run", "\"a(b)\""), 3))
	if same != 1 || weight == 0 {
		t.Fatalf("renaming lost overlap: %v %v", same, weight)
	}
	for _, u := range []*tree.Unit{unit("a", "delete", "\"a(b)\""), unit("a", "run", "\"other\"")} {
		if score, _ := a.Dice(Subtrees(u, 3)); score != 0 {
			t.Fatalf("member/literal was erased: %v", score)
		}
	}
	if got := Subtrees(unit("a", "run", "x"), 6); len(got) != 0 {
		t.Fatalf("min size ignored: %v", got)
	}
}

func TestSubtreeDiceCountsMultiplicityAndEmpty(t *testing.T) {
	key := [32]byte{1}
	a := SubtreeBag{key: 6}
	b := SubtreeBag{key: 3}
	if score, weight := a.Dice(b); score != 2.0/3 || weight != 3 {
		t.Fatalf("multiset overlap: %v %v", score, weight)
	}
	if score, weight := (SubtreeBag{}).Dice(SubtreeBag{}); score != 0 || weight != 0 {
		t.Fatalf("empty: %v %v", score, weight)
	}
}
