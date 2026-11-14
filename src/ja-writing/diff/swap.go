package diff

import (
	"slices"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 O: 主語と目的語の入れ替え (本家の _swap_risks・_case_map・_passive_rewrite)。両方の文にある 2 つの語 A と B で、元で A が p、B が q (p と q は違う)、後で A が q、B が p なら出す。
// 助詞は が、を、に、は、から、へ、より (「と」は入れない)。

var (
	caseWord     = pyre.NewLazy(`[一-龥々ァ-ヴーA-Za-z0-9０-９・]{1,12}`, 0)
	honorific    = pyre.NewLazy(`さん|くん|ちゃん`, 0)
	caseParticle = pyre.NewLazy(`から|より|が|を|に|は|へ`, 0)
	// 受け身の形 (される、された、されて、られる、られた など)
	passiveForm = pyre.NewLazy(`(?:さ|ら)れ(?:る|た|て|ます|ました|ない)`, 0)
)

// caseMap は「語と助詞」の組。語ごとに最初の 1 つ。語は最長の並びで、直後の さん、くん、ちゃん を外す。words は語の最初に出た順。
type caseMap struct {
	words     []string
	particles map[string]string
}

func newCaseMap(text string) *caseMap {
	c := &caseMap{particles: map[string]string{}}
	t := pyre.NewText(text)
	for _, m := range mustFinditer(caseWord, t, 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		end := m.End(0)
		if h, err := honorific.Get().Match(t, end, end+3); err != nil {
			panic(err)
		} else if h != nil {
			end = h.End(0)
		} else if rs := pyunicode.Runes(word); len(rs) > 2 && (rs[len(rs)-1] == '様' || rs[len(rs)-1] == '氏') {
			// 「田中様」「佐藤氏」の敬称を外す。2 字の語 (仕様、同様、彼氏) は外さない
			word = pyunicode.FromRunes(rs[:len(rs)-1])
		}
		p, err := caseParticle.Get().Match(t, end, end+2)
		if err != nil {
			panic(err)
		}
		if _, seen := c.particles[word]; p != nil && !seen {
			g, _ := p.Group(0)
			c.particles[word] = g
			c.words = append(c.words, word)
		}
	}
	return c
}

// passiveRewrite は「に」をとる動詞の受け身への書き換え (この変更がユーザーに影響する → ユーザーがこの変更に影響される) か。片側だけに受け身の形があり、
// 入れ替えた助詞の組がすべて「が」と「に」なら真 (受け身への書き換えは出さない)。
func passiveRewrite(oldText, newText string, before, after *caseMap, words []string) bool {
	if (mustSearchAt(passiveForm, pyre.NewText(oldText), 0, pyre.NoEnd) != nil) == (mustSearchAt(passiveForm, pyre.NewText(newText), 0, pyre.NoEnd) != nil) {
		return false
	}
	for _, w := range words {
		b, a := before.particles[w], after.particles[w]
		if (b != "が" || a != "に") && (b != "に" || a != "が") {
			return false
		}
	}
	return true
}

// swapRisks は主語と目的語の入れ替え (kind swap)。
func (s *sides) swapRisks(groups []*sentenceGroup) []riskItem {
	var out []riskItem
	order := 0
	for _, group := range groups {
		for _, p := range group.pairs {
			old, new := group.old[p[0]], group.new[p[1]]
			if old.text == new.text {
				continue
			}
			before, after := newCaseMap(old.text), newCaseMap(new.text)
			var common, words []string
			for _, w := range before.words {
				if _, ok := after.particles[w]; ok {
					common = append(common, w)
				}
			}
			for x, a := range common {
				for _, b := range common[x+1:] {
					if before.particles[a] != before.particles[b] && after.particles[a] == before.particles[b] && after.particles[b] == before.particles[a] {
						for _, w := range []string{a, b} {
							if !slices.Contains(words, w) {
								words = append(words, w)
							}
						}
					}
				}
			}
			if len(words) > 0 && passiveRewrite(old.text, new.text, before, after, words) {
				words = nil
			}
			if len(words) > 0 {
				slices.SortStableFunc(words, func(a, b string) int { return slices.Index(common, a) - slices.Index(common, b) })
				pairs := make([]pyjson.Value, len(words))
				for i, w := range words {
					o := pyjson.NewObject()
					o.Set("word", w)
					o.Set("old", before.particles[w])
					o.Set("new", after.particles[w])
					pairs[i] = o
				}
				d := pyjson.NewObject()
				d.Set("pairs", pairs)
				out = append(out, riskItem{groupIndex(group, &new, 1), order, newRisk("swap", nil, old.line, new.line, old.text, new.text, true, true, d)})
				order++
			}
		}
	}
	return out
}

// SwapRisks は元の文と書き直した文の、主語と目的語の入れ替えの恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func SwapRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.swapRisks(s.sentencePairs(full.hunks))), nil
}
