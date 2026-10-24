// Package swift は、Swift のソースを tree-sitter で読み、比べる単位 (関数・初期化子・クロージャ) を言語に依らない木に写す adapter。
package swift

import (
	"fmt"
	"path"
	"strings"

	swiftgrammar "github.com/alex-pinkus/tree-sitter-swift/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"dupscan/internal/tree"
)

// Extensions は adapter が読むファイルの拡張子。
var Extensions = []string{".swift"}

// Language は文法を返す。
func Language() *sitter.Language { return sitter.NewLanguage(swiftgrammar.Language()) }

// Parser はファイルを読んで単位を取り出す。goroutine ごとに 1 つ作る (tree-sitter の parser は共有できない)。
type Parser struct{ p *sitter.Parser }

// NewParser は Parser を作る。
func NewParser() (*Parser, error) {
	p := sitter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		p.Close()
		return nil, err
	}
	return &Parser{p: p}, nil
}

// Close は parser を閉じる。
func (p *Parser) Close() { p.p.Close() }

// FileResult は 1 ファイルの結果。
type FileResult struct {
	Units []*tree.Unit
	// HasError はファイルのどこかに読めない構文 (ERROR / MISSING) があること。
	HasError bool
}

// Parse は file (repo の root からの相対) の src から単位を取り出す。テスト関数そのものとその中のクロージャは単位にしない。
func (p *Parser) Parse(file string, src []byte) (FileResult, error) {
	t := p.p.Parse(src, nil)
	if t == nil {
		return FileResult{}, fmt.Errorf("%s を parse できない", file)
	}
	defer t.Close()
	root := t.RootNode()
	w := &walker{file: file, src: src, test: IsTestPath(file), closures: map[string]int{}, ownerIsUnit: map[string]bool{}}
	w.walk(root, nil, "")
	return FileResult{Units: w.units, HasError: root.HasError()}, nil
}

// IsTestPath は Swift の慣習で test のファイルかを決める (`Tests` / `…Tests` / `…UITests` のディレクトリの下、`…Tests.swift`)。
// repo ごとの上書きは設定ファイルで行う (コードに特定の repo の規則を入れない)。
func IsTestPath(file string) bool {
	if strings.HasSuffix(path.Base(file), "Tests.swift") {
		return true
	}
	for _, c := range strings.Split(path.Dir(file), "/") {
		if c == "Tests" || strings.HasSuffix(c, "Tests") {
			return true
		}
	}
	return false
}

type walker struct {
	file        string
	src         []byte
	test        bool
	units       []*tree.Unit
	closures    map[string]int
	ownerIsUnit map[string]bool
}

func (w *walker) text(n *sitter.Node) string { return n.Utf8Text(w.src) }

// walk は enclosing (囲む型の名前の並び) と owner (囲む単位の鍵。クロージャの鍵に使う) を持って木を辿る。
func (w *walker) walk(n *sitter.Node, enclosing []string, owner string) {
	switch n.Kind() {
	case "class_declaration", "protocol_declaration":
		if name := w.typeName(n); name != "" {
			enclosing = append(append([]string(nil), enclosing...), name)
		}
	case "function_declaration", "init_declaration":
		if w.isTestFunc(n, enclosing) {
			return
		}
		u := w.funcUnit(n, enclosing)
		w.units = append(w.units, u)
		owner = u.Key
		w.ownerIsUnit[owner] = true
	case "lambda_literal":
		u := w.closureUnit(n, enclosing, owner)
		w.units = append(w.units, u)
		owner = u.Key
		w.ownerIsUnit[owner] = true
	}
	for i := range n.ChildCount() {
		w.walk(n.Child(i), enclosing, owner)
	}
}

func (w *walker) typeName(n *sitter.Node) string {
	if c := n.ChildByFieldName("name"); c != nil {
		return w.text(c)
	}
	return ""
}

func (w *walker) modifiers(n *sitter.Node) *sitter.Node {
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); c.Kind() == "modifiers" {
			return c
		}
	}
	return nil
}

// isTestFunc は Swift Testing の `@Test` か、XCTest の `func test…()` (引数なし・型の中) か。
func (w *walker) isTestFunc(n *sitter.Node, enclosing []string) bool {
	if n.Kind() != "function_declaration" {
		return false
	}
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			a := m.NamedChild(i)
			if a.Kind() != "attribute" {
				continue
			}
			for j := range a.NamedChildCount() {
				if c := a.NamedChild(j); c.Kind() == "user_type" && w.text(c) == "Test" {
					return true
				}
			}
		}
	}
	name := ""
	if c := n.ChildByFieldName("name"); c != nil {
		name = w.text(c)
	}
	return w.test && len(enclosing) > 0 && strings.HasPrefix(name, "test") && len(params(n)) == 0
}

func params(n *sitter.Node) []*sitter.Node {
	var ps []*sitter.Node
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); c.Kind() == "parameter" {
			ps = append(ps, c)
		}
	}
	return ps
}

