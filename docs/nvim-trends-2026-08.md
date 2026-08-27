# Neovim の最近のトレンド (2026-08 時点の調査)

2025 後半〜2026 前半に Neovim 本体とプラグイン生態系で起きた動きと、この dotfiles の nvim 設定
(`_nviminit.lua` / `nvim/lua/dotfiles/`) がどこに立っているかをまとめる。プラグイン単体の棚卸しは
[`nvim-plugins.md`](nvim-plugins.md) が担当で、こちらは「流れ」を書く。手元の実測 (`nvim --version`
= 0.11.5、`brew info neovim` = stable 0.12.4、`tree-sitter --version` = 0.26.9) は 2026-08-27。

## 要旨

- **本体に取り込まれる方向が続いている。** 0.11 で LSP 設定 (`vim.lsp.config` / `vim.lsp.enable`) と
  補完 (`vim.lsp.completion`) が、0.12 (2026-03-29) で **プラグインマネージャ `vim.pack`** と
  `:lsp` / `:Undotree` / `:DiffTool` / `vim.net.request()` が入った。「lspconfig・lazy.nvim・
  undotree 系プラグインが担っていたもの」が順に標準機能になっている
- **プラグインの世代交代は "folke / echasnovski 製に集約" と "軽量・低レベル化" の 2 方向。**
  補完は nvim-cmp → blink.cmp、finder は telescope → snacks.picker / fzf-lua、ファイラは
  nvim-tree → oil / mini.files / snacks.explorer、treesitter は master → main (機能を削って
  低レベル化)。ただし dotfyle の実インストール数ではまだ telescope / nvim-cmp / lazy.nvim が上位で、
  **「新規設定で選ばれるもの」と「動いている設定に入っているもの」が乖離している**時期
- **AI は "エディタ内チャット" から "エージェントをプロトコルで繋ぐ" へ。** Zed 発の
  Agent Client Protocol (ACP、JSON-RPC 2.0、2026-03 に v0.11.0) を avante.nvim / CodeCompanion が
  実装し、Claude Code / Codex / Gemini CLI を同じ口で呼ぶ形が主流になりつつある
- **この dotfiles は「本体に寄せる」側に既に立っている** (native LSP + `vim.lsp.config`、blink.cmp、
  treesitter main)。残る判断は 3 つ: nvim 0.12 への更新 (treesitter main が 0.12 必須になった)、
  lazy.nvim → `vim.pack`、telescope / nvim-tree の後継

## 1. Neovim 本体

| 版 | 日付 | 入ったもの | 設定への影響 |
|---|---|---|---|
| 0.11 | 2025-03 | `vim.lsp.config()` / `vim.lsp.enable()` (lspconfig 無しで LSP を定義・有効化。`after/lsp/<name>.lua` 配置)、`vim.lsp.completion.enable()` (標準の自動補完)、既定キーマップ (`grn` / `gra` / `grr` / `K` 等) | lspconfig は「設定データの供給元」に役割が縮む。`require("lspconfig").X.setup{}` は旧 API |
| 0.12 | 2026-03-29 | **`vim.pack`** (`vim.pack.add` / `update` / `del`、lockfile `nvim-pack-lock.json`)、`:lsp` サブコマンド (`:LspInfo` / `:LspRestart` / `:LspLog` は廃止)、`:restart`、`ui2` (「Press ENTER」の排除。experimental)、`:Undotree` / `:DiffTool` (`packadd nvim.undotree` 等で有効化)、`vim.net.request()`、`vim.list.unique` / `bisect`、`nvim_set_hl` の `update`、LSP の `inlineCompletion` / `linkedEditingRange` / `onTypeFormatting`、標準 statusline が式化され `vim.diagnostic.status()` を表示、端末の synchronized output (DEC 2026) | 破壊的変更: `i_CTRL-R` がレジスタを literal 挿入、`sign_define` による診断サインは無効、`vim.diagnostic.disable/is_disabled` 削除、`vim.diff` → `vim.text.diff`、LSP の JSON null が `vim.NIL`、treesitter `Query:iter_matches` の `all` オプション削除 |

**注意 (このリポジトリ固有)**: `_nviminit.lua` の treesitter 節が書いているとおり、nvim-treesitter
`main` は 0.12 の treesitter API 変更を前提にしている。upstream README の Requirements は
**「Neovim 0.12.0 or later」「tree-sitter CLI 0.26.1 以上 (npm ではなくパッケージマネージャで)」**。
手元は nvim 0.11.5 のまま lock (`3d3321b`) で止めているので動いているが、`:Lazy update` で
nvim-treesitter だけ進めると 0.11 では動かなくなる可能性が高い。**nvim を 0.12 に上げるのと
nvim-treesitter を進めるのは同じタイミングでやる** (brew の stable は 0.12.4 で、上げる材料は揃っている)。

