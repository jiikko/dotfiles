package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dupscan/internal/repo"
)

func TestQueryNextActionPreservesArgumentsWithoutExecution(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	query := "-name $(touch " + marker + "); `echo unsafe`\nquote\""
	sc := repo.Scope{Root: "/repo with spaces", Prefixes: []string{"one/", "two ; quote'/"}}
	a := queryNextAction(sc, "evidence", query, "reason\nwith newline", "-limit", "0")
	want := []string{"dupscan", "evidence", "-limit", "0", "--", query, "/repo with spaces/one", "/repo with spaces/two ; quote'"}
	if !reflect.DeepEqual(a.Argv, want) {
		t.Fatalf("argv = %#v; want %#v", a.Argv, want)
	}
	var out bytes.Buffer
	if err := writeNextAction(&out, a); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("action must occupy one line: %q", out.String())
	}
	var decoded nextAction
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(out.String()), "next_action: ")), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, decoded) {
		t.Fatalf("action round trip: %#v", decoded)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("action must never execute: %v", err)
	}
}

func TestQueryNextActionReplaysMultipleScopes(t *testing.T) {
	root := repoWith(t, map[string]string{
		"one space/A.swift":   "func chosen() {}\nfunc first() { chosen() }\n",
		"two;literal/B.swift": "func second() { chosen() }\n",
		"outside/C.swift":     "func outside() { chosen() }\n",
	})
	sc, err := repo.Resolve([]string{filepath.Join(root, "one space"), filepath.Join(root, "two;literal")})
	if err != nil {
		t.Fatal(err)
	}
	a := queryNextAction(sc, "callers", "chosen", "inspect callers")
	rc, out, errs := runReport(t, a.Argv[1:]...)
	if rc != 0 || errs != "" || !strings.Contains(out, "first") || !strings.Contains(out, "second") || strings.Contains(out, "outside()") {
		t.Fatalf("scope replay rc=%d out=%q stderr=%q argv=%q", rc, out, errs, a.Argv)
	}
	sc.Prefixes = append(sc.Prefixes, "")
	a = queryNextAction(sc, "callers", "chosen", "inspect all")
	if !reflect.DeepEqual(a.Argv, []string{"dupscan", "callers", "--", "chosen", sc.Root}) {
		t.Fatalf("whole scope: %q", a.Argv)
	}
}

func TestQueryUXReportsApproximationOmissionsAndCoverage(t *testing.T) {
	s := (queryUX{Approximation: "lexical", ParseErrors: 2, Unsupported: true, Truncated: true}).suffix()
	for _, want := range []string{"近似: lexical", "省略: あり", "構文解析失敗: 2 ファイル", "未対応言語: あり"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
}

func TestQueryNextActionReplaysLiteralQuery(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	query := "-needle; $(touch " + marker + ") `echo ignored`"
	root := repoWith(t, map[string]string{"Sources/A.swift": "// " + query + "\nfunc candidate() {}\n"})
	sc, err := repo.Resolve([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	action := queryNextAction(sc, "evidence", query, "inspect literal term")
	rc, out, errs := runReport(t, action.Argv[1:]...)
	if rc != 0 || errs != "" || !strings.Contains(out, "その他のソース 1 件") {
		t.Fatalf("literal query replay rc=%d out=%q stderr=%q argv=%q", rc, out, errs, action.Argv)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("query must not execute: %v", err)
	}
}
