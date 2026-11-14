package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
)

// 群 Y: 意味が変わる恐れの組み立て (本家の _meaning_risks・meaning_risks)。群 M〜X の各検出を本家と同じ順で集め、
// 打ち消し合う候補 (文の種類の印と確度・状態と確度・引用と数値) を決める。

// riskOrder は並べるときの、候補の種類の順 (本家の _RISK_ORDER。表にない種類は 9)。
var riskOrder = map[string]int{
	"number": 0, "negation": 1, "swap": 2, "sentence_type": 3, "protected": 4, "contrast_reversal": 5, "feeling": 6, "certainty": 7, "certainty_review": 8,
	"quote": 9, "state": 10, "promise": 11, "claim": 12, "promise_review": 13, "modality": 14, "modality_review": 15, "choice": 16, "causality": 17, "number_qualifier": 18,
}

// meaningResult は meaning_risks の組み立ての結果。immutable と quiet は本家が _Sides に置く値 (sides.immutable・sides.quiet_marks)、
// restored は強める向きの推量の候補を戻した数 (確度の候補が出なかった文の数)、filtered は状態の候補に写して取り除いた確度の見直しの数。
type meaningResult struct {
	risks     []*pyjson.Object
	immutable *pyjson.Object
	quiet     []*pyjson.Object
	restored  int
	filtered  int
}

// riskLineKey は意味が変わる恐れを行の順に並べる鍵 (本家の (old_line があればそれ、無ければ new_line か 0, new_line か 0))。
func riskLineKey(r *pyjson.Object) [2]int {
	line := func(key string) (int, bool) {
		v, _ := r.Get(key)
		n, ok := v.(int64)
		return int(n), ok
	}
	oldLine, hasOld := line("old_line")
	newLine, _ := line("new_line")
	if hasOld {
		return [2]int{oldLine, newLine}
	}
	return [2]int{newLine, newLine}
}

func sortByLines(list []*pyjson.Object) {
	slices.SortStableFunc(list, func(a, b *pyjson.Object) int {
		ka, kb := riskLineKey(a), riskLineKey(b)
		for i := range 2 {
			if ka[i] != kb[i] {
				return ka[i] - kb[i]
			}
		}
		return 0
	})
}

func kindNamesOf(hits []KindHits) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Kind
	}
	return out
}

