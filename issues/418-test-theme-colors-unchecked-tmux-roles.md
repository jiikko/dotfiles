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

すべて「test(theme): info_cyan / blink_magenta / error_red の tmux 側を colors.yml と突き合わせる」で対応。

- [x] info_cyan の tmux 側を検査 — session 帯・path 帯・bell (`window-status-bell-style`)・通知 (`message-style`) の 4 行
- [x] blink_magenta の tmux / scratch popup 側を検査 — 点滅 (`@blink-phase`)・scratch の時計・`scripts/tmux_scratch_popup.sh` の枠の地
- [x] error_red の tmux 側を検査 — 同期中の枠 (`pane_synchronized`)
- 固定したのは colors.yml の役割のコメントが挙げる用途の行だけで、行に固有の断片ごと固定した。_tmux.conf は同じ番号を
  別の意味にも使っている (区切り線の 196、scratch の点滅の相方の 196、コピーモードの 51 等) ので、番号だけの部分一致だと
  別の意味の行が残っていれば緑になる。**別の意味の行は固定していない** (変えたときに誤って赤くならないように)
- yml の info_cyan が挙げる「popup ブランチ」の消費先は特定できなかった (`scripts/tmux_agent_panel.sh` の場所の列は
  `38;5;51` だが、ブランチではない)。未固定
- 検証: 変異 (bin/mutate-verify) 3 本とも red (session 帯を 50 に / 同期の枠を 197 に / scratch popup の地を 200 に)。
  判断ロジックを持たない検査の追加なので敵対的レビューは省略
