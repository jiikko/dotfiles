# Neovim プラグイン棚卸し

現在 `~/.config/nvim/init.lua` で利用しているプラグインと、そのカテゴリごとのデファクトスタンダード候補、乗り換え可否をまとめた。

## 読み方

- **現行**: いま使っているプラグイン。
- **主な役割**: 何を解決しているか。
- **最終更新**: ローカルの lazy ダウンロードにおける直近コミット日。
- **デファクト**: いまよく使われている、もしくは後継として認知されている選択肢。
- **乗り換え可否**: 互換性や設定量を加味した主観。○=無難、△=少し工数あり、×=ハード or そのままで良い。
- **今すぐ捨てる?**: 即座に削除/置き換えを検討すべきか。○=すぐ捨てたい、△=検討余地あり、×=現状維持でOK。

## UI / 操作性

| 現行 | 主な役割 | 最終更新 | デファクト候補 | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| ---- | -------- | -------- | -------------- | ------------ | ------------- | -------------- | ---- |
| ✅ ellisonleao/gruvbox.nvim | カラースキーム | 2025-09-04 | 同プラグイン, `folke/tokyonight.nvim` | × | × | いいえ | Vimscript 版から移行済み。Lua 版で truecolor/透明度設定が容易。 |
| ✅ folke/which-key.nvim | キーマップチートシート | 2025-10-28 | 同プラグインがデファクト | × | × | いいえ | 定番。置き換え不要。 |
| ✅ nvim-lualine/lualine.nvim | ステータスライン | 2025-11-23 | 同プラグイン | × | × | いいえ | Lightline から移行済み。診断表示はネイティブ LSP (vim.diagnostic) の `diagnostics` コンポーネントを統合。 |
| ✅ akinsho/bufferline.nvim | タブライン | 2025-01-14 | `romgrk/barbar.nvim`, 標準タブライン | × | × | いいえ | Barbar から移行済み。LSP/診断アイコンも扱える Lua 実装で軽量。 |
| ✅ nvim-tree/nvim-tree.lua | ファイラ | 2026-01-11 | `nvim-neo-tree/neo-tree.nvim`, `stevearc/oil.nvim` | △ | △ | いいえ | Oil は軽量、Neo-tree は機能豊富。好み。 |
| ✅ nvim-tree/nvim-web-devicons | アイコン | 2026-01-11 | 同プラグイン | × | × | いいえ | 事実上の標準。 |
| ✅ lukas-reineke/indent-blankline.nvim | インデントガイド | 2025-03-18 | `echasnovski/mini.indentscope`, `glepnir/indent-guides.nvim` | ○ | △ | いいえ | mini/indent-guides は軽量。 |
| ✅ rcarriga/nvim-notify | 通知 UI | 2025-09-06 | 同プラグイン | × | × | いいえ | 競合が少なく現状維持。 |
| ✅ dstein64/nvim-scrollview | スクロールバー | 2025-10-01 | `petertriho/nvim-scrollbar`, `scrollbar.nvim` | △ | × | いいえ | 仮想テキストのみで軽量。Neoscroll とも競合しにくい。 |
| ~~karb94/neoscroll.nvim~~ | スクロールアニメ | — | — | — | — | — | **✅ 削除済み**(2026-07-12)。キーリピート時もアニメし割り込み連鎖でカーソルが乱れた (A/B で確認)。既製アニメ系は「リピート中は素通し」分岐を持たないため `nvim/lua/dotfiles/smooth_scroll.lua` を自作 (単発=アニメ / 押しっぱなし=素通し)。 |
| ✅ b0o/incline.nvim | 浮動ファイル名表示 | 2025-12-17 | `akinsho/bufferline.nvim` のみでも可 | △ | × | いいえ | 分割ウィンドウで各ウィンドウにファイル名表示。未保存/親ディレクトリ表示など実装。 |
| ✅ TaDaa/vimade | 非アクティブフェード | 2025-11-09 | `levouh/tint.nvim`, `folke/twilight.nvim` | △ | × | いいえ | 非アクティブウィンドウを暗くして集中力向上。アニメーション対応。 |
| ✅ folke/noice.nvim | UI リッチ化 | 2025-11-03 | 同プラグイン | × | × | いいえ | コマンドライン・検索・LSPメッセージをリッチ化。nvim-notify連携。 |
| ~~mvllow/modes.nvim~~ | モード別カーソル色 | — | — | — | — | — | **✅ 削除済み**(2026-07-10)。この config では truecolor 端末のカーソル色のみに価値が限定される（cursorline/number/signcolumn は 256色運用で off）ため廃止。詳細: `issues/nvim-plugin-rewrite-candidates-2026-07-10.md`。 |

