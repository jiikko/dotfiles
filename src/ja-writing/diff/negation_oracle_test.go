package diff

import (
	"slices"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 N・O の Go の表と関数を、本家の記録と突き合わせる道具。

// _WORD_CHAR の本家の正規表現。Go の本体は字の範囲の関数 (isWordChar) で判定するので、同じ字の集合であることを全コードポイントで確かめる (TestWordCharAgreesWithPattern)。
var wordCharPattern = pyre.NewLazy(`[一-龥々〆ヵヶァ-ヴー]`, 0)

func TestWordCharAgreesWithPattern(t *testing.T) {
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		if got, want := isWordChar(r), wordCharPattern.MustMatchAt0(string(r)); got != want {
			t.Fatalf("U+%04X で関数 %v、正規表現 %v", r, got, want)
		}
	}
}

func negationConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_NEGATION":
		return lazyValue(negationMark)
	case "_NEGATION_MASKED":
		return lazyValue(negationMasked)
	case "_ADVERB_MAZU":
		return lazyValue(adverbMazu)
	case "_NEGATION_LIMIT":
		return int64(negationLimit)
	case "_NEGATION_END_RULE":
		return negationEndRule
	case "_END_WINDOW":
		return int64(endWindow)
	case "_PLACE_GAP":
		return int64(placeGap)
	case "_SENTENCE_TAIL":
		return lazyValue(sentenceTail)
	case "_HIRAGANA":
		return lazyValue(hiraganaPattern)
	case "_WORD_CHAR":
		return lazyValue(wordCharPattern)
	case "_WIDEN_RIGHT":
		return int64(negationWidenMax)
	case "_ZU_TO_TE":
		return replay.List(setList(zuToTe[0]), setList(zuToTe[1]))
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func setList(words []string) pyjson.Value { return replay.Strings(sortedWords(words)) }

func swapConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_CASE_WORD":
		return lazyValue(caseWord)
	case "_HONORIFIC":
		return lazyValue(honorific)
	case "_CASE_PARTICLE":
		return lazyValue(caseParticle)
	case "_PASSIVE":
		return lazyValue(passiveForm)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

// intPairs は JSON の [[a, b], ...] の引数 (空でもよい)。
func intPairs(t *testing.T, a any) [][2]int {
	t.Helper()
	if nested, ok := a.([][]int); ok {
		out := make([][2]int, len(nested))
		for i, p := range nested {
			out[i] = [2]int{p[0], p[1]}
		}
		return out
	}
	list, ok := a.([]any)
	if !ok {
		t.Fatalf("リストでない引数 %T", a)
	}
	out := make([][2]int, len(list))
	for i, e := range list {
		pair := e.([]any)
		out[i] = [2]int{argInt(t, pair[0]), argInt(t, pair[1])}
	}
	return out
}

func stringMapArg(t *testing.T, a any) map[string]string {
	t.Helper()
	m, ok := a.(map[string]any)
	if !ok {
		t.Fatalf("dict でない引数 %T", a)
	}
	out := map[string]string{}
	for k, v := range m {
		out[k] = v.(string)
	}
	return out
}

var negationHits = map[string]int{}

func countRiskKinds(v pyjson.Value, prefix string, hits map[string]int) {
	items, _ := v.([]pyjson.Value)
	for _, item := range items {
		triple := item.([]pyjson.Value)
		risk := triple[2].(*pyjson.Object)
		kind, _ := risk.Get("kind")
		hits[prefix+kind.(string)]++
	}
	if len(items) == 0 {
		hits[prefix+"(none)"]++
	}
}

// checkNegationSwapHits は群 N・O の段の記録が、否定が出る組・入れ替えが出る組・何も出ない組のどれにも十分届いていることを確かめる。
func checkNegationSwapHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"negation:negation", "negation:(none)", "swap:swap", "swap:(none)"} {
		if negationHits[want] < 20 {
			t.Errorf("群 N・O の記録が %s に届いた回数は %d (20 回以上が要る)", want, negationHits[want])
		}
	}
	t.Logf("群 N・O の記録の届き方: %v", negationHits)
}

// invokeNegation は群 N・O の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeNegation(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_negation_risks":
		v, err := NegationRisks(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_negation_risks: %v", err)
		}
		countRiskKinds(v, "negation:", negationHits)
		return replay.SortKeys(v)
	case "_swap_risks":
		v, err := SwapRisks(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_swap_risks: %v", err)
		}
		countRiskKinds(v, "swap:", negationHits)
		return replay.SortKeys(v)
	case "_negation_marks":
		return spansValue(negationMarks(argString(t, a[0])))
	case "_negation_flip":
		from, to, ok := negationFlip(argString(t, a[0]), argString(t, a[1]))
		if !ok {
			return nil
		}
		return replay.List(from, to)
	case "_changed_places":
		places := changedPlaces(pyunicode.Runes(argString(t, a[0])), pyunicode.Runes(argString(t, a[1])))
		out := make([]pyjson.Value, len(places))
		for i, p := range places {
			out[i] = replay.List(int64(p[0]), int64(p[1]), int64(p[2]), int64(p[3]))
		}
		return out
	case "_widen":
		return widen(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1]), argInt(t, a[2]))
	case "_covered":
		return int64(covered(argInt(t, a[0]), argInt(t, a[1]), intPairs(t, a[2])))
	case "_unmarked_letters":
		return unmarkedLetters(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1]), argInt(t, a[2]), intPairs(t, a[3]))
	case "_verb_stem_change":
		return int64(verbStemChange(pyunicode.Runes(argString(t, a[0])), pyunicode.Runes(argString(t, a[1]))))
	case "_case_map":
		o := pyjson.NewObject()
		for k, v := range newCaseMap(argString(t, a[0])).particles {
			o.Set(k, v)
		}
		return replay.SortKeys(o)
	case "_passive_rewrite":
		var words []string
		for _, w := range a[4].([]any) {
			words = append(words, w.(string))
		}
		return passiveRewrite(argString(t, a[0]), argString(t, a[1]), mapCase(stringMapArg(t, a[2])), mapCase(stringMapArg(t, a[3])), words)
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

func mapCase(m map[string]string) *caseMap {
	c := &caseMap{particles: m}
	for k := range m {
		c.words = append(c.words, k)
	}
	slices.Sort(c.words)
	return c
}
