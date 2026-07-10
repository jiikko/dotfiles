# Neovim 設定バグ監査 — ロジック/キーマップ/遅延ロード契約/256色規律 (2026-07-10)

調査日: 2026-07-10
対象: `_nviminit.lua` / `nvim/lua/dotfiles/{basic,lsp,js_ts_common}.lua` / `nvim/ftplugin/*.lua` / `vendor/nvim-plugins/{vim-toggle,ambiwidth.nvim}` / `tests/nvim/*.sh` / `_lazy-lock.json` / `docs/nvim-plugins.md`
棲み分け: 同日の [`nvim-config-audit-2026-07-10.md`](nvim-config-audit-2026-07-10.md) は「API 陳腐化 / silent option loss」観点。本 issue は**別レンズ** (Lua ロジックバグ・キーマップの組み込み潰し・lazy ロード契約・256色運用規律・vendored 移植・テスト基盤) で、前回クローズ済み事項は対象外。
調査方法: 並行 finder 6 視点 (2 視点はセッション上限で死亡 → main のインライン精読で代替) + main agent が**全 finding を nvim v0.11.5 実機の headless 実測で再検証**してから採録。finder の実測が誤っていた指摘 3 件を棄却済み (末尾「棄却」参照)。実測に使った検証スクリプトは `tmp/nvim-audit/` (git 対象外) に残置。

---

## P1

### 1. [test-infra] test_nvim.sh はプラグイン config/init のエラーを検知できず false-pass する

- **ファイル**: [tests/nvim/test_nvim.sh:24-34](../tests/nvim/test_nvim.sh)
- **内容**: lazy.nvim は各プラグインの `init` (全プラグイン) と `config` (そのとき**ロードされるプラグインのみ**) の失敗を xpcall で捕捉し、エラー通知を `vim.schedule()` で**次イベントループへ遅延**する (`lazy/core/util.lua` の `M.try`)。`"+lua vim.cmd('qa')"` は遅延通知が flush される前にプロセスを終了させるため、**exit code は 0・ログは空**になり、30 行目の stderr grep (`E[0-9]{2,}:` 等) も何も見ない。36 行目以降の第二チェック (`lazy.stats().count > 0`) も config 失敗では count が減らないため素通りする。→ 「config が headless でロードできること」を保証するはずのテストが、起動時にロードされる (start/eager) プラグインの config や全プラグインの init の破壊を検知できない。なお `qa` 即時終了のため遅延プラグインの config はそもそも起動時に走らず、本テストの守備範囲外である点も明記しておく (別途 `Lazy! load all` 相当を回すなら別テスト)。
- **実測**: 実 lazy.nvim + `config = function() error("INJECTED") end` の最小 spec で `nvim --headless -u init.lua "+lua vim.cmd('qa')"` → **exit 0 / ログ 0 byte / grep 不検出**。同じ init で `+lua vim.wait(500)` を先に挟むと `Error detected while processing command line: Failed to run \`config\` for vim-toggle` が出力され grep にマッチする (= `vim.schedule` 遅延が原因であることの裏取り)。再現 init: `tmp/nvim-audit/lazytest/init.lua`。
- **対応方針**: 症状 (grep パターン追加) ではなく構造で潰す。qa の前にイベントループを pump して遅延通知を flush する (`"+lua vim.wait(200)"` を qa の前に挟む、または Lua チェック側で `vim.wait` 後に `vim.notify` の捕捉ログを検査して `cquit 1`)。test_ftplugins.sh / test_ambiwidth.sh と同じ「pcall + cquit」規律に寄せ、eager プラグインの config 完了を明示的に assert するのがより強い (例: `require("lazy.core.config").plugins` の `_.loaded` と error の有無を検査)。
- **補足**: 前回監査の元 P1 (「+luafile が error でも exit 0」) と同族の「握りつぶし」だが経路が別 (`vim.schedule` 遅延)。前回の修正はこの経路を塞いでいない。

---

## P2

