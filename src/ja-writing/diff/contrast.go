package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 S: 否定対比の主張の逆転 (本家の _contrast_risks。kind contrast_reversal。--fail-on=meaning-risk には数えない)。
// 原文の「Aではなく B」の A が、推敲後の文で重みの語と一緒に肯定で出ていたら、主張が逆になった疑いとして出す。

// affirmedWithWeight は text の中で token が否定のない節に出ていて、重みの語と同じ文にあれば、その位置を返す。なければ -1 (本家の _affirmed_with_weight)。
// clauseOnly が真なら、重みの語が token の節そのもの・その節のすぐ前の主題の句・すぐ前の重みの語だけの句にあるときだけ。
// side は原文で肯定した側の語。重みの語だけの句から重みを取ったときだけ使い、B の語が同じ節の A より後ろに肯定で出れば A を B に変えた推敲とみなして数えない。
func affirmedWithWeight(text ctext, token string, clauseOnly bool, side []string) int {
	if !text.has(contrastWeightPat) {
		return -1
	}
	tokenRunes := pyunicode.Runes(token)
	start := 0
	for {
		position := runeFind(text.rs, tokenRunes, start, len(text.rs))
		if position < 0 {
			return -1
		}
		start = position + 1
		left, right := clauseBounds(text, position)
		after := text.sub(position+len(tokenRunes), right)
		clause := text.sub(left, right)
		if negAfter(after, token) || negativeZu(after.s) {
			continue
		}
		if demoted(text, left, position, after) {
			continue // 「Aより」「Aの代わりに」「Aは後にする」などは比べて下げた形
		}
		clauseWeight := clause.has(contrastWeightPat)
		ownWeight := clauseWeight
		topicWeight := false
		if clauseOnly && !clauseWeight && left >= 2 && text.rs[left-2] == 'は' {
			// 「最優先は、Aを使うことだ」: A の節のすぐ前の主題の句の重みの語も、A の節の重みとして見る
			from, _ := clauseBounds(text, left-2)
			clauseWeight = mustSearchAt(contrastWeightPat, text.t, from, left-1) != nil
			topicWeight = clauseWeight
		}
		if clauseOnly && !clauseWeight && left >= 2 {
			// 「まず、Aで回す」: A の節のすぐ前の節が重みの語だけの句なら、A の節の重みとして見る
			from, _ := clauseBounds(text, left-2)
			clauseWeight = newCtext(pystr.Strip(runeSlice(text.rs, from, left-1))).hasMatch(contrastWeightOnly)
		}
		if clauseOnly && clauseWeight && !ownWeight && !topicWeight && sideAffirmed(text, position, token, right, side) {
			continue // 「## まず、Aの手順をBにする」: 重みの語だけの句のあとで、A を B に変えた
		}
		if clauseOnly && !clauseWeight {
			continue
		}
		if clause.has(contrastHypothesis) {
			continue // 「まずAを疑いましたが」: 検証で外れた仮説
		}
		if !clauseWeight && clause.has(contrastAdversative) {
			continue // 「Aもできますが、目的は…」
		}
		return position
	}
}

// hasMatch は pattern.match(c) (先頭から当てる) が当たるか。
func (c ctext) hasMatch(l *pyre.Lazy) bool { return mustMatch(l, c.t, 0) != nil }

// sideAffirmed は text の position にある A (token) の節で、原文で肯定した側の語 (words) のどれかが、A より後ろ (節の終わり right まで) に肯定で出れば真 (本家の _side_affirmed)。
// 「Aの」のすぐあとの語は A の句の一部として見ない。B の語のすぐあとが「も」「にも」「でも」・否定や下げる形・B を置き換えられる側にしたとき・B を退ける言い方のときは数えない。
func sideAffirmed(text ctext, position int, token string, right int, words []string) bool {
	tokenLen := len(pyunicode.Runes(token))
	for _, word := range words {
		wordRunes := pyunicode.Runes(word)
		start := position + tokenLen
		for {
			at := runeFind(text.rs, wordRunes, start, right)
			if at < 0 {
				break
			}
			start = at + 1
			if runeSlice(text.rs, position+tokenLen, at) == "の" {
				continue
			}
			after := text.sub(at+len(wordRunes), right)
			if strings.HasPrefix(after.s, "も") || strings.HasPrefix(after.s, "にも") || strings.HasPrefix(after.s, "でも") {
				continue
			}
			if negAfter(after, word) || negativeZu(after.s) || demoted(text, position, at, after) {
				continue
			}
			if after.hasMatch(contrastSideReplaced) || after.hasMatch(contrastSideRejected) {
				continue
			}
			return true
		}
	}
	return false
}

