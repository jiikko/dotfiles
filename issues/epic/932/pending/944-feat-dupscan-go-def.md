# 944 (feat): Go の定義を本体つきで引く def (保留)

起票日: 2026-10-28

> **保留** (2026-10-28)。**再開の条件**: [943](../943-perf-dupscan-result-row-shape.md) の終端行を入れた後も、Go のセッションで `dupscan index` / `callers` の直後に**範囲指定のない Read** (ファイル全体) が続く割合が高いと分かったとき。それまでは実装しない (932 の方針: 条件付きの設計は一括実装しない)。

## 概要

`dupscan def` / `refs` / `context` は Swift だけを読み、Go では `go のファイル N 個は def / refs の対象外 (その言語の定義・参照は LSP で引く)` と返す。「メソッド定義を探すときは dupscan」を Go でも成立させるには、`def` が Go の本体を返せるかが分かれ目になる。現状 Go で dupscan が答えるのは `index` (場所・署名・doc 先頭) と `callers` までで、本体を見るには Read が要る。

## 現状 (2026-10-28 に確認)

- `dupscan def ReadCookies` (dotfiles) は「定義なし」と対象外の通知を返す。
- `index` / `callers` は Go を解析する。構文木 (go/ast) の単位抽出は既にあり、本体の範囲を出す材料は揃っている。
- LSP (gopls) 経由の費用と実際の利用回数は**未計測**。[899](../done/899-research-dupscan-lsp-integration.md) は dupscan 自身が LSP を採らない方針を維持しており、本 issue もそれを変えない。

## 設計の候補 (再開時に見直す)

- Swift の `def` と同じ出力契約 (本体つき・`Type.member` で絞る・`-max-lines`) を、**構文だけ**で Go に広げる。型は解決しない: 同名の別 receiver・埋め込みで昇格したメソッド・interface の実装は区別できないと明記し、`0 件は不存在の証明ではない` を保つ。
- 一括で `refs` / `context` まで広げない。`def` だけを先に切り出す。
- 実装するなら [939](../done/939-ux-dupscan-go-grep-nudge.md) の hook の案内文 (「def / refs / context は Go 非対応」) と skill の 2 節 (「Go の定義・参照は LSP で引く」) を同じ変更で直す。

## 再開時の受け入れ条件

- [ ] 再開条件の集計 (transcript で `index` / `callers` の直後の Read に offset / limit があるか) を結果つきで書く。
- [ ] 本体つき `def` で 1 回の探索が `index` + 範囲 Read より小さいか、byte で比べる。
- [ ] 同名の別 receiver がある Go の fixture で、混ざる/混ざらないの扱いを test で固定する。
