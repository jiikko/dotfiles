package lint

// G20 語の一覧の語 (project_term。本家 yomiyasu_lint.py の 5314-5448)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3・visibility (flag-patterns の CheckedPattern)

// g20State は G20 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g20State struct{}

// g20ProjectTermFindings は後から足す検出の 7 番目 (本家の _project_term_findings)。s.words (flag の語とパターン) を見える文字に当てる。内部の欄の Words・Replacements・PatternMatches・FlagHits は、post.go の applyWords が allow.txt で外すときに読む。
func (s *lintState) g20ProjectTermFindings() {}

// projectTermMessage は project_term の message (本家の _project_term_message(words, pattern_names, flag_hits, replacements))。post.go の withWords が、
// allow.txt で一部の語を外した候補の message を作り直すときに呼ぶ。
func projectTermMessage(words []string, patterns []patternMatch, flagHits []string, replacements []wordPair) string {
	panic("G20 の projectTermMessage がまだ入っていません")
}
