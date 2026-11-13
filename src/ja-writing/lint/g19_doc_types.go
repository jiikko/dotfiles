package lint

// G19 文書の型 (dialogue_period・narrative_density。本家 yomiyasu_lint.py の 5162-5300)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3

// g19State は G19 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g19State struct{}

// g19DocumentFindings は後から足す検出の 6 番目 (本家の _v220_document_findings。script と essay だけ)。
func (s *lintState) g19DocumentFindings() {}
