package swift

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

func TestEveryKindIsClassified(t *testing.T) {
	l := Language()
	n := 0
	for id := range uint16(l.NodeKindCount()) {
		if !l.NodeKindIsNamed(id) || !l.NodeKindIsVisible(id) {
			continue
		}
		k := l.NodeKindForId(id)
		n++
		c := 0
		for _, m := range []map[string]bool{exprKinds, nameKinds, typeKinds, commentKinds, structKinds} {
			if m[k] {
				c++
			}
		}
		if literalKinds[k] != "" {
			c++
		}
		if classify(k) == classUnknown {
			t.Errorf("%s を classify が分類できない", k)
		}
		if c != 1 {
			t.Errorf("%s が分類されていない、または 2 つ以上の分類にある (%d)", k, c)
		}
	}
	if n < 150 {
		t.Fatalf("文法の名前つきの種類が %d しかない (列挙が空振りしていないか)", n)
	}
}

func parse(t *testing.T, file, src string) FileResult {
	t.Helper()
	p, err := NewParser()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r, err := p.Parse(file, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func find(t *testing.T, r FileResult, name string) *tree.Unit {
	t.Helper()
	for _, u := range r.Units {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("単位 %s が無い (%d 個)", name, len(r.Units))
	return nil
}

// docs/dupscan-detection.md の例を本物の parser に通す (合成の木だけのテストでは adapter の退行を検出しない)。
func TestDetectionDocExample(t *testing.T) {
	r := parse(t, "Sources/A.swift", `
struct S {
    func location(path: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: path))
    }
    func bucketLocation(_ p: String) -> StorageLocation {
        StorageLocation(connectionID: connectionID, path: ObjectPath(rawValue: p))
    }
    func other(_ p: String) -> StorageLocation {
        StorageLocation(connectionID: connectionID, path: BucketPath(rawValue: p))
    }
}`)
	a := normBoth(t, find(t, r, "location"))
	b := normBoth(t, find(t, r, "bucketLocation"))
	c := normBoth(t, find(t, r, "other"))
	for _, l := range []norm.Level{norm.L1, norm.L2} {
		if a.Hash[l] == b.Hash[l] {
			t.Errorf("%s で一致してしまう\n%s\n%s", l, a.Forms[l], b.Forms[l])
		}
	}
	if a.Hash[norm.L3] != b.Hash[norm.L3] {
		t.Errorf("L3 で一致しない\n%s\n%s", a.Forms[norm.L3], b.Forms[norm.L3])
	}
	if b.Hash[norm.L3] == c.Hash[norm.L3] {
		t.Errorf("呼ぶ型が違う (ObjectPath / BucketPath) のに L3 で一致する\n%s\n%s", b.Forms[norm.L3], c.Forms[norm.L3])
	}
	if a.L3Trivial || b.L3Trivial {
		t.Error("引数を使う単位が L3Trivial になった")
	}
}

func TestRenamedLocalsMatchAtL1(t *testing.T) {
	r := parse(t, "Sources/A.swift", `
func f(x: Int) -> Int { let y = x + 1; return y * 2 }
func g(a: Int) -> Int { let b = a + 1; return b * 2 }
func h(a: Int) -> Int { let b = a + 1; return b * 3 }`)
	f, g, h := normBoth(t, find(t, r, "f")), normBoth(t, find(t, r, "g")), normBoth(t, find(t, r, "h"))
	if f.Hash[norm.L1] != g.Hash[norm.L1] {
		t.Errorf("局所の名前だけが違うのに L1 で一致しない\n%s\n%s", f.Forms[norm.L1], g.Forms[norm.L1])
	}
	if g.Hash[norm.L1] == h.Hash[norm.L1] || g.Hash[norm.L2] != h.Hash[norm.L2] {
		t.Error("リテラルだけが違う単位は L1 で違い L2 で一致するはず")
	}
}

func TestTestFunctionsAreNotUnits(t *testing.T) {
	r := parse(t, "Tests/FooTests.swift", `
import XCTest
import Testing
final class FooTests: XCTestCase {
    func testA() { XCTAssertEqual(make(), 1); items.forEach { print($0) } }
    func makeFixture(name: String) -> Int { name.count }
}
@Test func bar() async throws { #expect(a == b) }
func helper(x: Int) -> Int { x + 1 }`)
	names := map[string]bool{}
	for _, u := range r.Units {
		names[u.Name] = true
		if !u.Test {
			t.Errorf("%s が test のファイルなのに Test=false", u.Name)
		}
	}
	if names["testA"] || names["bar"] || names["{closure}"] {
		t.Errorf("テスト関数 (XCTest / Swift Testing) とその中のクロージャが単位になった: %v", names)
	}
	if !names["makeFixture"] || !names["helper"] {
		t.Errorf("ヘルパーが単位にならない: %v", names)
	}
}

func TestKeysDistinguishEnclosingTypesAndClosures(t *testing.T) {
	r := parse(t, "Sources/A.swift", `
struct A { func make() -> Int { 1 } }
struct B { func make() -> Int { 1 } }
func run(xs: [Int]) { xs.forEach { print($0) }; xs.forEach { print($0 + 1) } }`)
	keys := map[string]bool{}
	for _, u := range r.Units {
		if keys[u.Key] {
			t.Errorf("鍵が重なった: %s", u.Key)
		}
		keys[u.Key] = true
	}
	if len(keys) != 5 {
		t.Errorf("単位の数 = %d, 期待 5 (make×2・run・クロージャ×2): %v", len(keys), keys)
	}
}

func TestUnreadableUnitIsMarked(t *testing.T) {
	r := parse(t, "Sources/A.swift", `
func ok(x: Int) -> Int { x + 1 }
func broken(x: Int) -> Int { let = = x }`)
	if find(t, r, "ok").Unreadable {
		t.Error("読める単位が Unreadable になった")
	}
	if !r.HasError {
		t.Error("ファイルの HasError が立たない")
	}
	bad := false
	for _, u := range r.Units {
		if u.Unreadable {
			bad = true
		}
	}
	if !bad {
		t.Error("読めない構文を含む単位が Unreadable にならない")
	}
}

func TestIsTestPath(t *testing.T) {
	cases := map[string]bool{
		"macOS/Tests/FooTests.swift":          true,
		"macOS/ObaketTests/Helpers/X.swift":   true,
		"shared/Tests/Foo/Bar.swift":          true,
		"Sources/Views/MainView.swift":        false,
		"Sources/TestsSupport/Foo.swift":      false,
		"Sources/LatestFeature/Contest.swift": false,
		"Sources/Foo/BarTests.swift":          true,
		"Sources/Foo/UITestsHelper/Bar.swift": false,
		"macOS/ObaketUITests/Flows/Foo.swift": true,
	}
	for f, want := range cases {
		if got := IsTestPath(f); got != want {
			t.Errorf("IsTestPath(%q) = %v, 期待 %v", f, got, want)
		}
	}
}

func TestAllowCommentIsReadOnlyDirectlyBeforeDeclaration(t *testing.T) {
	src := `let a = 1 // dupscan:allow intentional 前の行の末尾
func trailing() { g() }
// dupscan:allow intentional 属性の前
@MainActor
func attributed() { g() }
// dupscan:allow intentional 空行を挟む

func separated() { g() }
class C {
    /// 説明
    /**
     * dupscan:allow false-positive 複数行の形
     */
    /// 説明の続き
    var v: Int { 1 }
    // dupscan:allow
    init() {}
    // TODO: dupscan:allow intentional は文の途中なので読まない
    func plain() { g() }
}
`
	want := map[string]string{"attributed": "intentional 属性の前", "v": "false-positive 複数行の形", "init": ""}
	got := map[string]bool{}
	for _, u := range parse(t, "A.swift", src).Units {
		w, ok := want[u.Name]
		if u.HasAllow != ok || u.Allow != w {
			t.Errorf("%s: HasAllow=%v Allow=%q, 期待は %v %q", u.Name, u.HasAllow, u.Allow, ok, w)
		}
		got[u.Name] = true
	}
	for _, n := range []string{"trailing", "attributed", "separated", "v", "init", "plain"} {
		if !got[n] {
			t.Errorf("%s が単位として出ない", n)
		}
	}
}

func TestDocCommentAndParamTypes(t *testing.T) {
	src := `struct D {
    /**
     * dupscan:allow intentional 理由
     * Makes a thing.
     * second line
     */
    func make(_ a: Int, label b: [String: Int], c: @escaping (Int) -> Void) {}
    // ふつうのコメント
    func plain() {}
    /// Doc は空行を挟むと付かない

    func separated() {}
}
`
	got := map[string]*tree.Unit{}
	for _, u := range parse(t, "D.swift", src).Units {
		got[u.Name] = u
	}
	if u := got["make"]; u.Doc != "Makes a thing." || strings.Join(u.ParamTypes, "|") != "Int|[String: Int]|@escaping (Int) -> Void" {
		t.Errorf("make: Doc=%q ParamTypes=%q", u.Doc, u.ParamTypes)
	}
	if got["plain"].Doc != "" || got["separated"].Doc != "" {
		t.Errorf("doc comment でないコメント・空行を挟んだ doc は付かない: %q %q", got["plain"].Doc, got["separated"].Doc)
	}
}

// kindOf は表から引くので、Node.Kind() と同じ名前を返すことを、本番が名前で比べる種類 (属性・where・didSet・subscript・
// テスト関数・可視性・capture・読めない構文など) を含む入力の全部の節で確かめる。
func TestKindOfMatchesKind(t *testing.T) {
	src := []byte(`import XCTest
@MainActor
public struct A<T>: Equatable where T: Hashable {
    private var v: Int { didSet { print(oldValue) } willSet { print(newValue) } }
    subscript(i: Int) -> Int { get { i } }
    init(x: Int) { self.v = x }
    deinit { }
    func f(_ x: Int, label y: Int) -> Int {
        let g = { [weak self] (z: Int) -> Int in z + 1 }
        if case .some(let w) = Optional(x) { return w }
        guard let q = Optional(y) else { return 0 }
        for i in 0..<q where i > 0 { _ = g(i) }
        switch x { case 1: break; default: break }
        return a.b?.c(x: 1, 2) { $0 } ?? 0
    }
}
protocol P { func p() }
enum E { case a, b(Int) }
final class B: XCTestCase { func testX() { XCTAssertEqual(1, 1) } }
@Test func t() {}
struct Broken {
    @@
    func broken() {}
}
`)
	p := sitter.NewParser()
	defer p.Close()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	tr := p.Parse(src, nil)
	defer tr.Close()
	seen := map[string]bool{}
	var walk func(*sitter.Node)
	walk = func(x *sitter.Node) {
		seen[x.Kind()] = true
		if got, want := kindOf(x), x.Kind(); got != want {
			t.Errorf("kindOf = %q, Kind() = %q", got, want)
		}
		for i := range x.ChildCount() {
			walk(x.Child(i))
		}
	}
	walk(tr.RootNode())
	for _, k := range []string{"attribute", "where_clause", "didset_clause", "willset_clause", "subscript_declaration", "visibility_modifier",
		"capture_list_item", "lambda_literal", "call_suffix", "value_argument_label", "navigation_suffix", "ERROR"} {
		if !seen[k] {
			t.Errorf("入力に %s の節が無い (入力の前提)", k)
		}
	}
}

// 本番のコードが種類の名前と比べる文字列 (kindOf(x) == "…"・switch kindOf(x) の case・childOfKind の引数・入れ物の表) は、
// どれも文法に在る名前でなければならない。綴りを誤った比較は黙って偽になり、テストの入力にその形が無ければ誰も気づかない。
func TestComparedKindNamesExistInGrammar(t *testing.T) {
	known := map[string]bool{}
	for _, k := range kindNames() {
		known[k] = true
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "swift.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	isKindOf := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := c.Fun.(*ast.Ident)
		return ok && id.Name == "kindOf"
	}
	lit := func(e ast.Expr) (string, bool) {
		b, ok := e.(*ast.BasicLit)
		if !ok || b.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(b.Value)
		return s, err == nil
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op == token.EQL || x.Op == token.NEQ {
				if s, ok := lit(x.Y); ok && isKindOf(x.X) {
					names = append(names, s)
				}
			}
		case *ast.SwitchStmt:
			if x.Tag != nil && isKindOf(x.Tag) {
				for _, c := range x.Body.List {
					for _, e := range c.(*ast.CaseClause).List {
						if s, ok := lit(e); ok {
							names = append(names, s)
						}
					}
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "childOfKind" && len(x.Args) == 2 {
				if s, ok := lit(x.Args[1]); ok {
					names = append(names, s)
				}
			}
		case *ast.KeyValueExpr:
			if s, ok := lit(x.Key); ok && strings.Contains(s, "_") {
				names = append(names, s)
			}
		}
		return true
	})
	if len(names) < 30 {
		t.Fatalf("比べる名前を %d しか集められない (集め方が空振りしていないか)", len(names))
	}
	for _, s := range names {
		if !known[s] {
			t.Errorf("本番のコードが比べる %q は文法に無い種類の名前", s)
		}
	}
}

// normBoth は本番の経路 (Normalize。形を作らない) と形を作る経路の両方で正規化し、ハッシュが段ごとに一致することを確かめてから、
// 形つきの結果を返す (失敗の表示に形を出すため)。テストが形つきの経路だけを通ると、本番の経路の退行が見えなくなる。
func normBoth(t *testing.T, u *tree.Unit) norm.Result {
	t.Helper()
	with, plain := norm.NormalizeWithForms(u), norm.Normalize(u)
	for _, l := range norm.Levels {
		if with.Hash[l] != plain.Hash[l] || with.L3Trivial != plain.L3Trivial || with.Nodes != plain.Nodes {
			t.Errorf("%s: 本番の経路と形を作る経路で結果が違う: %s / %s", l, plain.Hash[l], with.Hash[l])
		}
	}
	return with
}
