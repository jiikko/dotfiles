package diff

import (
	"sort"
	"strconv"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 M の Go の表と関数を、本家の記録と突き合わせる道具。

// 本家の _KANJI・_KANA_NUMBER_KATAKANA・_HIRAGANA_CHAR の正規表現。Go の本体は字の範囲の関数 (isKanjiChar・isKatakanaChar・isHiraganaChar) で判定するので、
// 同じ字の集合であることを全コードポイントで確かめる (TestRunePredicatesAgreeWithPatterns)。
var (
	kanjiAnyPattern = pyre.NewLazy(`[一-龥々〆ヵヶ]`, 0)
	katakanaPattern = pyre.NewLazy(`[ァ-ヴー]`, 0)
	hiraganaPattern = pyre.NewLazy(`[ぁ-ゖ]`, 0)
)

func TestRunePredicatesAgreeWithPatterns(t *testing.T) {
	for _, c := range []struct {
		name string
		pred func(rune) bool
		re   *pyre.Lazy
	}{{"漢字", isKanjiChar, kanjiAnyPattern}, {"片仮名", isKatakanaChar, katakanaPattern}, {"平仮名", isHiraganaChar, hiraganaPattern}} {
		for r := rune(0); r <= 0x10FFFF; r++ {
			if r >= 0xD800 && r <= 0xDFFF {
				continue
			}
			if got, want := c.pred(r), c.re.MustMatchAt0(string(r)); got != want {
				t.Fatalf("%s: U+%04X で関数 %v、正規表現 %v", c.name, r, got, want)
			}
		}
	}
}

// charSet は文字列の字の集合を、本家の set のとおり昇順の並びにした値 (本家は集合を sorted の並びで JSON にする)。
func charSet(s string) pyjson.Value {
	var out []string
	for _, r := range pyunicode.Runes(s) {
		out = append(out, string(r))
	}
	sort.Strings(out)
	return replay.Strings(out)
}

func sortedSet(set map[string]bool) pyjson.Value {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return replay.Strings(out)
}

func stringMap(m map[string]string) pyjson.Value {
	o := pyjson.NewObject()
	for k, v := range m {
		o.Set(k, v)
	}
	return replay.SortKeys(o)
}

func listMap[K comparable](m map[K][]string, key func(K) string) pyjson.Value {
	o := pyjson.NewObject()
	for k, v := range m {
		o.Set(key(k), replay.Strings(v))
	}
	return replay.SortKeys(o)
}

func wordSets(m map[string][]string) pyjson.Value {
	return listMap(m, func(k string) string { return k })
}

func sortedWords(words []string) []string {
	out := append([]string(nil), words...)
	sort.Strings(out)
	return out
}

// numberConstantValue は群 M の本家の定数 (oracle.DiffNumberConstantNames) の、Go の表から作った値。
func numberConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	runeKey := func(r rune) string { return string(r) }
	switch name {
	case "_ARABIC":
		return lazyValue(arabicNumber)
	case "_EMPHASIS":
		return lazyValue(emphasisMark)
	case "_THOUSANDS":
		return lazyValue(thousandsRun)
	case "_UNITS":
		return replay.Strings(numberUnits)
	case "_UNIT_NOT_BEFORE":
		return stringMap(unitNotBefore)
	case "_UNIT":
		return lazyValue(unitPattern)
	case "_SPACED_UNIT":
		return lazyValue(spacedUnit)
	case "_LETTER_UNIT":
		return lazyValue(letterUnit)
	case "_THOUSAND":
		return lazyValue(thousandMark)
	case "_SAME_UNIT":
		return stringMap(sameUnit)
	case "_KANJI_COUNTERS":
		return replay.Strings(kanjiCounters)
	case "_KANJI_NUMBER":
		return lazyValue(kanjiNumber)
	case "_KANJI_DIGITS":
		o := pyjson.NewObject()
		for r, n := range kanjiDigits {
			o.Set(string(r), int64(n))
		}
		return replay.SortKeys(o)
	case "_KANJI_UNITS":
		o := pyjson.NewObject()
		for r, n := range kanjiUnits {
			o.Set(string(r), int64(n))
		}
		return replay.SortKeys(o)
	case "_KANJI_BEFORE":
		return charSet(kanjiBefore)
	case "_KANJI_BEFORE_WORDS":
		return sortedSet(kanjiBeforeWords)
	case "_TIME_OF_DAY_WORDS":
		return sortedSet(timeOfDayWords)
	case "_TIME_OF_DAY_CHARS":
		return charSet(timeOfDayChars)
	case "_ICHIJI_HOUR_NEXT":
		return charSet(ichijiHourNext)
	case "_KANJI":
		return lazyValue(kanjiAnyPattern)
	case "_WHOLE_PERIOD_WORDS":
		return sortedSet(wholePeriodWords)
	case "_SAME_PERIOD":
		return listMap(samePeriod, runeKey)
	case "_KANJI_CHAIN":
		return lazyValue(kanjiChain)
	case "_KANJI_HOUR_BEFORE":
		return lazyValue(kanjiHourBefore)
	case "_JIPPUN_AFTER":
		return replay.Strings(jippunAfter)
	case "_JIPPUN_DE":
		return "で"
	case "_JIPPUN_DE_NOT":
		return replay.Strings(jippunDeNot)
	case "_ICHIJI_JIPPUN_AFTER":
		return replay.Strings(ichijiJippunAfter)
	case "_KANJI_CHAIN_REACH":
		return int64(kanjiChainReach)
	case "_KANJI_EXCLUDED":
		return lazyValue(kanjiExcluded)
	case "_KANJI_NUMERALS":
		return charSet(kanjiNumerals)
	case "_BUN_NOT_BEFORE":
		return replay.Strings(bunNotBefore)
	case "_NIBUN_NOT_BEFORE":
		return replay.Strings(nibunNotBefore)
	case "_JUNIBUN_NOT_BEFORE":
		return replay.Strings(junibunNotBefore)
	case "_JOU_NOT_BEFORE":
		return replay.Strings(jouNotBefore)
	case "_KANJI_RANGE_HEAD":
		return lazyValue(kanjiRangeHead)
	case "_KANJI_RANGE_REACH":
		return int64(kanjiRangeReach)
	case "_NOTATION_JOIN":
		return lazyValue(notationJoin)
	case "_JOIN_RANGE":
		return lazyValue(joinRange)
	case "_DATE_UNITS":
		return replay.Strings(dateUnits)
	case "_TIME_UNITS":
		return replay.Strings(timeUnits)
	case "_NEXT_UNIT":
		return stringMap(nextUnit)
	case "_NOTATION_SPACE":
		return lazyValue(notationSpace)
	case "_WEAK_ONE_AFTER":
		return replay.Strings(weakOneAfter)
	case "_WEAK_ONE_BEFORE":
		return weakOneBefore
	case "_WEAK_ONE_REPEATED":
		return replay.Strings(weakOneRepeated)
	case "_WEAK_ONE_MO":
		return lazyValue(weakOneMo)
	case "_WEAK_ONE_PAIRED":
		return replay.Strings(weakOnePaired)
	case "_WEAK_ONE_ONLY":
		return weakOneOnly
	case "_WEAK_SINGLE":
		return weakSingle
	case "_WEAK_EACH":
		return weakEach
	case "_KANA_NUMBERS":
		o := pyjson.NewObject()
		for k, v := range kanaNumbers {
			o.Set(k, replay.Strings(v[:]))
		}
		return replay.SortKeys(o)
	case "_KANA_NUMBER":
		return lazyValue(kanaNumber)
	case "_KANA_NUMBER_NOT_NEXT":
		return lazyValue(kanaNumberNotNext)
	case "_KANA_NUMBER_NEXT_STRICT":
		return replay.Strings(kanaNumberNextStrict)
	case "_KANA_NUMBER_STRICT":
		return sortedSet(kanaNumberStrict)
	case "_KANA_NUMBER_NOT_KANJI_NEXT":
		m := map[string]string{}
		for k, v := range kanaNumberNotKanjiNext {
			m[k] = string(v)
		}
		return stringMap(m)
	case "_KANA_NUMBER_BEFORE":
		return charSet(kanaNumberBefore)
	case "_KANA_NUMBER_KATAKANA":
		return lazyValue(katakanaPattern)
	case "_HIRAGANA_CHAR":
		return lazyValue(hiraganaPattern)
	case "_KANA_ONLY":
		return kanaOnly
	case "_QUANTITY_WORDS":
		return lazyValue(quantityWords)
	case "_QUANTITY_SAME":
		return stringMap(quantitySame)
	case "_QUANTITY_CLAUSE_WORDS":
		return setMap(quantityClauseWords)
	case "_QUANTITY_FOR":
		return setMap(quantityFor)
	case "_QUANTITY_KEEP_ANY":
		o := pyjson.NewObject()
		for k, l := range quantityKeepAny {
			o.Set(k, lazyValue(l))
		}
		return replay.SortKeys(o)
	case "_QUANTITY_OTHER_NOUN":
		return lazyValue(quantityOtherNoun)
	case "_QUANTITY_TOPIC":
		return lazyValue(quantityTopic)
	case "_QUANTITY_SUBJECT":
		return lazyValue(quantitySubject)
	case "_QUANTITY_CLAUSE_END":
		return lazyValue(quantityClauseEnd)
	case "_QUANTITY_NEG":
		return lazyValue(quantityNeg)
	case "_QUANTITY_LEAD":
		return lazyValue(quantityLead)
	case "_QUANTITY_SCOPE_END":
		return lazyValue(quantityScopeEnd)
	case "_QUANTITY_NOT_NEG":
		return lazyValue(quantityNotNeg)
	case "_QUANTITY_LIMIT_OFF":
		return lazyValue(quantityLimitOff)
	case "_QUANTITY_SENTENCE_END":
		return lazyValue(quantitySentenceEnd)
	case "_QUANTITY_CLAUSE_SPLIT":
		return lazyValue(quantityClauseSplit)
	case "_QUANTITY_PARTICLE_END":
		return lazyValue(quantityParticleEnd)
	case "_QUANTITY_HEAD":
		return lazyValue(quantityHead)
	case "_QUANTITY_KEEP_BEFORE":
		return stringMap(quantityKeepBefore)
	case "_QUANTITY_CHANGE_BEFORE":
		return stringMap(quantityChangeBefore)
	case "_NEGATION_WORDS":
		return lazyValue(negationWords)
	case "_ZU_PREV":
		return charSet(zuPrev)
	case "_ZU_NEXT":
		return lazyValue(zuNext)
	case "_LIST_NUMBER":
		return lazyValue(listNumber)
	case "_NUMBER_MASKED":
		return replay.Strings(numberMaskedKindList)
	case "_LEAD_MODIFIERS":
		return replay.Strings(leadModifiers)
	case "_CLAUSE_END":
		return lazyValue(clauseEnd)
	case "_CONTEXT_WORD":
		return lazyValue(contextWord)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

// setMap は文字列の集合の辞書 (値は昇順の並びにする)。
func setMap(m map[string][]string) pyjson.Value {
	sorted := map[string][]string{}
	for k, v := range m {
		sorted[k] = sortedWords(v)
	}
	return wordSets(sorted)
}

var numberHits = map[string]int{}

func countNumberHits(v pyjson.Value) {
	items, _ := v.([]pyjson.Value)
	for _, item := range items {
		triple := item.([]pyjson.Value)
		risk := triple[2].(*pyjson.Object)
		sub, _ := risk.Get("subkind")
		name := sub.(string)
		if triple[1].(int64) >= 1000000 {
			name = "doc-" + name
		}
		numberHits[name]++
	}
	if len(items) == 0 {
		numberHits["(none)"]++
	}
}

// checkNumberHits は群 M の段の記録が、組の文の比べ (replaced・added・lost・order)・文書全体の比べ (added・lost)・何も出ない組のどれにも十分届いていることを確かめる。
func checkNumberHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"replaced", "added", "lost", "order", "doc-added", "doc-lost", "(none)"} {
		if numberHits[want] < 20 {
			t.Errorf("群 M の記録が %s に届いた回数は %d (20 回以上が要る)", want, numberHits[want])
		}
	}
	t.Logf("群 M の記録の届き方: %v", numberHits)
}

