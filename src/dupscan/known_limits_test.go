package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dupscan/internal/report"
)

func limitDefinitions(t *testing.T, root, query string) report.DefinitionsSummary {
	t.Helper()
	rc, out, errs := def(t, "-format", "json", query, root)
	var got report.DefinitionsSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc > 1 || errs != "" {
		t.Fatalf("def %s: rc=%d out=%s stderr=%s err=%v", query, rc, out, errs, err)
	}
	return got
}

func limitReferences(t *testing.T, root, query string) report.ReferencesSummary {
	t.Helper()
	rc, out, errs := refs(t, "-format", "json", query, root)
	var got report.ReferencesSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc > 1 || errs != "" {
		t.Fatalf("refs %s: rc=%d out=%s stderr=%s err=%v", query, rc, out, errs, err)
	}
	return got
}

func TestRefsTypeScopeOutsideUnits(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `let global: Target? = nil
struct Outer {
 let value: Target? = nil
 struct Inner {
  let value: Target? = nil
 }
 func method(_ value: Target) {}
}
extension Outer.Inner {
 var extra: Target? = nil
}
extension Target: Target {}
`})
	got := limitReferences(t, root, "Target")
	var scopes []string
	for _, e := range got.Entries {
		scopes = append(scopes, e.Enclosing)
	}
	want := []string{"", "Outer", "Outer.Inner", "Outer.method(_ value: Target)", "Outer.Inner", "", ""}
	if !reflect.DeepEqual(scopes, want) {
		t.Fatalf("enclosing=%v want=%v", scopes, want)
	}
	rc, out, errs := refs(t, "Target", root)
	if rc != 0 || strings.Count(out, "(単位の外)") != 3 || !strings.Contains(out, "Sources/A.swift:3 Outer [型の位置]") {
		t.Fatalf("text: rc=%d out=%s stderr=%s", rc, out, errs)
	}
}

func TestDefParenthesesOnlyCallables(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `struct A {
 var count: Int = 0
 func count() {}
 init() {}
 subscript() -> Int { 0 }
 deinit {}
}
enum E { case count }
struct B { typealias count = Int }
protocol P {
 func count()
 var count: Int { get }
 init()
 subscript() -> Int { get }
}
`})
	for _, tc := range []struct {
		query string
		count int
		kind  string
	}{
		{"count", 6, ""}, {"count()", 2, "func"}, {"init()", 2, "init"}, {"subscript()", 2, "property"}, {"A.count()", 1, "func"}, {"deinit()", 1, "property"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := limitDefinitions(t, root, tc.query)
			if got.Matches != tc.count {
				t.Fatalf("matches=%d want=%d: %+v", got.Matches, tc.count, got)
			}
			for _, e := range got.Entries {
				if tc.kind != "" && e.Kind != tc.kind {
					t.Errorf("kind=%s want=%s", e.Kind, tc.kind)
				}
			}
		})
	}
}

func TestDefAllPropertyBindings(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `var globalA = 0, globalB = 1
struct A {
 var x: Int = 0, y: String = "", z = 2
 let (left, right) = pair
 let (label: tl, other: tr) = pair
 let (outer: (inner /* gap */: ta, tb), tc) = nested
 let sa, sb: Double
 var ia = 0, ib: Float = 1
 func method() { var localA = 0, localB = 1 }
}
`})
	for _, tc := range []struct{ query, signature string }{
		{"globalB", "var globalB"}, {"A.x", "var x: Int"}, {"A.y", "var y: String"}, {"A.z", "var z"}, {"A.left", "let left"}, {"A.right", "let right"},
		{"A.tl", "let tl"}, {"A.tr", "let tr"}, {"A.ta", "let ta"}, {"A.tb", "let tb"}, {"A.tc", "let tc"},
		{"A.sa", "let sa: Double"}, {"A.sb", "let sb: Double"}, {"A.ia", "var ia"}, {"A.ib", "var ib: Float"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := limitDefinitions(t, root, tc.query)
			if got.Matches != 1 || got.Entries[0].Signature != tc.signature {
				t.Fatalf("got=%+v want signature=%s", got, tc.signature)
			}
		})
	}
	for _, name := range []string{"localB", "label", "other", "outer", "inner"} {
		if got := limitDefinitions(t, root, name); got.Matches != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	for _, name := range []string{"globalB", "y", "z", "left", "right"} {
		if got := limitReferences(t, root, name); got.Matches != 0 {
			t.Errorf("declaration %s counted: %+v", name, got)
		}
	}
	if got := limitReferences(t, root, "String"); got.Matches != 1 {
		t.Fatalf("annotation lost: %+v", got)
	}
}

