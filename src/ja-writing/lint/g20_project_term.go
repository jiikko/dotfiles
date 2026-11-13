package lint

import (
	"cmp"
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G20 語の一覧の語 (project_term。本家 yomiyasu_lint.py の 5314-5448)。表は g20_tables.go。
//
// 見える文字 (コード・URL・リンク先・HTML・frontmatter を伏せた行) に flag.txt の語か flag-patterns.txt のパターンが出る行に 1 件。
// 引用とカギ括弧の中は数えない。内部の欄 Words・Replacements・PatternMatches・FlagHits は、post.go の applyWords が allow.txt で外すときに読む。

func isKanjiRune(r rune) bool {
	return '一' <= r && r <= '龥' || strings.ContainsRune("々〆ヵヶ", r)
}
func isHiraganaRune(r rune) bool { return 'ぁ' <= r && r <= 'ん' }
func isKatakanaRune(r rune) bool { return 'ァ' <= r && r <= 'ヴ' || r == 'ー' }

// flagPattern は本家の _flag_pattern。flag.txt の語を 1 つの正規表現にする (長い語を先に。英数字の端には識別子の境目を付ける)。語がなければ nil。
func flagPattern(flag []string) *pyre.Pattern {
	words := slices.Clone(flag)
	slices.SortFunc(words, func(a, b string) int {
		if c := cmp.Compare(pystr.Len(b), pystr.Len(a)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	words = slices.Compact(words)
	if len(words) == 0 {
		return nil
	}
	parts := make([]string, len(words))
	for i, word := range words {
		runes := pyunicode.Runes(word)
		part := pyre.Escape(word)
		if mustMatchAt0(wordASCII, string(runes[0])) {
			part = wordBeforePattern + part
		}
		if mustMatchAt0(wordASCII, string(runes[len(runes)-1])) {
			part += wordAfterPattern
		}
		// 漢字と仮名を両方含む語 (根の、の木) は、漢字の端の前後に漢字が続く所 (大根の、の木構造) では数えない。
		// 漢字だけの語 (公開鍵) と仮名だけの語は、語の一部にも当たる。
		if slices.ContainsFunc(runes, isKanjiRune) && slices.ContainsFunc(runes, func(r rune) bool { return isHiraganaRune(r) || isKatakanaRune(r) }) {
			if isKanjiRune(runes[0]) {
				part = wordKanjiBeforePattern + part
			}
			if isKanjiRune(runes[len(runes)-1]) {
				part += wordKanjiAfterPattern
			}
		}
		parts[i] = part
	}
	return pyre.MustCompile(strings.Join(parts, "|"), 0)
}

// replacementBounded は本家の _replacement_bounded。text の start にある使う語が、英数字の端で識別子や別の英単語の一部になっていないか。
// flag.txt の語の端 (flagPattern) と同じ境目を、使う語の端にも当てる。日本語の字の端では見分けない。
func replacementBounded(text pyre.Text, start int, replacement string) bool {
	runes := pyunicode.Runes(replacement)
	if mustMatchAt0(wordASCII, string(runes[0])) && !matchAtPos(replacementBefore.Get(), text, start) {
		return false
	}
	return !mustMatchAt0(wordASCII, string(runes[len(runes)-1])) || matchAtPos(replacementAfter.Get(), text, start+len(runes))
}

// inReplacement は本家の _in_replacement。flag.txt の語の当たりが、その語の使う語の中に収まっているか (使う語が語を含む組で、もう使う語で書いてある所を数えない)。
func inReplacement(text pyre.Text, matchStart int, word string, replacements map[string]string) bool {
	replacement := replacements[word]
	if replacement == "" || !strings.Contains(replacement, word) {
		return false
	}
	runes, wordRunes, source := pyunicode.Runes(replacement), pyunicode.Runes(word), pyunicode.Runes(text.String())
	for offset := runeIndexAt(runes, wordRunes, 0); offset >= 0; offset = runeIndexAt(runes, wordRunes, offset+1) {
		start := matchStart - offset
		if start >= 0 && start+len(runes) <= len(source) && slices.Equal(source[start:start+len(runes)], runes) && replacementBounded(text, start, replacement) {
			return true
		}
	}
	return false
}

// runeIndexAt は runes.find(sub, from) (見つからなければ -1)。
func runeIndexAt(runes, sub []rune, from int) int {
	for i := from; i+len(sub) <= len(runes); i++ {
		if slices.Equal(runes[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// g20ProjectTermFindings は後から足す検出の 7 番目 (本家の _project_term_findings)。s.words (flag の語とパターン) を見える文字に当てる。
func (s *lintState) g20ProjectTermFindings() {
	words := s.words
	if words == nil || (len(words.Flag) == 0 && (words.Patterns == nil || len(words.Patterns.Entries) == 0)) {
		return
	}
	var pattern *pyre.Pattern
	if len(words.Flag) > 0 {
		pattern = flagPattern(words.Flag)
	}
	var patterns []visibility.FlagPattern
	if words.Patterns != nil {
		patterns = words.Patterns.Entries
	}
	for _, row := range s.an.Lines {
		if isOpaqueRow(row.Kind) || row.Kind == "quote" || row.QuoteDepth != 0 {
			continue
		}
		visible := s.doc.proseVisibleText(row.Line)
		if pystr.Strip(visible) == "" {
			continue
		}
		masked := maskQuotedSegments(visible)
		maskedText := pyre.NewText(masked)
		hits := []string{}
		if pattern != nil {
			matches, err := pattern.Finditer(maskedText, 0, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			for _, m := range matches {
				word, _ := m.Group(0)
				if len(words.FlagReplacements) > 0 && inReplacement(maskedText, m.Start(0), word, words.FlagReplacements) {
					continue
				}
				hits = uniqueAppend(hits, word)
			}
		}
		var matches []patternMatch
		for _, p := range patterns {
			if !slices.ContainsFunc(p.Compiled.Required, func(runs []string) bool {
				return !slices.ContainsFunc(runs, func(literal string) bool { return !strings.Contains(masked, literal) })
			}) {
				continue
			}
			found, err := p.Compiled.Compiled.Finditer(maskedText, 0, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			var strs []string
			for _, m := range found {
				str, _ := m.Group(0)
				strs = uniqueAppend(strs, str)
			}
			if len(strs) == 0 {
				continue
			}
			if i := slices.IndexFunc(matches, func(pm patternMatch) bool { return pm.Name == p.Name }); i >= 0 {
				for _, str := range strs {
					matches[i].Strings = uniqueAppend(matches[i].Strings, str)
				}
			} else {
				matches = append(matches, patternMatch{Name: p.Name, Strings: strs})
			}
		}
		if len(hits) == 0 && len(matches) == 0 {
			continue
		}
		shown := slices.Clone(hits)
		for _, pm := range matches {
			shown = uniqueAppend(shown, pm.Name)
		}
		var replacements []wordPair
		for _, word := range hits {
			if replacement, ok := words.FlagReplacements[word]; ok {
				replacements = append(replacements, wordPair{word, replacement})
			}
		}
		f := newFinding("project_term", row.Line, "info", projectTermMessage(shown, matches, hits, replacements), pystr.Strip(row.Raw))
		f.Words = shown
		f.Replacements = replacements
		if len(matches) > 0 {
			f.PatternMatches = matches
			f.FlagHits = hits
		}
		s.add(f)
	}
}

// uniqueAppend は dict.fromkeys の順で、まだ無い語だけを後ろに足す。
func uniqueAppend(items []string, item string) []string {
	if slices.Contains(items, item) {
		return items
	}
	return append(items, item)
}

// projectTermMessage は project_term の message (本家の _project_term_message(words, pattern_names, flag_hits, replacements))。
// flagHits が nil なら、パターンの名前でない語を flag.txt の語とみなす。post.go の withWords が、allow.txt で一部の語を外した候補の message を作り直すときにも呼ぶ。
func projectTermMessage(words []string, patterns []patternMatch, flagHits []string, replacements []wordPair) string {
	isPattern := func(word string) bool {
		return slices.ContainsFunc(patterns, func(p patternMatch) bool { return p.Name == word })
	}
	fromPatterns := slices.ContainsFunc(words, isPattern)
	var flagWords []string
	for _, word := range words {
		if (flagHits == nil && !isPattern(word)) || (flagHits != nil && slices.Contains(flagHits, word)) {
			flagWords = append(flagWords, word)
		}
	}
	joined := strings.Join(words, "、") + "）" + v220MessageTail
	var message string
	switch {
	case !fromPatterns:
		message = projectTermMessageHead + joined
	case len(flagWords) == 0:
		message = projectTermPatternMessageHead + joined
	default:
		message = projectTermBothMessageHead + joined
	}
	var suggestions []string
	for _, word := range flagWords {
		if i := slices.IndexFunc(replacements, func(p wordPair) bool { return p.Word == word }); i >= 0 {
			suggestions = append(suggestions, word+" → "+replacements[i].Replacement)
		}
	}
	if len(suggestions) > 0 {
		message += "使う語: " + strings.Join(suggestions, "、") + "。"
	}
	return message
}
