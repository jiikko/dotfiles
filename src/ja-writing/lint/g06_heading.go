package lint

// G6 見出し (heading_colon・short_summary_heading・redundant_bracket。本家 yomiyasu_lint.py の 5699-5724)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: G1 の指標 (s.metrics)・T2 (stripEndDecoration)

// g06State は G6 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g06State struct{}

// g06Heading は見出しの行 (kind が heading) の最初。この後に骨組みが g08Vocabulary を呼ぶ。short_summary_heading は s.metrics.charCount < 2000 のとき。
func (s *lintState) g06Heading(_ *rowCtx) {}
