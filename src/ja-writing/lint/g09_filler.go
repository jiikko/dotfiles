package lint

// G9 文の型 (meta_filler・summary_restatement・meta_intro・count_declaration。本家 yomiyasu_lint.py の 1350-1395 と 5773-5804)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T2・T3。G11 (cleft_list_ending) がフィラーの表 (g09State に持つ) を読む

// g09State は G9 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g09State struct{}

// g09Filler はmeta_filler (フィラーの 15 型。文ごとに見て同じ型は 1 行 1 件)。引用を伏せた文 (s.rule9[r.lineNo]) と、飾りを除いた文 (withUndecorated・endClosersFor) を、型の文字列で使い分ける (地図 3.9 節の 5777-5792)。
func (s *lintState) g09Filler(_ *rowCtx) {}

// g09InfoSentences はinfo の 3 型 (summary_restatement・meta_intro・count_declaration)。
func (s *lintState) g09InfoSentences(_ *rowCtx) {}
