# 770 (test): pro-con が手で写しているテーマの色番号を theme/colors.yml との一致の検査に入れる

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

## 進捗

- [x] 2026-10-15: pro-con/ui のテーマの番号を style.go の `col*` (current_accent / quantity_yellow / marker_orange / info_cyan / active_green / cold_gray / error_red) に集めた。
  `TestThemeColorsMatchYML` が yml と突き合わせ、`TestNoThemeColorLiterals` が ui の production の .go を AST で読んで直書きを落とす (色でない同じ数とログの役の色 (見本 A) は `theme-literal-ok: <理由>` で外す)。
  CI の paths と `scripts/test_changed.sh` に theme/colors.yml → pro-con を足し、`docs/theme-colors.md` に Go の消費者を書いた。出す SGR は 1 バイトも変えていない (レビューで確認)
  - 意味のずれ: pro-con は quantity_yellow (214) を「要対応」に使っている (yml に要対応の role は無い)。番号は追従させ、意味は docs に書いた
  - commit:「test(pro-con): テーマの色番号を style.go の col* に集め…」/「…AST で全部拾い、残っていた 7 か所を col* に寄せる」/「…素通りを塞ぐ」
  - 変異 (全部 red): colCurrent を 203 → TestThemeColorsMatchYML / colMarker を 209 → 同 / fg(202) の直書き・`laneOn = 202`・tmux の `fg=colour202`・stateColor の `return 51`・
    SGR の文字列 `"\x1b[48;5;196m"`・`var colorForLane = 214`・16 進 `0xF0` → TestNoThemeColorLiterals
  - 敵対的レビュー (opus) 3 周: 1 周目 P1 正規表現が fg(N) と SGR の文字列しか見ず 7 か所残っていた → AST で全部拾う形に作り直した / 2 周目 P1 `\b` のせいで SGR の文字列が素通り (後退) → 直した、
    P2 外す定義が「col で始まる名前ならどこでも」 → style.go の関数の外の const に限り、数をテーマの番号の数と合わせる、P3 理由の空の印・16 進 → 直した
  - 検出しない形 (テストの冒頭に書いた): ui の外の package・計算で作る番号・分けた文字列の連結・rune や float・1 行の印が同じ行のほかのリテラルも外すこと。
    settings.go の `fg(239)` は selection_bg と同じ番号だが、テーマの色として使っているかは未確認 (themeNums の外)
- make test (統合した木・rebase の後) rc=0 (src/PACKAGES.md の索引の漏れを直した後に test-lint を回し直した)
