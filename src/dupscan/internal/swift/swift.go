// Package swift は、Swift のソースを tree-sitter で読み、比べる単位 (関数・初期化子・クロージャなど) を言語に依らない木に写す adapter。
package swift

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// HasError はファイルのどこかに読めない構文 (ERROR / MISSING) があること。読めない部分に飲み込まれた関数は単位として出てこないので、
	// 呼び出し側は「比べていない部分がある」ことを出力に出す。
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
	w := &walker{file: file, src: src, test: IsTestPath(file), closures: map[string]int{}, seen: map[string]int{}, isUnit: map[string]bool{}}
	w.walk(root, ctx{})
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
	file     string
	src      []byte
	test     bool
	units    []*tree.Unit
	closures map[string]int
	seen     map[string]int  // 鍵の重なりの数 (`#if` の両側の同じ関数など)
	isUnit   map[string]bool // 単位の鍵 (クロージャの Owner にしてよいか)
}

// ctx は辿っている場所の文脈。
type ctx struct {
	enclosing []string // 囲む型の名前
	owner     string   // 囲む単位の鍵
	inError   bool     // 読めない構文 (ERROR) の中
	testClass bool     // XCTestCase を継承した型の中
	property  string   // 囲む property の名前 (didSet などの単位の名前に使う)
}

func (w *walker) text(n *sitter.Node) string { return n.Utf8Text(w.src) }

func (w *walker) walk(n *sitter.Node, c ctx) {
	if n.IsError() || n.IsMissing() {
		c.inError = true
	}
	switch n.Kind() {
	case "class_declaration", "protocol_declaration":
		if name := w.typeName(n); name != "" {
			c.enclosing = append(append([]string(nil), c.enclosing...), name)
		}
		if w.inheritsTestCase(n) {
			c.testClass = true
		}
	case "function_declaration", "init_declaration", "deinit_declaration", "subscript_declaration":
		if w.isTestFunc(n, c) {
			return
		}
		c.owner = w.add(w.declUnit(n, c), c)
	case "property_declaration":
		if name := w.boundName(n); name != "" {
			c.property = name
		}
		if cp := n.ChildByFieldName("computed_value"); cp != nil {
			c.owner = w.add(w.propertyUnit(n, cp, c.property, c), c)
		}
	case "didset_clause", "willset_clause":
		name := "didSet"
		if n.Kind() == "willset_clause" {
			name = "willSet"
		}
		c.owner = w.add(w.newUnit(n, tree.KindProperty, c.property+"."+name, "", nil, n, c), c)
	case "lambda_literal":
		c.owner = w.add(w.closureUnit(n, c), c)
	}
	for i := range n.ChildCount() {
		w.walk(n.Child(i), c)
	}
}

// add は単位を足し、囲む単位として使う鍵を返す。鍵が重なったら (`#if` の両側の同じ関数・同じ署名の入れ子の関数) 番号を付けて分ける。
func (w *walker) add(u *tree.Unit, c ctx) string {
	w.seen[u.Key]++
	if n := w.seen[u.Key]; n > 1 {
		u.Key = fmt.Sprintf("%s#%d", u.Key, n)
	}
	u.Unreadable = u.Unreadable || c.inError
	w.units = append(w.units, u)
	w.isUnit[u.Key] = true
	return u.Key
}

func (w *walker) typeName(n *sitter.Node) string {
	c := n.ChildByFieldName("name")
	if c == nil {
		return ""
	}
	name := w.text(c)
	// 条件つきの extension (`extension Array where Element == Int`) どうしを分ける。
	for i := range n.NamedChildCount() {
		if ch := n.NamedChild(i); ch.Kind() == "where_clause" || ch.Kind() == "type_constraints" {
			name += " " + collapse(w.text(ch))
		}
	}
	return name
}

func (w *walker) inheritsTestCase(n *sitter.Node) bool {
	for i := range n.NamedChildCount() {
		if ch := n.NamedChild(i); ch.Kind() == "inheritance_specifier" && strings.HasSuffix(w.text(ch), "TestCase") {
			return true
		}
	}
	return false
}

