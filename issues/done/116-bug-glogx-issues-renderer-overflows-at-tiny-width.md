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

---

## 対応 (2026-08-27)

**`RenderBody` の出口 1 箇所に最終 clip を置いた** (`issues/render.go:clipToWidth`)。
「幅 N 未満は畳む」という下限を決める案は採らなかった — 下限を跨ぐ入力ごとに畳み方を決める
ことになり、一律に切る方が契約が単純。glogx 本体も同名の関数で同じことをしており、
**今日この溢れが表に出ていなかったのは、その下流 clip が吸収していたから**。

コストは問題にならない: `Body.Lines` が (width, colored) でキャッシュしているので
`RenderBody` は幅か色が変わったときだけ走る (毎フレームではない)。ANSI 無しの行の fast-path も残した。

### テストの掃きを 1..120 の全数に広げた

以前は `{20,40,60,86,120}` の離散点しか見ておらず、20 未満を一度も通していなかった。
**「離散点で緑」は「その点では壊せなかった」でしかない。**

### 変異検証 5/5 red

clip を外す (修正前へ) / 超過時に切らない / fast-path の判定を緩める / 超過判定を 2 桁甘くする /
幅 0 以下でそのまま返す。

### 追加で確かめたこと

- **切り詰めても色が開いたまま終わらない** (次の行へ漏れない)。実測して pin した
  (`TestRenderBodyClipDoesNotLeakColor`)。`paintLine` がスパンごとに reset を出しており、
  `ansi.Truncate` がそれを持ち越すため
- **幅 0 / 負でも空行を返す** (`TestRenderBodyAtZeroOrNegativeWidth`)。呼び出し側の
  「width - 固定列」が極小幅で 0 や負になる形は issue 053 が本体側で踏んでいる

### 敵対的レビューは回していない (判断)

出口 1 箇所への純粋な文字列処理の追加で、状態も並行も外部 I/O も動かない。
懸念だった「色の漏れ」は自分で実測して pin した。
