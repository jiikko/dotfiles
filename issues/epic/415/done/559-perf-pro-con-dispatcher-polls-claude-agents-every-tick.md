# 559 (perf): dispatcher が何もしていなくても 3 秒ごとに `claude agents --json` (claude 本体) を起こす

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

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

- [ ] カードが動いていない間の `claude agents --json` の回数が 1 分 20 回より大きく減る (テストでは 30 秒に 1 回。本物では未実測。PM が生きている間は減らない)
- [x] 起動の確かめ・落ちた PG の検出が、延ばした間隔でも遅れすぎない (どこまで遅れてよいかを決めて書く): 間引くのは起動の結果待ち・
  生きている PG・役が 1 つも無いときだけなので、起動の確かめと落ちた PG の検出は遅れない。遅れるのは暇な間の外の出来事 (最大 30 秒)

## 関連

- [502](502-perf-pro-con-agents-list-spawned-per-screen-and-dispatcher.md) (画面と dispatcher がそれぞれ 3 秒ごとに起動していた。画面の側は 502 で直り、dispatcher 自身の常時の呼び出しが残りの論点として残っていた。559 はその続き)
- 500 (カーネルのメモリの漏れ) / 455 / 535

## 進捗

- 2026-09-27 着手 (dotfiles-01)。まだ実装していない。分かったこと:
  - 実機の状態 (`~/.local/state/pro-con/live`): カード 48 枚が全部完了・PM は `stopped`。この「暇」の状態でも 3 秒ごとに一覧を取っている
  - 一覧を使わない処理 (`watch` / `tickRuns` / `tickBtws` / `refreshUsage` / `collectProgress`) は暇でも仕事がある (完了したカードへの btw の回答など)。間引くのは一覧の取得と、それを使う処理だけにする
  - 画面は `seen.json` が `seenFresh` (15 秒) より古いと自分で `claude agents` を叩く (`live/live.go`)。間引くなら、この鮮度の閾値も揃える (`seen.json` に「いつまで使ってよいか」を書く案)
  - 役の様子は、一覧と照らせなかった Tick を「確かめ中」(`RoleChecking`) と出す (`dispatcher/role.go` の `roleState`)。一覧を飛ばす Tick で、この表示が出っぱなしにならないようにする
- 通知で動く形への置き換えは採らない (2026-09-27 に pro-con 全体のポーリングを調べた結論): `~/.claude/jobs/<id>/state.json` に pid が無く、
  `claude agents --json` の呼び出しは省けない。依頼の側からの起こし (`wake.Poke`) と画面への知らせ (`Changed`) は既に通知になっている
- 実装 (pro-con: 暇な間は dispatcher の session の一覧を 30 秒に 1 回へ間引く):
  - `dispatcher/idle.go` の `listing` / `quiet`: カードが全部完了 (止める途中・削除待ち・起動の結果待ちが無い) で、役が off・session 無し・
    止めてあるなら暇。前に一覧を取った Tick も暇なときだけ間引く (忙しい → 暇の直後は 1 回取り直す)
  - `tick` は一覧を使う経路 (`reconcile` = 登録〜消えた PG、`deliverOrders`、役、割り当て、`collectDoing`) を一覧を取れた Tick だけ回す。
    一覧を使わない経路 (`watch` / `tickRuns` / `tickBtws` / `refreshUsage` / `collectProgress`) は毎 Tick
  - 画面の鮮度: `store.Seen` に `Keep` (次の取得までの最長の間) を書き、`Seen.Fresh` が `SeenFresh` (15 秒) + `Keep` (上限 2 分) まで使う。
    判定を `live/live.go` から `store` へ寄せた
  - 間引いた Tick は役の様子の鮮度を前の値に戻す (「確かめ中」を出さない)
  - 受付の箱の依頼の有無は判定に入れない: 依頼は一覧の判定より前に記録へ適用されるので、カードが増えた・動いた Tick は `quiet` が偽になる
- 検証: `make -C src/pro-con lint` 0 issues / `make -C src/pro-con test` (`-race`) 21 パッケージ ok。
  既存の `serve_test.go` 6 本は一覧の回数で Tick を数えていたので、間引きを切る (`listEveryTick`)
- 変異 (`bin/mutate-verify`、7 本とも想定のテストが red): 常に一覧を取る / 忙しい直後の取り直しを外す / `StopAfterClose` を外す /
  生きている PM を外す / 役の様子の鮮度を戻さない / `Keep` を書かない / `Fresh` が `Keep` を見ない
- 回数 (見積もり。本物の dispatcher で未実測): 暇な間は 1 分 20 回 → 2 回
- 敵対的レビュー (opus、2 周) の結果 (2026-09-28。後追いの commit「pro-con: 559 の敵対的レビューの対応」):
  - 1 周目: 実害 (PG が残る・二重起動) の手順は無し。採用 3: 間引いた Tick でも一覧を使わない経路が回るテストが無い
    (`TestQuietTickStillAnswersBtw` を足した) / 取り込みの係・記録を読めないときの条件にテストが無い / 時計が戻ったときの分岐は
    本番で到達しない (外した)。rig のカードの `Since` が zero で、完了したカードが最初の Tick で書庫へ移り、前のテストは空の記録で
    回っていた (直した)。却下 1: 「間引き中に一覧の取得が失敗すると取り直しは 30 秒後」— 取得に行くのは前回の成功から 30 秒経った
    ときなので、失敗の次の Tick で取り直す
  - 1 周目の P2「生きている PM が居ると暇にならない (効果が出ない)」を受けて生きている役を暇の妨げから外したが、2 周目で戻した:
    PM の入力待ちの知らせが 3 秒 → 最長 30 秒に遅れ、間引いている間に PM が落ちると `DeadSince` が付かず、終了 (`stopRole`) が
    自動の再開を待たずに抜けて再開した PM が残りうる。理由は `dispatcher/idle.go` の `quiet` のコメントに残した
  - 記録だけ (P3): 画面が dispatcher の一覧を信じる上限が 15 秒 → 最長 45 秒に延びた。誤った表示になるのは dispatcher が落ちた・
    終了した直後だけ (変更前も 15 秒までは同じ)。次の取得までの余裕は約 2 秒で、足りなければ画面が自分で 1 回叩くだけ
- 変異 (2 周目の後に当て直し。10 本とも想定のテストだけが red): 上の 7 本 + 間引いた Tick で後段を飛ばす / 取り込みの係を外す /
  記録を読めないとき暇と言う
- 効果が出る範囲 (限界): PM と取り込みの係が off・止めてある・session 無しで、カードが全部完了の間だけ。PM が生きている間は
  従来どおり 3 秒ごとに取る。本物の dispatcher で暇な間の回数は未実測 (テストでは 30 秒に 1 回)。
  **再開の trigger**: PM が常駐している間の `claude agents` の呼び出しも減らしたくなったとき (入力待ちの知らせと `DeadSince` を
  一覧なしで保つ仕組みが要る)