func (w *walker) boundName(n *sitter.Node) string {
	p := n.ChildByFieldName("name")
	if p == nil {
		return ""
	}
	if b := p.ChildByFieldName("bound_identifier"); b != nil {
		return w.text(b)
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

// isTestFunc は Swift Testing の `@Test` (`@Testing.Test` も) か、XCTest の `func test…()` (引数なし。test のファイルか XCTestCase を継承した型の中) か。
func (w *walker) isTestFunc(n *sitter.Node, c ctx) bool {
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
				if t := a.NamedChild(j); t.Kind() == "user_type" {
					if s := w.text(t); s == "Test" || strings.HasSuffix(s, ".Test") {
						return true
					}
				}
			}
		}
	}
	name := ""
	if ch := n.ChildByFieldName("name"); ch != nil {
		name = w.text(ch)
	}
	return (w.test || c.testClass) && len(c.enclosing) > 0 && strings.HasPrefix(name, "test") && len(params(n)) == 0
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

func (w *walker) isStatic(n *sitter.Node) bool {
	if m := w.modifiers(n); m != nil {
		for i := range m.NamedChildCount() {
			if s := w.text(m.NamedChild(i)); s == "static" || s == "class" {
				return true
			}
		}
	}
	return false
}

func (w *walker) declUnit(n *sitter.Node, c ctx) *tree.Unit {
	kind := tree.KindFunc
	var name string
	switch n.Kind() {
	case "init_declaration":
		kind, name = tree.KindInit, "init"
	case "deinit_declaration":
		kind, name = tree.KindProperty, "deinit"
	case "subscript_declaration":
		kind, name = tree.KindProperty, "subscript"
	default:
		if ch := n.ChildByFieldName("name"); ch != nil {
			name = w.text(ch)
		}
	}
	var sig, locals []string
	for _, p := range params(n) {
		sig = append(sig, collapse(w.text(p)))
		if in := internalName(p, w.src); in != "" {
			locals = append(locals, in)
		}
	}
	signature := name + "(" + strings.Join(sig, ", ") + ")"
	ret := returnType(n, w.src)
	if ret != "" {
		signature += " -> " + ret
	}
	if w.isStatic(n) {
		signature = "static " + signature
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		body = childOfKind(n, "computed_property") // subscript の本体 (引数の宣言を本体に入れない)
	}
	if body == nil && n.Kind() == "deinit_declaration" {
		body = childOfKind(n, "function_body")
	}
	u := w.newUnit(n, kind, name, signature, locals, body, c)
	u.ReturnType = ret
	u.Visibility = w.visibility(n)
	return u
}

func (w *walker) propertyUnit(decl, computed *sitter.Node, name string, c ctx) *tree.Unit {
	u := w.newUnit(decl, tree.KindProperty, name, "var "+name, nil, computed, c)
	for i := range decl.NamedChildCount() {
		if ch := decl.NamedChild(i); ch.Kind() == "type_annotation" {
			u.ReturnType = collapse(strings.TrimPrefix(w.text(ch), ":"))
			u.Signature += ": " + u.ReturnType
		}
	}
	u.Visibility = w.visibility(decl)
	return u
}

func (w *walker) closureUnit(n *sitter.Node, c ctx) *tree.Unit {
	owner := c.owner
	if owner == "" {
		owner = w.file + "|" + strings.Join(c.enclosing, ".")
	}
	w.closures[owner]++
	u := w.newUnit(n, tree.KindClosure, "{closure}", "", nil, n, c)
	u.Key = fmt.Sprintf("%s#closure%d", owner, w.closures[owner])
	if w.isUnit[c.owner] {
		u.Owner = c.owner
	}
	u.Visibility = "local"
	return u
}

