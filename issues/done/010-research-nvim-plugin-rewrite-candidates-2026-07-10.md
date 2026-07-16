# Neovim: vendoring + 全リライトが割に合うプラグインの調査（2026-07-10）

調査日: 2026-07-10
目的: 使用中の全プラグイン（Lua/Vimscript 問わず）のうち、**実装が粗く、仕様を引用して Lua で全書き直し（vendoring + rewrite）した方が快適・保守しやすくなる**ものがあるかを洗い出し、あれば差し替える。
調査方法: Workflow で (1) `_nviminit.lua` + `nvim/**` + `_lazy-lock.json` から全プラグインを棚卸し・サイズ実測、(2) 候補を実ソースで深掘り、(3) リライト推奨を敵対的に検証（仕事の捏造を防ぐ）。全 finding はコミット日付・行数・実装をソースで裏取り済み。

---

## 結論: **該当なし（リライトすべきプラグインは 0 件）**

候補5件すべて **keep-as-is**。「実装が最悪 かつ リライトで良くなる」を満たすものは存在しない。差し替えは行わない。

**なぜ全部 keep なのか（共通構造）**: どの候補も「config が実際に使う表面」は狭いが、その表面は**削れない正しさ表面**（スクロールエンジン / highlight 機構 / 安全ガード / guicursor 管理）の上に乗っている。忠実なリライトはこの正しさ表面をほぼ 1:1 で写経することになり、複雑性は減らない。加えて上流はいずれも生存中で、fork は「上流が無償でやっている nvim API 追随・バグ修正」を自分が引き受ける**保守負債の新規発生**になる。CLAUDE.md /「小さい・動いている・surface が狭い、だけでは rewrite の根拠にならない」に合致。

### リライトを正当化する条件（今回どれも満たさなかった）
1. **小さい × 単一責務**（総行数おおむね < 1500）
2. かつ次のいずれか — (a) 上流が停滞/放置、(b) 実装が粗い/バグ持ち、(c) config が使うのはごく一部
3. **かつ** リライトで快適さ・保守性が実際に上がる（＝削れる部分が「使う経路」にあり、fork 負債を上回る）

「小さいだけ」「動いているだけ」では書き直さない。

---

## 深掘りした候補5件（すべて keep-as-is）

| 候補 | 総行数 | 上流 | config が使う範囲 | 判定根拠（要約） |
|---|---:|---|---|---|
| WhoIsSethDaniel/mason-tool-installer.nvim | 417 | 減速（2026-01） | `ensure_installed` の4個のみ（全機能の5-10%） | 実装は clean。存在価値が「Mason の install API churn からの断熱」そのもの。Mason は直近 v2 に major を上げたばかりで、fork すると次の破壊的変更で自作コードが壊れ**自分が追随義務を負う**。読まない dead code はゼロコスト。リライトは負のROI。 |
| ~~karb94/neoscroll.nvim~~ | 1540 | 減速（2025-12） | `C-u`/`C-d` + 5オプション（約25%） | 狭い表面が乗るのは**削れないスムーズスクロールエンジン**（タイマーループ + easing time-step + scrolloff/EOF/fold 停止判定 + wrapped-line 補正 + 連続スクロール merge）。忠実版は約250-350行の写経で快適さ向上ゼロ、体感 degrade リスク大。**候補中で cost/risk 最悪**。**→ 後日談: 2026-07-12 に実バグ trigger で自作置換・削除済み（下の追記参照）** |
| mvllow/modes.nvim | 600 | 生存（origin/main 2026-03、tag v0.3.0=2025-05） | `set_cursor` のみ（他3機能は256色運用のため off） | 下記「特記」参照。**唯一の borderline**。リライトはせず（上流生存・正しさ表面 fiddly）、価値限定のため**削除**を選択・実施（2026-07-10）。 |
| echasnovski/mini.trailspace | 215 | active（mini.nvim mirror） | `trim()` + highlight 機構（default setup） | 上質な単一ファイル実装。素朴な再実装が落とす edge-case（WinEnter/BufEnter 二重処理・buftype 再入対策・normal-mode gating・match-id dedup）を内包。active 上流を fork する負債のみ。 |
| chrisgrieser/nvim-early-retirement | 174 | active（2026-06、nvim 0.12 対応継続） | 3/11 オプション（コア自動クローズ） | clean な単一ファイル。使うクローズ判定の安全性が約60行のガード群（visible/special/unsaved/alt/quickfix）に依存し忠実版は70-90行で縮まない。上流の nvim 互換追随を失うだけ。 |

