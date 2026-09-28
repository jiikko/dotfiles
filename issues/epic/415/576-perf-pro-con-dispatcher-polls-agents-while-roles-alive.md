# 576 (perf): 役 (PM・取り込みの係) が生きている間、dispatcher が 3 秒ごとに `claude agents --json` を起こし続ける

> 🚨 **担当中: dotfiles-f9**（2026-09-28〜）

起票日: 2026-09-28

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

[559](done/559-perf-pro-con-dispatcher-polls-claude-agents-every-tick.md) で、暇な間 (`dispatcher/idle.go` の `quiet` が真) に
session の一覧を取る間隔を 3 秒から 30 秒 (`defaultQuietListEvery`) へ延ばした。ただし `quiet` は、**生きている役
(`pm.Session != "" && !pm.Stopped`) が 1 つでも居ると偽**を返す。PM を常駐させる使い方では、カードが全部完了していても
Tick (`dispatchercmd.go` の `dispatcherInterval` = 3 秒) のたびに `claude agents --json` (`agents/agents.go` の `execAgents`)
が起き、559 の対策が効かない。1 分 20 回・1 日約 2.9 万回・CPU 約 1 時間/日 (いずれも 559 の実測値。1 回 0.14 秒 / CPU 0.13 秒、macOS 27)。

559 は、生きている役を暇の妨げから外す案を敵対的レビューの 2 周目で戻している (理由は `quiet` のコメント)。一覧で見張っているのは次の 2 つ:

1. **役の入力待ち (人への知らせ)**: `dispatcher/role.go` の `tellRole` が一覧の session の様子を見て知らせる。間引くと最長 30 秒遅れる
2. **落ちた時刻 (`DeadSince`)**: 終了の処理 (`stopRole`) は、Claude Code の自動の再開 (約 25 秒) を待つかどうかをこの時刻で決める。
   間引いている間に役が落ちると `DeadSince` が付かず、`stopRole` が待たずに抜け、そのあと再開した PM が残りうる

## 対応方針 (案。どれも未検証)

一覧を取らずに上の 2 つを保つ仕組みを先に用意し、その後で「生きている役」を `quiet` の妨げから外す。

- 入力待ち: 一覧以外に知らせる経路が無いか調べる (Claude Code の hook (Notification / Stop) で状態をファイルへ書く、など)。
  `claude agents --json` の様子との対応は実物で測ってから決める (`measure-external-cli-streams-separately.md`)
- `DeadSince`: 役の pid の生死を一覧なしで見る案。🚨 `kill(pid, 0)` はゾンビでも pid の再利用でも成功するので、それだけでは判定しない
  (`mutation-verify-new-tests.md` の「生死を kill(pid, 0) で判定しない」)。起動時刻との突き合わせなども含めて検討する
- 代わりの案: 役が生きている間は 3 秒と 30 秒の間の間隔 (例 10 秒) にする。入力待ちの遅れと、`DeadSince` の取りこぼし
  (約 25 秒の再開待ちの窓より短い間隔が要る) を受け入れられるかで決める
- 🚨 一覧の遅れで壊れるものを先に列挙してから延ばす (559 と同じ。`survey-receiver-guards-before-passing-new-values.md`)。
  画面が dispatcher の一覧を信じる鮮度 (`store.Seen` の Keep / `live/live.go` の `seenFresh`) も合わせて見直す

## 対象外

- 動いているカード (完了していないカード・起動の結果待ち) がある間の 3 秒ごとの取得。PG の起動の確かめと落ちた PG の検出に要る

## 受け入れ条件

- [ ] PM が生きていて、カードが全部完了している間の `claude agents --json` の回数が 1 分 20 回より大きく減る (本物の dispatcher で実測する)
- [ ] 役の入力待ちの知らせの遅れを、延ばす前と比べて実測して書く
- [ ] 間引いている間に役が落ちても、`stopRole` が自動の再開を待ってから抜ける (変異で red を確かめたテスト)

## 関連

- [559](done/559-perf-pro-con-dispatcher-polls-claude-agents-every-tick.md): 暇な間の間引き。本 issue はその「再開の trigger」
- [502](done/502-perf-pro-con-agents-list-spawned-per-screen-and-dispatcher.md): 画面の側の呼び出し
- [500](../../done/500-bug-macos-kernel-zone-leak-from-tmux-clients.md): カーネルのメモリの漏れ。559 は `claude agents` の多さを容疑の 1 つに挙げていた

## 進捗

- 2026-09-28 起票。まだ着手していない
- 2026-09-28 反証レビュー (read-only のサブエージェント 1 本): 反証できず。`quiet` が生きている役で偽になることは
  `dispatcher/idle_test.go` のテーブル (「PM が生きている」の行) が既に固定している。`DeadSince` の取りこぼしは今は起きない
  (役が生きている間は毎 Tick 一覧を取るので)。間引いたときに起きる問題として書いている
