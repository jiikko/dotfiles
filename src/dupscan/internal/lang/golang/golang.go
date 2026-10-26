// Package golang は、Go のソースを標準の go/ast で読み、比べる単位 (関数・メソッド・関数リテラル) を言語に依らない木に写す adapter。
//
// tree-sitter-go を使わないのは、コアが見るのが internal/tree の中立の木だけで、go/ast から写せば足りるため
// (文法の版を pin しなくてよく、正しい Go で読めない構文が出ない)。型は解決しない (go/types を通さない)。
package golang

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"dupscan/internal/lang"
	"dupscan/internal/tree"
)

// Extensions は adapter が読むファイルの拡張子。
var Extensions = []string{".go"}

// Adapter は Go の読み方。def と refs の材料 (Options.Defs / Refs) は集めない (Go では gopls が型を解決して答えられる)。
var Adapter = lang.Adapter{Name: "go", Extensions: Extensions, IsTestPath: IsTestPath, NewParser: newParser}

// Parser は状態を持たない (go/parser はファイルごとに FileSet を作る)。
type Parser struct{ opt lang.Options }

func newParser(o lang.Options) (lang.Parser, error) { return &Parser{opt: o}, nil }

// Close は何もしない。
func (p *Parser) Close() {}

// IsTestPath は Go の慣習で test のファイルか (`_test.go` と、go のツールが読まない `testdata` の下)。
func IsTestPath(file string) bool {
	if strings.HasSuffix(file, "_test.go") {
		return true
	}
	for _, c := range strings.Split(path.Dir(file), "/") {
		if c == "testdata" {
			return true
		}
	}
	return false
}

// allowMarker は単位の直前のコメントに書く許可の印 (Swift の adapter と同じ)。
const allowMarker = "dupscan:allow"

// Parse は file (repo の root からの相対) の src から単位を取り出す。テスト関数そのものとその中の関数リテラルは単位にしない。
// 生成されたファイル (`// Code generated … DO NOT EDIT.`) は単位も呼び出しも出さない (人が寄せる対象ではない)。
func (p *Parser) Parse(file string, src []byte) (lang.FileResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments|parser.AllErrors|parser.SkipObjectResolution)
	var errs scanner.ErrorList
	if err != nil && !errors.As(err, &errs) {
		return lang.FileResult{}, fmt.Errorf("%s を parse できない: %w", file, err)
	}
	if f == nil {
		return lang.FileResult{}, fmt.Errorf("%s を parse できない", file)
	}
	r := lang.FileResult{HasError: len(errs) > 0}
	if ast.IsGenerated(f) {
		return r, nil
	}
	// 関数の宣言の Doc のほかは、節に付いたコメントを外す。型の字面は go/printer で書き出すので、残すと `X int // c` や
	// (型の中の関数リテラルの) `var k = 1 // c` のコメントが型の字面に入り、コメントだけが違う写し (テストの表の struct に
	// よくある) を見逃す。この adapter が読むコメントは関数の宣言の Doc (doc と許可の印) だけ。
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			x.Doc, x.Comment = nil, nil
		case *ast.ValueSpec:
			x.Doc, x.Comment = nil, nil
		case *ast.TypeSpec:
			x.Doc, x.Comment = nil, nil
		case *ast.ImportSpec:
			x.Doc, x.Comment = nil, nil
		case *ast.GenDecl:
			x.Doc = nil
		}
		return true
	})
	w := &walker{fset: fset, file: file, src: src, test: IsTestPath(file), errs: errs,
		closures: map[string]int{}, seen: map[string]int{}}
	for i, d := range f.Decls {
		// 宣言の誤りは次の宣言の手前までで数える。閉じないまま EOF で終わる関数は、誤りの位置が宣言の End より後ろに出る
		limit := len(src) + 1 // EOF の誤りはファイルの長さちょうどの位置に出る
		if i+1 < len(f.Decls) {
			limit = w.offset(f.Decls[i+1].Pos())
		}
		switch d := d.(type) {
		case *ast.FuncDecl:
			w.funcDecl(d, limit)
		case *ast.GenDecl:
			// パッケージの変数の初期値の関数リテラル (`var handler = func(…) {…}`)
			for _, s := range d.Specs {
				if vs, ok := s.(*ast.ValueSpec); ok {
					for i, v := range vs.Values {
						name := ""
						if i < len(vs.Names) {
							name = vs.Names[i].Name
						}
						w.closuresIn(v, ctx{property: name, broken: w.errorIn(d.Pos(), limit)})
					}
				}
			}
		}
	}
	r.Units = w.units
	if p.opt.Calls {
		r.Calls = w.calls(f)
	}
	return r, nil
}

type walker struct {
	fset     *token.FileSet
	file     string
	src      []byte
	test     bool
	errs     scanner.ErrorList
	units    []*tree.Unit
	closures map[string]int
	seen     map[string]int // 鍵の重なりの数 (1 つのファイルに複数書ける `func init()`)
	lines    []string
}

