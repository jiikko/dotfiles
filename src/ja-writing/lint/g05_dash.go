package lint

// G5 ダッシュ (dash_decoration・dash_insertion・dash_list_ending。本家 yomiyasu_lint.py の 1005-1182 と 5694-5698)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: T2 (endDecorationRun)・T3 (引用の伏せ maskQuotedSegments)

// g05State は G5 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g05State struct{}

// g05Dash は表と引用でない行で、文書にダッシュの字があるとき (s.dashPossible) だけ呼ばれる。_dash_finding(r.plainText, r.lineNo, r.stripped)。1 文書の件数の上限 (4 件目以降) は post.go の applyDashLimit が持つので、ここでは全部足す。
func (s *lintState) g05Dash(_ *rowCtx) {}
