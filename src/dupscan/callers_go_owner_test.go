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

// Swift の XCTest / swift-testing のテスト関数は比べる単位にしないが、callers では呼び出し元として名前を出す。
// テスト関数の中の局所の関数も単位にならないので、その関数を名指しする (囲む型を付ける表記は単位と同じ)。クロージャは囲む関数に帰属させる。
// 局所の型の property / didSet の中は、テスト関数に帰属させない (型の中はテスト関数の本体ではない)。
func TestSwiftCallersNamesTestFunctions(t *testing.T) {
	src := `import XCTest
import Testing

final class FrameTests: XCTestCase {
    func helper() { target() }
    func testReports() async throws {
        target()
        try await wait { target() }
        func local() { target() }
        local()
        struct Local {
            var value: Int { target() }
            var count = 0 { didSet { target() } }
            func run() { target() }
        }
    }
    func testWith(_ x: Int) { target() }
}

@Test func swiftTesting() { target() }
`
	root := repoWith(t, map[string]string{"Tests/FrameTests.swift": src})
	rc, out, errs := callers(t, "-format", "json", "-limit", "0", "target", root)
	var got report.CallersSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 {
		t.Fatalf("callers rc=%d err=%v stderr=%q output=%q", rc, err, errs, out)
	}
	want := []string{"FrameTests.helper()", "FrameTests.testReports()", "FrameTests.testReports()", "FrameTests.local()", "", "", "FrameTests.Local.run()", "FrameTests.testWith(_ x: Int)", "swiftTesting()"}
	var owners []string
	for _, e := range got.Entries {
		owners = append(owners, e.Caller)
	}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("Swift のテスト関数を呼び出し元として名指しする:\n got: %q\nwant: %q", owners, want)
	}
}

// テスト関数の中の局所の型の property は、持ち主を「不明」と明示する (nil にしない)。nil は callers に単位の行範囲で探し直させ、
// 同じ行の別の関数へ帰属させ、test の印も落とす (テストのパスでない swift-testing のファイルでは test: false になる)。
func TestSwiftCallersKeepsLocalTypePropertiesInsideTestsUnowned(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Tests/Repro.swift": "import XCTest\nfunc target() -> Int { 1 }\nfinal class Sample: XCTestCase {\n" +
			"    func helper() {}; func testExample() { struct Local { var value: Int { target() } } }\n}\n",
		"Specs/Repro.swift": "import Testing\nfunc target2() -> Int { 1 }\n@Test func example() {\n    struct Local {\n        var value: Int { target2() }\n    }\n}\n",
	})
	for _, name := range []string{"target", "target2"} {
		rc, out, errs := callers(t, "-format", "json", "-limit", "0", name, root)
		var got report.CallersSummary
		if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 || len(got.Entries) != 1 {
			t.Fatalf("%s: callers rc=%d err=%v stderr=%q output=%q", name, rc, err, errs, out)
		}
		if e := got.Entries[0]; e.Caller != "" || !e.Test {
			t.Errorf("%s: 局所の型の property は呼び出し元を名指しせず、test の印は残す: %+v", name, e)
		}
	}
}
