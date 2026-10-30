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

// Shared は対称で、どちらを歩いても同じ。大きさが違う bag でも、共通のキーの min の和になる。
func TestSubtreeSharedIsSymmetricMinSum(t *testing.T) {
	k1, k2, k3 := [32]byte{1}, [32]byte{2}, [32]byte{3}
	small := SubtreeBag{k1: 4, k2: 1}
	large := SubtreeBag{k1: 2, k2: 5, k3: 9, [32]byte{4}: 7, [32]byte{5}: 7}
	want := 2 + 1 // min(4,2) + min(1,5)
	if got := small.Shared(large); got != want {
		t.Errorf("small.Shared(large) = %d, want %d", got, want)
	}
	if got := large.Shared(small); got != want {
		t.Errorf("large.Shared(small) = %d, want %d (対称)", got, want)
	}
	if got := small.Shared(SubtreeBag{}); got != 0 {
		t.Errorf("空の bag との共有 = %d, want 0", got)
	}
	if got := small.Total(); got != 5 {
		t.Errorf("Total = %d, want 5", got)
	}
	// Dice は Shared と Total から組み立てた値と一致する
	score, shared := small.Dice(large)
	if shared != want || score != 2*float64(want)/float64(small.Total()+large.Total()) {
		t.Errorf("Dice = %v %v", score, shared)
	}
}

// 写しの途中に局所の名前の宣言を 1 文足しても、後ろの文の部分木は一致したまま (番号を単位全体の出た順で振ると、後ろの名前の番号がずれて一致しなくなっていた)。
func TestSubtreesIgnoreInsertedLocalDeclaration(t *testing.T) {
	ident := func(role tree.Role, s string) *tree.Node { return &tree.Node{Kind: "identifier", Role: role, Text: s} }
	stmts := func(extra bool) *tree.Unit {
		var body []*tree.Node
		if extra {
			body = append(body, &tree.Node{Kind: "define", Children: []*tree.Node{ident(tree.RoleDecl, "group"), {Kind: "literal", Role: tree.RoleLiteral, Text: "0", LitKind: "int"}}})
		}
		body = append(body,
			&tree.Node{Kind: "define", Children: []*tree.Node{ident(tree.RoleDecl, "x"), {Kind: "call", Action: true, Children: []*tree.Node{ident(tree.RoleName, "load"), ident(tree.RoleName, "p")}}}},
			&tree.Node{Kind: "call", Action: true, Children: []*tree.Node{ident(tree.RoleName, "show"), ident(tree.RoleName, "x"), ident(tree.RoleName, "p")}},
		)
		return &tree.Unit{Params: []string{"p"}, Body: &tree.Node{Kind: "block", Children: body}}
	}
	plain := Subtrees(stmts(false), 3)
	shared := plain.Shared(Subtrees(stmts(true), 3))
	root := 0 // 根 (本体の block) は文の数が違うので一致しない。それ以外の部分木は全部一致する
	for _, w := range plain {
		root = max(root, w)
	}
	if want := plain.Total() - root; shared != want {
		t.Errorf("宣言を 1 文足した写しと共有する重み %d, want %d (後ろの文の部分木が一致しない)", shared, want)
	}
}
