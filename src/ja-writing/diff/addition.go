package diff

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 V: 書き足した内容の恐れ (本家の _addition_risks。kind は state・promise・promise_review・claim)。
// 原文で言っていた状態 (未確認・検討中など) が推敲後に消えた文 (state)、原文に無い約束・保証・再発防止を足した文 (promise・promise_review)、
// 原文に無い理由・原因・結果のつなぎを足した文 (claim) を、文の組ごとに比べて出す。

var (
	addDialogueOnly = pyre.NewLazy(`^[^「」『』、。\s]{0,8}[「『].*[」』][。．]?$`, 0)
	addKaEnd        = pyre.NewLazy(`か[」』）)]*$`, 0)
	addSendOffTail  = pyre.NewLazy(`り(?:と(?:な|し|させ|いたし)|にな|です|でした|だ|。|$)`, 0)
	addStemUnfinish = pyre.NewLazy(`未([`+addK+`]{1,3})`, 0)
	addStemNeg      = pyre.NewLazy(`([`+addK+`ァ-ヴー]{1,4})[ぁ-ん]{0,4}(?:`+addNeg+`|`+addPerfNeg+`)$`, 0)
	addPlainStemBad = pyre.NewLazy(`(?:れる|られる|できる|なる|ある|いる|える|める|わる|まる|がる)$`, 0)
	addPolitePast   = pyre.NewLazy(`(?:ます|です|でしょう)$`, 0)
	addResultHead   = pyre.NewLazy(`につなが|をもたら|に寄与|に貢献|が功`, 0)
	addPromiseLead  = pyre.NewLazy(`(?:予定|つもり|方針)(?:は|が|も)?(?:ない|なく|ありません|ございません|なし)`, 0)
	addOldFutureNeg = pyre.NewLazy(`いたします$`, 0)
	addOldFutureEnd = pyre.NewLazy(`(?:です|でした|だ|である)$`, 0)
	addPastLater    = pyre.NewLazy(`(?:ます|する|ない|ません)(?:が|ので|、)`, 0)
	addKanjiTwoTail = pyre.NewLazy(`[`+addK+`]{2}$`, 0)
	addOldNegative  = pyre.NewLazy(`(?<!あり)(?<!ござい)(?:ない|ません)(?=[。、]|$|ので|が)`, 0)
	addBigramDrop   = pyre.NewLazy(`[\s、。，．・「」『』（）()]`, 0)
	addSplitLine    = pyre.NewLazy(`(?<=[。\n])`, 0)
)