func TestDefMultilineEnumCaseRange(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `enum E {
 case foo(
  a: Int
 ), bar(
  b: String
 )
 case last
 case comma(
  c: Int = 0
 )
 , next
 case raw = 1
 /* gap */
 , after = 2
}
`})
	for _, tc := range []struct {
		name        string
		first, last int
	}{{"foo", 2, 4}, {"bar", 4, 6}, {"last", 7, 7}, {"comma", 8, 10}, {"next", 11, 11}, {"raw", 12, 12}, {"after", 14, 14}} {
		t.Run(tc.name, func(t *testing.T) {
			got := limitDefinitions(t, root, tc.name)
			if got.Matches != 1 {
				t.Fatalf("%+v", got)
			}
			e := got.Entries[0]
			if e.Line != tc.first || e.EndLine != tc.last || len(e.Source) != tc.last-tc.first+1 {
				t.Fatalf("range: %+v", e)
			}
		})
	}
	rc, out, errs := def(t, "foo", root)
	if rc != 0 || !strings.Contains(out, "Sources/A.swift:2-4") || !strings.Contains(out, "  a: Int\n ), bar(") {
		t.Fatalf("text: rc=%d out=%s stderr=%s", rc, out, errs)
	}
}

func TestRefsOptionalBindingShorthand(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `func f() {
 if let foo {}
 if let foo = other {}
 if let foo, let bar = foo {}
 guard var foo else { return }
 while let foo {}
 if let foo = other, let foo {}
 if case let .some(foo) = other {}
 if let foo: Int = other {}
 if case let foo? = other {}
 if let foo /* gap */ {}
 guard let foo /* gap */ else { return }
 if let /* gap */ foo {}
}
`})
	got := limitReferences(t, root, "foo")
	var lines []int
	for _, e := range got.Entries {
		lines = append(lines, e.Line)
		if e.Kind != "expression_position" {
			t.Errorf("kind: %+v", e)
		}
	}
	if want := []int{2, 4, 4, 5, 6, 7, 11, 12, 13}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines=%v want=%v: %+v", lines, want, got)
	}
	rc, out, errs := refs(t, "foo", root)
	if rc != 0 || strings.Count(out, "[式の位置]") != 9 {
		t.Fatalf("text: rc=%d out=%s stderr=%s", rc, out, errs)
	}
}

func TestRefsPackDeclarationExcludedUsesKept(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": `func f<each T: P>(
 _ values: repeat each T
) -> (repeat each T) { (repeat each values) }
struct Box<each U> {
 let values: (repeat each U)
}
`})
	for _, tc := range []struct {
		name  string
		lines []int
	}{{"T", []int{2, 3}}, {"U", []int{5}}, {"P", []int{1}}} {
		got := limitReferences(t, root, tc.name)
		var lines []int
		for _, e := range got.Entries {
			lines = append(lines, e.Line)
			if e.Kind != "type_position" {
				t.Errorf("kind: %+v", e)
			}
		}
		if !reflect.DeepEqual(lines, tc.lines) {
			t.Errorf("%s lines=%v want=%v", tc.name, lines, tc.lines)
		}
	}
}
