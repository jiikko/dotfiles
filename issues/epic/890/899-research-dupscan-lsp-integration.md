# 899 (research): dupscan と LSP・言語の解析の道具を組み合わせる (呼び出し元・型・値の解決)

起票日: 2026-10-24

## 概要

[891 (要件の正本)](891-design-dupscan-requirements.md) の「構想: LSP と組み合わせる」節。dupscan の出力のうち構文木だけでは不正確なもの (呼び出し元の数・プロパティの型と値・所属ターゲット) を、
LSP (sourcekit-lsp・gopls) か LSP ではない言語の解析の道具 (Swift の index store・Go の `go/packages`) から取れるかを調べる。
書く前に探す手順 (LSP の名前と、dupscan の索引の署名の型・doc 文を並べる) は [903](903-docs-dupscan-search-before-write-procedure.md) で書く。
obaket で LSP を使う手順 (初回の待ち・worktree ごとの索引・結果の絞り方・マクロの重なり) は obaket issue 1299。

2026-10-24 の調査 ([901](901-research-dupscan-existing-tools.md)) で、名前で引く手段は Claude Code の LSP ツール (組み込み) と Serena (MCP。Swift・Go・Bash を含む 40 以上の言語) にあると分かった。
この issue は「dupscan の出力を補う値をどこから取るか」に絞り、名前で引く手段そのものは作らない。

## 受け入れ条件

- [ ] 取る手段ごとに、取れるもの・所要・前提 (索引の有無・ビルドの要否) を表にして書く
- [ ] 採る手段と、取れないときに構文木での近似と分かる形で出す方法を決める

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 名前で引く手段は既にあることを書き、範囲を絞った (901)
