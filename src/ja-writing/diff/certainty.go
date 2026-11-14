package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 U: 確度 (本家の _certainty_change・_certainty_risks。kind certainty と certainty_review)。
// 可能性・見込み・予定・確実の語を強さで打ち消して比べ、強さが動いた段を出す。--fail-on に数えるのは、強さ 1 と 2 の core の語が強まった段だけ。

var (
	// 事実や仕様を述べる種類 (countedTypes) で数える、頻度の言い方 (core でない語) の言い切り
	certaintyCountedByDocTypeFrom = []string{"場合がある", "ことがある"}
	// 「場合がある」「ことがある」を消しても、書き直した文に残っていれば言い切りにならない頻度の言い方。この語のある文では、種類で数える言い切りにしない。
	certaintyFrequency = pyre.NewLazy(`まれに|稀に|ときどき|時々|時折|ときおり|たまに|一部|によっては|場合によって|こともある`, 0)
)

// 断定に強さ 5 の語を足した段を、種類で数えない原文の言い方 (決まりを言う文の「必須にする」を「必ず実施する」にした書き換えは、決まりの強さを言い換えただけのことが多い)。
// 文の種類を表す語の「依頼・決まり」と同じ表に、必須、ものとする、手順の「〜すること。」を足したもの。
var certaintyDuty = pyre.NewLazyFrom(func() string {
	for _, k := range typeKinds {
		if k.name == "依頼・決まり" {
			return k.pattern.Pattern() + `|必須|ものと(?:する|します)|こと(?=[。]|$)`
		}
	}
	panic("文の種類の表に 依頼・決まり が無い")
}, pyre.MULTILINE)

// certaintyStep は強さが動いた段 1 つ。
type certaintyStep struct {
	from, to           string
	fromLevel, toLevel int
	counted            bool
	byDocType          bool
}

func (s certaintyStep) value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("from", s.from)
	o.Set("to", s.to)
	o.Set("from_level", int64(s.fromLevel))
	o.Set("to_level", int64(s.toLevel))
	o.Set("counted", s.counted)
	if s.byDocType {
		o.Set("counted_by", "doc_type")
	}
	return o
}

// certaintyResult は _certainty_change の戻り値 (kind・向き・detail)。
type certaintyResult struct {
	kind, direction string
	oldWords        pyjson.Value
	newWords        pyjson.Value
	steps           []certaintyStep
	mixed           bool
}

func (r certaintyResult) detail() pyjson.Value {
	d := pyjson.NewObject()
	d.Set("old_words", r.oldWords)
	d.Set("new_words", r.newWords)
	d.Set("steps", pyjson.MapList(r.steps, certaintyStep.value))
	d.Set("mixed", r.mixed)
	return d
}

func wordNames(words []*certWord) pyjson.Value {
	return pyjson.MapList(words, func(w *certWord) pyjson.Value { return w.name })
}

func minLevel(words []*certWord) int {
	base := 4
	for _, w := range words {
		base = min(base, w.level)
	}
	return base
}

type certaintyTransition struct{ from, to *certWord }

