package main

import (
	"strings"
	"testing"
)

const closureLabelSource = `struct Footer {
    let onSaved: (Int) async -> Void
    let path: String
    var computed: () -> Void { {} }
    func run() async { await onSaved(1) }
}
struct Host {
    func body() {
        _ = Footer(onSaved: { _ in }, path: "a")
        _ = Footer(onSaved: { _ in }, path: "b")
    }
}
`

func TestRefsShowsArgumentLabelOfClosureProperty(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Footer.swift": closureLabelSource})
	rc, out, errs := refs(t, "onSaved", root)
	if rc != 0 || strings.Count(out, "[引数ラベル]") != 2 || !strings.Contains(out, "await onSaved(1)") {
		t.Errorf("関数型の stored property は、呼び出しの引数ラベルも [引数ラベル] で出す: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestRefsOmitsArgumentLabelOfOtherNames(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Footer.swift": closureLabelSource})
	// 関数型でない stored property のラベルは雑音なので出さない
	rc, out, errs := refs(t, "path", root)
	if strings.Contains(out, "[引数ラベル]") {
		t.Errorf("関数型でないプロパティの引数ラベルは出さない: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	// 定義の無い名前も同じ
	rc, out, errs = refs(t, "unknownLabel", root)
	if rc != 1 || strings.Contains(out, "[引数ラベル]") {
		t.Errorf("定義の無い名前は出さない: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	// computed property は stored ではない
	rc, out, errs = refs(t, "computed", root)
	if strings.Contains(out, "[引数ラベル]") {
		t.Errorf("computed property は出さない: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

// 関数型かどうかは型注釈の形で決める。Optional・括弧・属性つきは関数型、型の中に -> を含むだけの型と typealias は対象外。
const closureShapeSource = `struct S {
    let plain: (Int) -> Void
    let optional: ((Int) -> Void)?
    let attributed: @Sendable () async -> Void
    let dict: [String: (Int) -> Void]
    let boxed: Box<(Int) -> Int>
    let aliased: Handler
    var observed: (() -> Void)! = nil
}
protocol P {
    var requirement: (Int) -> Void { get }
}
struct Use {
    func body() {
        _ = S(plain: {}, optional: nil, attributed: {}, dict: [:], boxed: Box(), aliased: h, observed: {})
        _ = Impl(requirement: { _ in })
    }
}
`

func TestRefsClosureLabelFollowsTypeAnnotationShape(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/S.swift": closureShapeSource})
	for name, want := range map[string]bool{
		"plain": true, "optional": true, "attributed": true, "observed": true,
		"dict": false, "boxed": false, "aliased": false, "requirement": false,
	} {
		_, out, errs := refs(t, name, root)
		if got := strings.Contains(out, "[引数ラベル]"); got != want {
			t.Errorf("%s: 引数ラベルを出すか = %v, want %v: out=%q stderr=%q", name, got, want, out, errs)
		}
	}
}

// 型は解決しないので、同名の別の型の引数ラベルも出る (README の「近似」の記述を固定する)。
func TestRefsClosureLabelIncludesSameNameOfOtherType(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `struct A { let path: String }
struct B { let path: () -> String }
func use() { _ = A(path: "x"); _ = B(path: { "y" }) }
`})
	_, out, errs := refs(t, "path", root)
	if n := strings.Count(out, "[引数ラベル]"); n != 2 {
		t.Errorf("同名の別の型のラベルも出る近似: 引数ラベル %d 件 out=%q stderr=%q", n, out, errs)
	}
}

// trailing closure は引数ラベルが無いので出ない (README の限界の記述を固定する)。
func TestRefsClosureLabelOmitsTrailingClosure(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/F.swift": `struct F { let onSaved: () -> Void }
func use() { _ = F { } ; _ = F(onSaved: { }) }
`})
	_, out, _ := refs(t, "onSaved", root)
	if n := strings.Count(out, "[引数ラベル]"); n != 1 {
		t.Errorf("引数ラベルを書いた呼び出しだけ 1 件: %d out=%q", n, out)
	}
}
