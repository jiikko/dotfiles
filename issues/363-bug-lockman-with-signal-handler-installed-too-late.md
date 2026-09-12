# `with` の `signal.Notify` が `Acquire` と `cmd.Start()` の後にあり、中断で lock と孤児の子が残る

起票日: 2026-09-12
カテゴリ: bug / priority: **high**
対象: `src/lockman/with.go` の `runWith`
出典: [issue 358](358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) の敵対レビュー 5 周目 (観点③「並行・中断」)
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

[issue 356](356-bug-lockman-with-releases-lock-while-grandchildren-run.md) は
**子が正常終了した経路**で「孫が残っているのに解放する」話。本 issue の trigger は
**シグナルが早く届いた経路**で、356 を直しても消えない。

## 対応の候補 (未決)

- `Acquire` の前にハンドラを立てて、その時点から「受けたら解放して落ちる」を保証する
  (buffer だけでは足りず、Acquire 中に受けた場合の解放経路が要る)
- 子を起こす前に受けた場合と、起こした後に受けた場合で処理を分ける

## 残タスク

- [ ] 反証レビュー
- [ ] 対応方針の決定 (356 とまとめて直すかを含む)
