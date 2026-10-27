package report

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"dupscan/internal/tree"
)

func TestSearchTerms(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		want        []string
		bad         bool
	}{
		{name: "preserve originals and lowercase deduplicate", query: " HTTPClient\thttpclient\n接続 HTTP_CLIENT ", want: []string{"HTTPClient", "接続", "HTTP_CLIENT"}},
		{name: "eight distinct", query: "a b c d e f g h", want: []string{"a", "b", "c", "d", "e", "f", "g", "h"}},
		{name: "duplicates do not exhaust term bound", query: "a a a a a a a a a", want: []string{"a"}},
		{name: "unicode lowercase", query: "ÄPFEL äpfel", want: []string{"ÄPFEL"}},
		{name: "no unicode normalization", query: "é e\u0301", want: []string{"é", "e\u0301"}},
		{name: "64 runes", query: strings.Repeat("接", 64), want: []string{strings.Repeat("接", 64)}},
		{name: "512 bytes", query: strings.Repeat("a", 64) + strings.Repeat(" "+strings.Repeat("b", 63), 7), want: []string{strings.Repeat("a", 64), strings.Repeat("b", 63)}},
		{name: "empty", query: " \t\n", bad: true},
		{name: "nine distinct", query: "a b c d e f g h i", bad: true},
		{name: "65 runes", query: strings.Repeat("接", 65), bad: true},
		{name: "513 bytes", query: strings.Repeat("a", 64) + strings.Repeat(" "+strings.Repeat("b", 63), 7) + " ", bad: true},
		{name: "invalid utf8", query: string([]byte{0xff}), bad: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SearchTerms(tt.query)
			if (err != nil) != tt.bad || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SearchTerms = %q, %v; want %q, bad=%v", got, err, tt.want, tt.bad)
			}
		})
	}
}

func TestSearchLexicalEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, query, unitName, signature, doc string
		fields, components                    []string
		score                                 int
	}{
		{name: "camel across separated field tokens", query: "bucketFixture", unitName: "make_fixture_for_bucket", fields: []string{"name"}, components: []string{"bucket", "fixture"}, score: 3},
		{name: "acronym boundary before lowercase", query: "HTTPClient", unitName: "client_http", fields: []string{"name"}, components: []string{"http", "client"}, score: 3},
		{name: "snake query", query: "http_client", unitName: "newHTTPClient", fields: []string{"name"}, components: []string{"http", "client"}, score: 3},
		{name: "substring", query: "buck", unitName: "Buckets", fields: []string{"name"}, components: []string{"buck"}, score: 3},
		{name: "all fields only strongest scores", query: "bucket", unitName: "bucket", signature: "bucket() Bucket", doc: "make bucket", fields: []string{"name", "signature", "doc"}, components: []string{"bucket"}, score: 3},
		{name: "signature", query: "StorageLocation", unitName: "make", signature: "make() -> storage_location", fields: []string{"signature"}, components: []string{"storage", "location"}, score: 2},
		{name: "doc", query: "fixture", unitName: "make", doc: "Fixture for tests", fields: []string{"doc"}, components: []string{"fixture"}, score: 1},
		{name: "Japanese raw substring", query: "保存先", unitName: "テスト保存先作成", fields: []string{"name"}, components: []string{"保存先"}, score: 3},
		{name: "unicode case", query: "Äpfel", unitName: "äpfel", fields: []string{"name"}, components: []string{"äpfel"}, score: 3},
		{name: "components cannot cross fields", query: "bucketFixture", unitName: "bucket", doc: "fixture"},
		{name: "missing acronym component", query: "HTTPClient", unitName: "http_server"},
		{name: "doc later line ignored", query: "fixture", unitName: "make", doc: "create\nfixture"},
		{name: "Japanese no segmentation", query: "保存_先", unitName: "先保存"},
		{name: "no synonyms", query: "connection", unitName: "socket"},
		{name: "no unicode normalization", query: "é", unitName: "e\u0301"},
		{name: "combining marks are not discarded", query: "e\u0301", unitName: "e"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			terms, err := SearchTerms(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			u := &tree.Unit{Kind: tree.KindFunc, Name: tt.unitName, Signature: tt.signature, Doc: tt.doc}
			got := BuildSearch([]*tree.Unit{u}, SearchQuery{Terms: terms})
			if tt.score == 0 {
				if got.Units != 1 || got.Matches != 0 || len(got.Candidates) != 0 {
					t.Fatalf("unexpected hit: %+v", got)
				}
				return
			}
			if got.Matches != 1 || len(got.Candidates) != 1 {
				t.Fatalf("missing hit: %+v", got)
			}
			c := got.Candidates[0]
			want := []SearchEvidence{{Term: tt.query, Fields: tt.fields, Components: tt.components}}
			if c.Score != tt.score || !reflect.DeepEqual(c.Matched, want) || len(c.Missing) != 0 || c.Entry.Calls != nil {
				t.Fatalf("candidate=%+v, want score=%d evidence=%+v", c, tt.score, want)
			}
		})
	}
}

