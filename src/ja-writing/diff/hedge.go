package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 U の推量の分類 (本家の _classify_hedge_marks と、その下の _hedge_*)。推量を表す語だけの変化を、確度の語の強さで分ける。

var (
	// 推量の語のすぐあとの条件の形 (おそれがあれば、可能性がある場合)。条件の有無が変わった文は同じ強さにしない。
	hedgeCondition = pyre.NewLazy(`[^、。！？!?\n]{0,8}?(?:れば|たら|なら|場合|とき|時(?:は|に))`, 0)
	// 節の初めを決める字と、節の中身として比べる字 (漢字、片仮名、英数字)
	hedgeClauseStop = pyre.NewLazy(`[、。！？!?\n，,]`, 0)
	hedgeContent    = pyre.NewLazy(`[一-龥々〆ヵヶァ-ヴA-Za-z0-9０-９Ａ-Ｚａ-ｚ]`, 0)
)

// runesIndexLast は Python の text.rfind(sub, 0, end) (見つからなければ -1)。
func runesIndexLast(rs []rune, sub rune, end int) int {
	for i := min(end, len(rs)) - 1; i >= 0; i-- {
		if rs[i] == sub {
			return i
		}
	}
	return -1
}

// hedgePolarity は語のある節 (前後の読点・句点・改行まで) の否定の印の数。確度の語の範囲と、そのすぐあとに続く否定は数えない。
func hedgePolarity(text string, word *certWord, words []*certWord) int {
	rs := pyunicode.Runes(text)
	head := runesIndexLast(rs, '\n', word.start) + 1
	for _, stop := range "、。！？!?，," {
		head = max(head, runesIndexLast(rs, stop, word.start)+1)
	}
	tail := len(rs)
	if m := mustSearchAt(hedgeClauseStop, pyre.NewText(text), word.end, pyre.NoEnd); m != nil {
		tail = m.Start(0)
	}
	count := 0
	for _, mark := range negationMarks(runeSlice(rs, head, tail)) {
		a, b := mark[0], mark[1]
		if !slices.ContainsFunc(words, func(w *certWord) bool { return (a+head < w.end && b+head > w.start) || a+head == w.end }) {
			count++
		}
	}
	return count
}

// hedgeConditions はすぐあとが条件の形になっている確度の語の数。
func hedgeConditions(text string, words []*certWord) int {
	t := pyre.NewText(text)
	n := 0
	for _, w := range words {
		if mustMatch(hedgeCondition, t, w.end) != nil {
			n++
		}
	}
	return n
}

// hedgeClauseChars は語の前の、同じ節の中身の字 (読点・句点・改行から語の頭まで)。
func hedgeClauseChars(text string, start int) map[string]bool {
	rs := pyunicode.Runes(text)
	head := pyre.NewText(runeSlice(rs, max(0, start-200), start))
	cut := 0
	for _, m := range mustFinditer(hedgeClauseStop, head, 0, pyre.NoEnd) {
		cut = m.End(0)
	}
	out := map[string]bool{}
	for _, m := range mustFinditer(hedgeContent, pyre.NewText(head.Slice(cut, head.Len())), 0, pyre.NoEnd) {
		w, _ := m.Group(0)
		out[w] = true
	}
	return out
}

func disjoint(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return false
		}
	}
	return true
}

