package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

// canonicalValue は json.dumps(sort_keys=True) と同じになるよう、鍵を昇順 (コードポイントの順) に並べ直した値。
func canonicalValue(v pyjson.Value) pyjson.Value {
	switch x := v.(type) {
	case *pyjson.Object:
		keys := x.Keys()
		sort.Strings(keys)
		out := pyjson.NewObject()
		for _, k := range keys {
			inner, _ := x.Get(k)
			out.Set(k, canonicalValue(inner))
		}
		return out
	case []pyjson.Value:
		out := make([]pyjson.Value, len(x))
		for i, e := range x {
			out[i] = canonicalValue(e)
		}
		return out
	}
	return v
}

func dumpCompact(v pyjson.Value) (string, error) { return pyjson.Dumps(v, canonical) }

// canonicalDigest は値の正規の JSON (sort_keys・ensure_ascii・separators=(",", ":")) の SHA-256 の先頭 width 字。
func canonicalDigest(t *testing.T, v pyjson.Value, width int) string {
	t.Helper()
	s, err := pyjson.Dumps(canonicalValue(v), canonical)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:width]
}

// findingToken は検出の生の並びの記録 (py/record_lint_raw.py) の 1 件 (「規則:行:digest」。digest は内部の欄を含む候補の dict の正規の JSON の SHA-256 の先頭 8 字)。
func findingToken(t *testing.T, f finding) string {
	t.Helper()
	return fmt.Sprintf("%s:%d:%s", f.Rule, f.Line, canonicalDigest(t, f.internalValue(), 8))
}

func tokenRule(token string) string { return token[:strings.IndexByte(token, ':')] }

// 検出の生の並びの記録 (testdata/golden/lint-raw.json: 入力の集まりの全ファイルを 8 つの文脈で通した、種類・allow・ダッシュの上限で外す前の候補 (内部の欄を含む)) と、
// Go の検出 (detect) を、入った規則ごとに突き合わせる。入った規則 (implementedRules が true) の候補だけを、検出の順のまま比べる。
// 入っていない規則の候補を Go が出したら、implementedRules を直し忘れているので落とす。
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
	compared := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
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
			if !implementedRules[f.Rule] {
				t.Fatalf("%s: 入っていない規則 %s の候補が出た (implementedRules の %s を true にして、検出を記録と比べる)", c.Name, f.Rule, f.Rule)
			}
			got = append(got, findingToken(t, f))
		}
		var want []string
		for _, token := range strings.Fields(rec.Findings) {
			if implementedRules[tokenRule(token)] {
				want = append(want, token)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: 入った規則の検出が本家と違う\n  Go   %v\n  本家 %v", c.Name, got, want)
			continue
		}
		compared++
	}
	if compared != len(cases) && !t.Failed() {
		t.Errorf("比べた記録が %d 件 (全 %d 件)", compared, len(cases))
	}
}

// 記録に規則 ID ごとに当たる入力が 1 件以上ある (40 規則全部。空振りの規則があれば、規則を入れた群が自分の規則を比べる範囲が空になる)。
// 規則の表 (ruleGroup) が本家の規則の全部であること (記録に出る規則 ID の集合と同じ) も見る。
func TestEveryRuleHasHitsInRawRecording(t *testing.T) {
	var g oracle.LintRawGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintRawFile, &g); err != nil {
		t.Fatal(err)
	}
	hits := map[string]int{}
	for _, rec := range g.Cases {
		for _, token := range strings.Fields(rec.Findings) {
			hits[tokenRule(token)]++
		}
	}
	for rule := range ruleGroup {
		if hits[rule] == 0 {
			t.Errorf("規則 %s に当たる入力が記録にない (入力の集まりに足す。地図 4.3 節)", rule)
		}
	}
	for rule := range hits {
		if _, ok := ruleGroup[rule]; !ok {
			t.Errorf("記録に出た規則 %s が規則の表 (ruleGroup) にない", rule)
		}
	}
	if len(ruleGroup) != 40 || len(implementedRules) != 40 || len(oracle.LintPostRules) != 40 {
		t.Errorf("規則の表の大きさ: ruleGroup %d・implementedRules %d・oracle.LintPostRules %d (どれも 40 のはず)", len(ruleGroup), len(implementedRules), len(oracle.LintPostRules))
	}
	for _, rule := range oracle.LintPostRules {
		if _, ok := ruleGroup[rule]; !ok {
			t.Errorf("oracle.LintPostRules の %s が規則の表にない", rule)
		}
	}
	for rule := range implementedRules {
		if _, ok := ruleGroup[rule]; !ok {
			t.Errorf("implementedRules の %s が規則の表にない", rule)
		}
	}
}

// 群どうしの依存 (G11 → G9・G14 → G8・G17 → G13): 依存先の群の規則が全部入るまで、依存元の群の規則は入れられない。
func TestImplementedRulesRespectGroupDependencies(t *testing.T) {
	if violations := dependencyViolations(); len(violations) > 0 {
		t.Errorf("依存先の群が入っていないのに入れた規則: %v", violations)
	}
}

// 依存の検査は、実際に違反を見つける (G9 の規則を全部外して cleft_list_ending だけを入れた表を作り、同じ判定を通す)。
func TestDependencyCheckDetectsAViolation(t *testing.T) {
	saved := maps.Clone(implementedRules)
	defer func() { implementedRules = saved }()
	for rule, group := range ruleGroup {
		implementedRules[rule] = rule == "cleft_list_ending" && group == "G11"
	}
	if violations := dependencyViolations(); !slices.Equal(violations, []string{"cleft_list_ending"}) {
		t.Errorf("違反は cleft_list_ending だけのはず: %v", violations)
	}
}

func dependencyViolations() []string {
	var out []string
	for _, rule := range sortedRules() {
		if !implementedRules[rule] {
			continue
		}
		for _, dep := range groupDependencies[ruleGroup[rule]] {
			for other, group := range ruleGroup {
				if group == dep && !implementedRules[other] {
					out = append(out, rule)
					break
				}
			}
		}
	}
	return out
}
