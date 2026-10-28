package swift

import (
	"strings"
	"testing"

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
