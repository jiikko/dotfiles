# 490 (feat): カードの右上に、見積もりの重さ (ポイント) を数字で出す

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「カードの右上に、カードの重さ (見積もり)・実際の重さみたいな指標を数字で書いて」。
どの数にするかを聞き、ユーザーが選んだ:

- **見積もり = ポイント 1 / 2 / 3 / 5 / 8** (相対の大きさ)。PM がカードを分解してキューに積むとき (`pro-con card plan`) に付ける
- 作るのは PG (このカード)。見た目は見本を出して人間が選ぶ

## 決定 (2026-09-26。見本 `tmp/pro-con-490-effort-sample.py` の案 A / B / C を見た後のユーザーの決定)

- **カードに出すのはポイントだけ**。当初の「実際 = 作業中だった時間の合計」は出さない。
  表示しない数は使い道が無いので、記録もしない (一度入れた 829df4c5 の作業中の時間の欄と足し込みは外した。`card show`・詳細にも出さない)
- **表示**: カードの 1 行目の右端に「3pt」を薄く (dim)。見積もり無しは出さない。タイトルは 1 行目で切らずに 2 行目へ続ける。
  狭くてタイトルの 1 行目が 8 桁より短くなるなら、ポイントを出さない (`ui/view.go` の `cardCell` / `minTitleHead`)
- `pro-con card plan <カード> --issue <repo>#<番号> --points <1|2|3|5|8>`。0 を含むほかの値は使い方の誤り (箱に手で置いた依頼も store で除ける)。
  付けないカードは見積もり無し
- `pro-con card show` と詳細 (enter) に「見積もり: 3pt」の行 (見積もり無しなら行を出さない)
- **ポイントの意味 (ヘルプ)**: 相対の大きさであって時間の見積もりではない / 付けるのは PM が積むとき / 1・2・3・5・8 の目安。
  文面の正本は `src/pro-con/card/points.go` の `PointsMeaning` の 1 か所で、画面の ? の表・`pro-con card` の使い方 (plan --points)・
  PM の指示書 (`pm-guide.md` の役目 3。`{{ポイントの目安}}` の行を差し込む) がそこから出す。テスト: `points_test.go` / `ui/points_test.go` / `store/points_test.go`

## 関連ファイル

- `src/pro-con/card/card.go` (`Card.Points`) / `src/pro-con/card/points.go` (値の検査・意味の正本) / `src/pro-con/store/store.go` (`case "plan"`) / `src/pro-con/cardcmd.go` (`plan` の引数)
- `src/pro-con/ui/view.go` の `cardCell` / `src/pro-con/ui/legend.go` (? の表) / `src/pro-con/cardview.go` (`card show`) / `src/pro-con/ui/drawer.go` (詳細) / `src/pro-con/pm-guide.md` の役目 3

## 関連

- 468 (PM の見積もり = `--after`。同じ積む時に付ける) / 469 (カードを開いたら進捗) / 455 (作業中と待ちを分ける。作業中の時間を出さないことにしたので、定義を揃える必要は無くなった)

## 順番の見積もり (PM, 2026-09-26。C-047 を C-033 の後に積んだ)

- 触る場所: `card.Card` の欄、`store/store.go` の `case "plan"` と列の移り変わり、`cardcmd.go` の `plan` の引数、`ui/view.go` の `cardCell` のタイトルの 1 行目の右端、`cardview.go`、`pm-guide.md` の役目 3
- 変える判断: 「作業中」に居た時間として何を数えるか
- 順番の理由: 455 (C-033) が「作業中」を、PG が動いている分と待っている分に分ける (枠の数え方は push 済みで、ボードの見た目は人の回答待ち)。同じ定義を 2 つのカードで別々に決めないよう、455 の後にする (依頼の原文でも指定)。452 (C-032) と 485 (C-043) も `cardCell` を触るが、触るのはバッジの行でこちらはタイトルの行なので、順番は付けていない
