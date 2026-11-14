package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 S の段の記録の突き合わせ。

var contrastHits = map[string]int{}

// checkContrastHits は群 S の段の記録が、逆転が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkContrastHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"contrast:contrast_reversal", "contrast:(none)",
		"negated_tokens:found", "negated_tokens:none", "neg_after:true", "neg_after:false", "demoted:true", "demoted:false",
		"affirmed_with_weight:found", "affirmed_with_weight:none", "affirmed_with_weight_clause:found", "affirmed_with_weight_clause:none",
		"affirmed_plain:found", "affirmed_plain:none", "side_affirmed:true", "side_affirmed:false", "original_affirms:true", "original_affirms:false",
		"shown_word:extended", "shown_word:same", "b_side_phrases:found", "b_side_phrases:none", "continues_word:true", "continues_word:false", "other_object:true", "other_object:false"} {
		if contrastHits[want] < 20 {
			t.Errorf("群 S の記録が %s に届いた回数は %d (20 回以上が要る)", want, contrastHits[want])
		}
	}
	t.Logf("群 S の記録の届き方: %v", contrastHits)
}

// contrastHit は関数の単位の呼び出しの結果が、真偽 (見つかった・見つからない) のどちらにも届くことを数える。
func contrastHit(name string, found bool, yes, no string) {
	if found {
		contrastHits[name+":"+yes]++
	} else {
		contrastHits[name+":"+no]++
	}
}

// contrastConstantValue は群 S の本家の定数の、Go の表から作った値。
func contrastConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_CONTRAST":
		return lazyValue(contrastMark)
	case "_ZU_MARK":
		return lazyValue(contrastZuMark)
	case "_NOT_ONLY":
		return replay.Strings(contrastNotOnly)
	case "_RHETORICAL":
		return lazyValue(contrastRhetoric)
	case "_SUBORDINATE":
		return lazyValue(contrastSubord)
	case "_CLAUSE_BREAK":
		return lazyValue(contrastBreak)
	case "_TOPIC":
		return lazyValue(contrastTopic)
	case "_TOKEN_OKURI_MID":
		return contrastTokenOkuriMid
	case "_TOKEN":
		return lazyValue(contrastToken)
	case "CONTRAST_WEIGHTS":
		return replay.Strings(contrastWeights)
	case "_STOP_TOKENS":
		return sortedSet(contrastStopTokens)
	case "_NEG_AFTER":
		return lazyValue(contrastNegAfter)
	case "_DEMOTE_WORD":
		return lazyValue(contrastDemoteWord)
	case "_OTHER_NOUN":
		return contrastOtherNoun
	case "_DEMOTE_WORD_OTHER":
		return lazyValue(contrastDemoteWordElse)
	case "_LEXICAL_NEG":
		return lazyValue(contrastLexicalNeg)
	case "_COORD_HEAD":
		return lazyValue(contrastCoordHead)
	case "_TOPIC_MARKS":
		return lazyValue(contrastTopicMarks)
	case "_PROMOTE_TO":
		return contrastPromoteTo
	case "_USE_A":
		return contrastUseA
	case "_DEMOTE_REDUCE":
		return contrastDemoteReduce
	case "_DEMOTE":
		return lazyValue(contrastDemote)
	case "_REDUCE_ITSELF":
		return lazyValue(contrastReduceItself)
	case "_DEMOTE_ANCHORED":
		return lazyValue(contrastDemoteAnchored)
	case "_A_PHRASE":
		return lazyValue(contrastAPhrase)
	case "_OTHER_OBJECT":
		return lazyValue(contrastOtherObject)
	case "_SWITCH_TO":
		return lazyValue(contrastSwitchTo)
	case "_SWITCH_FROM":
		return lazyValue(contrastSwitchFrom)
	case "_CONTRAST_SKIP_KINDS":
		return sortedSet(contrastSkipKinds)
	case "_ALSO_AFTER":
		return lazyValue(contrastAlsoAfter)
	case "_COORD_AFTER":
		return lazyValue(contrastCoord)
	case "_COMPARE_CLAUSE":
		return lazyValue(contrastCompare)
	case "_HYPOTHESIS":
		return lazyValue(contrastHypothesis)
	case "_ADVERSATIVE_END":
		return lazyValue(contrastAdversative)
	case "_WEIGHT_PATTERN":
		return lazyValue(contrastWeightPat)
	case "_WEIGHT_ONLY_CLAUSE":
		return lazyValue(contrastWeightOnly)
	case "_ADJ_NOUN":
		return lazyValue(contrastAdjNoun)
	case "_SIDE_REPLACED":
		return lazyValue(contrastSideReplaced)
	case "_SIDE_REJECTED":
		return lazyValue(contrastSideRejected)
	case "_KATAKANA_HEAD":
		return lazyValue(contrastKatakanaHdr)
	case "_LATIN_HEAD":
		return lazyValue(contrastLatinHdr)
	case "_SHOWN_TAIL":
		return lazyValue(contrastShownTail)
	case "_SHOWN_TAIL_NOT_HEAD":
		return charSet(contrastShownNotHead)
	case "_SHOWN_TAIL_PARTICLES":
		return replay.Strings(sortedWords(contrastShownParticles))
	case "_SHOWN_TAIL_MO_GA":
		return charSet(contrastShownMoGa)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func contrastStringList(t *testing.T, a any) []string {
	t.Helper()
	if list, ok := a.([]string); ok {
		return list
	}
	list, ok := a.([]any)
	if !ok {
		t.Fatalf("リストでない引数 %T", a)
	}
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = argString(t, e)
	}
	return out
}

