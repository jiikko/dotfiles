# `with` の `signal.Notify` が `Acquire` と `cmd.Start()` の後にあり、中断で lock と孤児の子が残る

起票日: 2026-09-12
カテゴリ: bug / priority: **high**
対象: `src/lockman/with.go` の `runWith`
出典: [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点③「並行・中断」)
反証レビュー: 未実施。**出典は opus 1 体による実測 A-B**（下の表）

## 問題

`with.go` の `signal.Notify` は `pgid := cmd.Process.Pid` の直後にある。
それ以前 (= `Acquire` 全体 + `cmd.Start()`) に届いた INT / TERM / HUP は
**既定処理**で lockman を即座に殺す。

しかも子は `Setpgid: true` で独立したプロセスグループに居るので端末のシグナルを
受けておらず、**孤児として走り続ける**。lease を更新する者が居ないまま TTL が切れ、
他ホストが引き継ぐと **同じ排他区間に 2 つの子**が並ぶ。

## 実測 (A-B)

40 試行 ×3、遅延 0..10ms でランダムに SIGTERM。子は TERM を無視する。
「rc=143 かつ lock 残存」を漏れと数える。

| 腕 | 漏れ |
|---|---|
| 現行 | 7/40・9/40・4/40 = **20/120** |
| `signal.Notify` を `Acquire` の前へ移動 | **0/40 ×3 = 0/120** |

漏れ 20 件の内訳:

- **`child_done=1` (孤児の子 + lock 漏れ) が 9 件** ← 重いのはこちら
- `child_done=0` (子が起動する前に lock だけ漏れた) が 11 件

**全 20 件で stdout 0B / stderr 0B**（完全に無言）。

🚨 上の A-B の「移動」は**機構を特定するための対照であって、提案する修正ではない**。
前へ移すとシグナルは buffer されるだけで (`sigCh` を読むのは後段のループ)、挙動が別物になる。

> ハーネスの注記: 最初の版は SIGINT で全 40 件が rc=0 になった。原因は実装ではなく
> **非対話シェルのバックグラウンドジョブが SIGINT に SIG_IGN を継承する**こと。
> `verify-interactive-prompt-with-pty-driver.md`「失敗したらまずハーネスを疑う」の適用例。

## 356 との違い

[issue 356](done/356-bug-lockman-with-releases-lock-while-grandchildren-run.md) は
**子が正常終了した経路**で「孫が残っているのに解放する」話。本 issue の trigger は
**シグナルが早く届いた経路**で、356 を直しても消えない。

## 対応の候補 (未決)

- `Acquire` の前にハンドラを立てて、その時点から「受けたら解放して落ちる」を保証する
  (buffer だけでは足りず、Acquire 中に受けた場合の解放経路が要る)
- 子を起こす前に受けた場合と、起こした後に受けた場合で処理を分ける

## 356 の実装後の状況 (2026-09-15)

**356 は完了したが、この issue は解消していない** (本文の「356 との違い」のとおり)。
356 が入れたのは経路 2 (lease 喪失時の TERM → 猶予 → KILL への昇格) と、経路 1 を
**直さないと決めた**記録で、**シグナルが `signal.Notify` より早く届く窓**には触れていない。

356 の実装で本 issue に効く変更が 1 つある: シグナル転送が `killGroup` を通るようになり、
**`pgid <= 1` には撃たなくなった**。窓そのものは変わらない。

## 2026-09-16 追記 (366 の敵対的レビュー 観点③ より)

**① 取得中の Ctrl-C が残すものが増えた。** 366 で `tryTakeover` に引き継ぎの調停の目印
(`tmp/<gen>.takeover`) と回収の目印 (mark) を足したので、`signal.Notify` より前の即死で
残る中間状態が 3 種類になった。塞ぐ長さは:

| 残るもの | 塞ぐ長さ | 契約違反か |
|---|---|---|
| `lock` | TTL (既定 30m) | **違反ではない** (TTL の意味そのもの) |
| `tmp/<gen>.takeover` | 猶予 = `max(io-timeout, 10s) × 3` (既定 30s、最大 15m) | 違反ではない (短い `--ttl` では超える) |
| `tmp/<gen>.takeover.<nanos>` (mark) | **掃除まで (~1h10m)** | **違反**。既定 TTL 30m を超える |

3 行目は 366 が新設した状態なので、**この issue が閉じれば 366 の回収機構を
「取りこぼしの受け皿」へ格下げできる**可能性がある (判断は 366 の残存窓を見てから)。

