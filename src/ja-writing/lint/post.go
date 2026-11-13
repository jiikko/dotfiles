package lint

import (
	"slices"
	"strings"

	"ja-writing/visibility"
)

// 検出が終わった後の処理 (本家 lint_text の 5936-5988): 種類で外す → allow.txt で外す → ダッシュの上限 → 点数と件数。

// skipCount は種類で外した規則と件数 (skipped_by_doc_type の 1 要素。鍵の順は挿入の順)。
type skipCount struct {
	rule  string
	count int
}

// result は lint_text の戻り値 (--json に出す全体)。
type result struct {
	findings       []finding
	counts         [3]int // error・warn・info
	penaltyScore   int
	docType        *string
	reader         *string
	skipped        []skipCount
	words          *wordsReport
	metrics        metrics
	candidateCount int
}

// wordsReport は context.words (語の一覧の 5 つか 7 つの欄と allow_removed)。
type wordsReport struct {
	context      visibility.WordsContext
	allowRemoved []skipCount
}

// finish は検出の生の並び (s.findings) から、外す・数える・点数をつけるまでを行って結果にする。
func (s *lintState) finish() *result {
	findings, skipped := applyDocType(s.findings, s.docType)
	findings, words := applyWords(findings, s.words)
	findings, dashHits := applyDashLimit(findings)
	res := &result{
		docType: s.docType, reader: s.reader, skipped: skipped, words: words, metrics: s.metrics,
		penaltyScore: rulePenaltyScore(slices.DeleteFunc(slices.Clone(findings), func(f finding) bool { return slices.Contains(penaltyUncounted, f.Rule) }), s.proseChars, dashHits),
	}
	for _, f := range findings {
		switch f.Severity {
		case "error":
			res.counts[0]++
		case "warn":
			res.counts[1]++
		case "info":
			res.counts[2]++
		}
	}
	res.findings, res.candidateCount = findings, len(findings)
	return res
}

// applyDocType は文書の種類で外す規則を、検出が終わった後に前から順に除いて、規則ごとに数える (件数と点数を数える前)。残った候補の順は変えない。
// 種類を指定しないときは、種類を限って当てる規則 (docTypeOnlyRules) だけを除く。
func applyDocType(findings []finding, docType *string) ([]finding, []skipCount) {
	var skipped []skipCount
	var skipRules []string
	var partialRule string
	var partialPrefixes []string
	count := func(rule string) *skipCount {
		for i := range skipped {
			if skipped[i].rule == rule {
				return &skipped[i]
			}
		}
		skipped = append(skipped, skipCount{rule: rule})
		return &skipped[len(skipped)-1]
	}
	if docType != nil {
		skipRules = skipRulesFor(*docType)
		for _, rule := range skipRules {
			count(rule)
		}
		if p, ok := docTypeSkipPatterns[*docType]; ok {
			partialRule, partialPrefixes = p.rule, p.prefixes
			count(partialRule)
		}
	}
	kept := make([]finding, 0, len(findings))
	for _, f := range findings {
		partial := f.Rule == partialRule && slices.ContainsFunc(partialPrefixes, func(prefix string) bool { return strings.HasPrefix(f.Message, prefix) })
		if slices.Contains(skipRules, f.Rule) || partial {
			count(f.Rule).count++
			continue
		}
		only, restricted := docTypeOnlyRules[f.Rule]
		if !restricted || (docType != nil && slices.Contains(only, *docType)) {
			kept = append(kept, f)
		}
	}
	return kept, skipped
}

