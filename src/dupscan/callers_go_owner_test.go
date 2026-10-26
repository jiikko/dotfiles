package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dupscan/internal/report"
)

func TestGoCallersNamesTestsWithoutComparingThem(t *testing.T) {
	src := `package fixture
import "testing"
var before = target()
func helper() { target() }
func TestA(t *testing.T) { target(); target(); t.Run("nested", func(t *testing.T) { target() }) }; var after = target()
func BenchmarkB(b *testing.B) { target() }
func FuzzC(f *testing.F) { target() }
func ExampleD() { target() }
type A struct{}
type B struct{}
func (a *A) TestSame() { target() }
func (b *B) TestSame() { target() }
func first() { target() }; func second() { target() }
`
	root := repoWith(t, map[string]string{"owner_test.go": src})
	rc, out, errs := callers(t, "-format", "json", "target", root)
	var got report.CallersSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 {
		t.Fatalf("callers rc=%d err=%v stderr=%q output=%q", rc, err, errs, out)
	}
	want := []string{"", "helper()", "TestA(t *testing.T)", "TestA(t *testing.T)", "TestA(t *testing.T)", "", "BenchmarkB(b *testing.B)", "FuzzC(f *testing.F)", "ExampleD()", "A.TestSame()", "B.TestSame()", "first()", "second()"}
	var owners []string
	for _, e := range got.Entries {
		owners = append(owners, e.Caller)
		if !e.Test {
			t.Errorf("test file entry lost test marker: %+v", e)
		}
	}
	if got.Matches != len(want) || !reflect.DeepEqual(owners, want) {
		t.Fatalf("owners=%q want=%q; entries=%+v", owners, want, got.Entries)
	}
	if got.Entries[2].Col == got.Entries[3].Col {
		t.Error("same-line calls must retain distinct columns")
	}
	rc, text, errs := callers(t, "-limit", "0", "target", root)
	if rc != 0 || strings.Count(text, "TestA(t *testing.T) [test]") != 3 || strings.Count(text, "(単位の外)") != 2 {
		t.Errorf("text rc=%d err=%q output=%q", rc, errs, text)
	}
}

func TestGoCallersDoesNotNameBrokenOwner(t *testing.T) {
	root := repoWith(t, map[string]string{"broken_test.go": `package fixture
func TestRecovered(t *testing.T) { target() }
func TestBroken(t *testing.T) { target(); for {
`})
	rc, out, errs := callers(t, "-format", "json", "target", root)
	var got report.CallersSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 {
		t.Fatalf("rc=%d err=%v stderr=%q output=%q", rc, err, errs, out)
	}
	if got.Matches != 2 || got.Entries[0].Caller != "TestRecovered(t *testing.T)" || got.Entries[1].Caller != "" {
		t.Errorf("broken owner must remain unknown, recovered owner must be named: %+v", got)
	}
}