## ナビゲーション / 検索

| 現行                             | 主な役割           | 最終更新                | デファクト候補                                      | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ                                      |
| -------------------------------- | ------------------ | ----------------------- | --------------------------------------------------- | ------------ | ------------- | --------------- | ----------------------------------------- |
| ✅ nvim-telescope/telescope.nvim | ファジーファインダ | 2026-01-11              | `ibhagwan/fzf-lua`, `junegunn/fzf.vim`              | △            | △             | いいえ          | Telescope は機能性◎。軽量化なら fzf-lua。依存は telescope-ui-select（ネイティブ LSP 移行で telescope-coc は撤去、ジャンプは builtin `lsp_*` を使用）。`path_display=filename_first`・insert の `<esc>` 即クローズを設定。 |
| ✅ rbtnn/vim-ambiwidth (**vendored, Lua移植**) | 全角幅調整 | 2025-08-02          | 代替少                                              | ×            | ×             | いいえ           | 日本語環境で必要なため `vendor/nvim-plugins/ambiwidth.nvim` に取り込み **Lua 移植** (lazy `dir`)。setcellwidths の幅テーブルを Lua 化し、原 .vim/generator(3800行)/list.txt(4200行) は削除。原版との getcellwidths A/B で一致確認 (VENDOR.md 参照)。 |
| ✅ lewis6991/gitsigns.nvim       | Git ハイライト     | 2026-01-09              | 同プラグイン                                         | ×            | ×             | いいえ          | 差分/ブレーム/ステージングまで一括管理できる Lua 実装。 |

## テキスト編集支援

| 現行                                         | 主な役割        | 最終更新   | デファクト候補                                       | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| -------------------------------------------- | --------------- | ---------- | ---------------------------------------------------- | ------------ | ------------- | --------------- | ---------------------------------------------------- |
| ✅ lukelbd/vim-toggle (**vendored, Lua移植**) | トグル          | 2025-02-03 | `echasnovski/mini.operators`, `folke/which-key`      | △ | △ | いいえ | 辞書が充実しており今も実用。上流の更新が遅く語彙を自分で保守するため `vendor/nvim-plugins/toggle.nvim` に取り込み (原名 vim-toggle から rename) **Lua 移植済み** (2026-07-09)。lazy の `dir` でローカル読み込み (VENDOR.md 参照)。 |
| ✅ andymass/vim-matchup                      | 括弧/タグマッチ | 2025-12-31 | 同プラグイン                                         | × | × | はい | 本体は Vimscript 6,848 行（`lua/` は treesitter 連携のみ ~1,210 行）。Treesitter 対応でデファクト。Lua 化は最難・低ROI で対象外（`issues/nvim-vimscript-to-lua-migration.md` §4 参照）。 |
| ✅ nvim-treesitter/\*                        | 構文解析        | 2026-01-10 | 同プロジェクト                                       | × | × | いいえ | 乗り換え不要。 |
| ✅ RRethy/nvim-treesitter-endwise            | end 自動補完    | 2025-12-29 | 同プラグイン, `tpope/vim-endwise`                    | × | × | いいえ | Ruby/Lua/Vimscript 等で `if`〜`end` を自動補完。Treesitter ベースで構文解析に追従。 |
| ✅ echasnovski/mini.trailspace               | 末尾空白可視    | 2025-11-03 | `echasnovski/mini.trailspace`, `axieax/typo.nvim`    | × | × | いいえ | 末尾空白削除を mini.trailspace で実装。`mini.nvim` 全体ではなく単体リポジトリのみ導入。 |
| ✅ MeanderingProgrammer/render-markdown.nvim | Markdown レンダリング | 2026-01-07 | `OXY2DEV/markview.nvim`, `ellisonleao/glow.nvim` | × | × | いいえ | Treesitter ベースで見出し/リスト/コードブロックを装飾。軽量でシンプル。 |