// affirmedPlain は重みの語がなくても、text の中で token (A) を B と並べて肯定している位置を返す。なければ -1 (本家の _affirmed_plain)。
// A のすぐあとが「だけでなく」など (そのあとに否定がない)、または並べる語で、原文の同じ文の A 以外の名詞 (others) が A より後ろに出る。
func affirmedPlain(text ctext, token string, others []string) int {
	tokenRunes := pyunicode.Runes(token)
	start := 0
	for {
		position := runeFind(text.rs, tokenRunes, start, len(text.rs))
		if position < 0 {
			return -1
		}
		start = position + 1
		end := position + len(tokenRunes)
		left, right := clauseBounds(text, position)
		after := text.sub(end, right)
		clause := text.sub(left, right)
		if clause.has(contrastHypothesis) {
			continue
		}
		if also := mustMatch(contrastAlsoAfter, after.t, 0); also != nil {
			rest := after.sub(also.End(0), len(after.rs))
			if !negAfter(rest, "") && !negativeZu(rest.s) {
				return position
			}
			continue
		}
		if !after.hasMatch(contrastCoord) {
			continue
		}
		tail := runeSlice(text.rs, end, len(text.rs))
		if !slices.ContainsFunc(others, func(other string) bool { return strings.Contains(tail, other) }) {
			continue
		}
		if negAfter(after, token) || negativeZu(after.s) || demoted(text, left, position, after) || clause.has(contrastCompare) || clause.has(contrastAdversative) {
			continue
		}
		return position
	}
}

// originalAffirms は原文の否定の文 (text) で、名詞 token がもともと肯定で出ているか (本家の _original_affirms)。
// 重みの語と一緒に否定のない節に出るか、否定の印より後ろ (B の側) にも出れば真。B の側の同じ名詞が、どれも直後が「の」(「道具ではなく、道具の使い方」) なら、
// B の名詞を修飾しているだけで A そのものは否定されているので、B の側に出たことでは真にしない (そのときは否定の節より前の所だけで見る)。
func originalAffirms(text ctext, token string, markEnd, markStart int) bool {
	rest := text.sub(markEnd, len(text.rs))
	tokenRunes := pyunicode.Runes(token)
	// re.finditer(re.escape(token), rest): 重ならない当たりの始まり
	var positions []int
	for at := 0; ; {
		found := runeFind(rest.rs, tokenRunes, at, len(rest.rs))
		if found < 0 {
			break
		}
		positions = append(positions, found)
		at = found + len(tokenRunes)
	}
	for _, position := range positions {
		if !hasPrefixAt(rest.rs, position+len(tokenRunes), "の") || continuesWord(rest.rs, position, token) {
			return true
		}
	}
	if len(positions) > 0 {
		left, _ := clauseBounds(text, max(0, markStart-1))
		return affirmedWithWeight(text.sub(0, left), token, false, nil) >= 0
	}
	return affirmedWithWeight(text, token, false, nil) >= 0
}

// continuesWord は text の position にある token が、直前の字と続けて 1 つの語になっているか (直前の字が token の頭の字と同じ字の種類。本家の _continues_word)。
func continuesWord(text []rune, position int, token string) bool {
	if position == 0 {
		return false
	}
	before := pyre.NewText(string(text[position-1]))
	head := pyre.NewText(string(pyunicode.Runes(token)[0]))
	for _, pattern := range []*pyre.Lazy{stiffKanji, contrastKatakanaHdr, contrastLatinHdr} {
		if matchAt(pattern, head, 0) {
			return matchAt(pattern, before, 0)
		}
	}
	return false
}

// bSidePhrases は原文の否定を表す語より後ろ (B の側) の「Aの<名詞>」の句 (本家の _b_side_phrases)。
// 推敲後の文でこの句を伏せてから A を探す (推敲後も B の側の「道具の使い方」を主張しているだけの文を、A の逆転にしない)。
func bSidePhrases(text ctext, token string, markEnd int) []string {
	rest := text.sub(markEnd, len(text.rs))
	key := pyunicode.Runes(token + "の")
	var out []string
	for start := 0; ; {
		index := runeFind(rest.rs, key, start, len(rest.rs))
		if index < 0 {
			return out
		}
		start = index + 1
		if follow := mustMatch(contrastToken, rest.t, index+len(key)); follow != nil {
			w, _ := follow.Group(0)
			out = append(out, token+"の"+w)
		}
	}
}

// headClue は B の側の「Aの<語>」の<語>の手がかり (本家の _head_clue)。「使い方」「淹れ方」のような「〜方」は初めの 1 字、ほかは語そのもの。
func headClue(head string) string {
	rs := pyunicode.Runes(head)
	if len(rs) >= 2 && rs[len(rs)-1] == '方' {
		return string(rs[:1])
	}
	return head
}

