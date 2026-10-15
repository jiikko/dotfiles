# 770 (test): Go が手で写しているテーマの色番号を theme/colors.yml との一致の検査に入れる

起票日: 2026-10-15

## 概要

`theme/colors.yml` が色の単一ソースだが、一致を見る `tests/theme/test_theme_colors.sh` は `_tmux.conf` と nvim の `palette.lua` しか見ない。
Go 側の生成は git-popup の退役 (2026-08-06) で「Go の消費者が居ない」として畳まれた (`scripts/gen_theme_colors.sh` の冒頭) が、
今は Go が色番号を手で写している所がある。yml を変えても Go 側は黙ってずれる。

## 詳細 (2026-10-15 に本文で確認)

- `pro-con/ui/style.go`: コメントに「色の意味は theme/colors.yml に揃える (現在地 202 / 要対応 214 / 未 push 208 / 情報 51 / 正常 46 / 消灯 240 / 危険 196)」とあり、番号を直に使う
- `glogx/render.go` の `ansiFrameBorder` (`38;5;201`)
- ほかの Go module に同じ写しがあるかは数えていない (着手時に `38;5;` と色番号の定数で grep して数える)

## 対応方針 (どちらかを着手時に決める)

- A. 生成を戻す: `scripts/gen_theme_colors.sh` が tuikit の下 (例: `tuikit/sgr` の隣) に Go の定数を出し、各 module がそれを使う
- B. 検査だけ足す: `test_theme_colors.sh` に Go の写しを読ませて yml と突き合わせる
- A は写しそのものが消える。B は写しを残したまま、ずれを機械で止める。docs/theme-colors.md の記述も同じ変更で直す

## 関連ファイル

- `theme/colors.yml` / `tests/theme/test_theme_colors.sh` / `scripts/gen_theme_colors.sh` / `docs/theme-colors.md`
- `pro-con/ui/style.go` / `glogx/render.go`
- issue 418 (tmux の role。別の話) / 調査の全体: 773