## 言語・LSP

| 現行                      | 主な役割     | 最終更新   | デファクト候補                                           | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| ------------------------- | ------------ | ---------- | -------------------------------------------------------- | ------------ | ------------- | --------------- | ---------------------------------------------------- |
| ~~tpope/vim-rails~~       | Rails 支援   | — | — | — | — | — | **✅ 削除済み**(2026-07-10)。def ジャンプは native LSP (solargraph) が代替、minitest では `:A` が効きにくく不使用だったため廃止。 |
| ✅ terraform-ls + treesitter + conform | Terraform | - | 同構成 | × | × | いいえ | **2026-07: hashivim/vim-terraform (Vimscript) を置換**。ft は nvim 標準検出、構文/fold は treesitter(terraform/hcl)、補完/診断は terraform-ls、整形は conform terraform_fmt を terraform ft の保存時に発火 (旧 terraform_fmt_on_save 相当)。 |
| ✅ gopls + treesitter + conform + treesitter-textobjects | Go | - | 同構成 | × | × | いいえ | **2026-07: fatih/vim-go (Vimscript 19k 行) を置換**。ハイライトは treesitter(go/gomod/gosum)、定義/実装/参照/hover は gopls (native LSP)、整形は conform goimports を go 保存時に発火 (旧 go_fmt_autosave/go_imports_autosave 相当)、関数ジャンプ `]]`/`[[` とテキストオブジェクト `af`/`if`/`ac`/`ic` は treesitter-textobjects、GoDecls は telescope symbols で置換 (Go 限定は nvim/ftplugin/go.lua)。`K`=hover は nvim 0.11 native 既定。 |
| ~~github/copilot.vim~~    | AI 補完      | — | — | — | — | — | **✅ 削除済み**(2026-07。残骸掃除まで完了)。AI 支援は sidekick.nvim + Claude Code に集約。 |
| ✅ neovim/nvim-lspconfig  | LSP サーバ設定 | - | 同プロジェクト | × | × | いいえ | 2026-07 に coc.nvim から移行。nvim 0.11 の `vim.lsp.config`/`vim.lsp.enable` に載る。 |
| ✅ mason-org/mason.nvim | LSP/ツール管理 | - | 同プロジェクト | × | × | いいえ | サーバ/formatter/linter のバイナリ管理。mason-lspconfig は廃止 (2026-07-11、初回 BufReadPre ~13ms 削減)。enable は vim.lsp.enable() 直呼び、導入は mason-tool-installer に一本化。 |
| ✅ saghen/blink.cmp       | 補完         | - | `hrsh7th/nvim-cmp` | × | × | いいえ | coc の補完後継。`version="*"` でプリビルドバイナリ（cargo 不要）。`<CR>` 確定。 |
| ✅ stevearc/conform.nvim  | 整形         | - | 同プラグイン | × | × | いいえ | `:Format`/`<leader>f`。prettier/shfmt、他は `lsp_format=fallback`。 |
| ✅ mfussenegger/nvim-lint | Lint         | - | 同プラグイン | × | × | いいえ | sh の shellcheck（旧 coc-diagnostic 相当）。他言語は LSP 診断。 |


## 補助 / その他

| 現行                     | 主な役割           | 最終更新   | デファクト候補 | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| ------------------------ | ------------------ | ---------- | -------------- | ------------ | ------------- | --------------- | -------------------------- |
| ✅ nvim-lua/plenary.nvim | Lua ユーティリティ | 2025-07-26 | 同プラグイン   | × | × | いいえ | Telescope 等の依存が多い。 |
| ✅ chrisgrieser/nvim-early-retirement | バッファ自動削除 | 2026-01-06 | 同プラグイン | × | × | いいえ | 20分未使用バッファを自動削除。最低4バッファは保持。 |
| ✅ folke/sidekick.nvim | CLI統合 | 2025-10-31 | 同プラグイン | × | × | いいえ | Claude Code等のCLIをフロートウィンドウで表示。`<C-Space>`でトグル。 |

