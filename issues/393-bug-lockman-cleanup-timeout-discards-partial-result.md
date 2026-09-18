# 期限切れした cleanup が `removed=0` と報告する (実際は数千件消している)

起票日: 2026-09-18
カテゴリ: bug / priority: **medium**
対象: `src/lockman/timeout.go` の `CleanupTimed`、`src/lockman/main.go` の `dispatch`
出典: [issue 391](done/391-bug-lockman-os-exit-skips-defer-leaves-scratch.md) の反証レビュー
反証レビュー: 未実施。**数値は起票者の実測**（下表）

## 問題

`CleanupTimed` は期限切れのとき、**掃除の部分結果を捨てて** `CleanupResult{Errors: [...]}` を返す:

```go
res, err := withTimeout(l.timeout, func(_ *abandon) (CleanupResult, error) { return l.Cleanup(force), nil })
if err != nil {
    return CleanupResult{Errors: []string{err.Error()}}   // ← res を捨てている
}
```

`dispatch` はこれを `removed=%d` で表示する。つまり**実際には数千件を削除しているのに
`removed=0` と出る**。

`dispatch` のコメントは、まさにこの場面を名指しして逆のことを約束している:

> 件数も一緒に出す: 部分的に進んでいるのか何も進んでいないのかは、**失敗している場面でこそ知りたい**

**その唯一の場面で数字が嘘になる** (「何も進んでいない」と読める値が出る)。

## 実測 (2026-09-18、ローカル APFS)

tmp/ に 2 時間前に打刻した残骸 10,000 件を置き、`.cleanup_at` を 20 分前にして
`acquire --io-timeout 100ms` を 3 回:

| 回 | rc | tmp 残 | 実際に減った数 | 報告 |
|---|---|---|---|---|
| 1 | 0 | 7,451 | **2,549** | `removed=0 errors=[I/O timeout]` |
| 2 | 0 | 5,019 | **2,432** | `removed=0 errors=[I/O timeout]` |
| 3 | 0 | 2,564 | **2,455** | `removed=0 errors=[I/O timeout]` |

3,000 件まで減ると 1 回で完走し、`removed=<実数>` が出て打刻もされる。

## なぜ気にするか

1. **人が「掃除が全く進んでいない」と誤診する**。実際は排水中で、数回放っておけば収束する。
   誤診すると `rm -rf` のような手作業へ誘導しうる (lock dir に対して最も危険な操作)
2. **`removed` は「部分的に進んでいるか」を見るための唯一の出力**。ここが 0 に固定されると、
   「掃除が本当に 1 件も進まない状態」(権限ドリフト等) と**区別が付かない**

## 対応の候補 (未決)

- `withTimeout` の期限切れ時に、**部分結果を捨てずに返す**。`Cleanup` は
  `res` をポインタで積んでいくので、期限切れ時点のスナップショットを読むと
  **データ競合**になる (内側の goroutine はまだ書いている)。素朴にはできない
- 件数を `atomic` で持たせ、期限切れ時にその時点の値を読む
- 報告を「`removed=不明 (期限切れ。実際には進んでいる可能性がある)`」にする。
  🚨 **数を偽るより「分からない」と言うほうが正直**で、
  `adversarial-review-own-safeguards.md` の「判定不能を専用の値で出す」に沿う。**これが第一候補**

## 残タスク

- [ ] 反証レビュー
- [ ] 候補の選択。**データ競合を作らないこと**が制約 (見捨てた goroutine はまだ書いている)
- [ ] 選んだら、`dispatch` のコメント (「失敗している場面でこそ知りたい」) と実装を一致させる
