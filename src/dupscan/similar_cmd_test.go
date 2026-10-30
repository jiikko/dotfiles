package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const widthFixture = `package width
func spansWidth(spans []Span) int {
 w := 0
 for _, sp := range spans { w += termwidth.Of(sp.Text) }
 return w
}
func lineWidth(l Line) int {
 w := 0
 for _, sp := range l.spans { w += termwidth.Of(sp.Text) }
 return w
}
func different(spans []Span) int {
 w := 0
 for _, sp := range spans { w -= len(sp.Other) }
 return w
}
`

func TestSimilarFindsHistoricalWidthShapeWithoutChangingNear(t *testing.T) {
	root := repoWith(t, map[string]string{"width.go": widthFixture})
	target := filepath.Join(root, "width.go") + ":spansWidth"
	rc, out, errs := runReport(t, "near", "-min-nodes", "1", "-levels", "L1,L2,L3", "-format", "json", target)
	var exact struct{ Matches []json.RawMessage }
	if err := json.Unmarshal([]byte(out), &exact); err != nil || rc != 0 || len(exact.Matches) != 0 {
		t.Fatalf("exact near changed: %d %s %s %v", rc, out, errs, err)
	}
	rc, out, errs = runReport(t, "near", "-similar", "-format", "json", target)
	var s similarSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 {
		t.Fatalf("similar: %d %s %s %v", rc, out, errs, err)
	}
	if s.Total < 1 || s.Matches[0].Unit.Line != 7 || s.Matches[0].Score >= 1 || s.Matches[0].SharedWeight == 0 || s.Compared != 2 || !strings.Contains(s.Limitations, "reuse=not_proven") {
		t.Fatalf("width did not rank first with honest evidence: %+v", s)
	}
	rc, out, errs = runReport(t, "near", "-similar", "-similar-threshold", "1", "-format", "json", target)
	if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Total != 0 {
		t.Fatalf("threshold ignored: %d %s %s %v", rc, out, errs, err)
	}
}

func TestSimilarBoundsAndAncestorExclusion(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": `package a
func outer(xs []int) int { f:=func() int { n:=0;for _,x:=range xs {n+=x};return n };return f() }
func copy(xs []int) int { n:=0;for _,x:=range xs {n+=x};return n }
func again(xs []int) int { n:=0;for _,x:=range xs {n+=x};return n }
`})
	target := filepath.Join(root, "a.go") + ":outer"
	rc, out, errs := runReport(t, "near", "-similar", "-similar-threshold", "0.01", "-limit", "1", "-format", "json", target)
	var s similarSummary
	// outer の中のクロージャ (2 行目) は target の子孫なので候補にしない。copy・again は outer のクロージャと同じ本体なので候補になり、-limit 1 で 1 件だけ返す
	if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Skipped < 1 || s.Total != 2 || s.Returned != 1 || len(s.Matches) != 1 || s.Matches[0].Unit.Line == 2 {
		t.Fatalf("ancestor/limit: %d %s %s %v", rc, out, errs, err)
	}
	for _, args := range [][]string{{"-similar-threshold", "NaN"}, {"-similar-threshold", "0"}, {"-similar-threshold", "1.1"}, {"-limit", "0"}, {"-limit", "51"}, {"-format", "xcode"}} {
		flags := append([]string{"near", "-similar"}, args...)
		flags = append(flags, target)
		if rc, out, _ := runReport(t, flags...); rc != 2 || out != "" {
			t.Fatalf("invalid options accepted %v: %d %s", args, rc, out)
		}
	}
}

func TestSimilarLimitDoesNotChangeCandidateTotal(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": `package a
func a(xs []int) int { n:=0;for _,x:=range xs {n+=x};return n }
func b(ys []int) int { n:=0;for _,x:=range ys {n+=x};return n }
func c(zs []int) int { n:=0;for _,x:=range zs {n+=x};return n }
`})
	rc, out, errs := runReport(t, "near", "-similar", "-limit", "1", "-format", "json", filepath.Join(root, "a.go")+":a")
	var s similarSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Total != 2 || s.Returned != 1 || len(s.Matches) != 1 || s.Matches[0].Unit.Line != 3 {
		t.Fatalf("limit/order hid candidates: %d %s %s %v", rc, out, errs, err)
	}
}

func TestSimilarExcludesSwiftNamedNestedFunctionsBothWays(t *testing.T) {
	root := repoWith(t, map[string]string{"a.swift": `func outer() {
 func inner() { print("hello"); print("world") }
 inner()
}
func separate() { print("hello"); print("world") }
`})
	for _, name := range []string{"outer", "inner"} {
		rc, out, errs := runReport(t, "near", "-similar", "-format", "json", filepath.Join(root, "a.swift")+":"+name)
		var s similarSummary
		if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Skipped != 1 || s.Compared != 1 {
			t.Fatalf("nested unit compared (%s): %d %s %s %v", name, rc, out, errs, err)
		}
		for _, m := range s.Matches {
			if m.Unit.Line != 5 {
				t.Fatalf("ancestor/descendant leaked: %+v", m)
			}
		}
	}
}

func TestSimilarExcludesNamedFunctionInsideClosureBothWays(t *testing.T) {
	root := repoWith(t, map[string]string{"a.swift": `func outer() {
 let f = {
  func inner() { print("hello"); print("world") }
  inner()
 }
 f()
}
func separate() { print("hello"); print("world") }
`})
	for _, name := range []string{"outer", "inner"} {
		rc, out, errs := runReport(t, "near", "-similar", "-format", "json", filepath.Join(root, "a.swift")+":"+name)
		var s similarSummary
		if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Skipped != 2 || s.Compared != 1 {
			t.Fatalf("mixed nesting compared (%s): %d %s %s %v", name, rc, out, errs, err)
		}
		for _, m := range s.Matches {
			if m.Unit.Line != 8 {
				t.Fatalf("mixed ancestor leaked: %+v", m)
			}
		}
	}
}
