# Neovim: Vimscript プラグインの Lua 化 / vendoring 計画

作成日: 2026-07-10
目的: config を Lua に一本化するため、残っている Vimscript プラグインを洗い出し、vendoring + Lua 書き換えの難易度と戦略を整理する。
調査方法: `~/.local/share/nvim/lazy/*` の全プラグインで `.lua`/`.vim` のファイル数・行数を実測して言語構成を分類。config の実使用サーフェスを `_nviminit.lua` から確認。

---

## 大前提: 「Lua 化」の正しい目標

**「Vimscript を 1 行ずつ Lua へ移植する」ことが目的ではない。** 目的は「Vimscript プラグインへの依存を外し、config を Lua に揃える」こと。手段は 2 通りある:

1. **移植 (port)**: 小さく、自分で語彙を保守したいプラグイン → repo に vendoring して Lua 書き換え（vim-toggle・vim-ambiwidth で実施済み）。
2. **置換 (replace)**: 大きいが**実際に使っている機能はごく一部**のプラグイン → その使用サーフェスだけをネイティブ/既存 Lua プラグインで満たし、Vimscript プラグインごと**削除**する。全書き換えはしない。

**vendoring が妥当なのは 1 のケースだけ**（小さく・自分で保守する前提）。19k 行のプラグインを「書き換えないのに repo へコピー」しても保守負債が増えるだけ。大きいプラグインは「置換して削除」か「Vimscript のまま残す」の二択。

---

## 現状の Vimscript プラグイン一覧（実測）

| プラグイン | 分類 | .vim 行数 | .lua 行数 | 実使用サーフェス | 状態 |
|---|---|---:|---:|---|---|
| `fatih/vim-go` | Vimscript | 19,167 | 0 | 明示 keymap=GoDecls のみ。**ただし ftplugin が既定で `K`/textobj/`]] [[`/保存時 gofmt+goimports も有効化**（LSP/定義だけネイティブ委譲済み） | 要対応 |
| `andymass/vim-matchup` | Vimscript(+lua) | 6,848 | 1,210 | `%` マッチ / matchparen / offscreen popup / surround | 触らない推奨 |
| ~~`tpope/vim-rails`~~ | Vimscript | 5,767 | 0 | 既定挙動（`:A` 等・`gf`・projections・構文） | **✅ 削除済み**(2026-07-10) |
| `ambiwidth.nvim`（vendored、旧 vim-ambiwidth） | ~~Vimscript~~→Lua | — | 小(lua) | `setcellwidths()` 補正（起動時） | **✅ 移植完了**(2026-07-10) |
| `vim-toggle`（vendored） | ~~Vimscript~~→Lua | — | ~600(lua) | `+` トグル / `:Toggle` | **移植完了**(2026-07-09) |

（`nvim-scrollview`/`plenary`/`nvim-web-devicons`/`nvim-treesitter` 等に含まれる少量の `.vim` は補助ファイルで、プラグイン本体は Lua。対象外。）

---

## プラグイン別の難易度と推奨戦略

難易度: 易 / 中 / 難 / 最難