// pyReEscape は Python 3.7 以降の re.escape (特殊文字だけを逃がす)。
func pyReEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("()[]{}?*+-|^$\\.&~# \t\n\r\v\f", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// addStateWord は状態の語 1 つ (本家の (家族, 名前, 位置, 語))。
type addStateWord struct {
	family, name string
	pos          int
	word         string
}

func overlapsTaken(taken [][2]int, start, end int) bool {
	return slices.ContainsFunc(taken, func(s [2]int) bool { return s[0] < end && start < s[1] })
}

// additionStateWords は原文の状態の語を、表の順に重なりを除いて、位置の順で返す (本家の _addition_state_words)。
func additionStateWords(text string) []addStateWord {
	rs := pyunicode.Runes(text)
	t := pyre.NewText(text)
	var found []addStateWord
	var taken [][2]int
	for _, row := range addState {
		for _, m := range mustFinditer(row.regex, t, 0, pyre.NoEnd) {
			start, end := m.Start(0), m.End(0)
			if overlapsTaken(taken, start, end) {
				continue
			}
			taken = append(taken, [2]int{start, end})
			if mustMatch(addCond, t, end) != nil || mustMatch(addStateDenied, t, end) != nil {
				continue
			}
			cut := max(runesIndexLast(rs, '。', start), runesIndexLast(rs, '、', start)) + 1
			if strings.HasSuffix(row.name, "（読点）") && !strings.ContainsAny(runeSlice(rs, cut, start), "はが") {
				continue
			}
			if row.name == "見送り" && !addSendOffObject.MustSearch(runeSlice(rs, runesIndexLast(rs, '。', start)+1, start)) &&
				!addSendOffTail.MustMatchAt0(runeSlice(rs, start+2, len(rs))) {
				continue
			}
			word, _ := m.Group(0)
			found = append(found, addStateWord{row.family, row.name, start, word})
		}
	}
	slices.SortStableFunc(found, func(a, b addStateWord) int { return cmp.Compare(a.pos, b.pos) })
	return found
}

// additionTopic は状態の語の前の、同じ節の内容語 (何の状態か)。別の文に移ったとみなすときに照らす。
func additionTopic(text string, pos int) []string {
	rs := pyunicode.Runes(text)
	keep := func(words []string) []string {
		var out []string
		for _, w := range words {
			if !slices.Contains(addTopicStop, w) {
				out = append(out, w)
			}
		}
		return out
	}
	start := max(runesIndexLast(rs, '。', pos), runesIndexLast(rs, '、', pos)) + 1
	words := keep(findall(addContent, runeSlice(rs, start, pos)))
	if len(words) == 0 {
		words = keep(findall(addContent, runeSlice(rs, max(runesIndexLast(rs, '。', pos)+1, 0), pos)))
	}
	return words
}

// additionOnTopic は topic の語か、その語の 2 字の並びが text にあるか (topic が空なら真)。
func additionOnTopic(topic []string, text string) bool {
	if len(topic) == 0 {
		return true
	}
	for _, w := range topic {
		if strings.Contains(text, w) {
			return true
		}
		rs := pyunicode.Runes(w)
		for i := range len(rs) - 1 {
			if strings.Contains(text, pyunicode.FromRunes(rs[i:i+2])) {
				return true
			}
		}
	}
	return false
}

// additionStemNeg は「未X」の X と、「まだ〜ない」の述語の頭 (2 字)。推敲後に X + 否定 があれば残ったとみなす。
func additionStemNeg(word string) (string, bool) {
	if m := mustMatch(addStemUnfinish, pyre.NewText(word), 0); m != nil {
		s, _ := m.Group(1)
		return s, true
	}
	if m := mustSearchAt(addStemNeg, pyre.NewText(word), 0, pyre.NoEnd); m != nil {
		s, _ := m.Group(1)
		rs := pyunicode.Runes(s)
		return pyunicode.FromRunes(pySliceAny(rs, -2, len(rs))), true
	}
	return "", false
}

// promiseClause は推敲後の文の約束の句 1 つ (本家の (種類, 名前, key, 位置, 書き手の印))。
type promiseClause struct {
	sub, name, key string
	pos            int
	strong         bool
}

// additionPromiseClauses は推敲後の文の約束の句。会話の行、問いの文、依頼、名乗り・あいさつの定型は見ない。
// 書き手の印は、謙譲の形 (いたします、させていただきます、お・ご〜します、てまいります)、未来の印、書き手の主語のどれか。
func additionPromiseClauses(text string) []promiseClause {
	if text == "" || addDialogueOnly.MustSearch(text) {
		return nil
	}
	rs := pyunicode.Runes(text)
	t := pyre.NewText(text)
	stripped := pystr.RStrip(text)
	question := isQuestion(stripped) || addKaEnd.MustSearch(stripped)
	var found []promiseClause
	var taken [][2]int
	for _, row := range addPromise {
		for _, m := range mustFinditer(row.regex, t, 0, pyre.NoEnd) {
			start, end := m.Start(0), m.End(0)
			if overlapsTaken(taken, start, end) {
				continue
			}
			// 見ないと決めた位置も取っておく (あとの広い型が同じ位置の一部を拾わないように)
			taken = append(taken, [2]int{start, end})
			key, ok := groupByName(m, "k")
			if !ok || key == "" {
				key, _ = groupByName(m, "k2")
			}
			clauseStart := max(runesIndexLast(rs, '。', start)+1, 0)
			clauseEnd := runeFind(rs, []rune("。"), end, math.MaxInt)
			clauseStop := clauseEnd
			if clauseEnd < 0 {
				clauseStop = len(rs)
			}
			clause := runeSlice(rs, clauseStart, clauseStop)
			window := pySlice(rs, max(0, start-2), end+8)
			if question && clauseEnd < 0 {
				continue
			}
			openQuote := slices.Contains(rs[:start], '「') && !slices.Contains(rs[max(runesIndexLast(rs, '「', start), 0):start], '」')
			if addRequest.MustSearch(pyunicode.FromRunes(window)) || openQuote {
				continue
			}
			future := addFutureMark.MustSearch(clause)
			strong := slices.Contains([]string{"させていただく", "いたします", "てまいります", "お〜します"}, row.name) || future || addWriterMark.MustSearch(clause)
			if row.sub == "prevention" {
				tail := runeSlice(rs, end, clauseStop)
				if addPreventNotAct.MustSearch(runeSlice(pyunicode.Runes(tail), 0, 24)) || (strings.HasSuffix(key, "防止") && !addPreventAct.MustSearch(tail)) {
					continue
				}
			}
			if row.sub == "promise" {
				if addPerformative.MustSearch(pyunicode.FromRunes(pySliceAny(window, 0, len(window)-8))) {
					continue
				}
				head := pyunicode.FromRunes(dropHonorificPrefix(pyunicode.Runes(key)))
				sahen := row.name == "サ変します" || row.name == "動詞ます"
				if sahen && pystr.EndsWithAny(head, addIntransitiveSahen) {
					continue
				}
				if sahen && !strong && pystr.EndsWithAny(head, addAgentiveSahen) {
					continue
				}
				if row.name == "動詞ます" {
					stem := runeSlice(rs, start, end-2)
					if addIntransitiveTail.MustSearch(stem) || pystr.EndsWithAny(stem, addStative) {
						continue
					}
					if addERow.MustSearch(stem) && !addFutureMark.MustSearch(clause) {
						continue
					}
				}
				if row.name == "常体の約束" {
					stem := runeSlice(rs, start, end)
					if !addFutureMark.MustSearch(clause) || addPlainStemBad.MustSearch(stem) || addPolitePast.MustSearch(stem) {
						continue
					}
				}
				// 名乗りの文は見ない。ただし未来の印 (追って、改めて、後日) があれば約束として見る。
				later := addLaterMark.MustSearch(clause)
				if addReport.MustSearch(key) && (!later || (addPerfMark.MustSearch(clause) && !addLaterStrong.MustSearch(clause))) {
					continue
				}
				if addReportMaybe.MustSearch(key) && addPerfMark.MustSearch(clause) && !future {
					continue
				}
				if addOtherSubject.MustSearch(clause) {
					continue
				}
			}
			found = append(found, promiseClause{row.sub, row.name, key, start, strong || row.sub != "promise"})
		}
	}
	return found
}

// dropHonorificPrefix は re.sub("^[おご]", "", key) (先頭の 1 字だけ外す)。
func dropHonorificPrefix(rs []rune) []rune {
	if len(rs) > 0 && (rs[0] == 'お' || rs[0] == 'ご') {
		return rs[1:]
	}
	return rs
}

// additionKeyHead は約束の述語の頭。お・ご を外した漢字かカタカナの続き (1 字の漢字なら次のかな 1 字まで)。
func additionKeyHead(key string) string {
	rs := dropHonorificPrefix(pyunicode.Runes(key))
	n := 0
	for n < len(rs) && isAddKChar(rs[n]) {
		n++
	}
	if n == 0 {
		return runeSlice(rs, 0, 2)
	}
	if n == 1 && len(rs) > 1 {
		n = 2
	}
	return runeSlice(rs, 0, n)
}

// additionPredStem は述語の字 (時の字を外した漢字とカタカナ)。予定・つもりのような印だけなら、その前の動詞の字。昇順で重ねずに返す。
func additionPredStem(text []rune, end int) []rune {
	left := text[:min(end, len(text))]
	for range 2 {
		i := len(left)
		for i > 0 && isAddHiragana(left[i-1]) {
			i--
		}
		for i > 0 && isAddKChar(left[i-1]) {
			i--
		}
		seg := left[i:]
		var stem []rune
		for _, c := range seg {
			if isAddKChar(c) && !slices.Contains(addPredSkip, c) && !slices.Contains(stem, c) {
				stem = append(stem, c)
			}
		}
		if len(stem) > 0 || len(seg) == 0 {
			slices.Sort(stem)
			return stem
		}
		left = left[:len(left)-len(seg)]
	}
	return nil
}

// additionOldFutures は原文の文の、書き手の未来の行為や決まりを言える述語の字の並び (言い換えの打ち消しに使う。広く取る)。
func additionOldFutures(text string) [][]rune {
	if addPastEnd.MustSearch(pystr.RStrip(text)) && !addPastLater.MustSearch(text) {
		return nil
	}
	if isQuestion(text) || strings.HasSuffix(pystr.RStrip(text), "か") {
		return nil
	}
	rs := pyunicode.Runes(text)
	var out [][]rune
	for _, m := range mustFinditer(addOldFuture, pyre.NewText(text), 0, pyre.NoEnd) {
		start, end := m.Start(0), m.End(0)
		if addPromiseLead.MustMatchAt0(runeSlice(rs, start, start+12)) {
			continue
		}
		s := runeSlice(rs, max(0, start-4), end)
		if (addOldStative.MustSearch(s) && !addOldFutureNeg.MustSearch(s)) || addOldIntransitive.MustSearch(s) || addOldFutureEnd.MustSearch(s) {
			continue
		}
		out = append(out, additionPredStem(rs, end))
	}
	// 体言止めのサ変 (「次回は20日に開催。」)
	stripped := pyunicode.Runes(pystr.RStripChars(pystr.RStrip(text), "。"))
	if addKanjiTwoTail.MustSearch(pyunicode.FromRunes(stripped)) {
		out = append(out, additionPredStem(stripped, len(stripped)))
	}
	return out
}

func additionOldNegatives(text string) [][]rune {
	rs := pyunicode.Runes(text)
	var out [][]rune
	for _, m := range mustFinditer(addOldNegative, pyre.NewText(text), 0, pyre.NoEnd) {
		out = append(out, additionPredStem(rs, m.End(0)))
	}
	return out
}

// additionSameAct は原文の述語の字が、推敲後の約束の句 (読点から句まで) と同じ行為を言えるか。字のない述語 (やります) はどれとも合う。
func additionSameAct(stem []rune, part string) bool {
	if len(stem) == 0 {
		return true
	}
	for _, c := range stem {
		if strings.ContainsRune(part, c) {
			return true
		}
	}
	for _, c := range stem {
		for _, word := range addPredSynonyms[c] {
			if strings.Contains(part, word) {
				return true
			}
		}
	}
	return false
}

// causalWord は理由・原因と手段・結果のつなぎの語 1 つ (本家の (家族, 語, 始め, 終わり))。
type causalWord struct {
	family, word string
	start, end   int
}

func additionCausalWords(text string) []causalWord {
	rs := pyunicode.Runes(text)
	t := pyre.NewText(text)
	var found []causalWord
	var taken [][2]int
	for _, row := range addCausal {
		for _, m := range mustFinditer(row.regex, t, 0, pyre.NoEnd) {
			if overlapsTaken(taken, m.Start(0), m.End(0)) {
				continue
			}
			taken = append(taken, [2]int{m.Start(0), m.End(0)})
			word, _ := m.Group(0)
			found = append(found, causalWord{row.family, word, m.Start(0), m.End(0)})
		}
	}
	slices.SortStableFunc(found, func(a, b causalWord) int { return cmp.Compare(a.start, b.start) })
	var out []causalWord
	for _, w := range found {
		if strings.HasPrefix(w.word, "結果") && addInquiryResult.MustSearch(runeSlice(rs, 0, w.start)) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// additionBigrams は記号と空白を除いた文の、2 字の組の集まり。
func additionBigrams(text string) map[string]bool {
	rs := pyunicode.Runes(addBigramDrop.MustSub("", text))
	out := map[string]bool{}
	for i := 0; i+1 < len(rs); i++ {
		out[pyunicode.FromRunes(rs[i:i+2])] = true
	}
	return out
}

// additionNovelty は span の語のうち、どの 2 字も原文 (oldBigrams はその 2 字の組) にない語の割合。
func additionNovelty(span string, oldBigrams map[string]bool) float64 {
	words := findall(addContent, span)
	if len(words) == 0 {
		return 0
	}
	fresh := 0
	for _, w := range words {
		rs := pyunicode.Runes(w)
		known := false
		for i := 0; i+1 < len(rs); i++ {
			if oldBigrams[pyunicode.FromRunes(rs[i:i+2])] {
				known = true
				break
			}
		}
		if !known {
			fresh++
		}
	}
	return float64(fresh) / float64(len(words))
}

func additionTimeToCause(old, span string) bool {
	cause := map[string]bool{}
	for _, w := range findall(addContent, span) {
		cause[w] = true
	}
	rs := pyunicode.Runes(old)
	for _, m := range mustFinditer(addTimeLink, pyre.NewText(old), 0, pyre.NoEnd) {
		start := m.Start(0)
		cut := max(runesIndexLast(rs, '。', start), runesIndexLast(rs, '、', start)) + 1
		for _, w := range findall(addContent, runeSlice(rs, cut, start)) {
			if cause[w] {
				return true
			}
		}
	}
	return false
}

func additionCauseSpan(text string, family string, start, end int) string {
	rs := pyunicode.Runes(text)
	if family == "result" && !addResultHead.MustMatchAt0(runeSlice(rs, start, len(rs))) {
		stop := runeFind(rs, []rune("。"), end, math.MaxInt)
		if stop < 0 {
			stop = len(rs)
		}
		return runeSlice(rs, end, stop)
	}
	cut := max(runesIndexLast(rs, '、', start), runesIndexLast(rs, '。', start), runesIndexLast(rs, 'は', start))
	return runeSlice(rs, cut+1, start)
}

type keptKey struct{ family, word, text string }

// additionDoc は文書全体の根拠と語の検索結果 (本家の _AdditionDocument)。検出中に同じ本文を何度も走査しない。
type additionDoc struct {
	raw       string
	bodies    [2][]body
	oldText   string
	nonpast   string
	nonpastRx *pyre.Text
	prevent   bool
	states    map[string][]addStateWord
	kept      map[keptKey]bool
	heads     map[string]bool
	promises  map[string][]promiseClause
	causals   map[string][]causalWord
	oldGrams  map[string]bool
	counts    [2]map[string]int
	byKind    map[string][]*pyjson.Object
}

func (s *sides) newAdditionDoc(masks *[2]*masker, risks []*pyjson.Object) *additionDoc {
	d := &additionDoc{raw: pyunicode.FromRunes(s.texts[0]), states: map[string][]addStateWord{}, kept: map[keptKey]bool{}, heads: map[string]bool{},
		promises: map[string][]promiseClause{}, causals: map[string][]causalWord{}, byKind: map[string][]*pyjson.Object{}}
	for side := range 2 {
		d.bodies[side] = s.allBodiesWith(side, masks[side])
	}
	var old strings.Builder
	for _, b := range d.bodies[0] {
		old.WriteString(b.text)
	}
	d.oldText = old.String()
	var nonpast strings.Builder
	for _, piece := range addSplitLine.MustSplit(d.raw) {
		if !addPastEnd.MustSearch(pystr.RStrip(piece)) {
			nonpast.WriteString(piece)
		}
	}
	d.nonpast = nonpast.String()
	rawText := pyre.NewText(d.raw)
	for _, m := range mustFinditer(addPreventAny, rawText, 0, pyre.NoEnd) {
		if mustMatch(addPreventUnsettled, rawText, m.End(0)) == nil {
			d.prevent = true
			break
		}
	}
	for side := range 2 {
		d.counts[side] = map[string]int{}
		for _, b := range d.bodies[side] {
			for _, w := range d.stateWords(b.text) {
				d.counts[side][w.family]++
			}
		}
	}
	for _, r := range risks {
		d.byKind[riskKind(r)] = append(d.byKind[riskKind(r)], r)
	}
	return d
}

// oldBigrams は原文の本文 (oldText) の 2 字の組の集まり (additionNovelty で、語ごとに原文全体を探さないため。初めて要るときに作る)。
func (d *additionDoc) oldBigrams() map[string]bool {
	if d.oldGrams == nil {
		rs := pyunicode.Runes(d.oldText)
		d.oldGrams = map[string]bool{}
		for k := 0; k+1 < len(rs); k++ {
			d.oldGrams[pyunicode.FromRunes(rs[k:k+2])] = true
		}
	}
	return d.oldGrams
}

func (d *additionDoc) promiseClauses(text string) []promiseClause {
	found, ok := d.promises[text]
	if !ok {
		found = additionPromiseClauses(text)
		d.promises[text] = found
	}
	return found
}

func (d *additionDoc) causalWords(text string) []causalWord {
	found, ok := d.causals[text]
	if !ok {
		found = additionCausalWords(text)
		d.causals[text] = found
	}
	return found
}

func (d *additionDoc) stateWords(text string) []addStateWord {
	found, ok := d.states[text]
	if !ok {
		if addStateAny.MustSearch(text) {
			found = additionStateWords(text)
		}
		d.states[text] = found
	}
	return found
}

func (d *additionDoc) stateKept(family, word, text string) bool {
	key := keptKey{family, word, text}
	if kept, ok := d.kept[key]; ok {
		return kept
	}
	families := append([]string{family}, addKeepCross[family]...)
	kept := slices.ContainsFunc(d.stateWords(text), func(w addStateWord) bool { return slices.Contains(families, w.family) })
	for _, rx := range addKeepExtraRx[family] {
		kept = kept || rx.MustSearch(text)
	}
	if !kept && strings.HasSuffix(word, "中") && slices.Contains([]string{"investigating", "considering", "in_progress"}, family) {
		wr := pyunicode.Runes(word)
		kept = searchDynamic(pyReEscape(pyunicode.FromRunes(wr[:len(wr)-1]))+addProgressive, pyre.NewText(text))
	}
	if (family == "unknown" || family == "not_done") && !addDone.MustSearch(text) {
		kept = kept || addAnyNeg.MustSearch(addApology.MustSub("", text))
	}
	if stem, ok := additionStemNeg(word); !kept && ok {
		kept = searchDynamic(pyReEscape(stem)+`[^。、]{0,8}?(?:`+addNeg+`|`+addPerfNeg+`)`, pyre.NewText(text))
	}
	d.kept[key] = kept
	return kept
}

func (d *additionDoc) sameSentence(kind string, b body) []*pyjson.Object {
	var out []*pyjson.Object
	for _, r := range d.byKind[kind] {
		if sameReviewRisk(b, r) {
			out = append(out, r)
		}
	}
	return out
}

func (d *additionDoc) hasHead(head string) bool {
	if has, ok := d.heads[head]; ok {
		return has
	}
	has := false
	if head != "" {
		if d.nonpastRx == nil {
			t := pyre.NewText(d.nonpast)
			d.nonpastRx = &t
		}
		has = searchDynamic(pyReEscape(head)+`(?![`+addK+`ァ-ヴー])`, *d.nonpastRx)
	}
	d.heads[head] = has
	return has
}

func searchDynamic(source string, t pyre.Text) bool {
	m, err := dynamicPattern(source).Search(t, 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// additionGroup は既存の組を本文にし、隣の未対応文と打ち消し用の数を共有する (本家の _AdditionGroup)。i・j は組の相手の文の番号 (無いときは noPair)。
type additionGroup struct {
	bodies     [2][]body
	partner    [2]map[int]int
	paired     [2]map[int]bool
	contexts   map[[2]int][]int
	texts      map[[2]int]string
	future     [][][]rune
	negative   [][][]rune
	usedPreds  []map[int]bool
	causal     []int
	gramIndex  map[string][]int
	gramSizes  []int
	covers     map[string][]int
	joinedLine map[string][]int
}

const noPair = -1

func newAdditionGroup(g *sentenceGroup, masks *[2]*masker) *additionGroup {
	out := &additionGroup{contexts: map[[2]int][]int{}, texts: map[[2]int]string{}, covers: map[string][]int{}, joinedLine: map[string][]int{}}
	for side, list := range [2][]sentence{g.old, g.new} {
		for _, sn := range list {
			b, ok := masks[side].body(sn)
			if !ok {
				b = body{"", sn.text, sn.line, sn.start}
			}
			out.bodies[side] = append(out.bodies[side], b)
		}
		out.partner[side], out.paired[side] = map[int]int{}, map[int]bool{}
	}
	for _, p := range g.pairs {
		out.partner[0][p[0]], out.partner[1][p[1]] = p[1], p[0]
		out.paired[0][p[0]], out.paired[1][p[1]] = true, true
	}
	return out
}

// prepareFuture は約束の句があるときだけ、原文の述語を取る。
func (g *additionGroup) prepareFuture() {
	if g.future != nil {
		return
	}
	type preds struct{ futures, negatives [][]rune }
	cache := map[string]preds{}
	g.future, g.negative = [][][]rune{}, [][][]rune{}
	for _, b := range g.bodies[0] {
		p, ok := cache[b.text]
		if !ok {
			p = preds{additionOldFutures(b.text), additionOldNegatives(b.text)}
			cache[b.text] = p
		}
		g.future = append(g.future, slices.Clone(p.futures))
		g.negative = append(g.negative, slices.Clone(p.negatives))
		g.usedPreds = append(g.usedPreds, map[int]bool{})
	}
}

func (g *additionGroup) prepareCausal(doc *additionDoc) {
	if g.causal != nil {
		return
	}
	g.causal = make([]int, len(g.bodies[0]))
	for k, b := range g.bodies[0] {
		g.causal[k] = len(doc.causalWords(b.text))
	}
}

// context は文 index の番号と、すぐ前後に続く組にならなかった文の番号 (index が noPair なら、組にならなかった文の全部)。
func (g *additionGroup) context(side, index int) []int {
	key := [2]int{side, index}
	if c, ok := g.contexts[key]; ok {
		return c
	}
	var indices []int
	if index == noPair {
		for k := range g.bodies[side] {
			if !g.paired[side][k] {
				indices = append(indices, k)
			}
		}
	} else {
		indices = append([]int{index}, unpairedNeighbors(len(g.bodies[side]), index, g.paired[side])...)
	}
	g.contexts[key] = indices
	return indices
}

func (g *additionGroup) text(side, index int) string {
	key := [2]int{side, index}
	if t, ok := g.texts[key]; ok {
		return t
	}
	var b strings.Builder
	for _, k := range g.context(side, index) {
		b.WriteString(g.bodies[side][k].text)
	}
	g.texts[key] = b.String()
	return g.texts[key]
}

func (g *additionGroup) risk(kind, subkind string, i, j int, detail pyjson.Value) *pyjson.Object {
	var old, new body
	if i != noPair {
		old = g.bodies[0][i]
	}
	if j != noPair {
		new = g.bodies[1][j]
	}
	return newRisk(kind, subkind, old.line, new.line, old.raw, new.raw, i != noPair, j != noPair, detail)
}

// covered は文 text の 2 字の並びから、重なりの多い原文の文を逆引きする (全組み合わせで本文を読み直さない)。
func (g *additionGroup) covered(text string) []int {
	if c, ok := g.covers[text]; ok {
		return c
	}
	if g.gramIndex == nil {
		g.gramIndex = map[string][]int{}
		for k, b := range g.bodies[0] {
			grams := additionBigrams(b.text)
			g.gramSizes = append(g.gramSizes, len(grams))
			for gram := range grams {
				g.gramIndex[gram] = append(g.gramIndex[gram], k)
			}
		}
	}
	counts := map[int]int{}
	for gram := range additionBigrams(text) {
		for _, k := range g.gramIndex[gram] {
			counts[k]++
		}
	}
	covered := []int{}
	for k, n := range counts {
		if float64(n)/float64(g.gramSizes[k]) >= 0.4 {
			covered = append(covered, k)
		}
	}
	slices.Sort(covered)
	g.covers[text] = covered
	lines := make([]int, len(covered))
	for n, k := range covered {
		lines[n] = g.bodies[0][k].line
	}
	g.joinedLine[text] = lines
	return covered
}

// riskDetailMarkKinds は risk["detail"].get(key, []) の各項目の kind。
func riskDetailMarkKinds(r *pyjson.Object, key string) []string {
	detail, _ := r.Get("detail")
	d, _ := detail.(*pyjson.Object)
	if d == nil {
		return nil
	}
	v, _ := d.Get(key)
	list, _ := v.([]pyjson.Value)
	var out []string
	for _, e := range list {
		if o, ok := e.(*pyjson.Object); ok {
			kind, _ := o.Get("kind")
			s, _ := kind.(string)
			out = append(out, s)
		}
	}
	return out
}

// marksMatch は文 b と同じ原文の、文の種類の印の恐れ (subkind marks) のうち、消えた種類と足された種類が pred を満たすものがあるか。
func (d *additionDoc) marksMatch(b body, pred func(lost, added []string) bool) bool {
	return slices.ContainsFunc(d.sameSentence("sentence_type", b), func(r *pyjson.Object) bool {
		return riskString(r, "subkind") == "marks" && pred(riskDetailMarkKinds(r, "lost"), riskDetailMarkKinds(r, "added"))
	})
}

// riskDetailSteps は risk["detail"]["steps"]。
func riskDetailSteps(r *pyjson.Object) []pyjson.Value {
	detail, _ := r.Get("detail")
	d, _ := detail.(*pyjson.Object)
	v, _ := d.Get("steps")
	list, _ := v.([]pyjson.Value)
	return list
}

func (d *additionDoc) stateRisks(g *additionGroup) []*pyjson.Object {
	var out []*pyjson.Object
	for i, b := range g.bodies[0] {
		j, hasPair := g.partner[0][i]
		if !hasPair {
			j = noPair
		}
		if b.text == "" || (hasPair && b.text == g.bodies[1][j].text) {
			continue
		}
		// 否定の候補があれば、その候補に任せる。
		if slices.ContainsFunc(d.byKind["negation"], func(r *pyjson.Object) bool {
			old, _ := r.Get("old")
			text, _ := old.(string)
			return riskLineIs(r, "old_line", b.line) && Normalize(text) == Normalize(b.raw)
		}) {
			continue
		}
		// 推量を表す語が足された文は、文の種類を表す語の候補に任せる (確度の候補と同じ扱い)。
		if d.marksMatch(b, func(_, added []string) bool { return hasHedgeKind(added) }) {
			continue
		}
		var lostWords []string
		families := map[string]bool{}
		for _, w := range d.stateWords(b.text) {
			topic := additionTopic(b.text, w.pos)
			near := g.context(1, j)
			if slices.ContainsFunc(near, func(k int) bool {
				return d.stateKept(w.family, w.word, g.bodies[1][k].text) && (k == j || additionOnTopic(topic, g.bodies[1][k].text))
			}) {
				continue
			}
			if d.counts[1][w.family] >= d.counts[0][w.family] && slices.ContainsFunc(d.bodies[1], func(nb body) bool {
				return d.stateKept(w.family, w.word, nb.text) && additionOnTopic(topic, nb.text)
			}) {
				continue
			}
			lostWords = append(lostWords, w.word)
			families[w.family] = true
		}
		if len(lostWords) == 0 {
			continue
		}
		steps := []pyjson.Value{}
		for _, r := range d.sameSentence("certainty_review", b) {
			steps = append(steps, riskDetailSteps(r)...)
		}
		famList := make([]string, 0, len(families))
		for f := range families {
			famList = append(famList, f)
		}
		slices.Sort(famList)
		detail := pyjson.NewObject()
		detail.Set("words", pyjson.Strings(lostWords))
		detail.Set("families", pyjson.Strings(famList))
		detail.Set("certainty_steps", steps)
		out = append(out, g.risk("state", "lost", i, j, detail))
	}
	return out
}

func (d *additionDoc) promiseRisk(g *additionGroup, i, j int) []*pyjson.Object {
	b := g.bodies[1][j]
	clauses := d.promiseClauses(b.text)
	if len(clauses) == 0 {
		return nil
	}
	g.prepareFuture()
	context := g.context(0, i)
	oldText := g.text(0, i)
	rs := pyunicode.Runes(b.text)
	var kept []promiseClause
	for _, c := range clauses {
		switch c.sub {
		case "prevention":
			if d.prevent {
				continue
			}
		case "guarantee":
			if addGuaranteeFamilyRx[c.name].MustSearch(d.raw) {
				continue
			}
		default:
			head := additionKeyHead(c.key)
			if d.hasHead(head) || (head != "" && i != noPair && searchDynamic(pyReEscape(head)+`(?![`+addK+`ァ-ヴー])`, pyre.NewText(oldText))) {
				continue
			}
		}
		// 一つの原文の述語は、同じ行為を言える約束の句を一つだけ打ち消す (読みます → 破棄します は打ち消さない)。
		canCancel := c.sub == "promise" || (c.sub == "guarantee" && (c.name == "ことはない" || c.name == "変えない") && i != noPair)
		if !canCancel {
			kept = append(kept, c)
			continue
		}
		partStart := max(runesIndexLast(rs, '、', c.pos), runesIndexLast(rs, '。', c.pos)) + 1
		part := runeSlice(rs, partStart, c.pos+len(pyunicode.Runes(c.key))+4)
		// 「〜ことはない」は、否定の数が組の原文の文より増えたとき (複製しません → 複製しないため、漏れることはありません) は
		// 足した言い切りなので、組の原文の文の述語で字を問わずに打ち消さない (起きない → 起きることはない は打ち消す)。
		paired := i
		if c.name == "ことはない" && i != noPair && len(negationMarks(b.text)) > len(negationMarks(g.bodies[0][i].text)) {
			paired = noPair
		}
		if !g.cancel(context, c.sub, part, paired) {
			kept = append(kept, c)
		}
	}
	withoutPromises := func(list []promiseClause) []promiseClause {
		return slices.DeleteFunc(list, func(c promiseClause) bool { return c.sub == "promise" })
	}
	if i != noPair && len(d.sameSentence("certainty", g.bodies[0][i])) > 0 {
		kept = withoutPromises(kept)
	}
	// 読み手への依頼が消えて書き手の行為になった文は、文の種類を表す語の候補に任せる。
	if i != noPair && !addRequest.MustSearch(b.text) && d.marksMatch(g.bodies[0][i], func(lost, added []string) bool {
		return slices.Contains(lost, "依頼・決まり") && !slices.Contains(added, "依頼・決まり")
	}) {
		kept = withoutPromises(kept)
	}
	var counted, weak []promiseClause
	for _, c := range kept {
		if c.strong {
			counted = append(counted, c)
		} else {
			weak = append(weak, c)
		}
	}
	detailOf := func(list []promiseClause) pyjson.Value {
		phrases, names := make([]string, len(list)), make([]string, len(list))
		for n, c := range list {
			phrases[n] = runeSlice(rs, c.pos, c.pos+max(len(pyunicode.Runes(c.key)), 2)+6)
			names[n] = c.name
		}
		detail := pyjson.NewObject()
		detail.Set("phrases", pyjson.Strings(phrases))
		detail.Set("names", pyjson.Strings(names))
		return detail
	}
	var out []*pyjson.Object
	if len(counted) > 0 {
		subkind := "added"
		for _, sub := range []string{"prevention", "guarantee"} {
			if slices.ContainsFunc(counted, func(c promiseClause) bool { return c.sub == sub }) {
				subkind = sub
				break
			}
		}
		out = append(out, g.risk("promise", subkind, i, j, detailOf(counted)))
	}
	if len(weak) > 0 {
		out = append(out, g.risk("promise_review", "added", i, j, detailOf(weak)))
	}
	return out
}

// cancel は組の原文の文の述語を、言い換えとして字を問わずに打ち消す (確かめます → チェックします)。
// 隣の文や組にならなかった文の述語は、同じ行為を言えるときだけ打ち消す。
func (g *additionGroup) cancel(context []int, sub, part string, paired int) bool {
	for _, k := range context {
		type pred struct {
			n    int
			stem []rune
		}
		var preds []pred
		for n, stem := range g.future[k] {
			preds = append(preds, pred{n, stem})
		}
		if sub == "guarantee" {
			for n, stem := range g.negative[k] {
				preds = append(preds, pred{len(g.future[k]) + n, stem})
			}
		}
		for _, p := range preds {
			if !g.usedPreds[k][p.n] && (k == paired || additionSameAct(p.stem, part)) {
				g.usedPreds[k][p.n] = true
				return true
			}
		}
	}
	return false
}

func (d *additionDoc) claimRisks(g *additionGroup, i, j int) []*pyjson.Object {
	b := g.bodies[1][j]
	var remaining []causalWord
	causes := d.causalWords(b.text)
	if len(causes) > 0 {
		g.prepareCausal(d)
	}
	for _, cause := range causes {
		found := false
		for _, k := range g.context(0, i) {
			if g.causal[k] > 0 {
				g.causal[k]--
				found = true
				break
			}
		}
		if !found {
			remaining = append(remaining, cause)
		}
	}
	if len(remaining) == 0 {
		return nil
	}
	covered := g.covered(b.text)
	joined := len(covered) >= 2
	var words []string
	for _, w := range remaining {
		if joined || additionNovelty(additionCauseSpan(b.text, w.family, w.start, w.end), d.oldBigrams()) >= 0.5 {
			words = append(words, w.word)
		}
	}
	reason := "new_content"
	if joined {
		reason = "joined"
	}
	if len(words) == 0 && i != noPair {
		// 原文の「〜時に」「〜際に」を、同じ中身のまま原因の語にした (集中時に → 集中したことで)。
		old := g.bodies[0][i].text
		for _, w := range remaining {
			if w.family == "means" && additionTimeToCause(old, additionCauseSpan(b.text, w.family, w.start, w.end)) {
				words = append(words, w.word)
			}
		}
		reason = "time_to_cause"
	}
	if len(words) == 0 {
		return nil
	}
	lines := []pyjson.Value{}
	if joined {
		for _, line := range g.joinedLine[b.text] {
			lines = append(lines, int64(line))
		}
	}
	detail := pyjson.NewObject()
	detail.Set("words", pyjson.Strings(words))
	detail.Set("reason", reason)
	detail.Set("joined_lines", lines)
	return []*pyjson.Object{g.risk("claim", "causal", i, j, detail)}
}

// additionRisks は状態の消えと書き足し (本家の _addition_risks)。文の対応とマスクは既存の検出から受け取る。
func (s *sides) additionRisks(groups []*sentenceGroup, risks []*pyjson.Object, masks *[2]*masker) []*pyjson.Object {
	if len(groups) == 0 {
		return nil
	}
	var prepared []*additionGroup
	oldTexts, newTexts := map[string]bool{}, map[string]bool{}
	for _, src := range groups {
		g := newAdditionGroup(src, masks)
		prepared = append(prepared, g)
		for _, b := range g.bodies[0] {
			oldTexts[b.text] = true
		}
		for _, b := range g.bodies[1] {
			newTexts[b.text] = true
		}
	}
	// 手がかりが一つもない変更では、文書全体の根拠を走査しない。伏せた本文で調べるので、コードを外して隣り合った語も取りこぼさない。
	hasState := false
	for text := range oldTexts {
		if addStateAny.MustSearch(text) {
			hasState = true
			break
		}
	}
	if !hasState {
		hit := false
		for text := range newTexts {
			for _, row := range addPromise {
				hit = hit || row.regex.MustSearch(text)
			}
			for _, row := range addCausal {
				hit = hit || row.regex.MustSearch(text)
			}
		}
		if !hit {
			return nil
		}
	}
	doc := s.newAdditionDoc(masks, risks)
	var out []*pyjson.Object
	for _, g := range prepared {
		out = append(out, doc.stateRisks(g)...)
		var claims []*pyjson.Object
		for j, b := range g.bodies[1] {
			i, hasPair := g.partner[1][j]
			if !hasPair {
				i = noPair
			}
			if b.text == "" || (hasPair && b.text == g.bodies[0][i].text) {
				continue
			}
			out = append(out, doc.promiseRisk(g, i, j)...)
			claims = append(claims, doc.claimRisks(g, i, j)...)
		}
		out = append(out, claims...)
	}
	return out
}

// typeChangesOf は正規化した 2 つの文の、文の種類の印の変化 (本家の diff() が _type_changes に渡す opcodes を作ってから比べる)。
func typeChangesOf(o, r string) []TypeChange {
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	return TypeChanges(o, r, opcodes, &typeCache{})
}

// AdditionRisks は元の文と書き直した文の、状態の消え・約束・因果の足しの候補 (risk の並び。JSON の値の形)。docType は文書の種類 (空なら指定なし)。
// 既存の候補として、本家の _meaning_risks が渡す種類のうちこの検出が読む 3 つ (文の種類の印・確度・否定) を渡す。
func AdditionRisks(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	changes := typeChangesOf(Normalize(orig), Normalize(rw))
	marks := s.marksReviewsOf(changes)
	risks := slices.Concat(s.marksRisks(changes), s.certaintyRisks(groups, marks, nil, docType))
	for _, item := range s.negationRisks(groups) {
		risks = append(risks, item.risk)
	}
	return riskObjectsValue(s.additionRisks(groups, risks, &[2]*masker{s.newMasker(0), s.newMasker(1)})), nil
}
