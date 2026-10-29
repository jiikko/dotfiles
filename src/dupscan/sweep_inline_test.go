package main

import (
	"strings"
	"testing"
)

// sweep の節 5: 既存の小さい関数の本体が、別の単位の中に手で展開されている所。

const inlineHelper = `package a

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
`

// repoWith は writeFile の本体 (t.Helper() を除く 3 文) をループの中に手で書いている。
const inlineSite = `package a

import (
	"os"
	"path/filepath"
	"testing"
)

func repoWith(t *testing.T, files map[string]string) string {
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
`

func inlineSection(t *testing.T, files map[string]string) string {
	t.Helper()
	rc, out, errs := sweep(t, repoWith(t, files))
	if rc != 0 {
		t.Fatalf("rc=%d %q", rc, errs)
	}
	return sweepSection(t, out, 5)
}

func TestSweepInlinedHelperBodyInsideAnotherUnit(t *testing.T) {
	s := inlineSection(t, map[string]string{"a/helper_test.go": inlineHelper, "a/site_test.go": inlineSite})
	if !strings.Contains(s, "a/site_test.go:9 repoWith の中に writeFile (a/helper_test.go:9) の本体の文 3/4 個") {
		t.Errorf("手で展開した helper の本体を出す: %q", s)
	}
}

// 局所の名前の結び付きが違う並び (`MkdirAll` に渡すのが p でなく root) は同じ形でも別の処理なので出さない。
func TestSweepInlinedNeedsSameLocalBinding(t *testing.T) {
	site := strings.Replace(inlineSite, "os.MkdirAll(filepath.Dir(p)", "os.MkdirAll(filepath.Dir(root)", 1)
	if s := inlineSection(t, map[string]string{"a/helper_test.go": inlineHelper, "a/site_test.go": site}); !strings.Contains(s, "なし") {
		t.Errorf("局所の名前の結び付きが違えば出さない: %q", s)
	}
}

// test のファイルの helper は本番のコードから呼べない。別の dir (Go の別の package) の helper も呼べるとはみなさない。
func TestSweepInlinedOnlyWhereTheHelperIsCallable(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"test の helper を本番で展開": {"a/helper_test.go": inlineHelper, "a/site.go": inlineSite},
		"別の dir":               {"a/helper_test.go": inlineHelper, "b/site_test.go": strings.Replace(inlineSite, "package a", "package b", 1)},
	} {
		if s := inlineSection(t, files); !strings.Contains(s, "なし") {
			t.Errorf("%s: 出さない: %q", name, s)
		}
	}
}

// helper の本体の 8 割に満たない並び (後ろに大きい文がもう 1 つある helper の前半だけ) は出さない。
func TestSweepInlinedNeedsMostOfTheHelperBody(t *testing.T) {
	helper := strings.Replace(inlineHelper, "\t\tt.Fatal(err)\n\t}\n}\n", "\t\tt.Fatal(err)\n\t}\n\tif err := os.Chtimes(p, time.Unix(1, 0), time.Unix(2, 0)); err != nil {\n\t\tt.Fatalf(\"chtimes %s: %v\", filepath.Base(p), err)\n\t}\n\tif err := os.Chmod(p, 0o600); err != nil {\n\t\tt.Fatalf(\"chmod %s: %v\", filepath.Base(p), err)\n\t}\n}\n", 1)
	helper = strings.Replace(helper, "\"testing\"\n", "\"testing\"\n\t\"time\"\n", 1)
	if s := inlineSection(t, map[string]string{"a/helper_test.go": helper, "a/site_test.go": inlineSite}); !strings.Contains(s, "なし") {
		t.Errorf("helper の本体の大半を覆わない並びは出さない: %q", s)
	}
}

const inlineMethods = `package a

import "sync"

type A struct {
	mu    sync.Mutex
	stops int
}

func (a *A) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stops++
	println(a.stops)
}

type B struct {
	mu    sync.Mutex
	stops int
}

func (b *B) halt() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
	println(b.stops)
	println("halted")
}

func (a *A) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stops++
	println(a.stops)
	a.stops = 0
}
`

// メソッドの helper は同じ型のメソッドからしか呼べない。別の型の同じ名前のフィールドを触る並び (B.halt) は出さない。
func TestSweepInlinedMethodHelperOnlyInTheSameType(t *testing.T) {
	s := inlineSection(t, map[string]string{"a/a.go": inlineMethods})
	if !strings.Contains(s, "reset の中に stop") {
		t.Errorf("同じ型のメソッドの中の展開は出す: %q", s)
	}
	if strings.Contains(s, "halt の中に stop") {
		t.Errorf("別の型のメソッドの中の並びは出さない: %q", s)
	}
}

// 置き場所の単位に dupscan:allow があれば出さない。
func TestSweepInlinedSilencedByAllowOnTheSite(t *testing.T) {
	site := strings.Replace(inlineSite, "func repoWith", "// dupscan:allow intentional ループの中で書く\nfunc repoWith", 1)
	if s := inlineSection(t, map[string]string{"a/helper_test.go": inlineHelper, "a/site_test.go": site}); !strings.Contains(s, "なし") {
		t.Errorf("allow の付いた置き場所は出さない: %q", s)
	}
}

// 本体が 1 つの return の関数は、composite literal の要素を文と読まない (式の子の列は文の並びではない)。
func TestSweepInlinedDoesNotReadExpressionChildrenAsStatements(t *testing.T) {
	src := "package a\n\nimport \"strings\"\n\ntype N struct{ Kind, Text string; Expr, Keep bool }\n\n" +
		"func name(s string) *N {\n\treturn &N{Kind: strings.TrimSpace(\"id\"), Text: strings.ToLower(s), Expr: len(s) > 0}\n}\n\n" +
		"func callee(s string) *N {\n\treturn &N{Kind: strings.TrimSpace(\"id\"), Text: strings.ToLower(s), Expr: len(s) > 0, Keep: true}\n}\n"
	if s := inlineSection(t, map[string]string{"a/a.go": src}); !strings.Contains(s, "なし") {
		t.Errorf("式の子の列を文の並びとして比べない: %q", s)
	}
}

// 互いに相手の本体の大半を含む 2 つの関数は 1 行にする。
func TestSweepInlinedMutualPairIsOneLine(t *testing.T) {
	other := strings.Replace(strings.Replace(inlineHelper, "func writeFile", "func writeFileQuiet", 1), "\tt.Helper()\n", "", 1)
	other = strings.Replace(other, "package a", "package a\n\nvar _ = 0", 1)
	s := inlineSection(t, map[string]string{"a/helper_test.go": inlineHelper, "a/other_test.go": other})
	if n := strings.Count(s, "\na/"); n != 1 {
		t.Errorf("互いに含む対は 1 行 (%d 行): %q", n, s)
	}
}

const inlineSwift = `import Foundation

func save(_ data: Data, to url: URL) throws {
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try data.write(to: url, options: .atomic)
    print("saved", url.lastPathComponent)
}

func export(_ items: [String], to url: URL) throws {
    let data = Data(items.joined(separator: "\n").utf8)
    try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try data.write(to: url, options: .atomic)
    print("saved", url.lastPathComponent)
}
`

func TestSweepInlinedSwift(t *testing.T) {
	if s := inlineSection(t, map[string]string{"A.swift": inlineSwift}); !strings.Contains(s, "export の中に save") {
		t.Errorf("Swift の展開も出す: %q", s)
	}
}
