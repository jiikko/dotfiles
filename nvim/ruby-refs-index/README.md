# ruby-refs-index — 呼び出し側索引の実測プロトタイプ

issue [334](../../issues/done/334-ruby-references-call-site-index.md) の段階 2。
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

| シンボル | クエリ | AST 行 | rg 行 | rg のゴミ |
|---|---|---|---|---|
| `wrap_error` | 10 µs | 24 | 59 | **35 (59%)** |
| `symbolize_keys` | 9 µs | 403 | 445 | 42 (9%) |
| `perform` | 4 µs | 779 | 927 | 148 (16%) |
| `account` | 9 µs | 2265 | 26662 | **24397 (92%)** |

（ruby-lsp の references は同じプロジェクトで 11.2 秒）

**読み取れること**: 構築 1 秒・37 MB で O(1) 参照になる。**rg のゴミは固有名でも無視できない**
（`wrap_error` で 59%）。ありふれた名前では 9 割を超える（`account`）。

🚨 **この表は 1 度誤った数字を出している**。当初は計測側だけ `--type ruby` で絞っており、
production の `telescope.grep_string` はタイプを絞らない（`.erb` / `.yml` / `.js` も舐める）ので
**土俵がずれて rg のゴミを過小評価していた**（`wrap_error` は「ゴミ 0」に見えていた）。
比較する数字は、production と同じ条件で測ること。

🚨 **宣言も索引に入れること**。最初は呼び出しだけを集めたため、`def wrap_error` の行が
「rg だけに出る」に化けて rg の誤検出を過大に見積もった（実測で踏んだ）。`references` は
`includeDeclaration` で宣言も返すので、比較の土俵を合わせる必要がある。

🚨 **rg をパス引数なしで呼ばないこと**。stdin が tty でないと rg は **stdin を読む**ので、
`Open3` 越しだと 0 バイト検索になって黙って 0 件を返す（同じく実測で踏んだ）。

🚨 **rg の rc は 0=マッチあり / 1=マッチ無し / 2=エラー**。`status.success?` だけで判定すると
**エラーが「ゴミ 0 件」に化けて、rg が完璧だという逆の結論が出る**。`-F`（リテラル扱い）も付ける
（付けないと `valid?` の `?` が正規表現として解釈される）。

🚨 **単位を揃えること**。rg は「行」、AST は「出現」を数えるので、そのまま並べると比較できない
（同じ行に 2 回出る呼び出しは AST では 2、rg では 1）。突合はユニーク行どうしで行う。

## まだやっていないこと

この索引を実際に使うには、常駐させて nvim から引く（sidecar）形が要る。差分更新
（保存のたびに変更ファイルだけ張り替える）も未実装。やるかどうかは issue 334 で判断する。