## 乗り換え優先度の目安

1. **軽量化を急ぐ**: 重い Vimscript プラグインは順次 Lua 版へ。✅ vim-toggle / vim-ambiwidth は vendor 化 + Lua 移植済み (2026-07)。残る Vimscript 依存は vim-matchup のみ（Lua 化は低ROIで対象外）。
2. **UI/テーマ**: Lightline→Lualine、gruvbox Vimscript→Lua 版。
3. **Git/開発補助**: mini.trailspace など Lua ツールへ処理を寄せて重複を解消。
4. **言語/LSP**: ✅ coc.nvim はネイティブ LSP 構成 (nvim-lspconfig + mason + blink.cmp + conform + nvim-lint) へ移行済み (2026-07)。✅ vim-terraform も terraform-ls + treesitter + conform へ置換済み (2026-07)。✅ vim-go も native (gopls + treesitter + conform goimports + treesitter-textobjects) へ置換済み (2026-07。go.nvim は採らず native 構成を選択)。
5. **AI 補完**: ✅ copilot.vim は削除済み (2026-07)。AI 支援は sidekick.nvim + Claude Code に集約したため copilot.lua への移行も不要。

必要に応じてこの表を更新し、プラグイン整理や設定刷新時の判断材料にする。

トリガーゲート型プラグイン (telescope / nvim-tree / mason / render-markdown / sidekick 等) の実使用は `:PluginLoadStats` で数値確認できる (`docs/nvim-plugin-load-tracker.md`、2026-07-11 導入)。UI 系の無条件ロード型は計測対象外なので従来どおり使用実感で判断する。

## 直近で着手したい整理項目

### 完了 (〜2026-06-15 棚卸し時点で反映)
- ✅ `numToStr/Comment.nvim` を削除：コメントトグル機能を整理
- ✅ `windwp/nvim-ts-autotag` を削除：タグ補完を整理
- ✅ `echasnovski/mini.nvim`（フルリポジトリ）を削除：末尾空白は単体の `echasnovski/mini.trailspace` のみ残置
- ✅ `RRethy/nvim-treesitter-endwise` を導入：Ruby/Lua 等の `end` 自動補完
  - ※ `~/.local/share/nvim/lazy` には削除済みプラグイン（Comment.nvim / nvim-ts-autotag / mini.nvim）のダウンロードが残存。`:Lazy clean` で除去可能

### 完了 (2025-12-05)
- ✅ `folke/noice.nvim` を導入：コマンドライン・検索UIのリッチ化
  - コマンドパレットスタイルの表示
  - 長いメッセージの分割表示
  - nvim-notify連携
- ✅ `chrisgrieser/nvim-early-retirement` を導入：未使用バッファの自動削除
  - 20分間使わなかったバッファを自動削除
  - 最低4バッファは保持
- ~~`mvllow/modes.nvim` を導入：モード別カーソル色変更~~
  - **✅ 削除済み(2026-07-10)**：truecolor 端末のモード別カーソル色のみに価値が限定される（cursorline/number/signcolumn は 256色運用で off、カーソル色も guicursor 経由で 256色では非描画）ため廃止。詳細: `issues/nvim-plugin-rewrite-candidates-2026-07-10.md`
- ✅ `folke/sidekick.nvim` を導入：CLI統合
  - Claude Code等のCLIをNeovim内で表示
  - `<C-Space>`でフロートウィンドウをトグル

### 完了 (2025-12-03)
- ✅ `b0o/incline.nvim` を導入：分割ウィンドウでファイル名を浮動表示
  - 未保存時のオレンジ●マーク表示
  - 一般的なファイル名（index.tsx等）での親ディレクトリ表示
  - inactive時の薄い表示
- ✅ `TaDaa/vimade` を導入：非アクティブウィンドウのフェード機能
- ✅ `MeanderingProgrammer/render-markdown.nvim` を導入：Markdown装飾
- ✅ `WilliamHsieh/overlook.nvim` を削除：不要と判断
- ✅ coc-solargraphの設定調整：`useBundler: false`でシステムsolargraphを使用

