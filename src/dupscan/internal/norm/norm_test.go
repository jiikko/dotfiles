package norm

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"dupscan/internal/tree"
)

func name(s string) *tree.Node {
	return &tree.Node{Kind: "id", Text: s, Role: tree.RoleName, Expr: true}
}
func callee(s string) *tree.Node {
	return &tree.Node{Kind: "id", Text: s, Role: tree.RoleName, Expr: true, Keep: true}
}
func tok(s string) *tree.Node { return &tree.Node{Kind: s, Text: s, Role: tree.RoleToken} }
func str(s string) *tree.Node {
	return &tree.Node{Kind: "str", Text: s, Role: tree.RoleLiteral, LitKind: "str", Expr: true}
}
func label(s string) *tree.Node { return &tree.Node{Kind: "label", Text: s, Role: tree.RoleLabel} }
func decl(s string) *tree.Node  { return &tree.Node{Kind: "decl", Text: s, Role: tree.RoleDecl} }
func node(kind string, expr bool, cs ...*tree.Node) *tree.Node {
	return &tree.Node{Kind: kind, Expr: expr, Children: cs}
}

// call は `f(label: arg, ...)` の形。
func call(f string, args ...*tree.Node) *tree.Node {
	return node("call", true, append([]*tree.Node{callee(f)}, args...)...)
}
func arg(l string, v *tree.Node) *tree.Node { return node("arg", false, label(l), v) }

func unit(params []string, body *tree.Node) *tree.Unit {
	return &tree.Unit{Params: params, Body: node("body", false, body)}
}

func same(t *testing.T, a, b *tree.Unit, want map[Level]bool) {
	t.Helper()
	ra, rb := NormalizeWithForms(a), NormalizeWithForms(b)
	for _, u := range []*tree.Unit{a, b} {
		if p, w := Normalize(u), NormalizeWithForms(u); p.Hash[L1] != w.Hash[L1] || p.Hash[L2] != w.Hash[L2] || p.Hash[L3] != w.Hash[L3] {
			t.Errorf("本番の経路と形を作る経路でハッシュが違う: %v / %v", p.Hash, w.Hash)
		}
	}
	for _, l := range Levels {
		if got := ra.Hash[l] == rb.Hash[l]; got != want[l] {
			t.Errorf("%s: 一致=%v, 期待=%v\n a=%s\n b=%s", l, got, want[l], ra.Forms[l], rb.Forms[l])
		}
	}
}

func TestLocalNamesAreNumberedNotFlattened(t *testing.T) {
	eq := func(x, y string) *tree.Node { return node("eq", true, name(x), tok("=="), name(y)) }
	// a == a と a == b は、名前を一律の記号にすると同じになる。番号にするので違う。
	same(t, unit([]string{"a", "b"}, eq("a", "a")), unit([]string{"a", "b"}, eq("a", "b")),
		map[Level]bool{L1: false, L2: false, L3: false})
	// 局所の名前の字面だけが違うなら L1 で同じ。
	same(t, unit([]string{"a", "b"}, eq("a", "b")), unit([]string{"x", "y"}, eq("x", "y")),
		map[Level]bool{L1: true, L2: true, L3: true})
}

func TestNonLocalNamesAndTokensAreKept(t *testing.T) {
	// 局所でない名前 (プロパティ・型) と匿名トークンは字面のまま。
	same(t, unit([]string{"p"}, node("eq", true, name("p"), tok("=="), name("count"))),
		unit([]string{"p"}, node("eq", true, name("p"), tok("=="), name("size"))),
		map[Level]bool{L1: false, L2: false, L3: true})
	same(t, unit([]string{"p"}, node("eq", true, name("p"), tok("=="), name("p"))),
		unit([]string{"p"}, node("eq", true, name("p"), tok("!="), name("p"))),
		map[Level]bool{L1: false, L2: false, L3: false})
}

func TestLiteralsDifferAtL1AndMatchAtL2(t *testing.T) {
	mk := func(s string) *tree.Unit {
		return unit([]string{"p"}, call("Loc", arg("id", str(s)), arg("path", name("p"))))
	}
	same(t, mk("conn"), mk("c"), map[Level]bool{L1: false, L2: true, L3: true})
}

