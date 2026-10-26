package golang

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"dupscan/internal/tree"
)

// unknownKind は conv が知らない節に付ける種類の頭 (TestConvKnowsEveryNode が実物の Go のソースで 0 件を確かめる)。
// go/ast に節の型が増えたら、黙って構造だけの節にせずそこで落とす。
const unknownKind = "unknown:"

// kindPrefix は Go の節の種類名の頭。ハッシュには言語が入らないので、Swift (tree-sitter の種類名) の単位と同じ形に
// ならないことを、種類名が重ならないことで保証する (本体の根の種類が違えば、木の形がどうであれハッシュは違う)。
const kindPrefix = "go."

func node(kind string, children ...*tree.Node) *tree.Node {
	return &tree.Node{Kind: kindPrefix + kind, Children: children}
}

func expr(kind string, children ...*tree.Node) *tree.Node {
	n := node(kind, children...)
	n.Expr = true
	return n
}

func tok(s string) *tree.Node {
	return &tree.Node{Kind: kindPrefix + "tok", Role: tree.RoleToken, Text: s}
}

// none は省かれた部分 (`if` の初期化文・`a[:n]` の下限など)。省いた位置を残さないと `a[:n]` と `a[n:]` が同じになる。
func none() *tree.Node { return node("none") }

// typ は型の位置。字面のまま残す (型の名前を番号にすると、別の型を作る同じ形の処理が同じになる)。
func typ(e ast.Expr) *tree.Node {
	if e == nil {
		return none()
	}
	return &tree.Node{Kind: kindPrefix + "type", Role: tree.RoleType, Text: typeString(e)}
}

// typeString は型の字面。go/printer で書き出すが、位置を知らない新しい FileSet を渡す (位置が分からないと改行を入れず 1 行に
// 書くので、引数を複数行に分けて末尾にカンマを付けただけの型も同じ字面になる。元の FileSet を渡すと改行の位置で字面が変わる)。
// types.ExprString にしない: 構造体の tag と composite literal の中身 (`[len(T{1, 2})]int`) を落とし、tag の付く field を
// 入れ替えただけの別の型を同じにする。
func typeString(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return types.ExprString(e)
	}
	return b.String()
}

// decl は局所の名前の宣言 (`:=`・`var`・range の変数・関数リテラルの引数)。`_` は名前ではないので字面で残す。
func decl(id *ast.Ident) *tree.Node {
	if id.Name == "_" {
		return tok("_")
	}
	return &tree.Node{Kind: kindPrefix + "ident", Role: tree.RoleDecl, Text: id.Name}
}

// label は字面のまま残す名前 (構造体のリテラルの field 名・goto / break のラベル)。
func label(id *ast.Ident) *tree.Node {
	if id == nil {
		return none()
	}
	return &tree.Node{Kind: kindPrefix + "label", Role: tree.RoleLabel, Text: id.Name}
}

func name(id *ast.Ident) *tree.Node {
	switch id.Name {
	case "_":
		return tok("_")
	case "true", "false": // Go では識別子だが、Swift の boolean_literal と同じく L2 で種類にする
		return &tree.Node{Kind: kindPrefix + "lit", Role: tree.RoleLiteral, LitKind: "bool", Text: id.Name, Expr: true}
	}
	return &tree.Node{Kind: kindPrefix + "ident", Role: tree.RoleName, Text: id.Name, Expr: true}
}

var litKinds = map[token.Token]string{token.INT: "num", token.FLOAT: "num", token.IMAG: "num", token.CHAR: "char", token.STRING: "str"}

