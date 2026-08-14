# glogx: issues viewer / usage グランス / toast のフレームが、ベンチにも確保ゲートにも一度も載っていない

起票日: 2026-08-14
種別: perf
優先度: **P2** (048 と同型の退行を入れても、動く metric が 1 つも無い)

## 何が起きるか

`viewLines()` は**窓ごと差し替える**全画面ビューを 2 つ持つ (`tui.go` の `viewLines`、
2026-08-14 時点で 2827-2832 行):

```go
if m.statusOv.visible() { return m.finishViewerWindow(m.statusOv.lines(...), page) }
if m.issuesOv.visible() { return m.finishViewerWindow(m.issuesOv.lines(...), page) }
```

**status viewer 側にはゲートがある** (`BenchmarkStatusViewFrame` / `...2000` と
`TestFrameAllocBudget` の `status-40`)。**issues viewer 側には何も無い**。

実測 (grep で確認):

| 経路 | 時間ベンチ | 確保ゲート (回数/バイト) |
|---|---|---|
| 一覧フレーム | あり | あり |
| status viewer | あり | あり |
| **issues viewer** (`issuesOv.lines`) | **無し** | **無し** |
| **usage グランス** (`usageOv.boxLines`, `tui.go:2890`) | **無し** | **無し** |
| **toast** (`toast.boxLines`, `tui.go:2894`) | **無し** | **無し** |
| PR status ポップアップ / action モーダル / zoom / glide | 無し | 無し |

- ベンチは全部で 11 本 (`grep -h '^func Benchmark' src/glogx/*_test.go | wc -l`。うち CI が
  ゲートするのは 9 本) で、`TestFrameAllocBudget` は 5 ケース。**そのどれも
  `issuesOv.visible()` を真にしない**
- usage グランスは**意図的に視界外**になっている: `newBrowseModel` は
  `usageOv: usageOverlay{visible: true}` (`tui.go:359`) で起動直後にグランスを出すのに、
  フィクスチャは `m.usageOv = usageOverlay{}` で消している (`tui_bench_test.go:75`)。
  つまり**アプリ起動直後の実フレームは、どのゲートの外**

## なぜ重要か

048 で status viewer に見つけた退行 (「可視の窓の外まで毎フレーム整形する」= ファイル数に
比例するフレームコスト) は、**issues viewer に同じ形で入れても metric が 1 つも動かない**。
issues ドロワーは開閉アニメを持つので、発生するとしたら 30fps 側の経路になる。

051 (確保バイトの予算) の敵対的レビュー R1-2 の指摘。指摘そのものは grep で裏取り済み
(上表)。**issues viewer の実測確保量は未計測**なので、「今そこに退行がある」とは主張していない
— 主張は「**あっても気づけない**」という構成上の事実だけ。

## 対応方針 (案)

1. `benchIssuesBrowse(tb, n, w, h)` 相当のフィクスチャを足す (`issuesView` の rows を
   直接組む。`benchStatusBrowse` が `parseWorktreeStatus` 経由で組んでいるのと同じ流儀)
2. `BenchmarkIssuesViewFrame` を `bench_glogx.sh` の対象へ足し、`issues_view_frame` と
   `issues_view_frame_alloc_kb` を `bench_budgets.ci` に置く
3. `TestFrameAllocBudget` に `issues` ケースを足す (回数 + バイト。-race 実測 +3%)
4. usage グランスは**別ケース**にする (フィクスチャで `usageOv` を消さない版)。
   起動直後の実フレームなので代表性が高い

⚠️ zoom / glide / action モーダルはアニメ中の一過性フレームで、`AllocsPerRun` /
`b.Loop()` の定常測定と相性が悪い (毎 iteration 同じ状態を描くため実運用と乖離する)。
**後回しにするか、測るなら「1 フレームぶんの状態を固定して測る」形にする**こと。

## 未確認

- issues viewer のフレーム確保量 (40 件 / 2000 件でスケールするか。048 と同型の穴が
  既にあるかもしれない = 起票時点で未測定)
- metric を 2〜4 本増やしたときの Step Summary の可読性 (051 で 9 → 17 本になっている)

## 関連

- `issues/done/051-perf-glogx-bench-gates-time-only.md` (本 issue の出処。「残った限界」節)
- `issues/done/048-perf-glogx-status-displayindex-per-frame.md` (status viewer で潰した同型の退行)
- `issues/055-refactor-glogx-bench-fixtures-ascii-only.md` (フィクスチャの代表性。あちらは
  「同じ画面を何で測るか」、本 issue は「そもそも測っていない画面がある」)

## 対応記録 (2026-08-15)

方針 1〜4 を実装 (+ toast も holding 固定で追加):

- `benchIssuesBrowse` fixture 新設 (実ファイルを一度だけ scan して組む。View 中は読まない)
- `BenchmarkIssuesViewFrame` / `...2000` を bench_glogx.sh と bench_budgets.ci に配線
  (issues_view_frame 2 rel / issues_view_2000 3 rel / alloc は CI 実測待ちの +5% =
  34.7 / 36.6 KB)。CI 実測が出たら 065 と同様 +3% へ締める
- `TestFrameAllocBudget` に 3 ケース追加 (-race・-count=10 の実測から。全て 10/10 で安定):
  issues-40 209 回 33,849B → 上限 213 / 34,900、usage-glance 176 / 35,624B → 180 / 36,700、
  toast-holding 182 / 36,864B → 186 / 38,000
- usage グランスは fixture で消さない版 (`budgetUsageGlanceModel`) = 起動直後の実フレーム。
  toast は entering/leaving でなく holding を advance で作って固定 (方針の⚠️どおり
  一過性フレームは測らない)
- 変異検証: issuesView.lines に 4KB の確保水増しを入れると issues-40 のバイト側が
  37,944B > 34,900B で red (回数は 210 ≤ 213 で素通り = バイトゲートの存在意義も同時に実証)

「未確認」の解消:

- issues viewer はスケールしない: 40 件 22.4µs/33,826B ↔ 2000 件 23.6µs/35,657B
  (+5%/+2 allocs)。048 同型の穴は**無かった** (可視窓だけ整形できている)
- metric は 19 → 23 本。Step Summary の可読性は現状観察 (問題になったら別途)

zoom / glide / action モーダルは方針の⚠️どおり対象外のまま (一過性フレーム)。