### 2. [silent-behavior-loss] telescope の file_ignore_patterns が Lua パターンとして壊れている (package-lock.json / yarn-error.log は除外されない)

- **ファイル**: [_nviminit.lua:380](../_nviminit.lua)
- **内容**: `file_ignore_patterns` は telescope 内部で `string.find(file, pattern)` (Lua パターン) として評価される。`-` は Lua パターンでは「直前要素の 0 回以上・非貪欲」量指定子であり、`"package-lock.json"` と `"yarn-error.log"` は**文字どおりの文字列自身にマッチしない**。→ 両ファイルは find_files / live_grep で一切除外されず表示され続ける (`yarn.lock` は `.` が任意 1 文字として偶然マッチするため除外できている)。また `"^node_modules/"` は先頭アンカーのため、monorepo 等の `packages/app/node_modules/...` は除外されない (`"^.git/"` も同様にネスト側は不可だが、こちらは通常 cwd 直下運用で実害小)。
- **実測**: `string.find("package-lock.json", "package-lock.json")` = **nil**、`string.find("yarn-error.log", "yarn-error.log")` = **nil**、`string.find("yarn.lock", "yarn.lock")` = 1、`string.find("packages/app/node_modules/x.js", "^node_modules/")` = nil (検証: `tmp/nvim-audit/v1_patterns.lua`)。
- **対応方針**: `-` を `%-` にエスケープ (`"package%-lock%.json"`, `"yarn%-error%.log"`)。ネスト node_modules も除外したいなら `"/node_modules/"` と `"^node_modules/"` の併記 (または `"[/^]node_modules/"` 相当の 2 パターン)。`.` のエスケープ (`%.`) も併せて行い「Lua パターンである」ことをコメントで明記する (見た目が glob に見えるのが再発源)。

### 3. [silent-behavior-loss] 256色運用規律 (cterm 併記) から漏れた highlight 群 — 主環境で不可視/無色

- **ファイル**: [_nviminit.lua:640](../_nviminit.lua) (MiniTrailspace) / [_nviminit.lua:600](../_nviminit.lua) (GitSignsCurrentLineBlame) / [_nviminit.lua:766-776](../_nviminit.lua) (incline render) / [nvim/lua/dotfiles/basic.lua:94](../nvim/lua/dotfiles/basic.lua) (ZenkakuSpace)
- **内容**: この構成の主環境は `SUPPORT_TRUECOLOR=false` の 256色 (`termguicolors=off`)。bufferline (425 行コメント) と lsp.lua (167 行コメント) は「gui と cterm を併記する」規律を明文化して守っているが、以下が漏れている:
  - **MiniTrailspace** (`bg="#fb4934"` のみ): cterm も代替属性も無く、**末尾空白ハイライトが主環境で完全に不可視** (プラグインの意味が消える)
  - **GitSignsCurrentLineBlame** (`fg/bg` + bold): 黄背景+黒字が出ず bold のみ
  - **incline** の render が返す `guifg`/`guibg`: incline は `:highlight` 生コマンドに文字列連結する実装 (`incline/highlight.lua`) で、cterm 未指定だと focused/unfocused の色分けと `●` (未保存) の橙が全て端末既定色
  - **ZenkakuSpace** (`{underline=true, bg="darkgray"}`): ctermbg が無く背景が出ない (underline は cterm にも効くため完全不可視ではない)。93 行目のコメントは termguicolors=on 側の理屈のみで off 側の欠落に触れていない
- **実測**: `termguicolors=off` で gui のみの `nvim_set_hl` を適用 → `synIDattr(..., "bg", "cterm")` = 空 (検証: `tmp/nvim-audit/v2_hl.lua`)。incline の `:highlight` 連結実装は `~/.local/share/nvim/lazy/incline.nvim/lua/incline/highlight.lua` で確認。
- **対応方針**: bufferline/lsp.lua と同じく ctermfg/ctermbg を併記する (incline は render の戻り値テーブルに `ctermfg`/`ctermbg` キーを追加できる — `:highlight` に素通しされる)。「256色運用では gui のみ指定は無効」の規律が 2 ファイルのコメントに分散しているので、規律の一次情報をどこか 1 箇所 (例: `_nviminit.lua` 冒頭の WORKAROUND コメント) に集約し他から参照する形が再発防止になる。

