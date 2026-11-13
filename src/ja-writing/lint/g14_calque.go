package lint

// G14 直訳語と語の一覧 (calque・metaphor_label・katakana_term・thin_evaluation。本家 yomiyasu_lint.py の 3417-4671)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3 (v220ScanText)。G8 (metaphor_verb・translated_term の候補と _KIKU_RE・_PUBLICATION_WORDS)。語の一覧 (lists/) の読み手が要る

// g14State は G14 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g14State struct{}

// g14WordFindings は行のループの後 (後から足す検出の 1 番目。本家の _v220_word_findings)。s.findings (これまでの候補) を読んで、同じ行に metaphor_verb があれば calque を出さない・translated_term があれば型 3 を出さない (4588-4594)。
func (s *lintState) g14WordFindings() {}

// calqueMessage は calque の message (本家の _calque_message(numbers, word_types))。post.go の withWords が、allow.txt で一部の語を外した候補の message を作り直すときに呼ぶ。
// 群を入れるまでは候補が出ないので呼ばれない。入れ忘れて呼ばれたら、黙って別の message を出さずに止める。
func calqueMessage(numbers []int, wordTypes []wordType) string {
	panic("G14 の calqueMessage がまだ入っていません")
}