（`parallel[2]` = modes.nvim の自動 assessment は StructuredOutput リトライ上限で失敗したため、main が実ソース（v0.3.0）を直読して判定した。）

---

## 特記: modes.nvim（唯一の borderline、それでも keep）

モード別に UI 色を変えるプラグイン（600行）。**この config で唯一の borderline 候補**だったので詳述する。

- **used surface が極端に狭い**: config（`_nviminit.lua` の `mvllow/modes.nvim` spec）は `set_cursor=true` のみ有効で、`set_cursorline`/`set_number`/`set_signcolumn` はすべて off（gui色前提で 256色運用=`SUPPORT_TRUECOLOR=false` では効かないため）。→ プラグインの主要ロジック（winhighlight / CursorLine* / blend / cursorline 管理）の大半がこの config にとって dead。
- **さらにカーソル色（`set_cursor`）は guicursor 経由**で、`termguicolors=off` の 256色運用では基本描画されない。→ 実質 truecolor 端末でのみ「モード別カーソル色」を提供。**この config への価値は限定的**。
- **リライトしない理由**:
  1. 上流が生存（origin/main 2026-03-15、tag v0.3.0=2025-05-26）。fork は保守負債。
  2. 残る `set_cursor` の正しさ表面（guicursor の append/remove + focus 時の再設定 + neovim#21018 のカーソルリセット workaround + `vim.on_key` による operator-pending の y/d 色分け）は**fiddly で削れない**。忠実版は結局これを写経。
  3. 過去にこの config を silent break した実績（v0.2.1 で `set_number` 未参照 / `focus_only` 削除・`ignore_filetypes`→`ignore` 改称）はあるが、**v0.3.0 に pin 済みなら以降の churn は起きない**（＝ pin で中和済み。リライトの動機にならない）。
- **結末（2026-07-10）**: リライトではなく **削除を選択・実施した**。この config での価値が「truecolor 端末のカーソル色のみ」と限定的で（cursorline/number/signcolumn は 256色運用で off、カーソル色も guicursor 経由で 256色では非描画）、リライトより廃止が筋が良いとユーザー判断。`_nviminit.lua` の spec と `_lazy-lock.json` の entry を除去（実体は次回 `:Lazy clean` で消える）。`Modes*` highlight group や basic.lua 等への依存は grep でゼロを確認済み。config は loadfile で構文 OK。
  - 補足: 削除により、`008-research-nvim-config-audit-2026-07-10.md` §2 の「v0.3.0 へ更新（`:Lazy restore` 要フォロー）」は moot になった（実体はまだ buggy な v0.2.1 だったが、そのまま `:Lazy clean` で消える）。

---

## 候補外の主なプラグイン（なぜ対象外か）

- **成熟した大規模フレームワーク/基盤**（telescope 22k / nvim-treesitter 11k / noice 9.8k / blink.cmp 14.8k / mason 24k / nvim-lspconfig 58k / lualine 11.6k / gitsigns 24k / nvim-tree 16k / render-markdown 13.8k 等）: 規模・成熟度ともリライト対象外。
- **Vimscript を含むが大規模 × active**: `vim-matchup`（.vim 6,848、別 doc §4 で「触らない」確定）、`nvim-scrollview`（.vim 806 + .lua 6,327、2026-02 active）、`vimade`（.vim 1,099 + .lua 6,912、2026-05 active）。いずれも「置換して削除」も「全書き直し」も割に合わない（vim-matchup と同じ判断）。
- **既に vendored 済み**: `vim-toggle`、`ambiwidth.nvim`。
- **小さいが対象外**: `telescope-ui-select`（155行だが telescope 内部への結合を再実装することになる）、`nvim-treesitter-endwise`（339行だが言語別 node-type 判定という本質的複雑性が主で、書き直しても複雑性は減らない）。

