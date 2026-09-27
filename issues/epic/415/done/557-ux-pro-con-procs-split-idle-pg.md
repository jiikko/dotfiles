# 557 (ux): プロセスの一覧で、枠を使っていない (待機中の) PG を別のセクションに分ける

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27) でユーザーが「PG の枠は設定画面では 2 なのに、実際には 3 つ起動している」と気づいた。乖離ではなく今の作りどおりだが、一覧から理由が読み取れない。

- 枠 (同時に動かす PG の上限。既定は `dispatchercmd.go` の `--limit` の 2) に数えるのは、turn の途中の PG (`card.HoldsPGSlot`) だけ (issue 455 の決定)
- 質問して turn を終えた PG・回答や差し戻しを受けて同じ session の再開を待つ PG (着手待ちの列の ↻) は、プロセスが生きていても枠に数えない
- 2026-09-27 18:40 の実物 (`pro-con ps`): 作業中 C-003 (CPU 2.2%) / C-005 (1.5%) と、18:32 に質問して止まったまま再開待ちの C-002 (0.0%) の 3 つが同じ並びに出ていた。
  C-002 の状態の欄は「再開待ち (PG の空き待ち)」(`pscmd.go` の `pgState`) と出ているが、枠を使っていないことはどこにも出ない。
  設定画面のプロセスのタブの要約 (`ui/settings.go` の `procsSummary`) も「動いている 3」と数える

## 対応方針

- `pro-con ps` と設定画面のプロセスのタブで、PG を「作業中 (枠を使う)」と「待機中 (枠を使わない)」の 2 つのセクションに分ける。待機中の行には待っている訳を出す (今の `pgState` の訳を使う)
- 要約を「PG 作業中 2 / 枠 2 · 待機 1」の形にし、設定の枠と比べられる数を出す
- 🚨 **枠に数えたカードは dispatcher が決めて書き、一覧はそれを読むだけにする**。dispatcher は tick ごとに `dispatch` で数えた
  (`card.HoldsPGSlot` を通した) カードの ID を `dispatcher-state.json` (`store/dispatcher_state.go`) に書き、`pro-con ps`・設定画面のプロセスのタブ・
  ヘッダの「PG n/枠」(`ui/view.go`) がそれを読む
  - 一覧の側で同じ判定を組み直さない理由: `card.HoldsPGSlot` に要る idle は `claude agents` の一覧 (`dispatcher/orders.go` の `idle`) から決まるが、
    `pro-con ps` (`collectProcs`) は「claude agents は最大 10 秒かかり本物の claude を起こすので読まない」作り (`pscmd.go` 冒頭)。
    画面の `Snapshot.SlotsUsed` (`backend/backend.go`) は判定を通しているが、コメントのとおり母数が dispatcher と違う (一覧に居ない作業中の PG と
    Launching のカードを数えない)。dispatcher の書いた値に寄せると、このずれも一緒に消える
  - dispatcher が止まっている・古い (最後の Tick が古い) ときは、分けずに「判定できない」と出す (読めないのを 0 に見せない)

## スコープ外

- 待機中の PG の session を止めること (レビュー待ちの 536 と同じ扱いにする案)。何もしない claude が生き続けることは、メモリと、500 の見立て
  (claude が居るだけでカーネルのメモリが漏れる) にしか効かない。500 で漏れているマシンの測定をして、何もしない claude が漏らしていると分かってから考える

## 受け入れ条件

- [x] `pro-con ps` で、枠を使っている PG と待機中の PG が別のセクションに出る。待機中の行に待っている訳が出る
- [x] 設定画面のプロセスのタブも同じく分かれ、要約で「作業中の数 / 枠」と「待機の数」が読める
- [x] 一覧とヘッダは dispatcher が書いた「枠に数えたカード」を読むだけにする (一覧の側に判定を書かない。`Snapshot.SlotsUsed` の母数のずれも消える)
- [x] dispatcher が止まっている・古いときは「判定できない」と出る

## 進捗

- 2026-09-27 (pro-con C-007): `pro-con: プロセスの一覧で PG を枠を使う作業中と待機中に分け、枠の数は dispatcher が書いたものを読む (557)`
  - dispatcher は `dispatch` で枠に数えたカード (上限と比べるのと同じ値) を `dispatcher-state.json` の `slots` / `slots_at` に書く。途中で抜けた割り当て・一覧を取れない Tick は書き直さない
  - `pro-con ps` と設定画面のプロセスのタブは `backend.SplitPGs` で「PG 作業中 (枠を使う) n / 枠 m」「PG 待機中 (枠を使わない) k」に分ける (`--json` は各行の `slot`)。
    枠に数えたが起動の記録にまだ無いカードも作業中に 1 行出す。待機中の行の訳は `pgState` (「作業中 (テストの係の結果待ち)」を足した)
  - ヘッダの「PG n/枠」・設定の要約「PG 作業中 n / 枠 m · 待機 k」は `slots` を読むだけ (`Snapshot.SlotsUsed` を Consumers から組み直さない)
  - 人が止めた・dispatcher が居ない・`slots_at` が `DispatcherStale` (2 分) より古いときは、分けずに「判定できない」、ヘッダは `PG ?/m` (`backend.CountedSlots`)。
    古さは Tick ではなく数えた時刻で見る (一覧を取れない Tick が続くと Tick は新しくても数は古い)
- 確かめたこと: `make -C src/pro-con test` (go test -race) 全 ok / `CGO_ENABLED=0 make -C src/pro-con lint` 0 issues
  (cgo ありは golangci-lint 本体のリンクが SDK 27.0 の `.tbd` の `arm64e.x1` を読めずに落ちる。環境の問題)。
  新しいテスト (`pscmd_slot_test.go` / `backend/inspect_test.go` / `TestSettingsProcsSplitsPGsBySlot` / `TestSlotSkipsIdlePGAwaitingRun` の slots の確認) は実装を 8 か所壊して 8 か所とも赤になる。
  模擬モードを隔離した tmux で開き、プロセスのタブと要約を撮った (カードに添付)。codex の敵対的レビュー 3 件は、2 件を再現した状態のテストで否定し、1 件は意図どおり (コメントに明記)
- 残り: 本物の dispatcher で `pro-con ps` の見た目を確かめるのは、取り込み後に dispatcher が新版になってから

## 関連

- 455 (枠に数えるのは turn の途中の PG) / 535 (`pro-con ps` の PG の状態の欄) / 536 (レビュー待ちの PG を止める) / 500 (カーネルのメモリの漏れ)
