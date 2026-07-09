# vim-toggle (vendored)

- 取得元: https://github.com/lukelbd/vim-toggle
- コミット: c1c0eaab76e82968d72bc03aa77974e754f6e2e8 (2025-02-03)
- vendored: 2026-07-09
- 理由: 小さな Vimscript プラグインで上流の更新が遅く、トグル語彙 (`g:toggle_words_on`/
  `g:toggle_words_off`) を自分専用に保守したいため repo 内に取り込む。挙動は上流と同一
  (速度目的ではない)。
- 読み込み: `_nviminit.lua` の lazy spec で
  `dir = config_dir .. "/vendor/nvim-plugins/vim-toggle"` として GitHub 取得でなく
  このローカルコピーを使う。lazy が `plugin/toggle.lua` を source し `lua/` を rtp に追加する。
- ライセンス: **GPL v2.0** (原 plugin/toggle.vim・autoload/toggle.vim のヘッダに明記。
  Author: Timo Teifel / Forked: Luke Davis)。移植後のファイルにも継承。

## ローカル改変 = Lua 移植 (**上流の Vimscript から fork**)

**2026-07-09: 上流 Vimscript を Lua へ全面移植した。** 元の `autoload/toggle.vim` / `plugin/toggle.vim`
は削除し、以下に置換:

- `lua/toggle.lua`   — 本体 (原 autoload/toggle.vim 相当。`require('toggle').toggle()`)
- `plugin/toggle.lua` — マッピング/`:Toggle`/既定リスト (原 plugin/toggle.vim 相当)

移植方針・検証:

- 文字列/正規表現/バッファ操作は **Vim builtin (`vim.fn.matchstrpos`/`expand`/`substitute`/`index`/
  `strcharpart`/`cursor` 等) をそのまま使用**し、制御フローのみ Lua 化 → 分岐リスクを最小化。
- Vim の `index()` は 0 始まりのため Lua アクセス時に `+1`。`=~#` (大小区別) は `\C` 付き
  `vim.fn.match` で再現。
- 移植時に前回のローカル改変 (`s:toggle_validate` を invocation ごと 1 回へ集約 / typo 修正) を取り込み済み。
- **原 Vimscript との A/B テストで完全一致を確認** (単語 大小/UPPER/Title・日本語マルチバイト・
  整数/小数/符号・連続文字・非マッチ・空白・行途中位置・コマンドrange・visual line、計 45 ケース差分ゼロ)。
  実 config でも `+`/`:Toggle`/ユーザー語彙 (if→unless 等) の動作を確認。

## 更新手順 (fork のため上流とは自動同期しない)

上流 lukelbd/vim-toggle に変更が入った場合、単純な recopy は不可。上流の差分を読み、
`lua/toggle.lua` / `plugin/toggle.lua` へ手で反映し、A/B (原 .vim vs 本 Lua) を取り直して
本ファイルのコミット/日付を更新する。

## 既知の未修正 (上流由来・意図的に触っていない)

- 範囲/ビジュアルで語長が変わる複数トークンをトグルすると、cursor 前進の offset ヒューリスティックが
  一部を飛ばすことがある (原版と同挙動)。単一カーソルの `+` トグルは正常。範囲アルゴリズムの
  作り替えは単発トグルを壊すリスクが高いため保留。