func contrastPosition(n int) pyjson.Value {
	if n < 0 {
		return nil
	}
	return int64(n)
}

// invokeContrast は群 S の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeContrast(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_contrast_risks":
		v, err := ContrastRisks(argString(t, build[0]), argString(t, build[1]))
		if err != nil {
			t.Fatalf("_contrast_risks: %v", err)
		}
		countRiskKinds(v, "contrast:", contrastHits)
		return replay.SortKeys(v)
	case "_neg_after":
		got := negAfter(newCtext(argString(t, a[0])), argString(t, a[1]))
		contrastHit("neg_after", got, "true", "false")
		return got
	case "_clause_bounds":
		left, right := clauseBounds(newCtext(argString(t, a[0])), argInt(t, a[1]))
		return replay.List(int64(left), int64(right))
	case "_is_token":
		return contrastIsToken(argString(t, a[0]))
	case "_negated_tokens":
		var out []pyjson.Value
		for _, n := range negatedTokens(newCtext(argString(t, a[0]))) {
			out = append(out, replay.List(n.token, n.mark, int64(n.markEnd)))
		}
		contrastHit("negated_tokens", len(out) > 0, "found", "none")
		return replay.List(out...)
	case "_shown_word":
		got := shownWord(newCtext(argString(t, a[0])), argString(t, a[1]), argInt(t, a[2]))
		contrastHit("shown_word", got != argString(t, a[1]), "extended", "same")
		return got
	case "_demoted":
		got := demoted(newCtext(argString(t, a[0])), argInt(t, a[1]), argInt(t, a[2]), newCtext(argString(t, a[3])))
		contrastHit("demoted", got, "true", "false")
		return got
	case "_other_object":
		got := otherObject(newCtext(argString(t, a[0])))
		contrastHit("other_object", got, "true", "false")
		return got
	case "_affirmed_with_weight":
		got := affirmedWithWeight(newCtext(argString(t, a[0])), argString(t, a[1]), argBool(t, a[2]), contrastStringList(t, a[3]))
		name := "affirmed_with_weight"
		if argBool(t, a[2]) {
			name += "_clause"
		}
		contrastHit(name, got >= 0, "found", "none")
		return contrastPosition(got)
	case "_side_affirmed":
		got := sideAffirmed(newCtext(argString(t, a[0])), argInt(t, a[1]), argString(t, a[2]), argInt(t, a[3]), contrastStringList(t, a[4]))
		contrastHit("side_affirmed", got, "true", "false")
		return got
	case "_affirmed_plain":
		got := affirmedPlain(newCtext(argString(t, a[0])), argString(t, a[1]), contrastStringList(t, a[2]))
		contrastHit("affirmed_plain", got >= 0, "found", "none")
		return contrastPosition(got)
	case "_original_affirms":
		got := originalAffirms(newCtext(argString(t, a[0])), argString(t, a[1]), argInt(t, a[2]), argInt(t, a[3]))
		contrastHit("original_affirms", got, "true", "false")
		return got
	case "_continues_word":
		got := continuesWord(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1]), argString(t, a[2]))
		contrastHit("continues_word", got, "true", "false")
		return got
	case "_b_side_phrases":
		got := bSidePhrases(newCtext(argString(t, a[0])), argString(t, a[1]), argInt(t, a[2]))
		contrastHit("b_side_phrases", len(got) > 0, "found", "none")
		return replay.Strings(got)
	case "_head_clue":
		return headClue(argString(t, a[0]))
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}
