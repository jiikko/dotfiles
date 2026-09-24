# 418 (test): theme/colors.yml の 3 つの色の役割が、tmux 側の手書きのコピーと突き合わされていない

起票日: 2026-09-24
反証レビュー: 2026-09-24 実施 (読み取り専用のサブエージェント 1 体)。主要な主張は反証できず

## 概要

`theme/colors.yml` は色の単一の定義元で、`tests/theme/test_theme_colors.sh` が各消費先との一致を見ている。
ただし次の 3 つの役割は、tmux 側 (`_tmux.conf` / スクリプト) の値を検査していない。どれも手書きの数値で、
生成される `scripts/lib/theme_colors.sh` の定数 (`THEME_INFO_CYAN` など) を読む消費先も無い。

**値は今は一致している** (ずれではなく、検査の無い重複)。同じ日に見つかったペースゲージの色のずれ
(statusline と glogx) と同じ形で、どこかを変えても誰も気づかない。

出典: 2026-09-24 の設計・品質スキャン (観点 3「同じルールの二重実装のずれ」)。起票者が grep で確認した。

## 詳細

| 役割 | 値 | 検査されていない消費先 | 検査されている消費先 |
|---|---|---|---|
| `info_cyan` | 51 | `_tmux.conf` の `colour51` (8 行: status-left / status-format / message-style / copy-mode-match-style / window-status-bell-style 等) | なし |
| `blink_magenta` | 201 | `_tmux.conf` の `colour201` (3 行) / `scripts/tmux_scratch_popup.sh` | Go 側だけ (`src/glogx/box_test.go` の `TestFrameBorderMatchesThemeYML`) |
| `error_red` | 196 | `_tmux.conf` の `colour196` (3 行: window-status-separator / synchronize-panes の枠 等) | nvim 側だけ (`diag.error_bg`) |

確認方法: `grep -nE 'info_cyan\|blink_magenta\|error_red\|INFO_CYAN\|BLINK_MAGENTA\|ERROR_RED' tests/theme/test_theme_colors.sh`
のヒットは `THEME_ERROR_RED` (nvim) の 1 行だけ。`THEME_INFO_CYAN` / `THEME_BLINK_MAGENTA` / `THEME_ERROR_RED` を
`scripts/lib/theme_colors.sh` の外で読んでいる箇所は無い (worktree の残骸を除く)。

### 問題なしと判断した組 (スキャン時点)

- glogx のペースゲージの色 / 帯 ↔ statusline: `src/glogx/usage/pace_drift_test.go` が検査
- `docs/glogx-ui-guide.md` のキーの語彙表 ↔ `listnav.MotionOf`: 一致。`motion_vocabulary_test.go` が配線を固定
- statusline の `human_tokens` (k / M 表記) と `rate_color` の 50 / 80% の閾値: Go 側に対応する実装が無い (二重実装ではない)

### 同日に分かった新しい二重実装 (参考)

- src/pro-con 用に足される `tuikit/editor` ($VISUAL → $EDITOR → nvim) は、glogx の `editorCommand`
  (`src/glogx/external_commands.go`) と同じ契約。dotfiles-5c セッションが glogx の `editorCommand` を
  `tuikit/editor.Command` へ委ねて二重実装を消す予定 (2026-09-24 の申告)。この issue の対象外

## 対応方針

1. `tests/theme/test_theme_colors.sh` に、3 つの役割の tmux 側の `assert_tmux` を足す (既存の形に合わせる)
2. `scripts/tmux_scratch_popup.sh` の `colour201` も検査に含めるか、生成された定数を読む形にする
3. 足した検査は、`_tmux.conf` の値を 1 つ変える変異で red になることを確かめる

## 関連ファイル

- `theme/colors.yml` / `scripts/gen_theme_colors.sh` / `scripts/lib/theme_colors.sh`
- `tests/theme/test_theme_colors.sh` / `docs/theme-colors.md`
- `_tmux.conf` / `scripts/tmux_scratch_popup.sh`

## 進捗

- [ ] info_cyan の tmux 側を検査
- [ ] blink_magenta の tmux / scratch popup 側を検査
- [ ] error_red の tmux 側を検査