**② シグナル転送の枝だけ `exited` を見ていない。** `escalateGroupKill` は撃つ前に
`select { case <-exited: return; default: }` で子の回収を確認するが、`case sig := <-sigCh:`
の転送はその確認をしない。`cmd.Wait()` が返って `close(exited)` + `done <- err` が済んだ後、
select が `done` を処理する前に `sigCh` が ready だと、Go の select は**両方 ready のとき
ランダムに選ぶ**ので約 50% で `sigCh` 側を先に取り、**既に回収された pgid へ撃つ**。
実害には pid の巻き戻し再利用が要る (**未確認リスク**。通常は ESRCH で終わる) が、
`with.go` は「pgid が再利用されると無関係なプロセスグループへ撃つ」を明示的な脅威として
扱っているので、**同じ基準がこの枝に適用されていないのは非対称**。

## 着手前の整理 (2026-09-21。再導出を省くため)

### 周辺 issue の現状

| issue | 状態 | 本 issue への効き |
|---|---|---|
| [356](done/356-bug-lockman-with-releases-lock-while-grandchildren-run.md) | **done** | 転送が `killGroup` 経由になり `pgid <= 1` を撃たなくなった。窓そのものは不変 |
| [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) | **done** | **残る中間状態が 3 種類に増えた**(下の表)。回収機構の格下げ判断は本 issue と [362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) の両方が閉じてから |
| [384](done/384-bug-lockman-escalation-burns-out-and-sigkill-skips-recheck.md) | **done** | 下の「残タスク 4 の答え」に直結 |
| [385](done/385-design-lockman-on-lost-kill-vs-keep-renewing.md) | **done** | `runWith` の select ループに `leaseTracker` が入った。**この issue が触るのは同じ関数**なので、着手時に rebase 前提 |
| [362](362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) | **open** | 見捨てられた goroutine 側の残骸。本 issue とは経路が違う (あちらは timeout、こちらはシグナル) |

### 残タスク 4 (`case sig := <-sigCh:` に `exited` の guard を入れるか) は **もう答えが出ている**

[384](done/384-bug-lockman-escalation-burns-out-and-sigkill-skips-recheck.md) の実測
(2026-09-20 / darwin 24.6 / 3 回とも同じ) で、回収済み pgid への `kill(-pgid, sig)` は
**すべて空振り**と分かった: グループが空 → **ESRCH** / ゾンビだけ → **EPERM** /
**生きたメンバーが 1 つでもあれば成功**。実害には「pid 再利用 **かつ** 再利用した側が
グループリーダー」が要る。[364](done/364-bug-lockman-with-release-failure-and-graveyard-retention.md)
では**同じ結論で受容**し、`_ = killGroup(...)` の 2 箇所に捨てている理由を書いた。
→ **本 issue でも「非対称だが受容」で揃えるのが自然**。guard を足すなら「揃えるため」であって
実害の除去ではない (足すこと自体は数行)。

### ✅ 方針決定 (2026-09-21。ユーザー判断): **取得中の即死はそのまま**

「取得中の Ctrl-C は即死でよい」= **`Acquire` 中に片付けることは求めない**。
片付けるには生きている必要があるので、この判断で下の (A) は**採らない**ことが確定した。

**採る形は (B): `signal.Notify` を `cmd.Start()` の直前へ出すだけ**。これで:

| 窓 | 現行 | (B) の後 |
|---|---|---|
| `Acquire` 中に届く | 即死。`lock` / 目印 / mark が残る (子はまだ**居ない**) | **変えない** (即死のまま。ユーザー判断) |
| `cmd.Start()` の前後に届く | 即死。**子が孤児として走り続ける** (出典の実測で 9/20 = 重い方) | **消える**。Notify が先に立っているので runWith が受け取り、転送して解放する |

出典の実測 20/120 の内訳は「孤児の子 + lock 漏れ 9 件」「子が起動する前に lock だけ漏れ 11 件」。
**(B) は前者 (重い方) を構造的に消し、後者は受容する** (子が居ないので二重実行にはならない。
残るのは lock / 目印 / mark で、`lock` と目印は TTL・猶予で自然に解け、**mark だけが
掃除まで ~1h10m 塞ぐ** = 上の表の契約違反。ここは受容として記録する)。

🚨 **受容する側の害を過小評価しないこと**: mark の ~1h10m は既定 TTL (30m) を超える。
それでも受容するのは、消すには「取得中も生きて片付ける」= (A) が必要で、それは
今回の判断で外れたため。**別案を採るなら 366 の回収機構の猶予を短くする方が筋** (本 issue の外)。