---

## フォローアップ
1. ~~modes.nvim を「削除 / 現状維持」判断~~ → **✅ 削除を選択・実施済み（2026-07-10）**。`_nviminit.lua` の spec と `_lazy-lock.json` entry を除去。実体は次回 `:Lazy clean` で消える（ユーザー操作）。`docs/nvim-plugins.md`・`008-research-nvim-config-audit-2026-07-10.md` も追従更新。
2. 関連: [`009-refactor-nvim-vimscript-to-lua-migration.md`](009-refactor-nvim-vimscript-to-lua-migration.md)（Vimscript→Lua 移行の記録）/ [`008-research-nvim-config-audit-2026-07-10.md`](008-research-nvim-config-audit-2026-07-10.md)（設定監査）。

---

## 追記: outdated 軸での全数再点検（2026-07-10、同日）

上の調査は「リライトに値するか」が軸で、鮮度の実測は**インストール済みディレクトリの最終コミット日のみ**だった（＝「上流はもっと進んでいるか」「上流自体が死んでいるか」を見ていない。modes.nvim の v0.2.1 残留もこの盲点由来）。ユーザー指摘を受け、outdated 軸で全 35 プラグイン + vendored 2 を再点検した。

計測方法（全件・実測）:
- **lock vs 実体**: `git rev-parse HEAD` と `_lazy-lock.json` の commit を全件比較
- **上流との差**: lock の追跡ブランチを `git ls-remote origin` して手元 HEAD と比較（git プロトコル、全35件有効）
- **上流の生死**: GitHub API `pushed_at`/`archived` + リポジトリページの "Public archive" バッジ（API レート制限にかかった分はバッジ/README で補完）

### 🔴 発見: nvim-treesitter の上流リポジトリが archive 済み

- **事実**（2独立ソースで確認: API `archived=true` + repo ページの "Public archive" バッジ）: `nvim-treesitter/nvim-treesitter` は **2026-04 頃にアーカイブ（read-only）化**された（最終 push 2026-04-03）。今後 master に修正が入ることは永久にない。
- **ただし今すぐの問題ではない**: main ブランチ README が「**master は locked だが Nvim 0.11 との後方互換のために残す** / main の rewrite は **Neovim 0.12.0+（nightly）必須**」と明言している。この config は nvim **0.11.5** + master 凍結ピン（意図的設計、`008-research-nvim-config-audit-2026-07-10.md` §A）なので、**master ピンは 0.11 向けに正しい唯一の選択のまま**。archive によって挙動が変わるものは何もない。
- **移行トリガー（明確）**: **Neovim を 0.12+ へ上げる時**。その時点で main 系 rewrite（インストール/クエリ管理が全面非互換、`tree-sitter-cli` 必須）への移行を、`nvim-treesitter-textobjects`（同じく master=legacy / main=rewrite の二系統。こちらの repo は非 archive・活動中）とセットで計画する。それまでは何もしない。
- なお「リライト対象か」の答えは不変: 10k 行の基盤で、後継は自作 fork ではなく上流の main rewrite。

### 🟡 borderline（keep のまま・記録のみ）

| プラグイン | 事実 | 判断 |
|---|---|---|
| telescope-ui-select.nvim | 上流最終コミット **2023-12-04**（2.5年更新なし）。非 archive。手元=上流 tip | 155行の薄いアダプタで、被る API（`vim.ui.select`）は安定。feature-complete な休眠であり実害なし。keep |
| bufferline.nvim | 上流最終コミット **2025-01-14**（約1.5年更新なし）。非 archive。手元=上流 tip | 動作良好・深く設定済み。休眠だが 0.11 で問題なし。keep（0.12 移行時に要注視） |

### ✅ clean（確認済み・問題なし）

