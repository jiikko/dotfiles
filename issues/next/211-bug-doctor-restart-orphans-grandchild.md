# 211 bug: 再起動 (`r`) で doctor の孫プロセスが孤児として残る

起票日: 2026-09-03
出典: issue 205 の red team (opus、攻め口④)。**再現済み**
重要度: P2 (孤児プロセスが残る。実害は brew doctor の bash→ruby→git が走り続けること)
関連: `src/glogx/main.go` の `restartSelf` 経路 / `src/doctor/runner/runner.go` の `Exec` /
issue 205

## 症状

`cancel()` は**同期的に子を殺さない**。`exec.CommandContext` の watchdog goroutine が
`Kill(-pgid)` を打つので、`cancelAll()` が戻った時点で孫はまだ生きている。

`main.go` の再起動経路は `browse.cancelAll()` の**次の行**が `restartSelf()` = `syscall.Exec` で、
**プロセス像ごと消えるので watchdog goroutine が走らない**。子は `Setpgid` で別プロセスグループ
なので端末の SIGINT も届かず、`brew doctor` の子孫 (bash → ruby → git) が孤児として残る。

## 再現 (red team が実測、3/3 一致)

```
cancel 直後の孫 alive=true
2 秒後      alive=false     ← watchdog が生きていれば殺される
```

`syscall.Exec` はその「2 秒後」を待たずにプロセス像を置き換えるので、孤児が残る。

発火条件: brew doctor の走査中に `r` を押す。SIGINT / SIGTERM 終了 (`defer browse.cancelAll()`)
も同型の窓を持つが、そちらは exit がプロセスグループを道連れにしないだけで、窓自体は同じ。

## 訂正 (反証レビュー 2026-09-03)

- **「孤児」は不正確**。`syscall.Exec` は pid を保つので、子は**新しいプロセス像の子のまま**で
  init の里子にはならない。誰も `wait` しないので、終了後はゾンビとして残る。
  実害の記述 (brew の子孫が走り続ける) は変わらない
- **直し場所は `cancelAll` の後段ではない**。restart 経路は
  `defer waitPullCleanup()` / `defer browse.cancelAll()` を**一つも走らせない**ので、
  同じ窓は pull の後始末にも開いている。「`waitPullCleanup` と同じ形の待ちが doctor 側に無い」は
  「pull は守られている」と読めて誤解を招く。正しくは **`cancelAll()` と `restartSelf()` の間**に
  看取りを 1 箇所置く形
- **`usageOv.stop()` / `actModal.stop()` は同型でない** (「未確認」と書いていた点の答え)。
  action modal 側は `noPromptGitCmd(...).CombinedOutput()` 経路で `Setpgid` を持たず、
  後始末は `pullCleanup` の WaitGroup latch (`external_commands.go`) を既に持っている。
  したがって「`cancelAll` に 1 箇所」ではなく **「restart 直前に latch 群 (pullCleanup +
  doctor 用の新しい latch) を待つ」**形が素直

## 壊せなかった点 (併せて記録)

`cancelAll` は quitNow / quit / restart / defer の**全経路から呼ばれている**。
「呼ばれない経路がある」形は red team が探して見つからなかった。問題は呼ぶかどうかではなく
**戻ってから殺されるまでの窓**。

## 直し方 (案)

`syscall.Exec` の前に「走行中の子の帰還を看取る」= `cmd.Run` が戻るまで待つ。
`waitPullCleanup` と同じ形の待ちが doctor 側に無い。

置き場所は上の「訂正」節のとおり **`cancelAll()` と `restartSelf()` の間**。
`usageOv.stop()` / `actModal.stop()` は同型ではないので、doctor 用の latch を足して
pullCleanup と並べて待つ形にする。

## テスト観点

- cancel から「孫が消えるまで」を**時計で待たずに**判定する (孫 pid を受け取って生死を見る)。
  `_claude/rules/avoid-wall-clock-assertions.md`
- `syscall.Exec` は直接テストできないので、「看取り」関数が cancel 後に子の帰還を待つことを
  単体で固定する

## レビュー状態

red team (opus) が実測で再現 → **反証レビュー (opus) で核 (窓の存在) は反証できず**、
事実記述 3 点を訂正した (上記「訂正」節)。
