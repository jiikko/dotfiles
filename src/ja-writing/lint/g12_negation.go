package lint

// G12 否定の対比 (negative_parallelism・negative_parallelism_density・bold_label_list。本家 yomiyasu_lint.py の 1703-1760・2839-2905・5730・5855-5876・5907-5922)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を持つ。いまは中身が空 (候補を出さない)。入れる人は、このファイルだけを書く
// (implementedRules の表の true への変更を除く)。
//
// 依存: T2 (withUndecorated・endClosersFor)。

// g12State は G12 が文書の中で持つ状態 (本家の行のループの外の変数 negation_lines・bold_label_lines と、g12Prepare が作る extra_negation_lines)。
type g12State struct{}

// g12Prepare は行のループの前に 1 回。本家の _extra_negation_line_numbers(analysis) (5522) に当たる。
func (s *lintState) g12Prepare() {}

// g12RecordBoldLabel は表でも見出しでもない行の最初に呼ばれる (太字の見出し語とコロンで始まる箇条書きの行を覚える。5730)。
func (s *lintState) g12RecordBoldLabel(_ *rowCtx) {}

// g12Negation は行の中の negative_parallelism の判定 (5855-5876)。出した行は覚えて、g12Document が数える。
func (s *lintState) g12Negation(_ *rowCtx) {}

// g12Document は行のループの後に呼ばれる (negative_parallelism_density は 3 件目の行・bold_label_list は 1 件目の行。5907-5922)。
func (s *lintState) g12Document() {}
