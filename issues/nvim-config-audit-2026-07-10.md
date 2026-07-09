# Neovim 設定監査 — outdated / 挙動サイレント喪失 (2026-07-10)

調査日: 2026-07-10
対象: `_nviminit.lua` / `nvim/lua/dotfiles/{basic,lsp,js_ts_common}.lua` / `nvim/ftplugin/*.lua` / `tests/nvim/*.sh` / `_lazy-lock.json`（`vendor/nvim-plugins/vim-ambiwidth` は Lua 化作業中のため対象外）
調査方法: **実機 Neovim v0.11.5 での一次情報検証**を主軸に据える。
  1. `-u NONE` / フル config で使用 API の実在・型を実測。
  2. `nvim` ランタイムソース（`$VIMRUNTIME/lua/vim/lsp/*`）を grep して使用 API の非推奨注釈を確認。
  3. フル config を headless ロード + `Lazy! load all` で**全プラグインの setup() を実行**し、非推奨/削除通知と `:checkhealth` を採取（eager だけでなく lazy 経路も網羅）。
  4. Workflow で 19 プラグインのオプション/API を**固定コミットの実ソースに照合**（silent-ignore を含む）、各 finding をアドバーサリアル検証（証拠パス必須、recall 禁止）。計 21 エージェント。確定 finding は main が実ソースで再検閲。
判定方針: 「実装が既に強制している事実は指摘しない／証拠のない断定を書かない」。**未対応 2 件（いずれも P3）・本セッションで解決 1 件（元 P1）・意図的 2 件・clean 確認多数**。設定本体はおおむね modern。かつて主問題だったテストハーネスの構造欠陥は、本セッション中にユーザーが修正済み（#1 参照）。

---

## 未対応の課題（2 件、いずれも P3）

### 1. [P3 / silent-behavior-loss] sidekick.nvim: `win.width`/`win.height` がフラット指定で無効 — フロート窓が既定 0.9/0.9 に

- **ファイル**: `_nviminit.lua:667-671`（`win = { layout = "float", width = 0.6, height = 0.7 }`）
- **内容**: 固定バージョン（lazy-lock: `208e1c5`）では、レイアウト寸法は `win.float.{width,height}`（layout="float" 時）/ `win.split.{width,height}` に**ネスト**する仕様。`layout` の兄弟としてフラットに置いた `width`/`height` は**どこからも読まれない**。実ソース `lua/sidekick/cli/terminal.lua:360-361` の `open_win()` は `vim.deepcopy(is_float and win_opts.float or win_opts.split)` と `self.opts.float/split` だけをマージして `nvim_open_win` opts を構築し、`self.opts.width`/`self.opts.height` を参照しない。`config.lua` の `vim.tbl_deep_extend("force", defaults, opts)` はフラット `width`/`height` を「読まれない余剰キー」として黙ってマージする（警告なし）。→ フロート窓は意図の 0.6/0.7 でなく既定 `float.width=0.9, float.height=0.9` になる。
- **由来**: フラット→ネストの変更は上流 commit `c93c0cb`(2025-09-30, "feat(terminal): added full support for split / float layouts")。`git merge-base --is-ancestor c93c0cb HEAD` = 真（固定コミットは変更後）。
- **対応**: `win = { layout = "float", float = { width = 0.6, height = 0.7 } }` に修正。
- **検証メモ**: main が実ソース直読で再確認（`terminal.lua:360-372` が読むのは deepcopy 済みローカル `opts` であり `self.opts.width` ではない。トップレベル width/height の読み取り箇所は grep でゼロ）。severity P3 は「窓サイズが既定に落ちるだけで機能は動く」ため。

### 2. [P3 / silent-behavior-loss] modes.nvim: `set_number = false` が固定バージョン v0.2.1 で無効

