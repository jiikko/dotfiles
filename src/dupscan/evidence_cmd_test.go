package main

import (
	"strings"
	"testing"
)

const evidenceSwift = `struct Store {
    /// Builds the location.
    ///
    /// Use this when a bucket must be joined with a connection.
    func location(_ name: String) -> String { name }

    /** Opens it.
     *  The BUCKET is created lazily. */
    func open() {}

    // bucket in a plain comment is not a doc
    func plain() { let s = "bucket"; print(s) }
}
`

const evidenceSwiftTest = `import XCTest
final class StoreTests: XCTestCase {
    func testBucket() {
        let bucket = Store().location("b")
        XCTAssertEqual(bucket, "b")
    }
}
`

const evidenceGo = `package store

// Join builds a key.
//
// The bucket comes first.
func Join(a, b string) string { return a + b }
`

const evidenceGoTest = `package store

import "testing"

func TestJoin(t *testing.T) {
	bucket := Join("a", "b")
	_ = bucket
}
`

func evidence(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"evidence"}, args...)...)
}

func evidenceRepo(t *testing.T) string {
	t.Helper()
	return repoWith(t, map[string]string{
		"Sources/Store.swift":     evidenceSwift,
		"Tests/StoreTests.swift":  evidenceSwiftTest,
		"pkg/store/store.go":      evidenceGo,
		"pkg/store/store_test.go": evidenceGoTest,
	})
}

