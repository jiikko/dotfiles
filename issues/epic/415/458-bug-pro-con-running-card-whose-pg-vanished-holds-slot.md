# 458 (bug): PG の session が一覧から消えた作業中のカードが、作業中のまま PG の枠を占め続ける

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の P2。落ちた回数に数えられない消え方をした PG のカードに、作業中から出る経路が無い。

## 詳細

- 該当: `src/pro-con/dispatcher/dispatcher.go` の `trackDead` (一覧に無い / pid 無しを見たら DeadSince を付けるだけ) / 落ち続けたら止める所 (再開の文が transcript に出たときだけ数える) /
  `dispatch` (作業中の列のカードを全部 `running++` で枠に数える)。DeadSince を見るのは、落ち続けたときの停止・分解済みの再開・終了の `stopTarget` だけ
- 発火条件: 作業中の PG が、自動の再開なしに一覧から消える。外から `claude stop` された / `--stop` の子が Stop の後・カードを書き直す前に SIGKILL で死んだ /
  マシンを再起動した (430 と重なるのはこの形だけ)
- 壊れ方: カードは作業中のまま。answer (質問待ち)・rework (レビュー)・close (レビュー / 依頼) のどれも作業中では受けないので、人も PM も動かせない。
  枠を占め続けるので、枠が 1 なら次のカードが起動しない (枠のせいではないので hold の知らせも出ない)。抜けるのは quit → 開き直しだけ。停滞の印は 15 分で付く
- 根拠: 監査の係のプローブで再現 (枠 1 で session を一覧から消し、模擬時間で 200 分回しても C-001 は作業中・C-002 は分解済みのまま・起動は 1 本)。
  PM が `DeadSince` の使われ方を grep して、作業中のカードを動かす所が無いのを確かめた (2026-09-25)

## 対応方針 (候補)

- DeadSince から restartWait を過ぎても戻らない作業中のカードは、分解済みへ戻して同じ session を再開する (終了の `Shutdown` が既に同じ判定で分解済みへ戻している。それを使う)
- 戻したことを履歴と出来事に書く

## 進捗

### 2026-09-25 (C-011)

- やったこと: Tick の落ち続けたときの停止 (`stopCrashing`) の直後に `requeueVanished` を足した (`dispatcher.go`)。
  判定は `gone` (`shutdown.go`) で、終了の `stopTarget` が「止めるものが無い・待たない」と返す形に、作業中・起動の印なし・記録にある・
  DeadSince ありを重ねたもの。戻し方は Shutdown と共有の `requeue` (分解済み + 再開の文 + テストの係の頼みを取り下げ) に抜き出した。
  同じ Tick の割り当てが同じ session を止めずに再開する。履歴と出来事 (`crash`) に「一覧から消えて戻らない」と書く
  - DeadSince を条件に入れたのは、短い id だけ変わって session id が生きている形 (trackDead は生きていると見て外す) を、stopTarget が
    「一覧に無い」と読むため
  - 起動・再開が済んだら (`settle`) DeadSince を外すようにした。敵対レビューの指摘: 再開が同じ短い id を返すと (未実測)、記録の前の行が当たり、
    一覧に出る前に前の時刻で「消えた」と読んで再開し直す (`TestVanishedResumeDoesNotRepeatBeforeListed` で red を見てから直した。変異でも red)
- 確かめたこと: 偽の lister で一覧から消した形を作り、直す前に `TestVanishedRunningCardResumes` が red (作業中のまま・再開 0 回)。
  変異 4 本 (`bin/mutate-verify`) がそれぞれ想定のテストで red: 呼び出しを外す / 停止より前に呼ぶ (`TestVanishedAfterCrashLimitStillAsksHuman`)
  / restartWait の待ちを外す / DeadSince の条件を外す (`TestLiveSessionUnderOtherShortIDIsNotVanished`)
- 残り:
  - pid 無しのまま restartWait を過ぎても一覧に残る形 (自動の再開が止まった) は扱っていない (stopTarget は「止める」と返すので gone は偽)
  - 記録に載る前 (起動は返ったが一覧に一度も出ない) に消えた作業中のカードは扱っていない (再開に要る session id と cwd が無い)
  - 消える → 再開 → また消える、を繰り返しても落ちた回数に数えないので、止まらずに再開し続ける (外から止めた / 再起動の形では起きにくい)

## 関連

- 460 (監査の記録) / 455 (枠に何を数えるか) / 430 (再起動の後も bg session が残るか)