// ctx は辿っている場所の文脈。
type ctx struct {
	owner     string // 囲む単位の鍵
	ownerName string // 囲む単位の表示名
	property  string // 囲むパッケージの変数の名前 (その初期値の関数リテラルの表示に使う)
	broken    bool   // 囲む宣言が読めない構文を含む (Swift の adapter と同じく、中の単位も読めないとする)
}

// testPrefixes は go test が呼ぶ関数の名前の頭。引数の形は見ない (形の合わない `TestX()` は go test がビルドを拒むので、正しい Go には現れない)。
var testPrefixes = []string{"Test", "Benchmark", "Fuzz", "Example"}

// isTestFunc は go test が呼ぶ関数か (test のファイルの、receiver の無い Test / Benchmark / Fuzz / Example で始まり、
// その後ろが小文字で始まらない関数。go test の規則)。加えて、test のファイルにある receiver つきの引数の無い
// `Test…` メソッド (testify の suite が呼ぶ) も除く (Swift の adapter が XCTestCase の `func test…()` を除くのと同じ)。
func (w *walker) isTestFunc(d *ast.FuncDecl) bool {
	if !w.test {
		return false
	}
	for _, prefix := range testPrefixes {
		rest, ok := strings.CutPrefix(d.Name.Name, prefix)
		if !ok || startsLower(rest) {
			continue
		}
		if d.Recv == nil || (prefix == "Test" && d.Type.Params.NumFields() == 0) {
			return true
		}
	}
	return false
}

func startsLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return s != "" && unicode.IsLower(r)
}

func (w *walker) funcDecl(d *ast.FuncDecl, limit int) {
	if d.Body == nil || w.isTestFunc(d) {
		return
	}
	encl := ""
	var locals []string
	if d.Recv != nil && len(d.Recv.List) > 0 {
		encl = receiverType(d.Recv.List[0].Type)
		locals = append(locals, fieldNames(d.Recv)...)
	}
	name := d.Name.Name
	params, paramTypes, labels := paramList(d.Type.Params)
	locals = append(locals, fieldNames(d.Type.Params)...)
	locals = append(locals, fieldNames(d.Type.Results)...)
	sig := name + "(" + params + ")"
	if r := results(d.Type.Results); r != "" {
		sig += " " + r
	}
	broken := w.errorIn(d.Pos(), limit)
	u := w.newUnit(d, tree.KindFunc, name, sig, encl, locals, ctx{broken: broken})
	u.Body = w.conv(d.Body)
	u.Delegation = delegation(d.Body)
	// 戻り値は型だけをハッシュに入れる。名前は Params に入れて本体の中で番号にするので、字面で入れると名前を変えた写しを見逃す
	u.ReturnType = resultTypes(d.Type.Results)
	u.ParamTypes = paramTypes
	u.Labels = labels
	u.Visibility = "unexported"
	if ast.IsExported(name) {
		u.Visibility = "public"
	}
	u.Allow, u.AllowLine, u.HasAllow = w.allowComment(d.Doc)
	u.Doc = docLine(d.Doc)
	key := w.add(u)
	w.closuresIn(d.Body, ctx{owner: key, ownerName: name, broken: broken})
}

// closuresIn は n の中の関数リテラルを単位にする。入れ子のリテラルは、それを囲むリテラルを囲む単位にする。
func (w *walker) closuresIn(n ast.Node, c ctx) {
	ast.Inspect(n, func(x ast.Node) bool {
		fl, ok := x.(*ast.FuncLit)
		if !ok {
			return true
		}
		owner := c.owner
		if owner == "" {
			owner = w.file + "|"
		}
		w.closures[owner]++
		u := w.newUnit(fl, tree.KindClosure, "{closure}", "", "", nil, c)
		u.Key = fmt.Sprintf("%s#closure%d", owner, w.closures[owner])
		u.Owner = c.owner
		u.OwnerName = c.ownerName
		if u.OwnerName == "" {
			u.OwnerName = c.property
		}
		u.Visibility = "local"
		// 引数は本体の木の中で宣言として数える (Swift のクロージャと同じ。Params には入れない)。
		u.Body = w.conv(fl)
		key := w.add(u)
		w.closuresIn(fl.Body, ctx{owner: key, ownerName: u.OwnerName + ".{closure}", broken: u.Unreadable})
		return false
	})
}

// newUnit は単位を作る。鍵はファイル + 囲む型 + (入れ子なら囲む単位) + 署名。
func (w *walker) newUnit(n ast.Node, kind tree.UnitKind, name, sig, encl string, locals []string, c ctx) *tree.Unit {
	pos, end := w.position(n.Pos()), w.position(n.End())
	label := sig
	if label == "" {
		label = name
	}
	key := w.file + "|" + encl + "|" + label
	if c.owner != "" && kind != tree.KindClosure {
		key = c.owner + "/" + label
	}
	return &tree.Unit{
		Key: key, File: w.file, Line: pos.Line, EndLine: end.Line, Col: pos.Column,
		Name: name, Signature: sig, Kind: kind, Enclosing: encl, Test: w.test,
		Params: locals, Unreadable: c.broken || w.errorIn(n.Pos(), w.offset(n.End())+1),
	}
}

