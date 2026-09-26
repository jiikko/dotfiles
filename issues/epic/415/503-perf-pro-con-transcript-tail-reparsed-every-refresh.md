# 503 (perf): 作業中の PG の transcript の末尾 512KB を、画面が 3 秒ごとに読み直して解析する (1 回 5.7ms / 2.9MB)

起票日: 2026-09-26

## 概要

画面の裏の読み直し (`live/live.go` の `refresh`) は、pro-con が起動した session のカードごとに `b.transcript` を呼ぶ。キャッシュは
「大きさと更新時刻が同じなら読み直さない」だが、作業中の PG の transcript は 3 秒のあいだにほぼ必ず伸びるので、毎回 `ReadTail`
(末尾 `tailBytes` = 512KB を読んで行ごとに解析) が走る。使っているのは末尾の出力 3 行 (`cards[i].Log = tail(t.Outputs, 3)`) だけ。
画面の描画と同じプロセスで確保するので、GC の負荷も描画と分け合う (494)。

## 実測 (2026-09-26)

| 何を | 値 |
|---|---|
| `ReadTail` 1 回 (手元の最近の transcript のうち最大の 8.5 MB のファイル、Go の benchmark) | 5.7 ms / 2.9 MB / 7,370 allocs |
| 作業中の PG が N 体・画面 1 つのとき | 3 秒ごとに N × (5.7 ms / 2.9 MB)。4 体なら約 23 ms と 11.6 MB を 3 秒ごと (見積もり。N 体での実測はしていない) |

## dispatcher 側 (反証レビューで判明)

dispatcher も同じ transcript を読んでいて、画面より条件が悪い。`dispatchercmd.go` の `Transcript` は **毎回 `live.FindTranscript` (glob) から
`live.ReadTail` までキャッシュなしで行い**、1 回の tick で同じ session について `dispatcher.go` の `watch` と 643 行付近・`role.go` の `roleTurn`・
`doing.go` の `transcriptDoings`・`btw.go` の `pgOutputs` から別々に呼ばれうる (tick は 3 秒ごとと、依頼を置くたびの wake)。

## 対応方針 (案)

- 前回読んだ位置 (offset) から後ろだけを読んで、`Transcript` を積み上げる (行の途中で切れた分は次回へ持ち越す)。ファイルが縮んだ・
  別のファイルに変わったら頭から読み直す
- 🚨 `ReadTail` が末尾 512KB を読む理由 (ai-title が約 46KB に 1 回しか書かれないので、その数倍を読む。`live/transcript.go` の `tailBytes`) を
  差分読みでも満たすこと (最初の 1 回は今と同じ量を読む)
- 効果は「PG 1 体あたり 3 秒ごとの確保量」で before / after を測る

## 関連ファイル

- `src/pro-con/live/live.go` — `refresh` / `transcript`
- `src/pro-con/live/transcript.go` — `ReadTail` / `tailBytes`

## 進捗

- [x] 実測 (上の表)
- [x] 反証レビュー (読み取り専用のサブエージェント 1 体): 画面側の主張は反証されず。dispatcher 側も読んでいる (上の節) を追記
- [ ] 差分読みにする
