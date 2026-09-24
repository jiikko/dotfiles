# 421 (test): schedkeys の TestRenderCostStaysLinear が負荷の下で落ちる

起票日: 2026-09-24

## 概要

`src/schedkeys/render_test.go` の `TestRenderCostStaysLinear` は、100 文字と 10000 文字の入力で
`View()` を 20 回ずつ描き、1 フレームあたりの壁時計の比が 30 倍を超えたら落とす。
2026-09-24 に `make test` (並列 + `-race`) で 1 回落ちた。単独では 3/3 通る。

## 詳細

- 守りたいのは 7695eeae (fix(schedkeys): 敵対的レビューが見つけた描画の破綻 …) で直した二乗の描画コスト
  (旧 viewport がスクロールのたびに `startRune` で先頭から数え直していた。10000 文字で 797ms)
- 100 文字の 1 フレームはマイクロ秒で、他のテストのプロセスに 1 回割り込まれるだけで比が崩れる
  (測っているのはマシンの空き具合。`~/.claude/rules/avoid-wall-clock-assertions.md`)

## 対応方針

- 時間以外の観測 (回数・割り当て) で判定できるかを先に見た。**できない**: 旧 viewport の二乗は
  `startRune` が先頭から rune を数え直す部分で、ASCII の入力では割り当てが 0 (`string(rune)` は非 escape)。
  呼び出し回数を数えるには production の viewport に seam を足す必要があり、テストの都合で本体を歪めることになる
- そこで比は残し、**負荷に強い測り方**に変えた: 1000 文字と 16000 文字を交互に 7 回測り、**最小値どうし**を比べる
  (負荷は時間を足すだけで引かないので、最小値は割り込みを落とす)。小さい側を 100 → 1000 文字にして、
  マイクロ秒の測定にならないようにした
- 閾値は 16 倍の入力に対して 64 倍 (線形なら ≤16、二乗なら 256 の間)
- 絶対値の上限 (10000 文字で 200ms) は外した。壁時計の絶対値で、runner の速度をそのまま測る形だったため。
  比で止まらない「線形のまま定数倍だけ遅くなる」退行は、これで検出しなくなる (受容: 検出可能性は未確認。
  trigger は popup の入力が重いという報告)

## 関連ファイル

- `src/schedkeys/render_test.go` の `TestRenderCostStaysLinear`
- `src/schedkeys/editor.go` の `viewport`

## 進捗

- [x] 起票
- [x] 最小値どうしの比に変更 (test(schedkeys): 描画コストの線形性を最小値どうしの比で判定する)
  - 実測 (2026-09-24, M 系 14 コア): 今の実装で 11.0〜11.2 倍 (`-race` でも同じ)、旧 viewport で 287〜290 倍 (16000 文字 1 フレーム 2.1 秒)
  - `bin/mutate-verify` で旧 viewport (startRune で先頭から数え直す) に戻す変異を当て、`FAIL: TestRenderCostStaysLinear` で red
  - 未検証: `make test` の並列負荷の下で再び落ちないことは、今回の 1 回の実行でしか見ていない
