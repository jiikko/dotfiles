package lint

// G7 文末のコロン (trailing_colon。本家 yomiyasu_lint.py の 5737-5748)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。

// g07State は G7 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g07State struct{}

// g07TrailingColon は見出しでも表でもない行。r.stripped が「：」か「:」で終わり、コードの伏せを除いた行が http で始まらず、最後の字が保護された範囲にないとき。
func (s *lintState) g07TrailingColon(_ *rowCtx) {}