func TestL3FoldsValueExpressionsNotReferencingLocals(t *testing.T) {
	// 直書きの ConnectionID(rawValue: "conn") と、プロパティ connectionID は L3 で同じ記号に畳まれる。
	a := unit([]string{"path"}, call("StorageLocation",
		arg("connectionID", call("ConnectionID", arg("rawValue", str("conn")))),
		arg("path", call("ObjectPath", arg("rawValue", name("path"))))))
	b := unit([]string{"p"}, call("StorageLocation",
		arg("connectionID", name("connectionID")),
		arg("path", call("ObjectPath", arg("rawValue", name("p"))))))
	same(t, a, b, map[Level]bool{L1: false, L2: false, L3: true})
	if Normalize(a).L3Trivial {
		t.Error("引数を参照する単位が L3Trivial になった")
	}
}

func TestL3KeepsCalleeAndExpressionsReferencingLocals(t *testing.T) {
	// 呼ぶ相手が違うなら、L3 でも違う (callee を単独で畳まない)。
	same(t, unit([]string{"p"}, call("ObjectPath", arg("rawValue", name("p")))),
		unit([]string{"p"}, call("BucketPath", arg("rawValue", name("p")))),
		map[Level]bool{L1: false, L2: false, L3: false})
	// 局所の名前を参照する式は畳まない: 引数の使い方が違えば L3 でも違う。
	same(t, unit([]string{"p"}, call("f", arg("x", name("p")), arg("y", str("a")))),
		unit([]string{"p"}, call("f", arg("x", str("a")), arg("y", name("p")))),
		map[Level]bool{L1: false, L2: false, L3: false})
}

func TestL3TrivialWhenBodyReferencesNoLocals(t *testing.T) {
	u := unit(nil, call("StorageLocation", arg("id", str("conn"))))
	if !Normalize(u).L3Trivial {
		t.Error("局所の名前を参照しない単位が L3Trivial にならない")
	}
}

func TestLocalDeclarationsInBodyAreLocal(t *testing.T) {
	// let x = p; f(x) の x は局所の名前 (宣言) なので、名前が違っても L1 で同じ。
	mk := func(v string) *tree.Unit {
		return unit([]string{"p"}, node("stmts", false,
			node("let", false, decl(v), name("p")),
			call("f", arg("a", name(v)))))
	}
	same(t, mk("x"), mk("y"), map[Level]bool{L1: true, L2: true, L3: true})
}

// ハッシュは形を sha256 へ直接流して取る。形を作るかどうかで値が変わらず、形の sha256 の先頭 16 byte と一致する。
// 長い名前の長さを変えて、bufio の 4096 byte の境界をいろいろな位置で跨がせる。L3 で畳む (#F) と局所の名前の番号 ($1) も通す。
func TestStreamedHashMatchesForm(t *testing.T) {
	for n := 4000; n <= 4200; n += 7 {
		u := &tree.Unit{
			Params:     []string{"x"},
			ReturnType: "Int",
			Body: &tree.Node{Kind: "statements", Children: []*tree.Node{
				{Kind: "simple_identifier", Text: "x", Role: tree.RoleName, Expr: true},
				{Kind: "integer_literal", Text: "1", Role: tree.RoleLiteral, LitKind: "num", Expr: true},
				{Kind: "simple_identifier", Text: strings.Repeat("y", n), Role: tree.RoleName, Expr: true},
			}},
		}
		plain, with := Normalize(u), NormalizeWithForms(u)
		if plain.Forms != nil {
			t.Fatal("Normalize が形を持った")
		}
		if !strings.Contains(with.Forms[L1], "$1") || !strings.Contains(with.Forms[L3], "(#F)") {
			t.Fatalf("入力の前提: L1 に $1、L3 に (#F) が要る: %.80q / %.80q", with.Forms[L1], with.Forms[L3])
		}
		for _, l := range Levels {
			sum := sha256.Sum256([]byte(with.Forms[l]))
			if want := hex.EncodeToString(sum[:16]); plain.Hash[l] != want || with.Hash[l] != want {
				t.Errorf("名前 %d 字・%s: Normalize=%s WithForms=%s 形の sha256=%s", n, l, plain.Hash[l], with.Hash[l], want)
			}
		}
	}
}
