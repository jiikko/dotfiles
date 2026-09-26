# 545 (bug): 人の番の目印の残り — review / run の後も動く PG の入力待ち・並びが戻る・r を断る分岐のテスト

起票日: 2026-09-27

親: [415](../415-design-claude-pm-worker-orchestration.md) / 切り出し元: [452](452-ux-pro-con-mark-cards-waiting-for-human.md)

## 概要

452 の敵対レビューで見つけた、452 より前からの穴か判断が要るもの (452 の「残り」から移した)。

- PG が `card run` / `card review` の後も同じ turn で動き続けて入力待ちで止まると、人の番にならない (テストの係の結果待ちは待ちを上書きしないので移さない /
  レビュー待ちの列は見ていない)。テストの結果を渡す再開はその問いを殺す。PG の規律 (頼んだら turn を終える) に頼っている
- 入力待ちに出入りするたびに `Since` が変わり、人が並べ替えたレーンの順 (`card/rank.go`) が既定へ戻る。問いのたびに履歴 2 行・出来事 1 つ・通知 1 回
- 画面の `r` を入力待ちで断る分岐 (`ui/model.go`) にテストが無い

## 受け入れ条件

- [x] 1 つ目: 起きる形を決めてから直すか見送るかを決め、理由を書く (536 でレビュー待ちの PG は止まるので、レビューの列の分はもう起きない見込み。未確認)
- [x] 2 つ目: 入力待ちの出入りで並びが戻らない (テスト)
- [x] 3 つ目: `r` を断る分岐にテストがある (変異で落ちる)

## 進捗

- 2026-09-27 (ユーザーと話す Claude):
  - 1 つ目 (run / review の後も動く PG の入力待ち) は **見送り**。結果待ちのカードを人の番へ移すと待ち (WaitResource) を上書きして頼みを見失い、両方を持つには 1 枚に待ちを 2 つ持たせる作りの変更が要る。
    PG の規律 (頼んだら turn を終える) に頼る。レビューの列は 536 で PG を止めるので起きない。理由と見直す条件は `dispatcher/prompt.go` の `trackPrompts` のコメント。
    「問いのたびに履歴 2 行・出来事 1 つ・通知 1 回」は、人の番を知らせる本題なので今のまま
  - 2 つ目: `card.Card` に `PromptLaneKey` を足し、`EnterPrompt` で作業中の列での位置 (`LaneKey`) を覚え、`LeavePrompt` で `Rank` をその位置で付け直す。
    テスト `card/rank_test.go` の `TestPromptRoundTripKeepsLanePosition` (入った順と、人が並べた順の両方)
  - 3 つ目: テスト `ui/progress_test.go` の `TestAnswerRefusesPromptWaitingCard` (入力待ちのカードで r → 入力欄を開かず「a で attach して答える」)
  - 変異: 位置を覚えない → red / Rank を付け直さない → red / r の断りの分岐を外す → red (「回答できるのは質問待ちのカードだけ」が出る)。pro-con の全テストと make lint は緑