func TestEvidenceFindsUnitsByWordsBeyondTheFirstDocLine(t *testing.T) {
	root := evidenceRepo(t)
	rc, out, errs := evidence(t, "-limit", "0", "bucket", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	sec := contextSections(t, out)
	doc := sec["doc"]
	for _, want := range []string{
		"Sources/Store.swift:4 Store.location(_ name: String) -> String | /// Use this when a bucket must be joined with a connection.",
		"Sources/Store.swift:8 Store.open() | *  The BUCKET is created lazily. */",
		"pkg/store/store.go:5 Join(a, b string) string | // The bucket comes first.",
	} {
		if !strings.Contains(doc, want+"\n") {
			t.Errorf("doc の 2 行目以降 (大文字小文字を区別しない) の語から単位へ辿る。%q が無い: %q", want, doc)
		}
	}
	if strings.Contains(doc, "plain") {
		t.Errorf("doc でないコメントの語を doc として数えない: %q", doc)
	}
	if !strings.HasPrefix(out, `dupscan evidence: "bucket" — doc に語がある単位 3 個 /`) {
		t.Errorf("1 行目に doc の単位の数: %q", out)
	}
	_, idx, _ := runReport(t, "index", "-doc", "bucket", root)
	if strings.Contains(idx, "location") {
		t.Errorf("前提: index -doc は doc の最初の行しか見ないので location を引けない: %q", idx)
	}
}

func TestEvidenceSeparatesTestAndSourceLinesAndNamesCalledHelpers(t *testing.T) {
	root := evidenceRepo(t)
	rc, out, errs := evidence(t, "-limit", "0", "bucket", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	sec := contextSections(t, out)
	test, src := sec["テストの行"], sec["その他のソースの行"]
	for _, want := range []string{
		`Tests/StoreTests.swift:4 StoreTests.testBucket() | let bucket = Store().location("b")  → 呼び出し: location, Store`,
		`pkg/store/store_test.go:6 (単位の外) | bucket := Join("a", "b")  → 呼び出し: Join`,
	} {
		if !strings.Contains(test, want+"\n") {
			t.Errorf("テストの行に、囲む単位とその行の呼び出しを添える。%q が無い: %q", want, test)
		}
	}
	for _, want := range []string{
		"Sources/Store.swift:11 struct Store | // bucket in a plain comment is not a doc",
		`Sources/Store.swift:12 Store.plain() | func plain() { let s = "bucket"; print(s) }  → 呼び出し: print`,
	} {
		if !strings.Contains(src, want+"\n") {
			t.Errorf("doc でもテストでもない行 (コメント・文字列を含む) はその他のソースに出す。%q が無い: %q", want, src)
		}
	}
	called := sec["語を含む行で呼ばれている名前のうち、repo"]
	if !strings.HasPrefix(called, "Join (1 行)\nStore (1 行)\nlocation (1 行)\n") || strings.Contains(called, "XCTAssertEqual") || strings.Contains(called, "print") {
		t.Errorf("呼ばれている名前は repo に定義があるものだけを、行の数・名前の順で: %q", called)
	}
}

func TestEvidenceGroupsDocLinesPerUnit(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": "/// retry once\n/// then retry again\nfunc go() {}\n"})
	_, out, _ := evidence(t, "retry", root)
	if !strings.Contains(out, "Sources/A.swift:1 go() | /// retry once  (doc の 2 行に語がある)\n") || !strings.Contains(out, "## doc に語がある単位 (1 個 / doc の 2 行)") {
		t.Errorf("同じ単位の doc の行は 1 件にまとめて行の数を添える: %q", out)
	}
}

func TestEvidenceFindsDocsOfRequirementsStoredPropertiesAndTypes(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/P.swift": `/// A transport.
/// Frames are sent whole.
protocol Transport {
    /// Sends one frame.
    /// Not interleaved with another frame.
    func send(_ frame: [UInt8]) async throws
}
struct Negotiate {
    /// Server capabilities.
    /// The only signal for encryption.
    var capabilities: UInt32 = 0
}
final class StoreTests: XCTestCase {
    /// Checks the store.
    /// Uses a fake frame.
    func testStore() {}
}
`})
	rc, out, errs := evidence(t, "-limit", "0", "frame", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errs, out)
	}
	doc := contextSections(t, out)["doc"]
	for _, want := range []string{
		"Sources/P.swift:2 protocol Transport | /// Frames are sent whole.",
		"Sources/P.swift:4 Transport.send(_ frame: [UInt8]) | /// Sends one frame.  (doc の 2 行に語がある)",
		"Sources/P.swift:15 StoreTests.testStore() | /// Uses a fake frame.",
	} {
		if !strings.Contains(doc, want+"\n") {
			t.Errorf("protocol の要件・型・テスト関数の doc も単位へ辿る。%q が無い: %q", want, doc)
		}
	}
	_, out, _ = evidence(t, "encryption", root)
	if !strings.Contains(out, "Sources/P.swift:10 Negotiate.var capabilities: UInt32 | /// The only signal for encryption.") {
		t.Errorf("保存 property の doc も単位へ辿る: %q", out)
	}
}

func TestEvidenceDocRangeStopsAtNonDocCommentAboveIt(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": "/// old note\n// MARK: - Helpers\n/// joins parts\n// dupscan:allow helper\nfunc join() {}\n"})
	_, out, _ := evidence(t, "-limit", "0", "o", root)
	sec := contextSections(t, out)
	if !strings.Contains(sec["doc"], "Sources/A.swift:3 join() | /// joins parts\n") || strings.Contains(sec["doc"], "old note") || strings.Contains(sec["doc"], "MARK") {
		t.Errorf("doc は宣言に近い側から続く doc comment だけ (間の許可の印は飛ばし、MARK の上の doc は入れない): %q", out)
	}
}

func TestEvidenceShortensLongUnitSignatures(t *testing.T) {
	params := strings.Repeat("argumentNumber: Int, ", 8)
	root := repoWith(t, map[string]string{"Sources/A.swift": "func long(" + params + "last: Int) {\n    // needle\n}\n"})
	_, out, _ := evidence(t, "needle", root)
	if !strings.Contains(out, "Sources/A.swift:2 long("+strings.Repeat("argumentNumber: Int, ", 4)[:75]+"… | // needle\n") {
		t.Errorf("囲む単位の署名は 80 文字で切る: %q", out)
	}
}

func TestEvidenceTruncatesLongLinesAndReplacesBrokenUTF8(t *testing.T) {
	long := "// needle " + strings.Repeat("x", 300) + "\n"
	root := repoWith(t, map[string]string{"Sources/A.swift": long + "// needle \xff\nfunc f() {}\n"})
	rc, out, _ := evidence(t, "needle", root)
	if rc != 0 || !strings.Contains(out, "| // needle "+strings.Repeat("x", 150)+"…\n") || !strings.Contains(out, "| // needle \uFFFD\n") {
		t.Errorf("長い行は 160 文字で切り、壊れた UTF-8 は置き換える: rc=%d out=%q", rc, out)
	}
}

func TestEvidenceNoHitIsZeroCountsWithRcZero(t *testing.T) {
	root := evidenceRepo(t)
	rc, out, errs := evidence(t, "nowhere-to-be-found", root)
	if rc != 0 || errs != "" || !strings.Contains(out, "doc に語がある単位 0 個 / テスト 0 件 / その他のソース 0 件 / そこで呼ばれている名前 0 個") {
		t.Errorf("0 件でも rc=0 で件数を出す: rc=%d stderr=%q out=%q", rc, errs, out)
	}
}

func TestEvidenceRejectsBadArguments(t *testing.T) {
	root := evidenceRepo(t)
	for _, args := range [][]string{{}, {" "}, {"a\nb", root}, {"-limit", "-1", "bucket", root}} {
		if rc, _, _ := evidence(t, args...); rc != 2 {
			t.Errorf("%q は rc=2: rc=%d", args, rc)
		}
	}
}
