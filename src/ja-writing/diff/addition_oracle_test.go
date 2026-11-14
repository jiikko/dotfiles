package diff

import (
	"slices"
	"sort"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 V の段と下の関数の記録の突き合わせ。

var additionHits = map[string]int{}

func additionConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	pairs := func(m map[string][]string) pyjson.Value {
		o := pyjson.NewObject()
		for k, v := range m {
			o.Set(k, replay.Strings(v))
		}
		return replay.SortKeys(o)
	}
	switch name {
	case "_ADD_K":
		return addK
	case "_ADD_NEG":
		return addNeg
	case "_ADD_PERF_NEG":
		return addPerfNeg
	case "_ADD_CHU":
		return addChu
	case "_ADD_CHU_COMMA":
		return addChuComma
	case "_ADD_STATE":
		return replay.MapList(addState, func(r addStateRow) pyjson.Value { return replay.List(r.family, r.name, r.pattern) })
	case "_ADD_STATE_ANY":
		return lazyValue(addStateAny)
	case "_ADD_KEEP_EXTRA":
		return pairs(addKeepExtra)
	case "_ADD_KEEP_CROSS":
		return pairs(addKeepCross)
	case "_ADD_COND":
		return lazyValue(addCond)
	case "_ADD_STATE_DENIED":
		return lazyValue(addStateDenied)
	case "_ADD_SEND_OFF_OBJECT":
		return lazyValue(addSendOffObject)
	case "_ADD_DONE":
		return lazyValue(addDone)
	case "_ADD_PROGRESSIVE":
		return addProgressive
	case "_ADD_TOPIC_STOP":
		return replay.Strings(addTopicStop)
	case "_ADD_APOLOGY":
		return lazyValue(addApology)
	case "_ADD_ANY_NEG":
		return lazyValue(addAnyNeg)
	case "_ADD_PERFORMATIVE":
		return lazyValue(addPerformative)
	case "_ADD_REPORT":
		return lazyValue(addReport)
	case "_ADD_REPORT_MAYBE":
		return lazyValue(addReportMaybe)
	case "_ADD_PERF_MARK":
		return lazyValue(addPerfMark)
	case "_ADD_FUTURE_MARK":
		return lazyValue(addFutureMark)
	case "_ADD_LATER_MARK":
		return lazyValue(addLaterMark)
	case "_ADD_LATER_STRONG":
		return lazyValue(addLaterStrong)
	case "_ADD_OTHER_SUBJECT":
		return lazyValue(addOtherSubject)
	case "_ADD_WRITER_MARK":
		return lazyValue(addWriterMark)
	case "_ADD_STATIVE":
		return replay.Strings(addStative)
	case "_ADD_OLD_FUTURE":
		return lazyValue(addOldFuture)
	case "_ADD_PROMISE":
		return replay.MapList(addPromise, func(r addPromiseRow) pyjson.Value { return replay.List(r.sub, r.name, r.pattern) })
	case "_ADD_REQUEST":
		return lazyValue(addRequest)
	case "_ADD_INTRANSITIVE_SAHEN":
		return replay.Strings(addIntransitiveSahen)
	case "_ADD_AGENTIVE_SAHEN":
		return replay.Strings(addAgentiveSahen)
	case "_ADD_E_ROW":
		return lazyValue(addERow)
	case "_ADD_INTRANSITIVE_TAIL":
		return lazyValue(addIntransitiveTail)
	case "_ADD_PAST_END":
		return lazyValue(addPastEnd)
	case "_ADD_OLD_INTRANSITIVE":
		return lazyValue(addOldIntransitive)
	case "_ADD_OLD_STATIVE":
		return lazyValue(addOldStative)
	case "_ADD_PREVENT_ANY":
		return lazyValue(addPreventAny)
	case "_ADD_PREVENT_UNSETTLED":
		return lazyValue(addPreventUnsettled)
	case "_ADD_PREVENT_ACT":
		return lazyValue(addPreventAct)
	case "_ADD_PREVENT_NOT_ACT":
		return lazyValue(addPreventNotAct)
	case "_ADD_GUARANTEE_FAM":
		o := pyjson.NewObject()
		for k, v := range addGuaranteeFamily {
			o.Set(k, v)
		}
		return replay.SortKeys(o)
	case "_ADD_CAUSAL":
		return replay.MapList(addCausal, func(r addCausalRow) pyjson.Value { return replay.List(r.family, r.pattern) })
	case "_ADD_INQUIRY_RESULT":
		return lazyValue(addInquiryResult)
	case "_ADD_TIME_LINK":
		return lazyValue(addTimeLink)
	case "_ADD_CONTENT":
		return lazyValue(addContent)
	case "_ADD_PRED_SKIP":
		return runesValue(slices.Clone(addPredSkip))
	case "_ADD_PRED_SYNONYMS":
		o := pyjson.NewObject()
		for k, v := range addPredSynonyms {
			o.Set(string(k), replay.Strings(v))
		}
		return replay.SortKeys(o)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

// runesValue は字の集まりを、昇順の 1 字の文字列の配列にする (本家の set を昇順にした形)。
func runesValue(rs []rune) pyjson.Value {
	slices.Sort(rs)
	out := make([]pyjson.Value, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

func stringList(t *testing.T, a any) []string {
	t.Helper()
	var out []string
	for _, e := range a.([]any) {
		out = append(out, argString(t, e))
	}
	return out
}

// additionInputRisks は _addition_risks に渡す risks の引数 ($concat) を、Go の段の入力にする。
func additionInputRisks(t *testing.T, c *reviewContext, arg any) []*pyjson.Object {
	t.Helper()
	var out []*pyjson.Object
	for _, part := range arg.(map[string]any)["$concat"].([]any) {
		if m, ok := part.(map[string]any); ok && m["$column"] != nil {
			out = append(out, c.negationRisks()...)
			continue
		}
		switch name := refName(t, part); name {
		case "marks":
			out = append(out, c.marksRiskObjects()...)
		case "cert":
			out = append(out, c.certainty("")...)
		default:
			t.Fatalf("知らない参照 %s", name)
		}
	}
	return out
}

func invokeAddition(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_addition_risks":
		ctx := reviewContextOf(t, build)
		out := ctx.s.additionRisks(ctx.groups, additionInputRisks(t, ctx, a[2]), &[2]*masker{ctx.s.newMasker(0), ctx.s.newMasker(1)})
		for _, r := range out {
			sub, _ := r.Get("subkind")
			key := riskKind(r) + ":" + sub.(string)
			if riskKind(r) == "claim" {
				detail, _ := r.Get("detail")
				reason, _ := detail.(*pyjson.Object).Get("reason")
				key += ":" + reason.(string)
			}
			additionHits[key]++
		}
		if len(out) == 0 {
			additionHits["(none)"]++
		}
		return replay.SortKeys(riskObjectsValue(out))
	case "_addition_state_words":
		return replay.MapList(additionStateWords(argString(t, a[0])), func(w addStateWord) pyjson.Value {
			return replay.List(w.family, w.name, int64(w.pos), w.word)
		})
	case "_addition_topic":
		return replay.Strings(additionTopic(argString(t, a[0]), argInt(t, a[1])))
	case "_addition_on_topic":
		return additionOnTopic(stringList(t, a[0]), argString(t, a[1]))
	case "_addition_stem_neg":
		if s, ok := additionStemNeg(argString(t, a[0])); ok {
			return s
		}
		return nil
	case "_addition_promise_clauses":
		return replay.MapList(additionPromiseClauses(argString(t, a[0])), func(p promiseClause) pyjson.Value {
			return replay.List(p.sub, p.name, p.key, int64(p.pos), p.strong)
		})
	case "_addition_key_head":
		return additionKeyHead(argString(t, a[0]))
	case "_addition_pred_stem":
		return runesValue(additionPredStem(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1])))
	case "_addition_old_futures":
		return replay.MapList(additionOldFutures(argString(t, a[0])), runesValue)
	case "_addition_old_negatives":
		return replay.MapList(additionOldNegatives(argString(t, a[0])), runesValue)
	case "_addition_same_act":
		var stem []rune
		for _, s := range stringList(t, a[0]) {
			stem = append(stem, []rune(s)...)
		}
		return additionSameAct(stem, argString(t, a[1]))
	case "_addition_causal_words":
		return replay.MapList(additionCausalWords(argString(t, a[0])), func(w causalWord) pyjson.Value {
			return replay.List(w.family, w.word, int64(w.start), int64(w.end))
		})
	case "_addition_bigrams":
		grams := make([]string, 0)
		for g := range additionBigrams(argString(t, a[0])) {
			grams = append(grams, g)
		}
		sort.Strings(grams)
		return replay.Strings(grams)
	case "_addition_novelty":
		// 本家は old_bigrams を渡さないと原文の全体を探す (答えは 2 字の組の集まりで調べたときと同じ)
		old := pyunicode.Runes(argString(t, a[1]))
		grams := map[string]bool{}
		for k := 0; k+1 < len(old); k++ {
			grams[pyunicode.FromRunes(old[k:k+2])] = true
		}
		return additionNovelty(argString(t, a[0]), grams)
	case "_addition_time_to_cause":
		return additionTimeToCause(argString(t, a[0]), argString(t, a[1]))
	case "_addition_cause_span":
		return additionCauseSpan(argString(t, a[0]), argString(t, a[1]), argInt(t, a[2]), argInt(t, a[3]))
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

// checkAdditionHits は群 V の記録が、候補が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkAdditionHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{
		"state:lost", "promise:prevention", "promise:guarantee", "promise:added", "promise_review:added",
		"claim:causal:new_content", "claim:causal:joined", "claim:causal:time_to_cause", "(none)",
	} {
		if additionHits[want] < 20 {
			t.Errorf("群 V の記録が %s に届いた回数は %d (20 回以上が要る)", want, additionHits[want])
		}
	}
	t.Logf("群 V の記録の届き方: %v", additionHits)
}

// 入口の関数 AdditionRisks は、記録の突き合わせが使う段の呼び出しと同じ答えを返す。
func TestAdditionEntryPointMatchesStage(t *testing.T) {
	old := diffDocForTest("原因は未確認です。", "確認します。", "遅れました。", "集中した時に速くなった。")
	new := diffDocForTest("原因は対策しました。", "チェックします。", "障害により遅れました。再発防止に努めます。", "集中したことで速くなった。")
	ctx := reviewContextOf(t, []any{old, new})
	got, err := AdditionRisks(old, new, "")
	if err != nil {
		t.Fatal(err)
	}
	risks := slices.Concat(ctx.marksRiskObjects(), ctx.certainty(""), ctx.negationRisks())
	want := riskObjectsValue(ctx.s.additionRisks(ctx.groups, risks, &[2]*masker{ctx.s.newMasker(0), ctx.s.newMasker(1)}))
	if !equalValues(t, got, want) {
		t.Error("AdditionRisks が段と違う")
	}
	if list, _ := got.([]pyjson.Value); len(list) < 3 {
		t.Errorf("AdditionRisks が返した候補が %d 件 (状態・約束・因果のどれも出る入力のはず)", len(list))
	}
}
