package lint

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"ja-writing/oracle"
)

// 検出が文脈 (文書の種類・語の一覧) で変わる規則が、候補を出す文脈。ここに無い規則は、全部の文脈 (oracle.LintRawContexts) で候補を出す。
// 記録の文脈の選び方 (oracle.LintRawContext の説明) と本家の種類ごとの扱いに合わせてある。
// 記録を作り直して当たる文脈が変わったら、この表と突き合わせて落ちる (文脈が減っても増えても気づく)。
var ruleOwnContexts = map[string][]string{
	"dialogue_period":   {"script"},
	"narrative_density": {"essay"},
	"pause_comma":       {"article", "essay", "minutes", "slides"},
	"project_term":      {"words"},
	"slide_comma":       {"slides"},
}

type inputCounts struct{ hit, miss int }

// countRuleInputs は記録の case (名前は「入力|文脈」) を、規則 ID と文脈ごとに「その規則の候補が出た入力」と「出なかった入力」に数える。
// 出なかった入力は、その規則が候補を出す文脈の中だけで数える (種類の切り替えで規則が働かない文脈の入力を、当たらない入力に数えない)。
func countRuleInputs(cases []oracle.LintRawRecord) map[string]map[string]*inputCounts {
	hitNames := map[string]map[string]map[string]bool{} // 規則 → 文脈 → 入力
	totals := map[string]int{}                          // 文脈 → 入力の数
	for _, rec := range cases {
		cut := strings.LastIndexByte(rec.Name, '|')
		name, ctx := rec.Name[:cut], rec.Name[cut+1:]
		totals[ctx]++
		for _, token := range strings.Fields(rec.Findings) {
			rule := tokenRule(token)
			if hitNames[rule] == nil {
				hitNames[rule] = map[string]map[string]bool{}
			}
			if hitNames[rule][ctx] == nil {
				hitNames[rule][ctx] = map[string]bool{}
			}
			hitNames[rule][ctx][name] = true
		}
	}
	out := map[string]map[string]*inputCounts{}
	for rule, byCtx := range hitNames {
		out[rule] = map[string]*inputCounts{}
		for ctx, names := range byCtx {
			out[rule][ctx] = &inputCounts{hit: len(names), miss: totals[ctx] - len(names)}
		}
	}
	return out
}

// ruleInputProblems は、rules の各規則が、候補を出す文脈ごとに「当たる入力」と「当たらない入力」を 1 件以上持つか、
// 規則が候補を出す文脈の集合が表 (ruleOwnContexts と全文脈) と同じかを見て、違う所を返す。
func ruleInputProblems(rules []string, counts map[string]map[string]*inputCounts) []string {
	var all []string
	for _, c := range oracle.LintRawContexts() {
		all = append(all, c.Name)
	}
	var problems []string
	for _, rule := range rules {
		want := all
		if own, ok := ruleOwnContexts[rule]; ok {
			want = own
		}
		got := counts[rule]
		for _, ctx := range want {
			c := got[ctx]
			switch {
			case c == nil: // 当たった入力がある文脈しか countRuleInputs は作らない
				problems = append(problems, fmt.Sprintf("規則 %s は文脈 %s で当たる入力が 0 件 (比べる範囲が空)", rule, ctx))
			case c.miss == 0:
				problems = append(problems, fmt.Sprintf("規則 %s は文脈 %s で当たらない入力が 0 件 (全部の入力に当たる。入力を足す)", rule, ctx))
			}
		}
		for ctx := range got {
			if !slices.Contains(want, ctx) {
				problems = append(problems, fmt.Sprintf("規則 %s が表にない文脈 %s で候補を出した (ruleOwnContexts と記録を合わせる)", rule, ctx))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func sortedRuleIDs() []string {
	rules := make([]string, 0, len(ruleGroup))
	for r := range ruleGroup {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	return rules
}

func readRawGolden(t *testing.T) oracle.LintRawGolden {
	t.Helper()
	var g oracle.LintRawGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintRawFile, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// 規則 ID ごとに、当たる入力と当たらない入力が、記録に 1 件以上ある (件数は t.Log に出す)。
// 当たらない入力が 0 件の規則は、何にでも当たる検出を「全件一致」と見分けられない。
func TestEveryRuleHasHitAndMissInputsInRecording(t *testing.T) {
	g := readRawGolden(t)
	counts := countRuleInputs(g.Cases)
	for _, p := range ruleInputProblems(sortedRuleIDs(), counts) {
		t.Error(p)
	}
	for _, rule := range sortedRuleIDs() {
		var parts []string
		var ctxs []string
		for ctx := range counts[rule] {
			ctxs = append(ctxs, ctx)
		}
		sort.Strings(ctxs)
		for _, ctx := range ctxs {
			parts = append(parts, fmt.Sprintf("%s 当たる%d・当たらない%d", ctx, counts[rule][ctx].hit, counts[rule][ctx].miss))
		}
		t.Logf("%s: %s", rule, strings.Join(parts, " / "))
	}
}

// 検査自身が、当たる入力が空・当たらない入力が空・表にない文脈で当たる、の 3 形を落とす。記録の写しから、規則 1 つの候補を消す・全部の入力に足す・別の文脈に足す。
func TestRuleInputCheckRejectsEmptySides(t *testing.T) {
	g := readRawGolden(t)
	const rule = "calque"
	token := rule + ":1:00000000"
	mutate := func(f func(i int, rec *oracle.LintRawRecord)) []string {
		cases := slices.Clone(g.Cases)
		for i := range cases {
			f(i, &cases[i])
		}
		return ruleInputProblems([]string{rule}, countRuleInputs(cases))
	}
	if p := ruleInputProblems([]string{rule}, countRuleInputs(g.Cases)); len(p) != 0 {
		t.Fatalf("記録のままで問題が出た: %v", p)
	}
	noHits := mutate(func(_ int, rec *oracle.LintRawRecord) {
		var keep []string
		for _, f := range strings.Fields(rec.Findings) {
			if tokenRule(f) != rule {
				keep = append(keep, f)
			}
		}
		rec.Findings = strings.Join(keep, " ")
	})
	if len(noHits) != 8 || !strings.Contains(noHits[0], "当たる入力が 0 件") {
		t.Errorf("当たる入力が空の形を 8 文脈で落とすはず: %v", noHits)
	}
	noMiss := mutate(func(_ int, rec *oracle.LintRawRecord) {
		if !strings.Contains(" "+rec.Findings+" ", " "+rule+":") {
			rec.Findings += " " + token
		}
	})
	if len(noMiss) != 8 || !strings.Contains(noMiss[0], "当たらない入力が 0 件") {
		t.Errorf("当たらない入力が空の形を 8 文脈で落とすはず: %v", noMiss)
	}
	// dialogue_period は script だけで当たる。別の文脈に足したら落ちる。
	cases := slices.Clone(g.Cases)
	for i := range cases {
		if strings.HasSuffix(cases[i].Name, "|none") {
			cases[i].Findings += " dialogue_period:1:00000000"
			break
		}
	}
	if p := ruleInputProblems([]string{"dialogue_period"}, countRuleInputs(cases)); len(p) != 1 || !strings.Contains(p[0], "表にない文脈 none") {
		t.Errorf("表にない文脈で当たった形を落とすはず: %v", p)
	}
}
