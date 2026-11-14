package diff

import (
	"cmp"
	"slices"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 W: 文体の切り替え (本家の _register_changes)。常体と敬体が、組の文で切り替わった数を数える。

// registerOf は本家の _register_of。漢字の「下さい」は仮名の「ください」と同じに読む (「実行して下さい」→「実行してください」を常体→敬体と取り違えない)。
func registerOf(text string) string {
	return Register(strings.ReplaceAll(text, "下さい", "ください"))
}

// contained は short の字のうち long に同じ並びで残る割合 (本家の _contained)。
func contained(short, long string) float64 {
	if short == "" {
		return 0
	}
	size := 0
	for _, b := range difflib.New(pyunicode.Runes(short), pyunicode.Runes(long)).GetMatchingBlocks() {
		size += b.Size
	}
	return float64(size) / float64(pystr.Len(short))
}

// neighborPairs は組の並び order (原文の文の番号の小さい順。firsts はその原文の文の番号の並び) のうち、原文の文が i より前の最後の組と、i より後の最初の組の、
// 推敲後の文の番号 (前の組がなければ 0、後の組がなければ last。本家の _neighbor_pairs)。
func neighborPairs(order [][2]int, firsts []int, i, last int) (int, int) {
	before, after := bisectLeft(firsts, i), bisectRight(firsts, i)
	low, high := 0, last
	if before > 0 {
		low = order[before-1][1]
	}
	if after < len(order) {
		high = order[after][1]
	}
	return low, high
}

// sortedPairs は組の並びを (元の文の番号, 後の文の番号) の小さい順にしたもの (本家の sorted(group["pairs"]))。
func sortedPairs(pairs [][2]int) [][2]int {
	out := slices.Clone(pairs)
	slices.SortFunc(out, func(a, b [2]int) int { return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1])) })
	return out
}

type registerChange struct {
	old, new body
}

// registerChanges は本家の _register_changes。politeness は politeness() の節 (敬体の割合の候補が出ていれば、文体の切り替えは数えない)。
func (s *sides) registerChanges(groups []*sentenceGroup, politeness *pyjson.Object) *pyjson.Object {
	threshold := pyjson.NewObject()
	threshold.Set("min_sentences", int64(3))
	threshold.Set("min_ratio", 0.5)
	candidates, _ := politeness.Get("candidates")
	for _, c := range candidates.([]pyjson.Value) {
		if sub, _ := c.(*pyjson.Object).Get("subkind"); sub == "polite_ratio" {
			section := reviewSection(nil)
			section.Set("threshold", threshold)
			section.Set("skipped_by", "politeness")
			return section
		}
	}
	masks := [2]*masker{s.newMasker(0), s.newMasker(1)}
	eligible := func(side int, b body) bool {
		return pystr.Strip(b.text) != "" && !s.isQuoteRow(side, b.line) && !s.isDialogueLine(side, b.line)
	}
	var counts [2]map[string]int
	for side := range 2 {
		counts[side] = map[string]int{}
		for _, b := range s.allBodiesWith(side, masks[side]) {
			if eligible(side, b) {
				counts[side][registerOf(b.text)]++
			}
		}
	}
	kinds := map[[2]string]string{{"常体", "敬体"}: "to_polite", {"敬体", "常体"}: "to_plain"}
	changes := map[string][]registerChange{}
	for _, group := range groups {
		olds := make([]*body, len(group.old))
		for i, sn := range group.old {
			if b, ok := masks[0].body(sn); ok {
				olds[i] = &b
			}
		}
		news := make([]*body, len(group.new))
		newRegisters := make([]string, len(group.new))
		for j, sn := range group.new {
			if b, ok := masks[1].body(sn); ok {
				news[j] = &b
				if eligible(1, b) {
					newRegisters[j] = registerOf(b.text)
				}
			}
		}
		order := sortedPairs(group.pairs)
		firsts := make([]int, len(order))
		pairedOld := map[int]bool{}
		for k, p := range order {
			firsts[k] = p[0]
			pairedOld[p[0]] = true
		}
		type foundChange struct {
			index int
			kind  string
			registerChange
		}
		var found []foundChange
		for _, p := range order {
			i, j := p[0], p[1]
			old, new := olds[i], news[j]
			if old == nil || new == nil || !eligible(0, *old) || newRegisters[j] == "" {
				continue
			}
			if kind := kinds[[2]string{registerOf(old.text), newRegisters[j]}]; kind != "" {
				found = append(found, foundChange{i, kind, registerChange{*old, *new}})
			}
		}
		// 文を合わせた推敲 (「設定を開く。保存を押す。」→「設定を開き、保存を押します。」): 組にならなかった原文の文も、
		// 前後の組の推敲後の文のあいだ (最大 6 文) に中身の 6 割以上が残り、その文の文体が切り替わっていれば数える
		for i, old := range olds {
			if old == nil || pairedOld[i] || !eligible(0, *old) || pystr.Len(old.text) < 4 {
				continue
			}
			source := registerOf(old.text)
			if source == "" {
				continue
			}
			low, high := neighborPairs(order, firsts, i, len(news)-1)
			if high-low > 5 {
				middle := min(len(news)-1, i*len(news)/max(1, len(olds)))
				low, high = max(low, middle-2), min(high, middle+3)
			}
			for k := max(0, low); k < min(len(news), high+1); k++ {
				kind := kinds[[2]string{source, newRegisters[k]}]
				if news[k] != nil && kind != "" && contained(old.text, news[k].text) >= 0.6 {
					found = append(found, foundChange{i, kind, registerChange{*old, *news[k]}})
					break
				}
			}
		}
		slices.SortStableFunc(found, func(a, b foundChange) int { return a.index - b.index })
		for _, f := range found {
			changes[f.kind] = append(changes[f.kind], f.registerChange)
		}
	}
	var out []*pyjson.Object
	for _, c := range []struct{ kind, sourceName, targetName string }{{"to_polite", "常体", "敬体"}, {"to_plain", "敬体", "常体"}} {
		sourceName, targetName := c.sourceName, c.targetName
		pairs := changes[c.kind]
		if len(pairs) < 3 || float64(len(pairs)) < 0.5*float64(max(1, counts[0][sourceName])) {
			continue
		}
		pattern := "switched"
		if counts[0][targetName] > counts[0][sourceName] {
			pattern = "unified"
		}
		var lines []pyjson.Value
		for _, p := range pairs[:min(5, len(pairs))] {
			lines = append(lines, []pyjson.Value{int64(p.old.line), int64(p.new.line)})
		}
		detail := pyjson.NewObject()
		detail.Set("pattern", pattern)
		detail.Set("switched", int64(len(pairs)))
		detail.Set("old_counts", registerCounts(counts[0]))
		detail.Set("new_counts", registerCounts(counts[1]))
		detail.Set("lines", lines)
		out = append(out, reviewCandidate(c.kind, &pairs[0].old, &pairs[0].new, detail))
	}
	section := reviewSection(out)
	section.Set("threshold", threshold)
	section.Set("skipped_by", nil)
	return section
}

func registerCounts(found map[string]int) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("plain", int64(found["常体"]))
	o.Set("polite", int64(found["敬体"]))
	return o
}

// RegisterChanges は元の文と書き直した文の、文体の切り替えの節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。敬体の割合の候補が出ると切り替えは数えない)。
func RegisterChanges(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.registerChanges(s.sentencePairs(full.hunks), s.politeness(docType)), nil
}
