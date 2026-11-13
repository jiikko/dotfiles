package lint

import (
	"slices"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/lists"
	"ja-writing/oracle"
)

// gNN_tables.go (G8〜G12 の表と正規表現) が、本家の値と同じかを比べる。本家の値は lint-base.json の定数の記録 (oracle.LintBaseConstants の後ろ半分) で、
// constantValue がここで作った値を返し、TestLintBaseMatchesPythonRecording が正規の JSON の digest を比べる。
// re.compile の flags は str のパターンで re.UNICODE (32) が付くので、Go のフラグに 32 を足して比べる。

const pyUnicodeFlag = 32

// patternValue は本家の re.Pattern の JSON ({"flags", "pattern"}。鍵は昇順)。
func patternValue(p *lazyPattern) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("flags", int64(pyUnicodeFlag|p.flags))
	o.Set("pattern", p.pattern())
	return o
}

// rowPattern は patternRow の本家の文字列 (文末の飾りの並びを埋めたもの)。
func rowPattern(p patternRow) string {
	return strings.Replace(p.pattern, "{END}", endDecorationRun(), 1)
}

func pairList(rows []patternRow, swap bool) pyjson.Value {
	return mapList(rows, func(p patternRow) pyjson.Value {
		if swap {
			return list(p.desc, rowPattern(p))
		}
		return list(rowPattern(p), p.desc)
	})
}

// stringMap は鍵の昇順に並べた dict (本家の sort_keys=True)。
func stringMap[V any](m map[string]V, value func(V) pyjson.Value) pyjson.Value {
	o := pyjson.NewObject()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		o.Set(k, value(m[k]))
	}
	return o
}

func patternMap(m map[string]*lazyPattern) pyjson.Value {
	return stringMap(m, patternValue)
}

// literalHints は本家の _PATTERN_LITERAL_HINTS (パターン → 必要な語。必要な語のある行だけ)。
func literalHints() pyjson.Value {
	hints := map[string][]string{}
	for _, table := range [][]patternRow{slopPatterns, metaphorVerbPatterns, translatedTermPatterns, fillerPatterns} {
		for _, p := range table {
			if p.required != nil {
				hints[rowPattern(p)] = p.required
			}
		}
	}
	for _, p := range infoSentencePatterns {
		if p.required != nil {
			hints[p.re.pattern()] = p.required
		}
	}
	return stringMap(hints, stringList)
}

