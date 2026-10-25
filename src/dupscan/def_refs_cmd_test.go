package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const definitionsSource = `struct Box {
    let stored: Int = 1
    var value: Int { stored }
    func combine(_ left: Int, named right: Int) -> Int {
        left + right
    }
}
extension Box {
    func extra() -> Int { stored }
}
struct Other {
    func combine(_ left: Int, named right: Int) -> Int { left - right }
}
let closure = { let localOnly = 1; return localOnly }
func longBody() -> Int {
    print(1)
    print(2)
    print(3)
    return 4
}
`

func def(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"def"}, args...)...)
}

func refs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"refs"}, args...)...)
}

func TestDefMatchesExactNamesTypesAndLabels(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": definitionsSource})
	rc, out, errs := def(t, "combine", root)
	if rc != 0 || strings.Count(out, ".combine(") != 2 {
		t.Fatalf("combine は完全一致の 2 定義: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "Box.combine(_:named:)", root)
	if rc != 0 || strings.Count(out, ".combine(") != 1 || !strings.Contains(out, "Box.combine(_ left: Int, named right: Int)") {
		t.Errorf("型と引数ラベルで Box.combine だけを選ぶ: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "-limit", "1", "combine", root)
	if rc != 0 || !strings.Contains(out, "… 他 1 件") {
		t.Errorf("-limit は出力する定義を切る: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "combine", "-limit", "1", root)
	if rc != 0 || !strings.Contains(out, "… 他 1 件") {
		t.Errorf("定義名の後の -limit も受け付ける: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "missing", root)
	if rc != 1 || out != "dupscan def: missing の定義なし\n" || errs != "" {
		t.Errorf("定義なしは 1 行と rc=1: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefTypesIncludeExtensionsAndStoredProperties(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": definitionsSource})
	rc, out, errs := def(t, "Box", root)
	if rc != 0 || !strings.Contains(out, "struct Box") || !strings.Contains(out, "extension Box") ||
		!strings.Contains(out, "Box.combine(_ left: Int, named right: Int)") || !strings.Contains(out, "Box.extra()") {
		t.Fatalf("型宣言と extension ごとのメンバー一覧: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	valueAt := strings.Index(out, "Box.var value: Int")
	combineAt := strings.Index(out, "Box.combine(")
	if valueAt < 0 || combineAt < 0 || valueAt > combineAt {
		t.Errorf("型のメンバー一覧は index -file と同じ行順: %q", out)
	}
	rc, out, errs = def(t, "stored", root)
	if rc != 0 || !strings.Contains(out, "Box.let stored: Int") || !strings.Contains(out, "let stored: Int = 1") {
		t.Errorf("stored property も定義として出す: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "localOnly", root)
	if rc != 1 || !strings.Contains(out, "定義なし") {
		t.Errorf("closure の local variable は property 定義として出さない: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefFindsInitializersAndSubscriptsByLabels(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Shelf.swift": `struct Shelf {
    let count: Int
    init(count: Int) { self.count = count }
    subscript(index: Int) -> Int { index }
}
`})
	rc, out, errs := def(t, "Shelf.init(count:)", root)
	if rc != 0 || !strings.Contains(out, "Shelf.init(count: Int)") {
		t.Errorf("initializer をラベルで引く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "Shelf.subscript(index:)", root)
	if rc != 0 || !strings.Contains(out, "Shelf.subscript(index: Int) -> Int") {
		t.Errorf("subscript をラベルで引く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefListsProtocolRequirementsCasesAliasesAndNestedTypes(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Outline.swift": `protocol StorageContract {
    associatedtype Payload
    var transferWireCapabilities: String { get }
    init(configuration: Int)
    subscript(index: Int) -> Payload { get }
    func send(payload: Payload)
}
enum TransferStatus {
    case failed, ready
}
struct Container {
    let stored: Int = 1
    typealias Alias = String
    struct Inner {}
}
`})
	rc, out, errs := def(t, "StorageContract", root)
	for _, want := range []string{
		"StorageContract.associatedtype Payload", "StorageContract.var transferWireCapabilities: String",
		"StorageContract.init(configuration: Int)", "StorageContract.subscript(index: Int) -> Payload", "StorageContract.send(payload: Payload)",
	} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Errorf("protocol 要件を outline に含める %q: rc=%d out=%q stderr=%q", want, rc, out, errs)
		}
	}
	rc, out, errs = def(t, "TransferStatus", root)
	if rc != 0 || !strings.Contains(out, "TransferStatus.failed") || !strings.Contains(out, "TransferStatus.ready") {
		t.Errorf("enum case を outline に含める: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	for _, tc := range []struct{ query, want string }{
		{"TransferStatus.failed", "TransferStatus.failed"},
		{"failed", "TransferStatus.failed"},
		{"ready", "TransferStatus.ready"},
		{"Alias", "typealias Alias = String"},
		{"Payload", "associatedtype Payload"},
	} {
		rc, out, errs = def(t, tc.query, root)
		if rc != 0 || !strings.Contains(out, tc.want) {
			t.Errorf("case / typealias / associatedtype を def で引く %q: rc=%d out=%q stderr=%q", tc.query, rc, out, errs)
		}
	}
	rc, out, errs = def(t, "Container", root)
	if rc != 0 || !strings.Contains(out, "Container.let stored: Int") || !strings.Contains(out, "Container.typealias Alias = String") || !strings.Contains(out, "Container.struct Inner") {
		t.Errorf("保存 property・typealias・入れ子の型を outline に含める: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefTypeMemberMatchesGenericAndConditionalScopes(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Generic.swift": `protocol Marker {}
struct Box<T> where T: Marker {
    func member() {}
    func outer() {
        func nested() {}
    }
}
extension Box where T: Marker {
    func constrained() {}
}
struct Outer {
    struct Bar { func inside() {} }
}
extension Outer.Bar {
    func extensionMember() {}
}
`})
	for _, query := range []string{"Box.member", "Box.constrained", "Outer.Bar.inside", "Outer.Bar.extensionMember"} {
		rc, out, errs := def(t, query, root)
		if rc != 0 || !strings.Contains(out, query[strings.LastIndex(query, ".")+1:]) {
			t.Errorf("generic / where / qualified extension scope %q: rc=%d out=%q stderr=%q", query, rc, out, errs)
		}
	}
	rc, out, errs := def(t, "Box.nested", root)
	if rc != 1 || !strings.Contains(out, "定義なし") {
		t.Errorf("Type.member はメソッド内の nested func を除く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "nested", root)
	if rc != 0 || !strings.Contains(out, "nested()") {
		t.Errorf("nested func は名前だけなら引ける: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefTruncatesSourceAndEmitsJSON(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Box.swift": definitionsSource})
	rc, out, errs := def(t, "-max-lines", "2", "longBody", root)
	if rc != 0 || !strings.Contains(out, "… 残り 4 行") || strings.Contains(out, "print(2)") {
		t.Fatalf("2 行で切って残りを示す: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = def(t, "-format", "json", "-max-lines", "2", "longBody", root)
	var result struct {
		Matches int `json:"matches"`
		Entries []struct {
			Source         []string `json:"source"`
			Truncated      bool     `json:"truncated"`
			RemainingLines int      `json:"remaining_lines"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &result); rc != 0 || err != nil || result.Matches != 1 || len(result.Entries) != 1 ||
		!result.Entries[0].Truncated || result.Entries[0].RemainingLines != 4 || len(result.Entries[0].Source) != 2 {
		t.Errorf("JSON に切り詰め位置と残り行数を出す: rc=%d err=%v result=%+v stderr=%q", rc, err, result, errs)
	}
}

func TestRefsIgnoreDeclarationsCommentsAndStringsAndCountInterpolation(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/Refs.swift": `struct Box {
    func read() { target() }
}

func use(_ value: Box) { let result = value.read() }
let interpolated = "\(target)"
// target and Box are only words here.
let literal = "target Box"
`,
		"Tests/BoxTests.swift": `struct BoxChecks {
    func check(_ item: Box) { print(item) }
}
`,
	})
	rc, out, errs := refs(t, "Box", root)
	want := "Sources/Refs.swift:5 use(_ value: Box) [型の位置] | func use(_ value: Box) { let result = value.read() }\n" +
		"Tests/BoxTests.swift:2 BoxChecks.check(_ item: Box) [test] [型の位置] | func check(_ item: Box) { print(item) }\n"
	if rc != 0 || out != want {
		t.Errorf("型参照は宣言・コメント・文字列を除き prod を先に出す: rc=%d out=%q want=%q stderr=%q", rc, out, want, errs)
	}
	rc, out, errs = refs(t, "target", root)
	if rc != 0 || strings.Count(out, "[式の位置]") != 2 || !strings.Contains(out, "Sources/Refs.swift:6") || strings.Contains(out, ":1 ") {
		t.Errorf("呼び出しと文字列補間を数え、宣言・コメント・通常文字列を除く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = refs(t, "-limit", "1", "target", root)
	if rc != 0 || !strings.Contains(out, "… 他 1 件") {
		t.Errorf("refs の -limit は出力する参照を切る: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = refs(t, "target", "-limit", "1", root)
	if rc != 0 || !strings.Contains(out, "… 他 1 件") {
		t.Errorf("名前の後の -limit も受け付ける: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = refs(t, "-format", "json", "target", root)
	var refJSON struct {
		Matches int `json:"matches"`
		Entries []struct {
			Kind string `json:"kind"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &refJSON); rc != 0 || err != nil || refJSON.Matches != 2 || len(refJSON.Entries) != 2 || refJSON.Entries[0].Kind != "expression_position" {
		t.Errorf("refs JSON は構文上の位置と件数を出す: rc=%d err=%v json=%+v stderr=%q", rc, err, refJSON, errs)
	}
	rc, out, errs = refs(t, "absent", root)
	if rc != 1 || out != "dupscan refs: absent の参照なし\n" || errs != "" {
		t.Errorf("参照なしは 1 行と rc=1: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestRefsUseSyntacticPositionLabelsAndTestFunctionNames(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/Positions.swift": `struct Foo { static let shared = 1 }
func makeFoo() {
    _ = Foo()
    _ = Foo.shared
}
`,
		"Tests/RefsTests.swift": `import Testing
@Test func swiftTest() { target }
final class XCTestFixture: XCTestCase {
    func testValue() { target }
}
`,
	})
	rc, out, errs := refs(t, "Foo", root)
	if rc != 0 || strings.Count(out, "[式の位置]") != 2 || strings.Contains(out, "[型の位置]") {
		t.Errorf("型名でも式中の出現は式の位置と表示する: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = refs(t, "shared", root)
	if rc != 0 || !strings.Contains(out, "[式の位置]") {
		t.Errorf("メンバー参照は式の位置として数える: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	rc, out, errs = refs(t, "target", root)
	if rc != 0 || !strings.Contains(out, "swiftTest() [test] [式の位置]") ||
		!strings.Contains(out, "XCTestFixture.testValue() [test] [式の位置]") || strings.Contains(out, "(単位の外)") {
		t.Errorf("@Test / XCTest の参照はテスト関数名を囲む単位として出す: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}

func TestDefFindsNestedTypeByQualifiedName(t *testing.T) {
	// extension Outer.Inner と同じ書き方で、入れ子の型を引ける
	root := repoWith(t, map[string]string{"Sources/Nested.swift": `struct Outer {
    struct Inner {
        let value: Int = 1
    }
}
struct Inner {}
`})
	rc, out, errs := def(t, "Outer.Inner", root)
	if rc != 0 || !strings.Contains(out, "Sources/Nested.swift:2-4 struct Inner") || strings.Contains(out, "Sources/Nested.swift:6") {
		t.Errorf("Outer.Inner は入れ子の型だけを引く: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}