// meaningRisks は意味が変わる恐れを集める (本家の _meaning_risks)。changes は diff() の type_changes (withChanges が偽なら None: 文の種類の印を含めない)、
// modality と logic は _modality_changes と _logic_changes の戻り値 (None は nil)。docType は正規化した文書の種類 (空なら指定なし)。
func (s *sides) meaningRisks(full *fullDiff, groups []*sentenceGroup, changes []TypeChange, docType string, modality []ModalityChange, logic []LogicChange) meaningResult {
	found := slices.Concat(s.numberRisks(full, groups), s.negationRisks(groups), s.swapRisks(groups), s.questionRisks(groups), s.protectedRisks(full),
		s.contrastRisks(groups), s.feelingRisks(groups))
	slices.SortStableFunc(found, func(a, b riskItem) int {
		ka, kb := [3]int{a.index, riskOrder[riskKind(a.risk)], a.order}, [3]int{b.index, riskOrder[riskKind(b.risk)], b.order}
		for i := range 3 {
			if ka[i] != kb[i] {
				return ka[i] - kb[i]
			}
		}
		return 0
	})
	foundRisks := make([]*pyjson.Object, len(found))
	for i, f := range found {
		foundRisks[i] = f.risk
	}
	allMarks := s.marksRisks(changes)
	quiet := make([]bool, len(allMarks))
	delegated := make([]bool, len(allMarks))
	for i := range allMarks {
		c := changes[i]
		switch ClassifyHedgeMarks(c.Orig, c.Rewrite, kindNamesOf(c.Lost), kindNamesOf(c.Added)) {
		case "strengthen":
			quiet[i], delegated[i] = true, true
		case "same":
			quiet[i] = true
		}
	}
	// quiet の印を除いた印の恐れ (risks に入れる分) と、その印の変化 (確度が読む)
	loud := func() ([]*pyjson.Object, []TypeChange) {
		var risks []*pyjson.Object
		var kept []TypeChange
		for i, mark := range allMarks {
			if !quiet[i] {
				risks, kept = append(risks, mark), append(kept, changes[i])
			}
		}
		return risks, kept
	}
	loudRisks, loudChanges := loud()
	risks := slices.Concat(foundRisks, loudRisks)
	immutable, citations := s.immutableValues(risks, groups)
	// 足した候補は既存の候補の後ろに、行の順で並べる (既存の並びは変えない)
	sortByLines(citations)
	risks = append(risks, citations...)
	masks := &[2]*masker{s.newMasker(0), s.newMasker(1)}
	certainty := s.certaintyRisks(groups, s.marksReviewsOf(loudChanges), masks, docType)
	// 強める向きでも、同じ原文の文に数える確度の候補が出なければ、外した文の種類を表す語の候補を戻す。
	type lineText struct {
		line int
		text string
	}
	var strengthened []lineText
	for _, r := range certainty {
		if riskKind(r) == "certainty" && riskString(r, "subkind") == "strengthened" {
			old, _ := r.Get("old")
			text, _ := old.(string)
			line, _ := r.Get("old_line")
			n, _ := line.(int64)
			strengthened = append(strengthened, lineText{int(n), pystr.Strip(Normalize(text))})
		}
	}
	restored := 0
	for i, mark := range allMarks {
		if !delegated[i] {
			continue
		}
		old, _ := mark.Get("old")
		text, _ := old.(string)
		text = pystr.Strip(Normalize(text))
		markLine, hasLine := mark.Get("old_line")
		if !slices.ContainsFunc(strengthened, func(l lineText) bool {
			return text != "" && l.text != "" && (strings.Contains(l.text, text) || strings.Contains(text, l.text)) && (!hasLine || markLine == nil || markLine == int64(l.line))
		}) {
			quiet[i] = false
			restored++
		}
	}
	if restored > 0 {
		loudRisks, loudChanges = loud()
		risks = slices.Concat(foundRisks, loudRisks, citations)
		certainty = s.certaintyRisks(groups, s.marksReviewsOf(loudChanges), masks, docType)
	}
	var quietMarks []*pyjson.Object
	for i, mark := range allMarks {
		if quiet[i] {
			quietMarks = append(quietMarks, mark)
		}
	}
	// 許可・能力の言い方の候補と、選ぶ範囲・因果の言い方の候補、数に付いた、概数と範囲を表す語の候補も、確度とかぎかっこの候補と同じく、既存の候補の後ろに行の順で並べる。
	added := slices.Concat(certainty, s.quoteRisks(groups, masks), s.modalityRisks(modality), s.logicRisks(logic))
	for _, v := range s.qualifierRisks(groups) {
		added = append(added, v.(*pyjson.Object))
	}
	sortByLines(added)
	risks = append(risks, added...)
	// 出力から外した文の種類を表す語の候補も、状態・約束の候補を重ねないかを決めるのに使う。
	additions := s.additionRisks(groups, slices.Concat(risks, quietMarks), masks)
	// 状態の候補に写した確度の見直しだけを取り除く。数える確度は残す。
	var lost []body
	for _, r := range additions {
		if riskKind(r) == "state" {
			old, _ := r.Get("old")
			text, _ := old.(string)
			line, _ := r.Get("old_line")
			n, _ := line.(int64)
			lost = append(lost, body{"", text, int(n), 0})
		}
	}
	filtered := 0
	risks = slices.DeleteFunc(risks, func(r *pyjson.Object) bool {
		drop := riskKind(r) == "certainty_review" && slices.ContainsFunc(lost, func(b body) bool { return sameReviewRisk(b, r) })
		if drop {
			filtered++
		}
		return drop
	})
	sortByLines(additions)
	return meaningResult{risks: slices.Concat(risks, additions), immutable: immutable, quiet: quietMarks, restored: restored, filtered: filtered}
}

// meaningInputs は meaning_risks と diff() が _meaning_risks に渡す値を、正規化した 2 つの文から作る (許可・能力と選ぶ範囲・因果は、文の種類の印が作った範囲を使い回す)。
// withChanges が偽なら type_changes は None (文の種類の印も許可・能力も選ぶ範囲・因果も足さない)。
func meaningInputs(orig, rw string, withChanges bool) (changes []TypeChange, modality []ModalityChange, logic []LogicChange) {
	if !withChanges {
		return nil, nil, nil
	}
	o, r := Normalize(orig), Normalize(rw)
	cache := &typeCache{}
	changes = typeChangesOf(o, r, cache)
	return changes, ModalityChanges(o, r, cache), LogicChanges(o, r, cache)
}

// meaningRisksOf は本家の meaning_risks (JSON の meaning_risks)。withChanges が偽なら type_changes は None。
func meaningRisksOf(orig, rw string, withChanges bool, docType string) (meaningResult, *sides, []*sentenceGroup, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return meaningResult{}, nil, nil, err
	}
	groups := s.sentencePairs(full.hunks)
	changes, modality, logic := meaningInputs(orig, rw, withChanges)
	return s.meaningRisks(full, groups, changes, docType, modality, logic), s, groups, nil
}

// MeaningRisks は元の文と書き直した文の、意味が変わる恐れ (risk の並び。JSON の値の形。本家の meaning_risks)。
// withTypeChanges が偽なら文の種類の印と、許可・能力・選ぶ範囲・因果の言い方の候補を含めない。docType は文書の種類 (空なら指定なし)。
func MeaningRisks(orig, rw string, withTypeChanges bool, docType string) (pyjson.Value, error) {
	result, _, _, err := meaningRisksOf(orig, rw, withTypeChanges, docType)
	if err != nil {
		return nil, err
	}
	return riskObjectsValue(result.risks), nil
}