func tableConstantValue(name string) (pyjson.Value, bool) {
	lazy := map[string]*lazyPattern{
		"_KIKU_CANDIDATE": kikuCandidate,
		"_WRITING_ACTION": writingAction, "_RESIDUE_OPENER_RE": residueOpener, "_WRITING_STYLE": writingStyle, "_WRITING_LIMIT": writingLimit,
		"_SCOPE_EXCLUSION": scopeExclusion, "_READER_SCOPE": readerScope, "_SCOPE_REFERENCE": scopeReference,
		"_FRAGMENT_RUN": fragmentRun, "_SHORT_MOCHIRON": shortMochiron, "_MOCHIRON_ANSWER": mochironAnswer, "_QUOTED_SEGMENT": quotedSegment,
		"_CLEFT_LIST_COMMA": cleftListComma, "_CLEFT_LIST_MIXED": cleftListMixed, "_CLEFT_LIST_JOINER": cleftListJoiner, "_CLEFT_LIST_COMMA_MARK": cleftListCommaMark,
		"_CLEFT_LIST_MASKED_RUN": cleftListMaskedRun, "_CLEFT_LIST_SPACE": cleftListSpace, "_CLEFT_LIST_STEM": cleftListStem,
		"_V220_SENTENCE": v220Sentence, "_AI_PHRASE_HEAD": aiPhraseHead, "_AI_JUDGE": aiJudge, "_AI_INVITER": aiInviter,
		"_SLIDE_ACTOR_AFTER": slideActorAfter, "_SLIDE_LIST_AFTER": slideListAfter, "_SLIDE_LEAD": slideLead, "_SLIDE_BULLET": slideBullet, "_SLIDE_MARKER_ZONE": slideMarkerZone,
		"_SHAPE_COMMA_GATE": shapeCommaGate, "_SHAPE_HEADING_MARK": shapeHeadingMark, "_SHAPE_CURLY_QUOTED": shapeCurlyQuoted, "_SHAPE_SENTENCE": shapeSentence,
		"_SAY_HEDGE": sayHedge, "_SAY_HEDGE_NOUN_END": sayHedgeNounEnd, "_SAY_HEDGE_NA_ADJECTIVE": sayHedgeNaAdjective, "_SAY_HEDGE_JUDGE": sayHedgeJudge,
		"_COPY_COMMA_NOUN": copyCommaNoun, "_COPY_MODIFIER_PARTICLE": copyModifierParticle, "_COPY_NOUN_NOT": copyNounNot,
		"_BARE_VERB_PART": bareVerbPart, "_BARE_VERB_LAST": bareVerbLast, "_BARE_VERB_POLITE": bareVerbPolite, "_BARE_VERB_NOT_VERB": bareVerbNotVerb, "_BARE_VERB_PARTICLE": bareVerbParticle,
		"_NEGATION_EXTRA": negationExtra, "_NEGATION_JANAI": negationJanai, "_NEGATION_ALSO": negationAlso, "_BOLD_LABEL_ITEM": boldLabelItem,
	}
	if p, ok := lazy[name]; ok {
		return patternValue(p), true
	}
	switch name {
	case "SLOP_WORDS":
		return stringList(slopWords), true
	case "SLOP_WORD_PATTERNS":
		return pairList(slopPatterns, true), true
	case "METAPHOR_VERB_PATTERNS":
		return pairList(metaphorVerbPatterns, false), true
	case "TRANSLATED_TERM_PATTERNS":
		return pairList(translatedTermPatterns, false), true
	case "_KIKU_PATTERN":
		return kikuPattern, true
	case "_KIKU_RE":
		return patternMap(kikuRE), true
	case "_METAPHOR_VERB_MESSAGE_TAILS":
		return stringMap(metaphorVerbMessageTails, func(s string) pyjson.Value { return s }), true
	case "_METAPHOR_VERB_MESSAGE_TAIL":
		return metaphorVerbMessageTail, true
	case "_PATTERN_LITERAL_HINTS":
		return literalHints(), true
	case "FILLER_PATTERNS":
		return pairList(fillerPatterns, false), true
	case "FILLER_NOTES":
		return stringMap(fillerNotes, func(s string) pyjson.Value { return s }), true
	case "INFO_SENTENCE_PATTERNS":
		return mapList(infoSentencePatterns, func(p infoRow) pyjson.Value { return list(p.re.pattern(), p.rule, p.message) }), true
	case "_OVERSTATEMENT_PATTERN":
		return overstatementPattern, true
	case "_V220_MESSAGE_TAIL":
		return v220MessageTail, true
	case "_RENYO_BEFORE":
		return renyoBefore, true
	case "_RENYO_AFTER":
		return renyoAfter, true
	case "_AI_PHRASE_FORMS":
		return mapList(aiPhraseForms, namedPatternValue), true
	case "_SLIDE_LINE_MAX":
		return int64(slideLineMax), true
	case "_SLIDE_COMMA":
		return mapList(slideComma, namedPatternValue), true
	case "_SLIDE_SAME_NEXT":
		return patternMap(slideSameNext), true
	case "_SLIDE_MESSAGE":
		return slideMessageHead + "{}" + slideMessageTail, true
	case "_AI_PHRASE_MESSAGE":
		return aiPhraseMessageHead + "{}）" + v220MessageTail, true
	case "_RENYO_MESSAGE":
		return renyoMessageHead + "{}）" + v220MessageTail, true
	case "_SHAPE_RULE_MESSAGES":
		messages := map[string]string{}
		for _, r := range shapeRules {
			messages[r.name] = r.message
		}
		return stringMap(messages, func(s string) pyjson.Value { return s }), true
	case "_SHAPE_GATE_WORDS":
		return stringList(shapeGateWords), true
	case "_SHAPE_QUOTE_MARKS":
		return stringList(shapeQuoteMarks), true
	case "_COPY_NOT_MODIFIER":
		return stringList(slices.Sorted(slices.Values(copyNotModifier))), true
	case "_COPY_I_NOUNS":
		return stringList(slices.Sorted(slices.Values(copyINouns))), true
	}
	return nil, false
}

// 表の定数を Go が全部持っていること (constantValue が名前を知らないと、記録との比べが落ちる前にここで分かる)。
func TestLintTablesCoverRecordedConstants(t *testing.T) {
	for _, name := range oracle.LintBaseConstants {
		if _, ok := constantValue(name); !ok {
			t.Errorf("定数 %s を Go に持っていない", name)
		}
	}
}

// tableFunctionValue は oracle.lintTableFunctionCases の呼び出しの値。
func tableFunctionValue(name string) (pyjson.Value, bool) {
	switch name {
	case "_v220_load_list":
		data, err := lists.Read("renyo-nouns.txt")
		if err != nil {
			panic(err)
		}
		return list(stringList(parseList(string(data))), "references/lists/renyo-nouns.txt"), true
	}
	return nil, false
}

// namedPatternValue は (名前, 正規表現) の組 (G16 の型の表と G17 の読点の表は同じ形) の JSON。
func namedPatternValue(f struct {
	name string
	re   *lazyPattern
}) pyjson.Value {
	return list(f.name, patternValue(f.re))
}