// conv は go/ast の節を言語に依らない木に写す。式・型・文で入口を分けず 1 つの switch で受ける (型は字面の 1 節にする)。
func (w *walker) conv(n ast.Node) *tree.Node {
	switch n := n.(type) {
	case nil:
		return none()
	// --- 式
	case *ast.Ident:
		return name(n)
	case *ast.BasicLit:
		return &tree.Node{Kind: kindPrefix + "lit", Role: tree.RoleLiteral, LitKind: litKinds[n.Kind], Text: n.Value, Expr: true}
	case *ast.CompositeLit:
		out := expr("CompositeLit", typ(n.Type))
		for _, e := range n.Elts {
			out.Children = append(out.Children, w.element(e))
		}
		return out
	case *ast.FuncLit:
		return expr("FuncLit", w.funcType(n.Type), w.conv(n.Body))
	case *ast.ParenExpr:
		return expr("ParenExpr", w.conv(n.X))
	case *ast.SelectorExpr:
		return expr("SelectorExpr", w.conv(n.X), &tree.Node{Kind: kindPrefix + "member", Role: tree.RoleMember, Text: n.Sel.Name})
	case *ast.IndexExpr:
		return expr("IndexExpr", w.conv(n.X), w.conv(n.Index))
	case *ast.IndexListExpr:
		out := expr("IndexListExpr", w.conv(n.X))
		for _, i := range n.Indices {
			out.Children = append(out.Children, w.conv(i))
		}
		return out
	case *ast.SliceExpr:
		out := expr("SliceExpr", w.conv(n.X), w.optExpr(n.Low), w.optExpr(n.High), w.optExpr(n.Max))
		if n.Slice3 {
			out.Children = append(out.Children, tok("3"))
		}
		return out
	case *ast.TypeAssertExpr:
		t := typ(n.Type)
		if n.Type == nil {
			t = tok("type") // `x.(type)`
		}
		return expr("TypeAssertExpr", w.conv(n.X), t)
	case *ast.CallExpr:
		return w.call(n)
	case *ast.StarExpr:
		return expr("StarExpr", w.conv(n.X))
	case *ast.UnaryExpr:
		x := w.conv(n.X)
		out := expr("UnaryExpr", tok(n.Op.String()), x)
		if n.Op == token.ARROW { // チャネルからの受信は処理。相手のチャネルも畳まない (`<-chA` と `<-chB` を L3 で同じにしない)
			out.Action = true
			x.Keep = true
		}
		return out
	case *ast.BinaryExpr:
		return expr("BinaryExpr", w.conv(n.X), tok(n.Op.String()), w.conv(n.Y))
	case *ast.KeyValueExpr:
		return w.element(n)
	case *ast.Ellipsis: // `[...]int` の長さ・可変長の引数の型
		return typ(n)
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		return typ(n.(ast.Expr))
	case *ast.BadExpr:
		return node("BadExpr")
	// --- 文
	case *ast.BlockStmt:
		out := node("Block")
		for _, s := range n.List {
			c := w.conv(s)
			c.NoFold = true
			out.Children = append(out.Children, c)
		}
		return out
	case *ast.ExprStmt:
		x := w.conv(n.X)
		x.NoFold = true // 文として置いた式は畳まない (`deleteAll()` と `showWelcome()` を同じにしない)
		return node("ExprStmt", x)
	case *ast.AssignStmt:
		out := node("Assign")
		for _, l := range n.Lhs {
			if id, ok := l.(*ast.Ident); ok && n.Tok == token.DEFINE {
				out.Children = append(out.Children, decl(id))
				continue
			}
			c := w.conv(l)
			c.Keep = true // 代入の左辺を畳むと `isLoading = …` と `isEditing = …` が同じになる
			out.Children = append(out.Children, c)
		}
		out.Children = append(out.Children, tok(n.Tok.String()))
		for _, r := range n.Rhs {
			out.Children = append(out.Children, w.conv(r))
		}
		return out
	case *ast.IncDecStmt:
		x := w.conv(n.X)
		x.Keep = true
		return node("IncDec", x, tok(n.Tok.String()))
	case *ast.DeclStmt:
		return w.conv(n.Decl)
	case *ast.GenDecl:
		out := node("GenDecl", tok(n.Tok.String()))
		for _, s := range n.Specs {
			out.Children = append(out.Children, w.conv(s))
		}
		return out
	case *ast.ValueSpec:
		out := node("ValueSpec")
		for _, id := range n.Names {
			out.Children = append(out.Children, decl(id))
		}
		out.Children = append(out.Children, typ(n.Type))
		for _, v := range n.Values {
			out.Children = append(out.Children, w.conv(v))
		}
		return out
	case *ast.TypeSpec: // 関数の中の型の宣言。名前も型の字面として残す
		out := node("TypeSpec", &tree.Node{Kind: kindPrefix + "type", Role: tree.RoleType, Text: n.Name.Name})
		if n.Assign.IsValid() {
			out.Children = append(out.Children, tok("=")) // `type T = int` (別名) と `type T int` (新しい型) を分ける
		}
		out.Children = append(out.Children, typ(n.Type))
		return out
	case *ast.ReturnStmt:
		out := node("Return")
		for _, r := range n.Results {
			out.Children = append(out.Children, w.conv(r))
		}
		return out
	case *ast.IfStmt:
		return node("If", w.optStmt(n.Init), w.conv(n.Cond), w.conv(n.Body), w.optStmt(n.Else))
	case *ast.ForStmt:
		return node("For", w.optStmt(n.Init), w.optExpr(n.Cond), w.optStmt(n.Post), w.conv(n.Body))
	case *ast.RangeStmt:
		return node("Range", w.rangeVar(n.Key, n.Tok), w.rangeVar(n.Value, n.Tok), tok(n.Tok.String()), w.conv(n.X), w.conv(n.Body))
	case *ast.SwitchStmt:
		return node("Switch", w.optStmt(n.Init), w.optExpr(n.Tag), w.conv(n.Body))
	case *ast.TypeSwitchStmt:
		return node("TypeSwitch", w.optStmt(n.Init), w.conv(n.Assign), w.typeCases(n.Body))
	case *ast.CaseClause:
		out := node("Case")
		if n.List == nil {
			out.Children = append(out.Children, tok("default"))
		}
		for _, e := range n.List {
			out.Children = append(out.Children, w.conv(e))
		}
		return w.withStmts(out, n.Body)
	case *ast.SelectStmt:
		return node("Select", w.conv(n.Body))
	case *ast.CommClause:
		out := node("Comm", w.optStmt(n.Comm))
		if n.Comm == nil {
			out.Children[0] = tok("default")
		}
		return w.withStmts(out, n.Body)
	case *ast.GoStmt:
		c := w.conv(n.Call)
		c.NoFold = true
		return node("Go", c)
	case *ast.DeferStmt:
		c := w.conv(n.Call)
		c.NoFold = true
		return node("Defer", c)
	case *ast.BranchStmt:
		return node("Branch", tok(n.Tok.String()), label(n.Label))
	case *ast.LabeledStmt:
		return node("Labeled", label(n.Label), w.conv(n.Stmt))
	case *ast.SendStmt:
		ch := w.conv(n.Chan)
		ch.Keep = true // 送り先を畳むと `chA <- v` と `chB <- v` が同じになる
		return node("Send", ch, tok("<-"), w.conv(n.Value))
	case *ast.EmptyStmt:
		return node("Empty")
	case *ast.BadStmt:
		return node("BadStmt")
	}
	return node(fmt.Sprintf("%s%T", unknownKind, n))
}

