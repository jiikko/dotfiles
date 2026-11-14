package diff

import (
	"sort"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 P の Go の表と関数を、本家の記録と突き合わせる道具。

func stringMapValue(m map[string]string) pyjson.Value {
	o := pyjson.NewObject()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.Set(k, m[k])
	}
	return o
}

func questionConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_QUESTION":
		return lazyValue(questionEnd)
	case "_NOT_QUESTION_KA":
		return lazyValue(notQuestionKa)
	case "_FINAL_PERIOD_IN_QUOTE":
		return lazyValue(finalPeriodInQuote)
	case "_SENTENCE_END":
		return lazyValue(sentenceEnd)
	case "_PROTECTED_SPANS":
		return stringMapValue(protectedSubkinds)
	case "_PROTECTED_LINES":
		return stringMapValue(protectedLines)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

var questionHits = map[string]int{}

// countRiskSubkinds は risk の kind/subkind ごとの回数を数える (items は (hunk の番号, 順, risk) の並びか risk の並び)。
func countRiskSubkinds(v pyjson.Value, prefix string, hits map[string]int) {
	items, _ := v.([]pyjson.Value)
	for _, item := range items {
		risk, ok := item.(*pyjson.Object)
		if !ok {
			risk = item.([]pyjson.Value)[2].(*pyjson.Object)
		}
		kind, _ := risk.Get("kind")
		subkind, _ := risk.Get("subkind")
		hits[prefix+kind.(string)+"/"+subkind.(string)]++
	}
	if len(items) == 0 {
		hits[prefix+"(none)"]++
	}
}

// checkQuestionHits は群 P の段の記録が、各 subkind が出る組と何も出ない組のどれにも十分届いていることを確かめる。
func checkQuestionHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{
		"question:sentence_type/question", "question:(none)",
		"protected:protected/code", "protected:protected/inline_code", "protected:protected/url", "protected:protected/link", "protected:protected/frontmatter", "protected:protected/html", "protected:protected/quote", "protected:(none)",
		"marks:sentence_type/marks", "marks:(none)", "modality:modality/permission_lost", "modality:modality/permission_added", "modality:modality_review/ability_lost", "modality:(none)",
		"logic:choice/one_to_all", "logic:choice/all_to_one", "logic:causality/relation_to_cause", "logic:(none)",
	} {
		if questionHits[want] < 20 {
			t.Errorf("群 P の記録が %s に届いた回数は %d (20 回以上が要る)", want, questionHits[want])
		}
	}
	t.Logf("群 P の記録の届き方: %v", questionHits)
}

// changeRisksFor は同じ組への 3 つの呼び出し (印・許可と能力・選ぶ範囲と因果) が 1 回の計算を使い回す (記録の呼び出しの並びは、本家の diff() と同じ 1 つの流れ)。
var lastChange struct {
	orig, rw string
	risks    *ChangeRisks
}

func changeRisksFor(t *testing.T, orig, rw string) *ChangeRisks {
	t.Helper()
	if lastChange.risks == nil || lastChange.orig != orig || lastChange.rw != rw {
		risks, err := ChangeRisksOf(orig, rw)
		if err != nil {
			t.Fatalf("ChangeRisksOf: %v", err)
		}
		lastChange.orig, lastChange.rw, lastChange.risks = orig, rw, risks
	}
	return lastChange.risks
}

// invokeQuestion は群 P の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeQuestion(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_is_question":
		return isQuestion(argString(t, a[0]))
	case "_only_final_period_removed":
		return onlyFinalPeriodRemoved(argString(t, a[0]), argString(t, a[1]))
	}
	orig, rw := argString(t, build[0]), argString(t, build[1])
	switch c.Function {
	case "_question_risks":
		v, err := QuestionRisks(orig, rw)
		if err != nil {
			t.Fatalf("_question_risks: %v", err)
		}
		countRiskSubkinds(v, "question:", questionHits)
		return replay.SortKeys(v)
	case "_protected_risks":
		v, err := ProtectedRisks(orig, rw)
		if err != nil {
			t.Fatalf("_protected_risks: %v", err)
		}
		countRiskSubkinds(v, "protected:", questionHits)
		return replay.SortKeys(v)
	case "_protected_spans":
		_, s, err := buildFullDiff(orig, rw, nil)
		if err != nil {
			t.Fatalf("_protected_spans: %v", err)
		}
		list := s.protectedSpansOf(argInt(t, a[1]))
		spans := make([]pyjson.Value, len(list.spans))
		for i, sp := range list.spans {
			spans[i] = replay.List(int64(sp.left), int64(sp.right), sp.kind)
		}
		return replay.List(spans, replay.MapList(list.lefts, func(n int) pyjson.Value { return int64(n) }))
	case "_find_line":
		_, s, err := buildFullDiff(orig, rw, nil)
		if err != nil {
			t.Fatalf("_find_line: %v", err)
		}
		return optionalLine(s.findLine(argInt(t, a[1]), argString(t, a[2])))
	case "_marks_risks", "_modality_risks", "_logic_risks":
		risks := changeRisksFor(t, orig, rw)
		v := map[string]pyjson.Value{"_marks_risks": risks.Marks, "_modality_risks": risks.Modality, "_logic_risks": risks.Logic}[c.Function]
		countRiskSubkinds(v, map[string]string{"_marks_risks": "marks:", "_modality_risks": "modality:", "_logic_risks": "logic:"}[c.Function], questionHits)
		return replay.SortKeys(v)
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}
