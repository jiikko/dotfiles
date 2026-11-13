package diff

import (
	"maps"
	"slices"
	"strconv"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 AA の Go の表と関数を、本家の記録と突き合わせる道具。

// toneConstantValue は群 AA の本家の定数 (oracle.DiffToneConstantNames) の、Go の表から作った値。
func toneConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "TONE_INFO_DOC_TYPES":
		return replay.Strings(toneInfoDocTypes)
	case "TONE_TYPED_INFO_SUBKINDS":
		return replay.Strings(toneTypedInfoSubkinds)
	case "TONE_TYPED_REVIEW_DOC_TYPES":
		o := pyjson.NewObject()
		o.Set("emoji", replay.Strings(toneTypedReviewDocTypes["emoji"]))
		o.Set("heading", replay.Strings(toneTypedReviewDocTypes["heading"]))
		return o
	case "TONE_HEADING_RATIO":
		return float64(toneHeadingRatio)
	case "_TONE_EXCERPT_WIDTH":
		return int64(toneExcerptWidth)
	case "_INDIRECT_QUESTION":
		return lazyValue(indirectQuestion)
	case "_TONE_SKIP_KINDS":
		kinds := make([]string, 0, len(toneSkipKinds))
		for k := range toneSkipKinds {
			kinds = append(kinds, k)
		}
		slices.Sort(kinds)
		return replay.Strings(kinds)
	case "_TONE_KINDS":
		return replay.Strings(toneKinds)
	case "_EXCLAMATION":
		return lazyValue(exclamationMark)
	case "_QUESTION_MARK":
		return lazyValue(questionMark)
	case "_INTENSIFIER_EXCLUDED":
		return lazyValue(intensifierExcluded)
	case "_ATX_MARKERS":
		return lazyValue(atxMarkers)
	case "_MAYBE_EMOJI":
		return lazyValue(maybeEmoji)
	case "_ORDERED_ITEM_LINE":
		return lazyValue(orderedItemLine)
	case "_BOLD_ONLY_LINE":
		return lazyValue(boldOnlyLine)
	case "_SENTENCE_END_MARKS":
		return sentenceEndMarks
	case "_SPACES":
		return lazyValue(spacePattern)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func spansValue(spans [][2]int) pyjson.Value {
	out := make([]pyjson.Value, len(spans))
	for i, sp := range spans {
		out[i] = replay.List(int64(sp[0]), int64(sp[1]))
	}
	return out
}

// toneLinesValue は _tone_lines の戻り (行番号 → 数。本家の OrderedDict は整数の鍵の昇順に入れる)。
func toneLinesValue(l *toneLines) pyjson.Value {
	o := pyjson.NewObject()
	for _, no := range l.order {
		c := l.counts[no]
		words := pyjson.NewObject()
		for _, w := range slices.Sorted(maps.Keys(c.words)) {
			words.Set(w, int64(c.words[w]))
		}
		counts := pyjson.NewObject()
		counts.Set("emoji", int64(c.emoji))
		counts.Set("exclamation", int64(c.exclamation))
		counts.Set("heading_chars", int64(c.headingChars))
		counts.Set("intensifier", int64(c.intensifier))
		counts.Set("question", int64(c.question))
		counts.Set("text", c.text)
		counts.Set("words", words)
		o.Set(strconv.Itoa(no), counts)
	}
	return o
}

// recordedIntensifiers は記録の呼び出しが渡した強める語の正規表現の引数 (一覧から作ったものか、渡さない null) を Go の値にする。
// 記録の呼び出しは、一覧から作った正規表現を覚えた名前 "ip" の参照か null (_mark_spans の直接の呼び出しだけ) を渡す。それ以外の引数が記録に入ったら、黙って一覧から作らず落とす。
func recordedIntensifiers(t *testing.T, arg any) *pyre.Lazy {
	t.Helper()
	if arg == nil {
		return nil
	}
	if ref, ok := arg.(map[string]any); !ok || ref["$ref"] != "ip" {
		t.Fatalf("強める語の正規表現の引数が、一覧から作ったものの参照 (ip) でも null でもない: %v", arg)
	}
	return intensifierPattern(LoadList("intensifiers").Words)
}

// invokeTone は群 AA の呼び出し 1 つの結果。本家が例外を投げる呼び出しは error を返す。build は、直前の _build_full_diff の呼び出しの引数
// (_tone_lines が同じ 2 つの文から段を作り直す)。
func invokeTone(t *testing.T, c oracle.Call, build []any) (pyjson.Value, error) {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "tone":
		v, err := Tone(argString(t, a[0]), argString(t, a[1]), optionalDocType(t, a[2]))
		if err != nil {
			return nil, err
		}
		countToneHits(v)
		return replay.SortKeys(v), nil
	case "_tone_level":
		subkind := ""
		if a[1] != nil {
			subkind = argString(t, a[1])
		}
		return toneLevel(optionalDocType(t, a[0]), subkind), nil
	case "_pseudo_heading":
		heading, ok := PseudoHeading(argString(t, a[0]), argString(t, a[1]))
		if !ok {
			return nil, nil
		}
		return heading, nil
	case "_heading_chars":
		return int64(headingChars(argString(t, a[0]))), nil
	case "_mark_spans":
		return spansValue(MarkSpans(argString(t, a[0]), argString(t, a[1]), recordedIntensifiers(t, a[2]))), nil
	case "_tone_excerpt":
		var spans [][2]int
		for _, s := range a[2].([][]int) {
			spans = append(spans, [2]int{s[0], s[1]})
		}
		width := toneExcerptWidth
		if len(a) > 3 {
			width = a[3].(int)
		}
		x, y, err := ToneExcerpt(argString(t, a[0]), argString(t, a[1]), spans, width)
		if err != nil {
			return nil, err
		}
		return replay.List(x, y), nil
	case "_tone_lines":
		s, err := newSides(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_tone_lines: %v", err)
		}
		return toneLinesValue(s.toneLinesOf(a[1].(int), recordedIntensifiers(t, a[2]), argBool(t, a[3]))), nil
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil, nil
}

// toneHits は記録の組に Go の tone を通したときの、候補の種類 (と level・候補なし) ごとの数。
var toneHits = map[string]int{}

func countToneHits(v pyjson.Value) {
	list, _ := v.(*pyjson.Object).Get("candidates")
	candidates := list.([]pyjson.Value)
	if len(candidates) == 0 {
		toneHits["(none)"]++
	}
	for _, c := range candidates {
		co := c.(*pyjson.Object)
		subkind, _ := co.Get("subkind")
		level, _ := co.Get("level")
		toneHits[subkind.(string)]++
		toneHits["level:"+level.(string)]++
	}
}

// 空振りを止める: 群 AA の記録の組が、候補の 5 種類と 2 つの level と候補なしの全部に届く。
func checkToneHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"exclamation", "question", "emoji", "intensifier", "heading", "level:info", "level:review", "(none)"} {
		if toneHits[want] < 20 {
			t.Errorf("群 AA の記録が %s に届いた回数は %d (20 回以上が要る)", want, toneHits[want])
		}
	}
	t.Logf("群 AA の記録の届き方: %v", toneHits)
}
