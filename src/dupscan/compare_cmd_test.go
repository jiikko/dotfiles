package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const comparisonHelpers = `struct Direct {
 func location(path: String) -> StorageLocation {
  StorageLocation(connectionID: ConnectionID(rawValue: "conn"), namespace: .bucket("bucket"), path: ObjectPath(rawValue: path))
 }
 func changed(path: String) -> StorageLocation {
  StorageLocation(connectionID: ConnectionID(rawValue: "other"), namespace: .bucket("bucket"), path: ObjectPath(rawValue: path))
 }
}
struct Properties {
 let connectionID = ConnectionID(rawValue: "conn")
 let namespace = StorageNamespace.bucket("bucket")
 func location(path: String) -> StorageLocation {
  StorageLocation(connectionID: connectionID, namespace: namespace, path: ObjectPath(rawValue: path))
 }
}
`

func TestExplainCompareShowsDirectValuesAndUnresolvedProperty(t *testing.T) {
	root := repoWith(t, map[string]string{"a.swift": comparisonHelpers, "bad.swift": "func broken() { let x =\n"})
	target := filepath.Join(root, "a.swift") + ":2"
	for _, c := range []struct {
		name, other string
		total       int
		want        string
	}{{"same", ":2", 0, ""}, {"literal", ":5", 1, "other"}, {"property", ":12", 2, "connectionID"}} {
		t.Run(c.name, func(t *testing.T) {
			rc, out, errs := runReport(t, "explain", "-compare", filepath.Join(root, "a.swift")+c.other, "-format", "json", target)
			var s comparisonSummary
			if err := json.Unmarshal([]byte(out), &s); err != nil || rc != 0 || s.Schema != "dupscan.explain.compare.v1" || s.Total != c.total || s.Returned != c.total || s.ParseErrorFiles != 1 || !strings.Contains(s.Limitations, "property initializers unresolved") {
				t.Fatalf("comparison: rc=%d out=%q stderr=%q error=%v", rc, out, errs, err)
			}
			if c.name == "property" && (len(s.Differences[0].LeftLiterals) == 0 || !strings.Contains(s.Differences[0].LeftLiterals[0], "conn")) {
				t.Fatalf("literal value hidden by subtree truncation: %q", out)
			}
			if c.want != "" && !strings.Contains(out, c.want) {
				t.Fatalf("missing direct/unresolved value: %q", out)
			}
		})
	}
	rc, out, errs := runReport(t, "explain", "-compare", filepath.Join(root, "a.swift")+":12", "-limit", "1", target)
	if rc != 0 || !strings.Contains(out, "differences=2 returned=1 truncated=true parse_error_files=1") || !strings.Contains(out, "not source positions") {
		t.Fatalf("bounded text: %d %q %q", rc, out, errs)
	}
	for _, args := range [][]string{{"-compare", "invalid"}, {"-compare", ""}, {"-compare", filepath.Join(root, "a.swift") + ":missing"}, {"-compare", filepath.Join(root, "bad.swift") + ":1"}, {"-compare", target, "-limit", "0"}, {"-compare", target, "-limit", "51"}, {"-compare", target, "-format", "xcode"}} {
		argv := append([]string{"explain"}, args...)
		argv = append(argv, target)
		if rc, out, _ := runReport(t, argv...); rc != 2 || out != "" {
			t.Fatalf("invalid comparison %v: rc=%d out=%q", args, rc, out)
		}
	}
}

func TestExplainCompareRejectsOtherLanguageAndOversizeSources(t *testing.T) {
	root := repoWith(t, map[string]string{"a.swift": "func a() { print(1) }", "b.go": "package b\nfunc b() { print(1) }"})
	target := filepath.Join(root, "a.swift") + ":a"
	rc, out, errs := runReport(t, "explain", "-compare", filepath.Join(root, "b.go")+":b", target)
	if rc != 2 || out != "" || !strings.Contains(errs, "same language") {
		t.Fatalf("language boundary: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	writeFile(t, root, "a.swift", "func a() { print(1) }\n//"+strings.Repeat("x", 1<<20))
	rc, out, errs = runReport(t, "explain", "-compare", target, target)
	if rc != 2 || out != "" || !strings.Contains(errs, "source file exceeds") {
		t.Fatalf("source budget: rc=%d out=%q stderr=%q", rc, out, errs)
	}
}
