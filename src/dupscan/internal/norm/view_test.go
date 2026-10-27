package norm

import (
	"strings"
	"testing"

	"dupscan/internal/tree"
)

func TestL1ViewPreservesWriterOutputAndNativeTokenBoundaries(t *testing.T) {
	unit := &tree.Unit{ReturnType: "String", Params: []string{"arg"}, Body: &tree.Node{Kind: "body", Children: []*tree.Node{
		{Kind: "(", Role: tree.RoleToken, Text: "("},
		{Kind: "\"", Role: tree.RoleToken, Text: "\""},
		{Kind: "identifier", Role: tree.RoleName, Text: "arg"},
		{Kind: "literal", Role: tree.RoleLiteral, Text: "猫(\")", LitKind: "str"},
		{Kind: ")", Role: tree.RoleToken, Text: ")"},
	}}}
	view, err := L1View(unit, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var render func(*FormNode) string
	render = func(n *FormNode) string {
		s := n.Header
		for _, c := range n.Children {
			s += render(c)
		}
		return s + ")"
	}
	got := ""
	for _, n := range view {
		got += render(n)
	}
	if want := NormalizeWithForms(unit).Forms[L1]; got != want {
		t.Fatalf("L1 view drifted: got=%q want=%q", got, want)
	}
	if len(view[1].Children) != 5 || view[1].Children[0].Header != `(( "("` || view[1].Children[2].Header != "(identifier $1" {
		t.Fatalf("native/token/local boundaries: %+v", view[1])
	}
	empty := &tree.Unit{Body: &tree.Node{Kind: "empty"}}
	if _, err := L1View(empty, 100); err != nil {
		t.Fatalf("empty body: %v", err)
	}
	oversized := &tree.Unit{Body: &tree.Node{Kind: "literal", Role: tree.RoleLiteral, Text: strings.Repeat("猫", 100)}}
	if _, err := L1View(oversized, 64); err == nil {
		t.Fatal("serialized byte budget ignored despite small node count")
	}
	if _, err := L1View(unit, 10); err == nil {
		t.Fatal("byte budget ignored")
	}
	if _, err := L1View(&tree.Unit{}, 100); err == nil {
		t.Fatal("missing body accepted")
	}
	deep := &tree.Node{Kind: "end"}
	for range 257 {
		deep = &tree.Node{Kind: "node", Children: []*tree.Node{deep}}
	}
	if _, err := L1View(&tree.Unit{Body: deep}, 1<<20); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("depth budget: %v", err)
	}
}

func TestL1ViewKeepsUnitWideLocalNumbering(t *testing.T) {
	makeUnit := func(insert bool) *tree.Unit {
		children := []*tree.Node{}
		if insert {
			children = append(children, &tree.Node{Kind: "decl", Role: tree.RoleDecl, Text: "extra"})
		}
		children = append(children, &tree.Node{Kind: "decl", Role: tree.RoleDecl, Text: "value"}, &tree.Node{Kind: "ref", Role: tree.RoleName, Text: "value"})
		return &tree.Unit{Body: &tree.Node{Kind: "body", Children: children}}
	}
	a, err := L1View(makeUnit(false), 1000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := L1View(makeUnit(true), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if a[1].Children[1].Header != "(ref $1" || b[1].Children[2].Header != "(ref $2" {
		t.Fatalf("numbering drift is hidden: %+v %+v", a, b)
	}
}

func TestL1ViewPreservesParameterDeclarationOrder(t *testing.T) {
	u := &tree.Unit{Params: []string{"left", "right"}, Body: &tree.Node{Kind: "body", Children: []*tree.Node{
		{Kind: "ref", Role: tree.RoleName, Text: "right"},
		{Kind: "ref", Role: tree.RoleName, Text: "left"},
	}}}
	view, err := L1View(u, 1000)
	if err != nil || view[1].Children[0].Header != "(ref $2" || view[1].Children[1].Header != "(ref $1" {
		t.Fatalf("parameter declaration order changed: view=%+v error=%v", view, err)
	}
}
