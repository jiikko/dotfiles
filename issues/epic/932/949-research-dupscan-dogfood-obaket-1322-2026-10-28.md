# 949 (research): dupscan dogfood 所見 — obaket issue 1322 の小さなテスト確認 (2026-10-28)

起票日: 2026-10-28

## 概要

obaket の issue 1322 (重複テストの削除の記録と実行確認) を dupscan-dogfood で進めたときの所見。使った場面が 2 回だけの小さな作業で、所見も軽い。
**自分の所見だけを書いた記録で、既存 issue (942 / 948 等) との重複は調べていない** (重複していたら統合してよい)。

## 使った場面

| 場面 | コマンド | 結果 |
|---|---|---|
| 削除したテストが使う helper の呼び出し元 | `dupscan index -name assertStateChangeWaiterCancellation` / `dupscan callers -limit 0 assertStateChangeWaiterCancellation` | 定義 1 件・呼び出し元 1 件 (`shared/Tests/ObaketCoreTests/StateChangeSignalTests.swift`)。macOS 側に呼び出しが残っていないと言えた (役に立った) |

grep に戻した回数は 0。

## 所見

- **F1 (雑音・軽微)**: obaket (Swift) で `index` / `callers` を打つと毎回 `dupscan index: 読めないファイル 57 個は問い合わせの名前を含まない` の 1 行が stderr に付く。942 の縮小後の形で、この問い合わせでは全文にはならなかった。1 行なので害は小さいが、毎回出る。
- **F2 (遅い・初回のみ)**: 初回の `dupscan capabilities` が `dupscan: building...` で待たされた。**所要は測っていない** (未計測)。

## 副産物 (dupscan ではなく運用)

- obaket の `bin/test-shared --filter "<@Suite の表示名>"` が 0 件実行のまま rc=0 になっていた。dupscan の問題ではないので obaket 側で直した (obaket `1ea0b8a17`)。
