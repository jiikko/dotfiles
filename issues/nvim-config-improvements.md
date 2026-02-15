# nvim 設定改善

調査日: 2026-02-14
調査モード: Forge Minimum+（Explore, architecture-reviewer, research-assistant）

## 🔴 High Priority

### 1. `lazyredraw = true` が noice.nvim と競合
- **ファイル**: `nvim/lua/dotfiles/basic.lua:49`
- **内容**: noice.nvim 使用時に `lazyredraw = true` は RAM リークを引き起こす。noice.nvim の FAQ でも明記されている
- **対応**: 該当行を削除

### 2. foldexpr が非推奨 API を使用
- **ファイル**: `_nviminit.lua:742`
- **内容**: `nvim_treesitter#foldexpr()` は非推奨。Neovim 本体に組み込まれた新 API を使うべき
- **対応**: `v:lua.vim.treesitter.foldexpr()` に変更

### ~~3. flash.nvim の opts 重複（デッドコード）~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: `opts = {}` の重複行を削除

### 4. bufferline diagnostics 設定ミス
- **ファイル**: `_nviminit.lua:335`
- **内容**: `diagnostics = "nvim_lsp"` だが、LSP クライアントは coc.nvim を使用している
- **対応**: `diagnostics = "coc"` に変更

## 🟡 Medium Priority

### ~~5. `updatetime`/`signcolumn` が coc 設定セクション内に配置~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: `nvim/lua/dotfiles/basic.lua` の set_options() へ移動

### 6. CursorHold + timer_start による二重遅延
- **ファイル**: `_nviminit.lua:214-221`
- **内容**: CursorHold（300ms）+ timer_start(500) で合計800msの遅延。ハイライト表示が遅い
- **対応**: timer_start を削除し CursorHold だけで直接実行

### ~~7. filepath コピーキーマップが coc 設定内に配置~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: basic.lua の set_keymaps() へ移動

### ~~8. `*`/`#` キーマップが coc 設定内に配置~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: basic.lua の set_keymaps() へ移動

### 9. `showmode = true` が noice.nvim で無効
- **ファイル**: `nvim/lua/dotfiles/basic.lua:41`
- **内容**: noice.nvim がモード表示を管理するため `showmode` は効果がない
- **対応**: `showmode = false` に変更

### ~~10. `_G.show_documentation` によるグローバル汚染~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: local 関数化し、vim.keymap.set で直接参照

### ~~11. `vim.fn.eval()` 非慣用パターン~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: `vim.fn['coc#rpc#ready']()` に変更

## 🟢 Low Priority

### ~~12. `t_vb = ""` デッドコード~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: Neovim に存在しない設定の pcall を削除

### 13. netrw 無効化漏れ
- **ファイル**: `_nviminit.lua`
- **内容**: `loaded_netrw = 1` はあるが `loaded_netrwPlugin = 1` がない
- **対応**: `vim.g.loaded_netrwPlugin = 1` を追加

### ~~14. ftplugin JS/TS 4ファイル重複~~ 対応済み
- **対応日**: 2026-02-14
- **内容**: `_js_ts_common.lua` に共通化、各ファイルから require

### 15. disabled_plugins に追加候補
- **ファイル**: `_nviminit.lua`
- **内容**: `tutor`, `zipPlugin`, `zip`, `gzip`, `tarPlugin`, `tar` 等が無効化されていない
- **対応**: lazy.nvim の `performance.rtp.disabled_plugins` に追加

## ⚪ 対応不要

| 項目 | 理由 |
|------|------|
| `vim.loader.enable()` 追加 | lazy.nvim が自動呼出し済み |
| _nviminit.lua ファイル分割 | 現状753行で許容範囲 |
| coc.nvim → native LSP 移行 | 大規模変更のためスコープ外 |
| copilot.vim → copilot.lua 移行 | 現状で問題なし |
