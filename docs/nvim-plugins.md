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
| --- | --- | --- | --- | --- | --- | --- | --- |
| ✅ morhetz/gruvbox | カラースキーム | 2023-08-14 | `ellisonleao/gruvbox.nvim`, `folke/tokyonight.nvim` | ○ | △ | はい | Lua 版は遅延ロード対応が良く起動が軽い。 |
| ✅ folke/which-key.nvim | キーマップチートシート | 2025-02-22 | 同プラグインがデファクト | × | × | いいえ | 定番。置き換え不要。 |
| ✅ itchyny/lightline.vim | ステータスライン | 2024-12-30 | `nvim-lualine/lualine.nvim`, `famiu/feline.nvim` | ○ | △ | はい | Lua 実装の方が高速・非同期。余裕があれば移行。 |
| ✅ akinsho/bufferline.nvim | タブライン | 2025-01-14 | `romgrk/barbar.nvim`, 標準タブライン | × | × | いいえ | Barbar から移行済み。LSP/診断アイコンも扱える Lua 実装で軽量。 |
| ✅ nvim-tree/nvim-tree.lua | ファイラ | 2025-04-04 | `nvim-neo-tree/neo-tree.nvim`, `stevearc/oil.nvim` | △ | △ | いいえ | Oil は軽量、Neo-tree は機能豊富。好み。 |
| ✅ nvim-tree/nvim-web-devicons | アイコン | 2025-04-07 | 同プラグイン | × | × | いいえ | 事実上の標準。 |
| ✅ lukas-reineke/indent-blankline.nvim | インデントガイド | 2025-03-18 | `echasnovski/mini.indentscope`, `glepnir/indent-guides.nvim` | ○ | △ | いいえ | mini/indent-guides は軽量。 |
| ✅ rcarriga/nvim-notify | 通知 UI | 2025-01-20 | 同プラグイン | × | × | いいえ | 競合が少なく現状維持。 |
| ✅ dstein64/nvim-scrollview | スクロールバー | 2025-10-02 | `petertriho/nvim-scrollbar`, `Xuyuanp/scrollbar.nvim` | △ | × | いいえ | 仮想テキストのみで軽量。Neoscroll とも競合しにくい。 |
| ✅ karb94/neoscroll.nvim | スクロールアニメ | 2024-12-06 | 同プラグイン | × | × | いいえ | Lua 実装で保守継続中。Smoothie から置換済み。 |
| ✅ folke/flash.nvim | 高速ジャンプ | 2025-02-14 | 同プラグイン | × | × | いいえ | 代替少。維持。 |
| ✅ echasnovski/mini.nvim (animate) | ミニユーティリティ | 2025-01-30 | `folke/zen-mode.nvim`, `pocco81/true-zen.nvim` | △ | △ | いいえ | 使うモジュールだけ残せば軽量。リポジトリは `nvim-mini` へ移管済み。 |

## ナビゲーション / 検索

| 現行                             | 主な役割           | 最終更新                | デファクト候補                                      | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ                                      |
| -------------------------------- | ------------------ | ----------------------- | --------------------------------------------------- | ------------ | ------------- | --------------- | ----------------------------------------- |
| ✅ nvim-telescope/telescope.nvim | ファジーファインダ | 2025-03-18              | `ibhagwan/fzf-lua`, `junegunn/fzf.vim`              | △            | △             | いいえ          | Telescope は機能性◎。軽量化なら fzf-lua。 |
| ✅ rbtnn/vim-ambiwidth           | 全角幅調整         | 2024-10-24              | 代替少                                              | ×            | ×             | はい             | 日本語環境では維持推奨。                  |
| ✅ lewis6991/gitsigns.nvim       | Git ハイライト     | 2025-10-19              | 同プラグイン                                         | ×            | ×             | いいえ          | 差分/ブレーム/ステージングまで一括管理できる Lua 実装。 |

## テキスト編集支援

