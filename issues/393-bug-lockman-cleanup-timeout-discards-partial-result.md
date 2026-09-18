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


## 進捗 (2026-09-19): 実装完了・敵対レビュー待ち

### 候補の選択: **atomic のカウンタ** (issue の第一候補「removed=不明」は採らなかった)

| 候補 | 判定 |
|---|---|
| (a) 部分結果 (`res`) をそのまま返す | **却下**。見捨てた goroutine がまだ `res` を書いているのでデータ競合 (issue 本文どおり) |
| (b) **atomic で件数を持たせ、期限切れ時点の値を読む** | **採用** |
| (c) `removed=不明` と報告する | **却下**。数を偽らない点は解決するが、issue の「なぜ気にするか」の**2 番目** (排水中か 1 件も進まないかの区別) が解けない。`removed` は部分進捗を見るための唯一の出力なので、「不明」に固定しても「本当に 1 件も進まない状態」と区別が付かないまま |

### 実装

- `cleanupProgress` (atomic.Int64) を新設し、`Cleanup(force bool, progress ...*cleanupProgress)` で受ける。
  `sweepDir` は消すたびに `res.Removed++` と `p.removed.Add(1)` の**両方**を進める
- `CleanupTimed` は期限切れ時に `p.removed.Load()` のスナップショットと `Partial: true` を返す。
  **`res` は読まない** (データ競合)
- `CleanupResult.Partial` を新設。true = 「**少なくとも** N 件。掃除は継続中」
- `dispatch` は `removed=N` と `removed>=N (期限切れ。掃除は途中で、次回の続きから減る)` を書き分ける
- 🚨 **progress は可変長引数**。掃除の入口を 2 つに割ると `timeout_wiring_test.go` の gate
  (「生の I/O は timeout.go からしか呼ばない」) が見ている名前と実際の入口がずれる。
  実際に `cleanupWithProgress` を別メソッドへ切り出したら **gate が落ちた** (= gate は正しく働いた)。
  入口を 1 つに保ち、渡さない呼び出し側 (テスト 20 箇所) は今までどおり書ける

### テスト (新設 3 本。`cleanup_partial_test.go`)

`.cleanup_at` を FIFO にすると **sweep が終わった後の `stampCleanup` で詰まる**ので、
「消した件数 > 0 なのに期限切れ」を決定論的に作れる (壁時計に依存しない)。

| テスト | 何を固定するか |
|---|---|
| `TestCleanupTimedReportsPartialProgress` | 期限切れでも実数 (5 件) と `Partial` を返す。**FS 側でも実際に消えたことを確認**する (報告だけが正しい形を防ぐ) |
| `TestCleanupMarksCompleteRunAsNotPartial` | 対照。完走した掃除に `Partial` が立たない (「常に部分的」へ倒れていない) |
| `TestCleanupCLIDistinguishesPartialFromComplete` | CLI の出口で `removed>=N` が出る (書式の書き分けが production に届いている) |

### 変異検証

| 変異 | 結果 |
|---|---|
| 期限切れ時に部分結果を捨てる (旧挙動) | `TestCleanupTimedReportsPartialProgress` + `TestCleanupCLIDistinguishesPartialFromComplete` **FAIL** / 対照の `TestCleanupMarksCompleteRunAsNotPartial` と既存の `TestIOTimeoutWrapsDeferredCleanup` は PASS |
| CLI の書き分けを潰す (常に `removed=N`) | `TestCleanupCLIDistinguishesPartialFromComplete` **FAIL** |

### 結果

- `go test -race ./...` 緑 (34.3s) / `go vet` 緑
- `dispatch` のコメント (「失敗している場面でこそ知りたい」) と実装が一致した

## 残タスク

- [ ] 反証レビュー (**実施中**。観点: データ競合 / 数字の嘘 / 可変長引数の設計 / テストの穴)
- [x] 候補の選択 → (b) atomic のカウンタ。(c) は「排水中か 1 件も進まないか」を区別できないので却下
- [x] `dispatch` のコメントと実装を一致させた (`removed=N` / `removed>=N` の書き分け)