// certaintyChange は強さで打ち消して比べる (本家の _certainty_change)。oldContext と newContext は、隣の組にならなかった文の語のリスト (打ち消しだけに使い、
// 新しい段にはしない。使った語は取り除く)。確度を否定する形は、両側で同じなら外して残りを比べ、違えば比べない。
// splitContext は newContext のうち、元の文から分けた文とみなした文の語のリスト。打ち消しのあとに残った強さ 5 の語を、書き直した文の語に足して比べる。
// docType が countedTypes なら、「場合がある」「ことがある」を消して言い切った段と、断定に強さ 5 の語を足した段 (duty が偽のとき) も数え、その段に counted_by を付ける。
func certaintyChange(old, new string, oldContext, newContext, splitContext []*[]*certWord, docType string, duty bool) *certaintyResult {
	if certaintyQuestionText(old) || certaintyQuestionText(new) {
		return nil
	}
	before, after := certaintyWordSpans(old), certaintyWordSpans(new)
	if !countsEqual(certaintyNegations(old, before), certaintyNegations(new, after)) {
		return nil
	}
	before, after = notNegative(before), notNegative(after)
	restOld, restNew := cancelSameLevel(before, after)
	restOld = cancelWithContext(restOld, newContext)
	restNew = cancelWithContext(restNew, oldContext)
	type splitWord struct {
		pool *[]*certWord
		word *certWord
	}
	var split []splitWord
	for _, pool := range splitContext {
		for _, word := range *pool {
			if word.level == 5 {
				split = append(split, splitWord{pool, word})
			}
		}
	}
	for _, sw := range split {
		restNew = append(restNew, sw.word)
	}
	oldBase, newBase := minLevel(before), minLevel(after)
	sortCertWords(restOld)
	sortCertWords(restNew)
	paired := min(len(restOld), len(restNew))
	var transitions []certaintyTransition
	for i := range paired {
		transitions = append(transitions, certaintyTransition{restOld[i], restNew[i]})
	}
	for _, w := range restOld[paired:] {
		if (w.level < 4 && newBase > w.level) || w.level == 5 {
			transitions = append(transitions, certaintyTransition{w, nil})
		}
	}
	for _, w := range restNew[paired:] {
		if (w.level < 4 && w.level < oldBase) || w.level == 5 {
			transitions = append(transitions, certaintyTransition{nil, w})
		}
	}
	for _, sw := range split {
		used := slices.ContainsFunc(transitions, func(tr certaintyTransition) bool { return tr.to == sw.word })
		if used && slices.ContainsFunc(*sw.pool, func(w *certWord) bool { return *w == *sw.word }) {
			*sw.pool = removeWord(*sw.pool, sw.word)
		}
	}
	byDocType := slices.Contains(countedTypes, docType)
	frequent := byDocType && certaintyFrequency.MustSearch(new)
	var steps []certaintyStep
	for _, tr := range transitions {
		fromLevel, toLevel := 4, 4
		fromName, toName := "断定", "断定"
		if tr.from != nil {
			fromLevel, fromName = tr.from.level, tr.from.name
		}
		if tr.to != nil {
			toLevel, toName = tr.to.level, tr.to.name
		}
		if fromLevel == toLevel {
			continue
		}
		step := certaintyStep{from: fromName, to: toName, fromLevel: fromLevel, toLevel: toLevel,
			counted: tr.from != nil && tr.from.core && slices.Contains(certaintyCountedFrom, fromLevel) && toLevel > fromLevel}
		if byDocType && !step.counted && ((tr.from != nil && slices.Contains(certaintyCountedByDocTypeFrom, tr.from.name) && toLevel >= 4 && !frequent) ||
			(tr.from == nil && toLevel == 5 && !duty)) {
			step.counted, step.byDocType = true, true
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return nil
	}
	up := slices.ContainsFunc(steps, func(s certaintyStep) bool { return s.toLevel > s.fromLevel })
	down := slices.ContainsFunc(steps, func(s certaintyStep) bool { return s.toLevel < s.fromLevel })
	r := &certaintyResult{kind: "certainty_review", direction: "weakened", oldWords: wordNames(before), newWords: wordNames(after), steps: steps, mixed: up && down}
	if slices.ContainsFunc(steps, func(s certaintyStep) bool { return s.counted }) {
		r.kind = "certainty"
	}
	if up {
		r.direction = "strengthened"
	}
	return r
}

// hasHedgeKind は文の種類の印に、推量か意見があるか (確度と状態の候補が、文の種類の印の候補に任せる文を決める)。
func hasHedgeKind(kinds []string) bool {
	return slices.ContainsFunc(kinds, func(kind string) bool { return kind == "推量" || kind == "意見" })
}

// marksReview は文の種類の印の恐れ (kind sentence_type・subkind marks) のうち、確度が読む欄 (本家の risks から取り出すもの)。
type marksReview struct {
	oldLine     int
	old         string
	lost, added []string
}

// marksReviewsOf は文の種類の印の変化を、確度が読む形にする。
func (s *sides) marksReviewsOf(changes []TypeChange) []marksReview {
	out := make([]marksReview, len(changes))
	for i, c := range changes {
		out[i] = marksReview{oldLine: s.findLine(0, c.Orig), old: c.Orig}
		for _, k := range c.Lost {
			out[i].lost = append(out[i].lost, k.Kind)
		}
		for _, k := range c.Added {
			out[i].added = append(out[i].added, k.Kind)
		}
	}
	return out
}

// sameReviewSentence は hedges と確度で共通の、原文の文の照らし方 (本家の _same_review_sentence)。
func sameReviewSentence(b body, r marksReview) bool { return sameReviewText(b, r.oldLine, r.old) }

// sameReviewRisk は sameReviewSentence を、risk の dict (old_line が null なら行を問わず、old が null なら空) に使う形。
func sameReviewRisk(b body, r *pyjson.Object) bool {
	var oldLine int
	if v, _ := r.Get("old_line"); v != nil {
		oldLine = int(v.(int64))
	}
	old, _ := r.Get("old")
	text, _ := old.(string)
	return sameReviewText(b, oldLine, text)
}

// sameReviewText は oldLine (0 なら行を問わない) と old の文が、b と同じ文か。行が違えば偽で、同じ行なら正規化した文の含み合いで見る。
func sameReviewText(b body, oldLine int, old string) bool {
	if oldLine != 0 && oldLine != b.line {
		return false
	}
	text, other := pystr.Strip(Normalize(b.raw)), pystr.Strip(Normalize(old))
	return strings.Contains(other, text) || strings.Contains(text, other)
}

// unpairedNeighbors は組の文 index のすぐ前とすぐあとに続く、組にならなかった文の番号 (本家の _unpaired_neighbors)。
func unpairedNeighbors(count, index int, paired map[int]bool) []int {
	first := index
	for first > 0 && !paired[first-1] {
		first--
	}
	last := index
	for last+1 < count && !paired[last+1] {
		last++
	}
	var out []int
	for k := first; k <= last; k++ {
		if k != index {
			out = append(out, k)
		}
	}
	return out
}

// certaintyRisks は確度の候補 (本家の _certainty_risks)。組 (i, j) ごとに比べる。組にならなかった隣の書き直した文のうち、2 文字の組の半分以上が元の文 i にある文は、
// 元の文から分けた文とみなし、その文の強さ 5 の語も比べる。masks は元と後の masker (nil なら本文の数えない行の種類で作る)。docType は certaintyChange に渡す。
func (s *sides) certaintyRisks(groups []*sentenceGroup, marks []marksReview, masks *[2]*masker, docType string) []*pyjson.Object {
	if masks == nil {
		masks = &[2]*masker{s.newMasker(0), s.newMasker(1)}
	}
	var out []*pyjson.Object
	for _, group := range groups {
		// 塊ごとに一度だけ伏せる。全文書き換えでも組ごとに全文を走査しない。
		var bodies [2][]body
		for side, list := range [2][]sentence{group.old, group.new} {
			for _, sn := range list {
				b, ok := masks[side].body(sn)
				if !ok {
					b = body{"", sn.text, sn.line, sn.start}
				}
				bodies[side] = append(bodies[side], b)
			}
		}
		paired := [2]map[int]bool{{}, {}}
		for _, p := range group.pairs {
			paired[0][p[0]], paired[1][p[1]] = true, true
		}
		pools := [2]map[int]*[]*certWord{{}, {}}
		pool := func(side, index int) *[]*certWord {
			if p, ok := pools[side][index]; ok {
				return p
			}
			words := notNegative(certaintyWordSpans(bodies[side][index].text))
			pools[side][index] = &words
			return &words
		}
		for _, p := range group.pairs {
			i, j := p[0], p[1]
			old, new := bodies[0][i], bodies[1][j]
			if old.text == "" || new.text == "" || old.text == new.text {
				continue
			}
			neighbors := [2][]int{unpairedNeighbors(len(bodies[0]), i, paired[0]), unpairedNeighbors(len(bodies[1]), j, paired[1])}
			var contexts [2][]*[]*certWord
			for side := range 2 {
				for _, k := range neighbors[side] {
					contexts[side] = append(contexts[side], pool(side, k))
				}
			}
			oldGrams := bigrams(pyunicode.Runes(old.text))
			var split []*[]*certWord
			for _, k := range neighbors[1] {
				grams := bigrams(pyunicode.Runes(bodies[1][k].text))
				common := 0
				for g := range grams {
					if _, ok := oldGrams[g]; ok {
						common++
					}
				}
				if len(grams) > 0 && common*2 >= len(grams) {
					split = append(split, pool(1, k))
				}
			}
			duty := certaintyDuty.MustSearch(old.text)
			for _, k := range neighbors[0] {
				duty = duty || certaintyDuty.MustSearch(bodies[0][k].text)
			}
			change := certaintyChange(old.text, new.text, contexts[0], contexts[1], split, docType, duty)
			if change == nil {
				continue
			}
			if slices.ContainsFunc(marks, func(r marksReview) bool {
				kinds := r.added
				if change.direction == "strengthened" {
					kinds = r.lost
				}
				return sameReviewSentence(old, r) && hasHedgeKind(kinds)
			}) {
				continue
			}
			out = append(out, newRisk(change.kind, change.direction, old.line, new.line, old.raw, new.raw, true, true, change.detail()))
		}
	}
	return out
}

// CertaintyRisks は元の文と書き直した文の、確度の候補 (risk の並び。JSON の値の形)。docType は文書の種類 (空なら指定なし)。
func CertaintyRisks(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	o, r := Normalize(orig), Normalize(rw)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	marks := s.marksReviewsOf(TypeChanges(o, r, opcodes, &typeCache{}))
	return riskObjectsValue(s.certaintyRisks(s.sentencePairs(full.hunks), marks, nil, docType)), nil
}