- **ファイル**: `_nviminit.lua:648,654`（`tag = "v0.2.1"` 固定 + `set_number = false`）
- **内容**: 固定 `v0.2.1`（commit `2cd194d`）の実ソース `lua/modes.lua` で `set_number` は **L15 の既定テーブルにしか現れず、どこからも読まれない**（`grep set_number lua/` のヒットは 1 行のみ）。`M.highlight()` は全 scene の `winhighlight` マップ（`CursorLineNr` 含む）を `set_number` の判定なしに無条件適用する。→ `set_number=false`（行番号背景を無効化する意図）は**効かない**。ゲート `if not config.set_number then winhl_map.CursorLineNr = nil end` は後続 commit `9ca1d68`("fix: ignored `set_number` config (#45)", 2023-09-20) で追加され、これは `2cd194d` の子孫（`git merge-base --is-ancestor 2cd194d 9ca1d68` = 真）。
- **対応（2 択）**:
  - (a) **modes.nvim を `v0.3.0` へ更新**する。`set_number` ゲートを含む版であり、この旧ピン（v0.2.1 固定）自体も同時に解消する。更新後に 256色運用で表示崩れがないか目視確認。
  - (b) v0.2.1 を維持するなら、`set_number = false` は**この版では無効**である旨と「なぜ v0.2.1 に固定するか」を該当行にコメントで残す（[`pending-issue-rationale-in-code.md`] 準拠。現状ピン理由は未文書）。
- **検証メモ**: main が HEAD=2cd194d と set_number 未参照を実測再確認。「v0.2.1 旧ピン」と同一根なので (a) が根本解。severity P3 は「行番号背景の色だけ・256色運用では視認性が低い」ため。

---

## 本セッションで解決済み（1 件・記録用）

### 元 P1 [test-infra] テストハーネスが Lua エラーを握りつぶし「常に緑」だった — 存在しない API 参照を隠蔽

- **ファイル**: `tests/nvim/test_ftplugins.sh` / `tests/nvim/test_nvim.sh`
- **かつての問題（根本）**: 両スクリプトは `nvim --headless -u <config> +luafile <check.lua> +qall` の exit code で失敗を検知しようとしていたが、この形は **check.lua が `error()` しても exit 0 を返す**（`error("boom")` で exit 0 を実測。`E5113` は stderr に出るが exit code に伝播しない）。→ 失敗検知が無効化され、`test_ftplugins.sh` は「ftplugin mappings ok」を印字して常に緑。
- **症状（根本が隠していた）**: `test_ftplugins.sh` が存在しない API `vim.keymap.get(...)` を使用（0.11.5 で `type` は `nil`、直接呼ぶと `E5113`）。握りつぶしのため今まで表面化していなかった。
- **影響（当時）**: `make test`（`test-runtime` → `test-nvim`、`Makefile:71-78`）と GitHub Actions CI（`.github/workflows/tests.yml:44` が `make test-runtime` を実行）がこのテストを走らせるため、ftplugin マッピングや config ロードのデグレを**検知できず偽の安心**を与えていた（※ dotfiles は lefthook 不使用。gate は make + CI）。
- **解決（本セッション中にユーザーが修正・main が検証）**: `vim.keymap.get` を実在 API `vim.fn.maparg("<leader>bi", "n")` へ差し替え、`pcall` で捕捉して失敗時 `vim.cmd("cquit 1")` で明示的に非0終了させる形に修正（両スクリプトに根本原因のコメントも追記）。**検証結果**: (a) `zsh tests/nvim/test_ftplugins.sh` は exit 0 で pass、(b) `cquit 1` は `+luafile`+`+qall` 経由でも非0を返すことをパイプなしで実測、(c) 期待値をわざと壊すと exit 1 = **修正後は本当に失敗を検知する**ことを確認。
- **補足**: 推奨代替として `nvim -l <script.lua>`（Lua エラーで exit 1 を実測）もあるが、pcall+cquit 方式で既に非0伝播は達成済み。CLAUDE.md「不具合対応の原則（構造的根本改修）」どおり、症状（API 差し替え）だけでなく根本（握りつぶすハーネス）が是正された。

---

## 意図的 / 現状維持（再評価不要 — 次回監査のノイズ削減）

