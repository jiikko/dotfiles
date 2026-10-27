package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"subproc"
	"testing"
	"unicode/utf8"

	"dupscan/internal/report"
)

const searchGoFixture = `package location
type Connection struct{}
type Destination struct{}
// Assemble joins a bucket to its connection.
func Assemble(connection Connection) Destination { return Destination{} }
func Other() int { return 0 }
`

func TestSearchFindsUnknownNameAcrossFieldsAndTypes(t *testing.T) {
	root := repoWith(t, map[string]string{"location.go": searchGoFixture, "notes.txt": "unparsed"})
	var out, errs bytes.Buffer
	rc := runSearch([]string{"-query", "connection bucket", "-returns", "Destination", "-min-matched-terms", "2", root}, &out, &errs)
	if rc != 0 || errs.Len() != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, &errs)
	}
	for _, want := range []string{"候補 1 件 / 表示 1 件", "Assemble(connection Connection) Destination", "matched \"connection\" fields=signature,doc", "matched \"bucket\" fields=doc", "reuse=not_proven", "returns=\"Destination\""} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, &out)
		}
	}
	_, old, _ := runReport(t, "index", "-name", "connection bucket", "-returns", "Destination", root)
	if !strings.Contains(old, "当たる単位なし") {
		t.Fatal(old)
	}
	rc, result, errout := runReport(t, "search", "-query", "connection bucket", "-returns", "Destination", root)
	if rc != 0 || errout != "" || !strings.Contains(result, "Assemble") {
		t.Fatalf("dispatch: %d %q %q", rc, result, errout)
	}
}

func TestSearchRespectsLanguageScopeFileAndNoAliasResolution(t *testing.T) {
	root := repoWith(t, map[string]string{
		"location.go":         searchGoFixture,
		"location_test.go":    "package location\n// Bucket creates a test connection.\nfunc fixtureConnection() Destination { return Destination{} }\nfunc TestHidden() {}\n",
		"Sources/Swift.swift": "struct Destination {}\n/// A bucket connection.\nfunc swiftFixture() -> Destination { Destination() }\n",
	})
	for _, tc := range []struct {
		flags        []string
		want, absent string
	}{
		{[]string{"-lang", "go", "-scope", "prod"}, "Assemble", "swiftFixture"},
		{[]string{"-lang", "go", "-scope", "test"}, "fixtureConnection", "Assemble"},
		{[]string{"-lang", "swift"}, "swiftFixture", "Assemble"},
		{[]string{"-file", filepath.Join(root, "Sources/Swift.swift")}, "swiftFixture", "Assemble"},
	} {
		var out, errs bytes.Buffer
		args := append([]string{"-query", "bucket connection"}, tc.flags...)
		args = append(args, root)
		if rc := runSearch(args, &out, &errs); rc != 0 || errs.Len() != 0 || !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.absent) {
			t.Errorf("flags=%q rc=%d out=%s stderr=%s", tc.flags, rc, &out, &errs)
		}
	}
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "bucket", "-returns", "StorageAlias", root}, &out, &errs); rc != 0 || !strings.Contains(out.String(), "候補 0 件") || !strings.Contains(out.String(), "不存在の証明ではない") {
		t.Fatalf("alias: %d %s %s", rc, &out, &errs)
	}
}

func TestSearchByteBudgetDoesNotTurnHiddenCandidatesIntoNoMatch(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package a\n// bucket\nfunc Giant(" + strings.Repeat("argument", 1400) + " int) int { return 0 }\n"})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "bucket", "-max-bytes", "1024", root}, &out, &errs); rc != 0 || errs.Len() != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, &errs)
	}
	if out.Len() > 1024 || !utf8.Valid(out.Bytes()) || !strings.Contains(out.String(), "候補 1 件 / 表示 0 件") || !strings.Contains(out.String(), "省略: あり") {
		t.Fatalf("bad budget semantics: %d %s", out.Len(), &out)
	}
}

