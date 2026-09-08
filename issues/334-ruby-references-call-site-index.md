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

- [ ] 段階 1: 使用実績の記録と `:DotfilesRefsStats`
- [ ] 段階 1: LSP 版へのフォールバック用マッピング（これが「困った」の観測点になる）
- [ ] 段階 2: 逆引き索引プロトタイプ
- [ ] 段階 2: ①構築時間 ②メモリ ③クエリ時間 ④精度差 の実測
- [ ] 実測を受けての判断（sidecar 化 / rg のまま確定）
