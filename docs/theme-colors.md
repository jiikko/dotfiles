# テーマカラー変更ガイド — 色を変えたくなったら最初に読む

tmux (ステータスバー / pane 装飾) と nvim (colorscheme / bufferline / カスタム highlight) の
色は「意味 (role) → 定数」で管理している。**色を変えるときは使用箇所ではなく定数を触る**。
2026-07-16 整備。

## 色の意味マップ (どの色が何を意味するか)

| 意味 | 色 | tmux 側 | nvim 側 |
|---|---|---|---|
| **現在地** (いまここ) | 蛍光オレンジ `#FF5F00`/202 (cterm 完全一致。変遷: ピンク199→Coral 173→蛍光 202、いずれも 2026-07-16) | `@cur-accent` (current window 島) | `palette.accent.current_accent` (bufferline 選択タブ。選択バーはクリーム light1=蛍光橙地で橙マーカーが消えるため) |
| 最近作業した (鮮度) | バイオレット ramp 201→164→127→90→53 (黄昏の残光。旧シアン 51→23) | `@fade-*` (放置フェード) | — (対応概念なし。持ち込まない) |
| 選択中テキスト | Kraft `#D4A27F`/180 (旧ローズ #d3869b) | — | `palette.accent.kraft` (Visual)。現在地 (蛍光オレンジ) より一段落ち着けたのは意図的 (長時間注視するため)。truecolor/256色の両環境に適用 |
| 通知 (bell/メッセージ) | シアン 51 (fade から転用。稀なイベントの ping 2026-07-16) | bell セル反転・message-style (alert 帯/: プロンプト)・copy-mode current match | — |
| マーカー (未保存) | 橙 208 | copy-mode mark 行 | `palette.bright_orange` (incline ● のみ) |
| 選択バー (bufferline) | クリーム light1 `#ebdbb2`/223 | — | `palette.light1` (indicator_selected。旧橙208は蛍光橙地202と d=40 でほぼ不可視のため変更 2026-07-16) |
| 点滅/scratch アイデンティティ | マゼンタ 201 | prefix/SCRATCH 点滅・scratch チップ/popup 枠 (fade hot 201 と紛れるなら 213 へ。ライブ判断) | — |
| 危険/警告状態 | 赤 160 (zoom) / 196 (sync) | `@zoom-accent` / pane-border sync | `palette.diag.error_bg` (診断は coc 踏襲の別系統) |
| アクティブ pane | 緑 46 (枠・ACTIVE 帯) / terminal 既定地 (=プロファイルの暖色。旧紺 17) | pane-active-border / `window-active-style bg=terminal` | — (nvim は自前で地を塗る) |
| 地色 | 234 (pane/エディタ) / 235 (バー) | `window-style` bg / status-style bg | retrobox Normal bg=234 / `palette.dark0_hard` (bufferline fill)。**両ツールの地は 234 で揃っている** |

## 定数の出典 (単一ソース)

- **nvim**: [`nvim/lua/dotfiles/palette.lua`](../nvim/lua/dotfiles/palette.lua) — 全カスタム色の hex↔cterm 組。
  3 節構成 (gruvbox 基調 / accent=tmux 共有 / diag=coc 踏襲)。適用規律 (ColorScheme 再適用・
  cterm 併記) は [`hl.lua`](../nvim/lua/dotfiles/hl.lua) が一次情報
- **tmux**: `_tmux.conf` の `@fade-*` (フェード) / `@cur-accent`・`@zoom-accent` (島と zoom) /
  `@claude-state-fg`・`@claude-state-glyph` (Claude 状態)。フェードの設計は
  [`tmux-window-fade.md`](tmux-window-fade.md)
- **Terminal.app プロファイル (基調層)**: `mac/ClaudeWarm.terminal` — 地 #1F1E1D / 字 Manilla #EBDBBC /
  カーソル Coral #D97757。256色主環境で唯一フル RGB を持てる層で、tmux の `bg=terminal`・nvim の
  透過がこの色を継承する。**変えるには**: Terminal.app 設定→プロファイルで調整し、書き出しで
  この .terminal を上書き (repo が復元手段)。**戻すには**: 適用前に控えた旧既定プロファイルへ
  切り替えるだけ (このファイルの削除は不要)

## ツール横断で対にして変える色

**現在地色 (蛍光オレンジ)** は tmux と nvim で同じ色を使う設計 (2026-07-16 統一)。変えるときは必ず両方:

1. `_tmux.conf` の `@cur-accent` (256色番号)
2. `nvim/lua/dotfiles/palette.lua` の `accent.current_accent` (hex + cterm の組)

片方だけ変えると「いまここ」の色言語が再び割れる。tmux/nvim は設定言語が別で定数を共有
できないため、この対応表が唯一のリンク (grep: `current_accent` / `@cur-accent`)。

## 変え方の手順

### tmux の色
1. 実行中の tmux に `tmux set -g @cur-accent colour209` のように打ってライブで見る
   (⚠️ 201 は fade hot・213 は点滅退避先の候補なので、試し色には使わないのが無難)
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

## 点火アニメ (window 切替の「ぬるっと」遷移)

window を切り替えると、current 島が **暗赤 52→94→130→166→蛍光 202 へ ~0.2 秒でランプ**する
(意味論は放置フェードと対: 離れた window は紫の残光で冷め、入った window は点火する)。

- 仕組み: hook (`after-select-window[1]` / `client-session-changed`) →
  `scripts/tmux_ignite_current.sh` が一時 option `@ignite` を 4 フレーム駆動 + `refresh-client -S`。
  描画は `@cur-live` (アニメ中=@ignite / 平常時=@cur-accent) を参照するので、
  **テーマの色変更は従来どおり `@cur-accent` だけ触ればよい** (@cur-live は仕組み側)
- 調整ノブ: 軌跡 = スクリプト内の色列 / 速度 = sleep 値。連打時は世代トークンで最後の切替だけ完走
- 文字単位のスイープ (一文字ずつ色が変わる) は不可: format に部分文字列×スタイル分割の手段が無く、
  幅可変の reveal 方式は「セル幅が変わると列がずれる」で却下済み (チップ案と同根)

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
- 選択タブの現在地色は ローズ→ショッキングピンク→Coral→蛍光オレンジ 202 と変遷 (いずれも 2026-07-16)。
  戻す/変えるときは `accent.current_accent` と tmux `@cur-accent` の対で (経緯: issues/claude-code-orange-theme-2026-07-16.md)
- fade のシアンはオレンジ基調テーマで「基調から浮く」(ユーザー判断) となりバイオレットへ変更 (2026-07-16)。
  シアンは bell/メッセージの通知色へ転用 (常在させず稀なイベントに使うことで「浮き」を強みに変えた)