### 採らなかった案 (A)。記録として残す

#### (A) 取得中も片付ける案

`signal.Notify` を `Acquire` の前へ出すだけでは足りない (本文の 🚨 のとおり buffer されるだけ)。
**回収する枝**が本体:

```go
signal.Notify(sigCh, INT, TERM, HUP)   // ← Acquire より前
meta, err := l.AcquireTimed(ttl, label)
// 取得中に届いていたら、ここで片付けて落ちる (子はまだ起こしていない)
select { case sig := <-sigCh: /* 解放 + 目印の回収 */; return 128 + sig; default: }
cmd.Start()
```

🚨 **これは挙動変更を含む**: 取得中の Ctrl-C が**即死しなくなり、最大 `--io-timeout` (既定 10s)
待ってから片付けて落ちる**。**2026-09-21 のユーザー判断で「即死のままでよい」となったので、
この案は採らない**。

代案: `AcquireTimed` は既に `withTimeout` + `abandon` (issue 362 の機構) で動いているので、
シグナルで `abandon` を立てて `undoAbandonedPlace` に回収させる形もありうる。窓は縮むが
**timeout.go の API を広げる**ので、機構を 1 つ増やすコストとの比較が要る (§0-A)。

### 塞ぐ対象 (366 以降は 3 種類。**mark だけが契約を割る**)

| 残るもの | 塞ぐ長さ | 契約違反か |
|---|---|---|
| `lock` | TTL (既定 30m) | 違反ではない |
| `tmp/<gen>.takeover` | 猶予 (既定 30s、最大 15m) | 違反ではない |
| `tmp/<gen>.takeover.<nanos>` (mark) | **掃除まで (~1h10m)** | **違反** |

### テストの形 ((B) なら **in-process で書ける**)

素朴には「修正が無い = シグナルで即死するので、`go test` の中で自分へ SIGTERM を撃つと
**テストランナーごと死ぬ**」が、これは回避できる:

🚨 **`TestMain` (または当該テストの冒頭) で TERM を `signal.Notify` しておく**。Go の
`os/signal` は **登録された全チャネルへ配送**し、1 つでも登録があれば**既定処理 (即死) が
無効になる**ので、①変異を当ててもランナーは死なない ②`runWith` 側が登録していれば
そちらにも同じシグナルが届く。つまり**「runWith が受け取れたか」だけを差として観測できる**。

- 窓は決定論で作る: `cmd.Start()` の直後に呼ばれる seam を足し、そこで自分へ TERM を撃つ
  (`avoid-wall-clock-assertions.md`。ランダム遅延の A-B は**機構の特定用**で、恒久テストには
  向かない — 確率的な観測を CI に置くと flaky になる)
- 判定: **子が TERM を受けて死んだか** / **lock が解放されたか** / rc
- 変異: `signal.Notify` を `cmd.Start()` の**後ろ**へ戻す → runWith の sigCh には届かず、
  子が走り続けて lock が残る = red
- 🚨 この形だと**「即死しない」ことは検査できない** (テスト側の Notify が既定処理を潰すため)。
  そこは (B) が変えない部分なので対象外だが、**テストが何を検査していないか**として明記すること

### 見積もり (2026-09-21 時点)

| 作業 | 目安 |
|---|---|
| 方針決定 | ✅ 済 (即死のまま = (B)) |
| 実装 (`signal.Notify` を `cmd.Start()` の直前へ + seam を 1 つ) | **15〜20 分** |
| テスト (in-process。TestMain の Notify + Start 直後の seam) | **30〜45 分** |
| 変異検証 (Notify を Start の後ろへ戻す) + 敵対レビュー 1〜2 周 | 45〜60 分 |
| 受容の記録 (mark の ~1h10m / 転送枝の非対称 = 364 と同じ扱い) | 10 分 |

**(A) を外したことで e2e サブプロセスが不要になり、見積もりが半分以下になった。**

## 進捗 (2026-09-21): (B) を実装

### 変えたこと (production は 1 箇所)

`signal.Notify` を `cmd.Start()` の**前**へ出した。あわせて検査用の seam
`afterChildStartHook` (Start の直後・select ループの前) を新設。

**`Acquire` 中の窓は受容**。理由 (即死を保つこと / 片付けるには生きている必要があること) と、
残る 3 種類のうち **mark だけが ~1h10m 塞ぐ**ことをコードに書いた。

