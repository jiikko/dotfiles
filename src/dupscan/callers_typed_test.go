package main

import (
	"encoding/json"
	"strings"
	"testing"

	"dupscan/internal/typed"
)

// 型で鍵を確定する callers -typed の fixture。受け手の型が違う同名のメソッド・interface を通した呼び出し・関数値・埋め込みで昇格したメソッド・
// ジェネリクス・テストのファイルの呼び出し・build tag で外れるファイル・型の検査が通らないパッケージを 1 つの module に置く。
var typedFixture = map[string]string{
	"go.mod": "module fix\n\ngo 1.26\n",
	"a/a.go": `package a

type A struct{}

func (A) Save() {}

type B struct{}

func (*B) Save() {}

type Saver interface{ Save() }

type Base struct{}

func (Base) Load() {}

type Wrap struct{ Base }

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

func Use() {
	var a A
	a.Save()
	b := &B{}
	b.Save()
	var s Saver = a
	s.Save()
	f := a.Save
	f()
	g := s.Save
	g()
	w := Wrap{}
	w.Load()
	x := Box[int]{}
	_ = x.Get()
}
`,
	"a/a_test.go":  "package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { A{}.Save() }\n",
	"tagged/t.go":  "//go:build never\n\npackage tagged\n\nimport \"fix/a\"\n\nfunc g() { a.A{}.Save() }\n",
	"a/x_never.go": "//go:build never\n\npackage a\n\nfunc k() { A{}.Save() }\n",
	"broken/b.go":  "package broken\n\nfunc H() { undefinedThing() }\n",
	"user/u.go":    "package user\n\nimport (\n\t\"fix/a\"\n\t\"fix/broken\"\n)\n\nfunc U() { a.A{}.Save(); broken.H() }\n",
}

func typedCallers(t *testing.T, root, query string) typed.Result {
	t.Helper()
	rc, out, errs := callers(t, "-typed", "-format", "json", query, root)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errs)
	}
	var res typed.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v: %q", err, out)
	}
	return res
}

func keysOf(cs []typed.Call, kind string) map[string]int {
	m := map[string]int{}
	for _, c := range cs {
		if kind == "" || c.Kind == kind {
			m[c.Key]++
		}
	}
	return m
}

// 受け手の型が違う同名のメソッドは鍵ごとに分かれ、テストのファイルの呼び出しも数える (test の変種で同じファイルを 2 回読んでも 1 回)。
func TestTypedCallersSplitsSameNameMethodsByReceiver(t *testing.T) {
	root := repoWith(t, typedFixture)
	res := typedCallers(t, root, "Save")
	got := keysOf(res.Confirmed, "")
	if got["fix/a.A.Save"] != 3 || got["fix/a.B.Save"] != 1 {
		t.Errorf("A.Save は Use・テスト・別の package (依存が壊れていても走査する) の 3 件、B.Save は 1 件のはず: %v", got)
	}
	if _, ok := got["fix/a.Saver.Save"]; ok {
		t.Errorf("interface を通した呼び出しを確定に数えている: %v", got)
	}
}

// interface を通した呼び出しと、関数値として渡した所は、確定に数えず「確定できない」に出す。
func TestTypedCallersReportsInterfaceAndValueAsUnknown(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "Save")
	if keysOf(res.Unknown, "interface")["fix/a.Saver.Save"] != 1 {
		t.Errorf("interface を通した呼び出しが確定できないに出ていない: %+v", res.Unknown)
	}
	if keysOf(res.Unknown, "value")["fix/a.A.Save"] != 1 {
		t.Errorf("method value (f := a.Save) が確定できないに出ていない: %+v", res.Unknown)
	}
}

// 埋め込みで昇格したメソッドは宣言した型の鍵、ジェネリクスはインスタンス化の前の型の鍵になる。package 名から書く問い合わせも当たる。
func TestTypedCallersKeysPromotedAndGenericMethods(t *testing.T) {
	root := repoWith(t, typedFixture)
	if got := keysOf(typedCallers(t, root, "Base.Load").Confirmed, ""); got["fix/a.Base.Load"] != 1 {
		t.Errorf("昇格したメソッド (w.Load) を Base.Load に数えていない: %v", got)
	}
	if got := keysOf(typedCallers(t, root, "a.Box.Get").Confirmed, ""); got["fix/a.Box.Get"] != 1 {
		t.Errorf("ジェネリクスのメソッド (x.Get) を Box.Get に数えていない: %v", got)
	}
}

