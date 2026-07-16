# テーマカラー変更ガイド — 色を変えたくなったら最初に読む

tmux (ステータスバー / pane 装飾) と nvim (colorscheme / bufferline / カスタム highlight) の
色は「意味 (role) → 定数」で管理している。**色を変えるときは使用箇所ではなく定数を触る**。
2026-07-16 整備。

## 色の意味マップ (どの色が何を意味するか)

| 意味 | 色 | tmux 側 | nvim 側 |
|---|---|---|---|
| **現在地** (いまここ) | ショッキングピンク #ff00af/199 | `@cur-accent` (current window 島) | `palette.accent.current_pink` (bufferline 選択タブ) |
| 最近作業した (鮮度) | シアン ramp 51→23 | `@fade-*` (放置フェード) | — (対応概念なし。持ち込まない) |
| 選択中テキスト | ローズ #d3869b/175 | — | `palette.bright_purple` (Visual)。現在地より一段落ち着けたのは意図的 (長時間注視するため)。⚠️ truecolor (gruvbox) 分岐のみ。256色主環境の retrobox では既定の青灰 (ctermbg=109) のまま |
| 通知/注意 | 橙 208 | bell セル反転 (window list) | `palette.bright_orange` (bufferline indicator・incline ●) |
| 一時メッセージ/点滅 | マゼンタ 201 | message-style (alert-bell 帯・: プロンプト)・prefix/SCRATCH 点滅・copy-mode current match | — |
| 危険/警告状態 | 赤 160 (zoom) / 196 (sync) | `@zoom-accent` / pane-border sync | `palette.diag.error_bg` (診断は coc 踏襲の別系統) |
| アクティブ pane | 緑 46 (枠・ACTIVE 帯) / 紺 17 (素シェルの地) | pane-active-border / `window-active-style` | — (nvim は自前で地を塗るため紺の影響を受けない。意図は _tmux.conf の window-style コメント) |
| 地色 | 234 (pane/エディタ) / 235 (バー) | `window-style` bg / status-style bg | retrobox Normal bg=234 / `palette.dark0_hard` (bufferline fill)。**両ツールの地は 234 で揃っている** |

## 定数の出典 (単一ソース)

- **nvim**: [`nvim/lua/dotfiles/palette.lua`](../nvim/lua/dotfiles/palette.lua) — 全カスタム色の hex↔cterm 組。
  3 節構成 (gruvbox 基調 / accent=tmux 共有 / diag=coc 踏襲)。適用規律 (ColorScheme 再適用・
  cterm 併記) は [`hl.lua`](../nvim/lua/dotfiles/hl.lua) が一次情報
- **tmux**: `_tmux.conf` の `@fade-*` (フェード) / `@cur-accent`・`@zoom-accent` (島と zoom) /
  `@claude-state-fg`・`@claude-state-glyph` (Claude 状態)。フェードの設計は
  [`tmux-window-fade.md`](tmux-window-fade.md)

## ツール横断で対にして変える色

**現在地ピンク**は tmux と nvim で同じ色を使う設計 (2026-07-16 統一)。変えるときは必ず両方:

1. `_tmux.conf` の `@cur-accent` (256色番号)
2. `nvim/lua/dotfiles/palette.lua` の `accent.current_pink` (hex + cterm の組)

片方だけ変えると「いまここ」の色言語が再び割れる。tmux/nvim は設定言語が別で定数を共有
できないため、この対応表が唯一のリンク (grep: `current_pink` / `@cur-accent`)。

## 変え方の手順

### tmux の色
1. 実行中の tmux に `tmux set -g @cur-accent colour201` のように打ってライブで見る
2. 気に入ったら `_tmux.conf` の定数へ書き戻す (フェード定数と同じ流儀)
3. `tests/tmux/test_tmux.sh` で format 展開のリーク検査

### nvim の色
1. `palette.lua` の該当エントリを変える (hex と cterm は**必ず組で**。cterm 近似の確認は
   `:lua vim.cmd('hi TestX ctermbg=NNN')` などでライブ確認可)
2. 検証: 変更前後で highlight group を dump して意図した差分だけか見る
   ```sh
   nvim --headless -u _nviminit.lua "+lua vim.wait(500)" \
     "+lua vim.cmd('Lazy! load all'); vim.wait(500); local h=vim.api.nvim_get_hl(0,{name='BufferLineBufferSelected',link=false}); print(vim.inspect(h))" +qall
   ```
3. `tests/nvim/test_nvim.sh` (config ロード + 全プラグイン強制ロード)

## 制約 (なぜこうなっているか)

- **主環境は 256色** (~/.zshenv の SUPPORT_TRUECOLOR=false → termguicolors=off。_nviminit.lua
  冒頭の WORKAROUND 参照)。**hex が設計の真・cterm はその忠実な近似**。新しい色は必ず
  {hex, cterm} の組で定義する (gui だけだと 256色で無言に効かない。hl.set が WARN で検知)
- **colorscheme を替える場合**: 256色主環境では cterm 完備のスキームが必須。gruvbox.nvim は
  gui 色のみのため、非 truecolor では nvim 同梱 retrobox に分岐している (truecolor-only の
  テーマ (tokyonight 等) へ全面移行するには主環境の truecolor 化が先)
- **tmux の連続グラデ**: format 算術で 16 進が組めないため truecolor グラデは不可。256色
  cube/grayscale の離散 ramp で近似する (フェードの実装参照)

## 却下済み (再提案は棄却してよい)

- フェードの grayscale 階調 — 「上品だが目に飛び込んでこない」(2026-07-04)
- window list の虹色分け (index%6) — 「実使用で見にくい」(2026-07-02)
- `#[blink]` 属性 — 消灯位相で読めない (2026-07-03)
- 選択タブのローズ→ショッキングピンク統一は 2026-07-16 採用。戻したくなったら
  `accent.current_pink` を `bright_purple` の値に戻すだけ (1 箇所)
