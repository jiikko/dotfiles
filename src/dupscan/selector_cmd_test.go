package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const selectorFixture = `package sample
func wanted(xs []string) string {
 _ = filepath.Join(xs...)
 _ = tw.Join(xs, ",")
 _ = strings.Join(xs, ","); _ = strings.Join(xs, ",")
 _ = strings.
 Join(xs, ",")
 _ = (strings.Join)(xs, ",")
 _ = get().Join(xs, ",")
 _ = items[0].Join(xs, ",")
 _ = strings.Join[int](xs, ",")
 _ = strings.Join(xs)
 // strings.Join(xs, ",")
 _ = "strings.Join(xs, separator)"
 return strings.Join(xs, ",")
}
func shadow(strings Fake, xs []string) string { return strings.Join(xs, ",") }
`

func TestSelectorFiltersSyntaxAndPreservesCallPositionsAndOwners(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a.go":         selectorFixture,
		"a_test.go":    "package sample\nfunc TestExample(t *testing.T) { strings.Join(xs, \",\") }\n",
		"a.swift":      "func swift() { strings.Join(xs, separator) }\n",
		"broken.swift": "func broken( {\n",
		"unicode.go":   "package sample\nfunc unicode() { 对象.合并(xs, \",\") }\n",
	})
	rc, out, errs := callers(t, "Join(_:_:)", "-selector", " strings .\n Join ", "-format", "json", root)
	var s struct {
		Selector    string `json:"selector"`
		Limitations string `json:"selector_limitations"`
		Matches     int    `json:"matches"`
		Entries     []struct {
			File      string
			Line, Col int
			Caller    string
			Test      bool
		}
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if rc != 0 || errs != "" || s.Selector != "strings.Join" || s.Matches != 7 {
		t.Fatalf("selector must AND with the existing argument query: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	wantLines := []int{5, 5, 6, 8, 15, 17, 2}
	for i, e := range s.Entries {
		if e.Line != wantLines[i] || strings.HasSuffix(e.File, ".swift") {
			t.Errorf("unexpected call or ordering at %d: %+v", i, e)
		}
		if i < 5 && !strings.Contains(e.Caller, "wanted(") {
			t.Errorf("lost owner: %+v", e)
		}
	}
	if s.Entries[0].Col == s.Entries[1].Col || !s.Entries[6].Test || !strings.Contains(s.Entries[6].Caller, "TestExample(") {
		t.Errorf("same-line positions/test owner lost: %+v", s.Entries)
	}
	for _, note := range []string{"Swiftは対象外", "alias", "receiver", "動的/indexed", "shadowing"} {
		if !strings.Contains(s.Limitations, note) {
			t.Errorf("missing limit %q: %q", note, s.Limitations)
		}
	}
	rc, out, errs = callers(t, "Other", "-selector", "strings.Join", "-format", "json", root)
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if rc != 0 || errs != "" || s.Matches != 0 || !strings.Contains(s.Limitations, "Swiftは対象外") {
		t.Errorf("name constraint must remain AND, with limits on zero results: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = callers(t, "合并", "-selector", "对象 . 合并", "-format", "json", root)
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	if rc != 0 || errs != "" || s.Matches != 1 || s.Selector != "对象.合并" {
		t.Errorf("Unicode selector must remain valid: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
}

func TestSelectorRejectsUnsupportedQueriesAndReportsPartialAnalysis(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": selectorFixture, "broken.go": "package sample\nfunc broken( { // Join Missing\n"})
	for _, value := range []string{"Join", "strings..Join", "get().Join", "strings.Join[int]", "strings.Join()"} {
		t.Run(value, func(t *testing.T) {
			rc, out, errs := callers(t, "Join", "-selector", value, root)
			if rc != 2 || out != "" || !strings.Contains(errs, "selector") {
				t.Errorf("invalid selector reported as absence: rc=%d stdout=%q stderr=%q", rc, out, errs)
			}
		})
	}
	for _, query := range []string{"Join", "Missing"} {
		t.Run(query, func(t *testing.T) {
			rc, out, errs := callers(t, query, "-selector", "strings.Join", "-limit", "1", root)
			if rc != 0 || !strings.Contains(errs, "解析不完全") || !strings.Contains(out, "Swiftは対象外") || !strings.Contains(out, "alias") {
				t.Errorf("limited or empty result must expose coverage: rc=%d stdout=%q stderr=%q", rc, out, errs)
			}
			if query == "Join" && (!strings.Contains(out, "… 他") || strings.Count(out, "wanted(") != 1) {
				t.Errorf("selector text must respect legacy limit: %q", out)
			}
		})
	}
	swiftOnly := repoWith(t, map[string]string{"a.swift": "func f() { strings.Join(xs, separator) }\n"})
	if rc, out, errs := callers(t, "Join", "-selector", "strings.Join", swiftOnly); rc != 2 || out != "" || !strings.Contains(errs, "Swiftは対象外") {
		t.Errorf("unsupported-only scope must not imply absence: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
}

func TestSelectorMetadataDoesNotChangeLegacyJSON(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package sample\nfunc f() { strings.Join(xs, \",\") }\n"})
	rc, out, errs := callers(t, "Join", "-format", "json", root)
	want := `{"query":"Join","matches":1,"entries":[{"file":"a.go","line":2,"col":12,"caller":"f()","test":false,"text":"func f() { strings.Join(xs, \",\") }"}]}`
	var got, expected any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(expected)
	if rc != 0 || errs != "" || string(a) != string(b) {
		t.Errorf("internal Go selector must not leak into legacy JSON: rc=%d stdout=%q stderr=%q", rc, out, errs)
	}
}
