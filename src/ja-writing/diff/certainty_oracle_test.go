package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
	"ja-writing/visibility"
)

// 群 U の段の記録の突き合わせ。

var certaintyHits = map[string]int{}

// certaintySaved は覚えるだけの _certainty_word_spans の呼び出し (名前 → 文)。参照を渡す関数は、同じ文から Go で語を作り直す。
var certaintySaved = map[string]string{}

// checkCertaintyHits は群 U の段の記録が、確度の候補が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkCertaintyHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"certainty:certainty", "certainty:certainty_review", "certainty:(none)", "certainty:strengthened", "certainty:weakened", "certainty:mixed",
		"certainty:counted_by", "change:found", "change:none", "hedge:same", "hedge:strengthen", "hedge:weaken", "hedge:negation", "hedge:question", "hedge:condition", "hedge:other_kinds", "hedge:not_in_table"} {
		if certaintyHits[want] < 20 {
			t.Errorf("群 U の記録が %s に届いた回数は %d (20 回以上が要る)", want, certaintyHits[want])
		}
	}
	t.Logf("群 U の記録の届き方: %v", certaintyHits)
}

func certaintyConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_CERTAINTY_VERB":
		return certaintyVerb
	case "_PREDICATE_END":
		return predicateEnd
	case "_PREDICATE_STEM":
		return predicateStem
	case "_PREDICATE_BEFORE":
		return predicateBefore
	case "_YOTEI":
		return certaintyYotei
	case "_MIKOMI":
		return certaintyMikomi
	case "_APPEARANCE":
		return certaintyAppearing
	case "_CERTAINTY_TABLE":
		rows := make([]pyjson.Value, len(certaintyTable))
		for i, e := range certaintyTable {
			rows[i] = replay.List(int64(e.level), e.name, e.pattern, e.core)
		}
		return rows
	case "_COUNTED_FROM":
		return replay.List(int64(certaintyCountedFrom[0]), int64(certaintyCountedFrom[1]))
	case "_CERTAINTY_NEG_AFTER":
		return lazyValue(certaintyNegAfter)
	case "_CERTAINTY_NEG_FORMS":
		return lazyValue(certaintyNegForms)
	case "_CERTAINTY_QUESTION":
		return lazyValue(certaintyQuestion)
	case "_HEDGE_CONDITION":
		return lazyValue(hedgeCondition)
	case "_HEDGE_CLAUSE_STOP":
		return lazyValue(hedgeClauseStop)
	case "_HEDGE_CONTENT":
		return lazyValue(hedgeContent)
	case "_COUNTED_BY_DOC_TYPE_FROM":
		return replay.Strings(certaintyCountedByDocTypeFrom)
	case "_CERTAINTY_FREQUENCY":
		return lazyValue(certaintyFrequency)
	case "_CERTAINTY_DUTY":
		return lazyValue(certaintyDuty)
	case "COUNTED_TYPES":
		return replay.Strings(countedTypes)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func certWordValue(w *certWord, withEnd bool) pyjson.Value {
	if withEnd {
		return replay.List(int64(w.level), w.name, int64(w.start), w.negative, w.core, int64(w.end))
	}
	return replay.List(int64(w.level), w.name, int64(w.start), w.negative, w.core)
}

func certWordsValue(words []*certWord, withEnd bool) pyjson.Value {
	out := make([]pyjson.Value, len(words))
	for i, w := range words {
		out[i] = certWordValue(w, withEnd)
	}
	return out
}

// certaintySpansOf は、覚えた語 (参照) の文から語を作り直す。
func certaintySpansOf(t *testing.T, ref any) []*certWord {
	t.Helper()
	m, ok := ref.(map[string]any)
	if !ok {
		t.Fatalf("参照でない引数 %T", ref)
	}
	text, ok := certaintySaved[m["$ref"].(string)]
	if !ok {
		t.Fatalf("覚えていない参照 %v", m)
	}
	return certaintyWordSpans(text)
}

func kindsArg(t *testing.T, a any) []string {
	t.Helper()
	list, _ := a.([]any)
	var out []string
	for _, e := range list {
		out = append(out, e.(map[string]any)["kind"].(string))
	}
	return out
}

func intsArg(t *testing.T, a any) map[int]bool {
	t.Helper()
	out := map[int]bool{}
	list, _ := a.([]any)
	for _, e := range list {
		out[argInt(t, e)] = true
	}
	return out
}

