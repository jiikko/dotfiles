package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 Q の Go の表と関数を、本家の記録と突き合わせる道具。

func qualifierConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	set := func(m map[string]bool) pyjson.Value { return sortedSet(m) }
	switch name {
	case "_QUALIFIER_JOIN":
		return lazyValue(qualifierJoin)
	case "_QUALIFIER_SKIP":
		return lazyValue(qualifierSkip)
	case "_QUALIFIER_SHOWN":
		return lazyValue(qualifierShown)
	case "_QUALIFIER_KATAKANA_UNIT":
		return lazyValue(qualifierKatakanaUnit)
	case "_QUALIFIER_PART":
		return lazyValue(qualifierPart)
	case "_QUALIFIER_AFTER":
		return lazyValue(qualifierAfter)
	case "_QUALIFIER_UNIT_ONLY":
		return set(qualifierUnitOnly)
	case "_QUALIFIER_PLACE_UNITS":
		return set(qualifierPlaceUnits)
	case "_QUALIFIER_VERSION_BEFORE":
		return lazyValue(qualifierVersionBefore)
	case "_QUALIFIER_BEFORE":
		return lazyValue(qualifierBefore)
	case "_QUALIFIER_BEFORE_REACH":
		return int64(qualifierBeforeReach)
	case "_QUALIFIER_YAKU_WORDS":
		return set(qualifierYakuWords)
	case "_QUALIFIER_YAKU_NOUNS":
		return set(qualifierYakuNouns)
	case "_QUALIFIER_NEAR_AFTER":
		return lazyValue(qualifierNearAfter)
	case "_QUALIFIER_NEAR_BEFORE":
		return lazyValue(qualifierNearBefore)
	case "_QUALIFIER_SENTENCE":
		o := pyjson.NewObject()
		for k, l := range qualifierSentence {
			o.Set(k, lazyValue(l))
		}
		return replay.SortKeys(o)
	case "_QUALIFIER_FAMILIES":
		return replay.Strings(qualifierFamilies)
	case "_QUALIFIER_BOUNDS":
		return set(qualifierBounds)
	case "_QUALIFIER_STRICT":
		return set(qualifierStrict)
	case "_QUALIFIER_SENTENCE_STOP":
		return lazyValue(qualifierSentenceStop)
	case "_QUALIFIER_SPLIT_DECIMAL":
		return lazyValue(qualifierSplitDecimal)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

var qualifierHits = map[string]int{}

// checkQualifierHits は群 Q の段の記録が、lost・added・changed と何も出ない組のどれにも十分届いていることを確かめる。
func checkQualifierHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"lost", "added", "changed", "(none)"} {
		if qualifierHits[want] < 20 {
			t.Errorf("群 Q の記録が %s に届いた回数は %d (20 回以上が要る)", want, qualifierHits[want])
		}
	}
	t.Logf("群 Q の記録の届き方: %v", qualifierHits)
}

func invokeQualifier(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_qualifier_risks":
		v, err := QualifierRisks(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_qualifier_risks: %v", err)
		}
		items, _ := v.([]pyjson.Value)
		for _, item := range items {
			sub, _ := item.(*pyjson.Object).Get("subkind")
			qualifierHits[sub.(string)]++
		}
		if len(items) == 0 {
			qualifierHits["(none)"]++
		}
		return replay.SortKeys(v)
	case "_qualifier_yaku_ok":
		return qualifierYakuOK(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1]), argInt(t, a[2]))
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}
