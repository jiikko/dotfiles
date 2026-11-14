package diff

import (
	"cmp"
	"slices"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 W: かぎかっこの中の言葉の書き換え (本家の _quote_risks。kind quote)。原文の変わった文のかぎかっこを、推敲後の対応する文とすぐ隣の組にならなかった文
// (組がなければ同じ塊の近い位置の文) だけと照らす。文書全体のどこかに同じ言葉があるだけでは残ったとしない (「はい」→「いいえ」、「必要」→「不必要」)。
// かっこを外しただけ (照らす文のかっこの外に同じ言葉がある) と、同じかぎかっこを別の場所に動かしただけ (文書全体の同じ形の数が減っていない) は出さない。

var (
	// 会話の行 (行頭に 8 字以内の話し手の名前があってもよい)。「」と『』、閉じのあとの句点も見る。
	dialogueRe = pyre.NewLazy(`^[^「」『』、。\s]{0,8}[「『].*[」』][。．]?$`, 0)
	// 書き方だけの違いとして外す字。長音の「ー」は語の一部なので外さない (「ビール」と「ビル」)。
	quoteDrop = pyre.NewLazy(`[\s、。，．,.!！…‥・〜~－-]|\.\.\.`, 0)
	quoteOpen = map[rune]rune{'「': '」', '『': '』'}
)

func quoteNormalize(text string) string {
	return quoteDrop.MustSub("", strings.ReplaceAll(pyunicode.Normalize(pyunicode.NFKC, text), "?", "？"))
}

// quoteFound は 1 行のかぎかっこ 1 つ (開きの位置・かっこ・中身)。
type quoteFound struct {
	start       int
	bracket, in string
}

// lineQuotes は 1 行のかぎかっこ (「」『』) を、かっこの対応を追って取る (本家の _line_quotes)。入れ子は外側 (中身に内側のかっこを含む) と内側の両方。
// 文の句点をまたぐ会話 (「うん。うん。そう。」) も 1 つとして取る。閉じないかっこは見ない。いちばん外側のかっこの範囲の並びも返す。
func lineQuotes(text string) (found []quoteFound, tops [][2]int) {
	type open struct {
		position int
		char     rune
	}
	rs := pyunicode.Runes(text)
	var stack []open
	for position, char := range rs {
		if _, ok := quoteOpen[char]; ok {
			stack = append(stack, open{position, char})
		} else if char == '」' || char == '』' {
			depth := len(stack) - 1
			for depth >= 0 && quoteOpen[stack[depth].char] != char {
				depth--
			}
			if depth < 0 {
				continue
			}
			start, opener := stack[depth].position, stack[depth].char
			stack = stack[:depth]
			found = append(found, quoteFound{start, string([]rune{opener, char}), pyunicode.FromRunes(rs[start+1 : position])})
			if len(stack) == 0 {
				tops = append(tops, [2]int{start, position + 1})
			}
		}
	}
	slices.SortStableFunc(found, func(a, b quoteFound) int { return cmp.Compare(a.start, b.start) })
	return found, tops
}

// blankMasked は伏せる範囲 (インラインのコード、URL など) を同じ長さの \x00 にした文字列 (位置を保つ。本家の _blank_masked)。
func (m *masker) blankMasked(text string, start int) string {
	rs := pyunicode.Runes(text)
	end := start + len(rs)
	first := max(0, bisectRight(m.lefts, start)-1)
	changed := false
	for _, span := range m.spans[first:] {
		left, right := span[0], span[1]
		if left >= end {
			break
		}
		if right <= start {
			continue
		}
		changed = true
		for k := max(left, start); k < min(right, end); k++ {
			rs[k-start] = 0
		}
	}
	if !changed {
		return text
	}
	return pyunicode.FromRunes(rs)
}

// quoteEntry は 1 つのかぎかっこ (比べ用の形・かっこ・中身・文)。
type quoteEntry struct {
	key, bracket, content string
	sentence              sentence
}

// quoteSide は 1 つの側のかぎかっこ (本家の _QuoteSide)。quotes は文の位置から、outside は文の位置から、かぎかっこの外の文字を比べ用の形にしたもの、
// count は文書全体の比べ用の形の数。
type quoteSide struct {
	quotes  map[int][]quoteEntry
	outside map[int]string
	count   map[string]int
}

func (s *sides) newQuoteSide(side int, m *masker) *quoteSide {
	q := &quoteSide{quotes: map[int][]quoteEntry{}, outside: map[int]string{}, count: map[string]int{}}
	rows := s.analyses[side].Lines
	var lineOrder []int
	byLine := map[int][]sentence{}
	for _, sn := range s.lineSentences(side, 0, len(s.lines[side])) {
		if _, ok := byLine[sn.line]; !ok {
			lineOrder = append(lineOrder, sn.line)
		}
		byLine[sn.line] = append(byLine[sn.line], sn)
	}
	for _, lineNo := range lineOrder {
		sentences := byLine[lineNo]
		if lineNo-1 >= len(rows) || m.skip[rows[lineNo-1].Kind] || s.isQuoteRow(side, lineNo) {
			continue
		}
		lineStart := s.starts[side][lineNo-1]
		text := m.blankMasked(s.lines[side][lineNo-1], lineStart)
		found, tops := lineQuotes(text)
		starts := make([]int, len(sentences))
		for i, sn := range sentences {
			starts[i] = sn.start - lineStart
		}
		for _, f := range found {
			sn := sentences[max(0, bisectRight(starts, f.start)-1)]
			key := quoteNormalize(f.in)
			q.quotes[sn.start] = append(q.quotes[sn.start], quoteEntry{key, f.bracket, f.in, sn})
			q.count[key]++
		}
		outside := pyunicode.Runes(text)
		for _, t := range tops {
			for k := t[0]; k < t[1]; k++ {
				outside[k] = 1
			}
		}
		for i, sn := range sentences {
			q.outside[sn.start] = quoteNormalize(runeSlice(outside, starts[i], starts[i]+pystr.Len(sn.text)))
		}
	}
	return q
}

// sentenceSlice は list[low:high] (Python の切り出し。範囲の外は詰める)。
func sentenceSlice(list []sentence, low, high int) []sentence {
	low, high = min(max(low, 0), len(list)), min(max(high, 0), len(list))
	if low >= high {
		return nil
	}
	return list[low:high]
}

func (s *sides) isDialogueLine(side, line int) bool {
	return dialogueRe.MustFullMatch(pystr.Strip(s.lines[side][line-1]))
}

// quoteRisks は本家の _quote_risks。masks が nil なら本文の数えない行の種類で作る。
func (s *sides) quoteRisks(groups []*sentenceGroup, masks *[2]*masker) []*pyjson.Object {
	if masks == nil {
		masks = &[2]*masker{s.newMasker(0), s.newMasker(1)}
	}
	oldSide, newSide := s.newQuoteSide(0, masks[0]), s.newQuoteSide(1, masks[1])
	var out []*pyjson.Object
	for _, group := range groups {
		pairs := map[int]int{}
		pairedNew := map[int]bool{}
		for _, p := range group.pairs {
			pairs[p[0]] = p[1]
		}
		for _, j := range pairs {
			pairedNew[j] = true
		}
		order := sortedPairs(group.pairs)
		oldCount, newCount := len(group.old), len(group.new)
		for i, sn := range group.old {
			quotes := oldSide.quotes[sn.start]
			if len(quotes) == 0 {
				continue
			}
			j, paired := pairs[i]
			var region []sentence
			switch {
			case paired:
				region = append(region, group.new[j])
				for _, k := range unpairedNeighbors(newCount, j, pairedNew) {
					region = append(region, group.new[k])
				}
			case newCount > 0:
				// 組にならなかった文は、前後の組の推敲後の文のあいだ (最大 6 文) と照らす
				low, high := 0, newCount-1
				for _, p := range order {
					if p[0] < i {
						low = p[1]
					}
				}
				for _, p := range order {
					if p[0] > i {
						high = p[1]
						break
					}
				}
				if high-low > 5 {
					middle := min(newCount-1, i*newCount/max(1, oldCount))
					low, high = max(low, middle-2), min(high, middle+3)
				}
				region = sentenceSlice(group.new, low, high+1)
			}
			var regionQuotes []quoteEntry
			regionKeys := map[string]bool{}
			var outsides []string
			for _, r := range region {
				for _, q := range newSide.quotes[r.start] {
					regionQuotes = append(regionQuotes, q)
					regionKeys[q.key] = true
				}
				outsides = append(outsides, newSide.outside[r.start])
			}
			outside := strings.Join(outsides, "\x01")
			var partnerQuotes []quoteEntry
			if paired {
				partnerQuotes = newSide.quotes[group.new[j].start]
			}
			for index, q := range quotes {
				if pystr.Len(q.key) < 2 || regionKeys[q.key] || strings.Contains(outside, q.key) {
					continue
				}
				if newSide.count[q.key] >= oldSide.count[q.key] {
					continue
				}
				var best *quoteEntry
				ratio := 0.0
				for k := range regionQuotes {
					similarity := difflib.New(pyunicode.Runes(q.key), pyunicode.Runes(regionQuotes[k].key)).Ratio()
					if similarity > ratio {
						best, ratio = &regionQuotes[k], similarity
					}
				}
				if ratio < 0.4 && len(partnerQuotes) == len(quotes) {
					// 同じ数のかぎかっこを持つ組の文では、同じ順の言葉を書き換えた先とみなす (「行く」→「行かない」)
					best, ratio = &partnerQuotes[index], 1.0
				}
				changed := best != nil && ratio >= 0.4
				if !changed && newSide.count[q.key] > 0 {
					// 同じ言葉のかぎかっこが推敲後の文書のほかの所に残っていて、この文からは消えただけ (くり返しの名前を 1 か所省いた) なら出さない。
					// 言葉を別の言葉に替えた形は上の changed で出る
					continue
				}
				var target *sentence
				switch {
				case changed:
					target = &best.sentence
				case paired:
					target = &group.new[j]
				}
				subkind := "lost"
				var newQuote pyjson.Value
				if changed {
					subkind, newQuote = "changed", best.content
				}
				detail := pyjson.NewObject()
				detail.Set("old_quote", q.content)
				detail.Set("new_quote", newQuote)
				detail.Set("bracket", q.bracket)
				detail.Set("dialogue", s.isDialogueLine(0, sn.line))
				newLine, newText := 0, ""
				if target != nil {
					newLine, newText = target.line, target.text
				}
				out = append(out, newRisk("quote", subkind, sn.line, newLine, sn.text, newText, true, target != nil, detail))
			}
		}
	}
	return out
}

// QuoteRisks は元の文と書き直した文の、かぎかっこの中の言葉を書き換えた恐れ (risk の並び。JSON の値の形)。
func QuoteRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskObjectsValue(s.quoteRisks(s.sentencePairs(full.hunks), nil)), nil
}
