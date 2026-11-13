package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G14 直訳語と語の一覧 (calque・metaphor_label・katakana_term・thin_evaluation。本家 yomiyasu_lint.py の 3417-4671)。
//
// 本家の _v220_word_findings は文書全体に手掛かりの語を 1 度当て、当たった行と、ラベルになりうる見出しと 1 行目だけを細かく見る (速さのための絞り込みだが、
// 元の文字に手掛かりが無い行は見ない形をそのまま写す)。calque の型 1〜5 は 1 文ずつ当て、1 行に 1 件で当たった言い方を並べる。
// G8 の metaphor_verb がある行は calque を出さず、translated_term がある行は型 3 を出さない (s.findings を読む)。

// g14State は G14 が文書の中で持つ状態。行ごとに決まるので、持つものは無い。
type g14State struct{}

// calqueMessage は calque の message (本家の _calque_message(types, word_types))。post.go の withWords が、allow.txt で一部の語を外した候補の message を作り直すときにも呼ぶ。
func calqueMessage(numbers []int, wordTypes []wordType) string {
	var hit []string
	for _, wt := range wordTypes {
		hit = append(hit, wt.Word)
	}
	hit = uniqueInOrder(hit)
	shown := ""
	if len(hit) > 0 {
		shown = "。当たった言い方: " + strings.Join(hit, "、")
	}
	names := make([]string, len(numbers))
	for i, n := range numbers {
		names[i] = calqueTypeNames[n]
	}
	return "直訳語（" + strings.Join(names, "。") + shown + "）" + v220MessageTail
}

// memo は初めて呼ばれたときに 1 度だけ求める値 (文ごとに決まる値を、句の一致ごとに求め直さない)。
func memo(f func() bool) func() bool {
	known, value := false, false
	return func() bool {
		if !known {
			known, value = true, f()
		}
		return value
	}
}

// sent は 1 つの文 (コードポイントの列と正規表現用の文字)。位置はコードポイントの数。
type sent struct {
	s string
	r []rune
	t pyre.Text
}

func newSent(s string) *sent { return &sent{s: s, r: pyunicode.Runes(s), t: pyre.NewText(s)} }

func (x *sent) search(p *lazyPattern, pos, end int) *pyre.Match {
	m, err := p.get().Search(x.t, pos, end)
	if err != nil {
		panic(err)
	}
	return m
}

// techMemo は文に機械やシステムの語 (calqueTech) があるか (初めて呼ばれたときに 1 度だけ求める)。
func (x *sent) techMemo() func() bool {
	return memo(func() bool { return x.searchAll(calqueTech) })
}

func (x *sent) searchAll(p *lazyPattern) bool { return x.search(p, 0, pyre.NoEnd) != nil }

