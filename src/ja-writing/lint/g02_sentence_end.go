package lint

// G2 文末の反復 (sentence_end_repetition。本家 yomiyasu_lint.py の 1802-1900 と 5574)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。

// g02State は G2 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g02State struct{}

// g02SentenceEnd は行のループの前に 1 回。s.sentences (段落の文の記録) を _split_decoration_breaks で分けて、_sentence_end_findings が 3 文以上続く同じ語尾を候補にする。T2 (decorationBreaks) と T3 (plainSentenceRecords) は土台にある。
func (s *lintState) g02SentenceEnd() {}
