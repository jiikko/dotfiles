package lint

// G11 短い文の型 (short_mochiron・fragment_run・cleft_list_ending。本家 yomiyasu_lint.py の 1561-1703 と 5817-5853)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T2・T3。cleft_list_ending は G9 のフィラーの表を読むので、G9 が入るまで cleft_list_ending は入れられない (groupDependencies)

// g11State は G11 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g11State struct{}

// g11ShortSentences はこの順に出す: short_mochiron (r.sentenceBefore と r.lineSentences を見る)・fragment_run・cleft_list_ending。
func (s *lintState) g11ShortSentences(_ *rowCtx) {}