## 推奨プラグイン候補（リサーチ結果）

| 候補                                                                                                           | Stars (2025-11-14) | 最終 Push  | 用途/置き換え先                           | 採用メリット                                        | 今の構成での乗り換え可否 |
| --------- | ------------------ | ---------- | ----------------------------------------- | --------------------------------------------------- | ------------------------ |
| [`echasnovski/mini.trailspace`](https://github.com/nvim-mini/mini.nvim/blob/main/readmes/mini-trailspace.md) | (mini.nvim 4k+) | 2025-01-30 | 末尾空白削除（導入済み）                  | Lua 実装で Trim/可視化を一本化済み。                 | -: `<leader>lr` も mini.trailspace の API を呼ぶよう更新済み。                              |
| [`ellisonleao/gruvbox.nvim`](https://github.com/ellisonleao/gruvbox.nvim)                                      | 2.9k            | 2025-09-30 | カラースキーム（導入済み）                | Lua 版で truecolor/透明度の調整が容易。             | -: Vimscript 版から移行済み。                                                              |
| [`nvim-lualine/lualine.nvim`](https://github.com/nvim-lualine/lualine.nvim)                                    | 4.9k             | 2025-10-03 | ステータスライン（導入済み）              | 高速・拡張性抜群。Coc/Copilot 情報も統合しやすい。  | -: Lightline→lualine へ切り替え済み。                                                      |
| [`ray-x/go.nvim`](https://github.com/ray-x/go.nvim)                                                            | 2.1k            | 2025-09-15 | Go 開発（→ `vim-go`）                     | gopls + 補助ツールを一括管理。軽量。                | -: **不採用** (2026-07)。vim-go は native 構成 (gopls + treesitter + conform) で置換済み。 |
| [`hashicorp/terraform-ls`](https://github.com/hashicorp/terraform-ls) + `nvim-lspconfig`                       | 3.5k            | 2025-10-23 | Terraform LSP（→ `vim-terraform`）        | 公式 Language Server で整形/補完を統合。            | -: ✅ **導入済み** (2026-07)。vim-terraform を terraform-ls + treesitter + conform で置換。 |
| [`zbirenbaum/copilot.lua`](https://github.com/zbirenbaum/copilot.lua)                                          | 3.8k            | 2025-09-27 | Copilot（→ `copilot.vim`）                | Lua 版で遅延ロード・cmp 連携が簡単。                | -: **対象外** (2026-07)。copilot.vim ごと削除し AI 支援は sidekick.nvim + Claude Code へ。 |

> Note: Stars/Push dates are取得時点 (2025-11-14) の GitHub API レスポンスより。採用前に再チェック推奨。

## 今すぐ導入を検討したいプラグイン

| プラグイン                                                                | Stars (2025-11-14) | 用途                           | 追加メリット                                                                                  |
| ------------------------------------------------------------------------- | ------------------ | ------------------------------ | --------------------------------------------------------------------------------------------- |
| [`folke/todo-comments.nvim`](https://github.com/folke/todo-comments.nvim) | 3.9k            | TODO/FIXME のハイライト & 検索 | コメントベースのタスク管理。プロジェクト横断で TODO を拾いやすい。                            |
| [`kylechui/nvim-surround`](https://github.com/kylechui/nvim-surround)     | 3.9k            | 囲み編集                       | `vim-surround` の Lua 後継で軽量＆Neovim向けに最適化。                                        |
| [`stevearc/conform.nvim`](https://github.com/stevearc/conform.nvim)       | 4.6k            | フォーマッタ統合               | ✅ **導入済み (2026-07)**。prettier/shfmt を統合、他は lsp_format fallback。                  |
| [`nvimdev/lspsaga.nvim`](https://github.com/nvimdev/lspsaga.nvim)         | 3.8k            | LSP UI 拡張                    | 使いやすい hover/rename/codeaction UI を提供。ネイティブ LSP で利用可。                       |
| [`folke/trouble.nvim`](https://github.com/folke/trouble.nvim)             | 6.5k            | Diagnostics/UI                 | LSP のエラー/警告を視覚的に表示し、移動が楽になる。                                           |
| [`mason-org/mason.nvim`](https://github.com/mason-org/mason.nvim)         | 9.7k            | ツール/LSP マネージャ          | ✅ **導入済み (2026-07)**。LSP/formatter/linter のバイナリ管理 (mason-lspconfig は 2026-07-11 に廃止)。             |

> いずれも現在の `init.lua` には含まれていないが、導入すると利便性・UI 体験が向上する定番。優先度はプロジェクトや用途に合わせて調整。

## 新しめの注目プラグイン（2025年12月6日リサーチ）

| プラグイン | Stars | 最終 Push | 主な用途 | 推奨理由 |
| --- | --- | --- | --- | --- |
| [`rareitems/printer.nvim`](https://github.com/rareitems/printer.nvim) | 300+ | Active | デバッグprint挿入 | VSCodeのturbo console log的な機能。変数名を含むprint文を自動生成。 |
| [`luukvbaal/statuscol.nvim`](https://github.com/luukvbaal/statuscol.nvim) | 800+ | Active | 折り畳みバー | クリック可能な折り畳みガター。マウス操作対応。 |
| [`utilyre/sentiment.nvim`](https://github.com/utilyre/sentiment.nvim) | 200+ | Active | ペアブロックハイライト | カーソル位置のブロック対応（括弧等）をハイライト。 |

> 出典: [innei.ren - nvim-plugin-recommend](https://blog.innei.ren/nvim-plugin-recommend)

## ネイティブLSP移行後に使えるプラグイン

ネイティブ LSP へ移行済み (2026-07) のため、以下は追加導入を検討できる LSP 系プラグイン。

| プラグイン | Stars | 主な用途 | 推奨理由 |
| --- | --- | --- | --- |
| [`rmagatti/goto-preview`](https://github.com/rmagatti/goto-preview) | 1,000+ | LSP参照プレビュー | 定義/参照をフローティングで表示。ジャンプ前に確認できる。Telescope連携あり。 |
| [`nvimdev/lspsaga.nvim`](https://github.com/nvimdev/lspsaga.nvim) | 3.8k | LSP UI拡張 | hover/rename/codeactionのUIをリッチ化。アウトライン表示も。 |
| [`ray-x/lsp_signature.nvim`](https://github.com/ray-x/lsp_signature.nvim) | 2.0k | 関数シグネチャ表示 | 関数呼び出し時に引数のヒントをリアルタイム表示。 |
| [`SmiteshP/nvim-navic`](https://github.com/SmiteshP/nvim-navic) | 1.4k | パンくずリスト | lualineに現在のコード位置（クラス > メソッド等）を表示。 |
| [`kosayoda/nvim-lightbulb`](https://github.com/kosayoda/nvim-lightbulb) | 800+ | CodeAction通知 | CodeActionが利用可能な行に💡アイコンを表示。 |
| [`j-hui/fidget.nvim`](https://github.com/j-hui/fidget.nvim) | 2.0k | LSP進捗表示 | LSPの読み込み状況を右下に小さく表示。noice.nvimと併用可。 |
| [`folke/neodev.nvim`](https://github.com/folke/neodev.nvim) | 2.0k | Neovim Lua開発 | Neovim APIの型定義・補完を提供。init.lua編集が快適に。 |
| [`Wansmer/symbol-usage.nvim`](https://github.com/Wansmer/symbol-usage.nvim) | 300+ | 参照数表示 | 関数/変数の参照数をインラインで表示（VSCode風）。 |

> これらはネイティブLSP専用。移行済みなので導入すれば利用できる。

## 新しめの注目プラグイン（2025年11月14日リサーチ）

| プラグイン | Stars (2025-11-14) | 最終 Push | 主な用途 | 推奨理由 |
| --- | --- | --- | --- | --- |
| [`folke/tokyonight.nvim`](https://github.com/folke/tokyonight.nvim) | 7.6k | 2025-11-05 | Lua テーマ | LSP/Treesitter 最適化済みで UI 系プラグインとの相性が良い。morhetz/gruvbox からの移行候補。 |
| [`rmagatti/auto-session`](https://github.com/rmagatti/auto-session) | 1.7k | 2025-10-30 | セッション管理 | バッファやウィンドウ構成を自動保存/復元。開発用プロジェクトを頻繁に切り替える場合に便利。 |
| [`mason-org/mason.nvim`](https://github.com/mason-org/mason.nvim) | 9.7k | 2025-10-01 | LSP/Formatter管理 | GUI パッケージマネージャ。✅ **導入済み (2026-07)**。ネイティブ LSP 移行の土台として採用。 |
| [`folke/todo-comments.nvim`](https://github.com/folke/todo-comments.nvim) | 3.9k | 2025-11-10 | TODO ハイライト | コメント内の TODO/FIXME を一元管理し、一覧表示もできる。長期案件でタスク管理に有効。 |
| [`stevearc/conform.nvim`](https://github.com/stevearc/conform.nvim) | 4.6k | 2025-11-05 | フォーマッタ統合 | null-ls の後継的ポジションとして人気。LSP/formatter を軽量に統合できる。 |
| [`folke/trouble.nvim`](https://github.com/folke/trouble.nvim) | 6.5k | 2025-10-31 | Diagnostics UI | LSP のエラー/警告をリッチに表示。ジャンプ操作が楽になる。 |
| [`nvim-neotest/neotest`](https://github.com/nvim-neotest/neotest) | 3.0k | 2025-11-08 | テストランナー | Ruby/Go/JS など多言語対応のテスト UI。 |
| [`folke/lazydev.nvim`](https://github.com/folke/lazydev.nvim) | 1.3k | 2025-11-06 | LuaLS 最適化 | Neovim 自体の Lua 開発向けの LSP プロファイル。Lua 設定を書き続けるなら導入価値大。 |

> これらは現在の `init.lua` には未導入。用途（テーマ刷新、LSP移行、タスク管理など）に合わせて選び、必要であれば専用セクションを設けて運用すると良い。

## パフォーマンス最適化プラグイン/ツール

| ツール                                                                    | Stars (2025-11-14) | 状態                   | 役割                     | メモ                                                                                                                                                 |
| ------------------------------------------------------------------------- | ------------------ | ---------------------- | ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`nathom/filetype.nvim`](https://github.com/nathom/filetype.nvim)         | 547             | **Archived** (2024-05) | 高速 filetype 判定       | Neovim 0.9 未満なら有効。0.10 以降は `vim.filetype.add()` + 標準 `vim.filetype` で代替。                                                             |
| [`stevearc/profile.nvim`](https://github.com/stevearc/profile.nvim)       | 182             | Active (2025-03 push)  | Lua プロファイラ         | `require('profile').instrument_autocmds()` などで起動/ランタイムのホットスポット可視化。Lazy プラグインの負荷特定に便利。                            |
| [`dstein64/vim-startuptime`](https://github.com/dstein64/vim-startuptime) | 649             | Active (2025-02 push)  | 起動イベント計測         | 純正 `nvim --startuptime` の結果をフローティングで整形表示。履歴比較ができる。                                                                       |
| [`lewis6991/impatient.nvim`](https://github.com/lewis6991/impatient.nvim) | 1.1k            | **Archived** (2023-05) | Lua `require` キャッシュ | Neovim 0.9+ では `vim.loader.enable()` が同等機能を標準提供。旧版を使う場合のみ導入価値あり。                                                        |
| `vim.loader.enable()` (Neovim builtin)                                    | -               | Core                   | Lua モジュールキャッシュ | `if vim.loader then vim.loader.enable() end` を早期に実行すると `require` キャッシュが高速化。`impatient` の後継的存在。                             |
| Lazy.nvim `performance.rtp.disabled_plugins`                              | -               | Core                   | 不要プラグイン停止       | `require("lazy").setup(..., { performance = { rtp = { disabled_plugins = { ... } } } })` を使うと組み込み `netrw` 等を無効化して起動を軽量化できる。 |

> Tip: プロファイルでホットスポットを特定 → `disabled_plugins` / 遅延ロードで無駄を削る → `vim.loader.enable()` で Lua `require` のコストを下げる、という順序が効果的。
