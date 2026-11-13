//go:build !race

// 記録との突き合わせの重い検査。1 つの goroutine で順に呼ぶだけで、-race を付けても見つけられるデータ競争は無く、時間だけが数倍になる
// (CI の race の run で lint のパッケージが go test の時間の上限 10 分を超えた)。race の run からは外し、Makefile の test が race なしで別に回す。

package lint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ja-writing/oracle"
	"ja-writing/visibility"
)

// 検出の生の並びの記録 (testdata/golden/lint-raw.json: 入力の集まりの全ファイルを 8 つの文脈で通した、種類・allow・ダッシュの上限で外す前の候補 (内部の欄を含む)) と、
// Go の検出 (detect) を、規則の表 (ruleSet) の 40 規則の全部について、検出の順のまま突き合わせる。
// 規則の表にない規則の候補を Go が出したら落とす。
func TestRawFindingsMatchPythonRecording(t *testing.T) {
	root := workRoot(t)
	var g oracle.LintRawGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintRawFile, &g); err != nil {
		t.Fatal(err)
	}
	cases := oracle.LintRawCases(fixtureCorpus)
	if len(cases) != len(g.Cases) {
		t.Fatalf("case が %d 件、記録が %d 件 (make golden で作り直す)", len(cases), len(g.Cases))
	}
	wordsFolder := "words"
	words, _, err := visibility.ResolveWords(&root, &wordsFolder, false)
	if err != nil {
		t.Fatal(err)
	}
	compared := map[string]int{}
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		if err := rec.CheckCount(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Path)))
		if err != nil {
			t.Fatal(err)
		}
		text, _, derr := visibility.DecodeSourceBytes(data)
		if derr != nil {
			t.Fatal(derr)
		}
		docType, err := visibility.NormalizeDocType(c.DocType)
		if err != nil {
			t.Fatal(err)
		}
		lc := lintContext{docType: docType}
		if c.Words != "" {
			lc.words = words
		}
		s, err := detect(text, lc)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got := canonicalDigest(t, s.metrics.value(), 8); got != rec.Metrics {
			t.Errorf("%s: 文書の指標 (metrics) が本家と違う: %s と %s (Go %v)", c.Name, got, rec.Metrics, s.metrics)
		}
		var got []string
		for _, f := range s.findings {
			if _, ok := ruleSet[f.Rule]; !ok {
				t.Fatalf("%s: 規則の表 (ruleSet) にない規則 %s の候補が出た", c.Name, f.Rule)
			}
			got = append(got, findingToken(t, f))
		}
		want := strings.Fields(rec.Findings)
		if !slices.Equal(got, want) {
			t.Errorf("%s: 検出が本家と違う\n  Go   %v\n  本家 %v", c.Name, got, want)
			continue
		}
		for _, token := range want {
			compared[tokenRule(token)]++
		}
	}
	// 規則ごとに、比べた候補が 1 件以上あること (記録にその規則の当たりが無いと、比べる範囲が空のまま緑になる)
	for rule := range ruleSet {
		if compared[rule] == 0 && !t.Failed() {
			t.Errorf("規則 %s の候補を 1 件も比べていない", rule)
		}
	}
	t.Logf("規則ごとに比べた候補の数: %v", compared)
}
