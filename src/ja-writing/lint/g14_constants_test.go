package lint

import (
	"strconv"

	"ja-writing/internal/pyjson"
	"ja-writing/lists"
	"ja-writing/oracle/replay"
)

// G14 の表 (g14_tables.go) と、本家が組む一覧の値を、本家の定数の記録と比べる値にする (tableConstantValue の続き)。

func g14ConstantValue(name string) (pyjson.Value, bool) {
	switch name {
	case "_CALQUE_SUBJECT_WORD":
		return patternValue(calqueSubjectWord), true
	case "_CALQUE_PERSON_SUBJECT":
		return patternValue(calquePersonSubject), true
	case "_CALQUE_PERSON_EXT":
		return patternValue(calquePersonExt), true
	case "_CALQUE_ESTABLISHED_TYPE1_RE":
		return patternValue(calqueEstablishedType1Re), true
	case "_CALQUE_KANA_RUN":
		return patternValue(calqueKanaRun), true
	case "_CALQUE_SUBJECT_BREAK":
		return patternValue(calqueSubjectBreak), true
	case "_CALQUE_PLACE_ALWAYS":
		return patternValue(calquePlaceAlways), true
	case "_CALQUE_PLACE_CONTEXT":
		return patternValue(calquePlaceContext), true
	case "_CALQUE_PLACE_REAL":
		return patternValue(calquePlaceReal), true
	case "_CALQUE_PLACE_REAL_DOC":
		return patternValue(calquePlaceRealDoc), true
	case "_CALQUE_ENTRY_WAIT":
		return patternValue(calqueEntryWait), true
	case "_CALQUE_PLACE_ORG":
		return patternValue(calquePlaceOrg), true
	case "_CALQUE_MOUTH_REAL":
		return patternValue(calqueMouthReal), true
	case "_CALQUE_MOUTH_IDIOM":
		return patternValue(calqueMouthIdiom), true
	case "_CALQUE_TECH":
		return patternValue(calqueTech), true
	case "_CALQUE_ENTRY_FLOW":
		return patternValue(calqueEntryFlow), true
	case "_CALQUE_ENTRY_SUBJECT":
		return patternValue(calqueEntrySubject), true
	case "_CALQUE_CODE":
		return patternValue(calqueCode), true
	case "_CALQUE_SAKI_TIME":
		return patternValue(calqueSakiTime), true
	case "_CALQUE_MATTERS_FOCUS":
		return patternValue(calqueMattersFocus), true
	case "_CALQUE_TABLE_SUBJECT":
		return patternValue(calqueTableSubject), true
	case "_CALQUE_TABLE_ATTRIBUTIVE":
		return patternValue(calqueTableAttributive), true
	case "_CALQUE_TURN_CONTEXT":
		return patternValue(calqueTurnContext), true
	case "_CALQUE_TURN_GAME":
		return patternValue(calqueTurnGame), true
	case "_CALQUE_KIKU_DECIDE":
		return patternValue(calqueKikuDecide), true
	case "_CALQUE_HIBIKU_LITERAL":
		return patternValue(calqueHibikuLiteral), true
	case "_CALQUE_HIBIKU_ALL":
		return patternValue(calqueHibikuAll), true
	case "_CALQUE_HIBIKU_ALL_LITERAL":
		return patternValue(calqueHibikuAllLiteral), true
	case "_CALQUE_HIBIKU_MACHINE":
		return patternValue(calqueHibikuMachine), true
	case "_CALQUE_HIBIKU_ALL_NOUN":
		return patternValue(calqueHibikuAllNoun), true
	case "_CALQUE_HIBIKU_LISTED":
		return patternValue(calqueHibikuListed), true
	case "_CALQUE_MACHINE_TALK":
		return patternValue(calqueMachineTalk), true
	case "_LABEL_NUMBERING":
		return patternValue(labelNumbering), true
	case "_LABEL_ASCII":
		return patternValue(labelAscii), true
	case "_LABEL_NOT_JAPANESE":
		return patternValue(labelNotJapanese), true
	case "_LABEL_SEPARATED":
		return patternValue(labelSeparated), true
	case "_CALQUE_VERSION_PUB":
		return patternValue(calqueVersionPub), true
	case "_CALQUE_MARK":
		return patternValue(calqueMark), true
	case "_CALQUE_MARK_CONTEXT":
		return patternValue(calqueMarkContext), true
	case "_CALQUE_HEAD":
		return patternValue(calqueHead), true
	case "_CALQUE_BRANCH":
		return patternValue(calqueBranch), true
	case "_CALQUE_BRANCH_CONTEXT":
		return patternValue(calqueBranchContext), true
	case "_CALQUE_TITLE":
		return patternValue(calqueTitle), true
	case "_CALQUE_RULE":
		return patternValue(calqueRule), true
	case "_CALQUE_STYLE_CONTEXT":
		return patternValue(calqueStyleContext), true
	case "_CALQUE_PERSON_END":
		return calquePersonEnd, true
	case "_CALQUE_ORG_SUFFIXES":
		return calqueOrgSuffixes, true
	case "_CALQUE_SAKI_TAIL":
		return calqueSakiTail, true
	case "_CALQUE_PHRASE_KIKU":
		return calquePhraseKiku, true
	case "_CALQUE_PHRASE_MATTERS":
		return calquePhraseMatters, true
	case "_LABEL_MARKS":
		return labelMarks, true
	case "_LABEL_AFTER_NOUN":
		return labelAfterNoun, true
	case "_LABEL_ATTRIBUTIVE":
		return labelAttributive, true
	case "_LABEL_ANYWHERE":
		return labelAnywhere, true
	case "_LABEL_BETWEEN":
		return labelBetween, true
	case "_LABEL_VERB_END":
		return labelVerbEnd, true
	case "_THIN_ADVERB":
		return thinAdverb, true
	case "_THIN_TAIL":
		return thinTail, true
	case "_KIKU_LITERAL":
		return kikuLiteral, true
	case "_PUBLICATION_WORDS":
		return publicationWords, true
	case "_CALQUE_STYLE_CONTEXT_PATTERN":
		return calqueStyleContextPattern, true
	case "_CALQUE_SUBJECT_EXT":
		return int64(calqueSubjectExt), true
	case "_CALQUE_SUBJECT_REACH":
		return int64(calqueSubjectReach), true
	case "_CALQUE_DOC_TECH_MIN":
		return int64(calqueDocTechMin), true
	case "_LABEL_MAX":
		return int64(labelMax), true
	case "_LABEL_MIN_BODY":
		return int64(labelMinBody), true
	case "_CALQUE_BEFORE_WINDOW":
		return int64(calqueBeforeWindow), true
	case "_THIN_PREFIX_MAX":
		return int64(thinPrefixMax), true
	case "_V220_TYPE1_END":
		return int64(v220Type1End), true
	case "_V220_TYPE2_END":
		return int64(v220Type2End), true
	case "_V220_TYPE3_END":
		return int64(v220Type3End), true
	case "_V220_TYPE4_END":
		return int64(v220Type4End), true
	case "_CALQUE_SUBJECTS":
		return replay.Strings(calqueSubjects), true
	case "_CALQUE_SUBJECT_PARTICLES":
		return replay.Strings(calqueSubjectParticles), true
	case "_CALQUE_COMPOUND_OK":
		return replay.Strings(calqueCompoundOk), true
	case "_CALQUE_REQUEST":
		return replay.Strings(calqueRequest), true
	case "_CALQUE_ATTRIBUTIVE_END":
		return replay.Strings(calqueAttributiveEnd), true
	case "_CALQUE_ESTABLISHED_SAKI":
		return replay.Strings(calqueEstablishedSaki), true
	case "_CALQUE_SAKI_KANJI":
		return replay.Strings(calqueSakiKanji), true
	case "_CALQUE_ORG_WORDS":
		return replay.Strings(calqueOrgWords), true
	case "_CALQUE_DOC_TECH_GENERAL":
		return replay.Strings(calqueDocTechGeneral), true
	case "_CALQUE_HIBIKU_COST":
		return replay.Strings(calqueHibikuCost), true
	case "_LABEL_COMMON":
		return replay.Strings(labelCommon), true
	case "_LABEL_ENDINGS":
		return replay.Strings(labelEndings), true
	case "_LABEL_NOT_ENDINGS":
		return replay.Strings(labelNotEndings), true
	case "_LABEL_ABSTRACT_ENDINGS":
		return replay.Strings(labelAbstractEndings), true
	case "_V220_TRIGGER_LITERALS":
		return replay.Strings(v220TriggerLiterals), true
	case "_CALQUE_PHRASE_VERB":
		return stringMap(calquePhraseVerb, stringValue), true
	case "_CALQUE_TYPE_NAMES":
		return intKeyedStrings(calqueTypeNames), true
	case "_CALQUE_PHRASES":
		return replay.MapList(calquePhrases, namedLazyValue), true
	case "_CALQUE_PHRASES_V250":
		return replay.MapList(calquePhrasesV250, namedLazyValue), true
	case "_CALQUE_VERSION":
		return replay.MapList(calqueVersion, patternValue), true
	case "_CALQUE_VERB_EACH":
		return replay.MapList(calqueVerbEach, func(v calqueVerb) pyjson.Value { return replay.List(v.stem, v.name, patternValue(v.re)) }), true
	case "_CALQUE_TERMS_V300":
		return replay.MapList(calqueTermsV300, func(t calqueTerm) pyjson.Value {
			return replay.List(t.head, patternValue(t.pattern), optPatternValue(t.before), optPatternValue(t.context))
		}), true
	case "_CALQUE_PHRASE":
		return patternValue(calquePhrase), true
	case "_V220_QUOTES":
		return patternValue(v220Quotes), true
	case "_V220_IDENTIFIER":
		return patternValue(v220Identifier), true
	case "_V220_BUILTIN_LISTS":
		return v220BuiltinLists(), true
	}
	return nil, false
}

