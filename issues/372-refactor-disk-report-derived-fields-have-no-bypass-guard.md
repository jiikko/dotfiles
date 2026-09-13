# 372 refactor: disk.Report/Result の導出フィールドを「所有者を迂回して」書く経路を機械で止めていない

- 起票: 2026-09-13
- 種別: `refactor` (現状は全経路が正しい。次に足す人への防御)
- 出典: audit の `encapsulation` (E2 不変条件の外部維持) で見つけた 2 件を
  `disk.Report.WithResults` / `disk.Result.WithItems` へ寄せた commit の残課題

## 何が残っているか

`Report.Total` は `Results` の導出値、`Result.Size` は `Items` の導出値 (例外: Items を持たない
Reused / FromSnapshot は Size を保つ)。この 2 つの整合は
**`WithResults` / `WithItems` を通ったときだけ**保たれる。

寄せた結果、いま導出フィールドへ直接書く箇所は所有者パッケージ内の以下だけ:

- `src/doctor/disk/report.go` の `WithResults` / `WithItems` (唯一の出典)
- `src/doctor/disk/scan.go` の `Items = append(...)` / `Size += ...` (生成時。対で書いている)

**ただし「次に足す人が `rep.Results = xs` と直接書く」のを止めるものは無い**。build もテストも通り、
合計だけが古いまま残る (silent)。寄せる前の glogx 側 3 箇所がまさにその形だった。

## 発火条件

- 新しい経路が `Results` / `Items` を差し替えて `WithResults` / `WithItems` を通さないとき
- 症状は「行は正しいのに画面上部の合計だけ古い」「消したのに減らない」

## 修正方向 (どちらか)

1. **ソース走査テスト**を足す (前例: `src/glogx/issues_rows_setter_test.go` が
   `issuesView.rows` の直接代入を AST で止めている)。所有者パッケージの外から
   `.Total =` / `.Size =` / `.Items =` を書く形を違反にする
2. **導出値をフィールドから外す** (`Total()` / `Size()` をメソッドにする)。JSON の
   スキーマ (`json:"total"` / `json:"size"`) が snapshot の保存形式なので、
   互換のために出力時だけ埋める形になる。影響が大きい

🚨 1 を採るなら、それ自体が「自作の検査」なので
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) の
手順 (脅威モデルと「検出しない形」を先に書く / canary は本走査と同じ関数を通す / 変異で red を見る) を
通すこと。`issues_rows_setter_test.go` のヘッダがその作法の実例になっている。

## 進捗

- 2026-09-13 `refactor(disk): 導出フィールドの整合を Report.WithResults / Result.WithItems へ寄せる`
  — 呼び出し側 8 箇所 (disk 3 / glogx 5) を所有者の API へ寄せ、glogx 側で導出フィールドを手で書く
  箇所は 0 件になった。変異 3 本 (例外ガード除去 / Size 引き直し除去 / Total 引き直し除去) で red を確認。
  **本 issue が言う「迂回の機械的な禁止」はこの commit には入っていない**

## 残タスク

- [ ] 1 と 2 のどちらを採るか決める (未着手)
- [ ] 採った方を実装する (未着手)
