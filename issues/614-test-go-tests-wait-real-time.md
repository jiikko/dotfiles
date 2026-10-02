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

- [x] 1〜8 を入れ、package の時間を before / after で記録する (7 は理由つきで残した。下の進捗)
- [x] 9 の設計 (注入口の形) を決め、`with.go` の不変条件 (更新が止まったら lease が死ぬ) を tick の回数で表したテストに置き換える → 見送り (下の進捗に理由)
- [x] 10 は module ごとに 1 つの helper へ寄せる
- [x] [615](615-chore-gate-new-sleeps-in-tests.md) の Go 側の検査の許可リストを、この表の残りと一致させる (615 で印を付け、検査は違反 0 件)

## 関連ファイル

- `src/pro-con/dispatcher/runner.go` (`runKillGrace`) / `src/pro-con/dispatchercmd.go` (`signalGrace`) / `src/pro-con/dispatcher/shutdown.go`
- `src/lockman/with.go` (更新の ticker) / `src/lockman/lock.go` (`serverNow`) / `src/lockman/lease_tracker.go`
- 実測のログ: `go test -count=1 -v ./...` の `--- PASS` 行 (再取得のコマンドはこれだけ)

## 進捗

- 2026-10-02 「test(pro-con): SIGKILL までの猶予・画面を待つ猶予・follow の上限を実時間で消化しない (614 の 1 / 2 / 5)」
  - 1: `ExecRunner` に `KillGrace` (0 なら既定 `runKillGrace`。既存の `StopGrace` / `StopPoll` と同じ形) を足し、2 本で 300 ms に。
    `TestExecRunnerCancelKillsStubbornChild` 5.04 → 0.35 s / `TestExecRunnerLockedCancelKillsStubbornChild` 5.35 → 0.85 s。
    変異: 猶予の後の SIGKILL への昇格を外す → Locked の方が red (lockman を挟まない方の昇格は WaitDelay の別経路で、この変異の対象外)
  - 2: `TestServeStopsOnSignalWhenSpawnedByScreen` で `signalGrace` を 200 ms に (package に `t.Parallel` は無い)。5.07 → 0.24 s。
    変異: 持ち主の画面を見ずに止める (`ownersStayOpen` を外す) → red
  - 5: `TestScreenFollowWaitsForFirstFrame` の `--timeout 2s` → `500ms`。2.00 → 0.51 s (3 回連続)。変異: 1 枚目を待たずに「閉じた」で抜ける → red
- 2026-10-02 「test(pro-con): RequestStop の見直しの間隔をテストで縮める (614 の 8)」
  - `shutdown.go` の 200 ms のリテラルを `stopRequestPoll` (package の変数) にし、`TestRequestStop` で 10 ms に。各ケースの否定の窓 (300 ms) も
    「見直す間隔の 10 倍」に揃えた。1.21 → 0.38 s。変異: lock を見ずに「止める側が死んだ」で抜ける → 「止まる前に待ちを抜けた」で red
  - `TestStopTakesOverWhenStopperDies` (2.2 s) は pro-con 本体のテストで、dispatcher の非公開の変数に届かない。今回は触らない (2.2 s の内訳も未確認)
  - 7 (`stopped_test.go:104` の `sleep 1`) は残す: 「一部だけ止まっている間、猶予 (StopGrace 200 ms) を超えても止め直さない」を見る否定の窓で、
    猶予の 5 倍。縮めても 0.5 s しか減らず、余裕が減ると負荷の日に誤った実装を見逃す (偽の緑) 側に倒れる
- 2026-10-02 「test(lockman): lease の期限切れ・TERM の確認・取得の再試行を実時間で待たない (614 の 3 / 4 / 6)」
  - 6: `lock_test.go` の `time.Sleep(3 * ttl)` 9 箇所 → `expireLease` (lock の mtime を「3 × ttl 前」へ `os.Chtimes`)。遠い過去にしないのは、
    同じ mtime を時計のずれの判定 (`clockSkewTolerance`) にも使う経路があるため。変異: `expired` を常に偽 → `TestReleaseRefusesExpiredLease` /
    `TestRenewDetectsLostLease` が red
  - 3: `TestEscalateSendsTermWhenChildIsAlive` は `escalateGroupKill` を goroutine で呼び、TERM の記録を見たら `exited` を閉じて猶予を打ち切る
    (猶予 3 s は「短いと記録が SIGKILL に間に合わない」理由のまま残した)。3.01 → 0.03〜0.24 s (-race で 3 回)。変異: TERM を撃たない → red
  - 4: `cmdAcquire` の最初の backoff を `acquireBackoff` (package の変数、既定 1 s) にし、`TestWaitLoopDoesNotWarnPerRetry` は 10 ms・`--wait 150ms`。
    3.01 → 0.17 s。変異: 再試行のたびに警告を出す → 「待ち直す枝でも鳴っている」で red
  - lockman package: 42.3 s → 34.8 s (`make -C src/lockman test`、lint 0 件)。残りの大半は 9 (with.go の更新 ticker と子の `sleep 3/4`)
- 2026-10-02 9 (lockman `with.go` に now と ticker を注入) は**着手しない**。起票時の見積り (42 s → 15 s) の前提が成り立たなかった:
  - lease が生きているかを**他のプロセスが**判定するのは「lock ファイルの mtime と FS の今の時刻」(`serverNow` = probe ファイルの mtime) の差。
    偽の時計で進められるのは自プロセスの ticker と `leaseTracker` の期限だけで、FS の時刻は進まない
  - `on_lost_kill_test.go` / `on_lost_deadline_test.go` の長いテストは、別の Locker から `probeLease` で lease の生死を見る形。偽の時計に替えると
    「自分は 9 tick 進んだが、他人から見た lease は実時間で 0.1 s しか経っていない」になり、本番と違う時間の流れで判定する
  - 置き換えるには FS の時刻を読む口 (`serverNow` と mtime を読む全経路) ごと差し替える必要があり、lease を守る安全機構の大きな作り直しになる
  - 再評価の trigger: lockman の時刻の取り方を変える別の理由が出たとき (例: probe ファイルをやめる)、または lockman のテストが CI の律速になったとき
- 2026-10-02 「test(go): 手書きの待ちを package ごとの helper に寄せる (614 の 10)」 (sonnet が書き換え、main が diff を検閲)
  - pro-con 本体: 同じ package に 2 つあった `waitUntil` (上限 10 s) と `eventually` (5 s) を `waitUntil` に寄せ、`eventually` を消した (7 箇所)。
    手書きの待ち 5 箇所を置き換え (うち `ticks==0` の 3 箇所は元が無上限で、10 s の上限が付いた = ハングが失敗になる側の変化)
  - lockman: `waitForCondition` (`t.Helper` だけで Fatal しないので goroutine からも呼べる) に 6 箇所、`waitAbandoned` に 1 箇所
  - pro-con/dispatcher: 汎用の `pollUntil` を `execrunner_test.go` に作り 4 箇所
  - 触らなかった: 否定の確認・窓・ループの中に副作用 (tick を回す / Lock を試す / `done` を読み戻す) があるもの・`t` の無い helper プロセスの中・
    失敗時にログを出す上限 60 s の待ち
  - 各 package 変更後 3 回 rc=0 (pro-con 本体 16.4〜16.6 s / dispatcher 13.1〜13.2 s / lockman 34.0〜34.1 s)。lint 0 件
  - 9 を見送った理由を `src/lockman/on_lost_kill_test.go` の冒頭にも書いた (コードの側に残す)
