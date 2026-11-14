package diff

import (
	"math/big"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
)

// 群 AC: 読点が増えた候補 (本家の _comma_increase。JSON の comma_increase)。最終稿の地の文の読点が原文より 3 個以上多く、1 文あたり 0.3 個以上増えたときに出す。
// 1 文あたりの増え方は分数で比べる (0.3 個ちょうどの増え方も出す)。

const (
	commaMinAdded      = 3
	commaSentenceAdded = 2
	commaShown         = 5
	commaJoinShare     = 0.6 // 元の文の 2 文字の組のうち、書き直した文に入っている割合がこれ以上なら、つないだ元の文とみなす
	commaJoinMax       = 4   // 1 つの書き直した文につないだとみなす元の文の数の上限
	commaMinRateNum    = 3
	commaMinRateDenom  = 10
)

var commaMarks = pyre.NewLazy(`[、，]`, 0)

// commaCounter は 1 つの側の、文ごとの地の文の読点の数。カギ括弧と二重カギ括弧の中は、行ごとに範囲を取って外す (中に「。」があって文が分かれた引用も外すため)。
// インラインのコードや URL のような伏せる範囲は、位置で外す。
type commaCounter struct {
	s      *sides
	side   int
	masker *masker
	quotes map[int][][2]int
}

func (s *sides) newCommaCounter(side int) *commaCounter {
	return &commaCounter{s: s, side: side, masker: s.newMasker(side), quotes: map[int][][2]int{}}
}

func (c *commaCounter) quoteSpans(index int) [][2]int {
	spans, ok := c.quotes[index]
	if !ok {
		row := c.masker.rows[index]
		_, tops := lineQuotes(row.Raw)
		for _, t := range tops {
			spans = append(spans, [2]int{row.Start + t[0], row.Start + t[1]})
		}
		c.quotes[index] = spans
	}
	return spans
}

// count は文の読点の数。数えない行 (コード・表・見出し・引用など) の文と、伏せると何も残らない文は ok が偽。
func (c *commaCounter) count(sn sentence) (n int, ok bool) {
	index := sn.line - 1
	if index >= len(c.masker.rows) {
		return 0, false
	}
	row := c.masker.rows[index]
	if row.QuoteDepth != 0 || row.Kind == "quote" || c.masker.skip[row.Kind] {
		return 0, false
	}
	text := c.masker.blankMasked(sn.text, sn.start)
	if pystr.Strip(strings.ReplaceAll(text, "\x00", "")) == "" {
		return 0, false
	}
	quotes := c.quoteSpans(index)
	for _, m := range mustFinditer(commaMarks, pyre.NewText(text), 0, pyre.NoEnd) {
		at := sn.start + m.Start(0)
		inside := false
		for _, q := range quotes {
			if q[0] <= at && at < q[1] {
				inside = true
				break
			}
		}
		if !inside {
			n++
		}
	}
	return n, true
}

// commaOld は書き直した文 index と比べる元の文の並び。actorOldIndex の文と、その前後で組にならなかった元の文のうち、中身が書き直した文に入っている文
// (2 文をつないだ推敲)。元の文がなければ空の並び。
func commaOld(group *sentenceGroup, index int, pairs map[int]int, paired []int, counter *commaCounter) []sentence {
	at := actorOldIndex(group, index, pairs, paired)
	if at < 0 {
		return nil
	}
	olds := group.old
	used := map[int]bool{}
	for _, i := range pairs {
		used[i] = true
	}
	grams := headingBigrams(group.new[index].text)
	chosen := []int{at}
	for _, step := range []int{-1, 1} {
		n := at + step
		for n >= 0 && n < len(olds) && !used[n] && len(chosen) < commaJoinMax {
			piece := headingBigrams(olds[n].text)
			shared := 0
			for k := range piece {
				if _, ok := grams[k]; ok {
					shared++
				}
			}
			if _, counted := counter.count(olds[n]); float64(shared) < commaJoinShare*float64(len(piece)) || !counted {
				break
			}
			chosen = append(chosen, n)
			n += step
		}
	}
	sort.Ints(chosen)
	out := make([]sentence, len(chosen))
	for i, n := range chosen {
		out[i] = olds[n]
	}
	return out
}

// commaIncrease は読点が増えた候補 (JSON の comma_increase)。当たらなければ nil。
func (s *sides) commaIncrease(full *fullDiff, groups []*sentenceGroup) pyjson.Value {
	if len(full.hunks) == 0 {
		return nil
	}
	counters := [2]*commaCounter{s.newCommaCounter(0), s.newCommaCounter(1)}
	var totals [2][2]int
	for side := range 2 {
		for _, sn := range s.lineSentences(side, 0, len(s.lines[side])) {
			if n, ok := counters[side].count(sn); ok {
				totals[side][0] += n
				totals[side][1]++
			}
		}
	}
	oldCommas, oldSentences, newCommas, newSentences := totals[0][0], totals[0][1], totals[1][0], totals[1][1]
	rate := func(commas, sentences int) *big.Rat {
		if sentences == 0 {
			return new(big.Rat)
		}
		return big.NewRat(int64(commas), int64(sentences))
	}
	grown := new(big.Rat).Sub(rate(newCommas, newSentences), rate(oldCommas, oldSentences))
	if newCommas-oldCommas < commaMinAdded || grown.Cmp(big.NewRat(commaMinRateNum, commaMinRateDenom)) < 0 {
		return nil
	}
	listed := []pyjson.Value{}
	for _, group := range groups {
		pairs := map[int]int{}
		for _, p := range group.pairs {
			pairs[p[1]] = p[0]
		}
		paired := make([]int, 0, len(pairs))
		for j := range pairs {
			paired = append(paired, j)
		}
		sort.Ints(paired)
		for index, sn := range group.new {
			if len(listed) >= commaShown {
				break
			}
			newCount, ok := counters[1].count(sn)
			if !ok || newCount < commaSentenceAdded {
				continue
			}
			olds := commaOld(group, index, pairs, paired, counters[0])
			if len(olds) == 0 {
				continue
			}
			first, counted := counters[0].count(olds[0])
			if !counted {
				continue
			}
			oldCount := first
			var joined strings.Builder
			joined.WriteString(olds[0].text)
			for _, old := range olds[1:] {
				n, _ := counters[0].count(old)
				oldCount += n
				joined.WriteString(old.text)
			}
			if newCount-oldCount < commaSentenceAdded {
				continue
			}
			item := pyjson.NewObject()
			item.Set("old_line", int64(olds[0].line))
			item.Set("new_line", int64(sn.line))
			item.Set("old", joined.String())
			item.Set("new", sn.text)
			item.Set("old_count", int64(oldCount))
			item.Set("new_count", int64(newCount))
			listed = append(listed, item)
		}
	}
	out := pyjson.NewObject()
	out.Set("old_count", int64(oldCommas))
	out.Set("new_count", int64(newCommas))
	out.Set("old_sentences", int64(oldSentences))
	out.Set("new_sentences", int64(newSentences))
	out.Set("sentences", listed)
	return out
}
