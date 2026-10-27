package report

import (
	"bytes"
	"strings"
	"testing"

	"dupscan/internal/tree"
)

func TestGroupedSymbolsPreserveDetailsAndInputOrder(t *testing.T) {
	file := "dir/a\"b.swift"
	refs := ReferencesSummary{Query: "value", Matches: 4, Entries: []ReferenceEntry{
		{File: file, Line: 4, Col: 8, Enclosing: "Box.read()", Kind: "type_position", Text: "let value: Value"},
		{File: file, Line: 4, Col: 20, Text: "value.run()"},
		{File: "z.swift", Line: 2, Col: 3, Text: "value"},
		{File: file, Line: 1, Col: 6, Test: true, Enclosing: "testValue()", Text: "value"},
	}}
	var got bytes.Buffer
	if err := ReferencesTextGrouped(&got, refs, 0, "more"); err != nil {
		t.Fatal(err)
	}
	want := "\"dir/a\\\"b.swift\"\n  4:8 Box.read() [型の位置] | let value: Value\n  4:20 (単位の外) [式の位置] | value.run()\n\"z.swift\"\n  2:3 (単位の外) [式の位置] | value\n\"dir/a\\\"b.swift\"\n  1:6 testValue() [test] [式の位置] | value\n"
	if got.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got.String(), want)
	}
	callers := CallersSummary{Query: "run", Matches: 3, Entries: []CallerEntry{
		{File: "same.swift", Line: 8, Col: 9, Caller: "Box.run()", Text: "run()"},
		{File: "same.swift", Line: 9, Col: 2, Text: "run()"},
		{File: "same.swift", Line: 10, Col: 4, Test: true, Caller: "testRun()", Text: "run()"},
	}}
	got.Reset()
	if err := CallersTextGrouped(&got, callers, 0, "more"); err != nil {
		t.Fatal(err)
	}
	want = "\"same.swift\"\n  8:9 Box.run() | run()\n  9:2 (単位の外) | run()\n\"same.swift\"\n  10:4 testRun() [test] | run()\n"
	if got.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got.String(), want)
	}
}

func TestGroupedSymbolsLimitCountsEntriesWithoutEmptyHeadings(t *testing.T) {
	var got bytes.Buffer
	refs := ReferencesSummary{Matches: 3, Entries: []ReferenceEntry{{File: "a.swift", Line: 1, Col: 1}, {File: "a.swift", Line: 2, Col: 3}, {File: "hidden.swift", Line: 3, Col: 1}}}
	if err := ReferencesTextGrouped(&got, refs, 2, "refs follow-up"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(got.String(), "\"a.swift\"") != 1 || strings.Contains(got.String(), "hidden.swift") || !strings.HasSuffix(got.String(), "… 他 1 件 (refs follow-up)\n") {
		t.Fatal(got.String())
	}
	got.Reset()
	callers := CallersSummary{Matches: 2, Entries: []CallerEntry{{File: "a.swift", Line: 1, Col: 1}, {File: "hidden.swift", Line: 2, Col: 1}}}
	if err := CallersTextGrouped(&got, callers, 1, "caller follow-up"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String(), "hidden.swift") || !strings.HasSuffix(got.String(), "… 他 1 件 (caller follow-up)\n") {
		t.Fatal(got.String())
	}
}

func TestGroupedSymbolsKeepEmptyMessages(t *testing.T) {
	var got bytes.Buffer
	if err := ReferencesTextGrouped(&got, ReferencesSummary{Query: "missing"}, 1, "more"); err != nil {
		t.Fatal(err)
	}
	if got.String() != "dupscan refs: missing の参照なし\n" {
		t.Fatal(got.String())
	}
	got.Reset()
	if err := CallersTextGrouped(&got, CallersSummary{Query: "missing"}, 1, "more"); err != nil {
		t.Fatal(err)
	}
	if got.String() != "dupscan callers: missing の呼び出しなし\n" {
		t.Fatal(got.String())
	}
}

func TestEvidenceGroupedPreservesSectionsCountsDetailsAndLimits(t *testing.T) {
	s := EvidenceSummary{DocLines: 4, Called: []CalledName{{Name: "first", Lines: 5}, {Name: "second", Lines: 3}, {Name: "hidden", Lines: 1}},
		Doc:    []EvidenceEntry{{File: "a.swift", Line: 1, Unit: "Box.read()", Text: "/// word", Hits: 3}, {File: "a.swift", Line: 5, Text: "/// word", Hits: 1}},
		Test:   []EvidenceEntry{{File: "same.swift", Line: 8, Unit: "testRead()", Text: "read(word)", Calls: []string{"read", "verify"}}},
		Source: []EvidenceEntry{{File: "same.swift", Line: 2, Text: "word"}, {File: "same.swift", Line: 3, Unit: strings.Repeat("界", 81), Text: "read(word)", Calls: []string{"read"}}, {File: "hidden.swift", Line: 4, Text: "hidden"}},
	}
	var got bytes.Buffer
	if err := EvidenceTextGrouped(&got, s, 2); err != nil {
		t.Fatal(err)
	}
	text := got.String()
	for _, part := range []string{
		"first (5 行)\nsecond (3 行)\n… 他 1 個 (-limit 0 で全部)",
		"## doc に語がある単位 (2 個 / doc の 4 行)\n\"a.swift\"\n  1 Box.read() | /// word  (doc の 3 行に語がある)\n  5 (単位の外) | /// word",
		"## テストの行 (1 件)\n\"same.swift\"\n  8 testRead() | read(word)  → 呼び出し: read, verify",
		"## その他のソースの行 (3 件。コメント・文字列を含む)\n\"same.swift\"\n  2 (単位の外) | word",
		"  3 " + strings.Repeat("界", 80) + "… | read(word)  → 呼び出し: read\n… 他 1 件 (-limit 0 で全部)",
	} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in:\n%s", part, text)
		}
	}
	if strings.Contains(text, "hidden.swift") || strings.Count(text, "\"same.swift\"") != 2 {
		t.Fatal(text)
	}
	got.Reset()
	if err := EvidenceTextGrouped(&got, s, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), "\"hidden.swift\"\n  4 (単位の外) | hidden") || strings.Contains(got.String(), "… 他") {
		t.Fatal(got.String())
	}
}

func TestEvidenceMetadataIdentifiesDocSymbolAndActualShortening(t *testing.T) {
	for _, tt := range []struct {
		name, text, signature string
		want                  bool
	}{
		{"short", "/// word", "read()", false},
		{"long text", "/// word" + strings.Repeat("界", 160), "read()", true},
		{"long unit", "/// word", strings.Repeat("界", 81), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			units := []*tree.Unit{{File: "a.swift", Name: "read", Signature: tt.signature, Line: 2, EndLine: 3, DocStart: 1, DocEnd: 1}}
			s := BuildEvidence("word", map[string][]byte{"a.swift": []byte(tt.text + "\nfunc read() {}")}, units, nil, nil, func(string) bool { return false })
			if len(s.Doc) != 1 || s.Doc[0].Symbol != "read" || s.Truncated != tt.want {
				t.Fatalf("got %+v", s)
			}
		})
	}
	s := BuildEvidence("word", map[string][]byte{"a.swift": []byte("word " + strings.Repeat("界", 160))}, nil, nil, nil, func(string) bool { return false })
	if !s.Truncated {
		t.Fatalf("source shortening missing: %+v", s)
	}
}