func (w *walker) optExpr(e ast.Expr) *tree.Node {
	if e == nil {
		return none()
	}
	return w.conv(e)
}

func (w *walker) optStmt(s ast.Stmt) *tree.Node {
	if s == nil {
		return none()
	}
	return w.conv(s)
}

func (w *walker) withStmts(out *tree.Node, body []ast.Stmt) *tree.Node {
	for _, s := range body {
		c := w.conv(s)
		c.NoFold = true
		out.Children = append(out.Children, c)
	}
	return out
}

// typeCases は型の switch の case (並ぶのは型なので字面で残す)。
func (w *walker) typeCases(b *ast.BlockStmt) *tree.Node {
	out := node("Block")
	for _, s := range b.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			out.Children = append(out.Children, w.conv(s))
			continue
		}
		c := node("TypeCase")
		if cc.List == nil {
			c.Children = append(c.Children, tok("default"))
		}
		for _, e := range cc.List {
			c.Children = append(c.Children, typ(e))
		}
		out.Children = append(out.Children, w.withStmts(c, cc.Body))
	}
	return out
}

// rangeVar は range の変数 (`:=` なら宣言、`=` なら代入の左辺)。
func (w *walker) rangeVar(e ast.Expr, t token.Token) *tree.Node {
	if e == nil {
		return none()
	}
	if id, ok := e.(*ast.Ident); ok && t == token.DEFINE {
		return decl(id)
	}
	c := w.conv(e)
	c.Keep = true
	return c
}

// element は複合リテラルの要素。`Name: v` の Name は構造体の field 名として字面で残す
// (型を解決しないので map のキーに書いた変数も字面になる。局所の変数をキーにした map のリテラルは番号にならず、
// 局所の `k` をキーにしたものとパッケージの定数 `k` をキーにしたものは同じになる)。名前に倒すと、field 名と同じ字面の局所の変数
// (`item{name: name}` と `item{title: title}`) が L1 で同じになる。どちらが多いかは測っていない (field 名の誤検知は gate を誤って止めるので、そちらを避ける側に倒した)。
// map のキーの誤検知が gate で出たら、型を解決する (go/types) か、複合リテラルの型が map のときだけ名前にするかを再評価する。
func (w *walker) element(e ast.Expr) *tree.Node {
	kv, ok := e.(*ast.KeyValueExpr)
	if !ok {
		return w.conv(e)
	}
	k := w.conv(kv.Key)
	if id, ok := kv.Key.(*ast.Ident); ok {
		k = label(id)
	}
	return node("KeyValue", k, w.conv(kv.Value))
}

