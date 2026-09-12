# 365 colorscheme が定義していない treesitter capture の「潰れ」を誰も検出していない

種別: research / test
状態: 起票直後 (本文を埋め中)

`dotfiles.hl.set` は「gui 色のみで cterm 併記が無い」を WARN で拾うが、
**colorscheme がその capture 自体を定義していない**ために兄弟が同じ見た目へ潰れる形は
何も検出しない。実例は issue なしで直した markdown の見出し (commit 4887d531)。

全数勘定と検出手段の提案を本文へ書く。
