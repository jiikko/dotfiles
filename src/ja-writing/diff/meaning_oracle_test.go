package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 Y の段 (_meaning_risks) と公開の関数 (meaning_risks) の記録の突き合わせ。

var meaningHits = map[string]int{}

func countMeaning(r meaningResult) {
	for _, risk := range r.risks {
		meaningHits["kind:"+riskKind(risk)]++
	}
	if len(r.risks) == 0 {
		meaningHits["(none)"]++
	}
	if len(r.quiet) > 0 {
		meaningHits["quiet"]++
	}
	if r.restored > 0 {
		meaningHits["restored"]++
	}
	if r.filtered > 0 {
		meaningHits["filtered"]++
	}
	if section, ok := r.immutable.Get("candidates"); ok && len(section.([]pyjson.Value)) > 0 {
		meaningHits["immutable"]++
	}
}

func invokeMeaning(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "meaning_risks":
		docType := ""
		if dt, ok := c.Kwargs["doc_type"].(string); ok {
			docType = dt
		}
		result, _, _, err := meaningRisksOf(argString(t, a[0]), argString(t, a[1]), len(a) > 3 && a[3] != nil, docType)
		if err != nil {
			t.Fatalf("meaning_risks: %v", err)
		}
		meaningHits["public"]++
		return replay.SortKeys(riskObjectsValue(result.risks))
	case "_meaning_risks":
		ctx := reviewContextOf(t, build)
		docType := ""
		if dt, ok := a[4].(string); ok {
			docType = dt
		}
		var changes []TypeChange
		var modality []ModalityChange
		var logic []LogicChange
		if a[2] != nil {
			changes, modality, logic = meaningInputs(ctx.old, ctx.new, true)
		}
		result := ctx.s.meaningRisks(ctx.full, ctx.groups, changes, docType, modality, logic)
		ctx.lastMeaning = &result
		countMeaning(result)
		return replay.SortKeys(riskObjectsValue(result.risks))
	case "getattr":
		ctx := reviewContextOf(t, build)
		switch name := argString(t, a[1]); name {
		case "immutable":
			return replay.SortKeys(ctx.lastMeaning.immutable)
		case "quiet_marks":
			return replay.SortKeys(riskObjectsValue(ctx.lastMeaning.quiet))
		default:
			t.Fatalf("知らない属性 %s", name)
		}
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

// checkMeaningHits は群 Y の記録が、各種類の候補と、文の種類の印を出力から外す形・戻す形・状態の候補に写して確度の見直しを取り除く形に届いていることを確かめる。
func checkMeaningHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{
		"kind:number", "kind:negation", "kind:swap", "kind:sentence_type", "kind:protected", "kind:contrast_reversal", "kind:feeling", "kind:certainty", "kind:certainty_review",
		"kind:quote", "kind:state", "kind:promise", "kind:claim", "kind:promise_review", "kind:modality", "kind:modality_review", "kind:choice", "kind:causality", "kind:number_qualifier",
		"(none)", "quiet", "restored", "filtered", "immutable", "public",
	} {
		if meaningHits[want] < 20 {
			t.Errorf("群 Y の記録が %s に届いた回数は %d (20 回以上が要る)", want, meaningHits[want])
		}
	}
	t.Logf("群 Y の記録の届き方: %v", meaningHits)
}
