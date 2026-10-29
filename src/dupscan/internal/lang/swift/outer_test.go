package swift

import (
	"testing"

	"dupscan/internal/tree"
)

// `if let a, let b = g(a)`: a は shorthand で `=` を持たず、その場で効く。後ろの `=` は b の束縛のもので、
// a を右辺の後まで遅らせない (遅らせると g(a) の a を外側の名前と読む)。`,` で探すのをやめるのはこのため。
func TestMarkOuterStopsAssignSearchAtComma(t *testing.T) {
	ref := &tree.Node{Role: tree.RoleName, Text: "a"}
	body := &tree.Node{Kind: "if_statement", Children: []*tree.Node{
		{Role: tree.RoleDecl, Text: "a"},
		{Role: tree.RoleToken, Text: ","},
		{Role: tree.RoleDecl, Text: "b"},
		{Role: tree.RoleToken, Text: "="},
		{Kind: "call_expression", Children: []*tree.Node{{Role: tree.RoleName, Text: "g"}, ref}},
	}}
	markOuter(body, nil)
	if ref.Outer {
		t.Errorf("g(a) の a は shorthand で効いた局所の名前で、外側の名前ではない")
	}
}

// 局所の型の init の引数は、同じ型の本体の後ろのメソッドへ届かない (init を出たら外す)。
func TestMarkOuterInitParamDoesNotReachSiblingMethod(t *testing.T) {
	ref := &tree.Node{Role: tree.RoleName, Text: "min"}
	body := &tree.Node{Kind: "class_body", Children: []*tree.Node{
		{Kind: "init_declaration", Children: []*tree.Node{{Kind: "parameter", Children: []*tree.Node{{Role: tree.RoleDecl, Text: "min"}}}}},
		{Kind: "function_declaration", Children: []*tree.Node{{Kind: "statements", Children: []*tree.Node{ref}}}},
	}}
	markOuter(body, nil)
	if !ref.Outer {
		t.Errorf("後ろのメソッドの min は init の引数ではなく外側の名前")
	}
}