func (x *sent) match(p *lazyPattern, pos int) *pyre.Match {
	m, err := p.get().Match(x.t, pos, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m
}

func (x *sent) finditer(p *lazyPattern, pos, end int) []*pyre.Match {
	ms, err := p.get().Finditer(x.t, pos, end)
	if err != nil {
		panic(err)
	}
	return ms
}

// slice は文の text[start:end] (範囲の外は切り詰める)。
func (x *sent) slice(start, end int) string { return runeSlice(x.r, start, end) }

// startsWithAt は text.startswith(prefix, pos)。
func (x *sent) startsWithAt(prefix string, pos int) bool {
	p := pyunicode.Runes(prefix)
	return pos >= 0 && pos+len(p) <= len(x.r) && slices.Equal(x.r[pos:pos+len(p)], p)
}

func groupText(m *pyre.Match) string { s, _ := m.Group(0); return s }

func isWordRune(r rune) bool { return isKanjiRune(r) || 'ァ' <= r && r <= 'ヴ' }

func isAlnumWide(r rune) bool {
	return r < 128 && (r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') || '０' <= r && r <= '９' || 'Ａ' <= r && r <= 'Ｚ' || 'ａ' <= r && r <= 'ｚ'
}

func isAsciiAlnum(r rune) bool { return r < 128 && isAlnumWide(r) }

func isAsciiAlpha(r rune) bool { return r < 128 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') }

// nounStart は本家の _v220_noun_start (position の字が漢字かカタカナか英字か)。
func (x *sent) nounStart(pos int) bool {
	if pos >= len(x.r) {
		return false
	}
	r := x.r[pos]
	return isKanjiRune(r) || isKatakanaRune(r) || isAsciiAlpha(r)
}

func (x *sent) nounOrAlnum(pos int) bool {
	return 0 <= pos && pos < len(x.r) && (isKanjiRune(x.r[pos]) || isKatakanaRune(x.r[pos]) || isAlnumWide(x.r[pos]))
}

func containsRuneIn(set map[rune]bool, text []rune) bool {
	for _, r := range text {
		if set[r] {
			return true
		}
	}
	return false
}

// calqueVerbStems は型 1 の動詞の語幹の字 (どれも 1 字)。
var calqueVerbStems = func() map[rune]bool {
	stems := map[rune]bool{}
	for _, v := range calqueVerbEach {
		stems[[]rune(v.stem)[0]] = true
	}
	return stems
}()

func hasVerbStem(x *sent) bool { return containsRuneIn(calqueVerbStems, x.r) }

// calqueSubjectEnd は本家の _calque_subject_end。limit より前で最後の、機械やシステムの語が主語の「は」「が」「も」「こそ」の終わりの位置 (なければ -1)。
// 主語の語のあとに漢字やカタカナが続いて、人や組織の語で終わるとき (サーバー管理者は) は主語にしない。
func calqueSubjectEnd(x *sent, limit int) int {
	last := -1
	for _, subject := range x.finditer(calqueSubjectWord, 0, limit) {
		end := subject.End(0)
		for extra := 0; extra <= calqueSubjectExt; extra++ {
			position := end + extra
			if position >= limit {
				break
			}
			particle := ""
			for _, item := range calqueSubjectParticles {
				if x.startsWithAt(item, position) {
					particle = item
					break
				}
			}
			if particle != "" {
				if position+pystr.Len(particle) <= limit && !mustSearch(calquePersonExt, x.slice(end, position)) {
					last = position + pystr.Len(particle)
				}
				break
			}
			r := x.r[position]
			if !isKanjiRune(r) && !isKatakanaRune(r) && !isAsciiAlnum(r) {
				break
			}
		}
	}
	return last
}

// calqueType1 は型 1 (機械やシステムが人や物の動作をする言い方)。当たれば真で、当たった動詞の辞書の形を words に足す。
func calqueType1(x *sent, words *[]string) bool {
	if !hasVerbStem(x) || !x.searchAll(calqueSubjectWord) {
		return false
	}
	type hit struct {
		start, index int
		m            *pyre.Match
	}
	var matches []hit
	for index, v := range calqueVerbEach {
		if !strings.Contains(x.s, v.stem) {
			continue
		}
		for _, m := range x.finditer(v.re, 0, pyre.NoEnd) {
			matches = append(matches, hit{m.Start(0), index, m})
		}
	}
	slices.SortFunc(matches, func(a, b hit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return a.index - b.index
	})
	found := false
	var established [][2]int
	establishedKnown := false
	covered := -1
	for _, h := range matches {
		start, name := h.start, calqueVerbEach[h.index].name
		if start < covered {
			continue // 同じ所に当たった別の動詞 (引き込む と 引く) は、表の前のほうだけを数える
		}
		if start > 0 && isKanjiRune(x.r[start-1]) && !slices.Contains(calqueCompoundOk, name) && !strings.HasPrefix(groupText(h.m), "食べ") {
			continue // 漢字の熟語の一部 (不足し、共通す、電話し、有効、朝食)
		}
		covered = h.m.End(0)
		subjectEnd := calqueSubjectEnd(x, start)
		if subjectEnd < 0 || start-subjectEnd > calqueSubjectReach || x.search(calqueSubjectBreak, subjectEnd, start) != nil {
			continue
		}
		if slices.ContainsFunc(x.finditer(calquePersonSubject, subjectEnd, start), func(person *pyre.Match) bool { return person.End(0) > subjectEnd }) {
			continue
		}
		if !establishedKnown {
			establishedKnown = true
			for _, m := range x.finditer(calqueEstablishedType1Re, 0, pyre.NoEnd) {
				established = append(established, [2]int{m.Start(0), m.End(0)})
			}
		}
		if slices.ContainsFunc(established, func(span [2]int) bool { return span[0] <= start && start < span[1] }) {
			continue
		}
		kana := groupText(x.match(calqueKanaRun, h.m.End(0)))
		if matched := groupText(h.m) + kana; endsWithAny(matched, calqueAttributiveEnd) && x.nounStart(h.m.End(0)+pystr.Len(kana)) {
			continue // 名詞を修飾する形 (拾った通知、話した内容)
		}
		if hasAnyPrefix(kana, calqueRequest) {
			continue // 読み手に頼む形 (1行を足してください)。動作をするのは読み手
		}
		*words = append(*words, name)
		found = true
	}
	return found
}

func hasAnyPrefix(s string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(s, prefix) })
}

// calqueSakiSpans は本家の _calque_saki_spans。「〜先」の当たり (calqueSakiKanji の語か、漢字 1 字と連用形の送り仮名のあとの「先」) の (始め, 終わり)。
func calqueSakiSpans(x *sent) [][2]int {
	var spans [][2]int
	for position := indexRune(x.r, '先', 0); position >= 0; position = indexRune(x.r, '先', position+1) {
		end := position + 1
		if position == 0 || (end < len(x.r) && (isKanjiRune(x.r[end]) || isKatakanaRune(x.r[end]))) {
			continue
		}
		start := -1
		for _, word := range calqueSakiKanji {
			if position >= pystr.Len(word) && x.startsWithAt(word, position-pystr.Len(word)) {
				start = position - pystr.Len(word)
				break
			}
		}
		if start < 0 && strings.ContainsRune(calqueSakiTail, x.r[position-1]) {
			if position >= 2 && isKanjiRune(x.r[position-2]) {
				start = position - 2
			} else if position >= 3 && isHiraganaRune(x.r[position-2]) && isKanjiRune(x.r[position-3]) && x.slice(position-2, position) != "より" {
				start = position - 3
			}
		}
		if start >= 0 && !x.nounOrAlnum(start-1) {
			spans = append(spans, [2]int{start, end})
		}
	}
	return spans
}

func indexRune(rs []rune, target rune, from int) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == target {
			return i
		}
	}
	return -1
}