func TestSearchSnippetCountAndParserOmissionsAreVisible(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package a\n// bucket " + strings.Repeat("日", 180) + "\nfunc One() int { return 0 }\n// bucket\nfunc Two() int { return 1 }\n", "broken.go": "package b\nfunc bad( {\n"})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "bucket", "-limit", "1", root}, &out, &errs); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, &errs)
	}
	header := strings.Split(out.String(), "\n")[0]
	if !strings.Contains(header, "候補 2 件 / 表示 1 件") || !strings.Contains(header, "省略: あり") || !strings.Contains(header, "構文解析失敗: 1 ファイル") || !strings.Contains(out.String(), "doc_truncated=true") || !utf8.Valid(out.Bytes()) {
		t.Fatal(out.String())
	}
}

func TestSearchRetryChangesOneFilterAndKeepsAbsoluteScope(t *testing.T) {
	root := repoWith(t, map[string]string{"one space/location.go": searchGoFixture, "outside/other.go": strings.ReplaceAll(searchGoFixture, "Assemble", "Outside"), "one space/ignored.go": strings.ReplaceAll(searchGoFixture, "Assemble", "OutsideSameScope")})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "connection bucket", "-returns", "Alias", "-file", filepath.Join(root, "one space/location.go"), "-lang", "go", "-scope", "prod", filepath.Join(root, "one space")}, &out, &errs); rc != 0 {
		t.Fatalf("rc=%d %s", rc, &errs)
	}
	var action nextAction
	for _, line := range strings.Split(out.String(), "\n") {
		if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
			if err := json.Unmarshal([]byte(raw), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(action.Argv) == 0 || !strings.Contains(action.Reason, "returns 条件を外す") {
		t.Fatalf("missing explicit relaxation: %s", &out)
	}
	t.Chdir(t.TempDir())
	rc, result, errout := runReport(t, action.Argv[1:]...)
	if rc != 0 || errout != "" || !strings.Contains(result, "Assemble") || strings.Contains(result, "Outside") || !strings.Contains(result, "scope=prod") || !strings.Contains(result, "lang=go") {
		t.Fatalf("retry: %q rc=%d out=%q stderr=%q", action.Argv, rc, result, errout)
	}
	var miss, missErr bytes.Buffer
	if rc := runSearch([]string{"-query", "noSuchHelper", root}, &miss, &missErr); rc != 0 || !strings.Contains(miss.String(), `"evidence"`) {
		t.Fatalf("unfiltered miss must use new evidence rather than repeat search: %d %s", rc, &miss)
	}
}

func TestSearchRejectsInvalidInputsBeforeRepositoryAccess(t *testing.T) {
	for _, args := range [][]string{nil, {"-query", "x", "-limit", "0"}, {"-query", "x", "-limit", "51"}, {"-query", "x", "-max-bytes", "1023"}, {"-query", "x", "-max-bytes", "1048577"}, {"-query", "x", "-min-matched-terms", "2"}, {"-query", "x", "-scope", "other"}, {"-query", "x", "-lang", "python"}, {"-query", "x", "-format", "json-v2"}, {"-query", strings.Repeat("x", 65)}} {
		var out, errs bytes.Buffer
		if rc := runSearch(args, &out, &errs); rc != 2 || out.Len() != 0 || errs.Len() == 0 {
			t.Errorf("args=%q rc=%d out=%q stderr=%q", args, rc, &out, &errs)
		}
	}
}

func TestSearchCandidateTextEscapesDataAndMarksSnippetOmission(t *testing.T) {
	text, truncated := report.SearchCandidateText(report.SearchCandidate{Entry: report.IndexEntry{UnitRef: report.UnitRef{File: "a\n.go", Line: 2, Col: 3, Signature: "func a()\n"}, Doc: strings.Repeat("日", 161)}, Matched: []report.SearchEvidence{{Term: "bucket\n", Fields: []string{"doc"}}}})
	if !truncated || !utf8.ValidString(text) || !strings.Contains(text, `"a\n.go":2:3`) || !strings.Contains(text, `matched "bucket\n"`) || !strings.Contains(text, "doc_truncated=true") {
		t.Fatal(text)
	}
}

func TestSearchRetryRelaxesTermCountBeforeHardTypes(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": searchGoFixture})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "Assemble neverWord", "-returns", "Destination", "-min-matched-terms", "2", root}, &out, &errs); rc != 0 {
		t.Fatalf("%d %s", rc, &errs)
	}
	var action nextAction
	for _, line := range strings.Split(out.String(), "\n") {
		if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
			if err := json.Unmarshal([]byte(raw), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !strings.Contains(action.Reason, "min-matched-termsを1") {
		t.Fatalf("lost hard filter first: %s", &out)
	}
	rc, result, errout := runReport(t, action.Argv[1:]...)
	if rc != 0 || errout != "" || !strings.Contains(result, "候補 1 件") || !strings.Contains(result, `returns="Destination"`) {
		t.Fatalf("%d %q %q", rc, result, errout)
	}
}

func TestSearchEvidenceFallbackCanFindDocBeyondIndex(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": "package a\n// Build creates a value.\n// secretBucket belongs here.\nfunc Build() int { return 0 }\n"})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "secretBucket", root}, &out, &errs); rc != 0 || !strings.Contains(out.String(), "候補 0 件") {
		t.Fatalf("%d %s %s", rc, &out, &errs)
	}
	var action nextAction
	for _, line := range strings.Split(out.String(), "\n") {
		if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
			if err := json.Unmarshal([]byte(raw), &action); err != nil {
				t.Fatal(err)
			}
		}
	}
	if action.Kind != "evidence" {
		t.Fatal(action)
	}
	rc, result, errout := runReport(t, action.Argv[1:]...)
	if rc != 0 || errout != "" || !strings.Contains(result, "Build() int") {
		t.Fatalf("%d %q %q", rc, result, errout)
	}
}

