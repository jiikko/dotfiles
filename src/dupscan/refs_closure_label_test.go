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
