package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 Z の Go の表と関数を、本家の記録と突き合わせる道具。

// stiffConstantValue は群 Z の本家の定数 (oracle.DiffStiffConstantNames) の、Go の表から作った値。
func stiffConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "STIFFNESS_THRESHOLDS":
		o := pyjson.NewObject()
		o.Set("doc_kanji_pt", float64(stiffDocKanjiPt))
		o.Set("doc_kanji_up", int64(stiffDocKanjiUp))
		o.Set("sent_kanji_pt", float64(stiffSentKanjiPt))
		o.Set("sent_kanji_up", int64(stiffSentKanjiUp))
		o.Set("sent_hira_down", int64(stiffSentHiraDown))
		o.Set("sent_nominal_kanji", int64(stiffSentNominalKanji))
		o.Set("nominal_delta", int64(stiffNominalDelta))
		// no_nouns は本家の中でも読まれない (「の」の連続の正規表現の {2,} が名詞 3 つ以上を表す)ので、Go に表を持たない
		o.Set("no_nouns", int64(3))
		o.Set("no_kango", int64(stiffNoKango))
		return replay.SortKeys(o)
	case "STIFFNESS_INFO_DOC_TYPES":
		return replay.Strings(stiffInfoDocTypes)
	case "_K":
		return stiffKanjiClass
	case "_OKURI_MID":
		return stiffOkuriMid
	case "_OKURI_TAIL":
		return stiffOkuriTail
	case "_NOUN_CHARS":
		return stiffNounChars
	case "_NOUN":
		return stiffNounPattern
	case "_STIFF_KANJI":
		return lazyValue(stiffKanji)
	case "_HIRAGANA_CHARS":
		return lazyValue(stiffHiragana)
	case "_NO_CHAIN":
		return lazyValue(stiffNoChain)
	case "_KANGO":
		return lazyValue(stiffKango)
	case "_NUMERAL_HEAD":
		return lazyValue(stiffNumeralHead)
	case "_LETTER_OR_DIGIT":
		return lazyValue(letterOrDigit)
	case "_LENGTH_PUNCT_PATTERN":
		return lazyValue(lengthPunctPattern)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func optionalDocType(t *testing.T, a any) *string {
	t.Helper()
	if a == nil {
		return nil
	}
	s := argString(t, a)
	return &s
}

// invokeStiff は群 Z の呼び出し 1 つの結果。
func invokeStiff(t *testing.T, c oracle.Call) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "stiffness":
		v, err := Stiffness(argString(t, a[0]), argString(t, a[1]), optionalDocType(t, a[2]))
		if err != nil {
			t.Fatalf("stiffness: %v", err)
		}
		countStiffHits(v)
		return replay.SortKeys(v)
	case "stiffness_metrics":
		v, err := StiffnessMetrics(argString(t, a[0]))
		if err != nil {
			t.Fatalf("stiffness_metrics: %v", err)
		}
		return replay.SortKeys(v)
	case "_measure":
		length, kanji, counted := stiffMeasure(argString(t, a[0]))
		return replay.List(int64(length), int64(kanji), int64(counted))
	case "_ratio":
		return stiffRatio(a[0].(int), a[1].(int))
	case "_nominal_hits":
		return replay.MapList(nominalHits(argString(t, a[0])), func(h nominalHit) pyjson.Value {
			return replay.List(int64(h.start), int64(h.end), h.name)
		})
	case "_no_chains":
		return replay.Strings(noChains(argString(t, a[0])))
	case "_changed_excerpt":
		width := 40
		if len(a) > 2 {
			width = a[2].(int)
		}
		x, y := ChangedExcerpt(argString(t, a[0]), argString(t, a[1]), width)
		return replay.List(x, y)
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

// stiffHits は記録の組に Go の stiffness を通したときの、候補の種類 (と level・候補なし) ごとの数 (Go の答えは記録と同じなので、記録が届く範囲の数え)。
var stiffHits = map[string]int{}

func countStiffHits(v pyjson.Value) {
	o := v.(*pyjson.Object)
	list, _ := o.Get("candidates")
	candidates := list.([]pyjson.Value)
	if len(candidates) == 0 {
		stiffHits["(none)"]++
	}
	for _, c := range candidates {
		co := c.(*pyjson.Object)
		subkind, _ := co.Get("subkind")
		level, _ := co.Get("level")
		stiffHits[subkind.(string)]++
		stiffHits["level:"+level.(string)]++
	}
}

// 空振りを止める: 群 Z の記録の組が、候補の 5 種類と 2 つの level と候補なしの全部に届く。
func checkStiffHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"sentence", "sentence_nominal", "document", "nominalization", "no_chain", "level:info", "level:review", "(none)"} {
		if stiffHits[want] < 20 {
			t.Errorf("群 Z の記録が %s に届いた回数は %d (20 回以上が要る)", want, stiffHits[want])
		}
	}
	t.Logf("群 Z の記録の届き方: %v", stiffHits)
}
