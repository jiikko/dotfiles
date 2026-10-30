package main

import (
	"strings"
	"testing"
)

// 探索のコマンドも gate・report と同じく、Go の testdata の下 (go のツールが読まない置き場) の読めないファイルを「解析不完全」に数えない。
// 問い合わせの名前を含むときだけ、そこが欠けうることを短く言う。
func TestQueryTestdataParseErrorsAreNotIncomplete(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/good.go":                    "package a\nfunc Helper(x int) int { return x + 1 }\nfunc Use() int { return Helper(1) }\n",
		"a/testdata/broken/fixture.go": "package broken\nfunc Helper(",
	})
	// def / refs は Swift だけなので Go の repo では見ない
	for _, command := range []string{"callers", "index"} {
		args := []string{command, "Helper", root}
		if command == "index" {
			args = []string{"index", "-name", "Helper", root}
		}
		_, _, errs := runReport(t, args...)
		if strings.Contains(errs, "解析不完全") || !strings.Contains(errs, "読めない testdata 1 個") {
			t.Errorf("%s: 名前を含む testdata の読めないファイル: %q", command, errs)
		}
		args[len(args)-2] = "Use"
		if _, _, errs := runReport(t, args...); strings.Contains(errs, "解析不完全") || strings.Contains(errs, "読めない") {
			t.Errorf("%s: 名前を含まない testdata の読めないファイルは何も言わない: %q", command, errs)
		}
	}
	// 名前で絞れない問い合わせ (-returns だけ) では、名前があるとは言わずに欠けうることだけを言う
	if _, _, errs := runReport(t, "index", "-returns", "int", root); strings.Contains(errs, "解析不完全") || strings.Contains(errs, "名前がある") || !strings.Contains(errs, "読めない testdata 1 個") {
		t.Errorf("index -returns: %q", errs)
	}
	_, out, _ := runReport(t, "evidence", "Helper", root)
	if first, _, _ := strings.Cut(out, "\n"); !strings.Contains(first, "構文解析失敗: 0 ファイル") || !strings.Contains(first, "読めない testdata 1") || strings.Contains(out, "読めない構文を含むファイルが") {
		t.Errorf("evidence: %q", out)
	}
	_, out, _ = runReport(t, "sweep", root)
	if first, _, _ := strings.Cut(out, "\n"); !strings.Contains(first, "読めないファイル 0") || !strings.Contains(first, "読めない testdata 1") {
		t.Errorf("sweep: %q", first)
	}
}

// testdata の外の読めないファイルは今までどおり「解析不完全」(対照)。
func TestQueryNonTestdataParseErrorsStillIncomplete(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a/good.go":   "package a\nfunc Helper(x int) int { return x + 1 }\n",
		"a/broken.go": "package a\nfunc Helper(",
	})
	if _, _, errs := runReport(t, "callers", "Helper", root); !strings.Contains(errs, "解析不完全") {
		t.Errorf("testdata の外の読めないファイルで解析不完全と出ない: %q", errs)
	}
}