// applyWords は語の一覧の allow.txt の語で候補を外す。外すのは allowRules の規則の候補で、候補の Words のどれかが allow の語と同じもの。残った候補の順は変えない。
// 1 行に 1 件で語を並べる規則 (allowPartialRules) は、許した語だけを除き、許していない語が残れば候補を残して message を作り直す (外した件数には数えない)。
// words が nil (一覧を使わない) なら候補をそのまま返し、context の値は nil。
func applyWords(findings []finding, words *visibility.Words) ([]finding, *wordsReport) {
	if words == nil {
		return findings, nil
	}
	removed := make([]skipCount, len(allowRules))
	for i, rule := range allowRules {
		removed[i].rule = rule
	}
	kept := findings
	if len(words.Allow) > 0 {
		kept = make([]finding, 0, len(findings))
		for _, f := range findings {
			allowed := func(word string) bool { return slices.Contains(words.Allow, word) }
			if len(f.PatternMatches) > 0 {
				// flag-patterns.txt のパターンは、名前が allow にあるか、その行で当たった文字列がどれも allow にあれば外す。
				base := allowed
				allowed = func(word string) bool {
					return base(word) || slices.ContainsFunc(f.PatternMatches, func(p patternMatch) bool {
						return p.Name == word && !slices.ContainsFunc(p.Strings, func(s string) bool { return !base(s) })
					})
				}
			}
			i := slices.Index(allowRules, f.Rule)
			if i < 0 || !slices.ContainsFunc(f.Words, allowed) {
				kept = append(kept, f)
				continue
			}
			rest := slices.DeleteFunc(slices.Clone(f.Words), allowed)
			if slices.Contains(allowPartialRules, f.Rule) && len(rest) > 0 {
				kept = append(kept, f.withWords(rest))
			} else {
				removed[i].count++
			}
		}
	}
	return kept, &wordsReport{context: visibility.WordsContextOf(words), allowRemoved: removed}
}

// withWords は許していない語 (rest) だけを残した候補の写し。calque・project_term・renyo_noun・ai_phrase は message も作り直す。
func (f finding) withWords(rest []string) finding {
	out := f
	out.Words = rest
	switch f.Rule {
	case "calque":
		out.WordTypes = nil
		var numbers []int
		for _, wt := range f.WordTypes {
			if slices.Contains(rest, wt.Word) {
				out.WordTypes = append(out.WordTypes, wt)
				if !slices.Contains(numbers, wt.Number) {
					numbers = append(numbers, wt.Number)
				}
			}
		}
		if out.WordTypes == nil {
			out.WordTypes = []wordType{}
		}
		out.Message = calqueMessage(numbers, out.WordTypes)
	case "project_term":
		out.Message = projectTermMessage(rest, f.PatternMatches, f.FlagHits, f.Replacements)
	case "renyo_noun":
		out.Message = renyoMessage(rest)
	case "ai_phrase":
		out.Message = aiPhraseMessage(rest)
	}
	return out
}

// applyDashLimit は、残った候補のうちダッシュの 3 規則の 4 件目以降を除く (dashFindingLimit)。点数には全部を数えるので、数えた件数 (外す前を含む) も返す。
func applyDashLimit(findings []finding) ([]finding, int) {
	dashHits := 0
	shown := make([]finding, 0, len(findings))
	for _, f := range findings {
		if slices.Contains(dashRules, f.Rule) {
			dashHits++
			if dashHits > dashFindingLimit {
				continue
			}
		}
		shown = append(shown, f)
	}
	return shown, dashHits
}

// rulePenaltyScore は候補の重みを地の文 1,000 字あたりに直した x から、100 × 40 ÷ (40 + x) を丸める目安 (品質の点数ではない)。整数だけで計算する。
// findings は、点数に数えない規則 (penaltyUncounted) を除いたもの。
func rulePenaltyScore(findings []finding, proseChars, dashHits int) int {
	if len(findings) == 0 {
		return 100
	}
	lineWeight, documentWeight, dashFound := 0, 0, 0
	for _, f := range findings {
		if slices.Contains(dashRules, f.Rule) {
			dashFound++
		}
		if w, ok := documentRuleWeights[f.Rule]; ok {
			documentWeight += w
		} else if f.Rule != "negative_parallelism_density" {
			lineWeight += penaltyWeights[f.Severity]
		}
	}
	lineWeight += max(0, dashHits-dashFound) * penaltyWeights["warn"]
	m := max(proseChars, penaltyFloor)
	num := 100 * penaltyHalf * m
	den := (penaltyHalf+documentWeight)*m + 1000*lineWeight
	return max(1, min(99, (2*num+den)/(2*den)))
}
