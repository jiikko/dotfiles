package lint

// G17 スライドの読点 (slide_comma。本家 yomiyasu_lint.py の 4723-4884)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3・G13 (pause_comma の候補の内部の欄)

// g17State は G17 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g17State struct{}

// g17SlideCommaFindings は後から足す検出の 4 番目 (本家の _v240_slide_comma_findings。slides だけ)。s.findings の pause_comma の候補の Heads・Objects を読む。
func (s *lintState) g17SlideCommaFindings() {}