// newUnit は単位を作る。鍵はファイル + 囲む型 + (入れ子なら囲む単位) + 署名。
func (w *walker) newUnit(n *sitter.Node, kind tree.UnitKind, name, sig string, locals []string, body *sitter.Node, c ctx) *tree.Unit {
	pos := n.StartPosition()
	encl := strings.Join(c.enclosing, ".")
	label := sig
	if label == "" {
		label = name
	}
	key := w.file + "|" + encl + "|" + label
	if c.owner != "" && kind != tree.KindClosure {
		key = c.owner + "/" + label // 関数の中の関数は、囲む単位ごとに分ける
	}
	u := &tree.Unit{
		Key: key, File: w.file, Line: int(pos.Row) + 1, Col: int(pos.Column) + 1,
		Name: name, Signature: sig, Kind: kind, Enclosing: encl, Test: w.test || c.testClass,
		Params: locals, Unreadable: broken(n), Visibility: "internal",
	}
	if body != nil {
		u.Body = w.conv(body, nil, "", false, false)
	} else {
		u.Body = &tree.Node{Kind: "empty"}
	}
	return u
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

func childOfKind(n *sitter.Node, kind string) *sitter.Node {
	for i := range n.NamedChildCount() {
		if c := n.NamedChild(i); c.Kind() == kind {
			return c
		}
	}
	return nil
}

// containerKinds は単位の親として数えない入れ物 (ファイル・型の本体)。ここに読めない構文があっても、隣の単位は読めている。
var containerKinds = map[string]bool{"source_file": true, "class_body": true, "enum_class_body": true, "protocol_body": true, "statements": true}

// broken は単位の節か、入れ物 (型・ファイルの本体) に着くまでの祖先のどれかが読めない構文を含むか
// (読めない部分に飲み込まれた関数の本体が、隣のクロージャとして読まれることがある)。
func broken(n *sitter.Node) bool {
	for c := n; c != nil && !containerKinds[c.Kind()]; c = c.Parent() {
		if c.HasError() {
			return true
		}
	}
	return false
}

// conv は tree-sitter の節を言語に依らない木に写す。binding は let / var で束縛する pattern の中か、afterDot は直前のトークンが `.` か。
func (w *walker) conv(n, parent *sitter.Node, field string, binding, afterDot bool) *tree.Node {
	k := n.Kind()
	out := &tree.Node{Kind: k}
	switch {
	case !n.IsNamed():
		out.Role = tree.RoleToken
		out.Text = w.text(n)
		return out
	case classify(k) == className:
		out.Text = w.text(n)
		out.Role = w.nameRole(parent, field, binding, afterDot)
		out.Expr = out.Role == tree.RoleName
		return out
	case classify(k) == classType:
		out.Role = tree.RoleType
		out.Text = w.text(n)
		return out
	case classify(k) == classLiteral:
		out.Role = tree.RoleLiteral
		out.LitKind = literalKinds[k]
		out.Text = w.text(n)
		out.Expr = out.LitKind != "str" // 文字列の断片は、囲む文字列リテラルの節で畳む
		return out
	case n.ChildCount() == 0:
		// 子の無い名前つきの節 (custom_operator の `<+>`・statement_label の `outer:`) は字面をハッシュに入れる。
		out.Role = tree.RoleToken
		out.Text = w.text(n)
		out.Expr = classify(k) == classExpr
		return out
	}
	out.Expr = classify(k) == classExpr
	if k == "directly_assignable_expression" {
		out.Keep = true
	}
	// let / var は pattern の中 (`case let .some(z)`) にも、その外 (`let (a, b) = t` の property_declaration・
	// `if case let .pair(x, y) = v` の if_statement に平らに置かれたトークン) にも現れる。
	//   - pattern の中: 外から渡された束縛か、pattern 自身が let / var を持てば、その下全部に渡す
	//   - それ以外: let / var から、条件の区切りの `,` までを 1 つの節とし、その中の pattern と bound_identifier にだけ渡す
	//     (本体・初期化子の側へ渡すと中の switch の `case minValue:` まで局所になり、文の単位で渡すと
	//     `if let a = o, case .y(minValue) = z` の minValue まで局所になる)
	//   - for の項目 (`for (a, b) in`) は let が無くても束縛
	inPattern := k == "pattern"
	patternBinds := inPattern && binding
	if inPattern {
		for i := range n.ChildCount() {
			if isBindingKeyword(n.Child(i), w.src) {
				patternBinds = true
			}
		}
	}
	condList := k == "if_statement" || k == "guard_statement" || k == "while_statement" || k == "repeat_while_statement"
	clause := false
	depth := 0 // case パターンの `.pair(x, y)` の括弧と , も if 文の直下に平らに並ぶので、深さ 0 の , だけを条件の区切りにする
	firstNamed := true
	prevDot := false
	casePattern := false // `if case .timeout = e` の case から = までは pattern (case 名を畳まない)
	for i := range n.ChildCount() {
		c := n.Child(i)
		if classify(c.Kind()) == classComment {
			continue
		}
		f := n.FieldNameForChild(uint32(i))
		if isBindingKeyword(c, w.src) {
			clause = true
		}
		if condList && !c.IsNamed() {
			switch w.text(c) {
			case "case":
				casePattern = true
			case "=":
				casePattern = false
			case "(":
				depth++
			case ")":
				depth--
			case ",":
				if depth == 0 {
					clause = false
				}
			}
		}
		childBinds := patternBinds || (clause && (c.Kind() == "pattern" || f == "bound_identifier")) || (k == "for_statement" && f == "item")
		cn := w.conv(c, n, f, childBinds, prevDot)
		if casePattern && c.IsNamed() {
			cn.Keep = true
		}
		prevDot = !c.IsNamed() && w.text(c) == "."
		if c.IsNamed() && firstNamed {
			firstNamed = false
			if k == "call_expression" || k == "constructor_expression" {
				cn.Keep = true
			}
		}
		if k == "macro_invocation" && c.Kind() == "simple_identifier" {
			cn.Keep = true
		}
		if k == "statements" {
			cn.NoFold = true
		}
		out.Children = append(out.Children, cn)
	}
	if k == "call_expression" && len(out.Children) > 0 {
		out.Action = isActionCallee(n.NamedChild(0), w.src)
	}
	return out
}

func isBindingKeyword(c *sitter.Node, src []byte) bool {
	if c.Kind() == "value_binding_pattern" {
		return true
	}
	if c.IsNamed() {
		return false
	}
	t := c.Utf8Text(src)
	return t == "let" || t == "var"
}

// isActionCallee は callee が処理の呼び出しか。値を作るだけの呼び出し (大文字で始まる型の初期化 `ConnectionID(…)`・
// 暗黙のメンバー `.bucket(…)`) 以外は全部処理に数える (`_save`・“ `delete` “・英字以外の名前も処理)。
// 大文字で始まる処理 (`XCTUnwrap(…)`) は構文だけでは型の初期化と区別できず、値として畳まれる (docs/dupscan-detection.md の限界)。
func isActionCallee(c *sitter.Node, src []byte) bool {
	for c != nil {
		switch c.Kind() {
		case "postfix_expression": // onDelete!() の !
			c = c.NamedChild(0)
			continue
		case "tuple_expression": // (deleteAll)()
			if c.NamedChildCount() == 1 {
				c = c.NamedChild(0)
				continue
			}
		case "prefix_expression": // 暗黙のメンバー
			if op := c.ChildByFieldName("operation"); op != nil && op.Utf8Text(src) == "." {
				return false
			}
		case "array_literal", "dictionary_literal": // [Int](repeating:count:) などの型の初期化
			return false
		case "simple_identifier":
			return !startsUpper(c.Utf8Text(src))
		case "navigation_expression":
			if s := c.ChildByFieldName("suffix"); s != nil {
				if id := s.ChildByFieldName("suffix"); id != nil {
					name := id.Utf8Text(src)
					return name != "init" && !startsUpper(name) // ConnectionID.init(rawValue:) も型の初期化
				}
			}
		}
		return true
	}
	return true
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

// nameRole は simple_identifier の役割を、親・field 名・束縛の文脈から決める。
func (w *walker) nameRole(parent *sitter.Node, field string, binding, afterDot bool) tree.Role {
	if parent == nil {
		return tree.RoleName
	}
	if field == "bound_identifier" && binding {
		return tree.RoleDecl // `if case minValue = z` の minValue も bound_identifier だが、let / var が無ければ値
	}
	switch parent.Kind() {
	case "value_argument_label":
		return tree.RoleLabel
	case "navigation_suffix":
		return tree.RoleMember
	case "lambda_parameter", "parameter", "didset_clause", "willset_clause", "capture_list_item":
		return tree.RoleDecl // didSet(old) の old も引数
	case "pattern":
		// `case .light:` の light・`case let .some(z)` の some は enum の case 名。`case minValue:` は値。
		// 束縛 (let / var) の pattern の中の名前だけが局所の名前。
		if afterDot {
			return tree.RoleMember
		}
		if binding {
			return tree.RoleDecl
		}
	case "control_transfer_statement":
		// `break outer` / `continue outer` のラベル。
		if first := parent.Child(0); first != nil && (first.Kind() == "break" || first.Kind() == "continue") {
			return tree.RoleLabel
		}
	}
	return tree.RoleName
}
