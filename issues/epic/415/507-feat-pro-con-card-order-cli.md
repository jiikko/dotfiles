# 507 (feat): 追加オーダーを CLI から送る `pro-con card order` (今は画面の + キーだけ)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「追加オーダーを CLI から送る口を実装する issue とカードを作って」。

2026-09-26 18:00、作業中の C-061 (506) に部品の名前の決定を届けようとしたら、CLI には追加オーダーの口が無かった
(`pro-con card` の操作は add / plan / ask / answer / handoff / review / rework / close / delete / move だけ)。
画面の `+` キーと同じ依頼 (`store.Request{Kind: "order", …}`) を、使い捨ての Go のプログラムから `store.Submit` で受付の箱に置いて送った。
外の Claude (取り込みの係の代わりをした session・PM) と人の shell からは、同じことを正規の口で出せない。

## 今の形 (2026-09-26)

- 画面: `+` で追加オーダーの入力 (`ui/model.go`)。種類は 追記 (`card.OrderAppend`) / 方針変更 (`card.OrderRedirect`)。別件は新しい依頼にする
- 箱の依頼: `live/live.go` が `store.Request{Kind: "order", CardID, Order, Text}` を置く。適用は `store/store.go` の `case "order"`
  (完了のカードは断る・本文が空なら断る・追記 / 方針変更以外は断る)。PG へ届けるのは dispatcher (`dispatcher/orders.go`。追記は PG の turn の区切りで、方針変更は PG を止めて届ける)

## 期待する動作

- `pro-con card order <カード> "<本文>" [--redirect] [--from <人間|PM>]`: 既定は追記、`--redirect` で方針変更。箱に置き、適用は dispatcher (ほかの操作と同じ)
- 断られる条件は画面と同じ (store の適用が正本。CLI で先に弾くのは使い方の誤りだけ)
- `pro-con card` の使い方の文・README のコマンドの一覧・pm-guide (PM が人の追加の指示を PG に渡すとき) に載せる
- 指示書のコマンドがパーサに通る検査 (`TestPMGuideCommandsParse` / `TestIntegratorGuideCommandsParse`) に載る形にする

## 関連

- 438 (追加オーダー・btw・片付け) / 506 (C-061。CLI の口が無くて困った実例)