- **lock vs 実体の乖離: 全件ゼロ**（modes.nvim のみ lock 外だが、これは削除済みで `:Lazy clean` 待ちの正常状態）
- **blink.cmp**: installed **v1.10.2 = 上流最新リリース**（`git ls-remote --tags` で確認）。main ブランチが先行しているのは未リリース開発分で、`version="*"` 運用として正常
- **mini.trailspace**: 追跡ブランチが上流 tip より約7週古いのみ（通常の `:Lazy update` サイクル内。問題ではない）
- **それ以外の全プラグイン**: 追跡ブランチで上流 tip と一致（behind なし）。上流も生存
- **vendored の上流**: `rbtnn/vim-ambiwidth`（最終 push 2025-08-01）・`lukelbd/vim-toggle`（2025-02-03）とも **vendoring 基点から動きなし** → VENDOR.md の再同期は不要
- **非推奨 API**: `008-research-nvim-config-audit-2026-07-10.md` で headless 全ロード + `:checkhealth` 済み（非推奨/削除通知ゼロ）。本再点検では再実施せず同結果を引用

---

## 追記: 2026-07-12 再点検（多レンズレビュー時）

nvim 設定全体の多レンズレビュー（バグ/API/性能/死コード/堅牢性 × 敵対的検証）を実施した際に、本 issue の「リライト/vendor 候補はあるか」を再点検した。

### 結論: 不変（新たなリライト/vendor 候補なし）

プラグイン構成の変化は 2 件のみで、いずれも本調査の枠組みと整合する形で完了済み:

- **mason-lspconfig.nvim を廃止**（2026-07-11、commit 84a5bcf）: リライトでなく「使用サーフェス（installed サーバの enable）を `vim.lsp.enable()` 直呼びで置換して削除」。
- **neoscroll.nvim を自作置換**（2026-07-12、commit c29a998）: 本調査では keep-as-is（cost/risk 最悪）と判定したが、その後「押しっぱなしでカーソル乱れ」という**実バグが trigger になり**、エンジンの忠実移植（250-350 行と見積もり）ではなく「リピート中はアニメせず素通し」という**別設計の最小実装**（`nvim/lua/dotfiles/smooth_scroll.lua`、約 100 行）で置換・削除した。「実変更 trigger 待ち」原則どおりの経過で、忠実写経を避けたことで見積もりより小さく済んだ。keep 判定自体は「trigger なしで書き直さない」の意味で正しかった。

### 新観測: nvim-lspconfig (rolling) の 0.11 非互換 drift（watch 事項）

- 事実（2026-07-12 実測）: nvim-lspconfig の `lsp/terraformls.lua` が on_attach で **0.12 専用 API `vim.lsp.codelens.enable`** を無条件に呼び、nvim 0.11.5 では .tf/.hcl を開くたび ON_ATTACH_ERROR になっていた。`lsp/*.lua` の同型呼び出しは grep で terraformls のみ。
- 対応済み: `nvim/lua/dotfiles/lsp.lua` の `M.servers.terraformls` に存在ガード付き on_attach を追加して吸収（commit efe2c93）。
- 残るリスク: nvim-lspconfig は rolling（main 追従）のため、**nvim 0.11 に留まる間は `:Lazy update` のたびに同型 drift が再発しうる**。選択肢: (a) 実害が出たら都度 per-server override で吸収（今回の対応。当面はこれで足りる）、(b) 頻発するなら nvim-lspconfig を tag pin（新サーバ定義の取り込みも止まるトレードオフ）、(c) nvim 0.12 リリース後に上げて解消。vendor 化は不適（58k 行の設定集で、価値は上流の追随そのもの）。
- 解消条件: nvim 0.12+ への更新（その際 lsp.lua の terraformls override も削除してよい）。

### vendored (toggle.nvim) の既知の制限: 現状維持で確定

- 今回のレビューで「protected→private が到達不能（"protected" が on/off 両リストに載り、off 側優先で public↔protected の往復に収束する）」を検出・headless 実測で確認したが、**ユーザー判断で現状維持（対応しない）とした**（2026-07-12。実用上 public↔protected の往復で足りる）。
- 再検出防止の rationale は `_nviminit.lua` の toggle 語彙リスト直下コメントに記載（3 値サイクルが必要になった場合の組み替え方針も同所に一行残した）。issue ファイルは作らない。
