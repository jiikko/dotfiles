# 438 (feat): 本物のモードで追加オーダー・btw・片付けを受ける

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

本物のモード (live backend) が受ける操作は新しい依頼 (`n` / `i`) と回答 (`r`) だけで、追加オーダー (`+`)・btw (`w`)・
完了のレーンの片付け (`x`) は押した時点で断る (`live.Backend.Accepts`)。模擬のモードでは動く。

## 対応方針 (候補)

- 追加オーダー / btw: カードの PG に届ける。425 の実測で、idle の bg session には SendMessage が届き、busy なら今の turn の後に届く
  (AskUserQuestion で止まっている session には届かない)。受付の箱に種類を足し、dispatcher が PG へ送る
- 片付け: 完了のカードを画面から外す (記録からは消さない)。受付の箱で dispatcher が印を付ける
- どれも「書き手は dispatcher だけ」(426 の決定 1) を守る

## 関連

- `src/pro-con/live/live.go` の `Accepts` / `Apply`