| 現行                                         | 主な役割        | 最終更新   | デファクト候補                                       | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| -------------------------------------------- | --------------- | ---------- | ---------------------------------------------------- | ------------ | ------------- | --------------- | ---------------------------------------------------- |
| ✅ lukelbd/vim-toggle                        | トグル          | 2025-02-03 | `echasnovski/mini.operators`, `folke/which-key`      | △ | △ | はい | Lua 実装に置き換えると遅延が効く。 |
| ✅ kana/vim-operator-user                    | オペレータ拡張  | 2015-02-17 | 同プラグイン                                         | × | × | はい | 依存多数。継続。 |
| ✅ tyru/operator-camelize.vim                | ケース変換      | 2017-02-19 | `tpope/vim-abolish`                                  | ○ | ○ | はい | Abolish の方が多機能。入替候補。 |
| ✅ andymass/vim-matchup                      | 括弧/タグマッチ | 2025-03-30 | 同プラグイン                                         | × | × | いいえ | Treesitter 対応でデファクト。 |
| ✅ windwp/nvim-ts-autotag                    | タグ補完        | 2025-02-18 | 同プラグイン                                         | × | × | いいえ | 標準的。 |
| ✅ nvim-treesitter/\*                        | 構文解析        | 2025-04-07 | 同プロジェクト                                       | × | × | いいえ | 乗り換え不要。 |
| ✅ numToStr/Comment.nvim                     | コメントトグル  | 2024-06-09 | `folke/ts-comments.nvim`, `echasnovski/mini.comment` | ○ | △ | いいえ | mini/commentary 系は軽量。余裕があれば移行。 |
| ✅ echasnovski/mini.trailspace               | 末尾空白可視    | 2025-01-30 | `echasnovski/mini.trailspace`, `axieax/typo.nvim`    | × | × | いいえ | 末尾空白削除を mini.trailspace で実装。既に mini.nvim を使用中のため安定。 |
| ✅ MeanderingProgrammer/render-markdown.nvim | Markdown レンダ | 2025-04-08 | `ellisonleao/glow.nvim`, `OXY2DEV/markview.nvim`     | △ | △ | いいえ | 描画が重いとの報告あり。Glow/markview で軽量化。 |

## 言語・LSP

| 現行                      | 主な役割     | 最終更新   | デファクト候補                                           | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| ------------------------- | ------------ | ---------- | -------------------------------------------------------- | ------------ | ------------- | --------------- | ---------------------------------------------------- |
| ✅ tpope/vim-rails        | Rails 支援   | 2025-02-19 | 同プラグイン                                             | × | × | はい | Rails 界隈でデファクト。 |
| ✅ hashivim/vim-terraform | Terraform    | 2025-01-20 | `hashicorp/terraform-ls` + LSP, `mfussenegger/nvim-lint` | △ | △ | はい | LSP/formatter へ徐々に移行可能。 |
| ✅ fatih/vim-go           | Go 補助      | 2025-03-09 | `ray-x/go.nvim`, `nvim-lspconfig` + `gopls`              | △ | △ | はい | “重い/設定過多” の声が多く、LSP 構成へ移行推奨。 |
| ✅ github/copilot.vim     | AI 補完      | 2025-03-24 | `zbirenbaum/copilot.lua`, `sourcegraph/sg.nvim`          | ○ | △ | はい | Vimscript 版は重いとの声。Lua 版へ移行推奨。 |
| ✅ neoclide/coc.nvim      | LSP/補完統合 | 2025-04-06 | `nvim-lspconfig` + `nvim-cmp`                            | △ | △ | はい | Node 依存で重いとの評判。長期的にネイティブ LSP へ。 |

## 補助 / その他

| 現行                     | 主な役割           | 最終更新   | デファクト候補 | 乗り換え可否 | 今すぐ捨てる? | Vimscript依存? | メモ |
| ------------------------ | ------------------ | ---------- | -------------- | ------------ | ------------- | --------------- | -------------------------- |
| ✅ vim-jp/vimdoc-ja      | 日本語ヘルプ       | 2025-04-07 | 同プラグイン   | × | × | はい | 国内での標準ドキュメント。 |
| ✅ nvim-lua/plenary.nvim | Lua ユーティリティ | 2025-02-11 | 同プラグイン   | × | × | いいえ | Telescope 等の依存が多い。 |