func TestSearchHardFiltersAndScope(t *testing.T) {
	units := []*tree.Unit{
		{Kind: tree.KindFunc, File: "support.swift", Name: "bucketFixture", Signature: "make(connection: Connection) -> StorageLocation", ReturnType: "StorageLocation", ParamTypes: []string{"Connection"}, Doc: "make fixture\nsecret", Visibility: "private"},
		{Kind: tree.KindFunc, File: "tests.swift", Name: "bucketFixture", ReturnType: "StorageLocation", ParamTypes: []string{"Connection"}, Test: true},
		{Kind: tree.KindFunc, File: "alias.swift", Name: "bucketFixture", ReturnType: "PlaceAlias", ParamTypes: []string{"ConnectionAlias"}},
		{Kind: tree.KindFunc, File: "unknown.go", Name: "bucketFixture", Visibility: "unexported"},
		{Kind: tree.KindClosure, File: "closure.swift", Name: "bucketFixture", ReturnType: "StorageLocation", ParamTypes: []string{"Connection"}},
	}
	for _, tt := range []struct {
		name    string
		filters Query
		scope   string
		want    []string
	}{
		{name: "all includes private and unknown", want: []string{"alias.swift", "tests.swift", "support.swift", "unknown.go"}},
		{name: "prod", scope: "prod", want: []string{"alias.swift", "support.swift", "unknown.go"}},
		{name: "test", scope: "test", want: []string{"tests.swift"}},
		{name: "hard AND", filters: Query{Returns: "StorageLocation", ParamType: "Connection", File: "support.swift"}, want: []string{"support.swift"}},
		{name: "unknown and alias types not resolved", filters: Query{Returns: "StorageLocation", ParamType: "Connection"}, want: []string{"tests.swift", "support.swift"}},
		{name: "returns case sensitive", filters: Query{Returns: "storagelocation"}, want: []string{}},
		{name: "param case sensitive", filters: Query{ParamType: "connection"}, want: []string{}},
		{name: "wrong parameter rejects matching return", filters: Query{Returns: "StorageLocation", ParamType: "Bucket"}, want: []string{}},
		{name: "hard name and doc", filters: Query{Name: "BUCKET", Doc: "MAKE"}, want: []string{"support.swift"}},
		{name: "file is exact", filters: Query{File: "support"}, want: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := BuildSearch(units, SearchQuery{Terms: []string{"fixture"}, Filters: tt.filters, Scope: tt.scope})
			files := []string{}
			for _, c := range s.Candidates {
				files = append(files, c.Entry.File)
				if strings.Contains(c.Entry.Doc, "\n") || c.Entry.Calls != nil {
					t.Fatalf("non-search metadata: %+v", c.Entry)
				}
			}
			if s.Units != 4 || s.Matches != len(tt.want) || !reflect.DeepEqual(files, tt.want) {
				t.Fatalf("units=%d files=%q; want %q", s.Units, files, tt.want)
			}
		})
	}
}

