# 529 (perf): 差分の板は描くたびに全行 (最大 5000 行) を組み直す (1 描画 5.6ms・3.6MB)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md) / 見つけた監査: [513](done/513-research-pro-con-performance-audit-2026-09-26.md)

## 概要

差分の板 (`ui/diffview.go`) の `overlayDiff` は、描く (View) たびに `diffRows()` で畳んでいないファイルの全行を組み直す。
出すのは窓の数十行だけなのに、全行に `ansi.Strip` と文字列の結合をかけ、ファイルごとに見出しを `fmt.Sprintf` で作る。
本文は `card.DiffMaxLines` (5000 行) で切ってあるので上限はあるが、その上限で重い。

## 実測 (2026-09-26、module の一時コピーで。Apple M3 Max)

本文は dotfiles の本物の `git diff` (82 ファイル) を 5000 行で切ったもの。画面は本物の cards.json の写し (54 枚)・200x55。

| 何を | 1 回 |
|---|---:|
| 板を閉じた画面の View | 0.60 ms / 329 KB / 2,915 allocs |
| **板を開いた画面の View** | **5.57 ms / 3.59 MB / 25,042 allocs** |

- 板を開いている間は、作業中の PG が居ればスピナーで毎秒 10 回描く (`ui/spinner.go` の `spinInterval`) → 1 コアの約 5.6%・毎秒約 36MB の確保
- スクロールの滑り (`listnav.Pager`) の間は 33ms ごとのコマ (`ui/motion.go` の `frameInterval`) でも描く → 約 17%。
  494 の「5ms を超えると GC の跳ねと合わせて 33ms の枠に近づく」の範囲に入る
- ほかに `diffJumpTo` とキー処理 (`handleDiffKey`) も `diffRows()` を呼ぶ

## 対応方針 (案)

- 行の組み立て (`ansi.Strip` での見出しの判定・字下げ) は本文を読んだとき (`onDiffLoaded`) に 1 回だけにし、描くたびには畳みと選択の反転だけを反映する
  (見出しの行は選んでいるファイル `cur` で変わるので、そこだけ描くときに作る)
- または、畳み・選択が変わったときだけ `diffRows()` を作り直して持つ
- 効果はこの bench (5000 行の板を開いた View) の前後で測る

## 関連ファイル

- `src/pro-con/ui/diffview.go` — `diffRows` / `overlayDiff` / `diffJumpTo` / `onDiffLoaded`
- `src/pro-con/card/progress.go` — `DiffMaxLines`

## 対応 (2026-09-27、C-096)

方針の 1 つ目の形で直した (`ui/diffview.go`)。

- 本文の行の組み立て (`ansi.Strip` での見出しの行の判定・字下げ) は、読むときの裏の tea.Cmd (`diffBody`) で 1 回だけにした
- 全行を並べた列は持たない。各ファイルの見出しの行の位置 (`starts`) と行数 (`total`) だけを持ち、作り直すのは畳みが変わったとき (`relayout`。O(ファイル数)) だけ
- 描くときは窓の行だけを位置から引く (`rowText` / `fileAt`)。見出しの行は選んでいるファイルと畳みで変わるので、描くときに作る
- キー処理 (`handleDiffKey` / `diffJumpTo` / `followScroll`) も全行を組み立てない

検査 (`ui/diffview_perf_test.go` / `ui/diffview_rows_test.go`):

- `TestDiffBoardViewAllocDoesNotGrowWithLines`: 本文を 500 行から 5000 行にしても 1 描画の確保量が 1.3 倍を超えない。直す前のコードでは 4.38 倍で赤
- `TestDiffBoardRowsMatchFoldsAndCut`: 畳みの全組み合わせで、全行を並べた列と行の文字・ファイルが一致する (最後のファイルを畳んだときの切った知らせの行を含む)。変異 2 本 (畳みの判定を外す / 行の位置をずらす) で赤
- `BenchmarkDiffBoardView`: 下の実測

### 実測 (before / after)

`go test -run '^$' -bench BenchmarkDiffBoardView -benchmem -count 10 ./ui/` を benchstat で比べた (Apple M3 Max)。
本文は作った 82 ファイル・5000 行の差分 (色付けを通る形)、画面は 200x50。起票時の実測 (本物の差分と cards.json の写し) とは本文も画面も違うので、絶対値は比べられない。

| 何を | before | after | |
|---|---:|---:|---:|
| 板を開いた画面の View | 2.64 ms / 2.01 MB / 16,984 allocs | 0.48 ms / 345 KB / 1,453 allocs | -82% / -83% / -91% |
| 板を閉じた画面の View (参考) | 0.39 ms / 334 KB / 1,386 allocs | 0.40 ms / 334 KB / 1,386 allocs | 変えていない経路 (時間の +2.8% は揺れの範囲) |

- 板を開いた画面は、閉じた画面より 0.08 ms / 11 KB 重いだけになった (窓の行の切り詰めの分)
- スピナーで毎秒 10 回描いても約 0.5% のコア、スクロールの滑り (33ms ごと) でも約 1.5%
