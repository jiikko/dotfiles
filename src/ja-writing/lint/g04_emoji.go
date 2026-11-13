package lint

// G4 絵文字 (emoji_prohibited。本家 yomiyasu_lint.py の 5678-5686)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T1 (EmojiMatches)

// g04State は G4 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g04State struct{}

// g04Emoji は行のループの最初 (引用の行と表の行でも出る)。EmojiMatches(r.scanText) の最初の 3 つを空白でつないで message に入れる。
func (s *lintState) g04Emoji(_ *rowCtx) {}