### 4. [silent-behavior-loss] ZenkakuSpace の :match は window-local — 新規ウィンドウ/タブで全角スペースがハイライトされない

- **ファイル**: [nvim/lua/dotfiles/basic.lua:91-105](../nvim/lua/dotfiles/basic.lua)
- **内容**: `apply()` は setup 時と ColorScheme 時に `vim.cmd([[match ZenkakuSpace /　/]])` を実行するが、`:match` は**ウィンドウローカル**。起動後に `:sp` / `:vs` / `:tabnew` で開いたウィンドウには伝播せず、以降そのウィンドウでは全角スペースが可視化されない。
- **実測**: match 適用後 `getmatches()` = 1 件 → `vsplit` 直後の新ウィンドウで `getmatches()` = **0 件** (検証: `tmp/nvim-audit/v2_hl.lua`)。
- **対応方針**: window 単位の再適用を autocmd (`WinNew` または `WinEnter`) で行うか、window に依存しない手段へ寄せる。`vim.fn.matchadd` も window-local なので同じ問題を持つ — 全ウィンドウで確実に効かせるなら `WinEnter` で「未適用なら適用」(getmatches で重複ガード) が素直。

### 5. [lazy-load-contract] bufferline の event="BufAdd" は起動時に一度も発火しない — 単一バッファセッションで keymap ごと無効

- **ファイル**: [_nviminit.lua:407](../_nviminit.lua)
- **内容**: `BufAdd` は起動処理中に作られる最初のバッファでは発火しない (`:help BufAdd`)。`nvim <file>` でも `nvim` 単体でも bufferline はロードされず、**起動後に 2 個目のバッファを追加して初めて**ロードされる。`always_show_bufferline = true` (バッファ 1 個でも常時表示) の意図と矛盾するうえ、config 内で定義している `gt`/`gT`/`<Right>`/`<Left>`/`<C-a><C-a>` のマップもそれまで存在しない (`gt`/`gT` は素のタブ切替にフォールバック、`<C-a><C-a>` は増分+増分)。
- **実測**: 実 config で `nvim README.md` / 新規ファイル / 引数なしの 3 パターンとも起動直後 `package.loaded["bufferline"]` = **false** (検証: `tmp/nvim-audit/v3_startup.lua`)。
- **対応方針**: 同ファイルの gitsigns/ibl 等と同じ `event = { "BufReadPre", "BufNewFile" }` 系へ寄せる (bufferline は UI 常駐なので `VeryLazy` でもよい)。keymap は spec の `keys` に出すとロード前でも入口になる — ただし `<Right>`/`<Left>` のような常用キーを keys にすると押下でロードが走るため、event を直すほうが構造的。

---

## P3

### 6. [lazy-load-contract] incline / nvim-scrollview の event に BufNewFile が無く、新規 (未存在) ファイルで有効化されない

- **ファイル**: [_nviminit.lua:723](../_nviminit.lua) (incline, `BufReadPre` のみ) / [_nviminit.lua:613](../_nviminit.lua) (scrollview, `BufReadPost` のみ)
- **内容**: `BufReadPre`/`BufReadPost` は存在しないファイルでは発火しない。`nvim newfile.txt` や `:e newfile.txt` 起点のセッションではファイル名フロート表示/スクロールバーが出ない。同ファイルの gitsigns/ibl/nvim-lint は `BufNewFile` を併記済みで、この 2 spec だけ漏れ。
- **実測**: 未存在ファイル引数で起動 → `incline=false scrollview=false` (既存ファイルでは両方 true)。
- **対応方針**: `event = { "BufReadPre", "BufNewFile" }` / `{ "BufReadPost", "BufNewFile" }` に併記統一。

