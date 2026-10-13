# 749 (bug): glogx の issues viewer の見張りの基準 (指紋) を読み終えた後に取るため、走査中に入った編集を拾わない

起票日: 2026-10-13

## 概要

`src/glogx/issues_view.go` の `scanIssues` は `issues.Scan` → 全件の `LoadMeta` → `issuesFingerprint` の順で動き、`receive` はこの指紋を見張りの基準 (`seen`) にする。
読みと指紋の間に外部が書いた変化は、一覧には入らないのに基準には入る。その後の観測は `fp == seen` で「変化なし」になり、次の編集まで一覧が古いまま残る。
30 秒のポーリングでも戻らない (分類: concurrency K1)。

## 詳細

- 発火条件: ある issue の `LoadMeta` が終わってから指紋を取るまでの間に、その issue が編集される。`ReadDir` の後に作られたファイルも同じ
  (ディレクトリの mtime が基準に入る)。窓の幅は Scan と LoadMeta の所要で、実 repo の約 700 件で数十 ms (監査体の見積もり。実測ではない)
- 再現: 監査体が scratchpad の複製で `TestAuditScanFingerprintTakenAfterRead` を書いた。400 件の走査中に、0〜60ms の遅延を掃引して外部から書き込ませ、
  遅延 42.5ms の 1 回で「一覧は古い見出しなのに、基準の指紋は編集後と一致」が出た。頻度は低い
- 既存テストの穴: `TestIssuesWatchCatchesEditRacingTheBaseline` が見ているのは、`scanIssues` が返った後の編集だけ
- `go test -race` では出ない (data race ではなく、読みと基準の順序の問題)

## 対応方針

- 指紋を読みより前に取る (Scan → 指紋 → LoadMeta。ディレクトリの mtime は ReadDir の前に Stat する)。基準が古い側に寄れば、次の観測で差が出て取り直しになる
- または読みの前と後で指紋を取り、違えば取り直す (pending に入れる)
- 回帰テストは scanIssues の中に差し込み口を置き、読みの途中で書き込んで、次の観測で取り直しになることを見る

## 関連ファイル

- `src/glogx/issues_view.go` (`scanIssues` / `receive`)・`src/glogx/issues_watch.go` (`issuesFingerprint`)
- 監査の記録: 754

## 進捗

- [ ] 未着手
