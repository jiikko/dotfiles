# 770 (test): pro-con が手で写しているテーマの色番号を theme/colors.yml との一致の検査に入れる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

`theme/colors.yml` が色の単一ソースだが、pro-con は色番号を手で写していて、yml との一致を見る検査が無い (yml を変えても pro-con は黙ってずれる)。
glogx は既に `src/glogx/box_test.go` の `TestFrameBorderMatchesThemeYML` が yml を読んで `ansiFrameBorder` (201) と突き合わせ、
`src_glogx.yml` の paths にも `theme/colors.yml` が入っている。同じ形を pro-con にも置く。

## 詳細 (2026-10-15 に本文で確認)

- 一致の検査: `tests/theme/test_theme_colors.sh` は `_tmux.conf` と nvim の `palette.lua` だけを見る。Go の生成は git-popup の退役 (2026-08-06) で畳まれた
  (`scripts/gen_theme_colors.sh` の冒頭)。glogx だけは上の Go のテストで守られている
- pro-con の写し (grep `38;5;\(20[128]\|214\|240\|196\|46\|51\)m` ほか):
  - `pro-con/ui/style.go` (コメントに「現在地 202 / 要対応 214 / 未 push 208 / 情報 51 / 正常 46 / 消灯 240 / 危険 196」。`fg(n)` で番号を直に使う)
  - `pro-con/ui/view.go` の `sgrRed` (196) / `sgrYellow` (214) / `sgrCyan` (51)
  - `pro-con/ui/search.go` の `sgrHit` (214)
- 🚨 **意味と yml の role が合っていない**: `style.go` は 214 を「要対応」と呼ぶが、yml の 214 は `quantity_yellow` (数量)。番号の一致だけを見る検査を足すと、
  意味のずれを固定してしまう。先に「pro-con の各色がどの role か」を決める
- `src_pro-con.yml` の paths に `theme/colors.yml` が無い (yml を変えても pro-con の CI は起動しない)
- glogx の `issues_view.go` の `catYellow` (214。perf のカテゴリ) は TestFrameBorderMatchesThemeYML の対象外。テーマの role に属する色かは未確認

## 対応方針

1. pro-con の写しを列挙し、各色を yml の role に対応付ける (対応する role が無いものは yml に足すか、テーマの外の色として除く)
2. glogx と同じ形 (Go のテストが yml を読んで突き合わせる) を pro-con に置き、`src_pro-con.yml` の paths と `scripts/test_changed.sh` の対応 (`theme/*` → glogx) に pro-con を足す
3. 生成を戻す案 (tuikit の下に定数を出す) は、写しが 2 module を超えたら再評価する
4. `docs/theme-colors.md` の記述を同じ変更で直す

## 関連ファイル

- `theme/colors.yml` / `tests/theme/test_theme_colors.sh` / `scripts/gen_theme_colors.sh` / `scripts/test_changed.sh` / `docs/theme-colors.md`
- `src/glogx/box_test.go` (`TestFrameBorderMatchesThemeYML`。手本) / `pro-con/ui/style.go` / `pro-con/ui/view.go` / `pro-con/ui/search.go`
- issue 418 (tmux の role。別の話) / 調査の全体: 773

## 反証レビュー

- 2026-10-15 (read-only のサブエージェント 1 体): P1 glogx は既に TestFrameBorderMatchesThemeYML と CI の paths で守られている → glogx を外し、その形を手本にした。タイトルも pro-con に絞った /
  P2 写しの数え漏れ (view.go・search.go) と、214 の意味のずれ → 書いた
