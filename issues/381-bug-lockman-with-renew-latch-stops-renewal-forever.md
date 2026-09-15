# `lockman with` の renew ラッチが「詰まったら二度と更新しない」— 自分が禁じた失敗を別の形で作り直している

起票日: 2026-09-16
カテゴリ: bug / priority: **high**
対象: `src/lockman/with.go` の select ループ (`renewCh` / `renewExpired`) と `on_lost_kill_test.go`
出典: [issue 366](366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の敵対的レビュー (観点③ 並行・中断)
反証レビュー: 未実施

## 問題

`with.go` は更新中の goroutine が溜まらないよう `renewCh != nil` のあいだ tick を捨てる:

```go
case <-ticker.C:
    if renewCh != nil {
        continue // 前回の更新がまだ返っていない。新しく積まない
    }
    renewCh, renewExpired = l.renewAsync(meta.Token)
```

`renewCh` が nil に戻るのは **`case err := <-renewCh:` (詰まった `Renew` が返った) ときだけ**。
`renewExpired` は一度発火すると nil にされ、二度と鳴らない。したがって
**`Renew` の syscall が返らない限り、以後の tick はすべて `continue` で更新は永久に起きない**。

これは同じファイルの 🚨 が禁じている失敗そのもの:

> 🚨 **詰まっても更新をやめない。** 以前ここで `ticker.Stop()` していたが、それは
> 一過性のヒカップを**本物の lease 喪失**に変える: 更新が二度と走らないので lease は
> 実際に期限切れになり、他マシンが正当に引き継ぐ — 子はまだ走っているので二重実行。

`ticker.Stop()` を外して塞いだつもりの穴が、`renewCh` ラッチという別の形で残っている。
`case <-renewExpired:` のコメントは「詰まった 1 本が返れば renewCh が降りて再開する」と
書いており、**返らない場合を想定していない**。

## 二重実行になる列 (`--on-lost=warn`)

1. `t0`: `lockman with --on-lost=warn --ttl 30m -- job`。Acquire 成功 (token T1)。tick = 10m
2. `t0+10m`: tick → `renewAsync` の goroutine が `Renew` の中でブロック (マウント停止)
3. `t0+10m+io-timeout`: `renewExpired` 発火 → warn 1 回。`onLostKill == false` なので子は殺さない。
   `renewCh` は非 nil のまま
4. `t0+20m, 30m, …`: すべての tick が `continue`。**更新 0 回**
5. `t0+30m`: lock の mtime は `t0` のままなので lease が期限切れ
6. 別ホストが引き継ぐ。**子はまだ走っている = 2 人が同時に保持**

既定の `--on-lost=kill` では `escalateGroupKill` が子を落とすので、露出は `setsid()` した
子孫まで縮む (`with.go` が既知として記載済み)。**`--on-lost=warn` が本命**。

## テストが作っておきながら検査していない

- `on_lost_kill_test.go` の `TestRenewDoesNotPileUpGoroutinesWhenBlocked` は
  **恒久ラッチ状態を作っている** (`syscall.Mkfifo` で作った FIFO を lock に被せ、
  書き込み側を誰も開かないので `Renew` の read は返らない)。ところが assert は
  **exit code と `runtime.NumGoroutine()` の差だけ**で、**lease が生きているかを一度も見ていない**
  (実測 2026-09-16: 当該関数内の assert は 5 つで、`Inspect` / `readLock` / `Acquire` の
  呼び出しは 0 件)
- `TestLeaseSurvivesTransientRenewBlock` は**一過性**だけ。手順の途中で FIFO を退けて
  詰まった読み手を解放するので、`renewCh` が必ず返る条件でしか回していない

つまり「詰まっても更新をやめない」は *一過性* についてのみ守られており、*恒久* については
**テストが状態を作っておきながら検査していない**。

## 未確認

- 手順 2 の syscall が返らない (hard mount / stale handle) ことは実機で確認していない。
  マウントが復旧すれば goroutine も解放されてラッチが外れるので、噛むのは
  「返らない」か「復旧よりかなり遅れて返る」場合に限る
- ただし**テストの穴** (恒久ラッチを作って lease を見ていない) はこの前提なしで成立する

## 残タスク

- [ ] `TestRenewDoesNotPileUpGoroutinesWhenBlocked` に「lease が生きているか」の assert を足す
      (今の状態で red になるはず。ならないなら前提が作れていない)
- [ ] 恒久ラッチを解く設計を決める (例: `renewExpired` を発火のたびに張り直して次の tick で
      新しい更新を積む。ただし「見捨てた goroutine が溜まる」上限との両立が要る —
      [359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) の項目 4 が出典)
- [ ] `--on-lost=warn` での二重実行を A-B で実測する

## 関連

- [362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) — 見捨てられた goroutine が
  副作用を残す族。本 issue は「見捨てた**後**に更新を再開しない」側
- [380](380-bug-lockman-renew-and-release-act-on-name-after-check.md) — `Renew` 自体の TOCTOU