// calqueHasOrg は本家の _calque_has_org。文に組織の名前 (総務部、情報システム課、サポートセンター) があるか。
func calqueHasOrg(x *sent) bool {
	for _, word := range calqueOrgWords {
		if strings.Contains(x.s, word) {
			return true
		}
	}
	for i, r := range x.r {
		if strings.ContainsRune(calqueOrgSuffixes, r) && i > 0 && (isKanjiRune(x.r[i-1]) || isKatakanaRune(x.r[i-1])) && (i+1 >= len(x.r) || !isKanjiRune(x.r[i+1])) {
			return true
		}
	}
	return false
}

// docTech は本家の _v220_doc_tech。技術の文書か (calqueTech の語のうち、calqueDocTechGeneral を除いた語が calqueDocTechMin 種類以上)。
func docTech(text string) bool {
	set := map[string]bool{}
	ms, err := calqueTech.get().Finditer(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	for _, m := range ms {
		if word := groupText(m); !slices.Contains(calqueDocTechGeneral, word) {
			set[word] = true
		}
	}
	return len(set) >= calqueDocTechMin
}

// calqueType2 は型 2 (実物の名前の代わりに場所のたとえを使う名詞)。docTech は技術の文書か (入口と出口の拾い方に使う)。
func calqueType2(x *sent, words *[]string, docTech bool) bool {
	if !x.searchAll(calquePlaceHead) {
		return false
	}
	found := false
	tech := x.techMemo()
	entrySubjectKnown, entrySubject := false, -1
	for _, m := range x.finditer(calquePlaceAlways, 0, pyre.NoEnd) {
		text := groupText(m)
		if text != "受け口" && x.nounStart(m.End(0)) {
			continue
		}
		if !strings.HasSuffix(text, "受け口") && !strings.HasSuffix(text, "の口") && !tech() {
			continue
		}
		if text == "受け口" {
			*words = append(*words, "受け口")
		} else {
			*words = append(*words, "口")
		}
		found = true
	}
	for _, span := range calqueSakiSpans(x) {
		start, end := span[0], span[1]
		word := x.slice(start, end)
		before := x.slice(max(0, start-8), end)
		if mustSearch(calqueSakiTime, word) || endsWithAny(before, calqueEstablishedSaki) {
			continue
		}
		// 一覧の着地先などのほかは、技術の語のある文の「〜先」だけを見る (旅行の立ち寄り先、父の働き先は見ない)。
		if !slices.ContainsFunc(calqueSakiKanji, func(kanji string) bool { return strings.HasSuffix(word, kanji+"先") }) && !tech() {
			continue
		}
		*words = append(*words, word)
		found = true
	}
	for _, m := range x.finditer(calquePlaceContext, 0, pyre.NoEnd) {
		word := groupText(m)
		if word != "の口" && x.nounStart(m.End(0)) {
			continue
		}
		before := x.slice(max(0, m.Start(0)-10), m.Start(0))
		if word == "の口" {
			if mustSearch(calqueMouthReal, x.slice(max(0, m.Start(0)-6), m.End(0))) || x.match(calqueMouthIdiom, m.Start(0)+1) != nil {
				continue
			}
			if strings.HasSuffix(before, "向け") || strings.HasSuffix(before, "用") {
				continue // calquePlaceAlways で数えた
			}
			word = "口"
		} else if mustSearch(calquePlaceReal, before) {
			continue
		}
		if word == "窓口" {
			if mustSearch(calquePlaceOrg, before) || !x.searchAll(calqueCode) || calqueHasOrg(x) {
				continue
			}
		} else if !tech() {
			entryWord := word == "入口" || word == "入り口" || word == "出口"
			if !docTech || !entryWord || x.match(calqueEntryFlow, m.End(0)) == nil {
				continue
			}
			if !entrySubjectKnown {
				entrySubjectKnown = true
				if first := x.search(calqueEntrySubject, 0, pyre.NoEnd); first != nil {
					entrySubject = first.End(0)
				}
			}
			if entrySubject < 0 || entrySubject > m.Start(0) || mustSearch(calquePlaceRealDoc, before) || x.match(calqueEntryWait, m.End(0)) != nil {
				continue
			}
		}
		*words = append(*words, word)
		found = true
	}
	return found
}

// calquePhrase は型 3 の句の正規表現 (本家の _CALQUE_PHRASE。全部の句を、表の順に「(?:…)」でつなぐ)。
var calquePhrase = newLazyFrom(func() string {
	var parts []string
	for _, group := range [][]namedLazy{calquePhrases, calquePhrasesV250} {
		for _, p := range group {
			parts = append(parts, "(?:"+p.re.pattern()+")")
		}
	}
	return strings.Join(parts, "|")
}, 0)

func allCalquePhrases() []namedLazy { return slices.Concat(calquePhrases, calquePhrasesV250) }

// calqueType3 は型 3 (英語の言い方を一語ずつ写した句)。当たった句の名前を words に足す。docTech は技術の文書か (「全〇〇に響く」の拾い方に使う)。
// 文ごとに決まる値 (機械やシステムの語があるか など) は、句の一致ごとに読み直さず、文 1 つにつき 1 回だけ求める。
func calqueType3(x *sent, words *[]string, docTech bool) bool {
	if !x.searchAll(calquePhraseHead) {
		return false
	}
	found := false
	var type1Here []string
	type1Known := false
	type1 := func() []string {
		if !type1Known {
			type1Known = true
			calqueType1(x, &type1Here)
		}
		return type1Here
	}
	tech := x.techMemo()
	literal := memo(func() bool { return x.searchAll(calqueHibikuLiteral) })
	hibiku := false
	for _, m := range x.finditer(calquePhrase, 0, pyre.NoEnd) {
		name := groupText(m)
		for _, each := range allCalquePhrases() {
			if x.match(each.re, m.Start(0)) != nil {
				name = each.name
				break
			}
		}
		if (name == calquePhraseKiku || name == "ときに効く") && x.searchAll(kikuRE["context"]) {
			continue
		}
		if name == "ときに効く" && x.searchAll(calqueEstablishedType1Re) {
			continue
		}
		if name == calquePhraseMatters {
			verbAt := indexRune(x.r, '効', m.Start(0))
			if mustSearch(kikuRE["literal_subject"], x.slice(0, verbAt)) || x.match(calqueMattersFocus, m.End(0)) != nil ||
				x.searchAll(kikuRE["context_near"]) || x.searchAll(kikuRE["timing"]) {
				continue
			}
		}
		if name == "テーブルにある" && (x.search(calqueTableSubject, 0, m.Start(0)) == nil || x.match(calqueTableAttributive, m.End(0)) != nil) {
			continue
		}
		if name == "ときに効く" && !x.searchAll(calqueKikuDecide) && !tech() {
			continue
		}
		if name == "に響く" {
			if literal() {
				continue
			}
			if hasAnyPrefix(groupText(m), calqueHibikuCost) && !tech() {
				continue
			}
		}
		if name == "機械どうし" && !x.searchAll(calqueMachineTalk) {
			continue
		}
		if name == "書き込みが通る" && strings.HasPrefix(groupText(m), "アクセス") && !tech() {
			continue
		}
		// 型 1 が同じ文で同じ動詞を当てたときは、対応表から足した形を重ねない (このキャッシュは、応答の速さに響きます)。同じ行のほかの文の型 1 とは比べない。
		if verb, ok := calquePhraseVerb[name]; ok && slices.Contains(type1(), verb) {
			continue
		}
		// 「ターン」は、ゲームの手番でなく、指示や会話、モデルの呼び出しの話のときだけ。
		if name == "ターン" && (!x.searchAll(calqueTurnContext) || x.searchAll(calqueTurnGame)) {
			continue
		}
		*words = append(*words, name)
		found = true
		if name == "に響く" {
			hibiku = true
		}
	}
	// 一覧にない「全〇〇に響く」は、技術の文書の、機械やシステムの語 (「〜機」を含む) がある文だけ。一覧の形を含む「全〇〇」は上で見たので外す。
	if docTech && !hibiku && strings.ContainsRune(x.s, '全') {
		subjectEnd, subjectKnown := -1, false
		for _, m := range x.finditer(calqueHibikuAll, 0, pyre.NoEnd) {
			if x.search(calqueHibikuListed, m.Start(0), m.End(0)) != nil || strings.HasPrefix(groupText(m), "全身") {
				continue
			}
			if literal() || (!tech() && !x.searchAll(calqueHibikuMachine)) {
				break // どれも文ごとに決まるので、ほかの一致も外れる
			}
			after := m.End(0)
			if x.startsWithAt("た", m.End(0)) {
				after++
			}
			if x.match(calqueHibikuAllNoun, after) != nil || x.startsWithAt("渡", after) || x.startsWithAt("わた", after) || x.startsWithAt("ほど", after) || x.startsWithAt("よう", after) ||
				x.startsWithAt("渡", m.End(0)) || x.startsWithAt("わた", m.End(0)) {
				continue
			}
			if !subjectKnown {
				subjectKnown = true
				if first := x.search(calqueHibikuAllLiteral, 0, pyre.NoEnd); first != nil {
					subjectEnd = first.End(0)
				}
			}
			if 0 <= subjectEnd && subjectEnd <= m.Start(0) {
				continue
			}
			if slices.Contains(type1(), "響く") {
				break
			}
			*words = append(*words, "に響く")
			found = true
			break
		}
	}
	return found
}

// calqueType4 は型 4 (英語の短い術語に当てた漢字 1 字の名詞)。当たった字 (版、印、頭、枝、題、罫) を words に足す。
func calqueType4(x *sent, words *[]string, skipVersion bool) bool {
	found := false
	add := func(word string) { *words = append(*words, word); found = true }
	if !skipVersion && strings.Contains(x.s, "版") && !x.searchAll(calqueVersionPub) && slices.ContainsFunc(calqueVersion, func(p *lazyPattern) bool { return x.searchAll(p) }) {
		add("版")
	}
	if strings.Contains(x.s, "印") && x.searchAll(calqueMark) && x.searchAll(calqueMarkContext) {
		add("印")
	}
	if strings.Contains(x.s, "頭") && x.searchAll(calqueHead) {
		add("頭")
	}
	if strings.Contains(x.s, "枝") && x.searchAll(calqueBranch) && x.searchAll(calqueBranchContext) {
		add("枝")
	}
	if strings.Contains(x.s, "の題") && x.searchAll(calqueTitle) {
		add("題")
	}
	if strings.Contains(x.s, "罫") && x.searchAll(calqueRule) && x.searchAll(calqueStyleContext) {
		add("罫")
	}
	return found
}

// calqueType5 は型 5 (英語の術語に当てた 2 字以上の名詞)。当たった言い方 (本文の字のまま) を words に足す。
func calqueType5(x *sent, words *[]string) bool {
	found := false
	for _, term := range calqueTermsV300 {
		if !strings.Contains(x.s, term.head) {
			continue
		}
		if term.context != nil && !x.searchAll(term.context) {
			continue
		}
		for _, m := range x.finditer(term.pattern, 0, pyre.NoEnd) {
			// 前に要る形は短いので、語の前の calqueBeforeWindow 字だけを見る。
			if term.before == nil || x.search(term.before, max(0, m.Start(0)-calqueBeforeWindow), m.Start(0)) != nil {
				*words = append(*words, groupText(m))
				found = true
				break
			}
		}
	}
	return found
}

// calquePlaceHead・calquePhraseHead・calqueTermHead は、文ごとに型の手掛かりの語があるかを先に見る正規表現 (ない文は型の正規表現を当てない)。
var (
	calquePlaceHead  = literalPattern(v220TriggerLiterals[v220Type1End:v220Type2End])
	calquePhraseHead = literalPattern(append(slices.Clone(v220TriggerLiterals[v220Type2End:v220Type3End]), "効", "足す", "足し", "響", "引", "通"))
	calqueTermHead   = literalPattern(v220TriggerLiterals[v220Type4End:])
	// calqueKanji1Chars は型 4 の漢字 1 字の名詞 (どれも 1 字)。
	calqueKanji1Chars = func() map[rune]bool {
		chars := map[rune]bool{}
		for _, literal := range v220TriggerLiterals[v220Type3End:v220Type4End] {
			chars[[]rune(literal)[0]] = true
		}
		return chars
	}()
)

// g14WordFindings は後から足す検出の 1 番目 (本家の _v220_word_findings)。s.findings (これまでの候補) を読んで、同じ行に metaphor_verb があれば calque を出さない・
// translated_term があれば型 3 を出さない (4588-4594)。
func (s *lintState) g14WordFindings() {
	rows := s.an.Lines
	if len(rows) == 0 {
		return
	}
	tables := v220Patterns()
	candidates := map[int]bool{}
	for _, pattern := range tables.trigger {
		for _, n := range s.candidateLines(pattern.get()) {
			candidates[n] = true
		}
	}
	// ラベルを見る行: 見出しの行と、文書の 1 行目 (frontmatter と空行を除く)。伏せる前の文字でも長すぎる行は先に外す。
	labelLines := map[int]bool{}
	for _, block := range s.an.Blocks {
		if block.Kind == "heading" {
			for _, row := range block.Lines {
				if labelMayFit(row) {
					labelLines[row.Line] = true
				}
			}
		}
	}
	for _, row := range rows {
		if row.Kind == "frontmatter" || row.Kind == "blank" {
			continue
		}
		if labelMayFit(row) {
			labelLines[row.Line] = true
		}
		break
	}
	if len(candidates) == 0 && len(labelLines) == 0 {
		return
	}
	blockedAll, blockedType3, blockedVersion := map[int]bool{}, map[int]bool{}, map[int]bool{}
	for _, f := range s.findings {
		switch f.Rule {
		case "metaphor_verb":
			blockedAll[f.Line] = true
		case "translated_term":
			blockedType3[f.Line] = true
			if slices.ContainsFunc(f.Words, func(word string) bool { return strings.Contains(word, "版") }) {
				blockedVersion[f.Line] = true
			}
		}
	}
	var lineNumbers []int
	for n := range candidates {
		lineNumbers = append(lineNumbers, n)
	}
	for n := range labelLines {
		if !candidates[n] {
			lineNumbers = append(lineNumbers, n)
		}
	}
	slices.Sort(lineNumbers)
	isDocTech := memo(func() bool { return docTech(s.an.Text) })
	for _, lineNo := range lineNumbers {
		row := rows[lineNo-1]
		if (row.Kind != "paragraph" && row.Kind != "list" && row.Kind != "heading" && row.Kind != "table") || row.QuoteDepth != 0 {
			continue
		}
		scan := s.doc.v220ScanText(lineNo)
		if pystr.Strip(scan) == "" {
			continue
		}
		snippet := pystr.Strip(row.Raw)
		label := ""
		if labelLines[lineNo] {
			label = s.metaphorLabel(row, scan)
		}
		if !candidates[lineNo] {
			continue
		}
		// 伏せたあとの行に、規則ごとの手掛かりの語があるかを先に見る。
		x := newSent(scan)
		type finder struct {
			number int
			run    func(*sent, *[]string) bool
		}
		var finders []finder
		if !blockedAll[lineNo] {
			if hasVerbStem(x) && x.searchAll(calqueSubjectWord) {
				finders = append(finders, finder{1, calqueType1})
			}
			if label == "" && x.searchAll(calquePlaceHead) {
				tech := isDocTech()
				// 見出しのラベルと同じ行の型 2 は metaphor_label だけを出す。
				finders = append(finders, finder{2, func(sn *sent, w *[]string) bool { return calqueType2(sn, w, tech) }})
			}
			if !blockedType3[lineNo] && x.searchAll(calquePhraseHead) {
				tech := isDocTech()
				finders = append(finders, finder{3, func(sn *sent, w *[]string) bool { return calqueType3(sn, w, tech) }})
			}
			if containsRuneIn(calqueKanji1Chars, x.r) {
				// 同じ行に「版」の translated_term があれば、型 4 の「版」は重ねない (ほかの語の translated_term では止めない)。
				skip := blockedVersion[lineNo]
				finders = append(finders, finder{4, func(sn *sent, w *[]string) bool { return calqueType4(sn, w, skip) }})
			}
			if x.searchAll(calqueTermHead) {
				finders = append(finders, finder{5, calqueType5})
			}
		}
		thinPossible := x.searchAll(tables.thinNoun)
		var sentences []string
		if len(finders) > 0 || thinPossible {
			for _, part := range mustFindall0(v220Sentence, scan) {
				if clean := pystr.Strip(part); clean != "" {
					sentences = append(sentences, clean)
				}
			}
		}
		var types []int
		var calqueWords []string
		var wordTypes []wordType
		for _, fd := range finders {
			before := len(calqueWords)
			hit := false
			for _, sentence := range sentences {
				if fd.run(newSent(sentence), &calqueWords) {
					hit = true
				}
			}
			if hit {
				types = append(types, fd.number)
				for _, word := range calqueWords[before:] {
					wordTypes = append(wordTypes, wordType{word, fd.number})
				}
			}
		}
		if len(types) > 0 {
			f := newFinding("calque", lineNo, "info", calqueMessage(types, wordTypes), snippet)
			f.Words = uniqueInOrder(calqueWords)
			f.WordTypes = wordTypes
			s.add(f)
		}
	}
}
