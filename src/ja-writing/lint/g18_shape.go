package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G18 文の形 (say_hedge・copy_comma_noun・bare_verb_chain。本家 yomiyasu_lint.py の 5008-5159)。表は g18_tables.go。
//
// 文書の元の文字に手掛かり (読点の形か「えるでしょう」) を当て、当たりの始まりの行だけを見える文字で調べる (速さのための絞り込みだが、
// 元の文字に手掛かりが無い行は調べない形をそのまま写す)。本家の findings の引数 (既存の候補) は使わない。

// shapeVisibleText は本家の _shape_visible_text。形の規則で見る行の文字 (見ない行は空)。引用の中身は空白で伏せる。
func (s *lintState) shapeVisibleText(row *visibility.Line) string {
	if (row.Kind != "paragraph" && row.Kind != "list" && row.Kind != "heading") || row.QuoteDepth != 0 {
		return ""
	}
	text := strings.ReplaceAll(strings.ReplaceAll(s.doc.proseVisibleText(row.Line), "__", ""), "*", "")
	if row.Kind == "heading" {
		text = subText(shapeHeadingMark, "", text)
	}
	hasMark := false
	for _, mark := range shapeQuoteMarks {
		hasMark = hasMark || strings.Contains(text, mark)
	}
	if !hasMark {
		return text
	}
	text = maskQuotedSegments(text)
	if strings.Contains(text, "“") || strings.Contains(text, `"`) {
		text = blankMatches(shapeCurlyQuoted, text)
	}
	return text
}

// sayHedgeSentence は本家の _say_hedge_sentence。
func sayHedgeSentence(sentence string) bool {
	match := searchText(sayHedge, sentence)
	if match == nil {
		return false
	}
	head, _ := match.Group(1)
	if mustSearch(sayHedgeJudge, sentence) {
		return false
	}
	if head != "" {
		runes := pyunicode.Runes(head)
		if mustMatchAt0(sayHedgeNounEnd, string(runes[len(runes)-1])) && !mustSearch(sayHedgeNaAdjective, head) {
			return false
		}
	}
	return true
}

// copyCommaNounSentence は本家の _copy_comma_noun_sentence。
func copyCommaNounSentence(sentence string) bool {
	if pystr.Len(sentence) > 20 || !strings.HasSuffix(sentence, "。") {
		return false
	}
	match := searchText(copyCommaNoun, sentence)
	if match == nil {
		return false
	}
	group1, _ := match.Group(1)
	modifier := pystr.Strip(group1)
	noun, _ := match.Group(2)
	noun = strings.TrimSuffix(noun, "を")
	if noun == "" || pystr.Len(noun) > 6 || pystr.Len(modifier) < 2 || slices.Contains(copyNotModifier, modifier) || mustSearch(copyModifierParticle, modifier) {
		return false
	}
	if strings.HasSuffix(noun, "い") && !slices.Contains(copyINouns, noun) {
		return false
	}
	return !mustSearch(copyNounNot, noun)
}

// fullMatch は p.fullmatch(text) が当たるか。
func fullMatch(p *lazyPattern, text string) bool {
	m, err := p.get().FullMatch(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// bareVerbChainSentence は本家の _bare_verb_chain_sentence。
func bareVerbChainSentence(sentence string) bool {
	if pystr.Len(sentence) > 30 || !strings.HasSuffix(sentence, "。") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(sentence, "。"), "、")
	for i := range parts {
		parts[i] = pystr.Strip(parts[i])
	}
	if len(parts) < 3 || mustSearch(bareVerbParticle, sentence) {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if !fullMatch(bareVerbPart, part) || mustSearch(bareVerbPolite, part) || mustSearch(bareVerbNotVerb, part) {
			return false
		}
	}
	last := parts[len(parts)-1]
	return fullMatch(bareVerbLast, last) && !mustSearch(bareVerbPolite, last) && !mustSearch(bareVerbNotVerb, last)
}

// shapeGatePositions は文書の元の文字で、3 規則のどれかに当たりうる所の始まり (コードポイントの位置)。
func (s *lintState) shapeGatePositions() []int {
	matches, err := shapeCommaGate.get().Finditer(pyre.NewText(s.an.Text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	positions := make([]int, 0, len(matches))
	for _, m := range matches {
		positions = append(positions, m.Start(0))
	}
	text := pyunicode.Runes(s.an.Text)
	for _, word := range shapeGateWords {
		runes := pyunicode.Runes(word)
		for i := 0; i+len(runes) <= len(text); i++ {
			if slices.Equal(text[i:i+len(runes)], runes) {
				positions = append(positions, i)
			}
		}
	}
	return positions
}

// g18ShapeFindings は後から足す検出の 5 番目 (本家の _v220_shape_findings。1 行に同じ規則の候補は 1 件まで。
// copy_comma_noun は promo では文書の中で 2 回目に当たったものから出す)。
func (s *lintState) g18ShapeFindings() {
	positions := s.shapeGatePositions()
	if len(positions) == 0 {
		return
	}
	copyHits := 0
	for _, lineNo := range s.linesAt(positions) {
		row := s.an.Lines[lineNo-1]
		text := s.shapeVisibleText(row)
		if text == "" {
			continue
		}
		found := map[string]bool{}
		for _, part := range mustFindall0(shapeSentence, text) {
			sentence := pystr.Strip(part)
			if sentence == "" {
				continue
			}
			if !found["say_hedge"] && strings.Contains(sentence, "える") && sayHedgeSentence(sentence) {
				found["say_hedge"] = true
			}
			if strings.Contains(sentence, "、") {
				if copyCommaNounSentence(sentence) {
					copyHits++
					if s.docTypeName() != "promo" || copyHits >= 2 {
						found["copy_comma_noun"] = true
					}
				}
				if !found["bare_verb_chain"] && bareVerbChainSentence(sentence) {
					found["bare_verb_chain"] = true
				}
			}
		}
		for _, rule := range shapeRules {
			if found[rule.name] {
				s.add(newFinding(rule.name, lineNo, "info", rule.message, pystr.Strip(row.Raw)))
			}
		}
	}
}
