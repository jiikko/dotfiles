# 614 (test): Go のテストが本番の猶予・lease・ticker を実時間で消化している (lockman 42 s / pro-con の 5 s 級 3 本)

> 🚨 **担当中: dotfiles-38**（2026-10-02〜）

起票日: 2026-10-02

## 概要

[613](613-test-shell-tests-wait-with-sleep.md) の Go 版。`src/*/` の `*_test.go` で実時間を待つ箇所を全数分類した:
`time.Sleep(` 86 行 + 子プロセスとして起動する `sleep` 91 行 = 177 行 (sonnet の read-only 調査を main が抜き取りで検閲)。

**時間を食っているのは `time.Sleep` の行そのものより、本番の定数 (猶予・lease・ticker の周期) を実時間で過ぎさせている形**だった。
`time.Sleep` を grep しても見えない (例: `runKillGrace` の 5 s は、`sleep 60` の子を起動して止める DUMMY の行が消化している)。

## 全数勘定 (177 行)

| 分類 | Sleep | 子の sleep | 計 |
|---|---|---|---|
| ポーリングの刻み (TICK) | 52 | 7 | 59 |
| ダミープロセス (DUMMY) | 1 | 65 | 66 |
| 窓・遅い処理を演じる入力 (WINDOW) | 16 | 11 | 27 |
| lease / 期限を実時間で過ぎさせる (TTL) | 11 | 1 | 12 |
| 起きないことの確認 (NEGATIVE) | 4 | 0 | 4 |
| 素の固定待ち (FIXED) | 1 | 0 | 1 |
| 本番の ticker を実時間で回す (TICKER) | 1 | 0 | 1 |
| 文字列・コメントに出るだけ | 0 | 7 | 7 |

TICK 52 行のうち 35 行は、同じ module に待ちの helper があるのに手書き (pro-con 本体 12 / lockman 8 / pro-con/dispatcher 8 ほか)。
同型の helper も 5 実装ある (pro-con の `waitUntil` と `eventually`・wake と relay の `waitFor`・process_supervisor の `eventually`)。

## 実測 (2026-10-02、この Mac、ロードアベレージ 3〜4、`go test -count=1 -v ./...`)

package: lockman 42.3 s / pro-con/dispatcher 25.1 s / pro-con 23.3 s / pro-con/wtclean 15.2 s / restartable は最大 2.8 s (並列で問題なし)。

| テスト | 時間 | 作っている待ち |
|---|---|---|
| lockman `TestDefaultKillWaitsForLeaseDeadline` | 4.53 s | 子の `sleep 3` + 更新の詰まり 0.5 / 1.5 s |
| lockman `TestLeaseSurvivesLongHealthyRun` | 4.03 s | 子の `sleep 4` + `time.Sleep(2700ms)` (更新 ticker 300 ms の 9 周) |
| lockman `TestLeaseSurvivesPermanentRenewBlock` | 4.02 s | 子の `sleep 4` + 0.8 / 1.6 s |
| lockman `TestRenewDoesNotPileUpGoroutinesWhenBlocked` ほか 3 s 級 5 本 | 各 3.0〜3.2 s | 子の `sleep 3` が lease の期限を跨ぐ |
| lockman `TestWaitLoopDoesNotWarnPerRetry` | 3.01 s | `--wait 3s` と backoff 1 s (`main.go` の取得の待ち) |
| lockman `TestEscalateSendsTermWhenChildIsAlive` | 3.01 s | `escalateGroupKill(..., 3*time.Second)` を同期で呼ぶ (猶予 3 s は意図的。コメントに理由: 10 ms では記録が SIGKILL に間に合わず偽赤) |
| pro-con `TestExecRunnerLockedCancelKillsStubbornChild` / `TestExecRunnerCancelKillsStubbornChild` | 5.35 / 5.04 s | `runKillGrace = 5s` (`dispatcher/runner.go` の `const`。SIGTERM → SIGKILL の猶予) を満額 |
| pro-con `TestServeStopsOnSignalWhenSpawnedByScreen` | 5.07 s | Owner の画面があるケースで `signalGrace` (5 s、`var`) を満額 |
| pro-con `TestScreenFollowWaitsForFirstFrame` | 2.00 s | `--timeout 2s` を実時間で消化 |
| pro-con `TestRequestStop` / `TestStopTakesOverWhenStopperDies` | 1.21 / 2.20 s | `shutdown.go` の 200 ms のポーリング (リテラル。推測を含む) |
| pro-con `TestExecRunnerKeepsPartlyStopped` | 1.01 s | 子の `kill -STOP; sleep 1; kill -CONT` |
| pro-con `TestDispatcherUpgradesItselfE2E` | 7.41 s | 主因は go build (推測)。待ちの問題ではない |

