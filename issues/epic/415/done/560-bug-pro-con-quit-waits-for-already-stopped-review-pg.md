# 560 (bug): 最後の画面を閉じると、レビュー待ちで既に止めた PG を約 46 秒待ってから諦める

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

`pro-con e2e scenario` の「閉じる (Q → quit)」から終わりまでが、**5 回とも 61 秒**だった (2026-09-27 15:31〜15:36 の計測。500 の 15:10 の節の計測のついで)。
19:55 にもう 1 回回して dispatcher.log を読むと、閉じる操作は受け付けられていて、止める処理がレビュー待ちの PG を待っていた:

```
19:55:06 C-001 の PG (e2e00003) がまだ動いていたので止め直した
19:55:06 C-001: レビュー待ちの間は PG の session を止めた (差し戻し・追加オーダーは同じ session を続きから再開する)
19:55:06 画面 94cb4d 持ち主 (pid 98629): quit で閉じた: 最後の画面なので dispatcher と PG を止める
19:55:52 C-001 の PG は落ちて戻らない / 一覧に出ないので止められない (列はそのまま。次の dispatcher が扱う)
19:55:52 1 枚のカードは列を変えずに残した (記録にある session は止まっていることを確かめた)
```

- 画面は「dispatcher と PG を止めています… ctrl+c: 待たずに閉じる」を出したまま待つ (`ui/quit.go`)
- `e2eStop` (`e2ecmd.go`) の 60 秒の待ちもほぼ使い切る。make test の `tests/pro-con/test_e2e_scenario.sh` も 1 回ごとに約 1 分余計にかかる (見立て。make test の中では測っていない)

## 原因 (反証レビューの指摘をコードで確かめた。2026-09-27 20:05)

- dispatcher の `trackDead` (`dispatcher/dispatcher.go`) は、一覧に出ない PG のカードに「消えたのを見た時刻」(`DeadSince`) を付ける。除くのは完了 (Done) のカードだけで、
  **レビュー待ちで意図して止めた PG にも付く**
- 終了の `stopTarget` (`dispatcher/shutdown.go`) は、`DeadSince` から `restartWait` (1 分) 以内なら「落ちて自動の再開を待っている途中」とみなして待つ
  (`shutdownPoll` 2 秒 × `shutdownPolls` 23 回 = 46 秒。上のログの 19:55:06 → 19:55:52 と合う)
- e2e に限らない: e2e の偽の一覧 (`dispatcher/e2e.go` の List / ListAll) も、本物と同じく止めた session を List で除き ListAll で残す。
  **本物でも、カードがレビューに出てから 1 分以内に最後の画面を閉じると同じく約 46 秒待つ**。e2e の scenario は閉じるのがレビューの直後なので毎回踏む

## 対応方針 (案)

- 意図して止めた PG (レビュー待ちの停止) を「落ちた」と記録しない。`trackDead` で、dispatcher 自身が止めたと記録しているカードには `DeadSince` を付けない
  (付けてしまうと、終了の待ちのほかに、落ちた回数や自動の再開の判定にも混ざらないかを先に洗う)

## 受け入れ条件

- [x] e2e の scenario の「閉じる」から終わりまでが数秒になる (修正前 60 秒 → 修正後 0〜1 秒。下の結果)
- [ ] 本物でも、レビュー待ちの PG が居るときの quit が待たない (実測): 未実測。e2e の偽の一覧は本物と同じく止めた session を
  List で除き ListAll で残すので、同じ経路を通る。**trigger**: 次に本物の pro-con でカードがレビューに出た直後に閉じたとき、
  dispatcher.log の「quit で閉じた」から終わりまでの間を見る

## 結果 (2026-09-28、dotfiles-01)

- 直し方 (commit「pro-con: レビュー待ちで止めた PG に落ちた時刻を付けず、終了が待たない (560)」):
  - `card.MarkStopped` (`card/card.go`): 止めた印 (`Stopped`) を立てて、落ちたのを見た時刻 (`DeadSince`) を外す。`Stopped` を立てる
    `close.go` の `finishMarkedStop`・`shutdown.go`・`fake/fake.go` をこれに寄せた
  - `trackDead`: `Stopped` のカードには `DeadSince` を付けない。付いていれば外す (前の版が残した記録も次の Tick で直る)
  - 止める要求を出しただけ (`StopSent`) の間は従来どおり付ける。止まったのを確かめた Tick で `MarkStopped` が外す
- 実測 (`pro-con e2e scenario`。修正前は claim の commit の版を同じ手順で): 「閉じる」から「通った」まで **60 秒 → 0〜1 秒**、通し 69 秒 → 9 秒
- 検証: `make -C src/pro-con lint` 0 issues / `make -C src/pro-con test` (`-race`) 21 パッケージ ok
- 変異 (`bin/mutate-verify`、想定のテストが red): `trackDead` の `Stopped` の除外を外す / `MarkStopped` が `DeadSince` を外さない
  (dispatcher の Tick を通るテストと card のテストの両方)
- 敵対的レビュー (opus、1 周 + 修正の確認):
  - P1 無し。採用: 最初の版は `StopSent` も「落ちていない」に含めていたが、(a) 削除中の作業中カードで、止める要求の後に本当に落ちた PG を
    自動の再開を待たずに記録から外し、PG を取り残しうる (b) 止め終える前の差し戻しの再開が遅れる、を生んでいた。`StopSent` を外しても、
    止めたのを確かめた Tick で `MarkStopped` が外すので終了は待たない → `Stopped` だけに狭めた。
    `MarkStopped` の消去を守るテストが card の単体だけだった → dispatcher の Tick を通るテスト (`TestReviewStopConfirmedLaterClearsDeadSince`) に置き換えた
  - 記録だけ (到達しない): 作業中で `Stopped` のカードは、`trackDead` が `DeadSince` を付けないので、消えた PG の戻し (`gone` / `requeueVanished`) が
    働かない。作業中へ入る経路 (`settle`) が `Stopped` を外すので今のコードでは作れない。作業中のまま `Stopped` を残す経路を足すときは再評価する