// namedLazyValue は (名前, 正規表現の文字列) の組。
func namedLazyValue(n namedLazy) pyjson.Value { return replay.List(n.name, n.re.Pattern()) }

// stringValue は文字列の値。
func stringValue(s string) pyjson.Value { return s }

// thinPairValue は thin-evaluation.txt の 1 行 ([名詞, 述語の正規表現])。
func thinPairValue(p thinPair) pyjson.Value { return replay.List(p.noun, p.predicate) }

// optPatternValue は無いかもしれない正規表現 (本家の None は null)。
func optPatternValue(p *lazyPattern) pyjson.Value {
	if p == nil {
		return nil
	}
	return patternValue(p)
}

// intKeyedStrings は整数の鍵の dict (JSON の鍵は文字になる)。
func intKeyedStrings(m map[int]string) pyjson.Value {
	s := map[string]string{}
	for k, v := range m {
		s[strconv.Itoa(k)] = v
	}
	return stringMap(s, stringValue)
}

// v220BuiltinLists は本家の _V220_BUILTIN_LISTS (内蔵の一覧)。Go は内蔵の一覧を持たず、埋め込んだ一覧を読むので、同じ中身であることをこの記録と比べる。
func v220BuiltinLists() pyjson.Value {
	read := func(name string) string {
		data, err := lists.Read(name)
		if err != nil {
			panic(err)
		}
		return string(data)
	}
	o := pyjson.NewObject()
	o.Set("katakana-terms", replay.Strings(lists.Words(read("katakana-terms.txt"))))
	o.Set("renyo-nouns", replay.Strings(lists.Words(read("renyo-nouns.txt"))))
	o.Set("thin-evaluation", replay.MapList(parseThinList(read("thin-evaluation.txt")), thinPairValue))
	return o
}
