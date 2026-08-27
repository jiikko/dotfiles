# 116 bug: issues の markdown レンダラが幅 1〜8 で溢れる (053 の同型が第 3 の経路に残存)

起票日: 2026-08-27 / 出典: lint-from-done 監査 / priority: low (latent。下記「今日の実害」参照)

## 事実

`src/glogx/issues/markdown_test.go:TestRenderBodyNeverExceedsWidth` は
**離散的な幅 `{20, 40, 60, 86, 120}` しか掃いていない**。幅 1〜120 を全数掃くと溢れる:

| width | 溢れた行数 | 最大幅 |
|---|---|---|
| 1 | 80 | 31 |
| 2 | 13 | 31 |
| 3 | 42 | 17 |
| 4 | 12 | 16 |
| 5 | 10 | 9 |
| 6〜8 | 各 4 | 9 |
| **20 / 40 / 60 / 86 / 120** | **0** | — |

最悪の行: `"pr… │ 用…"` (幅 8 指定で 9 桁)。**既存テストが掃いている幅では絶対に出ない**。
(2026-08-27 に本 repo で再現。監査エージェントの報告と数値が一致)

## なぜ起きるか

`issues/done/053-bug-glogx-issues-rowline-overflows-at-tiny-width.md` で issues viewer と
status viewer の同型を潰したとき、**本体はテストの穴 (掃き始めが 20)** だと結論している。
053 自身が「同型の穴が status viewer 側にもある可能性 (未確認)」と書き残していた。

`issues` パッケージには main の `clipToWidth` (053 で `width <= 0` を空返しへ変えた最終 clip) が
無く、**「組んだ後に必ず切る」構造を持っていない**のが原因。

## 今日の実害 (正直な見積もり)

**ユーザーには見えていない。** issues viewer を本文モードで開いて幅 1〜80 を全数掃く
end-to-end では溢れ 0 件で、main 側の下流 clip が吸収している。

したがってこれは **latent な契約違反 + テストの穴**であり、`RenderBody` を
「main の clip を通らない出口」(静的出力・別 viewer・JSON 化) へ繋いだ瞬間に露出する。

## 対応

1. `TestRenderBodyNeverExceedsWidth` の掃きを `for width := 1; width <= 120; width++` に広げる
   (`issues_view_test.go` / `status_view_test.go` は既にこの形なので、枠にそのまま載る)
2. **広げた時点で red が出る**ので実装側の対応とセットになる。契約の下限を決めること:
   幅 N 未満は畳む / `issues` 側にも最終 clip を置く、のどちらか
