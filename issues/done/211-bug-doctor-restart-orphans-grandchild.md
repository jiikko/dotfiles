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

## 適用ログ (2026-09-03)

commit `?` (本体) / `ff5c57a5` (敵対レビューの P1 対応) / `bad49d3c` (副産物の競合修正)。

### 入れたもの

- `scanLatch` (新規 `src/glogx/doctor_cleanup.go`) と `waitDoctorCleanup`。
  disk / svc / brew の走査 3 本 **と削除**を登録する
- `main.go`: `cancelAll()` と `restartSelf()` の**間**で待つ (exec では defer が 1 つも
  走らないので明示で呼ぶ)。シグナル終了側は `defer waitDoctorCleanup()` を積む
  (LIFO で cancelAll → doctor → pull の順)
- `start()` の冒頭で `v.stop()` (前世代を止める)

### 敵対的レビュー (opus) が出した P1 2 件 — どちらも私の実装の穴

1. **削除経路が latch に載っていなかった**。走査 3 本だけ看取っても、いちばん危ない経路
   (`rm` / `trash` / `brew cleanup` / `simctl delete` と、その後のインベントリ記録) が
   終了・再起動で watchdog ごと消えて走り続ける
2. **私の修正が hang を新設していた**。`rescan()` → `start(true)` が `stop()` を経由せず
   `v.cancel` を上書きするため、前世代の走査を誰も cancel できないまま latch に残り、
   待ちの上限が 2 秒ではなく **PerEntry 60 秒 × 並列度 (約 6 分)** になっていた。
   latch を入れる前は即終了だったので、**この修正が無ければ 211 は改善ではなく劣化**だった

🚨 `adversarial-review-own-safeguards.md` の節 7 (指摘への修正は新しい安全機構なので、
直した差分にもう 1 周回す) の実例。**待ち受けを足す修正は hang を新設しうる**。

### sync.WaitGroup をやめた (-race が本物の競合を検出)

svc / brew / 削除は tea.Cmd の closure から登録するので **Add が Wait と同時に走りうる**。
WaitGroup はカウント 0 での Add と Wait の同時実行を禁じており、`-race -count=3` で
データ競合として検出された。状態を mutex の下に置いた `scanLatch` に置き換えた。
遅れた登録を Wait が取りこぼすのは許容する (まだ始まっていない Cmd は子を起こしていない)。

### 副産物: issue 214

着手中に `TestUpdateKeysYieldToDoctorDelete` が赤くなり、原因は **`doctor/disk` の
データ競合** (テスト専用カウンタ `collectVisits` が package 変数) だった。214 として起票し、
`atomic.Int64` へ直して並行走査の回帰テストを足した。

### テストの判定軸を 3 回やり直した

1. 子プロセスの生死で判定 → **テストプロセスでは watchdog が生きているので latch の有無を
   判別できない** (「disk を外す」変異を素通し。数 ms のタイミングで flaky でもあった)
2. 「走査が帰るまで Wait が戻らない」順序で判定 → 経路ごとの fixture が必要と判明
   (3 つまとめて 1 回試す形では 1 経路だけ外した変異が緑)
3. 削除の cancel は「待っている間に切れたか」ではなく **`stop()` の直後に判定**
   (別経路が遅れて cancel しても緑になり、変異を素通しした)

### 変異検証 (6 本すべて red)

削除を latch から外す / `stop()` の cancel を消す / `start` の `v.stop()` を消す /
restart の wait をコメントに / defer 順序を入れ替える / latch の done を no-op に。

🚨 この過程で**変異の適用漏れを 2 回見落とした**。以降は「適用の証拠 (grep の件数変化)」を
毎回出す運用にした。

### 重複を外した

`stop()` で削除の ctx を切る修正は **148 S2 のレビュー対応で既に入っていた**ので、私の追加分を
外して「cancel だけでは子の死を待たない」注記だけ残した (二重管理にしない)。

### 閉じる前の最終ゲート (opus、2026-09-03) — 壊せなかった

`waitDoctorCleanup` が**永久にブロックする入力は作れなかった**:

- disk は Cmd ではなく `start()` の中で `add()` するので必ず `done()` に届く
- `ch` の容量は `catalogN+1` で、`OnResult` は 1 エントリ 1 回 + 完了 1 回の**ちょうど**上限
  (`CatalogSize() == len(catalog)` を確認) なので送信で詰まらない
- `disk.Scan` は `wg.Wait()` してから返るので子 goroutine を置き去りにしない
- `runner.Exec` は `Kill(-pgid)` + `WaitDelay 2s` で有限
- svc / brew / 削除の**遅れた登録**は `doctorTrack` が f より前に `add()` するので
  「登録前に子が生まれる」窓が無く、しかも `cancelAll` が wait より前 (LIFO / restart の明示順)
  なので、遅れて走る closure は**キャンセル済み ctx** で子を起こさない
- `scanLatch` は全状態を mutex 下に置き、`wait` の再入・add/done の交錯・n の underflow でも
  wedge しない

この issue は**単独で閉じてよい**と判定された。