// funcType は関数リテラルの引数と戻り値。名前は局所の宣言、型は字面。
func (w *walker) funcType(ft *ast.FuncType) *tree.Node {
	out := node("FuncType")
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		part := node("Fields")
		if fl != nil {
			for _, f := range fl.List {
				for _, id := range f.Names {
					part.Children = append(part.Children, decl(id))
				}
				part.Children = append(part.Children, typ(f.Type))
			}
		}
		out.Children = append(out.Children, part)
	}
	return out
}

func (w *walker) call(n *ast.CallExpr) *tree.Node {
	var callee *tree.Node
	if isTypeExpr(n.Fun) {
		callee = typ(n.Fun) // `[]byte(s)`・`(*T)(p)` は型への変換
	} else {
		callee = w.conv(n.Fun)
	}
	callee.Keep = true // callee を畳むと呼ぶ相手が消える
	out := expr("CallExpr", callee)
	for _, a := range n.Args {
		out.Children = append(out.Children, w.conv(a))
	}
	if n.Ellipsis.IsValid() {
		out.Children = append(out.Children, tok("..."))
	}
	out.Action = isAction(n.Fun)
	return out
}

// isTypeExpr は式の位置に書いた型リテラルか (`(*T)` は括弧の中の * が型を作る)。
func isTypeExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		return true
	case *ast.ParenExpr:
		if s, ok := t.X.(*ast.StarExpr); ok {
			return isTypeExpr(s.X) || isNamedType(s.X)
		}
		return isTypeExpr(t.X)
	}
	return false
}

func isNamedType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		_, ok := t.X.(*ast.Ident)
		return ok
	}
	return false
}

// valueBuiltins は値を作るだけの組み込み (変換・長さ・確保)。これ以外の呼び出しは全部処理に数える。
// 型を解決しないので、大文字で始まる名前が型への変換 (`UserID(s)`) か関数 (`Do(x)`) かは決まらない。
// Swift と違って Go は公開の関数も大文字で始まるので、大文字を値とみなさずに処理に倒す (畳まない側に倒すと見逃しは増えるが、
// 副作用の違う呼び出しを同じにはしない)。
var valueBuiltins = map[string]bool{
	"bool": true, "byte": true, "rune": true, "string": true, "error": true, "any": true, "uintptr": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
	"len": true, "cap": true, "make": true, "new": true, "append": true, "min": true, "max": true,
	"complex": true, "real": true, "imag": true,
}

// isAction は callee が処理の呼び出しか (L3 でこれを含む式を畳まない)。
func isAction(fun ast.Expr) bool {
	if isTypeExpr(fun) {
		return false
	}
	for {
		switch t := fun.(type) {
		case *ast.ParenExpr:
			fun = t.X
		case *ast.IndexExpr:
			fun = t.X
		case *ast.IndexListExpr:
			fun = t.X
		case *ast.Ident:
			return !valueBuiltins[t.Name]
		default:
			return true
		}
	}
}

// delegation は、本体が「1 つの呼び出しに原子 (atom) だけを渡す」委譲か (Swift の adapter と同じ基準)。
// 本体の文がちょうど 1 つで、それが式の文か 1 つの値を返す return で、中身が呼び出しで、callee と引数が全部原子のとき。
func delegation(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	var e ast.Expr
	switch s := body.List[0].(type) {
	case *ast.ExprStmt:
		e = s.X
	case *ast.ReturnStmt:
		if len(s.Results) != 1 {
			return false
		}
		e = s.Results[0]
	default:
		return false
	}
	call, ok := e.(*ast.CallExpr)
	return ok && atomCall(call, true)
}

// atomCall は callee が原子で、引数が全部原子の呼び出しか。withArgs が false なら引数を許さない (原子の中の `time.Now()`)。
func atomCall(call *ast.CallExpr, withArgs bool) bool {
	if !atom(call.Fun) || (!withArgs && len(call.Args) > 0) {
		return false
	}
	for _, a := range call.Args {
		if !atom(a) {
			return false
		}
	}
	return true
}

// atom は計算をしない値か: リテラル・名前・メンバーの参照・数値の負号・引数の無い呼び出し。
func atom(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.SelectorExpr:
		return atom(t.X)
	case *ast.UnaryExpr:
		lit, ok := t.X.(*ast.BasicLit)
		return t.Op == token.SUB && ok && lit.Kind != token.STRING && lit.Kind != token.CHAR
	case *ast.CallExpr:
		return atomCall(t, false)
	}
	return false
}
