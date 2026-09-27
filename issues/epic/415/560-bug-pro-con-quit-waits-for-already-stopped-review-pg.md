# 560 (bug): 最後の画面を閉じると、レビュー待ちで既に止めた PG を約 46 秒待ってから諦める

> 🚨 **担当中: dotfiles-01**（2026-09-28〜）

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

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

- [ ] e2e の scenario の「閉じる」から終わりまでが数秒になる
- [ ] 本物でも、レビュー待ちの PG が居るときの quit が待たない (実測)