func TestSearchCandidateReadArgvShowsOnlyBoundedSource(t *testing.T) {
	root := repoWith(t, map[string]string{"space dir/a.go": "package a\n// bucket\nfunc Large() int {\n" + strings.Repeat(" _ = 1\n", 90) + " return 0\n}\n"})
	var out, errs bytes.Buffer
	if rc := runSearch([]string{"-query", "bucket", root}, &out, &errs); rc != 0 {
		t.Fatalf("%d %s", rc, &errs)
	}
	var argv []string
	for _, line := range strings.Split(out.String(), "\n") {
		if raw, ok := strings.CutPrefix(line, "  read_argv: "); ok {
			if err := json.Unmarshal([]byte(raw), &argv); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(argv) != 4 || argv[0] != "sed" || !filepath.IsAbs(argv[3]) {
		t.Fatalf("invalid read argv %q", argv)
	}
	body, err := subproc.CommandContext(context.Background(), argv[0], argv[1:]...).Output()
	if err != nil || strings.Count(string(body), "\n") != 80 || !strings.Contains(string(body), "func Large()") || !strings.Contains(out.String(), "source_read_truncated=true") {
		t.Fatalf("read %q err=%v bytes=%q out=%s", argv, err, body, &out)
	}
}

func TestSearchOptionalRetryFitsActualRemainingBudget(t *testing.T) {
	root := repoWith(t, map[string]string{"a.go": searchGoFixture})
	for size := 100; size <= 700; size += 20 {
		query := strings.Repeat("x", size)
		var wide, errs bytes.Buffer
		if rc := runSearch([]string{"-query", "Build", "-returns", query, "-max-bytes", "4096", root}, &wide, &errs); rc != 0 {
			t.Fatalf("wide rc=%d %s", rc, &errs)
		}
		_, action, ok := strings.Cut(wide.String(), "next_action: ")
		budget := wide.Len() - 1
		if !ok || budget < 1024 || len("next_action: ")+len(action) > budget/2 {
			continue
		}
		var out, narrowErr bytes.Buffer
		if rc := runSearch([]string{"-query", "Build", "-returns", query, "-max-bytes", strconv.Itoa(budget), root}, &out, &narrowErr); rc != 0 || out.Len() > budget || !strings.Contains(out.String(), "候補 0 件 / 表示 0 件") || strings.Contains(out.String(), "next_action:") {
			t.Fatalf("optional action must not hide valid summary: rc=%d budget=%d out=%q stderr=%q", rc, budget, &out, &narrowErr)
		}
		return
	}
	t.Fatal("fixture did not exercise an optional-action overflow")
}
