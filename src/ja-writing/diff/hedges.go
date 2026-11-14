package diff

import (
	"ja-writing/internal/difflib"
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 X: 慎重表現・動かしてはいけない値・敬語 (本家の _hedges・_immutable_values・_politeness)。
// 既存の 9 キーと推量印の判定は変えず、慎重表現は独立した節にして、既存の推量印の候補と文単位で重ねない。

// reviewCandidate は本家の _review_candidate。old・new は無ければ nil。
func reviewCandidate(subkind string, old, new *body, detail pyjson.Value) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("subkind", subkind)
	o.Set("level", "review")
	var oldLine, newLine pyjson.Value
	var oldRaw, newRaw pyjson.Value
	if old != nil {
		oldLine, oldRaw = int64(old.line), old.raw
	}
	if new != nil {
		newLine, newRaw = int64(new.line), new.raw
	}
	o.Set("old_line", oldLine)
	o.Set("new_line", newLine)
	o.Set("old", oldRaw)
	o.Set("new", newRaw)
	o.Set("detail", detail)
	return o
}

// reviewSection は本家の _review_section。fields は呼び出し側が続けて Set する (本家の result.update(fields))。
func reviewSection(candidates []*pyjson.Object) *pyjson.Object {
	list := make([]pyjson.Value, len(candidates))
	for i, c := range candidates {
		list[i] = c
	}
	o := pyjson.NewObject()
	o.Set("level", "review")
	o.Set("candidate_count", int64(len(candidates)))
	o.Set("candidates", list)
	return o
}

// hedgeRegex は慎重表現の一覧の語を長い順に選ぶ正規表現 (語が無ければ nil)。同じ長さの語は同じ位置で同時に当たらないので、並びは結果を変えない。
func hedgeRegex() *pyre.Lazy {
	words := slices.Clone(LoadList("hedges").Words)
	if len(words) == 0 {
		return nil
	}
	words = slices.Compact(sortedUnique(words))
	sort.SliceStable(words, func(i, j int) bool { return len(pyunicode.Runes(words[i])) > len(pyunicode.Runes(words[j])) })
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = pyre.Escape(w)
	}
	return pyre.NewLazy(strings.Join(parts, "|"), 0)
}

func sortedUnique(words []string) []string {
	out := slices.Clone(words)
	slices.Sort(out)
	return slices.Compact(out)
}

// countedMinus は本家の (Counter(before) - Counter(after)).elements(): before に出る順に、増えた分だけ並べる。
func countedMinus(before, after []string) []string {
	have := map[string]int{}
	for _, w := range after {
		have[w]++
	}
	seen := map[string]int{}
	var order []string
	for _, w := range before {
		if seen[w] == 0 {
			order = append(order, w)
		}
		seen[w]++
	}
	var out []string
	for _, w := range order {
		for range max(0, seen[w]-have[w]) {
			out = append(out, w)
		}
	}
	return out
}

// hedgeCovering は慎重表現の候補を出さない原文の文を決める、既存の候補 (本家の _hedges の marks)。
// marks は文の種類の印の恐れ (出力から外した分も含む。本家の risks + sides.quiet_marks) のうち、推量が減ったもの。
// certainty は確度の恐れ (kind certainty と、強まった certainty_review)。
func hedgeCovering(marks []marksReview, certainty []*pyjson.Object) []marksReview {
	var cover []marksReview
	for _, m := range marks {
		if slices.Contains(m.lost, "推量") {
			cover = append(cover, m)
		}
	}
	for _, r := range certainty {
		kind, _ := r.Get("kind")
		subkind, _ := r.Get("subkind")
		if kind != "certainty" && (kind != "certainty_review" || subkind != "strengthened") {
			continue
		}
		m := marksReview{}
		if line, ok := r.Get("old_line"); ok && line != nil {
			m.oldLine = int(line.(int64))
		}
		if old, ok := r.Get("old"); ok && old != nil {
			m.old = old.(string)
		}
		cover = append(cover, m)
	}
	return cover
}

// hedges は一覧の語の出現総数が減った文 (本家の _hedges)。文の分割先も数え、移動しただけの文は除く。
func (s *sides) hedges(groups []*sentenceGroup, marks []marksReview, certainty []*pyjson.Object) *pyjson.Object {
	regex := hedgeRegex()
	cover := hedgeCovering(marks, certainty)
	masks := [2]*masker{s.newMasker(0), s.newMasker(1)}
	var newDoc string
	haveNewDoc := false
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
		var newBodies []sentence
		for i, sn := range group.old {
			old, ok := masks[0].body(sn)
			if !ok || regex == nil {
				continue
			}
			before := findall(regex, old.text)
			if len(before) == 0 {
				continue
			}
			j, paired := pairs[i]
			var new *body
			if paired {
				if b, ok := masks[1].body(group.new[j]); ok {
					new = &b
				}
			}
			afterText := ""
			if new != nil {
				afterText = new.text
			}
			if paired {
				// 分割された後半に慎重表現が残った場合を削除と取り違えない
				if newBodies == nil {
					newBodies = make([]sentence, len(group.new))
					for k, ns := range group.new {
						b, ok := masks[1].body(ns)
						if !ok {
							b = body{"", ns.text, ns.line, ns.start}
						}
						newBodies[k] = sentence{text: b.text, line: b.line, start: b.start}
					}
				}
				afterText = withUnpairedText(newBodies, j, pairedNew)
			} else {
				if !haveNewDoc {
					newDoc, haveNewDoc = pyunicode.FromRunes(s.texts[1]), true
				}
				if strings.Contains(newDoc, old.raw) {
					continue
				}
			}
			after := findall(regex, afterText)
			if len(before) <= len(after) {
				continue
			}
			if slices.ContainsFunc(cover, func(r marksReview) bool { return sameReviewSentence(old, r) }) {
				continue
			}
			detail := pyjson.NewObject()
			detail.Set("old_count", int64(len(before)))
			detail.Set("new_count", int64(len(after)))
			detail.Set("removed", pyjson.Strings(countedMinus(before, after)))
			out = append(out, reviewCandidate("decreased", &old, new, detail))
		}
	}
	section := reviewSection(out)
	section.Set("list", ListSource("hedges"))
	section.Set("list_problem", nil)
	return section
}

// HedgesSection は元の文と書き直した文の、慎重な言い方の節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。確度の恐れを数える種類が変わる)。
// 既存の候補は、文の種類の印の恐れ (出力から外した分も含む) と確度の恐れだけを読む。
func HedgesSection(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	o, r := Normalize(orig), Normalize(rw)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	groups := s.sentencePairs(full.hunks)
	marks := s.marksReviewsOf(TypeChanges(o, r, opcodes, &typeCache{}))
	return s.hedges(groups, marks, s.certaintyRisks(groups, marks, nil, docType)), nil
}