func extractValue(items []NumberItem) pyjson.Value {
	return replay.SortKeys(replay.MapList(items, NumberItem.JSON))
}

func argInt(t *testing.T, a any) int {
	t.Helper()
	switch n := a.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	t.Fatalf("整数でない引数 %T", a)
	return 0
}

// invokeNumber は群 M の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数 (段が同じ 2 つの文から作り直す)。
func invokeNumber(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "extract_numbers":
		items, err := ExtractNumbers(argString(t, a[0]))
		if err != nil {
			t.Fatalf("extract_numbers: %v", err)
		}
		return extractValue(items)
	case "_number_risks":
		v, err := NumberRisks(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_number_risks: %v", err)
		}
		countNumberHits(v)
		return replay.SortKeys(v)
	case "_plain_number":
		return plainNumber(argString(t, a[0]))
	case "_kanji_value":
		v, ok := kanjiValue(pyunicode.Runes(argString(t, a[0])))
		if !ok {
			return nil
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("_kanji_value: %v", err)
		}
		return n
	case "_negative_zu":
		return negativeZu(argString(t, a[0]))
	case "_quantity_negated":
		return quantityNegated(pyre.NewText(argString(t, a[0])), argInt(t, a[1]))
	case "_quantity_word_counts":
		o := pyjson.NewObject()
		for k, v := range quantityWordCounts(argString(t, a[0])) {
			o.Set(k, int64(v))
		}
		return replay.SortKeys(o)
	case "_head_clauses":
		s, ok := headClauses(argString(t, a[0]), argString(t, a[1]))
		if !ok {
			return nil
		}
		return s
	case "_other_subject_masked":
		return otherSubjectMasked(argString(t, a[0]), argString(t, a[1]))
	case "_one_sentence_without":
		return oneSentenceWithout(argString(t, a[0]), argString(t, a[1]))
	case "_text_ratio":
		return textRatio(pyunicode.Runes(argString(t, a[0])), pyunicode.Runes(argString(t, a[1])))
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}
