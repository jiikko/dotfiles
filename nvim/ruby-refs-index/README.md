# ruby-refs-index — 呼び出し側索引の実測プロトタイプ

issue [334](../../issues/334-ruby-references-call-site-index.md) の段階 2。
**製品コードではなく、判断のための計測**。

## 何のために在るか

ruby-lsp の `textDocument/references` は索引を使わず、要求のたびにワークスペース全体を
Prism で再パースする（`requests/references.rb:63` の生 `Dir.glob`）。索引
（`ruby_indexer::Entry`）が持っているのは宣言だけで、呼び出し側の逆引きが無いため。
実測 11.2 秒（ubiregi-server）。上流は [#3051](https://github.com/Shopify/ruby-lsp/issues/3051)
を closed as not planned にしている。

🚨 **addon では references を差し替えられない**（ruby-lsp 0.26.11 で確認）。`RubyLsp::Addon` の
公開フックは code_lens / hover / document_symbol / semantic_highlighting / definition /
completion / discover_tests の 7 つだけで、`server.rb:799` は `Requests::References` を
直接生成する。だからこれは「組み込む実装」ではなく、**呼び出し側索引が現実的かを数字で
判断するための計測**にとどめてある。

## 使い方

```sh
ruby nvim/ruby-refs-index/measure.rb <workspace> <method_name> [--all-files]
```

- 既定は `rg --files` と同じ範囲（`.gitignore` 尊重）。比較の土俵を rg に合わせるため
- `--all-files` で ruby-lsp と同じ生 `Dir.glob(**/*.rb)` にする（`vendor/bundle` を含む）

出力は ①構築時間 ②メモリ増分（RSS）③クエリ時間 ④ripgrep との精度差。

## 実測 (2026-09-08 / ubiregi-server / ruby 3.1.6 / prism 1.9.0)

対象 2682 ファイル。構築 **1.05〜1.11 秒** / メモリ **+36〜41 MB** /
索引は呼び出し 6729 種・315369 箇所、宣言 4219 種・7028 箇所。

| シンボル | クエリ | AST 箇所 | rg 行 | rg のゴミ |
|---|---|---|---|---|
| `wrap_error` | 3 µs | 24 | 24 | 0 |
| `symbolize_keys` | 4 µs | 403 | 404 | 1 |
| `perform` | 5 µs | 779 | 858 | 79 (9%) |
| `account` | 7 µs | 2372 | 22547 | **20282 (90%)** |

（ruby-lsp の references は同じプロジェクトで 11.2 秒）

**読み取れること**: 構築 1 秒・37 MB で O(1) 参照になる。ありふれた名前ほど rg は
ゴミだらけになる（`account` で 90%）ので、rg 単体運用の弱点はここに出る。

🚨 **宣言も索引に入れること**。最初は呼び出しだけを集めたため、`def wrap_error` の行が
「rg だけに出る」に化けて rg の誤検出を過大に見積もった（実測で踏んだ）。`references` は
`includeDeclaration` で宣言も返すので、比較の土俵を合わせる必要がある。

🚨 **rg をパス引数なしで呼ばないこと**。stdin が tty でないと rg は **stdin を読む**ので、
`Open3` 越しだと 0 バイト検索になって黙って 0 件を返す（同じく実測で踏んだ）。

## まだやっていないこと

この索引を実際に使うには、常駐させて nvim から引く（sidecar）形が要る。差分更新
（保存のたびに変更ファイルだけ張り替える）も未実装。やるかどうかは issue 334 で判断する。
