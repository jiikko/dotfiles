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
