package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dupscan/internal/lang"
	"dupscan/internal/norm"
	"dupscan/internal/report"
)

func TestMetadataPlanPreservesAnalysisWithoutCloneBodies(t *testing.T) {
	files := []string{"a.swift", "a.go", "bad.swift", "bad.go"}
	contents := map[string][]byte{
		"a.swift":   []byte("struct A { func total(_ x: Int) -> Int { x + 1 }; func use() { let f = { total(2) }; f() } }"),
		"a.go":      []byte("package a\nfunc total(x int) int { return x+1 };func use(){ f:=func(){total(2)};f() }"),
		"bad.swift": []byte("func broken() { let x =\n"),
		"bad.go":    []byte("package a\nfunc broken() { x :=\n"),
	}
	plan := analysisPlan{Options: lang.Options{Calls: true, Defs: true, Refs: true}}
	metadata, err := parseAll(files, contents, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Normalize = true
	clones, err := parseAll(files, contents, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.units) == 0 || len(metadata.entries) != 0 || len(clones.entries) != len(clones.units) {
		t.Fatalf("metadata must have units without clone entries: metadata=%+v clones=%+v", metadata, clones)
	}
	if !reflect.DeepEqual(metadata.calls, clones.calls) || !reflect.DeepEqual(metadata.definitions, clones.definitions) || !reflect.DeepEqual(metadata.types, clones.types) || !reflect.DeepEqual(metadata.references, clones.references) || !reflect.DeepEqual(metadata.errFiles, clones.errFiles) || len(metadata.errFiles) != 2 {
		t.Fatal("metadata, source order, or parse-error coverage changed with analysis plan")
	}
	if len(metadata.units) != len(clones.units) {
		t.Fatal("unit set changed")
	}
	for i, u := range metadata.units {
		other := *clones.units[i]
		other.Body, other.Delegation, other.SingleCall = nil, false, false
		if u.Body != nil || !reflect.DeepEqual(u, &other) {
			t.Errorf("unit metadata changed: %+v / %+v", u, &other)
		}
	}
	for _, e := range clones.entries {
		for _, lv := range norm.Levels {
			if e.Result.Hash[lv] == "" {
				t.Errorf("clone plan lost %s normalization", lv)
			}
		}
	}
	if _, err := (scanned{}).cloneEntries(); err == nil {
		t.Fatal("metadata-only result must not be accepted as clone absence")
	}
	if _, err := (scanned{normalized: true}).cloneEntries(); err != nil {
		t.Fatal(err)
	}
}

func TestOutlineRetainsCrossFileCallsAndLiveWorktreeChanges(t *testing.T) {
	root := repoWith(t, map[string]string{"A.swift": "func helper(_ x: Int) -> Int { x + 1 }", "B.swift": "func use() { _ = helper(2) }"})
	for _, tc := range []struct {
		name, file, body, remove string
		calls                    int
	}{
		{name: "initial untracked", calls: 1},
		{name: "edit caller", file: "B.swift", body: "func use() { _ = helper(2); _ = helper(3) }", calls: 2},
		{name: "add caller", file: "C.swift", body: "func extra() { _ = helper(4) }", calls: 3},
		{name: "rename caller", file: "D.swift", body: "func extra() { _ = helper(4) }", remove: "C.swift", calls: 3},
		{name: "delete caller", remove: "D.swift", calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.remove != "" {
				if err := os.Remove(filepath.Join(root, tc.remove)); err != nil {
					t.Fatal(err)
				}
			}
			rc, out, errs := runReport(t, "index", "-format", "json", "-file", filepath.Join(root, "A.swift"), root)
			var got report.IndexSummary
			if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 {
				t.Fatalf("rc=%d error=%v stderr=%q", rc, err, errs)
			}
			if got.Matches != 1 || got.Entries[0].Calls == nil || *got.Entries[0].Calls != tc.calls {
				t.Fatalf("file outline lost scope-wide calls: %s", out)
			}
		})
	}
	other := repoWith(t, map[string]string{"A.swift": "func helper(_ x: Int) -> Int { x + 1 }"})
	rc, out, errs := runReport(t, "index", "-format", "json", other)
	var got report.IndexSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil || rc != 0 || got.Matches != 1 || got.Entries[0].Calls == nil || *got.Entries[0].Calls != 0 {
		t.Fatalf("separate root leaked metadata: rc=%d err=%v stderr=%q output=%s", rc, err, errs, out)
	}
}