// invokeCertainty は群 U の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeCertainty(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_certainty_risks":
		docType := ""
		if a[4] != nil {
			docType = argString(t, a[4])
		}
		v, err := CertaintyRisks(argString(t, build[0]), argString(t, build[1]), docType)
		if err != nil {
			t.Fatalf("_certainty_risks: %v", err)
		}
		items, _ := v.([]pyjson.Value)
		for _, item := range items {
			risk := item.(*pyjson.Object)
			kind, _ := risk.Get("kind")
			dir, _ := risk.Get("subkind")
			certaintyHits["certainty:"+kind.(string)]++
			certaintyHits["certainty:"+dir.(string)]++
			detail, _ := risk.Get("detail")
			if m, _ := detail.(*pyjson.Object).Get("mixed"); m == true {
				certaintyHits["certainty:mixed"]++
			}
			steps, _ := detail.(*pyjson.Object).Get("steps")
			for _, st := range steps.([]pyjson.Value) {
				if _, ok := st.(*pyjson.Object).Get("counted_by"); ok {
					certaintyHits["certainty:counted_by"]++
				}
			}
		}
		if len(items) == 0 {
			certaintyHits["certainty:(none)"]++
		}
		return replay.SortKeys(v)
	case "_certainty_words":
		return certWordsValue(certaintyWordSpans(argString(t, a[0])), false)
	case "_certainty_word_spans":
		return certWordsValue(certaintyWordSpans(argString(t, a[0])), true)
	case "_certainty_negations":
		o := pyjson.NewObject()
		counts := certaintyNegations(argString(t, a[0]), certaintySpansOf(t, a[1]))
		for k, n := range counts {
			o.Set(k, int64(n))
		}
		return replay.SortKeys(o)
	case "_hedge_conditions":
		return int64(hedgeConditions(argString(t, a[0]), certaintySpansOf(t, a[1])))
	case "_hedge_clause":
		set := hedgeClauseChars(argString(t, a[0]), argInt(t, a[1]))
		return charSetOf(set)
	case "_cancel_same_level":
		ro, rn := cancelSameLevel(certaintySpansOf(t, a[0]), certaintySpansOf(t, a[1]))
		return replay.List(certWordsValue(ro, true), certWordsValue(rn, true))
	case "_cancel_pairs":
		ro, rn, pairs := cancelPairs(certaintySpansOf(t, a[0]), certaintySpansOf(t, a[1]))
		ps := make([]pyjson.Value, len(pairs))
		for i, p := range pairs {
			ps[i] = replay.List(certWordValue(p[0], true), certWordValue(p[1], true))
		}
		return replay.List(certWordsValue(ro, true), certWordsValue(rn, true), ps)
	case "_classify_hedge_marks":
		got := ClassifyHedgeMarks(argString(t, a[0]), argString(t, a[1]), kindsArg(t, a[2]), kindsArg(t, a[3]))
		certaintyHits["hedge:"+got]++
		return got
	case "_certainty_change":
		docType := ""
		if a[5] != nil {
			docType = argString(t, a[5])
		}
		r := certaintyChange(argString(t, a[0]), argString(t, a[1]), nil, nil, nil, docType, argBool(t, a[6]))
		if r == nil {
			certaintyHits["change:none"]++
			return nil
		}
		certaintyHits["change:found"]++
		return replay.SortKeys(replay.List(r.kind, r.direction, r.detail()))
	case "_unpaired_neighbors":
		var out []pyjson.Value
		for _, k := range unpairedNeighbors(argInt(t, a[0]), argInt(t, a[1]), intsArg(t, a[2])) {
			out = append(out, int64(k))
		}
		return replay.List(out...)
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

func charSetOf(set map[string]bool) pyjson.Value {
	var chars []string
	for k := range set {
		chars = append(chars, k)
	}
	return replay.Strings(sortedWords(chars))
}

var _ = pyunicode.Runes

// invokeHedgePolarity は _hedge_polarity (語は覚えた語の先頭。語が無いと本家は添字の IndexError で止まる)。
func invokeHedgePolarity(t *testing.T, c oracle.Call) (pyjson.Value, error) {
	t.Helper()
	spans := certaintySpansOf(t, c.Args[2])
	if len(spans) == 0 {
		return nil, &visibility.Error{Type: visibility.TypeIndexError, Message: "list index out of range"}
	}
	return int64(hedgePolarity(argString(t, c.Args[0]), spans[0], spans)), nil
}
