package lint

// G15 連用形の名詞 (renyo_noun。本家 yomiyasu_lint.py の 4695-4720 と 4887-4920)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T3

// g15State は G15 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g15State struct{}

// g15RenyoFindings は後から足す検出の 2 番目 (本家の _v230_renyo_findings)。
func (s *lintState) g15RenyoFindings() {}

// renyoMessage は renyo_noun の message (本家の _RENYO_MESSAGE.format("、".join("の" + word for word in rest)))。post.go の withWords が呼ぶ。
func renyoMessage(words []string) string {
	panic("G15 の renyoMessage がまだ入っていません")
}