### テスト (`signal_handler_test.go`。**in-process**)

- `TestMain` で TERM を `signal.Notify` して安全網を張る。Go は**登録された全チャネルへ配送**し、
  **1 つでも登録があれば既定処理が無効**になるので、**変異を当ててもランナーが死なない**
- `afterChildStartHook` で自分へ TERM を撃ち、子への転送 / lock の解放 / rc を見る

### 🚨 「撃つだけ」では変異を検出できなかった (実測)

初版は seam で `syscall.Kill(os.Getpid(), SIGTERM)` を撃って即 return していた。これだと
**変異 (Notify を seam の直後へ置く) の窓が数 ns しかなく、配送が変異後の登録に間に合う** —
現行と変異が**どちらも rc=143 / 子が死ぬ**で、区別できなかった。

→ **テスト側のチャネルが受け取るまで seam で待つ**形にした。seam を抜けた時点で配送は完了して
いるので、その後に登録する実装 (= 旧版) は**原理的に受け取れない**。

| | 現行 | 変異 (Notify を Start の後ろへ) |
|---|---|---|
| 初版 (撃って即 return) | rc=143 / 子が死ぬ | rc=143 / 子が死ぬ ← **区別できない** |
| 現在 (配送を待つ) | rc=143 / 子が死ぬ / lock 解放 | **rc=0 / 子が 5 秒走り切る** = 孤児の再現 → **red** |

### 結果

- `go test -race ./...` 緑 / golangci-lint 0 issues
- 🚨 **このテストが検査しないもの**: 「即死しないこと」自体 (テスト側の Notify が既定処理を
  潰すので、修正の有無で死ぬ / 死なないの差が出ない)。(B) が変えないのは取得中の窓だけなので
  対象外だが、**何を検査していないか**として記録する

## 敵対的レビュー 1 周目 (2026-09-21。全数勘定)

指摘 9 件、**採用 5 / 記録 4 / 却下 0**。🚨 **P1 が 2 件で、どちらも「私の修正が支えていない /
副作用を作った」形**だった。

| # | 指摘 | 判定 |
|---|---|---|
| P1-1 | seam を `cmd.Start()` の**後**に置いたため、判別境界が Start ではなく seam になっていた。**「Start と Notify のあいだ」に Notify を戻す変異 (= 363 そのものの退行。子は既に居るので孤児になる) が緑で通る**。しかも seam の doc が契約を **Notify 基準**で書いており、**そのとおりに置くと盲点になる配置を明文で許していた** | **採用**。seam を Start の**前**へ移し、doc を **Start 基準**へ書き直した。変異 A で red |
| P1-2 | Notify を exec より前へ出した副作用で、**子が継承する HUP/INT の disposition が SIG_IGN → SIG_DFL に変わった**。Go の runtime は HUP/INT に限り継承 SIG_IGN を尊重するが `signal.Notify` が上書きするため。実測 (`trap '' HUP INT` の下): 素の実行と旧版は子が生き延び、**新版は死ぬ**。`with` の契約「子が無視するなら素で実行したときと同じ」に反し、`nohup` / cron で端末が切れたとき**以前は生き延びたジョブが死ぬ**。しかもその環境では**旧版に 363 の孤児問題は無かった** (lockman も子も無視していた) = 問題の無いところに挙動変更を作っていた | **採用**。`signal.Ignored` が true のものは Notify に渡さない。🚨 **空リストで `signal.Notify` を呼ぶと全シグナルを中継する**ので `len(sigs) > 0` のガードと対で意味を持つ |
| P2-1 | `defer signal.Stop(sigCh)` が解放の defer より**後**に登録されており、LIFO で **Stop → 解放**の順に走る。Stop で既定処理が戻るので**解放中の TERM/INT がプロセスを殺し lock が残る** = 363 が漏れと定義した signature そのもの (レビュー実測 3/3)。窓幅は `ReleaseTimed` の所要 = 詰まったマウントでは `--io-timeout` (既定 10s) まで。🚨 **この commit が作った窓ではなく既存** | **採用**。sigCh の生成と Stop の defer を解放より前へ。Notify の位置は変えないので**取得中の即死は据え置き** |
| P2-3 | 363 の主題は Ctrl-C = **SIGINT** なのに、テストが通しているのは TERM だけ | **採用**。INT/TERM のテーブル駆動にした (変異 D で red) |
| 変異 E (レビュー外だが自分で発見) | `notifiableSignals` が `signal.Ignored` を直に呼ぶと、**テストプロセスでは何も無視されていないので選別の有無で結果が変わらず**、「無視されていても渡す」変異が緑で通った | **採用**。述語を引数で受ける形にして単体で固定 (変異 E で red) |
| P2-2 | `defer signal.Stop` を消す変異が緑 (未 pin) | **記録**。下記のとおり in-process では原理的に pin できない |
| P3-1 | `TestMain` の `defer signal.Stop(guard)` は `os.Exit` で**永久に走らない** | **採用**。削除し、意図 (プロセスの寿命と同じライフタイム) をコメントに書いた |
| P3-2 / P3-3 | `boundedInt` の安全網 20s と seam の待ち (10s+10s) が同額 / Start 中はシグナルが buffer されるだけで割り込めない | **記録**。前者は前提が崩れたときにメッセージが変わるだけ (推論のみ、未実験)。後者は旧版が即死していたトレードオフ |
| 受容の記述が不正確 | mark と調停の目印は **stale takeover 経路でしか作られない**ので、素の Acquire 中の Ctrl-C では残らない。「3 種類が常に残る」と読める書き方だった | **採用 (訂正)**。害を**過大に**見積もる方向なので安全側だったが、記述としては誤り |