### 7. [autocmd-gap] cursorline 有効化が {WinEnter, BufRead} のみ — 引数なし起動/新規ファイルの初回ウィンドウで off のまま

- **ファイル**: [nvim/lua/dotfiles/basic.lua:115-127](../nvim/lua/dotfiles/basic.lua)
- **内容**: `WinEnter` は起動直後の最初のウィンドウでは発火せず (`:help WinEnter`)、新規ファイルは `BufRead` でなく `BufNewFile`。→ `nvim` 単体・`nvim newfile.txt` では、ウィンドウを離れて戻るまで cursorline が出ない (既存ファイルを開けば `BufRead` で入る)。
- **実測**: 実 config で引数なし/未存在ファイル起動 → `vim.wo.cursorline` = **false**、既存ファイル → true。
- **対応方針**: WinLeave/WinEnter の「アクティブウィンドウのみ cursorline」という意図は保ちつつ、初期状態を `opt.cursorline = true` (set_options) で与えるか、イベントに `BufNewFile`・`VimEnter` を足す。

### 8. [keymap-shadow] nmap <Tab> → zo が <C-i> (jumplist 前進) を乗っ取る

- **ファイル**: [_nviminit.lua:816](../_nviminit.lua)
- **内容**: `<Tab>` と `<C-i>` は内部キーコードが同一 (端末が拡張キー報告で区別しない限り。主環境の Apple Terminal + tmux は区別しない)。`nmap <Tab> zo` により `<C-o>` で戻った後 `<C-i>` で進む操作が **fold open に化ける**。
- **実測**: `<Tab>` に nmap を張り `<C-i>` を feedkeys → **マップが発火** (hit=1)。`maparg("<C-i>","n")` も同一マップを返す (検証: `tmp/nvim-audit/v7_tab.lua`)。
- **対応方針**: fold 開閉を `<Tab>` 以外 (既定の `za`/`zo` 系や `<leader>` 配下) へ移すのが根本。`<Tab>` を維持するなら「<C-i> は失われる」旨の rationale コメントを添える ([`pending-issue-rationale-in-code.md`](../_claude/rules/pending-issue-rationale-in-code.md))。

### 9. [keymap-shadow] nmap <C-]> → <Esc> が tag jump を全バッファで潰す (:help のリンク辿り不可)

- **ファイル**: [nvim/lua/dotfiles/basic.lua:71](../nvim/lua/dotfiles/basic.lua)
- **内容**: `<C-]>` は tag jump (help バッファのリンク追跡を含む)。normal mode でグローバルに `<Esc>` へ潰しているため、`:help` 内でタグへ飛べない (help の ftplugin は `<C-]>` を再定義しない)。insert 側 (70 行目) の Esc 代替とペアで入れた惰性の可能性が高く、意図ならコメントが無い。
- **実測**: finder が headless で確認 (マップ無し: `:help quickfix` からタグジャンプでバッファ遷移 / マップ有り: 遷移せず)。main はマップ定義とメカニズム (グローバル nmap が全バッファに効く) をソースで確認。
- **対応方針**: normal 側の `<C-]>` マップを削除する (normal で Esc 相当が必要な場面はほぼ無い)。意図的に残すなら rationale コメント必須。

### 10. [dead-code] lualine の relative_path_from_git_root は vim.b.git_dir を設定する主体が構成に無く、常にフォールバック

- **ファイル**: [_nviminit.lua:205-209](../_nviminit.lua)
- **内容**: `vim.b.git_dir` は伝統的に vim-fugitive が設定するバッファ変数で、本構成に fugitive は無い。gitsigns が設定するのは `b:gitsigns_*` のみ。→ 分岐は常に真で `vim.fn.expand("%")` (cwd 相対) を返し、「git root 相対」ロジックは一度も走らない。cwd ≠ git root のとき表示は意図 (git root 相対) と食い違う。
- **実測**: `grep -rln "b\.git_dir\|b:git_dir" ~/.local/share/nvim/lazy/` = **0 件**。
- **対応方針**: gitsigns が拾える情報へ寄せるか、`vim.fs.root(0, ".git")` (0.10+) で自前解決する。dead branch を残すなら「fugitive 導入時に活きる」等の rationale が必要だが、素直に実装を直すのが良い。