// add は単位を足し、鍵を返す。鍵が重なったら番号を付けて分ける。
func (w *walker) add(u *tree.Unit) string {
	w.seen[u.Key]++
	if n := w.seen[u.Key]; n > 1 {
		u.Key = fmt.Sprintf("%s#%d", u.Key, n)
	}
	w.units = append(w.units, u)
	return u.Key
}

// position は pos の実際の場所 (`//line` の指示で書き換えた場所ではない。単位と呼び出しの行は、このファイルの行を指す)。
func (w *walker) position(p token.Pos) token.Position { return w.fset.PositionFor(p, false) }

func (w *walker) offset(p token.Pos) int { return w.position(p).Offset }

// errorIn は from から limit (offset。含まない) までに構文の誤りがあるか (go/parser は誤りの周りを BadExpr などにして読み続ける)。
func (w *walker) errorIn(from token.Pos, limit int) bool {
	start := w.offset(from)
	for _, e := range w.errs {
		if e.Pos.Offset >= start && e.Pos.Offset < limit {
			return true
		}
	}
	return false
}

// receiverType は receiver の型の名前 (`*Box[T]` なら Box)。
func receiverType(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return typeString(e)
		}
	}
}

// fieldNames は局所の名前として数える名前 (`_` と名前の無いものは除く)。
func fieldNames(fl *ast.FieldList) []string {
	var out []string
	if fl == nil {
		return nil
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			if n.Name != "_" {
				out = append(out, n.Name)
			}
		}
	}
	return out
}

// paramList は署名に書く引数の並び・引数ごとの型・呼び出しの照合に使うラベル (Go に引数ラベルは無いので引数 1 つにつき `_`)。
func paramList(fl *ast.FieldList) (string, []string, []string) {
	var parts, typs, labels []string
	if fl == nil {
		return "", nil, nil
	}
	for _, f := range fl.List {
		t := typeString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, t)
			typs = append(typs, t)
			labels = append(labels, "_")
			continue
		}
		var names []string
		for _, n := range f.Names {
			names = append(names, n.Name)
			typs = append(typs, t)
			labels = append(labels, "_")
		}
		parts = append(parts, strings.Join(names, ", ")+" "+t)
	}
	return strings.Join(parts, ", "), typs, labels
}

// resultTypes は戻り値の型だけの並び (名前を除く。ハッシュと索引の -returns に使う)。
func resultTypes(fl *ast.FieldList) string {
	_, typs, _ := paramList(fl)
	switch len(typs) {
	case 0:
		return ""
	case 1:
		return typs[0]
	}
	return "(" + strings.Join(typs, ", ") + ")"
}

// results は戻り値の型 (1 つで名前が無ければ括弧を付けない)。
func results(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	s, _, _ := paramList(fl)
	if len(fl.List) == 1 && len(fl.List[0].Names) == 0 {
		return s
	}
	return "(" + s + ")"
}

// commentText はコメント 1 つの中身 (記号を除く)。
func commentText(c *ast.Comment) string {
	t := strings.TrimPrefix(c.Text, "//")
	t = strings.TrimSuffix(strings.TrimPrefix(t, "/*"), "*/")
	return strings.TrimSpace(t)
}

// allowComment は宣言の直前のコメント (空行を挟まない。go/ast の Doc) の行の先頭から許可の印を探す。
// 印はその行の先頭だけで読む (`// TODO: dupscan:allow は使わない` のような文を許可にしない)。
func (w *walker) allowComment(doc *ast.CommentGroup) (string, int, bool) {
	if doc == nil {
		return "", 0, false
	}
	for _, c := range doc.List {
		if after, ok := strings.CutPrefix(commentText(c), allowMarker); ok {
			return strings.TrimSpace(after), w.position(c.Pos()).Line, true
		}
	}
	return "", 0, false
}

// docLine は doc comment の最初の行 (許可の印の行と、`//go:` などの指示は除く)。
func docLine(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	for _, line := range strings.Split(doc.Text(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, allowMarker) {
			return line
		}
	}
	return ""
}

// calls はファイルの中の呼び出しを全部集める (テスト関数の中も)。型を解決しないので名前と引数の数だけの近似。
func (w *walker) calls(f *ast.File) []tree.Call {
	if w.lines == nil {
		w.lines = strings.Split(string(w.src), "\n")
	}
	var out []tree.Call
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := calleeName(call.Fun)
		if name == "" {
			return true
		}
		pos := w.position(call.Pos())
		c := tree.Call{File: w.file, Line: pos.Line, Col: pos.Column, Name: name, Labels: []string{}}
		for range call.Args {
			c.Labels = append(c.Labels, "_")
		}
		if pos.Line-1 < len(w.lines) {
			c.Text = strings.TrimSpace(w.lines[pos.Line-1])
		}
		out = append(out, c)
		return true
	})
	return out
}

// calleeName は呼ばれる名前 (`a.b.Make(…)` なら Make)。型リテラルへの変換と関数リテラルの即時呼び出しは名前が無い。
func calleeName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name
		case *ast.SelectorExpr:
			return t.Sel.Name
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr: // 型引数の明示 `f[int](x)`
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		default:
			return ""
		}
	}
}
