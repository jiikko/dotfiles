package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"subproc"
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
		"  4 Store.location(_ name: String) -> String | /// Use this when a bucket must be joined with a connection.",
		"  8 Store.open() | *  The BUCKET is created lazily. */",
		"  5 Join(a, b string) string | // The bucket comes first.",
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
		`  4 StoreTests.testBucket() | let bucket = Store().location("b")  → 呼び出し: location, Store`,
		`  6 (単位の外) | bucket := Join("a", "b")  → 呼び出し: Join`,
	} {
		if !strings.Contains(test, want+"\n") {
			t.Errorf("テストの行に、囲む単位とその行の呼び出しを添える。%q が無い: %q", want, test)
		}
	}
	for _, want := range []string{
		"  11 struct Store | // bucket in a plain comment is not a doc",
		`  12 Store.plain() | func plain() { let s = "bucket"; print(s) }  → 呼び出し: print`,
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
	if !strings.Contains(out, "  1 go() | /// retry once  (doc の 2 行に語がある)\n") || !strings.Contains(out, "## doc に語がある単位 (1 個 / doc の 2 行)") {
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
		"  2 protocol Transport | /// Frames are sent whole.",
		"  4 Transport.send(_ frame: [UInt8]) | /// Sends one frame.  (doc の 2 行に語がある)",
		"  15 StoreTests.testStore() | /// Uses a fake frame.",
	} {
		if !strings.Contains(doc, want+"\n") {
			t.Errorf("protocol の要件・型・テスト関数の doc も単位へ辿る。%q が無い: %q", want, doc)
		}
	}
	_, out, _ = evidence(t, "encryption", root)
	if !strings.Contains(out, "  10 Negotiate.var capabilities: UInt32 | /// The only signal for encryption.") {
		t.Errorf("保存 property の doc も単位へ辿る: %q", out)
	}
}

func TestEvidenceDocRangeStopsAtNonDocCommentAboveIt(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": "/// old note\n// MARK: - Helpers\n/// joins parts\n// dupscan:allow helper\nfunc join() {}\n"})
	_, out, _ := evidence(t, "-limit", "0", "o", root)
	sec := contextSections(t, out)
	if !strings.Contains(sec["doc"], "  3 join() | /// joins parts\n") || strings.Contains(sec["doc"], "old note") || strings.Contains(sec["doc"], "MARK") {
		t.Errorf("doc は宣言に近い側から続く doc comment だけ (間の許可の印は飛ばし、MARK の上の doc は入れない): %q", out)
	}
}

func TestEvidenceShortensLongUnitSignatures(t *testing.T) {
	params := strings.Repeat("argumentNumber: Int, ", 8)
	root := repoWith(t, map[string]string{"Sources/A.swift": "func long(" + params + "last: Int) {\n    // needle\n}\n"})
	_, out, _ := evidence(t, "needle", root)
	if !strings.Contains(out, "  2 long("+strings.Repeat("argumentNumber: Int, ", 4)[:75]+"… | // needle\n") {
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

func TestEvidenceNextActionReplaysCalledAndDocOnlyCandidates(t *testing.T) {
	for _, docOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "called", true: "doc only"}[docOnly], func(t *testing.T) {
			source := "/// needle is documented\nfunc candidate() {}\n"
			if !docOnly {
				source += "func use() { candidate() /* needle */ }\n"
			}
			root := repoWith(t, map[string]string{"Sources/A.swift": source})
			rc, out, errs := evidence(t, "needle", root)
			if rc != 0 || errs != "" {
				t.Fatalf("rc=%d out=%q stderr=%q", rc, out, errs)
			}
			if strings.Count(out, "next_action: ") != 2 {
				t.Fatalf("source confirmation followed by one lookup that does not prove reuse: %q", out)
			}
			var action nextAction
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "next_action: ") {
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "next_action: ")), &action); err != nil {
						t.Fatal(err)
					}
				}
			}
			rc, replay, replayErr := runReport(t, action.Argv[1:]...)
			if rc != 0 || replayErr != "" || !strings.Contains(replay, "candidate") {
				t.Fatalf("replay rc=%d out=%q stderr=%q argv=%q", rc, replay, replayErr, action.Argv)
			}
			if docOnly && action.Kind != "context" || !docOnly && action.Kind != "callers" {
				t.Fatalf("unexpected action: %#v", action)
			}
		})
	}
}

