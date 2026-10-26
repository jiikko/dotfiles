package golang

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dupscan/internal/lang"
	"dupscan/internal/lang/langtest"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

func parse(t *testing.T, file, src string) lang.FileResult {
	t.Helper()
	p, err := newParser(lang.Options{Calls: true})
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

func unknownKinds(n *tree.Node, out map[string]int) {
	if strings.HasPrefix(n.Kind, kindPrefix+unknownKind) {
		out[n.Kind]++
	}
	for _, c := range n.Children {
		unknownKinds(c, out)
	}
}

// go/ast に節の型が増えたとき、conv が知らない節を黙って構造だけの節にしない。全部の構文を持つ見本と、標準ライブラリの実物で確かめる。
func TestConvKnowsEveryNode(t *testing.T) {
	r := parse(t, "a.go", `package a
type T struct{ x int }
func (t *T) all(xs []int, m map[string]int, ch chan int, f func(int) int, v any) (out int, err error) {
	var a, b = 1, 2.5
	const c = 'x'
	type local struct{ y string }
	n := len(xs[1:2]) + cap(xs[:1:2]) - int(b)
	p := &T{x: n}
	q := [...]int{1, 2}
	_ = q
	p.x++
	a += f(n)
	s := []byte("s")
	_ = (*T)(nil)
	if a > 0 { out = a } else if a < 0 { out = -a } else { out = 0 }
	for i := 0; i < len(xs); i++ { continue }
	for k, v := range m { _, _ = k, v }
	switch x := a; { case x > 1: fallthrough; default: break }
	switch y := v.(type) { case int, string: _ = y; default: }
	select { case ch <- 1: case <-ch: default: }
outer:
	for range xs { break outer }
	go func() { ch <- 1 }()
	defer func(z int) {}(1)
	g := func(xs ...int) int { return len(xs) }
	_ = g(xs...)
	var l local
	_ = l.y
	_ = s
	_ = struct{}{}
	_ = interface{ M() }(nil)
	goto end
end:
	;
	return out, nil
}`)
	if len(r.Units) < 4 {
		t.Fatalf("単位が %d しかない", len(r.Units))
	}
	unknown := map[string]int{}
	for _, u := range r.Units {
		unknownKinds(u.Body, unknown)
	}
	// 実物: 標準ライブラリの大きめの package (generics・ラベル・select などを含む)
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	goroot := strings.TrimSpace(string(out))
	files := 0
	for _, dir := range []string{"go/ast", "go/types", "net/http", "encoding/json", "slices", "iter", "sync"} {
		paths, _ := filepath.Glob(filepath.Join(goroot, "src", dir, "*.go"))
		for _, p := range paths {
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			r := parse(t, filepath.Base(p), string(src))
			if r.HasError {
				t.Errorf("%s が読めない構文を含むことになっている", p)
			}
			files++
			for _, u := range r.Units {
				unknownKinds(u.Body, unknown)
			}
		}
	}
	if files < 100 {
		t.Fatalf("標準ライブラリのファイルが %d しか読めていない (GOROOT の取り違えで空振りしていないか)", files)
	}
	if len(unknown) > 0 {
		t.Errorf("conv が知らない節: %v", unknown)
	}
}

func TestRenamedLocalsMatchAtL1(t *testing.T) {
	r := parse(t, "a.go", `package a
func (s *Store) f(x int) int { y := x + 1; for _, v := range s.items { y += v }; return y * 2 }
func (m *Cache) g(a int) int { b := a + 1; for _, w := range m.items { b += w }; return b * 2 }
func (m *Cache) h(a int) int { b := a + 1; for _, w := range m.items { b += w }; return b * 3 }`)
	f, g, h := langtest.NormBoth(t, langtest.Find(t, r, "f")), langtest.NormBoth(t, langtest.Find(t, r, "g")), langtest.NormBoth(t, langtest.Find(t, r, "h"))
	if f.Hash[norm.L1] != g.Hash[norm.L1] {
		t.Errorf("receiver と局所の名前だけが違うのに L1 で一致しない\n%s\n%s", f.Forms[norm.L1], g.Forms[norm.L1])
	}
	if g.Hash[norm.L1] == h.Hash[norm.L1] || g.Hash[norm.L2] != h.Hash[norm.L2] {
		t.Error("リテラルだけが違う単位は L1 で違い L2 で一致するはず")
	}
}

// メンバーの名前と構造体のリテラルの field 名は、局所の名前と同じ字面でも番号にしない
// (番号にすると、別の field に入れる処理が局所の名前を揃えただけで同じになる)。
func TestMembersAndFieldNamesStayLiteral(t *testing.T) {
	r := parse(t, "a.go", `package a
func (s *S) f(name string) { s.name = name; s.save(name); s.log.Print(name) }
func (s *S) g(title string) { s.title = title; s.save(title); s.log.Print(title) }
func h(name string) Item { return Item{name: name, size: 1, kind: kind(name)} }
func k(title string) Item { return Item{title: title, size: 1, kind: kind(title)} }`)
	if langtest.NormBoth(t, langtest.Find(t, r, "f")).Hash[norm.L1] == langtest.NormBoth(t, langtest.Find(t, r, "g")).Hash[norm.L1] {
		t.Error("代入先のメンバーの違う単位が L1 で一致する")
	}
	if langtest.NormBoth(t, langtest.Find(t, r, "h")).Hash[norm.L1] == langtest.NormBoth(t, langtest.Find(t, r, "k")).Hash[norm.L1] {
		t.Error("field 名の違う構造体のリテラルが L1 で一致する")
	}
}

func TestSliceBoundsKeepTheirPosition(t *testing.T) {
	r := parse(t, "a.go", `package a
func f(xs []int, n int) []int { if n > len(xs) { n = len(xs) }; out := xs[:n]; sortInts(out); return out }
func g(xs []int, n int) []int { if n > len(xs) { n = len(xs) }; out := xs[n:]; sortInts(out); return out }`)
	if langtest.NormBoth(t, langtest.Find(t, r, "f")).Hash[norm.L1] == langtest.NormBoth(t, langtest.Find(t, r, "g")).Hash[norm.L1] {
		t.Error("`xs[:n]` と `xs[n:]` が同じになった")
	}
}

// 呼び出しは処理として扱い、L3 でも畳まない。組み込みの変換・長さは値なので畳む。
func TestCallsAreNotFoldedAtL3(t *testing.T) {
	r := parse(t, "a.go", `package a
func f(id string) error { if id == "" { return nil }; status = fetchA(); return save(id, status) }
func g(id string) error { if id == "" { return nil }; status = fetchB(); return save(id, status) }
func h(id string) error { if id == "" { return nil }; status = string(raw); return save(id, status) }
func k(id string) error { if id == "" { return nil }; status = int(other); return save(id, status) }`)
	f, g := langtest.NormBoth(t, langtest.Find(t, r, "f")), langtest.NormBoth(t, langtest.Find(t, r, "g"))
	if f.Hash[norm.L3] == g.Hash[norm.L3] {
		t.Errorf("呼ぶ関数の違う代入が L3 で一致する\n%s", f.Forms[norm.L3])
	}
	h, k := langtest.NormBoth(t, langtest.Find(t, r, "h")), langtest.NormBoth(t, langtest.Find(t, r, "k"))
	if h.Hash[norm.L3] != k.Hash[norm.L3] {
		t.Errorf("組み込みの変換 (値) だけが違う単位が L3 で一致しない\n%s\n%s", h.Forms[norm.L3], k.Forms[norm.L3])
	}
	if h.Hash[norm.L1] == k.Hash[norm.L1] {
		t.Error("変換する値の違う単位が L1 で一致する")
	}
}

func TestMethodsClosuresAndOwners(t *testing.T) {
	r := parse(t, "pkg/a.go", `package a
var handler = func(w int) int { return w * 2 }
func (b *Box[T]) Put(v T) { b.each(func(x T) { go func() { use(x) }() }) }
func (Box[T]) Len() int { return 0 }
func asm(x int) int
func init() { setup(1) }
func init() { setup(2) }`)
	put := langtest.Find(t, r, "Put")
	if put.Enclosing != "Box" || put.Signature != "Put(v T)" || put.Visibility != "public" || put.Kind != tree.KindFunc {
		t.Errorf("Put: encl=%q sig=%q vis=%q kind=%q", put.Enclosing, put.Signature, put.Visibility, put.Kind)
	}
	if len(put.Params) != 2 || put.Params[0] != "b" || put.Params[1] != "v" {
		t.Errorf("Put の局所の名前 (receiver と引数): %v", put.Params)
	}
	for _, u := range r.Units {
		if !strings.HasPrefix(u.Body.Kind, "go.") {
			t.Errorf("%s の本体の根の種類 %q に Go の頭が無い (Swift の単位と同じハッシュになりうる)", u.Key, u.Body.Kind)
		}
	}
	if langtest.Find(t, r, "Len").Enclosing != "Box" {
		t.Error("名前の無い receiver の型が取れない")
	}
	var closures []*tree.Unit
	inits := 0
	keys := map[string]bool{}
	for _, u := range r.Units {
		if keys[u.Key] {
			t.Errorf("鍵が重なった: %s", u.Key)
		}
		keys[u.Key] = true
		switch u.Name {
		case "{closure}":
			closures = append(closures, u)
		case "init":
			inits++
		case "asm":
			t.Error("本体の無い関数が単位になった")
		}
	}
	if inits != 2 {
		t.Errorf("init が %d", inits)
	}
	if len(closures) != 3 {
		t.Fatalf("関数リテラルが %d", len(closures))
	}
	byOwner := map[string]*tree.Unit{}
	for _, c := range closures {
		byOwner[c.OwnerName] = c
	}
	if c := byOwner["handler"]; c == nil || c.Owner != "" {
		t.Errorf("パッケージの変数の関数リテラル: %+v", byOwner)
	}
	outer, inner := byOwner["Put"], byOwner["Put.{closure}"]
	if outer == nil || inner == nil || outer.Owner != put.Key || inner.Owner != outer.Key {
		t.Errorf("入れ子の関数リテラルの囲む単位: %+v", byOwner)
	}
}

func TestTestFunctionsAreNotUnits(t *testing.T) {
	r := parse(t, "pkg/a_test.go", `package a
func TestA(t *testing.T) { run(func() { check(t) }) }
func BenchmarkB(b *testing.B) {}
func ExampleC() {}
func FuzzD(f *testing.F) {}
func (s *suite) TestMethod() { s.run() }
func (s *suite) TestWith(x int) { s.run() }
func Testable(x int) int { return x }
func Testify() {}
func Test(t *testing.T) {}
func (s *suite) BenchmarkX() {}
func newFixture(name string) int { return len(name) }`)
	names := map[string]bool{}
	for _, u := range r.Units {
		names[u.Name] = true
		if !u.Test {
			t.Errorf("%s が test のファイルなのに Test=false", u.Name)
		}
	}
	for _, n := range []string{"TestA", "BenchmarkB", "ExampleC", "FuzzD", "TestMethod", "Test", "{closure}"} {
		if names[n] {
			t.Errorf("テスト関数かその中の関数リテラル %s が単位になった", n)
		}
	}
	// go test が呼ばない形 (後ろが小文字・引数の数が違う) はヘルパー
	for _, n := range []string{"newFixture", "TestWith", "Testable", "Testify", "BenchmarkX"} {
		if !names[n] {
			t.Errorf("ヘルパー %s が単位にならない: %v", n, names)
		}
	}
	// test でないファイルの TestXxx はテスト関数ではない
	if len(parse(t, "pkg/a.go", "package a\nfunc TestLike() { run() }").Units) != 1 {
		t.Error("test でないファイルの TestLike が単位にならない")
	}
}

func TestDelegation(t *testing.T) {
	r := parse(t, "a.go", `package a
func a1(id string) error { return s.inner.Delete(ctx, id, -1, "x", time.Now()) }
func a2(id string) { s.log.Print(id) }
func b1(id string) error { return s.inner.Delete(ctx, id+"x") }
func b2(id string) error { s.log.Print(id); return nil }
func b3(id string) error { return s.inner.Delete(Opts{ID: id}) }
func b4(id string) (int, error) { return s.inner.Count(id), nil }
func b5(id string) error { return s.inner.Delete(func() {}) }`)
	for _, n := range []string{"a1", "a2"} {
		if !langtest.Find(t, r, n).Delegation {
			t.Errorf("%s が委譲にならない", n)
		}
	}
	for _, n := range []string{"b1", "b2", "b3", "b4", "b5"} {
		if langtest.Find(t, r, n).Delegation {
			t.Errorf("%s が委譲になった", n)
		}
	}
}

func TestAllowAndDoc(t *testing.T) {
	r := parse(t, "a.go", `package a

// Load は設定を読む。
//
// dupscan:allow intentional 写しの理由
func Load() {}

// TODO: dupscan:allow は使わない
func Other() {}

// dupscan:allow
func Empty() {}`)
	l := langtest.Find(t, r, "Load")
	if !l.HasAllow || l.Allow != "intentional 写しの理由" || l.AllowLine != 5 || l.Doc != "Load は設定を読む。" {
		t.Errorf("Load: allow=%q line=%d has=%v doc=%q", l.Allow, l.AllowLine, l.HasAllow, l.Doc)
	}
	if o := langtest.Find(t, r, "Other"); o.HasAllow {
		t.Error("文の途中の印を許可にした")
	}
	if e := langtest.Find(t, r, "Empty"); !e.HasAllow || e.Doc != "" {
		t.Errorf("後ろが空の印: has=%v doc=%q", e.HasAllow, e.Doc)
	}
}

func TestSyntaxErrorMarksOnlyTheBrokenUnit(t *testing.T) {
	r := parse(t, "a.go", `package a
func ok(x int) int { return x + 1 }
func bad(x int) int { return x + }
`)
	if !r.HasError {
		t.Fatal("読めない構文があるのに HasError=false")
	}
	if langtest.Find(t, r, "ok").Unreadable || !langtest.Find(t, r, "bad").Unreadable {
		t.Errorf("Unreadable: ok=%v bad=%v", langtest.Find(t, r, "ok").Unreadable, langtest.Find(t, r, "bad").Unreadable)
	}
}

func TestGeneratedFileHasNoUnits(t *testing.T) {
	r := parse(t, "a.pb.go", "// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage a\nfunc f() { g() }\n")
	if len(r.Units) != 0 || len(r.Calls) != 0 {
		t.Errorf("生成されたファイルから単位 %d・呼び出し %d", len(r.Units), len(r.Calls))
	}
}

func TestCallsCarryNameAndArgCount(t *testing.T) {
	r := parse(t, "a.go", "package a\nfunc f() {\n\ts.Save(a, b)\n\tdo[int](1)\n\t_ = []byte(x)\n}\n")
	got := map[string]int{}
	for _, c := range r.Calls {
		got[c.Name] = len(c.Labels)
		if c.Name == "Save" && (c.Line != 3 || c.Text != "s.Save(a, b)") {
			t.Errorf("Save の場所: %+v", c)
		}
	}
	if got["Save"] != 2 || got["do"] != 1 || len(got) != 2 {
		t.Errorf("呼び出し: %v", got)
	}
	if u := langtest.Find(t, r, "f"); len(u.Labels) != 0 {
		t.Errorf("引数の無い関数のラベル: %v", u.Labels)
	}
}

// 戻り値の名前は本体の中で番号にするので、署名の字面としてハッシュに入れない (入れると名前を変えた写しを L1 で見逃す)。
func TestNamedResultsAreLocals(t *testing.T) {
	r := parse(t, "a.go", `package a
func total(xs []int, limit int) (sum int, err error) { for _, x := range xs { if x > limit { return 0, errTooBig }; sum += x * 2 }; return sum, nil }
func total2(ys []int, max int) (acc int, e error) { for _, y := range ys { if y > max { return 0, errTooBig }; acc += y * 2 }; return acc, nil }`)
	a, b := langtest.Find(t, r, "total"), langtest.Find(t, r, "total2")
	if a.ReturnType != "(int, error)" || a.Signature != "total(xs []int, limit int) (sum int, err error)" {
		t.Errorf("ret=%q sig=%q", a.ReturnType, a.Signature)
	}
	if ha, hb := langtest.NormBoth(t, a), langtest.NormBoth(t, b); ha.Hash[norm.L1] != hb.Hash[norm.L1] {
		t.Errorf("戻り値の名前と局所の名前だけが違う写しが L1 で一致しない\n%s\n%s", ha.Forms[norm.L1], hb.Forms[norm.L1])
	}
}

// 型の字面に構造体の tag を残す (tag は読み書きする field を決める)。チャネルの送受信と型の別名も区別する。
func TestTypeTagsChannelsAndAliasesDiffer(t *testing.T) {
	r := parse(t, "a.go", "package a\n"+
		"func u1(b []byte) string { var r struct{ ID string `json:\"user_id\"` }; _ = json.Unmarshal(b, &r); return r.ID }\n"+
		"func u2(b []byte) string { var r struct{ ID string `json:\"group_id\"` }; _ = json.Unmarshal(b, &r); return r.ID }\n"+
		"func s1(v int) { if v > 0 { chA <- v }; log(v) }\n"+
		"func s2(v int) { if v > 0 { chB <- v }; log(v) }\n"+
		"func r1(v int) int { x := <-chA; return x + v }\n"+
		"func r2(v int) int { x := <-chB; return x + v }\n"+
		"func t1(v int) int { type T = int; return int(T(v)) + v }\n"+
		"func t2(v int) int { type T int; return int(T(v)) + v }\n"+
		"func p1(b []byte) (r struct{ ID string `json:\"user_id\"` }) { _ = json.Unmarshal(b, &r); return }\n"+
		"func p2(b []byte) (r struct{ ID string `json:\"group_id\"` }) { _ = json.Unmarshal(b, &r); return }\n"+
		"func m1(f func(a int, b string) error) error { var g func(a int, b string) error = f; return g(1, \"x\") }\n"+
		"func m2(f func(a int, b string) error) error { var g func(\n\ta int,\n\tb string,\n) error = f; return g(1, \"x\") }\n"+
		"func f1(b []byte) int { var r struct {\n\tA string `json:\"secret\"`\n\tB string\n}; _ = json.Unmarshal(b, &r); return len(r.A) + len(r.B) }\n"+
		"func f2(b []byte) int { var r struct {\n\tA string\n\tB string `json:\"secret\"`\n}; _ = json.Unmarshal(b, &r); return len(r.A) + len(r.B) }\n"+
		"func g1(v int) int { var a [len(T{1, 2})]struct{ X int }; return len(a) + v }\n"+
		"func g2(v int) int { var a [len(T{1, 2, 3})]struct{ X int }; return len(a) + v }\n"+
		"func w1(b []byte) string { var r struct{ ID string `json:\"a  b\"` }; _ = json.Unmarshal(b, &r); return r.ID }\n"+
		"func w2(b []byte) string { var r struct{ ID string `json:\"a b\"` }; _ = json.Unmarshal(b, &r); return r.ID }\n"+
		"func c1(v int) int { tests := []struct {\n\tname string // case name\n\t// expected\n\twant int\n}{{\"a\", 1}}; return len(tests) + v }\n"+
		"func c2(v int) int { tests := []struct{ name string; want int }{{\"a\", 1}}; return len(tests) + v }\n"+
		"func d1(v int) int { var x [unsafe.Sizeof(func() int {\n\tvar k = 1 // c\n\treturn k\n}())]int; return len(x) + v }\n"+
		"func d2(v int) int { var x [unsafe.Sizeof(func() int {\n\tvar k = 1\n\treturn k\n}())]int; return len(x) + v }\n")
	h := func(n string, l norm.Level) string { return langtest.NormBoth(t, langtest.Find(t, r, n)).Hash[l] }
	if h("u1", norm.L1) == h("u2", norm.L1) {
		t.Error("tag だけが違う構造体が L1 で一致する")
	}
	for _, p := range [][2]string{{"s1", "s2"}, {"r1", "r2"}} {
		if h(p[0], norm.L3) == h(p[1], norm.L3) {
			t.Errorf("チャネルの違う送受信 %s / %s が L3 で一致する", p[0], p[1])
		}
	}
	if h("t1", norm.L1) == h("t2", norm.L1) {
		t.Error("型の別名と新しい型の宣言が L1 で一致する")
	}
	if h("p1", norm.L1) == h("p2", norm.L1) {
		t.Error("戻り値の型の tag だけが違う関数が L1 で一致する")
	}
	if h("c1", norm.L1) != h("c2", norm.L1) || h("d1", norm.L1) != h("d2", norm.L1) {
		t.Error("型の中のコメントだけが違う写しが L1 で一致しない")
	}
	if h("m1", norm.L1) != h("m2", norm.L1) {
		t.Error("型を複数行に書いただけの写しが L1 で一致しない (型の字面が改行の位置に依る)")
	}
	for _, p := range [][2]string{{"f1", "f2"}, {"g1", "g2"}, {"w1", "w2"}} {
		if h(p[0], norm.L1) == h(p[1], norm.L1) {
			t.Errorf("型の違う %s / %s が L1 で一致する (tag の付く field・型の中の式・tag の中の空白)", p[0], p[1])
		}
	}
}

// `//line` の指示があっても、単位と呼び出しの行はこのファイルの行を指す。
func TestLineDirectiveDoesNotMoveUnits(t *testing.T) {
	r := parse(t, "a.go", "package a\n\n//line other.y:500\nfunc f() {\n\tg()\n}\n")
	if u := langtest.Find(t, r, "f"); u.Line != 4 || u.EndLine != 6 {
		t.Errorf("f の行: %d-%d", u.Line, u.EndLine)
	}
	if len(r.Calls) != 1 || r.Calls[0].Line != 5 || r.Calls[0].Text != "g()" {
		t.Errorf("呼び出し: %+v", r.Calls)
	}
}

// 閉じないまま EOF で終わる関数は、誤りの位置が関数の範囲の後ろに出ても読めない単位にする。中の関数リテラルも同じ。
func TestUnclosedFunctionAtEOFIsUnreadable(t *testing.T) {
	r := parse(t, "a.go", "package a\nfunc a(x int) int { return x + 1 }\nfunc b() { run(func() { work() }); for {\n")
	if !r.HasError {
		t.Fatal("HasError=false")
	}
	for _, u := range r.Units {
		if want := u.Name != "a"; u.Unreadable != want {
			t.Errorf("%s (%s): Unreadable=%v", u.Name, u.Key, u.Unreadable)
		}
	}
	if len(r.Units) < 2 {
		t.Fatalf("単位が %d", len(r.Units))
	}
	// パッケージの変数の初期値の関数リテラルも、閉じないまま EOF で終われば中の関数リテラルごと読めない
	r = parse(t, "b.go", "package a\nvar h = func() { run(func() { work() }); for {\n")
	if len(r.Units) != 2 {
		t.Fatalf("関数リテラルが %d", len(r.Units))
	}
	for _, u := range r.Units {
		if !u.Unreadable {
			t.Errorf("%s が読める単位になった", u.Key)
		}
	}
}

func TestCallOwnersLeaveCloneUnitsUnchanged(t *testing.T) {
	src := []byte(`package fixture
func helper() { target() }
func TestA(t *testing.T) { target(); use(func() { target() }) }
func BenchmarkB(b *testing.B) { target() }
func FuzzC(f *testing.F) { target() }
func ExampleD() { target() }
func (s *suite) TestMethod() { target() }
`)
	with, err := (&Parser{opt: lang.Options{Calls: true}}).Parse("owner_test.go", src)
	if err != nil {
		t.Fatal(err)
	}
	without, err := (&Parser{}).Parse("owner_test.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(with.Units) != 1 || with.Units[0].Name != "helper" || !reflect.DeepEqual(with.Units, without.Units) || len(without.Calls) != 0 {
		t.Fatalf("call metadata must not admit tests/closures into clone units: with=%+v without=%+v", with, without)
	}
	for _, c := range with.Calls {
		if c.Owner == nil || c.Owner.Signature == "" || !c.Owner.Test || c.Owner.Unreadable || c.Owner.Line > c.Line || c.Owner.EndLine < c.Line {
			t.Errorf("call must retain a readable named owner and range: %+v owner=%+v", c, c.Owner)
		}
	}
}