### 🚨 in-process では pin できないもの (2 件。記録)

| 変異 | なぜ緑のままか |
|---|---|
| **P2-1 の defer 順を戻す** | 害は「既定処理が戻ってプロセスが死ぬ」ことだが、**ランナーを守るための `TestMain` の guard がまさにその failure mode を抑止する**。修正の正しさはレビューの out-of-process A-B (3/3 で `rc=143` + lock 残存) が根拠。検出可能性は「**検出手段はあるが未実証**」(subprocess e2e なら可能) |
| **空でも `signal.Notify` を呼ぶ (変異 F)** | テストプロセスでは INT/TERM/HUP が無視されていないので**リストが空にならない**。空のケース自体は `notifiableSignals` の単体で固定済みで、ガードの必要性はコメントで説明 |

### 変異検証 (1 周目の修正分)

| 変異 | 結果 |
|---|---|
| A: Notify を `cmd.Start()` の後ろへ | `TestSignalIsHandledFromTheMomentChildExists` の **2 subtest とも red** |
| D: INT/HUP を落として TERM だけ Notify | 同 `/interrupt` + `TestNotifiableSignalsRespectsInheritedIgnore` red |
| E: 無視されていても Notify に渡す | `TestNotifiableSignalsRespectsInheritedIgnore` red |
| F: 空でも Notify を呼ぶ | **緑** (上表の理由。記録) |

`go test -race ./...` 緑 / golangci-lint 0 issues。

### 2 周目が要る

§7 の打ち切り条件 (a) を満たさない: 1 周目の対応で **production に新しい判定を 3 つ足した**
(seam の位置 / `notifiableSignals` の選別 / defer の登録順という新しい不変条件)。

## 残タスク

- [x] 反証レビュー 1 周目 (採用 5 / 記録 4)。変異 3 本で red
- [ ] **未実施**: 2 周目 (§7)。1 周目の対応が新しい判定 3 つを含むため
- [x] 対応方針の決定 → **(B) `signal.Notify` を `cmd.Start()` の直前へ出すだけ**
      (2026-09-21 のユーザー判断「取得中の即死はそのままでよい」)。取得中の窓は受容する
- [x] 塞ぐ対象の整理 → (B) では**取得中の残骸 (lock / 目印 / mark) は受容**する。
      **mark だけが TTL の契約を割る** (~1h10m) ことを受容の害として記録し、
      消したいなら 366 の回収機構の猶予を短くする方が筋 (本 issue の外)
- [x] (B) の実装とテスト (変異で red。上節)
- [ ] 閉じたら [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の回収機構
      (`reclaimTakeoverClaim` / `takeoverClaimGrace`) を「取りこぼしの受け皿」へ格下げできるか
      再評価する。**362 と両方閉じるまでは外せない** (どちらの経路も目印を残す)
- [ ] シグナル転送の枝 (`case sig := <-sigCh:`) に `escalateGroupKill` と同じ
      `select { case <-exited: return; default: }` を入れるか決める。**384 の実測で答えはほぼ
      出ている** (回収済み pgid への kill は ESRCH / EPERM で空振り。実害には pid 再利用 かつ
      リーダー一致が要る)。364 は同じ結論で**受容**したので、揃えるなら「非対称を消すため」に
      入れる (数行)、揃えないなら 364 と同じく**理由をコードに書く**
