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

(調査後に書く)

## 関連ファイル

- `src/schedkeys/render_test.go` の `TestRenderCostStaysLinear`
- `src/schedkeys/editor.go` の `viewport`

## 進捗

- [ ] 起票
