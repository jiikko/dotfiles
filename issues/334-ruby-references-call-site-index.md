# Ruby の参照検索: 使用実績の計測と、呼び出し側索引プロトタイプ

種別: research / perf
起票: 2026-09-08
前提: [332](332-ruby-lsp-selection-by-probe.md) の追補 3（`<C-k>` を Ruby のメソッドだけ ripgrep へ振り分けた）

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