## 2. プラグイン生態系

### 2.1 補完: nvim-cmp → blink.cmp

nvim-cmp のメンテナは活動を絞り、blink.cmp (Rust の fuzzy matcher をプリビルドで同梱) が
新規設定の既定になった。0.12 は `vim.lsp.completion` に順序制御 (`cmp` オプション) と inline
completion を足したが、スニペット・複数ソース・ドキュメント表示を含めた体験ではまだ blink が上。
**この dotfiles は blink.cmp 済み** (`version = "*"` でプリビルド固定)。

### 2.2 finder: telescope → snacks.picker / fzf-lua

folke が LazyVim の既定を telescope → fzf-lua → snacks.picker と動かしたことで新規設定は
snacks.picker に流れている (frecency・プレビュー・`vim.ui.select` 置換を 1 つで持つ)。
telescope は保守されており、拡張資産 (`telescope-ui-select` 等) が多い人はそのまま使える。
**この dotfiles は telescope + telescope-ui-select**。乗り換えの動機は「大規模 repo で遅い」を
体感したときで、それまでは据え置きで良い。乗り換えるなら snacks.picker は `vim.ui.select` も
兼ねるので `telescope-ui-select.nvim` が 1 つ減る。

### 2.3 ファイラ: nvim-tree → oil.nvim / mini.files / snacks.explorer

ツリー型 (nvim-tree / neo-tree) から「ディレクトリをバッファとして編集する」型 (oil / mini.files)
へ好みが動いた。**この dotfiles は nvim-tree**。ツリーを常時表示する使い方なら乗り換え理由は薄い。

### 2.4 プラグインマネージャ: lazy.nvim → vim.pack

`vim.pack` は mini.deps を原型に本体へ入ったもの (作者 echasnovski 本人の解説)。lazy.nvim との差:

| | vim.pack | lazy.nvim |
|---|---|---|
| 遅延ロード | 自前の autocmd で組む (`event` / `cmd` / `ft` の宣言的構文は無い) | 宣言的 |
| フック | `PackChangedPre` / `PackChanged` autocmd | spec の `config` / `build` |
| lockfile | `nvim-pack-lock.json` (config dir) | `lazy-lock.json` |
| 起動速度 | `vim.loader.enable()` + 適度な遅延で lazy.nvim 並み | 既定で速い |
| リリース | Neovim 本体の周期 | プラグインの周期 |

echasnovski の推奨は「mini.deps 利用者は移行、lazy.nvim 利用者は `vim.pack.add()` 1 呼び出しの
素朴な設定を試して起動が許容できるなら移行」。**この dotfiles は lazy.nvim で、`event` /
`keys` による遅延ロードと `_lazy-lock.json` の repo 管理 (setup.sh が symlink) に依存している**。
移行するなら (1) 0.12 化 (2) lockfile の置き場を `_lazy-lock.json` から `nvim-pack-lock.json`
へ (setup.sh と `tests/nvim` の追随) (3) 遅延ロードを autocmd に書き直す、の 3 段で、
それぞれ独立に価値があるわけではないので **0.12 化が済んだ後に「lazy.nvim を捨てる動機が出たら」**
で良い。`docs/nvim-plugin-load-tracker.md` の計測が移行前後の比較に使える。

### 2.5 treesitter: master → main (2025-05 に master 凍結)

完全な非互換の書き直し。`configs.setup{ensure_installed, highlight={enable=true}}` は無くなり、
`require("nvim-treesitter").install({...})` と `vim.treesitter.start()` を自分で呼ぶ。parser は
ローカルでコンパイルするため tree-sitter CLI が必須。textobjects は別プラグイン
(`nvim-treesitter-textobjects` main)、incremental_selection 等のモジュールは削除
(`treesitter-modules.nvim` が互換層)。lazy-loading は非対応と明言。
**この dotfiles は main に移行済み** (`_nviminit.lua` の treesitter 節に経緯と罠が書いてある)。
残りは上記の 0.12 要件だけ。

### 2.6 LSP 周辺: lspconfig は「設定データ」になった

nvim-lspconfig は `lsp/<name>.lua` の設定を供給する役に縮み、有効化は `vim.lsp.enable()`。
mason は「バイナリの導入」に専念。**この dotfiles は `nvim/lua/dotfiles/lsp.lua` が
`vim.lsp.config("*", …)` → `vim.lsp.config(name, cfg)` → `vim.lsp.enable` の順で組んでおり、
0.11 の作法に載っている**。0.12 で `:LspInfo` 等が消えるので、使っていれば `:lsp` へ
(コマンドの復元スニペットは jdhao の記事にある)。

### 2.7 QoL の集約: snacks.nvim / mini.nvim

