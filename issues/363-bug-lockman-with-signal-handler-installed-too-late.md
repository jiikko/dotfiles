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

## 残タスク

- [ ] 反証レビュー
- [ ] 対応方針の決定 — **356 は D + C で閉じたので、「356 とまとめて直す」選択肢は消えた**。
      単独で方針を決める
- [ ] **塞ぐ対象に引き継ぎの調停の目印と mark を含める (2026-09-16 追記)**。上の表のとおり、
      取得中の即死で残るのは lock だけではなくなった。**mark だけが TTL の契約を割る**
      (掃除まで ~1h10m) ので、方針を決めるときはそこを基準にする
- [ ] 閉じたら [366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の回収機構
      (`reclaimTakeoverClaim` / `takeoverClaimGrace`) を「取りこぼしの受け皿」へ格下げできるか
      再評価する。**362 と両方閉じるまでは外せない** (どちらの経路も目印を残す)
- [ ] シグナル転送の枝 (`case sig := <-sigCh:`) に `escalateGroupKill` と同じ
      `select { case <-exited: return; default: }` を入れるか決める。実害には pid の巻き戻し
      再利用が要る (未確認リスク) が、`with.go` はそれを明示的な脅威として扱っているので
      **同じ基準がこの枝にだけ適用されていないのは非対称**
