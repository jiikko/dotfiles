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
- **F2 (遅い・初回のみ)**: 初回の `dupscan capabilities` が `dupscan: building...` で待たされた。所要は下の「F2 の計測」。

## 副産物 (dupscan ではなく運用)

- obaket の `bin/test-shared --filter "<@Suite の表示名>"` が 0 件実行のまま rc=0 になっていた。dupscan の問題ではないので obaket 側で直した (obaket `1ea0b8a17`)。

## F2 の計測 (2026-10-28)

新しい worktree (`bin/dupscan` の build 先が無い状態) で `time bin/dupscan capabilities`:

| 回 | 所要 |
|---|---|
| 1 回目 (`dupscan: building...` が出る) | 1.856 秒 |
| 2 回目 | 0.025 秒 |

- **測った条件**: Go の build キャッシュ (GOCACHE) は温まっていた (他の worktree で何度も build 済み)。GOCACHE が空の完全な初回は**未計測** (キャッシュを消す操作になるので測っていない)。
- 気づく長さではあるが、待ちとしては 2 秒弱。

## 決着

- F1: 見送り。942 で縮めた 1 行で、欠けうることを知らせる安全の通知なので消さない。減らす方向は 942 / 946 (読めない構文そのものを減らす)。
- F2: 見送り。GOCACHE が温まっていれば 2 秒弱。GOCACHE が空の環境 (新しい機械・CI) で分単位かかると分かったら、そのとき別 issue にする。
- 副産物 (`bin/test-shared`): obaket `1ea0b8a17` で対応済み。
