package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
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
