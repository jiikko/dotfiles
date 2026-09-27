# 559 (perf): dispatcher が何もしていなくても 3 秒ごとに `claude agents --json` (claude 本体) を起こす

> 🚨 **担当中: dotfiles-01**（2026-09-27〜）

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dispatcher の Tick (3 秒。`dispatchercmd.go` の `dispatcherInterval`) は、毎回条件なしで `claude agents --json` を起こす
(`dispatcher/dispatcher.go` の Tick → `d.List` → `agents/agents.go` の `execAgents`)。カードが 1 枚も動いていなくても、
**claude 本体のプロセスが 1 分 20 回・1 日 約 2.9 万回**起きる (2026-09-27 に定数とコードで確かめた。500 の 15:10 の節)。

- 1 回 約 0.14 秒・CPU 0.13 秒 (macOS 27 で `time claude agents --json` を実測)。1 日で CPU 約 1 時間分
- 500 (macOS 15.7.7 でカーネルのメモリが漏れる) の容疑の 1 つ: 上流の報告では漏れが claude.exe のプロセスに付いて回る。
  macOS 27 では 1,000 回叩いても漏れなかった (500 の 15:10 の節)。漏れているマシンでは未測定

## 対応方針 (案。500 の測定を待たずに検討してよい)

- 一覧が要らない Tick では呼ばない: 生きている PG・PM・取り込みの係が居ない・起動の結果待ち (Launching) が無い・受付の箱に何も無いなら、間隔を延ばす (例 30 秒)
- 画面は dispatcher の一覧 (seen.json) を 15 秒以内なら使う (`live/live.go` の `seenFresh`) ので、間隔を延ばすときはこの鮮度の閾値も揃える
- 🚨 一覧の遅れで壊れるもの (起動の結果の確かめ・落ちた PG の検出・停滞の判定) を先に列挙してから延ばす (`survey-receiver-guards-before-passing-new-values.md`)

## 受け入れ条件

- [ ] カードが動いていない間の `claude agents --json` の回数が 1 分 20 回より大きく減る (実測で示す)
- [ ] 起動の確かめ・落ちた PG の検出が、延ばした間隔でも遅れすぎない (どこまで遅れてよいかを決めて書く)

## 関連

- [502](done/502-perf-pro-con-agents-list-spawned-per-screen-and-dispatcher.md) (画面と dispatcher がそれぞれ 3 秒ごとに起動していた。画面の側は 502 で直り、dispatcher 自身の常時の呼び出しが残りの論点として残っていた。559 はその続き)
- 500 (カーネルのメモリの漏れ) / 455 / 535

## 進捗

- 2026-09-27 着手 (dotfiles-01)。まだ実装していない。分かったこと:
  - 実機の状態 (`~/.local/state/pro-con/live`): カード 48 枚が全部完了・PM は `stopped`。この「暇」の状態でも 3 秒ごとに一覧を取っている
  - 一覧を使わない処理 (`watch` / `tickRuns` / `tickBtws` / `refreshUsage` / `collectProgress`) は暇でも仕事がある (完了したカードへの btw の回答など)。間引くのは一覧の取得と、それを使う処理だけにする
  - 画面は `seen.json` が `seenFresh` (15 秒) より古いと自分で `claude agents` を叩く (`live/live.go`)。間引くなら、この鮮度の閾値も揃える (`seen.json` に「いつまで使ってよいか」を書く案)
  - 役の様子は、一覧と照らせなかった Tick を「確かめ中」(`RoleChecking`) と出す (`dispatcher/role.go` の `roleState`)。一覧を飛ばす Tick で、この表示が出っぱなしにならないようにする
- 次の一手: pro-con 全体のポーリングを通知で動く形に置き換えられるかを調べている (`~/.claude/jobs` の変化の監視で一覧を取り直せるなら、間引くより筋が良い)。結果を見てから方式を決める
