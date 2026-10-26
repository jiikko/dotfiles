package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGateParseFailuresKeepFileCoverage(t *testing.T) {
	for _, language := range []struct{ name, ext, good, broken, partial string }{
		{"swift", ".swift", "func healthy(_ x: Int) -> Int { x + 1 }\n", "func broken(", "func parsed(_ x: Int) -> Int { x + 2 }\nfunc broken("},
		{"go", ".go", "package a\nfunc healthy(x int) int { return x + 1 }\n", "package a\nfunc broken(", "package a\nfunc parsed(x int) int { return x + 2 }\nfunc broken("},
	} {
		for _, partial := range []bool{false, true} {
			shape := "zero_units"
			broken := language.broken
			if partial {
				shape, broken = "partial_units", language.partial
			}
			for _, side := range []string{"base", "head", "both"} {
				for _, commitHead := range []bool{false, true} {
					headKind := "worktree"
					if commitHead {
						headKind = "commit"
					}
					t.Run(language.name+"/"+shape+"/"+side+"/"+headKind, func(t *testing.T) {
						file := "Broken" + language.ext
						baseBody, headBody := language.good, language.good
						if side != "head" {
							baseBody = broken
						}
						if side != "base" {
							headBody = broken
						}
						root := repoWith(t, map[string]string{"Good" + language.ext: language.good, file: baseBody})
						gitRun(t, root, "add", ".")
						gitRun(t, root, "commit", "-qm", "base")
						writeFile(t, root, file, headBody)
						// The malformed head really extracts zero units, or still extracts a valid unit.
						if side != "base" {
							var out, errs bytes.Buffer
							if rc := run([]string{"report", "-format", "json", root}, &out, &errs); rc != 0 {
								t.Fatalf("fixture rc=%d: %s", rc, errs.String())
							}
							var report struct {
								Units int `json:"units"`
							}
							if err := json.Unmarshal(out.Bytes(), &report); err != nil {
								t.Fatal(err)
							}
							if (!partial && report.Units != 1) || (partial && report.Units < 2) {
								t.Fatalf("fixture units=%d partial=%v", report.Units, partial)
							}
						}
						args := []string{"-base", "HEAD", "-format", "json"}
						if commitHead {
							gitRun(t, root, "add", ".")
							gitRun(t, root, "commit", "--allow-empty", "-qm", "head")
							args = []string{"-base", "HEAD~1", "-head", "HEAD", "-format", "json"}
						}
						for _, strict := range []bool{false, true} {
							call := slices.Clone(args)
							if strict {
								call = append(call, "-strict-analysis")
							}
							call = append(call, root)
							rc, out, errs := runGateCmd(t, call...)
							wantRC := 0
							if strict {
								wantRC = 2
							}
							if rc != wantRC || errs != "" {
								t.Fatalf("strict=%v rc=%d want=%d stderr=%s out=%s", strict, rc, wantRC, errs, out)
							}
							var got struct {
								Incomplete bool     `json:"analysis_incomplete"`
								Strict     bool     `json:"strict_analysis"`
								Base       []string `json:"base_files_with_errors"`
								Head       []string `json:"head_files_with_errors"`
							}
							if err := json.Unmarshal([]byte(out), &got); err != nil {
								t.Fatal(err)
							}
							var wantBase, wantHead []string
							if side != "head" {
								wantBase = []string{file}
							}
							if side != "base" {
								wantHead = []string{file}
							}
							if !got.Incomplete || got.Strict != strict || !slices.Equal(got.Base, wantBase) || !slices.Equal(got.Head, wantHead) {
								t.Fatalf("file coverage strict=%v got=%+v want base=%v head=%v", strict, got, wantBase, wantHead)
							}
						}
					})
				}
			}
		}
	}
}