### 1. ambiwidth.nvim（旧 vim-ambiwidth） — ✅ 移植完了（2026-07-10）— 難易度: **易〜中**（移植）
- **中身**: `list.txt`（東アジアあいまい幅の Unicode レンジ表）を `ambiwidth_generator.vim` が読み、`setcellwidths()` 呼び出しを生成。`plugin/ambiwidth.vim` が起動時（`has('vim_starting')`）に適用。
- **移植の勘所**: ランタイム本体は実質 `vim.fn.setcellwidths(ranges)` 1 呼び出し。データ（list.txt）はデータのまま残し、パース + レンジ table 構築を Lua 化するだけ。**難所は「原 Vimscript と同一の幅 table を出す」ことの A/B 一致確認**（vim-toggle と同じ検証作法）。`&encoding=='utf-8'` ガードと起動タイミング（`VimEnter`/即時）を踏襲する。
- **実施結果**: `vendor/nvim-plugins/ambiwidth.nvim/` に vendoring し、上流 Vimscript を全面 Lua 移植（ディレクトリ名は Lua ネイティブ Neovim プラグインの慣習に合わせ `ambiwidth.nvim`。上流 `rbtnn/vim-ambiwidth` の fork）。巨大な生成器（`ambiwidth_generator.vim`）とデータ（`list.txt`）は削除し、生成済みの 95 レンジ（base 32 + Cica/Nerd Font PUA 63）だけを `lua/ambiwidth.lua` の table として保持。`plugin/ambiwidth.lua` が utf-8 + `setcellwidths` 対応時に eager ロードで適用。`g:ambiwidth_cica_enabled`/`g:ambiwidth_add_list` は原版どおり尊重。**原 Vimscript との getcellwidths A/B 一致を確認済み**（既定95 / cica off32 / add_list増分、差分ゼロ）。`_nviminit.lua` の lazy spec を `dir=.../ambiwidth.nvim` に置換、VENDOR.md も Lua 版へ改訂、回帰テスト `tests/nvim/test_ambiwidth.sh` を追加（設定分岐・不正入力の WARN ガードを固定）。

### 2. vim-go — 難易度: **中**（置換して削除） / 最難（全書き換え・非推奨）
- **明示的に keymap しているのは GoDecls のみ**（`_nviminit.lua:135-149`）: `<leader>gd`→`<Plug>(go-decls)`、`<leader>gD`→`<Plug>(go-decls-dir)`、`:GoUpdateBinaries`(build)。`g:go_gopls_enabled=0`/`g:go_def_mapping_enabled=0` で LSP・定義ジャンプは**既にネイティブ委譲済み**。
- **⚠️ ただし vim-go の ftplugin が既定で有効化する挙動も削除対象になる**（config で無効化していないため。`ftplugin/go.vim` 実測）:
  - `K`→`:GoDoc`（`go_doc_keywordprg_enabled` 既定 1）
  - テキストオブジェクト `af/if/ac/ic`、関数ジャンプ `]]`/`[[`（`go_textobj_enabled` 既定 1）
  - **保存時 gofmt + goimports**（`BufWritePre → go#auto#fmt_autosave()`。`go_fmt_autosave`/`go_imports_autosave` はいずれも既定 1、config は未設定）
  - `:Go*` コマンド群（`:GoTest` 等）
  → **現 conform は Go を保存時整形していない**（`format_on_save` は terraform/hcl のみ、`_nviminit.lua:297-302`）。vim-go を削除すると **Go の保存時整形が失われる**ため、置換とセットで手当てが要る。
- **戦略**: 全書き換えは論外。**used surface を満たして vim-go ごと削除**。削除前チェックリスト:
  1. GoDecls 置換: `:GoDecls`=func/type 限定なので `require("telescope.builtin").lsp_document_symbols`（現ファイル）を kind filter（Function/Method/Struct 等）付きで。`:GoDeclsDir`=ディレクトリ対象は `lsp_workspace_symbols` か自前ディレクトリ走査で**別途**再現（telescope で厳密同等ではない）。
  2. **保存時整形**: conform の `formatters_by_ft.go = { "goimports" }`（or gopls formatting）＋ `format_on_save` に go を追加。
  3. `K`/textobjects/`]] [[` を使っているか棚卸しし、使うならネイティブ（`t` hover は既に有効。textobj は treesitter-textobjects 等）で補完。
- **現状の注意**: `g:go_decls_mode=""` は ctrlp/fzf 自動検出だが、**どちらも未導入**（lazy に `fzf.vim`/`ctrlp` なし）。→ 現状 `<leader>gd`/`gD` は動作しない可能性が高い（皮肉にも置換の障壁は低い＝失う実挙動は少ない）。
- **副次利得**: `:GoUpdateBinaries`（起動時 build ステップ）と `g:go_*` フラグ群も不要になり spec が単純化。
- **vendoring は不要**（削除するので repo へ取り込まない）。

