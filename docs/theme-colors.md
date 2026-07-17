# テーマカラー変更ガイド — 色を変えたくなったら最初に読む

tmux (ステータスバー / pane 装飾) と nvim (colorscheme / bufferline / カスタム highlight) の
色は「意味 (role) → 定数」で管理している。**色を変えるときは使用箇所ではなく定数を触る**。

## 色の意味マップ (どの色が何を意味するか)

| 意味 | 色 | tmux 側 | nvim 側 |
|---|---|---|---|
| **現在地** (いまここ) | 蛍光オレンジ `#FF5F00`/202 (cterm 完全一致) | `@cur-accent` (current window 島) | `palette.accent.current_accent` (bufferline 選択タブの pill。両端キャップも同色) |
| 最近作業した (鮮度) | バイオレット ramp 201→164→127→90→53 (黄昏の残光) | `@fade-*` (放置フェード) | — (対応概念なし。持ち込まない) |
| 選択中テキスト | Kraft `#D4A27F`/180 | — | `palette.accent.kraft` (Visual)。現在地 (蛍光オレンジ) より一段落ち着けたのは意図的 (長時間注視するため)。truecolor/256色の両環境に適用 |
| 通知 (bell/メッセージ) | シアン 51 (稀なイベントの ping) | bell セル反転・message-style (alert 帯/: プロンプト)・copy-mode current match | — |
| マーカー (未保存) | 橙 208 | copy-mode mark 行 | `palette.bright_orange` (incline ● のみ) |
| 選択バー (bufferline) | — (pill 化で廃止 2026-07-17) | — | slant 系スタイルではインジケータバー自体が描画されない。選択の強調は pill のキャップ (現在地色) が担う。indicator_selected のクリーム light1 指定は非 slant へ戻したとき用に残置 (橙系は蛍光橙地 202 と近接して不可視になるため使わない、の判断ごと保存) |
| 点滅/scratch アイデンティティ | マゼンタ 201 | prefix/SCRATCH 点滅・scratch チップ/popup 枠 (fade hot 201 と紛れるなら 213 へ。ライブ判断) | — |
| 危険/警告状態 | 赤 160 (zoom) / 196 (sync) | `@zoom-accent` / pane-border sync | `palette.diag.error_bg` (診断は coc 踏襲の別系統) |
| アクティブ pane | 緑 46 (枠・ACTIVE 帯) / terminal 既定地 (=プロファイルの暖色) | pane-active-border / `window-active-style bg=terminal` | — (nvim は自前で地を塗る) |
| 地色 | 234 (pane/エディタ) / 235 (バー) | `window-style` bg / status-style bg | retrobox Normal bg=234 / `palette.dark0_hard` (bufferline fill)。**両ツールの地は 234 で揃っている**。bufferline の非選択タブ pill だけ一段浮かせた dark0 (235) = tmux バー地と同段 |

## 定数の出典 (単一ソース)

- **nvim**: [`nvim/lua/dotfiles/palette.lua`](../nvim/lua/dotfiles/palette.lua) — 全カスタム色の hex↔cterm 組。
  3 節構成 (gruvbox 基調 / accent=tmux 共有 / diag=coc 踏襲)。適用規律 (ColorScheme 再適用・
  cterm 併記) は [`hl.lua`](../nvim/lua/dotfiles/hl.lua) が一次情報
- **tmux**: `_tmux.conf` の `@fade-*` (フェード) / `@cur-accent`・`@zoom-accent` (島と zoom) /
  `@claude-state-fg`・`@claude-state-glyph` (Claude 状態)。フェードの設計は
  [`tmux-window-fade.md`](tmux-window-fade.md)
- **Terminal.app プロファイル (基調層)**: `mac/ClaudeWarm.terminal` — 地 #1F1E1D / 字 Manilla #EBDBBC /
  カーソル Coral #D97757。256色主環境で唯一フル RGB を持てる層で、tmux の `bg=terminal`・nvim の
  透過がこの色を継承する。**復元/既定化**: `scripts/terminal_profile_restore.sh` が登録 + 既定
  プロファイル化まで行う (手でプリセットを選ぶ操作を根絶。setup.sh が macOS で自動実行)。
  **変えるには**: Terminal.app 設定→プロファイルで調整し、書き出しでこの .terminal を上書きして
  restore スクリプトを再実行。**戻すには**: 旧既定プロファイルへ切り替えるだけ (ファイル削除は不要)

## ツール横断で対にして変える色

**現在地色 (蛍光オレンジ)** は tmux と nvim で同じ色を使う設計。変えるときは必ず両方:

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

window を切り替えると、current 島が **切替直前の表示色 → 暗く沈む → 蛍光へ点火、の V 字輝度
エンベロープ (~0.3 秒)** で遷移する (例: 残光 hot 201 → 164→127→90→53 (紫が沈む) →
52→88→130→166 (点火) → 202 / 消灯 16 → 52→88→130→166 → 202)。既に暗い起点 (消灯・残光の尾) は
沈む工程を自動スキップ。前の色→現在地色の直行色相スイープでなく一度沈める V 字なのは、人の注意が
色相差より輝度差に強く反応するため (直行案は全フレーム高輝度で「分かりにくい」実感により却下済み)。
意味論は放置フェードと対: 離れた window は紫の残光で冷め、入った window は沈んでから点火する。

- 仕組み: hook (`after-select-window[1]` / `client-session-changed`) が **fire 時に直前の表示色を
  展開して引数で渡し**、`scripts/tmux_ignite_current.sh` が 256色 cube 座標の線形補間で一時 option
  `@ignite` を最大 8 フレーム駆動 + `refresh-client -S`。描画は `@cur-live` (アニメ中=@ignite /
  平常時=@cur-accent) を参照するので、**テーマの色変更は従来どおり `@cur-accent` だけ触ればよい**
  (@cur-live は仕組み側。終点は @cur-accent を読むので色を変えても補間は自動追従)
- 調整ノブ: 速度 = スクリプト内 sleep 値 (現 35ms) / 形 = 沈むフレーム数 D・点火フレーム数 A (現 4/4 =
  256色 cube の量子化上限。これ以上は中間色が存在しない)。連打時は世代トークンで最後の切替だけ完走。
  算術は `TT_IGNITE_DRYRUN=1 scripts/tmux_ignite_current.sh colour201` で決定的に確認できる
- CPU: 各フレームは「世代一致なら set+refresh」を `tmux if -F` でサーバ側原子実行 (1 フレーム 1 fork)。
  実測 = 最長 8 フレームの 1 回で user+sys ~60ms。切替イベント時のみで常時負荷ゼロ
  (毎秒の status 再描画が常設で回っている構成なので、この瞬間バーストは誤差の範囲)
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

- フェードの grayscale 階調 — 「上品だが目に飛び込んでこない」
- window list の虹色分け (index%6) — 「実使用で見にくい」
- `#[blink]` 属性 — 消灯位相で読めない
- 現在地色のローズ / ショッキングピンク / Coral 系は検討済み (採用は蛍光オレンジ 202。
  候補比較の経緯: issues/done/017-feat-claude-code-orange-theme-2026-07-16.md)
- fade のシアンは「オレンジ基調から浮く」(ユーザー判断) で却下しバイオレットへ。シアンは
  bell/メッセージの通知専用に転用 (常在させず稀なイベントに使うことで「浮き」を強みに変えた)
