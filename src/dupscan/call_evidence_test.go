package main

import (
	"dupscan/internal/report"
	"encoding/json"
	"strings"
	"testing"
)

const swiftLabelApproximation = "構文近似: 名前と引数ラベル/個数で照合。型未解決で同名の別定義が混ざりうる。 Swiftの既定引数を省略した呼び出しは取りこぼす。 0件でも未使用の証拠ではない。"
const nameApproximation = "構文近似: 名前のみ (引数ラベル・個数で絞らない)で照合。型未解決で同名の別定義が混ざりうる。 0件でも未使用の証拠ではない。"
const goLabelApproximation = "構文近似: 名前と引数ラベル/個数で照合。型未解決で同名の別定義が混ざりうる。 0件でも未使用の証拠ではない。"

const defaultArgumentCalls = "struct Worker { func run(_ x: Int = 1) {} }\nfunc use(_ w: Worker) { w.run(); w.run(2) }\n"

func TestCallEvidenceExplainsMatchingWithoutChangingRows(t *testing.T) {
	swift := defaultArgumentCalls
	goSource := "package p\nfunc run(x int) {}\nfunc use() { run(2) }\n"
	for _, c := range []struct {
		name, file, source, command, query, note string
		rows                                     int
	}{
		{"swift_index", "A.swift", swift, "index", "run", swiftLabelApproximation, 1},
		{"swift_index_zero", "A.swift", swift, "index", "missing", swiftLabelApproximation, 1},
		{"swift_bare", "A.swift", swift, "callers", "run", nameApproximation, 2},
		{"swift_bare_zero", "A.swift", swift, "callers", "missing", nameApproximation, 1},
		{"swift_label", "A.swift", swift, "callers", "run(_:)", swiftLabelApproximation, 1},
		{"swift_label_zero", "A.swift", swift, "callers", "run(x:)", swiftLabelApproximation, 1},
		{"go_index", "a.go", goSource, "index", "run", goLabelApproximation, 1},
		{"go_bare", "a.go", goSource, "callers", "run", nameApproximation, 1},
		{"go_label", "a.go", goSource, "callers", "run(_:)", goLabelApproximation, 1},
		{"go_label_zero", "a.go", goSource, "callers", "run(x:)", goLabelApproximation, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := repoWith(t, map[string]string{c.file: c.source})
			args := []string{c.command}
			if c.command == "index" {
				args = append(args, "-name")
			}
			args = append(args, c.query, root)
			rc, out, errs := runReport(t, args...)
			got := lines(out)
			if rc != 0 || errs != "" || len(got) != c.rows+1 || got[0] != c.note {
				t.Fatalf("matching explanation/rows: rc=%d out=%q stderr=%q", rc, out, errs)
			}
			if c.name == "swift_index" && !strings.Contains(got[1], "(呼び出し ~1)") {
				t.Fatalf("default omission still counts only explicit argument: %q", out)
			}
			if strings.HasSuffix(c.name, "zero") && !strings.Contains(got[1], "なし") {
				t.Fatalf("zero-result row preserved: %q", out)
			}
		})
	}
}

func TestCallEvidenceJSONRetainsSyntacticCounts(t *testing.T) {
	root := repoWith(t, map[string]string{"A.swift": defaultArgumentCalls})
	rc, out, errs := runReport(t, "index", "-format", "json", "-name", "run", root)
	var index report.IndexSummary
	if err := json.Unmarshal([]byte(out), &index); err != nil || rc != 0 || errs != "" || len(index.Entries) != 1 || index.Entries[0].Calls == nil || *index.Entries[0].Calls != 1 {
		t.Fatalf("legacy index JSON/count: rc=%d error=%v out=%q stderr=%q", rc, err, out, errs)
	}
	for _, c := range []struct {
		query string
		count int
	}{{"run", 2}, {"run(_:)", 1}, {"missing", 0}} {
		rc, out, errs := runReport(t, "callers", "-format", "json", c.query, root)
		var summary report.CallersSummary
		if err := json.Unmarshal([]byte(out), &summary); err != nil || rc != 0 || errs != "" || summary.Matches != c.count || len(summary.Entries) != c.count {
			t.Fatalf("legacy callers JSON/count: rc=%d error=%v out=%q stderr=%q", rc, err, out, errs)
		}
	}
	writeFile(t, root, "B.swift", "struct Other { func run(_ x: Int) {} }\nfunc useOther(_ o: Other) { o.run(3) }\n")
	_, out, _ = runReport(t, "index", "-format", "json", "-name", "run", root)
	if err := json.Unmarshal([]byte(out), &index); err != nil || len(index.Entries) != 2 {
		t.Fatalf("same-name declarations remain separate: error=%v out=%q", err, out)
	}
	for _, entry := range index.Entries {
		if entry.Calls == nil || *entry.Calls != 2 {
			t.Fatalf("same-name mixing remains approximate: %q", out)
		}
	}
}
