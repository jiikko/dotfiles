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

## 決定 (2026-09-25・取り込みの係)

- 人が意図して止めた (`pro-con dispatcher --stop`) ことを状態の置き場に印として残し、印がある間は画面が dispatcher を起こさない。
  ゲージには「止めてある」と出す。印を外すのは画面の明示の操作か、次に手で dispatcher を起動したとき。
  画面の quit で止めた場合 (最後の画面) は印を置かない

## 対応 (C-027)

- 印は `store.HeldFile` (`<state>/dispatcher-held`)。`--stop` は止める前に置く (止め終えた直後の keeper に先を越されない)
- 画面が起こす dispatcher・画面の quit の停止・e2e の後始末には内部用の `--from-screen` を付ける (`dispatcherCmd` / `stopCmd`)。
  付いた `--stop` は印を置かない。付いていない起動 (手の `pro-con dispatcher`) は lock を取ってから印を外す
- 画面の側: `startDispatcherIfIdle` (開いたとき・keeper の両方が通る) が印を見て起こさない。印を見てから起こすまでに `--stop` が
  来ても、起こされた dispatcher (`--from-screen`) が lock を取ってから印を見て回らずに抜ける
- 印がある間は、止めている途中で画面が開いても止めるのをやめない (`stopUntilDone`。やめると、続ける者 = 画面の起こす dispatcher が来ない)
- ゲージは黄で「dispatcher 止めてある (c で起こす)」。`c` (continue。glogx で空いている字) → y/N で印を外して起こす。
  印があるときだけ効き、案内にもそのときだけ出す。`--view` では受けない
- テスト: `src/pro-con/held_test.go` (発火条件を e2e モードの偽物で: 画面を開いたまま `--stop` → keeper / 開いた画面 / `--from-screen` の
  dispatcher が起こさない・回らない、手の起動で外れる、quit の停止は置かない)、`serve_test.go` (止めきる・`stopCmd` の引数)、
  `live` / `ui` の各 1 本。守りを 1 つずつ外す変異 8 本で red を確かめた (`bin/mutate-verify`)
- 未対応のまま残した形: 画面の quit が止めきれなかったときの案内「もう一度止める: pro-con dispatcher --stop」を人が打つと、それは人の停止なので
  印が置かれる (次に開いた画面は起こさず「止めてある」と出る)