func TestSearchCrossFieldORAndMinimum(t *testing.T) {
	u := &tree.Unit{Kind: tree.KindFunc, Name: "bucket", Signature: "make(connection: Connection)", Doc: "fixture"}
	for _, tt := range []struct{ min, matches int }{{0, 1}, {1, 1}, {3, 1}, {4, 0}} {
		got := BuildSearch([]*tree.Unit{u}, SearchQuery{Terms: []string{"bucket", "connection", "fixture", "absent"}, MinMatched: tt.min})
		if got.Matches != tt.matches {
			t.Fatalf("minimum %d: %+v", tt.min, got)
		}
		if tt.matches == 0 {
			continue
		}
		c := got.Candidates[0]
		want := []SearchEvidence{{"bucket", []string{"name"}, []string{"bucket"}}, {"connection", []string{"signature"}, []string{"connection"}}, {"fixture", []string{"doc"}, []string{"fixture"}}}
		if c.Score != 6 || !reflect.DeepEqual(c.Matched, want) || !reflect.DeepEqual(c.Missing, []string{"absent"}) {
			t.Fatalf("evidence: %+v", c)
		}
	}
}

func TestSearchRankingGolden(t *testing.T) {
	// The expected order is literal so a changed ranker cannot regenerate its own oracle.
	units := []*tree.Unit{
		{Kind: tree.KindFunc, File: "all.swift", Name: "connectionBucketFixture", Visibility: "private"},
		{Kind: tree.KindFunc, File: "two.swift", Name: "connectionBucket"},
		{Kind: tree.KindFunc, File: "shared.go", Name: "connection", Test: true, Visibility: "unexported"},
		{Kind: tree.KindFunc, File: "visible_test.go", Name: "connection", Test: true},
		{Kind: tree.KindFunc, File: "private.go", Name: "connection", Visibility: "unexported"},
		{Kind: tree.KindFunc, File: "visible.go", Name: "connection"},
		{Kind: tree.KindFunc, File: "weak.go", Name: "make", Doc: "connection"},
		{Kind: tree.KindFunc, File: "two_doc.go", Name: "make", Doc: "connection bucket"},
		{Kind: tree.KindFunc, File: "z-prefix.go", Name: "newConnection"},
	}
	want := []string{"all.swift", "two.swift", "two_doc.go", "shared.go", "visible.go", "z-prefix.go", "visible_test.go", "private.go", "weak.go"}
	q := SearchQuery{Terms: []string{"connection", "bucket", "fixture"}, Filters: Query{Shared: func(file string) bool { return file == "shared.go" }}}
	for range 2 {
		got := BuildSearch(units, q)
		files := []string{}
		for _, c := range got.Candidates {
			files = append(files, c.Entry.File)
		}
		if !reflect.DeepEqual(files, want) {
			t.Fatalf("order=%q, want %q", files, want)
		}
		for i, j := 0, len(units)-1; i < j; i, j = i+1, j-1 {
			units[i], units[j] = units[j], units[i]
		}
	}
}

func TestSearchExactAndPositionTies(t *testing.T) {
	units := []*tree.Unit{
		{Kind: tree.KindFunc, File: "a.go", Line: 2, Col: 1, Name: "newFixture"},
		{Kind: tree.KindFunc, File: "z.go", Line: 9, Name: "FIXTURE"},
		{Kind: tree.KindFunc, File: "a.go", Line: 1, Col: 8, Name: "makeFixture", Signature: "b"},
		{Kind: tree.KindFunc, File: "a.go", Line: 1, Col: 8, Name: "makeFixture", Signature: "a"},
		{Kind: tree.KindFunc, File: "a.go", Line: 1, Col: 2, Name: "makeFixture", Signature: "z"},
		{Kind: tree.KindFunc, File: "b.go", Name: "makeFixture"},
	}
	want := []string{"z.go:9:0:", "a.go:1:2:z", "a.go:1:8:a", "a.go:1:8:b", "a.go:2:1:", "b.go:0:0:"}
	for range 2 {
		s := BuildSearch(units, SearchQuery{Terms: []string{"fixture"}})
		got := []string{}
		for _, c := range s.Candidates {
			got = append(got, fmt.Sprintf("%s:%d:%d:%s", c.Entry.File, c.Entry.Line, c.Entry.Col, c.Entry.Signature))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order=%q, want %q", got, want)
		}
		for i, j := 0, len(units)-1; i < j; i, j = i+1, j-1 {
			units[i], units[j] = units[j], units[i]
		}
	}
}
