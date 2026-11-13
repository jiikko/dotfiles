package diff

import (
	"slices"
	"strings"
	"sync"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 M: 「一つの」「一つずつ」(数量を主張しない値 weak) の相手の文の語を見る決まり (本家の _QUANTITY_* と _quantity_changed・_drop_weak・_quantity_text ほか)。
// 打ち消しの見方 (_quantity_negated) は、否定の検査 (群 N) と共有する「少ない」「危ない」などの語 (negationWords) と打ち消しの「ず」を使う。

var (
	// 数を言う語 (書き方の違いは quantitySame でそろえてから比べる)。「いくつ」は「いくつか」「いくつも」「いくつでも」だけ
	quantityWords = pyre.NewLazy(`一斉|一気に|いっぺんに|別々|まとめて|一括|一度に|並列|並行|同時|複数|上限|無制限|いくつ(?=[かもで])`, 0)
	quantitySame  = map[string]string{"一括": "まとめて", "一度に": "まとめて", "並行": "まとめて", "並列": "まとめて", "同時": "まとめて",
		"一斉": "まとめて", "一気に": "まとめて", "いっぺんに": "まとめて", "いくつ": "複数"}
	// 形ごとに、「相手の文のどこに出ても数える」語のほか、「一つずつ」の動詞・「一つの」の名詞のすぐ前だけで見る語
	quantityClauseWords = map[string][]string{weakEach: {"複数"}, weakSingle: {"別々"}}
	quantityFor         = map[string][]string{weakEach: {"まとめて"}, weakSingle: {"複数", "上限", "無制限"}}
	quantityKeepAny     = map[string]*pyre.Lazy{
		weakSingle: pyre.NewLazy(`同じ|同一|単一`, 0),
		weakEach:   pyre.NewLazy(`(?<![`+stiffKanjiClass+`])順(?:番)?に|個別に|一件ずつ|一つずつ|ひとつずつ`, 0),
	}
	quantityOtherNoun = pyre.NewLazy(`(?:複数|いくつか|いくつも)の([`+stiffKanjiClass+`ァ-ヴーA-Za-z0-9]{1,12})`, 0)
	// 元の文の主題 (文頭の「〜は」「〜が」) と、違う主語 (名詞＋が・は)
	quantityTopic   = pyre.NewLazy(`\s*([^、。はがを]{1,20}?)(?:は|が)`, 0)
	quantitySubject = pyre.NewLazy(`([`+stiffKanjiClass+`ァ-ヴーA-Za-z0-9]{1,12}[りれえけげせめみいきしち]?)(?:が|は)`, 0)
	// 数を言う語の打ち消し
	quantityClauseEnd = pyre.NewLazy(`[、。，．,.！？!?\n]`, 0)
	quantityNeg       = pyre.NewLazy(`(?<!かまわ)(?<!構わ)(?<!問題)(?<!差し支え)(?<!少)ない|なかっ|なし|(?:では|でも|じゃ|で|ことは?)なく`+
		`|(?<!かまい)(?<!構い)(?<!問題あり)(?<!差し支えあり)ません`, 0)
	quantityLead     = pyre.NewLazy(`(?:に|で)?して(?![もは])`, 0)
	quantityScopeEnd = pyre.NewLazy(`ので(?!き)|のに|(?<!あら)ため|けど|けれど|(?<=[ぁ-ゖ])ても|(?<=[んい])でも`+
		`|(?<=[うくぐすつぬぶむるたい])から`+
		`|(?<=[いきぎしちびみりっえけげせねべめれ])て(?=[`+stiffKanjiClass+`ァ-ヴ])`+
		`|(?<=ん)で(?=[`+stiffKanjiClass+`ァ-ヴ])`, 0)
	quantityNotNeg = pyre.NewLazy(
		`なければ(?:なら|なりませ|いけ|だめ)[ぁ-ゖ]{0,4}|なくては(?:なら|なりませ|いけ|だめ)[ぁ-ゖ]{0,4}|ねば(?:なら|なりませ)[ぁ-ゖ]{0,4}`+
			`|ないと(?!し[たてま]|する).*|ざるを(?:得|え)[ぁ-ゖ]{0,4}|かもしれ[ぁ-ゖ]{0,4}|に違い(?:ない|ありません)`+
			`|(?:しか|ほか|ほかは)(?:ない|なかっ|ありません)`+
			`|(?:おこ|まか|とも)な(?=[いうえわっ])|(?<=お)はな(?=[しすさせ])|(?:み|こ)な(?=[しすさせ])`, 0)
	quantityLimitOff = pyre.NewLazy(
		`上限(?:[はがもをの]|について(?:は)?)?(?:特に)?(?:な[いくしかけ]|無[いくし]|ありません`+
			`|(?:設け|定め|決め|置か|置い)[ぁ-ゖ]{0,4}?(?:ない|なかっ|ず|ません)`+
			`|なく[さしすせなっ]|撤廃|廃止|外[さしすせ]|はず[さしすせ]|取り払|取り除|取り外|解除)`, 0)
	// 1 つの文の中で、読点で区切った節
	quantitySentenceEnd = pyre.NewLazy(`[。！？!?\n]`, 0)
	quantityClauseSplit = pyre.NewLazy(`[、，,]`, 0)
	quantityParticleEnd = pyre.NewLazy(`(?:[はもがをにでとへや]|には|では|とは|からは|までは)$`, 0)
	// 「一つの」の名詞、「一つずつ」の動詞
	quantityHead = pyre.NewLazy(`[`+stiffKanjiClass+`ァ-ヴーA-Za-z0-9]{1,12}`, 0)
	// その名詞・動詞のすぐ前の、数の主張が同じ語 (正規表現の元の文字列)
	quantityKeepBefore = map[string]string{weakSingle: `(?:同じ|同一の?|単一の?)`,
		weakEach: `(?:順に|順番に|個別に|一件ずつ|一つずつ|ひとつずつ)`}
	// 変わった語 (数の主張が変わった形)
	quantityChangeBefore = map[string]string{
		weakSingle: `(?:(?:複数|すべて|全て|全部|別々|いくつか|いくつも)の?|各|ごと[にの](?:別々?の)?)`,
		weakEach: `(?:まとめて|一括(?:で|して)?|一度に|並列(?:で|に|して)?|並行(?:で|に|して)?|同時(?:に|で)?` +
			`|一斉に|一気に|いっぺんに|複数(?:件|個|の[^、。]{1,6})?(?:を|でも)?|いくつでも|いくつも)` +
			`(?:[^、。]{1,8}?を)?`,
	}
	// 否定の形をした語の一部 (少ない、危ない など)。「な」を伏せれば、ない・なく・なかっ・なけれ のどれにも当たらない
	negationWords = pyre.NewLazy(`(?:少|危|切|仕方|間違い|もったい|つまら|くだら|たまら|情け|頼り|あっけ|はか|ぎこち|さりげ|味気|素っ気)`+
		`(な)(?=[いくかけ])`, 0)
	// 打ち消しの「ず」(狙わず、使わずに、せず)。直前は動詞の未然形の仮名、直後は読点・句点・文末・「に」・漢字・片仮名・英字
	zuPrev = "わかさたなまらがばぱせけげてでねべめれえきぎしじちにびみりい"
	zuNext = pyre.NewLazy(`[、。，．`+stiffKanjiClass+`ァ-ヴA-Za-zＡ-Ｚａ-ｚ]|に`, 0)
)

var dynamicPatterns = struct {
	sync.Mutex
	m map[string]*pyre.Pattern
}{m: map[string]*pyre.Pattern{}}

// dynamicPattern は実行時に組み立てる正規表現 (本家の re.finditer(文字列, ...) の、同じ文字列を何度も組み直さないための覚え)。
func dynamicPattern(source string) *pyre.Pattern {
	dynamicPatterns.Lock()
	defer dynamicPatterns.Unlock()
	p, ok := dynamicPatterns.m[source]
	if !ok {
		p = pyre.MustCompile(source, 0)
		dynamicPatterns.m[source] = p
	}
	return p
}

func finditerDynamic(source string, text pyre.Text) []*pyre.Match {
	ms, err := dynamicPattern(source).Finditer(text, 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return ms
}

func repeatMask(m *pyre.Match) string { return strings.Repeat("＃", m.End(0)-m.Start(0)) }

// negativeZu は text に打ち消しの「ず」があれば真 (副詞の「まず」「あえず」「かならず」は打ち消しにしない)。
func negativeZu(text string) bool {
	rs := pyunicode.Runes(text)
	t := pyre.NewText(text)
	for i, r := range rs {
		if r != 'ず' {
			continue
		}
		prev := rune(0)
		if i > 0 {
			prev = rs[i-1]
		}
		if prev == 0 || !strings.ContainsRune(zuPrev, prev) {
			continue
		}
		if prev == 'ま' && (i < 2 || !matchAt(stiffKanji, t, i-2)) {
			continue
		}
		window := runeSlice(rs, max(0, i-4), i+1)
		if strings.HasSuffix(window, "あえず") || strings.HasSuffix(window, "かならず") {
			continue
		}
		if i+1 == len(rs) || matchAt(zuNext, t, i+1) {
			return true
		}
	}
	return false
}

// quantityNegated は text の position からの、数を言う語の打ち消しがあれば真。見るのは節の終わりまでのうち、語がかかる述語の終わりまで。
// 義務や条件の言い方と平仮名の動詞の「な」、「少ない」などの語は伏せてから探す。
func quantityNegated(text pyre.Text, position int) bool {
	position = min(position, text.Len())
	end := mustSearchAt(quantityClauseEnd, text, position, pyre.NoEnd)
	stop := text.Len()
	if end != nil {
		stop = end.Start(0)
	}
	rest := text.Slice(position, max(position, stop))
	rt := pyre.NewText(rest)
	from := 0
	if lead := mustMatch(quantityLead, rt, 0); lead != nil {
		from = lead.End(0)
	}
	if cut := mustSearchAt(quantityScopeEnd, rt, from, pyre.NoEnd); cut != nil {
		rest = rt.Slice(0, cut.Start(0))
	}
	rest = mustSubFunc(quantityNotNeg, pyre.NewText(rest), repeatMask)
	rest = mustSubFunc(negationWords, pyre.NewText(rest), func(m *pyre.Match) string {
		whole, _ := m.Group(0)
		rs := pyunicode.Runes(whole)
		return pyunicode.FromRunes(rs[:len(rs)-1]) + "＃"
	})
	return mustSearchAt(quantityNeg, pyre.NewText(rest), 0, pyre.NoEnd) != nil || negativeZu(rest)
}

// quantityWordCounts は text の数を言う語 (書き方の違いをそろえた語) ごとの出た数。打ち消した語は数えない (「上限」は上限そのものを打ち消したときだけ数える)。
func quantityWordCounts(text string) map[string]int {
	t := pyre.NewText(text)
	counts := map[string]int{}
	for _, m := range mustFinditer(quantityWords, t, 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		if same, ok := quantitySame[word]; ok {
			word = same
		}
		if word == "上限" {
			if mustMatch(quantityLimitOff, t, m.Start(0)) != nil {
				counts[word]++
			}
		} else if !quantityNegated(t, m.End(0)) {
			counts[word]++
		}
	}
	return counts
}

func quantityWordSet(text string) map[string]bool {
	set := map[string]bool{}
	for w := range quantityWordCounts(text) {
		set[w] = true
	}
	return set
}

// newWords は a の語のうち b より数が増えたものの集合 (本家の set(a - b))。
func newWords(a, b map[string]int) map[string]bool {
	set := map[string]bool{}
	for w, n := range a {
		if n-b[w] > 0 {
			set[w] = true
		}
	}
	return set
}

func anyIn(set map[string]bool, words []string) bool {
	return slices.ContainsFunc(words, func(w string) bool { return set[w] })
}

// intersectWords は set のうち words にもあるものの集合。
func intersectWords(set map[string]bool, words map[string]bool) map[string]bool {
	out := map[string]bool{}
	for w := range set {
		if words[w] {
			out[w] = true
		}
	}
	return out
}

func wordsOf(words []string) map[string]bool {
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	return set
}

// weakHead は「一つの」の名詞、「一つずつ」の動詞 (doc の中の number のすぐあとの「の」「ずつ」に続く、漢字・片仮名・英数字の並び)。なければ ""。
func weakHead(doc pyre.Text, n number) string {
	skip := 2
	if n.weak == weakSingle {
		skip = 1
	}
	if doc.Len() == 0 {
		return ""
	}
	if m := mustMatch(quantityHead, doc, n.end+skip); m != nil {
		whole, _ := m.Group(0)
		return whole
	}
	return ""
}

// headClauses は相手の文 text が 1 つの文で、head が出るなら、head の出る節 (読点で区切る。助詞で終わる節は次の節も) をつないだ文字列。
// 2 つ以上の文か、head が出なければ ok が偽。
func headClauses(text, head string) (string, bool) {
	body := pystrRStripChars(text)
	if !strings.Contains(body, head) || mustSearchAt(quantitySentenceEnd, pyre.NewText(body), 0, pyre.NoEnd) != nil {
		return "", false
	}
	clauses := splitClauses(body)
	var picked []string
	for index, clause := range clauses {
		if !strings.Contains(clause, head) {
			continue
		}
		picked = append(picked, clause)
		for particleEnded(clauses[index]) && index+1 < len(clauses) {
			index++
			picked = append(picked, clauses[index])
		}
	}
	return strings.Join(picked, "、"), true
}

func pystrRStripChars(text string) string {
	return strings.TrimRight(text, " \t\n。！？!?」』）)")
}

func splitClauses(text string) []string {
	parts, err := quantityClauseSplit.Get().Split(pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Value
	}
	return out
}

func particleEnded(clause string) bool {
	return mustSearchAt(quantityParticleEnd, pyre.NewText(pystr.Strip(clause)), 0, pyre.NoEnd) != nil
}

// oneSentenceWithout は相手の文 text が 1 つの文で、head が出ないなら真 (言い換えた推敲)。
func oneSentenceWithout(text, head string) bool {
	body := pystrRStripChars(text)
	return !strings.Contains(body, head) && mustSearchAt(quantitySentenceEnd, pyre.NewText(body), 0, pyre.NoEnd) == nil
}

// otherSubjectMasked は partnerText の節 (読点で区切る) のうち、ownText の主題と違う主語を持ち、その主語のほかに主題の語が出ない節を伏せた文字列。
// 助詞で終わる節は次の節と 1 つにして見る。主題が取れなければ partnerText のまま。
func otherSubjectMasked(ownText, partnerText string) string {
	topicMatch := mustMatch(quantityTopic, pyre.NewText(ownText), 0)
	topic := ""
	if topicMatch != nil {
		g, _ := topicMatch.Group(1)
		topic = pystr.Strip(g)
	}
	if topic == "" {
		return partnerText
	}
	var clauses []string
	for _, clause := range splitClauses(partnerText) {
		if len(clauses) > 0 && particleEnded(clauses[len(clauses)-1]) {
			clauses[len(clauses)-1] += "、" + clause
		} else {
			clauses = append(clauses, clause)
		}
	}
	out := make([]string, len(clauses))
	for i, clause := range clauses {
		ct := pyre.NewText(clause)
		subject := mustSearchAt(quantitySubject, ct, 0, pyre.NoEnd)
		if subject != nil {
			word, _ := subject.Group(1)
			replaced := ct.Slice(0, subject.Start(1)) + "＃" + ct.Slice(subject.End(1), ct.Len())
			if !strings.Contains(topic, word) && !strings.Contains(replaced, topic) {
				clause = strings.Repeat("＃", ct.Len())
			}
		}
		out[i] = clause
	}
	return strings.Join(out, "、")
}

// quantityChanged は weak の値 n (「一つの」「一つずつ」) の数の主張が、相手の文 (partnerText) で変わったか。
// 1. 相手の文で、「一つの」の名詞や「一つずつ」の動詞のすぐ前に数の主張が変わった語があり、自分の文にその形がなければ変わった。
// 2. すぐ前に数の主張が同じ語があれば変わっていない。
// 3. ほかは、相手の文に新しく出た数を言う語 (partnerWords) が、その形の語 (quantityFor) であれば変わった。相手の文が 1 つの文で名詞・動詞が出るなら、その語のある節の語だけを見る。
// 4・5. 名詞・動詞が取れないときや、1 つの文でその語が出ないときは、複数・いくつ・別々も相手の文のどこに出ても数える (数の主張が同じ語があるときと、元の文にない名詞の「複数の〜」は数えない)。
// どれも打ち消した語は見ない。
func quantityChanged(n number, doc pyre.Text, ownText, partnerText string, partnerWords map[string]bool) bool {
	words, ok := quantityFor[n.weak]
	if !ok {
		return false
	}
	head := weakHead(doc, n)
	pt := pyre.NewText(partnerText)
	if head == "" {
		return anyIn(partnerWords, words) || anyIn(partnerWords, quantityClauseWords[n.weak])
	}
	for _, m := range finditerDynamic(quantityChangeBefore[n.weak]+pyre.Escape(head), pt) {
		whole, _ := m.Group(0)
		if !strings.Contains(ownText, whole) && !quantityNegated(pt, m.End(0)) {
			return true
		}
	}
	if n.weak == weakSingle {
		for _, m := range finditerDynamic(pyre.Escape(head)+"(?:を|は|が|も)?別々", pt) {
			whole, _ := m.Group(0)
			if !strings.Contains(ownText, whole) && !quantityNegated(pt, m.End(0)) {
				return true
			}
		}
	}
	for _, m := range finditerDynamic(quantityKeepBefore[n.weak]+pyre.Escape(head), pt) {
		if !quantityNegated(pt, m.End(0)) {
			return false
		}
	}
	scope, hasScope := headClauses(partnerText, head)
	if !hasScope && oneSentenceWithout(partnerText, head) {
		if anyIn(partnerWords, words) {
			return true
		}
		for _, m := range mustFinditer(quantityKeepAny[n.weak], pt, 0, pyre.NoEnd) {
			if !quantityNegated(pt, m.End(0)) {
				return false
			}
		}
		extra := intersectWords(partnerWords, wordsOf(quantityClauseWords[n.weak]))
		if len(extra) > 0 {
			extra = intersectWords(extra, quantityWordSet(otherSubjectMasked(ownText, partnerText)))
		}
		if len(extra) > 0 && n.weak == weakEach {
			masked := mustSubFunc(quantityOtherNoun, pt, func(m *pyre.Match) string {
				noun, _ := m.Group(1)
				if strings.Contains(ownText, noun) {
					whole, _ := m.Group(0)
					return whole
				}
				return repeatMask(m)
			})
			extra = intersectWords(extra, quantityWordSet(masked))
		}
		return len(extra) > 0
	}
	if hasScope && anyIn(partnerWords, words) {
		partnerWords = intersectWords(partnerWords, quantityWordSet(scope))
	}
	if hasScope && anyIn(partnerWords, quantityClauseWords[n.weak]) {
		scopeWords := quantityWordSet(scope)
		if anyIn(scopeWords, quantityClauseWords[n.weak]) && (n.weak != weakSingle || strings.Contains(scope, "別々に")) {
			return true
		}
	}
	return anyIn(partnerWords, words)
}

// textRatio は 2 つの文の似かた (pairSimilar と同じく、2,000 字を超えれば前後の同じ字の数から求める)。
func textRatio(a, b []rune) float64 {
	if len(a) > charAlignLimit || len(b) > charAlignLimit {
		return affixRatio(a, b)
	}
	return difflib.New[rune](a, b).Ratio()
}

// ratioBound は textRatio の上限。2,000 字以下なら quick_ratio (字の多重集合の重なり。ratio 以上)。超えれば 1.0。
func ratioBound(a, b []rune) float64 {
	if len(a) > charAlignLimit || len(b) > charAlignLimit {
		return 1.0
	}
	return difflib.New[rune](a, b).QuickRatio()
}