func TestEvidenceNoHitHasNoNextAction(t *testing.T) {
	_, out, _ := evidence(t, "nothing-matches", evidenceRepo(t))
	if strings.Contains(out, "next_action:") {
		t.Fatalf("no result must not suggest an action: %q", out)
	}
}

func TestEvidenceFirstLineReportsLimitAndSnippetOmissions(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": "func a() {}\n// needle " + strings.Repeat("x", 200) + "\n// needle again\n"})
	for _, limit := range []string{"0", "1"} {
		_, out, _ := evidence(t, "-limit", limit, "needle", root)
		first, _, _ := strings.Cut(out, "\n")
		if !strings.Contains(first, "近似: 語の一致") || !strings.Contains(first, "省略: あり") || !strings.Contains(first, "構文解析失敗: 0 ファイル") {
			t.Fatalf("coverage missing: %q", first)
		}
	}
}

func TestEvidenceDocOnlyPropertyActionShowsDefinition(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": "struct Store {\n /// needle documents capabilities\n var capabilities: UInt32 = 0\n}\n"})
	rc, out, errs := evidence(t, "needle", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d out=%q stderr=%q", rc, out, errs)
	}
	var action nextAction
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "next_action: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "next_action: ")), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if action.Kind != "context" {
		t.Fatalf("property action = %#v", action)
	}
	rc, replay, replayErr := runReport(t, action.Argv[1:]...)
	if rc != 0 || replayErr != "" || !strings.Contains(replay, "定義 1 件") || !strings.Contains(replay, "var capabilities: UInt32 = 0") {
		t.Fatalf("property replay rc=%d out=%q stderr=%q argv=%q", rc, replay, replayErr, action.Argv)
	}
}

func TestEvidenceGoDocOnlyActionUsesIndex(t *testing.T) {
	root := repoWith(t, map[string]string{"store.go": "package store\n// Join combines a needle.\nfunc Join() {}\n"})
	_, out, _ := evidence(t, "needle", root)
	var action nextAction
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "next_action: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "next_action: ")), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if action.Kind != "index" {
		t.Fatalf("Go action = %#v", action)
	}
	rc, replay, errs := runReport(t, action.Argv[1:]...)
	if rc != 0 || errs != "" || !strings.Contains(replay, "Join()") {
		t.Fatalf("rc=%d out=%q stderr=%q", rc, replay, errs)
	}
}

func TestEvidenceSourceConfirmationPrecedesUnrelatedCalledName(t *testing.T) {
	root := repoWith(t, map[string]string{
		"one space/A\"界.swift": "/// needle " + strings.Repeat("界", 180) + "\n/// UI-only changes must NOT be emitted as updates.\nfunc candidate() {}\n",
		"two/Other.swift":      "func unrelated() {}\nfunc usage() { unrelated() /* needle */ }\n",
		"outside/A.swift":      "/// needle outside scope\nfunc outside() {}\n",
	})
	rc, out, errs := evidence(t, "needle", filepath.Join(root, "one space"), filepath.Join(root, "two"))
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d out=%q stderr=%q", rc, out, errs)
	}
	var actions []nextAction
	for _, line := range strings.Split(out, "\n") {
		if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
			var action nextAction
			if err := json.Unmarshal([]byte(raw), &action); err != nil {
				t.Fatal(err)
			}
			actions = append(actions, action)
		}
	}
	if len(actions) != 2 || actions[0].Kind != "read_source" || actions[1].Kind != "callers" || !strings.Contains(out, "unrelated (1 行)") {
		t.Fatalf("original evidence must precede called-name exploration: %#v\n%s", actions, out)
	}
	a := actions[0]
	wantFile, err := absPath(filepath.Join(root, "one space/A\"界.swift"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Argv) != 4 || a.Argv[0] != "sed" || !filepath.IsAbs(a.Argv[3]) || a.Argv[3] != wantFile || !strings.Contains(a.Reason, "source_read_truncated=false") || !strings.Contains(a.Reason, "owner_unknown=false") {
		t.Fatalf("source action lost scope or metadata: %#v", a)
	}
	t.Chdir(t.TempDir())
	body, err := subproc.CommandContext(context.Background(), a.Argv[0], a.Argv[1:]...).Output()
	if err != nil || !bytes.Contains(body, []byte("UI-only changes must NOT be emitted as updates.")) || !bytes.Contains(body, []byte("func candidate()")) || bytes.Contains(body, []byte("outside scope")) {
		t.Fatalf("source confirmation must reveal the negative context: err=%v body=%q", err, body)
	}
	rc, replay, replayErr := runReport(t, actions[1].Argv[1:]...)
	if rc != 0 || replayErr != "" || !strings.Contains(replay, "unrelated() /* needle */") || strings.Contains(replay, "outside()") {
		t.Fatalf("secondary lookup must preserve both absolute prefixes: %d %q %q", rc, replay, replayErr)
	}
}