// ClassifyHedgeMarks は推量を表す語だけの変化を、確度の語の強さで分ける (本家の _classify_hedge_marks)。lostKinds・addedKinds は、変化した印の種類の名前の並び。
// same と strengthen だけを、文の種類を表す語の候補から外す。問い、否定 (述語の否定の数の違い、「とは限らない」を含む)、条件の形の違い、表外語を先に除き、
// 推量を表す語に重なる確度の語をすべて打ち消せたか、残った語がどちら向きに変わったかを見る。
func ClassifyHedgeMarks(orig, rewrite string, lostKinds, addedKinds []string) string {
	kinds := map[string]bool{}
	for _, k := range slices.Concat(lostKinds, addedKinds) {
		kinds[k] = true
	}
	if len(kinds) != 1 || !kinds["推量"] {
		return "other_kinds"
	}
	side := 1
	if len(lostKinds) > 0 {
		side = 0
	}
	texts := [2]string{orig, rewrite}
	var words [2][]*certWord
	var hits [2][][2]int
	for i, text := range texts {
		words[i] = certaintyWordSpans(text)
		for _, m := range markPositions(text) {
			if m.kind == "推量" {
				hits[i] = append(hits[i], [2]int{m.start, m.start + pyunicode.RuneCount(m.hit)})
			}
		}
	}
	for i, text := range texts {
		rs := pyunicode.Runes(text)
		spans := sentenceSpans(text)
		if len(spans) == 0 {
			spans = [][2]int{{0, len(rs)}}
		}
		for _, span := range spans {
			sentence := pystr.Strip(runeSlice(rs, span[0], span[1]))
			if sentence != "" && certaintyQuestionText(sentence) {
				if slices.ContainsFunc(words[i], func(w *certWord) bool { return span[0] <= w.start && w.start < span[1] }) ||
					slices.ContainsFunc(hits[i], func(h [2]int) bool { return span[0] <= h[0] && h[0] < span[1] }) {
					return "question"
				}
			}
		}
	}
	if !countsEqual(certaintyNegations(texts[0], words[0]), certaintyNegations(texts[1], words[1])) {
		return "negation"
	}
	// 「とは限らない」は起こらない見込みの言い方なので、かもしれない と打ち消さない
	for _, ws := range words {
		if slices.ContainsFunc(ws, func(w *certWord) bool { return w.name == "必ずしも" }) {
			return "negation"
		}
	}
	if hedgeConditions(texts[0], words[0]) != hedgeConditions(texts[1], words[1]) {
		return "condition"
	}
	var hitWords []*certWord
	for _, h := range hits[side] {
		var overlapping []*certWord
		for _, w := range words[side] {
			if w.start < h[1] && h[0] < w.end {
				overlapping = append(overlapping, w)
			}
		}
		if len(overlapping) == 0 {
			return "not_in_table"
		}
		if slices.ContainsFunc(overlapping, func(w *certWord) bool { return w.negative }) {
			return "negation"
		}
		// 「らしい」は伝聞とも読めるので、見込み・推測の語と打ち消さない
		sideRunes := pyunicode.Runes(texts[side])
		if slices.ContainsFunc(overlapping, func(w *certWord) bool {
			return w.name == "そう・よう" && strings.Contains(runeSlice(sideRunes, w.start, w.end), "らし")
		}) {
			return "not_in_table"
		}
		for _, w := range overlapping {
			if indexWord(hitWords, w) < 0 {
				hitWords = append(hitWords, w)
			}
		}
	}
	before, after := notNegative(words[0]), notNegative(words[1])
	restOld, restNew, pairs := cancelPairs(before, after)
	sortCertWords(restOld)
	sortCertWords(restNew)
	rest := restNew
	if side == 0 {
		rest = restOld
	}
	inHit := func(w *certWord) bool { return indexWord(hitWords, w) >= 0 }
	var left []*certWord
	for _, w := range rest {
		if inHit(w) {
			left = append(left, w)
		}
	}
	if len(left) == 0 {
		// 相手のない語が残った (再開が遅れるかもしれず、停止する見込みです → 遅れる恐れがあり、停止します)
		if len(restOld) != len(restNew) {
			return "unpaired"
		}
		for _, pair := range pairs {
			hit, other := pair[0], pair[1]
			if side == 1 {
				hit, other = pair[1], pair[0]
			}
			if !inHit(hit) {
				continue
			}
			// 数えない語 (ことがある、検討、傾向、期待できる など) は、推量の語と同じ意味の種類と言えない
			if !other.core {
				return "not_core"
			}
			// 慎重な言い方と一緒に、その節の述語の肯定と否定が替わった
			if hedgePolarity(texts[side], hit, words[side]) != hedgePolarity(texts[1-side], other, words[1-side]) {
				return "negation"
			}
			a, b := hedgeClauseChars(texts[side], hit.start), hedgeClauseChars(texts[1-side], other.start)
			if len(a) > 0 && len(b) > 0 && disjoint(a, b) {
				return "unanchored"
			}
		}
		return "same"
	}
	paired := min(len(restOld), len(restNew))
	oldBase, newBase := minLevel(before), minLevel(after)
	outcomes := map[string]bool{}
	var order []string
	add := func(s string) {
		if !outcomes[s] {
			outcomes[s] = true
			order = append(order, s)
		}
	}
	for _, w := range left {
		index := indexWord(rest, w)
		switch {
		case index < paired:
			otherList := restOld
			if side == 0 {
				otherList = restNew
			}
			other := otherList[index]
			up := w.level > other.level
			if side == 0 {
				up = other.level > w.level
			}
			if up {
				add("strengthen")
			} else {
				add("weaken")
			}
		case side == 0:
			if newBase > w.level {
				add("lost")
			} else {
				add("consolidated")
			}
		default:
			if oldBase > w.level {
				add("added")
			} else {
				add("consolidated")
			}
		}
	}
	if len(order) > 1 {
		return "mixed"
	}
	sort.Strings(order)
	return order[0]
}
