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

// 記録に出る規則 ID の集合が、規則の表 (ruleSet・oracle.LintPostRules) と同じで 40 件であること。
// 規則ごとに当たる入力が 1 件以上あることは、TestEveryRuleHasHitAndMissInputsInRecording が文脈ごとに見る。
func TestRawRecordingRuleIDsMatchRuleSet(t *testing.T) {
	var g oracle.LintRawGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintRawFile, &g); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, rec := range g.Cases {
		for _, token := range strings.Fields(rec.Findings) {
			seen[tokenRule(token)] = true
		}
	}
	for rule := range seen {
		if _, ok := ruleSet[rule]; !ok {
			t.Errorf("記録に出た規則 %s が規則の表 (ruleSet) にない", rule)
		}
	}
	for rule := range ruleSet {
		if !seen[rule] {
			t.Errorf("規則 %s が記録のどの入力にも出ない", rule)
		}
	}
	if len(ruleSet) != 40 {
		t.Errorf("規則の表の大きさ %d (40 のはず)", len(ruleSet))
	}
}

// 入力の集まりに当たらない語・型 (SLOP_WORDS の 5 語・METAPHOR_VERB_PATTERNS の 5 型) と、当たりが少ない規則の形を、足した入力 (rawgap/) が 1 行 1 件で当てる。
// 本家の表と入力の数が合わなくなったら (表が変わった・入力を書き換えた)、その入力を作り直す。
func TestGapInputsHitInRawRecording(t *testing.T) {
	var g oracle.LintRawGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintRawFile, &g); err != nil {
		t.Fatal(err)
	}
	counts := func(name string) map[string]int {
		for _, rec := range g.Cases {
			if rec.Name == name {
				out := map[string]int{}
				for _, token := range strings.Fields(rec.Findings) {
					out[tokenRule(token)]++
				}
				return out
			}
		}
		t.Fatalf("記録に %s がない", name)
		return nil
	}
	for name, want := range map[string]map[string]int{
		"rawgap/slop-words.md|none":        {"slop_vocabulary": 5},
		"rawgap/metaphor-verbs.md|none":    {"metaphor_verb": 5},
		"rawgap/heading-forms.md|none":     {"heading_colon": 1, "redundant_bracket": 5},
		"rawgap/count-declaration.md|none": {"count_declaration": 7},
		"rawgap/dialogue-period.md|script": {"dialogue_period": 1},
		"rawgap/dialogue-period.md|none":   {},
	} {
		if got := counts(name); !maps.Equal(got, want) {
			t.Errorf("%s: 規則ごとの件数 %v、期待 %v", name, got, want)
		}
	}
}