### 3. vim-rails — ✅ 削除済み（2026-07-10）— 移植も vendoring もせず廃止
- **決定**: 棚卸しの結果、**削除**（Lua 移植・代替導入なし）。`_nviminit.lua:112` の spec と `_lazy-lock.json` の entry を除去（`~/.local/share/nvim/lazy/vim-rails` の実体は次回 `:Lazy clean` で消える）。
- **判断根拠**:
  - **def ジャンプは vim-rails の担当ではなく native LSP（solargraph）** が担う（`gd`/`gD`/`<C-k>`）。→ 削除しても定義/参照ジャンプは一切失われない（ゼロリスク。config load も headless で確認済み）。
  - vim-rails 固有価値（`:A` 代替ファイル・`gf`・projections・DSL 構文）のうち、実運用で使うのは `:A` の model↔test 程度だが、**minitest だと命名が不規則で `:A` が効きにくい**うえ、普段は**参照ジャンプ中心**で `:A` 自体を使わなくなっていた。Rails 開発の頻度も低下。
- **将来 Rails 開発が増えたら**（再導入時の選択肢）: Rails ワークフロー重視なら **ror.nvim**（telescope 前提が既に揃う）、補完/定義重視なら **solargraph → ruby-lsp 乗り換え**（+ Rails は ruby-lsp-rails addon で関連ファイルジャンプ）。projections エンジンそのものの Lua 後継は無い（`vim-projectionist` が唯一で Vimscript）。
- **関連（別件・任意）**: def ジャンプ重視なら solargraph → ruby-lsp 化は体験改善として有効（vim-rails 削除とは独立の掃除 vs 改善）。nvim-lspconfig に `ruby_lsp` 定義は既に存在。

### 4. vim-matchup — 難易度: **最難**（触らない推奨）
- **中身**: 成熟した `%` マッチエンジン（matchparen 置換、区切り越え `%` モーション、テキストオブジェクト `i%`/`a%`、`matchup_matchparen_offscreen={method="popup"}`、surround）。`.vim` 6,848 行で、`lua/` は treesitter 連携の一部のみ。
- **戦略**: **全書き換えはプラグイン再著述に等しく非推奨**。config は offscreen popup + surround という**特徴的機能**を使っている（`_nviminit.lua:150-158`）ため、素朴な代替では劣化する。
  - 「Vimscript を残さない」を厳密に追うなら選択肢は: (a) matchup を Vimscript のまま許容（現実的）、(b) matchup を捨てて Neovim 組み込み matchparen + `nvim-treesitter` の matchup モジュール + 組み込み `%` に切替（offscreen popup / surround を失う）。
- **推奨**: 本移行プロジェクトの**対象外**とする。活発にメンテされており、Lua 化の労力対効果が最も悪い。

---

## 推奨着手順（価値 × 容易さ）

1. ~~**vim-ambiwidth**~~ — ✅ 2026-07-10 に移植完了（vendoring + Lua 全面移植、A/B 一致確認 + 回帰テスト追加）。
2. **vim-go を used surface 置換で削除** — 中・高価値（19k 行の Vimscript + build ステップを一掃）。上記チェックリスト順で: (a) conform に Go 保存時整形（goimports/gopls）を追加、(b) `lsp_document_symbols`(kind filter) で GoDecls を置換、(c) K/textobj/`]] [[` の使用有無を確認、してから vim-go の spec を削除。※ GoDecls は backend(ctrlp/fzf) 未導入で現状動いていない可能性が高い＝失う実挙動は小さい。
3. ~~**vim-rails**~~ — ✅ 2026-07-10 に削除済み（棚卸しの結果、移植も代替もせず廃止。def ジャンプは LSP 担当で喪失なし）。
4. **vim-matchup は対象外** — 最難・低ROI。Vimscript のまま残す（または組み込み matchparon+treesitter への割り切りを別途判断）。

---

## 補足
- vendoring 済みプラグインは `vendor/nvim-plugins/`（`VENDOR.md` に取得元・ライセンス・更新手順を記載する規約）。**Lua 移植したら VENDOR.md の「更新手順」も Lua 版に改訂**する（vim-toggle が手本）。
- 関連: 設定監査は [`nvim-config-audit-2026-07-10.md`](nvim-config-audit-2026-07-10.md)。
