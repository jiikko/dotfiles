# Ruby の参照検索: 使用実績の計測と、呼び出し側索引プロトタイプ

種別: research / perf
起票: 2026-09-08
前提: [332](done/332-ruby-lsp-selection-by-probe.md) の追補 3（`<C-k>` を Ruby のメソッドだけ ripgrep へ振り分けた）

## 背景

ruby-lsp の `textDocument/references` は索引を使わず、要求のたびにワークスペース全体を
Prism で再パースする（実測 11.2 秒 / ubiregi-server）。索引 (`ruby_indexer::Entry`) が
持っているのは**宣言だけ**で、呼び出し側の逆引きが無いため。上流は
[#3051](https://github.com/Shopify/ruby-lsp/issues/3051) を closed as not planned にしている。

332 で `<C-k>` を「Ruby のメソッドは ripgrep（0.104 秒）／定数と他言語は LSP」に振り分けた。
rg は速いが、AST を見ていないのでコメント・文字列・シンボルを拾う。

## 決めたこと（2 段階）

### 段階 1: 実運用でどれだけ困るかを数える

「rg の false positive で実際に困った回数」が分からないまま、重い仕組みを作らない。
`<C-k>` が rg 経路を通った回数と、その直後にユーザーが LSP 版へフォールバックした回数を
記録し、`:DotfilesRefsStats` で見られるようにする。困らないなら段階 2 は不要。

### 段階 2: 呼び出し側索引が現実的かを実測するプロトタイプ

**🚨 addon では references を差し替えられない**（実測 2026-09-08、ruby-lsp 0.26.11）:

- `RubyLsp::Addon` の公開フックは `create_code_lens_listener` / `create_hover_listener` /
  `create_document_symbol_listener` / `create_semantic_highlighting_listener` /
  `create_definition_listener` / `create_completion_listener` /
  `create_discover_tests_listener` の 7 つだけで、**references 用が無い**
- `server.rb:799` は `Requests::References.new(...).perform` を直接呼んでおり、addon の
  ディスパッチを通らない
- 索引側の `RubyIndexer::Enhancement` は「索引にエントリを足す」ためのもので、
  references はそのエントリを見ない

したがって「addon で O(1) にする」は公開 API では**不可能**。monkey patch は事実上の fork に
なるので採らない（upstream への PR も今回はやらない方針）。

代わりに、**判断に必要な数字だけを取るプロトタイプ**を書く:

- ワークスペースを 1 回だけ Prism でパースし、呼び出しノード名 → 位置の逆引きを構築する
- 測るのは ①構築時間 ②メモリ増分 ③クエリ時間 ④rg に対する精度差
  （rg のヒットのうち、AST 上は呼び出しでないもの＝コメント・文字列・シンボルの件数）
- この数字が良ければ「sidecar として常駐させ、nvim から叩く」が選択肢になる。
  悪ければ rg のままで確定し、この issue を閉じる

## 置き場所

- 段階 1（Lua）: `nvim/lua/dotfiles/refs_usage.lua`
- 段階 2（Ruby）: `nvim/ruby-refs-index/`（nvim 設定ツリーの下。Ruby なので `lua/` には置けない）

## todolist

- [x] 段階 1: 使用実績の記録と `:DotfilesRefsStats`
- [x] 段階 1: LSP 版へのフォールバック用マッピング（`<leader>K`。これが「困った」の観測点）
- [x] 段階 2: 逆引き索引プロトタイプ（`nvim/ruby-refs-index/measure.rb`）
- [x] 段階 2: ①構築時間 ②メモリ ③クエリ時間 ④精度差 の実測
- [ ] 実測を受けての判断（sidecar 化 / rg のまま確定）← **人の判断待ち**

## 進捗: 段階 1 (commit `feat(334): 参照検索の使用実績を記録する`)

`nvim/lua/dotfiles/refs_usage.lua`。`<C-k>` が rg 経路 / LSP 経路のどちらを通ったかを
state ディレクトリの JSONL へ追記し、`:DotfilesRefsStats` で集計を出す。

「困った」の観測点は **`<leader>K`（LSP で引き直す）**。rg で引いた直後に**同じ語**を
これで引き直したときだけ fallback として数える（語が違う / 30 秒より離れているものは
通常の LSP 利用として数える。混ぜると率が水増しされて判断を誤る）。

記録の失敗は握り潰す（書けない環境でも `<C-k>` は動く）。ログは repo に入れない。

変異検証 6 本すべて red（語の一致を外す / 窓を外す / fallback へ書き換えない /
pcall を外す / `<C-k>` の記録を外す / `<leader>K` を消す）。

## 進捗: 段階 2 (commit `feat(334): 呼び出し側索引の実測プロトタイプ`)

`nvim/ruby-refs-index/measure.rb` + README。ワークスペースを 1 回 Prism でパースし、
呼び出しノードと宣言の逆引きを作って、①構築時間 ②メモリ ③クエリ ④rg との精度差を測る。

### 実測 (2026-09-08 / ubiregi-server / ruby 3.1.6 / prism 1.9.0 / 対象 2682 ファイル)

構築 **1.05〜1.11 秒** / メモリ **+36〜41 MB** /
索引は呼び出し 6729 種・315369 箇所、宣言 4219 種・7028 箇所。

| シンボル | クエリ | AST 箇所 | rg 行 | rg のゴミ |
|---|---|---|---|---|
| `wrap_error` | 3 µs | 24 | 24 | 0 |
| `symbolize_keys` | 4 µs | 403 | 404 | 1 |
| `perform` | 5 µs | 779 | 858 | 79 (9%) |
| `account` | 7 µs | 2372 | 22547 | **20282 (90%)** |

比較対象: ruby-lsp の references は同じプロジェクトで **11.2 秒**、rg は **0.104 秒**。

### 読み取れること

- **構築 1 秒・37 MB で O(1) 参照になる**。ruby-lsp が毎回 11 秒かけているのは、
  索引に呼び出し側を持たせていないからで、コストの問題ではない
- **rg 単体運用の弱点は「ありふれた名前」に出る**。`account` で 90% がゴミ。
  逆に固有名 (`wrap_error` / `symbolize_keys`) ではほぼ差が無い
- したがって段階 1 の計測で「よく使う語で困る」が出るなら sidecar 化の価値がある。
  固有名しか引かないなら rg のままでよい

### 実測中に踏んだもの (どちらも計測を静かに壊す形)

- **宣言を索引に入れないと rg の誤検出を過大に見積もる**。最初は呼び出しだけを集めたため
  `def wrap_error` の行が「rg だけに出る」に化けた。`references` は `includeDeclaration` で
  宣言も返すので土俵を合わせる必要がある
- **rg をパス引数なしで呼ぶと stdin を読む**。`Open3` 越しだと 0 バイト検索になり、
  黙って 0 件を返す (`bytes_searched: 0` で気づいた)

### 次の判断材料

sidecar 化するなら、未実装なのは ①常駐と IPC ②保存のたびの差分更新
③定数・名前空間の解決 (今は名前一致のみで、ruby-lsp のメソッド一致と同じ粒度)。

## 追補: 残りの最適化余地を実測した (2026-09-08)

索引完了後のリクエスト別レイテンシ (ubiregi-server / 2 回目の値):

```
hover                   0.4 ms      completion            0.2 ms
definition              0.4 ms      codeAction            0.5 ms
documentSymbol          0.2 ms      documentHighlight     0.7 ms
foldingRange            0.1 ms      formatting            9.7 ms
semanticTokens/full     0.3 ms      workspace/symbol    190.9 ms
references          11563.8 ms  ← ここだけ桁が違う
```

**references 以外は全部 1 ミリ秒未満**。触る価値がない。

### 索引時間は gem 除外ではほぼ削れない (A-B 実測)

大きい gem 10 個 (rubocop / brakeman / solargraph / yard / language_server-protocol /
fog-aws / mongo / capybara / rr / newrelic_rpm) を `excludedGems` で外した:

```
baseline  索引 10.7 秒
除外 10   索引  9.1 秒   ← -1.6 秒 (15%) だけ
```

15% のために gem へのジャンプを失うのは割に合わない (多くが開発グループ専用で既定の
`initial_excluded_gems` に入っていたのが理由と思われる)。永続キャッシュも上流に無いので、
現実的な緩和は「nvim の起動回数を減らす」だけ。

### 残っている唯一の体感課題: 定数・クラスの `<C-k>`

332 でメソッドは rg へ回したが、**定数は LSP 経路のまま = 11.5 秒**。判断材料は段階 1 の
記録で集まる (`:DotfilesRefsStats` の「LSP N 回」が定数を引いた回数)。多ければ sidecar 化の
価値があり、少なければ現状維持でよい。

`vendor/bundle` を repo 外へ出せば、この経路も 11.5 秒 → 約 5 秒になる (references の
再パース対象の 88% が vendor/bundle のため)。索引には効かない (既に除外済み)。

### やらない方がいいと分かったもの

- `enabledFeatures` で機能を減らす: 各リクエストが 1ms 未満なので効果ゼロ。索引は機能フラグと
  無関係に全部作るので索引時間も減らない
- `excludedPatterns` を増やす: 既定で `vendor/bundle` / `tmp` / `node_modules` / dotdir が
  外れており、残りは 10 ファイル規模 (追補 1 の実測)

## 追補: 敵対的レビューの P1 2 件を直した (2026-09-08)

commit `fix(334): 参照検索の記録に filetype を残し、<C-k> の行き先を純関数へ切り出す`。

**P1-1: 記録の分母が「定数を引いた回数」になっていなかった。**
`record` が `{kind, word, at}` しか残しておらず、`kind="lsp"` に **Ruby 以外の全 filetype の
`<C-k>`** が混ざっていた (`use_ripgrep_references` が false を返す経路がそのまま
`record("lsp", ...)` を呼ぶため)。本 issue は「`:DotfilesRefsStats` の LSP 回数 = 定数を
引いた回数」で sidecar 化を判断すると決めているので、Go/TS も触る環境では判断が無関係な数字の
上に乗っていた。しかも ft がログに無いので**事後に分離もできない**。

→ `record(kind, word, ft)` にして entry へ `ft` を残し、`stats()` が `by_ft` で層別する。
`format_stats()` は **Ruby の行を先に出す** (判断に使う数字がどれかを取り違えないため)。
ft を持たない古い行は `no_ft` として別に数える。時刻も `%z` 付きに変えた。

**P1-2: `<C-k>` の分岐を丸ごと反転させても全テストが緑だった。**
配線の固定が「マッピングのブロックに文字列が存在するか」しか見ておらず、
①rg と LSP の入れ替え ②`word_match = "-w"` の削除 ③`cwd` を nvim の cwd に固定、が
すべて素通りしていた。

→ 行き先を **`M.references_action(filetype, word, root)` の返り値で表明する**純関数へ切り出し、
テーブル駆動で返り値そのものを assert する。静的 pin は「その関数を通っているか」の 1 点に縮めた。

**あわせて P2-1 も直した** (切り出した当の行だったため): `cwd` を `on_attach` が受け取った
`client` から取ると、キーマップが LspAttach ごとに貼り直される都合で**後から attach した
client の root** を掴む。lspconfig の solargraph の filetypes は `{ "ruby" }` で eruby を
含まず、tailwindcss は `erb` / `eruby` を含むので、`.erb` では tailwind の root が入りうる。
→ `M.ruby_root_for(bufnr)` が Ruby のサーバを**名前で**選び、無ければ repo 境界へ落とす。

### 変異検証 (7 本すべて red)

| 変異 | 結果 |
|---|---|
| `references_action` の分岐を反転 | RED |
| `word_match = "-w"` を落とす | RED |
| `cwd` を nvim の cwd に固定 | RED |
| `ruby_root_for` が client 名で絞らない | RED |
| `record` が ft を捨てる | RED |
| `stats` が層別しない | RED |
| `<C-k>` が record に ft を渡さない | RED |

上 4 本はレビュー前は**すべて緑で通っていた**もの。

### 残り (未着手)

- P2-2: client が終了すると statusline の「実行中」表示が永久に残る (`LspDetach` で掃除していない)
- P2-3/P2-4/P3-5: `measure.rb` が rg の rc=2 を 0 件に畳む / production に無い `--type ruby` で
  絞っている / 「行数」と「出現数」を並べている
- P3-2: `vim.lsp.status` 直呼び禁止の pin が `lualine_x` の中しか見ていない
- P3-6: JSONL にローテーションが無い (判断が出たらログごと削除する)