// contrastRisks は否定対比の主張の逆転 (本家の _contrast_risks)。
// 推敲後の文を探す範囲: 原文の否定の文に組があれば、組になった推敲後の文だけ。組がなければ、同じ塊の変わった文すべて。原文と同じ文は見ない。
func (s *sides) contrastRisks(groups []*sentenceGroup) []riskItem {
	var out []riskItem
	order := 0
	var maskers [2]*masker
	for _, group := range groups {
		if group.hunk.tag == "insert" || len(group.old) == 0 || len(group.new) == 0 {
			continue
		}
		if !slices.ContainsFunc(group.old, func(sn sentence) bool { return newCtext(sn.text).has(contrastMark) }) {
			continue
		}
		if maskers[0] == nil {
			maskers = [2]*masker{s.newMaskerWith(0, contrastSkipKinds), s.newMaskerWith(1, contrastSkipKinds)}
		}
		newForOld := map[int][]int{}
		oldForNew := map[int]int{}
		for _, p := range group.pairs {
			newForOld[p[0]] = append(newForOld[p[0]], p[1])
			oldForNew[p[1]] = p[0]
		}
		oldTexts := map[string]bool{}
		for _, sn := range group.old {
			oldTexts[sn.text] = true
		}
		type seenKey struct {
			oldLine, newLine int
			token            string
		}
		seen := map[seenKey]bool{}
		for i, sentence := range group.old {
			bd, ok := maskers[0].body(sentence)
			if !ok {
				continue
			}
			heading := maskers[0].kind(sentence) == "heading"
			bodyText := newCtext(bd.text)
			var tokens []negatedToken
			for _, item := range negatedTokens(bodyText) {
				if !originalAffirms(bodyText, item.token, item.markEnd, item.markEnd-pystr.Len(item.mark)) {
					tokens = append(tokens, item)
				}
			}
			if len(tokens) == 0 {
				continue
			}
			negated := map[string]bool{}
			for _, tk := range tokens {
				negated[tk.token] = true
			}
			var others []string
			for _, tk := range contrastTokens(bodyText) {
				if !negated[tk] && contrastIsToken(tk) {
					others = append(others, tk)
				}
			}
			targets, ok := newForOld[i]
			if !ok {
				targets = make([]int, len(group.new))
				for j := range targets {
					targets[j] = j
				}
			}
			for _, j := range targets {
				newSentence := group.new[j]
				newBody, ok := maskers[1].body(newSentence)
				if !ok || oldTexts[newBody.raw] {
					continue
				}
				for _, tk := range tokens {
					probe := newBody.text
					phrases := bSidePhrases(bodyText, tk.token, tk.markEnd)
					if slices.ContainsFunc(phrases, func(phrase string) bool {
						return !strings.Contains(probe, phrase) && strings.Contains(probe, headClue(runeSlice(pyunicode.Runes(phrase), pystr.Len(tk.token)+1, 1<<30)))
					}) {
						continue // 「道具の使い方」→「道具をどう使うか」: B の句の言い換え
					}
					for _, phrase := range phrases {
						probe = strings.ReplaceAll(probe, phrase, strings.Repeat("＿", pystr.Len(phrase)))
					}
					// 見出しの行と「Aは使わず」「Aはせず」から取った名詞は、A の節そのものの重みの語だけを見る
					narrow := heading || newCtext(tk.mark).hasMatch(contrastZuMark)
					// 原文で肯定した側 (否定を表す語より後ろ) の語。重みの語だけの句のあとで A を B に変えた推敲を見分ける
					var side []string
					for _, word := range contrastTokens(bodyText.sub(tk.markEnd, len(bodyText.rs))) {
						if contrastIsToken(word) && !negated[word] {
							side = append(side, word)
						}
					}
					probeText := newCtext(probe)
					if affirmedWithWeight(probeText, tk.token, narrow, side) < 0 && affirmedPlain(probeText, tk.token, others) < 0 {
						continue
					}
					if paired, ok := oldForNew[j]; ok && paired != i {
						if other, ok := maskers[0].body(group.old[paired]); ok && affirmedWithWeight(newCtext(other.text), tk.token, false, nil) >= 0 {
							continue
						}
					}
					key := seenKey{bd.line, newBody.line, tk.token}
					if seen[key] {
						continue
					}
					seen[key] = true
					d := pyjson.NewObject()
					d.Set("word", shownWord(bodyText, tk.token, tk.markEnd-pystr.Len(tk.mark)))
					d.Set("mark", tk.mark)
					out = append(out, riskItem{groupIndex(group, &newSentence, 1), order, newRisk("contrast_reversal", nil, bd.line, newBody.line, bd.raw, newBody.raw, true, true, d)})
					order++
				}
			}
		}
	}
	return out
}

// ContrastRisks は元の文と書き直した文の、否定対比の主張の逆転の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func ContrastRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.contrastRisks(s.sentencePairs(full.hunks))), nil
}