### 11. [lsp] LspDetach が detach した client を見ずにバッファの documentHighlight autocmd を全消去

- **ファイル**: [nvim/lua/dotfiles/lsp.lua:225-230](../nvim/lua/dotfiles/lsp.lua)
- **内容**: callback は `args.data.client_id` を見ず `args.buf` の `hl_augroup` を丸ごと clear する。JS/TS バッファは ts_ls (documentHighlight **対応**) と eslint (**非対応**) が同時 attach する構成のため、eslint 側だけが detach (クラッシュ・`:LspStop eslint` 等) すると、生きている ts_ls の documentHighlight までバッファを開き直すまで無言で消える。
- **実測**: eslint-lsp 実体 (`eslintServer.js`) に `documentHighlightProvider` が**無い**ことを grep で確認 (0 hits)。clear ロジックはソース確認。
- **対応方針**: detach した client が documentHighlight 対応のときだけ clear する、または clear 後に「残存 client に対応者がいれば再登録」する。LspAttach 側の貼り直しロジック (135 行) と対で考えると後者が対称的。

### 12. [hl-lifecycle] setup 時に一度だけ設定する nvim_set_hl 群が :colorscheme 再実行で消える

- **ファイル**: [nvim/lua/dotfiles/lsp.lua:169-170](../nvim/lua/dotfiles/lsp.lua) (DiagnosticSign*) / [_nviminit.lua:103](../_nviminit.lua) (Visual) / 600 (GitSignsCurrentLineBlame) / 640 (MiniTrailspace)
- **内容**: `:colorscheme` は highlight をクリアしてから再構築するため、起動後にテーマを切り替えるとこれらのカスタム色が既定に戻る。basic.lua の ZenkakuSpace だけ ColorScheme autocmd で再適用しており、規律が不統一。
- **実測**: `nvim_set_hl` → `colorscheme retrobox` → `synIDattr` で ctermbg **196 → 空** (検証: `tmp/nvim-audit/v2_hl.lua`)。
- **対応方針**: カスタム hl を 1 つの `apply_custom_highlights()` に集約し、setup 時 + ColorScheme autocmd で再適用する (ZenkakuSpace と同じパターンに統一)。#3 の cterm 併記と同時に直すのが効率的。

### 13. [vendored-doc] ambiwidth.nvim README の g:ambiwidth_add_list 設定例が既定 cica レンジと重複し、従うと全レンジが無効化される

- **ファイル**: [vendor/nvim-plugins/ambiwidth.nvim/README.md:14](../vendor/nvim-plugins/ambiwidth.nvim/README.md)
- **内容**: 例示の `[[0xfe566, 0xfe568, 2], [0xff500, 0xffd46, 2]]` は既定 cica テーブル (lua/ambiwidth.lua:109-110) に**既に含まれる**。例をそのまま設定すると `setcellwidths()` が E1113 (Overlapping ranges) で失敗し、all-or-nothing のため **base+cica 全 95 レンジが不適用**になる (WARN 通知 1 回のみ)。upstream 由来の doc drift だが vendored 後も実害が残る。
- **実測**: README の例を設定して setup → `getcellwidths()` = **0 件**、`strdisplaywidth("℃")` = 1 (本来 2)。E1113 の WARN も確認 (検証: `tmp/nvim-audit/v5_ambi.lua`)。
- **対応方針**: README の例を既定に含まれないレンジへ差し替える。構造側は、setup() で add_list を base+cica と突き合わせて重複を除外 (または add_list 単独で先に検証) すれば「例に従うと全滅」という脆さ自体が消える。

### 14. [maintainability / not-a-current-bug] telescope の load_extension("notify") が nvim-notify への依存を spec に持たない