func TestEvidenceUnknownGoTestOwnerCanReadOriginalTest(t *testing.T) {
	root := repoWith(t, map[string]string{
		"one space/read_test.go": "package p\nimport \"testing\"\nfunc TestRelativeLink(t *testing.T) {\n // needle belongs to this test\n candidate()\n}\n",
		"one space/source.go":    "package p\nfunc candidate() {}\n",
	})
	rc, out, errs := evidence(t, "needle", root)
	if rc != 0 || errs != "" || !strings.Contains(out, "(単位の外)") {
		t.Fatalf("rc=%d out=%q stderr=%q", rc, out, errs)
	}
	var action nextAction
	for _, line := range strings.Split(out, "\n") {
		if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
			if err := json.Unmarshal([]byte(raw), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if action.Kind != "read_source" || !strings.Contains(action.Reason, "owner_unknown=true") || !strings.Contains(action.Reason, "source_read_truncated=false") || strings.Count(out, "next_action: ") != 1 {
		t.Fatalf("unknown owner must remain unknown: %#v\n%s", action, out)
	}
	t.Chdir(t.TempDir())
	body, err := subproc.CommandContext(context.Background(), action.Argv[0], action.Argv[1:]...).Output()
	if err != nil || !bytes.Contains(body, []byte("func TestRelativeLink(t *testing.T)")) || !bytes.Contains(body, []byte("needle belongs to this test")) || !bytes.Contains(body, []byte("candidate()")) {
		t.Fatalf("source read must work without invented ownership: err=%v body=%q", err, body)
	}
}

func TestEvidenceSourceConfirmationBoundsAndDisplayedHit(t *testing.T) {
	for _, tc := range []struct {
		name, text, wantRange, truncated string
		wantLines                        int
	}{
		{"bounded first displayed hit", "package p\n// one\n// two\n// three\n// needle first\n" + strings.Repeat("// filler\n", 195) + "// needle later\n", "1,80p", "true", 80},
		{"no final newline", "package p\n// needle", "1,2p", "false", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repoWith(t, map[string]string{"a.go": tc.text})
			rc, out, errs := evidence(t, "-limit", "1", "needle", root)
			if rc != 0 || errs != "" {
				t.Fatalf("rc=%d out=%q stderr=%q", rc, out, errs)
			}
			var action nextAction
			for _, line := range strings.Split(out, "\n") {
				if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
					if err := json.Unmarshal([]byte(raw), &action); err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(action.Argv) != 4 || action.Argv[2] != tc.wantRange || !strings.Contains(action.Reason, "source_read_truncated="+tc.truncated) || strings.Count(out, "next_action: ") != 1 {
				t.Fatalf("source read must be clamped to the displayed hit and EOF: %#v\n%s", action, out)
			}
			body, err := subproc.CommandContext(context.Background(), action.Argv[0], action.Argv[1:]...).Output()
			if err != nil || len(bytes.Split(bytes.TrimSuffix(body, []byte{'\n'}), []byte{'\n'})) != tc.wantLines || bytes.Contains(body, []byte("needle later")) {
				t.Fatalf("source read must stop at its bound: err=%v body=%q", err, body)
			}
		})
	}
}
