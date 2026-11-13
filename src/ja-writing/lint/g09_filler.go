package lint

import "strings"

// G9 文の型 (meta_filler・summary_restatement・meta_intro・count_declaration。本家 yomiyasu_lint.py の 1350-1395 と 5773-5804)。表は g09_tables.go。
// G11 (cleft_list_ending) が fillerPatterns を読む。

const fillerMessageHead = "が検出されました。単なる前置きや不要な飾りであれば削り、本題から書いてください。ただし、「何が大事か」という評価や主張そのものを担っている場合は、述語に移すなどして意味を残してください。"

// g09Filler は meta_filler (フィラーの型。文ごとに見て、同じ型は 1 行 1 件)。
// 文末に固定した型 ($) は、文末の後ろの飾りを除いた文にも当てる。「過言ではない」の型だけは、引用を伏せた文 (rule9) に当てる。
func (s *lintState) g09Filler(r *rowCtx) {
	var lineCores, contextCores []string
	closers := s.doc.endClosersFor(r.lineNo)
	for _, p := range fillerPatterns {
		if !mayMatch(p.required, r.plainText) {
			continue
		}
		var sentences []string
		switch {
		case p.pattern == overstatementPattern:
			if contextCores == nil {
				var own []string
				for _, c := range s.rule9[r.lineNo] {
					own = append(own, c.Sentence)
				}
				contextCores = withUndecorated(own, closers)
			}
			sentences = contextCores
		case strings.Contains(p.pattern, "$"):
			if lineCores == nil {
				lineCores = withUndecorated(r.lineSentences, closers)
			}
			sentences = lineCores
		default:
			sentences = r.lineSentences
		}
		if anySentenceMatches(p.re, sentences) {
			s.add(newFinding("meta_filler", r.lineNo, "warn", p.desc+fillerMessageHead+fillerNotes[p.desc], r.stripped))
		}
	}
}

func anySentenceMatches(p *lazyPattern, sentences []string) bool {
	for _, sentence := range sentences {
		if mustSearch(p, sentence) {
			return true
		}
	}
	return false
}

// g09InfoSentences は info の型 (summary_restatement・meta_intro・count_declaration)。
func (s *lintState) g09InfoSentences(r *rowCtx) {
	for _, p := range infoSentencePatterns {
		if mayMatch(p.required, r.plainText) && anySentenceMatches(p.re, r.lineSentences) {
			s.add(newFinding(p.rule, r.lineNo, "info", p.message, r.stripped))
		}
	}
}
