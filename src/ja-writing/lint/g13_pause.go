package lint

// G13 読点 (pause_comma。本家 yomiyasu_lint.py の 2967-3380 と 5878-5904)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: G13 を受け持つ段が str を受ける関数を先に作る。G17 が候補の _heads と _objects を読む

// g13State は G13 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g13State struct{}

// g13PauseComma は種類が pause_comma を当てる種類 (docTypeOnlyRules) のときだけ。g13State に行ごとの結果の覚え (pauseLines。essay かどうかで中身が変わる) を持つ。候補の Objects (空でも []string{}) と Heads は、G17 (slide_comma) が読む。
func (s *lintState) g13PauseComma(_ *rowCtx) {}
