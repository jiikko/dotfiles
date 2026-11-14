package diff

import (
	"slices"
	"strings"
	"sync"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
)

// 群 T: 気持ちの創作 (本家の _feeling_risks。kind feeling)。元の文書のどこにもない気持ちの語 (ほっとした、安堵した、うれしかった…) が、推敲後の変わった文に足されたら候補にする。
// 一覧は埋め込みの feelings.txt (語の家族ごとに「名前<TAB>正規表現」。3 つ目の欄は原文の言い方で、原文にあればその家族の気持ちは原文にあるとみなす)。--fail-on=meaning-risk には数えない。

type feelingItem struct{ regex, third *pyre.Lazy }

type feelingPatterns struct {
	items    []feelingItem
	combined *pyre.Lazy
}

var (
	feelingOnce  sync.Once
	feelingCache *feelingPatterns
)

func feelings() *feelingPatterns {
	feelingOnce.Do(func() {
		p := &feelingPatterns{}
		var regexes []*pyre.Lazy
		for _, item := range LoadList("feelings").Items {
			fi := feelingItem{regex: pyre.NewLazy(item.Regex, 0)}
			if item.Third != "" {
				fi.third = pyre.NewLazy(item.Third, 0)
			}
			p.items = append(p.items, fi)
			regexes = append(regexes, fi.regex)
		}
		p.combined = anyOf(regexes)
		feelingCache = p
	})
	return feelingCache
}

// 原文の気持ちの語を探すときに数えない行の種類と、推敲後の変わった文で数えない行の種類 (推敲後は表も見ない)
var (
	feelingOldSkip = map[string]bool{"code": true, "frontmatter": true, "html_block": true}
	feelingNewSkip = map[string]bool{"code": true, "frontmatter": true, "html_block": true, "table": true}
)

func (s *sides) feelingRisks(groups []*sentenceGroup) []riskItem {
	patterns := feelings()
	if len(patterns.items) == 0 || !slices.ContainsFunc(groups, func(g *sentenceGroup) bool { return len(g.new) > 0 }) {
		return nil
	}
	// 原文にない家族だけを見る。3 つ目の欄 (原文の言い方) が原文に当たる家族も、原文にある気持ちとみなす。
	// 速くするため (結果は同じ)、原文を家族ごとに調べるのは、推敲後の変わった文に当たった家族だけにする。
	var original string
	haveOriginal := false
	memo := map[int]bool{}
	absent := func(index int) bool {
		if v, ok := memo[index]; ok {
			return v
		}
		if !haveOriginal {
			oldMasker := s.newMaskerWith(0, feelingOldSkip)
			var lines []string
			for number, line := range s.lines[0] {
				if number < len(oldMasker.rows) && feelingOldSkip[oldMasker.rows[number].Kind] {
					continue
				}
				lines = append(lines, oldMasker.mask(line, s.starts[0][number]))
			}
			original, haveOriginal = strings.Join(lines, "\n"), true
		}
		item := patterns.items[index]
		v := !item.regex.MustSearch(original) && (item.third == nil || !item.third.MustSearch(original))
		memo[index] = v
		return v
	}
	masker := s.newMaskerWith(1, feelingNewSkip)
	var out []riskItem
	order := 0
	for _, group := range groups {
		if len(group.new) == 0 {
			continue
		}
		oldTexts := map[string]bool{}
		for _, sn := range group.old {
			oldTexts[sn.text] = true
		}
		oldForNew := map[int]int{}
		for _, p := range group.pairs {
			oldForNew[p[1]] = p[0]
		}
		for j, current := range group.new {
			if oldTexts[current.text] {
				continue
			}
			body, ok := masker.body(current)
			if !ok {
				continue
			}
			if patterns.combined != nil && !patterns.combined.MustSearch(body.text) {
				continue
			}
			var words []string
			text := pyre.NewText(body.text)
			for index, item := range patterns.items {
				if m := mustSearchAt(item.regex, text, 0, pyre.NoEnd); m != nil {
					if hit, _ := m.Group(0); !slices.Contains(words, hit) && absent(index) {
						words = append(words, hit)
					}
				}
			}
			if len(words) == 0 {
				continue
			}
			var old sentence
			paired, hasOld := oldForNew[j]
			if hasOld {
				old = group.old[paired]
			}
			d := pyjson.NewObject()
			d.Set("words", pyjson.Strings(words))
			out = append(out, riskItem{groupIndex(group, &current, 1), order, newRisk("feeling", nil, old.line, current.line, old.text, current.text, hasOld, true, d)})
			order++
		}
	}
	return out
}

// FeelingRisks は元の文と書き直した文の、気持ちの語を足した恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func FeelingRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.feelingRisks(s.sentencePairs(full.hunks))), nil
}
