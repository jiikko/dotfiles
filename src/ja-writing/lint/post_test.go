package lint

import (
	"slices"
	"strings"
	"testing"

	"ja-writing/oracle"
	"ja-writing/visibility"
)

func injectedFindings(in []oracle.LintPostFinding) []finding {
	out := make([]finding, len(in))
	for i, f := range in {
		out[i] = newFinding(f.Rule, f.Line, f.Severity, f.Message, f.Snippet)
		if f.Words != nil {
			out[i].Words = f.Words
		}
		for _, name := range sortedKeys(f.PatternMatches) {
			out[i].PatternMatches = append(out[i].PatternMatches, patternMatch{Name: name, Strings: f.PatternMatches[name]})
		}
		for _, wt := range f.WordTypes {
			out[i].WordTypes = append(out[i].WordTypes, wordType{Word: wt[0].(string), Number: wt[1].(int)})
		}
		out[i].FlagHits = f.FlagHits
		for _, word := range sortedKeys(f.Replacements) {
			out[i].Replacements = append(out[i].Replacements, wordPair{Word: word, Replacement: f.Replacements[word]})
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// 検出より後の処理 (種類で外す・allow・ダッシュの上限・点数・件数・context・metrics) と出力 (--json の形・render_report) の記録
// (testdata/golden/lint-post.json: 本家の検出を作った候補に差し替えて、後の処理だけを本家のまま走らせたもの) と突き合わせる。
// 検出の群が入る前の Go が、候補のある入力で出力を確かめられる唯一の比較。
func TestPostProcessingMatchesPythonRecording(t *testing.T) {
	root := workRoot(t)
	var g oracle.LintPostGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintPostFile, &g); err != nil {
		t.Fatal(err)
	}
	cases := oracle.LintPostCases(visibility.DocTypeValues(), visibility.ReaderValues())
	if len(cases) != len(g.Cases) {
		t.Fatalf("case が %d 件、記録が %d 件 (make golden で作り直す)", len(cases), len(g.Cases))
	}
	t.Chdir(root)
	pending := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		docType, err := visibility.NormalizeDocType(c.DocType)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := visibility.NormalizeReader(c.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(c.Name, oracle.AllowPartialPrefix) && !implementedRules[c.Inject[0].Rule] {
			// message を作り直す関数は、その規則の群が入るまで panic する stub。入ったら、この case が本家の message と比べる。
			pending++
			continue
		}
		lc := lintContext{docType: docType, reader: reader}
		if c.Words != "" {
			folder := c.Words
			if lc.words, _, err = visibility.ResolveWords(&root, &folder, false); err != nil {
				t.Fatal(err)
			}
		}
		s, err := detect(c.Text, lc)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.findings) != 0 {
			t.Fatalf("%s: 入力から Go が自分で候補を出した (作った候補だけの結果にならない): %v", c.Name, s.findings)
		}
		s.findings = injectedFindings(c.Inject)
		res := s.finish()
		resultText, err := dumpCanonical(res)
		if err != nil {
			t.Fatal(err)
		}
		if got := shortDigest(strings.ReplaceAll(resultText, root, oracle.RootToken)); got != rec.Result {
			t.Errorf("%s: 結果の JSON が本家と違う (%s と %s)\n%.1500s", rec.Name, got, rec.Result, resultText)
		}
		if got := shortDigest(strings.ReplaceAll(renderReport(res), root, oracle.RootToken)); got != rec.Report {
			t.Errorf("%s: render_report が本家と違う (%s と %s)\n%s", rec.Name, got, rec.Report, renderReport(res))
		}
		if res.penaltyScore != rec.Score {
			t.Errorf("%s: 点数が本家と違う: %d と %d", rec.Name, res.penaltyScore, rec.Score)
		}
	}
	if len(cases) < 70 {
		t.Errorf("記録の case が少ない: %d", len(cases))
	}
	t.Logf("allow で語の一部だけを外す case のうち、規則が入るまで比べない %d 件", pending)
}

func dumpCanonical(r *result) (string, error) {
	v := canonicalValue(r.value())
	return dumpCompact(v)
}

// 種類で外す・ダッシュの上限は、検出の記録が空の間も働く部分なので、結果の中身を直接も見る (記録の digest が一致しても、何を外したかが読める形で残す)。
func TestApplyDocTypeAndDashLimit(t *testing.T) {
	mk := func(rule, message string) finding { return newFinding(rule, 1, "warn", message, "") }
	essay, mail := "essay", "mail"
	findings := []finding{
		mk("bare_verb_chain", ""), mk("pause_comma", ""), mk("slide_comma", ""), mk("meta_filler", mailFillerMessage()+"が検出されました。"), mk("meta_filler", "別の前置きが検出されました。"),
		mk("metaphor_label", ""), mk("trailing_colon", ""),
	}
	kept, skipped := applyDocType(findings, &essay)
	var rules []string
	for _, f := range kept {
		rules = append(rules, f.Rule)
	}
	if want := []string{"pause_comma", "meta_filler", "meta_filler", "trailing_colon"}; !slices.Equal(rules, want) {
		t.Errorf("essay で残る規則 %v、期待 %v", rules, want)
	}
	if want := []skipCount{{"bare_verb_chain", 1}, {"metaphor_label", 1}}; !slices.Equal(skipped, want) {
		t.Errorf("essay で外した件数 %v、期待 %v", skipped, want)
	}
	kept, skipped = applyDocType(findings, &mail)
	if len(kept) != 5 || !slices.Equal(skipped, []skipCount{{"emoji_prohibited", 0}, {"meta_filler", 1}}) {
		t.Errorf("mail: 残り %d 件・外した %v", len(kept), skipped)
	}
	kept, skipped = applyDocType(findings, nil)
	if len(skipped) != 0 || len(kept) != 5 {
		t.Errorf("種類なしでは、種類を限る規則 (pause_comma・slide_comma) だけを除く: 残り %d 件・外した %v", len(kept), skipped)
	}
	dashes := make([]finding, 5)
	for i := range dashes {
		dashes[i] = mk("dash_insertion", "")
	}
	shown, hits := applyDashLimit(append([]finding{mk("trailing_colon", "")}, dashes...))
	if len(shown) != 1+dashFindingLimit || hits != 5 {
		t.Errorf("ダッシュの上限: 出す %d 件・数える %d 件", len(shown), hits)
	}
}

func mailFillerMessage() string { return docTypeSkipPatterns["mail"].prefixes[2] }

func TestRulePenaltyScoreBoundaries(t *testing.T) {
	warn := newFinding("trailing_colon", 1, "warn", "", "")
	if got := rulePenaltyScore(nil, 5000, 0); got != 100 {
		t.Errorf("候補なしは 100: %d", got)
	}
	if got := rulePenaltyScore([]finding{warn}, 0, 0); got != 95 {
		t.Errorf("地の文 0 字は 1,000 字として数える: %d", got)
	}
	heavy := slices.Repeat([]finding{warn}, 5000)
	if got := rulePenaltyScore(heavy, 1000, 0); got != 1 {
		t.Errorf("下限は 1: %d", got)
	}
	if got := rulePenaltyScore([]finding{newFinding("pause_comma", 1, "info", "", "")}, 1000000, 0); got != 99 {
		t.Errorf("上限は 99: %d", got)
	}
}
