package lint

// G16 AI の言い回し (ai_phrase。本家 yomiyasu_lint.py の 4921-5005)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3・G13 の _TE_NOT_VERB (語の表)

// g16State は G16 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g16State struct{}

// g16AiPhraseFindings は後から足す検出の 3 番目 (本家の _v250_ai_phrase_findings。種類で型が変わる: script・essay・slides・minutes・promo)。
func (s *lintState) g16AiPhraseFindings() {}

// aiPhraseMessage は ai_phrase の message (本家の _AI_PHRASE_MESSAGE.format("、".join(rest)))。post.go の withWords が呼ぶ。
func aiPhraseMessage(words []string) string {
	panic("G16 の aiPhraseMessage がまだ入っていません")
}
