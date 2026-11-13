package lint

// G18 文の形 (say_hedge・copy_comma_noun・bare_verb_chain。本家 yomiyasu_lint.py の 5008-5159)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3 (maskQuotedSegments)

// g18State は G18 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g18State struct{}

// g18ShapeFindings は後から足す検出の 5 番目 (本家の _v220_shape_findings。promo で copy_comma_noun の数え方が変わる)。
func (s *lintState) g18ShapeFindings() {}