func (w *walker) funcUnit(n *sitter.Node, enclosing []string) *tree.Unit {
	name := "init"
	kind := tree.KindInit
	if n.Kind() == "function_declaration" {
		kind = tree.KindFunc
		if c := n.ChildByFieldName("name"); c != nil {
			name = w.text(c)
		}
	}
	var sig []string
	var locals []string
	for _, p := range params(n) {
		sig = append(sig, collapse(w.text(p)))
		if in := internalName(p, w.src); in != "" {
			locals = append(locals, in)
		}
	}
	signature := name + "(" + strings.Join(sig, ", ") + ")"
	if r := returnType(n, w.src); r != "" {
		signature += " -> " + r
	}
	encl := strings.Join(enclosing, ".")
	u := w.newUnit(n, kind, name, signature, encl)
	u.Key = w.file + "|" + encl + "|" + signature
	u.Params = locals
	u.Visibility = w.visibility(n)
	if b := n.ChildByFieldName("body"); b != nil {
		u.Body = w.convert(b)
	} else {
		u.Body = &tree.Node{Kind: "empty"}
	}
	return u
}

func (w *walker) closureUnit(n *sitter.Node, enclosing []string, owner string) *tree.Unit {
	if owner == "" {
		owner = w.file + "|" + strings.Join(enclosing, ".")
	}
	w.closures[owner]++
	u := w.newUnit(n, tree.KindClosure, "{closure}", "", strings.Join(enclosing, "."))
	u.Key = fmt.Sprintf("%s#closure%d", owner, w.closures[owner])
	if w.ownerIsUnit[owner] {
		u.Owner = owner
	}
	u.Visibility = "local"
	u.Body = w.convert(n)
	return u
}

func (w *walker) newUnit(n *sitter.Node, kind tree.UnitKind, name, sig, encl string) *tree.Unit {
	pos := n.StartPosition()
	return &tree.Unit{
		File: w.file, Line: int(pos.Row) + 1, Col: int(pos.Column) + 1,
		Name: name, Signature: sig, Kind: kind, Enclosing: encl, Test: w.test,
		Unreadable: n.HasError(),
	}
}

func (w *walker) visibility(n *sitter.Node) string {
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			if c := m.NamedChild(i); c.Kind() == "visibility_modifier" {
				return w.text(c)
			}
		}
	}
	return "internal"
}

// internalName は引数の本体の中で使う名前 (`_ p: String` なら p、`path: String` なら path)。
func internalName(p *sitter.Node, src []byte) string {
	last := ""
	for i := range p.ChildCount() {
		c := p.Child(i)
		if c.Kind() == ":" {
			break
		}
		if c.Kind() == "simple_identifier" {
			last = c.Utf8Text(src)
		}
	}
	return last
}

func returnType(n *sitter.Node, src []byte) string {
	after := false
	for i := range n.ChildCount() {
		c := n.Child(i)
		if c.Kind() == "->" {
			after = true
			continue
		}
		if after && c.IsNamed() {
			return collapse(c.Utf8Text(src))
		}
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// convert は tree-sitter の節を言語に依らない木に写す。
func (w *walker) convert(n *sitter.Node) *tree.Node {
	return w.conv(n, nil, "")
}

func (w *walker) conv(n *sitter.Node, parent *sitter.Node, field string) *tree.Node {
	k := n.Kind()
	out := &tree.Node{Kind: k}
	switch {
	case !n.IsNamed():
		out.Role = tree.RoleToken
		out.Text = w.text(n)
		return out
	case nameKinds[k]:
		out.Text = w.text(n)
		out.Role = nameRole(parent, field)
		out.Expr = out.Role == tree.RoleName
		return out
	case typeKinds[k]:
		out.Role = tree.RoleType
		out.Text = w.text(n)
		return out
	case literalKinds[k] != "":
		out.Role = tree.RoleLiteral
		out.LitKind = literalKinds[k]
		out.Text = w.text(n)
		out.Expr = out.LitKind != "str" // 文字列の断片は、囲む文字列リテラルの節で畳む
		return out
	}
	out.Expr = exprKinds[k]
	firstNamed := true
	for i := range n.ChildCount() {
		c := n.Child(i)
		if commentKinds[c.Kind()] {
			continue
		}
		cn := w.conv(c, n, n.FieldNameForChild(uint32(i)))
		if c.IsNamed() && firstNamed {
			firstNamed = false
			if k == "call_expression" || k == "constructor_expression" {
				cn.Callee = true
			}
		}
		if k == "macro_invocation" && c.Kind() == "simple_identifier" {
			cn.Callee = true
		}
		out.Children = append(out.Children, cn)
	}
	return out
}

// nameRole は simple_identifier の役割を親と field 名から決める。
func nameRole(parent *sitter.Node, field string) tree.Role {
	if parent == nil {
		return tree.RoleName
	}
	switch parent.Kind() {
	case "value_argument_label":
		return tree.RoleLabel
	case "navigation_suffix":
		return tree.RoleMember
	case "pattern", "lambda_parameter", "parameter":
		return tree.RoleDecl
	}
	if field == "bound_identifier" {
		return tree.RoleDecl
	}
	return tree.RoleName
}