- **ファイル**: [_nviminit.lua:399](../_nviminit.lua) (load_extension) / 362 (`<leader>fn`)
- **現状は壊れない (codex レビューで訂正)**: nvim-notify は [_nviminit.lua:506](../_nviminit.lua) に **event/cmd/keys/lazy を持たない top-level spec** として存在するため lazy.nvim の start plugin 扱いで起動時にロードされる。telescope は cmd/keys 遅延ロードで、初回起動 (VimEnter 後) 時点で nvim-notify は必ず rtp に居る。noice を外しても top-level spec が残る限り extension は解決するため、**当初書いた「noice が先にロードするから顕在化しないだけ」という因果は誤り**で、実運用で壊れる経路は無い。
- **残る保守性の論点 (P3 未満)**: 「telescope が使う extension の供給元 (nvim-notify) が telescope spec の dependencies に明示されていない」ため、依存関係がコード上で追いにくい。nvim-notify を start plugin から外す/削除する改修をしたときに初めて壊れる潜在リスク。
- **対応方針 (任意)**: telescope の dependencies に `"rcarriga/nvim-notify"` を明記すると依存が局所化される (1 行、機能変化なし)。バグ修正ではなく保守性改善。

---

## 棄却 (false positive として検証済み — 次回監査のノイズ削減)

### A. ruby.lua の `<Esc>` 終端マッピング (`<leader>rr`/`<leader>re`/`<leader>ds`) は**動く**

- finder 3 件 + main の事前仮説が「`:cw<Esc>` / `:e ...<Esc>` はコマンドラインがキャンセルされ no-op」と指摘したが、**誤り**。`:help c_<Esc>` に「**In macros** or when 'x' present in 'cpoptions', **start entered command**」とあり、マッピング経由の `<Esc>` はコマンドを**実行する** (Vi 互換仕様)。
- **実測**: ruby.lua と同形のマップを定義し LHS を feedkeys(remap) で投入 → `:e file<Esc>` で**ファイルが開き**、`:cw<Esc>` で **quickfix window が開いた** (検証: `tmp/nvim-audit/v6_map_esc.lua`)。finder の「実行されない」実測は nvim_input 投入直後 (未処理) に状態を読んだ誤検証。
- 旧 `_vimrc` (commit dc76691) から続く書き方で、機能している。ただし挙動が仕様の裏面に依存する難解 idiom なので、**触る機会があれば `<CR>` 終端へ正規化するか、この quirk のコメントを添える**とよい (対応必須ではない)。

### B. vendored vim-toggle の Lua 移植 — 監査済み・欠陥なし

- vendor-toggle 担当 finder はセッション上限で死亡したため、main が `lua/toggle.lua` / `plugin/toggle.lua` の全行をインライン精読で代替監査した。index() の 0/1 基点変換・`index(..., 26)` (a-z 26 文字より後ろ = 追加 consecutive 文字の判定)・matchstrpos の扱い・visual/range 分岐・`_nviminit.lua` の init タイミング (VeryLazy ロード前に g: が確定) を確認し、欠陥は見つからなかった。原版との A/B 一致は VENDOR.md 記載のとおり。

---

## カバレッジ注記

- finder 8 視点中 6 視点が完走 (lazy-spec / lua-logic / basic-lua / lsp-ftplugin / vendor-ambiwidth / consistency-tests)。**vendor-toggle と keymap-shadow はセッション上限で死亡** → 前者は上記 B のとおり main が代替、後者は main の事前精読 seed (#8, #9) と basic-lua finder の成果で主要キーは網羅したが、探索の網羅性は他視点より一段落ちる。
- consistency-tests finder の成果は #1 のみ (lock/spec の食い違い・modes.nvim 残骸は検出なし。main も `git grep modes.nvim` 相当の確認で残骸ゼロを確認済み)。
- 全 finding は main agent が実測または一次ソースで再検証してから採録した (sonnet finder の報告をそのまま転記していない)。