folke の snacks.nvim (picker / explorer / notifier / dashboard / indent / scroll …) と
echasnovski の mini.nvim が「小さなプラグインの寄せ集め」を 1 依存に畳む受け皿になっている。
この dotfiles では `mini.trailspace` だけを使っており、`indent-blankline` (→ `snacks.indent` /
`mini.indentscope`)、`nvim-notify` + `noice` (→ `snacks.notifier`)、`nvim-scrollview` は畳める候補。
ただし依存を 1 つに寄せると「その 1 つの更新で全部が動く」ので、集約は速度や保守の実害が出てから。

### 2.8 AI: エディタ内チャットから ACP へ

avante.nvim (Cursor 風の sidebar + diff) と CodeCompanion.nvim (バッファ統合型) の 2 強。
両方が **Agent Client Protocol** (Zed 発。Claude Code は Zed 製のブリッジ経由で ACP 化) を実装し、
Claude Code / Codex / Gemini CLI / OpenCode をエディタから同じ口で呼ぶ。agentic.nvim のような
ACP 専用クライアントも出ている。**この dotfiles は nvim 内に AI プラグインを持たず、Claude Code は
tmux の別 pane / popup で動かす設計** (`docs/tmux-as-platform.md`、`bin/glogx` 連携)。
ACP でエディタ内に取り込む価値は「選択範囲を渡して diff を当てる」往復が多いかで決まる。
tmux 側の連携が既にあるので、今すぐ足す理由は無い。

## 3. この dotfiles の立ち位置 (判断が要るもの)

| 項目 | 現状 | トレンド | 判断 |
|---|---|---|---|
| Neovim | 0.11.5 (brew stable は 0.12.4) | 0.12 | **上げる**。treesitter main の要件が 0.12 になっており、lock を進められない状態。0.12 の破壊的変更 (`sign_define` 系の診断サイン・`vim.diff`・`vim.diagnostic.disable`・`:LspInfo` 系) は `_nviminit.lua` / `nvim/lua/dotfiles/` / `nvim/ftplugin/` を grep して該当なし (2026-08-27) |
| LSP 設定 | `vim.lsp.config` / `enable` | 同じ | 済 |
| 補完 | blink.cmp | 同じ | 済 |
| treesitter | main (lock 固定) | 同じ | 済。0.12 化と同時に `:Lazy update` + `:TSUpdate` |
| plugin manager | lazy.nvim | vim.pack | 据え置き。0.12 化後に動機が出たら (遅延ロードと lockfile 運用の書き直しが要る) |
| finder | telescope | snacks.picker / fzf-lua | 据え置き。遅さを体感したら |
| ファイラ | nvim-tree | oil / mini.files | 据え置き (ツリー常時表示の使い方なら) |
| AI | なし (tmux 側で Claude Code) | ACP (avante / CodeCompanion) | 据え置き |

## 出典

- [News-0.12 — Neovim docs](https://neovim.io/doc/user/news-0.12/) / [What's New in Neovim 0.12](https://dotfiles.substack.com/p/whats-new-in-neovim-012) / [I read the nvim v0.12 release note so you don't have to (jdhao)](https://jdhao.github.io/2026/04/02/nvim-v012-release/)
- [A Guide to vim.pack (echasnovski)](https://echasnovski.com/blog/2026-03-13-a-guide-to-vim-pack)
- [nvim-lspconfig README](https://github.com/neovim/nvim-lspconfig) / [Neovim LSP 0.11 (Dave Lage)](https://davelage.com/posts/neovim-lsp-0.11/)
- [nvim-treesitter: How to migrate from master to main (Discussion #7927)](https://github.com/nvim-treesitter/nvim-treesitter/discussions/7927) / [nvim-treesitter README (main)](https://github.com/nvim-treesitter/nvim-treesitter)
- [dotfyle: Trending Neovim Plugins](https://dotfyle.com/neovim/plugins/trending) (実インストール数ベース。2026-08-27 閲覧)
- [Why I'm Moving from Telescope to Snacks Picker (linkarzu)](https://linkarzu.com/posts/neovim/snacks-picker/)
- [blink.cmp](https://github.com/saghen/blink.cmp)
- [avante.nvim vs CodeCompanion (Samuel Lawrentz)](https://samuellawrentz.com/blog/neovim-ai-plugins-avante-codecompanion/) / [CodeCompanion.nvim](https://codecompanion.olimorris.dev/) / [avante.nvim](https://github.com/avante-corp/avante.nvim) / [agentic.nvim](https://github.com/carlos-algms/agentic.nvim)
- [Agent Client Protocol explained (Morph)](https://www.morphllm.com/agent-client-protocol) / [Neovim — ACP Client (Zed)](https://zed.dev/acp/editor/neovim)
