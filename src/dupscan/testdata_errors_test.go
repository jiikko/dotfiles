package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// testdataRepo は、読める Go のファイルと、testdata の下 (go のツールが読まない置き場) か外に壊れた Go のファイルを持つ、commit 済みの repo。
func testdataRepo(t *testing.T, brokenAt string) string {
	t.Helper()
	root := repoWith(t, map[string]string{
		"a/good.go": "package a\nfunc healthy(x int) int { return x + 1 }\n",
		brokenAt:    "package broken\nfunc broken(",
	})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "base")
	return root
}

// 読めないファイルが testdata の下だけなら、1 行目の状態は「新しい重複なし」で、testdata の数を別に出す。json と strict の rc は変えない。
func TestGateTestdataOnlyParseErrorsDoNotMarkIncomplete(t *testing.T) {
	root := testdataRepo(t, "a/testdata/broken/broken.go")
	rc, out, errs := runGateCmd(t, "-base", "HEAD", root)
	first, _, _ := strings.Cut(out, "\n")
	if rc != 0 || !strings.Contains(first, "新しい重複なし") || strings.Contains(first, "解析が不完全") || !strings.Contains(first, "読めない testdata 1") {
		t.Errorf("rc=%d first=%q err=%q", rc, first, errs)
	}
	if strings.Contains(out, "読めないファイル (") {
		t.Errorf("-v なしで testdata の読めないファイルを 1 行ずつ出した: %q", out)
	}
	_, verbose, _ := runGateCmd(t, "-base", "HEAD", "-v", root)
	if !strings.Contains(verbose, "a/testdata/broken/broken.go") {
		t.Errorf("-v に testdata の読めないファイルが無い: %q", verbose)
	}
	_, js, _ := runGateCmd(t, "-base", "HEAD", "-format", "json", root)
	var s struct {
		Incomplete bool     `json:"analysis_incomplete"`
		Head       []string `json:"head_files_with_errors"`
	}
	if err := json.Unmarshal([]byte(js), &s); err != nil || !s.Incomplete || len(s.Head) != 1 {
		t.Errorf("json の解析の不完全さを変えた: %+v (err %v)", s, err)
	}
	// strict は testdata も数えて rc=2 で止める (契約は変えない)。止めるのに先頭が「なし」だと失敗に見えないので、先頭も不完全と言う
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", "-strict-analysis", root); rc != 2 || !strings.Contains(out, "解析が不完全") {
		t.Errorf("-strict-analysis: rc=%d (want 2) out=%q", rc, out)
	}
}

// testdata の外の読めないファイルは今までどおり「解析が不完全」。
func TestGateNonTestdataParseErrorsStillIncomplete(t *testing.T) {
	root := testdataRepo(t, "a/broken.go")
	_, out, _ := runGateCmd(t, "-base", "HEAD", root)
	if first, _, _ := strings.Cut(out, "\n"); !strings.Contains(first, "解析が不完全") {
		t.Errorf("testdata の外の読めないファイルで解析が不完全と出ない: %q", first)
	}
}

func TestReportTestdataOnlyParseErrorsNoted(t *testing.T) {
	root := testdataRepo(t, "a/testdata/broken.go")
	_, out, _ := runReport(t, root)
	if first, _, _ := strings.Cut(out, "\n"); strings.Contains(first, "読めない構文があるファイル") || !strings.Contains(first, "読めない testdata 1") {
		t.Errorf("first=%q", first)
	}
}
