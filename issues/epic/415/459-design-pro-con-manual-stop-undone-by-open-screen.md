# 459 (design): 画面が開いているときの `pro-con dispatcher --stop` を、画面の keeper が約 10 秒後に取り消す

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

もろい作りの監査 (460) の P2。人が CLI で止めた (README が止め方として案内している形) のに、本物のモードの画面が 1 つでも開いていると、
画面が dispatcher を起こし直し、止めた印 (Stopped) の付いたカードの PG がすぐ再開する。止めたつもりが元に戻り、利用枠も使う。

## 詳細

- 該当: `src/pro-con/live/live.go` の `Backend.keep` (dispatcher の Tick が keepAfter より古ければ keeper を呼ぶ。止めないのは、この画面が閉じている途中か画面の印が無いときだけ) /
  `main.startDispatcherIfIdle`
- 発火条件: 本物のモードの画面が開いている間に `pro-con dispatcher --stop` を打つ
- 壊れ方: 止め終えた dispatcher が抜ける → Tick が古くなる → keeper が新しい dispatcher を起こす → Stopped のカードを自動の再開を待たずに再開する
- 根拠: コードを読んだ (監査の係・PM)。実行はしていない。`--view` の画面は keeper を持たないので起こさない
- 意図の反証: README の keeper の説明は「落ちた・前の画面が止めている最中に開いた」の起こし直しで、人が意図して止めた場合を区別していない

## 決めること

- 人が意図して止めたことを置き場に印として残し、keeper はその印がある間は起こさない (印を外すのは画面の明示の操作か、次の手動の起動) — が候補
- それとも「画面が開いている間は dispatcher は居るもの」として `--stop` を断る / 画面の側に「止まっている」を出す か
- 2026-09-25 の dogfooding の「dispatcher の入れ替え」(`--stop` → 手で起動) は画面が開いていない (--view だけ) ので、この挙動には頼っていない

## 関連

- 460 (監査の記録) / 456 (設定画面。止める・起こす口もそこに置けるか)