検証フェーズで「意図的設計」「outdated でない」と判定。同じ指摘が再生成されたら以下で即棄却できる。

### A. nvim-treesitter を frozen `master` ブランチに固定 → intentional-design
- `_nviminit.lua:159-176`。`branch = "master"` + classic `require("nvim-treesitter.configs").setup(...)`。master は凍結系統（`main` が rolling）だが、**設定コメントに「parser/query の matched set を固定し `main` の drift を避ける」意図が明記済み**。`verify-design-intent-before-refactor.md` に従い「migrate to main」は**指摘しない**。固定コミット `cf12346a`(2026-03-23) で headless ロード・全プラグイン強制ロードとも非推奨/エラーなし。`:checkhealth nvim-treesitter` の "errors found in the query, try to run :TSUpdate {lang}" は**凡例テキスト**で実 parser エラーではない。

### B. blink.cmp `version = "*"` → v1.10.2（最新）で outdated ではない
- `_nviminit.lua:257`。lazy-lock の固定コミット `78336bc` を `git describe --tags` すると **`v1.10.2`**（この環境はネットワーク未達のため**キャッシュ済みローカルタグ上で最新**。外部最新は未確認だが、`version="*"` は最新リリースを追う指定なので古くはならない）。`"branch":"main"` は lockfile の既定ブランチ表記のノイズであり、commit が権威。プリビルドバイナリ利用 + `fuzzy.implementation="prefer_rust_with_warning"` フォールバックも意図的で、cargo 非搭載機の起動死を回避する正しい設計。誤検出防止のため明示的にクローズ。

---

## clean と確認済み（監査の網羅性の証跡・padding ではない）

一次情報で「outdated でない」と確認できた範囲。今後「ここが古い」と再指摘されたら以下で棄却する。

- **コア API（すべて 0.11 現行形）**: `vim.lsp.config`/`vim.lsp.enable`、mason 2.0（`mason-org/` + `automatic_enable`）、`vim.diagnostic.jump` / `signs.text`、`vim.lsp.inlay_hint` の filter-table 形、`vim.lsp.get_clients`（`get_active_clients` ではない）、`client:supports_method`（コロン形）、`vim.uv or vim.loop`、`ts_ls`（`tsserver` ではない）、`ibl`（indent-blankline v3）、`vim.treesitter.foldexpr`。
  - ランタイム grep: `buf_request_all` / `make_position_params` / `supports_method` に非推奨注釈なし。`make_position_params(0, offset_encoding)` は encoding を明示的に渡しており警告条件に当たらない（正しい用法）。
  - noice が override する `vim.lsp.util.convert_input_to_markdown_lines` / `stylize_markdown` は 0.11.5 に現存（override 対象は有効）。※ 参考: `jump_to_location` / `trim_empty_lines` は 0.12 で削除予定だが本 config は未使用。
- **プラグイン設定**: `Lazy! load all` で telescope/conform/nvim-tree/bufferline/gitsigns/noice/neoscroll/lualine/ibl/mason-lspconfig 等**全 setup() を実行しても非推奨/削除通知はゼロ**。19 プラグインのオプション照合でも #2/#3 以外に silent-ignore なし。
- **`:checkhealth`**: 私たちの設定起因の警告なし（出たのは luarocks/cargo/他言語ツール未導入と tailwind LSP 自身の既定スキーマの `deprecatedSupport` のみ、いずれも無関係）。

---

## 補足（ぼやきレベル・issue 化は任意）

- `_gvimrc`（`colorscheme koehler` 等）は**古典 gVim/MacVim 専用**で Neovim には効かない（Neovim の GUI は `ginit.vim`）。gVim を使っていないなら dead file。害はないが将来削除候補。
- `nvim/ftplugin/ruby.lua` の `<leader>rw`/`<leader>rr`/`<leader>re` が `/tmp/ruby_caller` をハードコード。個人用マップなので任意だが、`vim.fn.stdpath("run")` 等へ寄せると衛生的。