wtclean の 15 s は git fixture のコストで、sleep も子の sleep も無い (この issue の対象外)。

## 既にある seam

- pro-con: Dispatcher の `Now` / `Sleep` フィールド。`now func() time.Time` が ui / live / supervise ほか。`signalGrace` / `stopRetryEvery` /
  `screenPoll` / `wake.retryEvery` / `supRestartWait` は package の `var` でテストが縮めている
- lockman: `lease_tracker.go` は時刻を引数で受ける。`escalateGroupKill` は猶予を引数で受ける。**Locker の時刻は FS の probe ファイルの mtime**
  (`serverNow`) なので、期限切れは `os.Chtimes` で mtime を過去へ打てば作れる (`abandoned_test.go` が既にこの形)
- 注入口が無いもの: `runKillGrace` (`const`)、lockman `with.go` の `time.NewTicker(ttl / renewDivisor)` と `time.Now()`、`shutdown.go` の 200 ms

## 対応方針 (短縮はすべて見積り。実測していない)

| # | 施策 | 見込み | 難度 |
|---|---|---|---|
| 1 | `runKillGrace` を `var` にし、2 本のテストで 100 ms に縮める | −9.5 s | 小 |
| 2 | `TestServeStopsOnSignalWhenSpawnedByScreen` で `signalGrace` を縮める (`var` なので差し替えられる。同じファイルの `TestServeSignalWaitsForClosingScreens` は逆に 5 s へ**固定**しているので、差し替えの書き方の前例にはなるが縮めている例ではない) | −5 s | 小 |
| 3 | escalate の対照テスト: goroutine で呼び、TERM の記録を見たら子を終わらせる (猶予 3 s の理由を壊さない形) | −3 s | 小 |
| 4 | lockman の取得の待ちの backoff を注入するか `--wait` を縮める | −2.5 s | 小〜中 |
| 5 | ScreenFollow の `--timeout` を 200 ms に | −1.8 s | 小 |
| 6 | `lockman/lock_test.go` の `time.Sleep(3 * ttl)` 9 箇所を `os.Chtimes` に | −1.4 s | 小 |
| 7 | `stopped_test.go:104` の `sleep 1` を同期点に | −1 s | 小。🚨 この 1 s は「止まったまま runner が判定する時間」を作っている可能性がある。STAT=T の確認だけでは判定側が停止を観測した後かを保証しないので、判定の観測点 (呼び出しの記録等) を先に探す |
| 8 | `shutdown.go` のポーリング間隔を `var` に | 約 −1 s (推測) | 小 |
| 9 | lockman `with.go` に now と ticker を注入し、子の `sleep 3/4`・詰まり・待ちを tick の回数で表す (WINDOW / TTL / TICKER の 15 行前後) | lockman 42 s → 15 s 前後 | 中〜大 (設計変更) |
| 10 | 待ちの helper を module ごとに 1 つへ寄せる (手書き 35 行) | 壁時計はほぼ変わらない。上限の統一 | 中 |

- 1〜8 の合計で約 −25 s (lockman −6 s、pro-con −17 s)。9 まで入れて約 −40 s
- 🚨 9 は「ttl を縮めるだけ」では代替できない: lockman の I/O timeout の下限 200 ms と競り、負荷で落ちる形になる。
  時刻と tick を注入して、実時間の長さではなく**回数**で lease の生死を表す
- 🚨 猶予を縮める 1〜3 は、その猶予が何を担保していたかを先に確かめる (3 は既にコメントが「短いと偽赤」を説明している)。
  縮めた後に変異を当て、退行 (SIGKILL へ昇格しない等) が red になることを見る

## 受け入れ条件

- [ ] 1〜8 を入れ、package の時間を before / after で記録する (同じ負荷条件で 3 回ずつ)
- [ ] 9 の設計 (注入口の形) を決め、`with.go` の不変条件 (更新が止まったら lease が死ぬ) を tick の回数で表したテストに置き換える
- [ ] 10 は module ごとに 1 つの helper へ寄せる
- [ ] [615](615-chore-gate-new-sleeps-in-tests.md) の Go 側の検査の許可リストを、この表の残りと一致させる

## 関連ファイル

- `src/pro-con/dispatcher/runner.go` (`runKillGrace`) / `src/pro-con/dispatchercmd.go` (`signalGrace`) / `src/pro-con/dispatcher/shutdown.go`
- `src/lockman/with.go` (更新の ticker) / `src/lockman/lock.go` (`serverNow`) / `src/lockman/lease_tracker.go`
- 実測のログ: `go test -count=1 -v ./...` の `--- PASS` 行 (再取得のコマンドはこれだけ)

## 進捗