## 乗り換え優先度の目安

1. **軽量化を急ぐ**: 重い Vimscript プラグイン（例: vim-toggle）を Lua 版へ。
2. **UI/テーマ**: Lightline→Lualine、gruvbox Vimscript→Lua 版。
3. **Git/開発補助**: mini.trailspace など Lua ツールへ処理を寄せて重複を解消。
4. **言語/LSP**: vim-go, vim-terraform, coc.nvim を徐々にネイティブ LSP 構成へ。
5. **AI 補完**: copilot.vim→copilot.lua 等 Lua 版へ移行。

必要に応じてこの表を更新し、プラグイン整理や設定刷新時の判断材料にする。

## 直近で着手したい整理項目

- **tyru/operator-camelize.vim → tpope/vim-abolish**: Abolish なら camelCase ↔ snake_case 変換だけでなく置換辞書も統合できるため、操作系を一本化できる。

## 推奨プラグイン候補（リサーチ結果）

| 候補                                                                                                           | Stars (2025-11-14) | 最終 Push  | 用途                                      | 採用メリット                                        |
| -------------------------------------------------------------------------------------------------------------- | ------------------ | ---------- | ----------------------------------------- | --------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| 候補                                                                                                           | Stars (2025-11-14) | 最終 Push  | 用途/置き換え先                           | 採用メリット                                        | 今の構成での乗り換え可否                                                                   |
| ---                                                                                                            | ---             | ---        | ---                                       | ---                                                 | ---                                                                                        |
| [`echasnovski/mini.trailspace`](https://github.com/nvim-mini/mini.nvim/blob/main/readmes/mini-trailspace.md) | (mini.nvim 4k+) | 2025-01-30 | 末尾空白削除（導入済み）                  | Lua 実装で Trim/可視化を一本化済み。                 | -: `<leader>lr` も mini.trailspace の API を呼ぶよう更新済み。                              |
| [`ellisonleao/gruvbox.nvim`](https://github.com/ellisonleao/gruvbox.nvim)                                      | 2.9k            | 2025-09-30 | カラースキーム（→ `morhetz/gruvbox`）     | Lua 版で truecolor/透明度の調整が容易。             | ○: colorscheme 名を差し替えるだけ。                                                        |
| [`nvim-lualine/lualine.nvim`](https://github.com/nvim-lualine/lualine.nvim)                                    | 4.9k             | 2025-10-03 | ステータスライン（→ `lightline.vim`）     | 高速・拡張性抜群。Coc/Copilot 情報も統合しやすい。  | △: Lightline 依存関数を Lualine フォーマットに書き換える必要あり。                         |
| [`ray-x/go.nvim`](https://github.com/ray-x/go.nvim)                                                            | 2.1k            | 2025-09-15 | Go 開発（→ `vim-go`）                     | gopls + 補助ツールを一括管理。軽量。                | △: LSP + mason 連携前提。Coc との住み分けが必要。                                          |
| [`hashicorp/terraform-ls`](https://github.com/hashicorp/terraform-ls) + `nvim-lspconfig`                       | 3.5k            | 2025-10-23 | Terraform LSP（→ `vim-terraform`）        | 公式 Language Server で整形/補完を統合。            | △: LSP セットアップ（mason + conform などの formatter 連携）が未整備なので段階的導入推奨。 |
| [`zbirenbaum/copilot.lua`](https://github.com/zbirenbaum/copilot.lua)                                          | 3.8k            | 2025-09-27 | Copilot（→ `copilot.vim`）                | Lua 版で遅延ロード・cmp 連携が簡単。                | ○: 現在の Copilot キーは最小限で、Lua 版の設定にも転用しやすい。                           |

> Note: Stars/Push dates are取得時点 (2025-11-14) の GitHub API レスポンスより。採用前に再チェック推奨。

## 今すぐ導入を検討したいプラグイン

| プラグイン                                                                | Stars (2025-11-14) | 用途                           | 追加メリット                                                                                  |
| ------------------------------------------------------------------------- | ------------------ | ------------------------------ | --------------------------------------------------------------------------------------------- |
| [`folke/todo-comments.nvim`](https://github.com/folke/todo-comments.nvim) | 3.9k            | TODO/FIXME のハイライト & 検索 | コメントベースのタスク管理。プロジェクト横断で TODO を拾いやすい。                            |
| [`kylechui/nvim-surround`](https://github.com/kylechui/nvim-surround)     | 3.9k            | 囲み編集                       | `vim-surround` の Lua 後継で軽量＆Neovim向けに最適化。                                        |
| [`stevearc/conform.nvim`](https://github.com/stevearc/conform.nvim)       | 4.6k            | フォーマッタ統合               | 各言語の formatter を簡潔に管理。Coc 依存を減らせる。                                         |
| [`nvimdev/lspsaga.nvim`](https://github.com/nvimdev/lspsaga.nvim)         | 3.8k            | LSP UI 拡張                    | 使いやすい hover/rename/codeaction UI を提供。Coc/ネイティブどちらでも利用可。                |
| [`folke/trouble.nvim`](https://github.com/folke/trouble.nvim)             | 6.5k            | Diagnostics/UI                 | LSP や Coc のエラー/警告を視覚的に表示し、移動が楽になる。                                    |
| [`mason-org/mason.nvim`](https://github.com/mason-org/mason.nvim)         | 9.7k            | ツール/LSP マネージャ          | LSP サーバー/フォーマッタ/リンターを一括インストール管理。将来的な LSP ネイティブ移行が簡単。 |
| [`folke/noice.nvim`](https://github.com/folke/noice.nvim)                 | 5.4k            | UI リッチ化                    | コマンドラインや LSP メッセージをリッチ化し、通知/入力 UI を改善。                            |

> いずれも現在の `init.lua` には含まれていないが、導入すると利便性・UI 体験が向上する定番。優先度はプロジェクトや用途に合わせて調整。

## 新しめの注目プラグイン（2025年11月14日リサーチ）

| プラグイン | Stars (2025-11-14) | 最終 Push | 主な用途 | 推奨理由 |
| --- | --- | --- | --- | --- |
| [`folke/tokyonight.nvim`](https://github.com/folke/tokyonight.nvim) | 7.6k | 2025-11-05 | Lua テーマ | LSP/Treesitter 最適化済みで UI 系プラグインとの相性が良い。morhetz/gruvbox からの移行候補。 |
| [`rmagatti/auto-session`](https://github.com/rmagatti/auto-session) | 1.7k | 2025-10-30 | セッション管理 | バッファやウィンドウ構成を自動保存/復元。開発用プロジェクトを頻繁に切り替える場合に便利。 |
| [`mason-org/mason.nvim`](https://github.com/mason-org/mason.nvim) | 9.7k | 2025-10-01 | LSP/Formatter管理 | GUI パッケージマネージャ。将来的に Coc からネイティブ LSP へ移行する際の土台になる。 |
| [`folke/todo-comments.nvim`](https://github.com/folke/todo-comments.nvim) | 3.9k | 2025-11-10 | TODO ハイライト | コメント内の TODO/FIXME を一元管理し、一覧表示もできる。長期案件でタスク管理に有効。 |
| [`stevearc/conform.nvim`](https://github.com/stevearc/conform.nvim) | 4.6k | 2025-11-05 | フォーマッタ統合 | null-ls の後継的ポジションとして人気。LSP/formatter を軽量に統合できる。 |
| [`folke/trouble.nvim`](https://github.com/folke/trouble.nvim) | 6.5k | 2025-10-31 | Diagnostics UI | LSP/Coc のエラー/警告をリッチに表示。ジャンプ操作が楽になる。 |
| [`nvim-neotest/neotest`](https://github.com/nvim-neotest/neotest) | 3.0k | 2025-11-08 | テストランナー | Ruby/Go/JS など多言語対応のテスト UI。Coc に依存せずテスト結果を扱える。 |
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
