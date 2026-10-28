package swift

import (
	"strings"
	"testing"

	"dupscan/internal/tree"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"dupscan/internal/lang/langtest"
	"dupscan/internal/norm"
)

const localIsolated = `func load() -> Int {
    nonisolated(unsafe) let cache = 1
    return cache + 2
}
`

// 文法が読み違える関数の中の `nonisolated(unsafe) let` は、その関数を ERROR にせず、位置も変えない。
func TestLocalNonisolatedUnsafeDoesNotBreakTheFunction(t *testing.T) {
	r := parse(t, "Sources/X.swift", localIsolated)
	if r.HasError {
		t.Fatal("局所の nonisolated(unsafe) let で ERROR のまま")
	}
	u := unitBy(t, r, "load() -> Int")
	if u.Line != 1 {
		t.Errorf("位置が変わった: line=%d", u.Line)
	}
}

// 修飾子を落とした写しと同じ形に見える (空白に替えるので、修飾子の有無だけでは別の単位にならない)。
func TestLocalNonisolatedUnsafeHashesLikeThePlainLet(t *testing.T) {
	plain := strings.Replace(localIsolated, "nonisolated(unsafe) ", "", 1)
	a := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/A.swift", localIsolated), "load() -> Int"))
	b := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/B.swift", plain), "load() -> Int"))
	if a.Hash[norm.L1] != b.Hash[norm.L1] {
		t.Errorf("修飾子の有無で L1 が違う\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
	}
}

// ほかの ERROR は隠さない: 修飾子を空白にしても ERROR が減らなければ、元の木のまま HasError を立てる。
func TestOtherErrorsStillReportedNextToNonisolatedUnsafe(t *testing.T) {
	r := parse(t, "Sources/X.swift", localIsolated+"func broken(\n")
	if !r.HasError {
		t.Error("別の構文エラーまで隠している")
	}
}

// ERROR の無いファイルは読み直さない (修飾子を含んでいても最初の木を使う)。
func TestMemberNonisolatedUnsafeIsLeftAlone(t *testing.T) {
	src := "final class C {\n    nonisolated(unsafe) static var shared = 1\n    func f() -> Int { shared }\n}\n"
	r := parse(t, "Sources/X.swift", src)
	if r.HasError {
		t.Fatalf("メンバーの nonisolated(unsafe) は元から読める: %+v", r)
	}
}

// 置き換えは ERROR 節の範囲の中だけ。正常に読めている箇所 (関数の中の型のメンバーの修飾子) は、局所の ERROR が同じファイルにあっても残る。
func TestNonisolatedUnsafeOutsideTheErrorSpanIsKept(t *testing.T) {
	src := "func f() -> Int {\n    class C { nonisolated(unsafe) static var shared = 1 }\n    return C.shared + 2\n}\n" + strings.Replace(localIsolated, "load", "g", 1)
	plain := "func f() -> Int {\n    class C { static var shared = 1 }\n    return C.shared + 2\n}\n"
	a := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/A.swift", src), "f() -> Int"))
	b := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/B.swift", plain), "f() -> Int"))
	if a.Hash[norm.L1] == b.Hash[norm.L1] {
		t.Errorf("ERROR の外の修飾子まで消えて、未変更の f が修飾子なしの写しと同じ形になった\n%s", a.Forms[norm.L1])
	}
}

// 文字列リテラルの中の `\nonisolated(unsafe)` (エスケープの n から一致する) は、別の関数の ERROR が減るからと壊さない。
func TestNonisolatedUnsafeInsideStringLiteralIsNotBlanked(t *testing.T) {
	src := "func text() -> String { \"\\nonisolated(unsafe)\" }\n" + strings.Replace(localIsolated, "load", "a", 1) + strings.Replace(localIsolated, "load", "b", 1)
	r := parse(t, "Sources/X.swift", src)
	if r.HasError {
		t.Fatal("文字列を壊して ERROR が出た、または局所の修飾子が直らない")
	}
	unitBy(t, r, "text() -> String")
}

// 空のタプルを渡す呼び出し (`continuation.yield(())`) は、その関数を ERROR にせず、同じファイルの他の関数も読める。
func TestEmptyTupleArgumentDoesNotBreakTheFunction(t *testing.T) {
	src := "final class A {\n    func end(c: AsyncStream<Void>.Continuation) {\n        c.yield(())\n        c.finish()\n    }\n    func other() -> Int { 1 }\n}\n"
	r := parse(t, "Sources/X.swift", src)
	if r.HasError {
		t.Fatal("空のタプルの引数で ERROR のまま")
	}
	unitBy(t, r, "end(c: AsyncStream<Void>.Continuation)")
}

// 呼び出しの引数でない `(())` (別の関数の型) は、同じファイルに `f(())` の読み直しがあっても書き換えない (その関数のハッシュが変わらない)。
func TestEmptyTupleTypeInAnotherFunctionIsKept(t *testing.T) {
	alone := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/A.swift", "func b(){let x:(())}\n"), "b()"))
	with := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/B.swift", "func b(){let x:(())}\nfunc a(){f(())}\n"), "b()"))
	if alone.Hash[norm.L1] != with.Hash[norm.L1] {
		t.Errorf("隣の f(()) の読み直しで、別の関数の型 (()) まで書き換えた\n%s\n%s", alone.Forms[norm.L1], with.Forms[norm.L1])
	}
}

// 壊れた宣言 `func broken(()) {}` の構文エラーは、隣の `f(())` の読み直しで隠さない。
func TestEmptyTupleRepairDoesNotHideOtherErrors(t *testing.T) {
	if r := parse(t, "Sources/X.swift", "func f(){f(())}\nfunc broken(()) {}\n"); !r.HasError {
		t.Error("別の構文エラー (broken の引数宣言) を隠した")
	}
}

// 引数でない `(())` (代入の右辺) は書き換えない: 同じファイルの `g(())` を直しても、読めない関数 h の形は h だけのときと同じ。
func TestEmptyTupleOutsideCallArgumentIsKept(t *testing.T) {
	alone := parse(t, "Sources/A.swift", "func h() { let y = (()) }\n")
	with := parse(t, "Sources/B.swift", "func f() { g(()) }\nfunc h() { let y = (()) }\n")
	a, b := langtest.NormBoth(t, unitBy(t, alone, "h()")), langtest.NormBoth(t, unitBy(t, with, "h()"))
	if a.Hash[norm.L1] != b.Hash[norm.L1] {
		t.Errorf("隣の g(()) の読み直しで、引数でない (()) まで書き換えた\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
	}
}

// パターンの `case (()):` は書き換えない: 前に `g(())` が多くあっても、後ろの正常な関数 c を ERROR に飲み込ませない。
func TestEmptyTuplePatternDoesNotSwallowNextFunction(t *testing.T) {
	src := ""
	for i := range 6 {
		src += "func a" + string(rune('0'+i)) + "(){g(())}\n"
	}
	src += "switch x { case (()): break }\nfunc c() -> Int { return 1 }\n"
	unitBy(t, parse(t, "Sources/main.swift", src), "c() -> Int")
}

// 各置換で読めることと、手で書いた比較用の形との L1・行位置を固定する。
func TestMisparse958Repairs(t *testing.T) {
	for _, tt := range []struct{ name, body, plain string }{
		{"A_labeled", "g(value: ())", "g(value: [])"},
		{"A_tuple", "return (value: (), bytes: 0)", "return (value: [], bytes: 0)"},
		{"A_compare", "#expect(await task.value == ())", "#expect(await task.value == [])"},
		{"D_if", "if await box.ready { go() }", "if box.ready { go() }"},
		{"D_switch", "switch await task.value { case .ok: go() }", "switch task.value { case .ok: go() }"},
		{"D_try", "switch try #require(result) { case .ok: go() }", "switch #require(result) { case .ok: go() }"},
		{"D_where", "for _ in 0..<100 where await box.ready { go() }", "for _ in 0..<100 where box.ready { go() }"},
		{"D_binding", "if let gate = await box.gate { go() } else { stop() }", "if let gate = box.gate { go() } else { stop() }"},
		{"D_assignment", "if await box.ready { throw Stop() }\noutcome = run()", "if box.ready { throw Stop() }\noutcome = run()"},
		{"C_assignment", "var final = x\nfinal = resolution\nreturn final", "var result = x\nresult = resolution\nreturn result"},
		{"C_branch", "var final = x\nif let resolution = fetch() {\nfinal = resolution\n} else {\nfinal = flag ? x : y\n}\nreturn final", "var result = x\nif let resolution = fetch() {\nresult = resolution\n} else {\nresult = flag ? x : y\n}\nreturn result"},
		{"E_closure", "store.acquire { Issue.record(\"x\") ; return client }", "store.acquire { Issue.record(\"x\");  return client }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "func f() {\n" + tt.body + "\n}\n"
			r := parse(t, "Sources/X.swift", src)
			if r.HasError {
				t.Fatal("読み直しても ERROR")
			}
			u := unitBy(t, r, "f()")
			v := unitBy(t, parse(t, "Sources/Y.swift", "func f() {\n"+tt.plain+"\n}\n"), "f()")
			// A は [] を構文上の代理にするが字面は元の () のまま。
			// 手書き [] の L1 と同一とは主張せず、手書きの木を元の座標で歩いた参照と比べる。
			if strings.HasPrefix(tt.name, "A_") {
				v = rawUnit958(t, "func f() {\n"+tt.plain+"\n}\n", src)
			}
			a, b := langtest.NormBoth(t, u), langtest.NormBoth(t, v)
			if a.Hash[norm.L1] != b.Hash[norm.L1] {
				t.Errorf("L1 が違う\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
			}
			if u.Line != 1 || u.EndLine != strings.Count(src, "\n") {
				t.Errorf("行位置が変化: %d:%d", u.Line, u.EndLine)
			}
			if !parse(t, "Sources/X.swift", src+"func broken(\n").HasError {
				t.Error("別の構文エラーを隠した")
			}
		})
	}
}

func rawUnit958(t *testing.T, parsed, original string) *tree.Unit {
	t.Helper()
	p, err := NewParser()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	tr := p.p.Parse([]byte(parsed), nil)
	defer tr.Close()
	w := &walker{file: "Sources/X.swift", src: []byte(original), closures: map[string]int{}, seen: map[string]int{}, isUnit: map[string]bool{}}
	w.walk(tr.RootNode(), ctx{})
	for _, u := range w.units {
		if u.Signature == "f()" {
			return u
		}
	}
	t.Fatal("f がない")
	return nil
}

func TestMisparse958Exclusions(t *testing.T) {
	for _, body := range []string{
		"if try! box.ready { go() }", "if try? box.ready { go() }", "if try! await box.ready { go() }",
		"if box.await { go() }", "final class C { final func g() {} }",
		"let s = \"() await try final x ; return 日本語\"", "g(())",
	} {
		t.Run(body, func(t *testing.T) {
			src := "func f() {\n" + body + "\n}\n"
			baseline := parse(t, "Sources/A.swift", src)
			repaired := parse(t, "Sources/B.swift", src+"func repair() { g(value: ()) }\n")
			a, b := langtest.NormBoth(t, unitBy(t, baseline, "f()")), langtest.NormBoth(t, unitBy(t, repaired, "f()"))
			if a.Hash[norm.L1] != b.Hash[norm.L1] {
				t.Errorf("対象外の形が変化\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
			}
			if body != "g(())" {
				raw := langtest.NormBoth(t, rawUnit958(t, src, src))
				if raw.Hash[norm.L1] != b.Hash[norm.L1] {
					t.Error("元の木の L1 を変えた")
				}
			} else {
				plain := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/C.swift", "func f() { g() }"), "f()"))
				if plain.Hash[norm.L1] != b.Hash[norm.L1] {
					t.Error("既存の g(()) 規則より A を優先した")
				}
			}
		})
	}
}

// 座標を変えないことを、木を採るか否かとは独立に各規則で確認する。
func TestMisparse958BytePositions(t *testing.T) {
	for _, tt := range []struct {
		src    string
		repair func(*sitter.Node, []byte) bool
	}{
		{"func f() { g(value: ()) }\n", repairEmptyTuple},
		{"func f() { if await box.ready { go() } }\n", repairConditionOperators},
		{"func f() { var final = x\nfinal = y }\n", repairFinalAssignment},
		{"func f() { run { go() \t; return x } }\n", repairClosureSemicolon},
	} {
		p, err := NewParser()
		if err != nil {
			t.Fatal(err)
		}
		tr := p.p.Parse([]byte(tt.src), nil)
		fixed := []byte(tt.src)
		changed := false
		var visit func(*sitter.Node)
		visit = func(n *sitter.Node) {
			if tt.repair(n, fixed) {
				changed = true
			}
			for i := range n.ChildCount() {
				visit(n.Child(i))
			}
		}
		visit(tr.RootNode())
		tr.Close()
		p.Close()
		if !changed {
			t.Errorf("発火しない: %s", tt.src)
		}
		if len(fixed) != len(tt.src) {
			t.Fatal("長さが変化")
		}
		for i, b := range fixed {
			if (b == '\n') != (tt.src[i] == '\n') || (b != tt.src[i] && (b >= 128 || tt.src[i] >= 128)) {
				t.Errorf("座標または ASCII 外を変更: %d", i)
			}
		}
	}
}

// I5: 元の木で ERROR の無かったトップレベルの子が、新しい木で範囲・kind を変えたり ERROR を含んだりしたら、その木を採らない。
// obaket (2026-10-29) では、この判定で捨てた置き換えは 0 件だった (規則どうしの相殺への備え。採用の 1 か所で止める)。
func TestPreservesTopLevelRejectsChangedNeighbor(t *testing.T) {
	p, err := NewParser()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	tree := func(s string) *sitter.Tree {
		tr := p.p.Parse([]byte(s), nil)
		t.Cleanup(tr.Close)
		return tr
	}
	before := tree("func a() {}\nfunc b() {}\n")
	if !preservesTopLevel(before.RootNode(), tree("func a() {}\nfunc b() {}\n").RootNode()) {
		t.Error("同じ木を捨てた")
	}
	if preservesTopLevel(before.RootNode(), tree("func a() {}\nfunc b() {\n\n").RootNode()) {
		t.Error("正常だった b が ERROR になった木を採った")
	}
	if preservesTopLevel(before.RootNode(), tree("func a() {}\nlet b = 1  \n").RootNode()) {
		t.Error("正常だった b の kind が変わった木を採った")
	}
}

// D は、文として読めている if / switch / for では、エラーが条件の側にあるときだけ発火する。本体にだけエラー (直す対象の別の書き方) がある文で、
// 正しく読めている条件の try / await を消さない (直した単位の L1 は、本体の読み違えだけを手で直したコードと同じ)。
func TestConditionOperatorsOnlyWhenConditionHasError(t *testing.T) {
	for _, c := range []struct{ name, src, plain string }{
		{"if_let_try_await", "func f() async throws {\n    if let v = try await load() {\n        g(())\n    }\n}\n", "func f() async throws {\n    if let v = try await load() {\n        g()\n    }\n}\n"},
		{"if_try_A", "func f() throws {\n    if try check() {\n        g(value: ())\n    }\n}\n", "func f() throws {\n    if try check() {\n        g(value: [])\n    }\n}\n"},
		{"for_where_try", "func f() throws {\n    for x in xs where try check(x) {\n        g(())\n    }\n}\n", "func f() throws {\n    for x in xs where try check(x) {\n        g()\n    }\n}\n"},
		{"cond_closure_argument", "func f() throws {\n    if try items.contains(where: { $0 > 1 }) {\n        g(())\n    }\n}\n", "func f() throws {\n    if try items.contains(where: { $0 > 1 }) {\n        g()\n    }\n}\n"},
		{"cond_interpolation_closure", "func f() throws {\n    if try s == \"\\(xs.map { $0 })\" {\n        g(())\n    }\n}\n", "func f() throws {\n    if try s == \"\\(xs.map { $0 })\" {\n        g()\n    }\n}\n"},
		{"cond_has_other_misparse", "func f() async throws {\n    if let v = try await send(()) {\n        go()\n    }\n}\n", "func f() async throws {\n    if let v = try await send() {\n        go()\n    }\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := parse(t, "Sources/X.swift", c.src)
			if r.HasError {
				t.Fatal("本体の読み違えを直せていない")
			}
			want := unitBy(t, parse(t, "Sources/Y.swift", c.plain), "f()")
			if c.name == "if_try_A" {
				want = rawUnit958(t, c.plain, c.src)
			}
			a, b := langtest.NormBoth(t, unitBy(t, r, "f()")), langtest.NormBoth(t, want)
			if a.Hash[norm.L1] != b.Hash[norm.L1] {
				t.Errorf("条件の try / await まで消した\n%s\n%s", a.Forms[norm.L1], b.Forms[norm.L1])
			}
		})
	}
}

// 空のタプルを渡す呼び出しは、空白・改行だけが違っても同じ形 (引数の無い呼び出し) として比べる。
func TestEmptyTupleArgumentIgnoresSpacing(t *testing.T) {
	want := langtest.NormBoth(t, unitBy(t, parse(t, "Sources/W.swift", "func a(c: C) {\n    c.yield()\n}\n"), "a(c: C)")).Hash[norm.L1]
	for _, call := range []string{"c.yield(())", "c.yield( ())", "c.yield(() )", "c.yield(\n        ()\n    )", "c.yield(\n        () // done\n    )", "c.yield(/* x */ ())"} {
		r := parse(t, "Sources/X.swift", "func a(c: C) {\n    "+call+"\n}\n")
		if r.HasError {
			t.Errorf("%q が読めない", call)
			continue
		}
		if got := langtest.NormBoth(t, unitBy(t, r, "a(c: C)")).Hash[norm.L1]; got != want {
			t.Errorf("%q が引数の無い呼び出しと別の形になった", call)
		}
	}
}
