package lint

// G3 太字 (bold_not_rendered。本家 yomiyasu_lint.py の 2076-2836 と 5577-5584)。
//
// 骨組み (lint_text.go) が本家の順に呼ぶ口を、この群のメソッドとして持つ。いまは中身が空 (候補を出さない)。
// 入れる人は、このファイルだけを書く (implementedRules の表の true への変更を除く)。
//
// 依存: 他の群と独立。internal/bold と lint/bold*.go は 1083 が作る。

// g03State は G3 が文書の中で持つ状態 (本家の行のループの外の変数など)。骨組みは何も入れない。
type g03State struct{}

// g03Bold は行のループの前に 1 回。issue 1083 (dotfiles-24) の lint.BoldFindings(s.text, s.an) を呼び、返った候補を s.add(newFinding("bold_not_rendered", line, "error", message, snippet)) で足す (message と snippet の組み立ては本家の 5577-5584。BoldFinding の欄の写し方は 1083 の申し送りと issue 1075 の「G3 をつなぐ口」)。
func (s *lintState) g03Bold() {}