func TestGateParseFailureFormatsAndRecord(t *testing.T) {
	root := repoWith(t, map[string]string{"Good.swift": helperA, "Base.swift": "func broken(", "Head.swift": "func healthy() {}"})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "base")
	writeFile(t, root, "Base.swift", "func healthy() {}")
	writeFile(t, root, "Head.swift", "func broken(")
	for _, format := range []string{"text", "json", "xcode"} {
		t.Run(format, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "gate.jsonl")
			rc, out, errs := runGateCmd(t, "-base", "HEAD", "-strict-analysis", "-format", format, "-record", record, root)
			if rc != 2 || errs != "" {
				t.Fatalf("rc=%d stderr=%s out=%s", rc, errs, out)
			}
			if !strings.Contains(out, "Base.swift") || !strings.Contains(out, "Head.swift") || !strings.Contains(out, "base") || !strings.Contains(out, "head") {
				t.Fatalf("missing file/source: %s", out)
			}
			if format == "text" && (strings.Contains(out, "新しい重複なし") || !strings.Contains(out, "解析が不完全")) {
				t.Fatalf("misleading text: %s", out)
			}
			if format == "xcode" && (!strings.Contains(out, "warning:") || strings.Contains(out, ":1:1:")) {
				t.Fatalf("parse diagnostic must not invent an error position: %s", out)
			}
			b, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Incomplete bool     `json:"analysis_incomplete"`
				Strict     bool     `json:"strict_analysis"`
				Failed     bool     `json:"failed"`
				Base       []string `json:"base_files_with_errors"`
				Head       []string `json:"head_files_with_errors"`
			}
			if strings.Count(string(b), "\n") != 1 {
				t.Fatalf("record is not one line: %q", b)
			}
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if !got.Incomplete || !got.Strict || got.Failed || !slices.Equal(got.Base, []string{"Base.swift"}) || !slices.Equal(got.Head, []string{"Head.swift"}) {
				t.Fatalf("record coverage: %+v", got)
			}
		})
	}
}

func TestGateStrictAnalysisPreservesDetectedViolations(t *testing.T) {
	root := committedRepo(t)
	writeFile(t, root, "Sources/B.swift", helperB)
	writeFile(t, root, "Sources/Broken.swift", "func broken(")
	for _, strict := range []bool{false, true} {
		args := []string{"-base", "HEAD", "-format", "json"}
		want := 1
		if strict {
			args = append(args, "-strict-analysis")
			want = 2
		}
		rc, out, errs := runGateCmd(t, append(args, root)...)
		var got struct {
			Failed     bool              `json:"failed"`
			Violations []json.RawMessage `json:"violations"`
			Incomplete bool              `json:"analysis_incomplete"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if rc != want || errs != "" || !got.Failed || len(got.Violations) == 0 || !got.Incomplete {
			t.Fatalf("strict=%v rc=%d want=%d coverage=%+v stderr=%s", strict, rc, want, got, errs)
		}
	}
}

func TestGateHealthyOutputKeepsLegacyShape(t *testing.T) {
	root := committedRepo(t)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", "-format", "json", root)
	if rc != 0 || errs != "" {
		t.Fatalf("rc=%d stderr=%s", rc, errs)
	}
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &legacy); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"analysis_incomplete", "strict_analysis", "base_files_with_errors", "head_files_with_errors"} {
		if _, ok := legacy[key]; ok {
			t.Fatalf("new field in healthy legacy output: %s", key)
		}
	}
	if rc, _, errs := runGateCmd(t, "-base", "HEAD", "-strict-analysis", root); rc != 0 || errs != "" {
		t.Fatalf("healthy strict rc=%d stderr=%s", rc, errs)
	}
}

func TestGateStrictAnalysisDoesNotHideReadFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read permissionless files")
	}
	root := committedRepo(t)
	file := filepath.Join(root, "Sources", "A.swift")
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(file, 0o644); err != nil {
			t.Error(err)
		}
	})
	rc, out, errs := runGateCmd(t, "-base", "HEAD", "-strict-analysis", root)
	if rc != 2 || !strings.Contains(errs, "A.swift") || !strings.Contains(errs, "判定不能") || out != "" {
		t.Fatalf("read failure rc=%d out=%s stderr=%s", rc, out, errs)
	}
}

func TestGateParseFailureTextLimitKeepsCoverageCounts(t *testing.T) {
	root := committedRepo(t)
	for _, name := range []string{"BrokenA.swift", "BrokenB.swift", "BrokenC.swift"} {
		writeFile(t, root, name, "func broken(")
	}
	for _, limit := range []string{"1", "0"} {
		rc, out, errs := runGateCmd(t, "-base", "HEAD", "-strict-analysis", "-limit", limit, root)
		if rc != 2 || errs != "" || !strings.Contains(out, "base 0 / head 3") || !strings.Contains(out, "BrokenA.swift") {
			t.Fatalf("limit=%s rc=%d stderr=%s out=%s", limit, rc, errs, out)
		}
		if limit == "1" {
			if strings.Contains(out, "BrokenB.swift") || strings.Contains(out, "BrokenC.swift") || !strings.Contains(out, "他2件") {
				t.Fatalf("limited file diagnostics: %s", out)
			}
		} else if !strings.Contains(out, "BrokenB.swift") || !strings.Contains(out, "BrokenC.swift") || strings.Contains(out, "他2件") {
			t.Fatalf("full file diagnostics: %s", out)
		}
	}
}