// 型の検査が通らないパッケージと、この構成で読んでいないファイルは、正常な 0 件にせず名指しする。
func TestTypedCallersNamesUntypedPackagesAndUnreadFiles(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "Save")
	found := false
	for _, p := range res.Untyped {
		found = found || strings.HasSuffix(p.Path, "broken")
	}
	if !found {
		t.Errorf("型の検査が通らないパッケージを名指ししていない: %+v", res.Untyped)
	}
	if strings.Join(res.Unread, ",") != "a/x_never.go,tagged/t.go" {
		t.Errorf("build tag で外れるファイル (dir ごと・読み込んだ package の中の 1 本) を読んでいないと言っていない: %v", res.Unread)
	}
}

func TestTypedCallersRejectsSelector(t *testing.T) {
	rc, _, errs := callers(t, "-typed", "-selector", "strings.Join", "Join", repoWith(t, typedFixture))
	if rc != 2 || !strings.Contains(errs, "-typed と -selector は一緒に使えない") {
		t.Errorf("rc=%d stderr=%q", rc, errs)
	}
}

// 修飾した問い合わせ (A.Save) でも、名前が同じ interface のメソッドの呼び出しは「確定できない」に出す (鍵が違っても消さない)。
func TestTypedCallersQualifiedQueryKeepsInterfaceCallsAsUnknown(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "A.Save")
	if keysOf(res.Unknown, "interface")["fix/a.Saver.Save"] != 1 {
		t.Errorf("A.Save で引くと interface を通した呼び出しが消える: %+v", res.Unknown)
	}
	if keysOf(res.Confirmed, "")["fix/a.Saver.Save"] != 0 || keysOf(res.Confirmed, "")["fix/a.B.Save"] != 0 {
		t.Errorf("A.Save の問い合わせで、別の鍵を確定に数えている: %+v", res.Confirmed)
	}
}

// 依存のパッケージだけが壊れているパッケージは走査し (呼び出しを確定に数える)、結果が欠けうると注記する。
func TestTypedCallersScansPackagesWithBrokenDependency(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "A.Save")
	found := false
	for _, c := range res.Confirmed {
		found = found || c.File == "user/u.go"
	}
	if !found {
		t.Errorf("依存が壊れているだけのパッケージの呼び出しを落としている: %+v", res.Confirmed)
	}
	noted := false
	for _, p := range res.Untyped {
		noted = noted || (strings.HasSuffix(p.Path, "user") && strings.Contains(p.Reason, "依存のパッケージ"))
	}
	if !noted {
		t.Errorf("依存が壊れていることを注記していない: %+v", res.Untyped)
	}
}

// 存在しない型の問い合わせでは、名前が同じ interface のメソッドで広げても「当たった鍵」にしない。
func TestTypedCallersTypoQueryHasNoKeys(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "Typo.Save")
	if len(res.Keys) != 0 {
		t.Errorf("存在しない Typo.Save で鍵が当たったことになっている: %v", res.Keys)
	}
	_, out, _ := callers(t, "-typed", "Typo.Save", repoWith(t, typedFixture))
	if !strings.Contains(out, "当たる関数の呼び出し・参照は無い") {
		t.Errorf("text で当たらないと言っていない: %q", out)
	}
}

// interface のメソッドを関数値として渡した所 (g := s.Save) も、修飾した問い合わせで消さずに「確定できない」に出す。
func TestTypedCallersKeepsInterfaceMethodValue(t *testing.T) {
	res := typedCallers(t, repoWith(t, typedFixture), "A.Save")
	if keysOf(res.Unknown, "value")["fix/a.Saver.Save"] != 1 {
		t.Errorf("interface の method value が消えている: %+v", res.Unknown)
	}
}

// 依存の誤りで注記したパッケージは、text では「数えていない」の見出しと分ける (実際には上に数えている)。
func TestTypedCallersTextSeparatesPartialPackages(t *testing.T) {
	_, out, _ := callers(t, "-typed", "A.Save", repoWith(t, typedFixture))
	i := strings.Index(out, "依存のパッケージの誤りで結果が欠けうるパッケージ")
	j := strings.Index(out, "🚨 型で確定できないパッケージ")
	if i < 0 || j < 0 || strings.Contains(out[j:i], "user") {
		t.Errorf("依存の誤りのパッケージを「数えていない」の見出しに並べている: %q", out)
	}
}
