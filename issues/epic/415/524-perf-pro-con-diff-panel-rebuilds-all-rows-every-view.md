# 524 (perf): 差分の板は描くたびに全行 (最大 5000 行) を組み直す (1 描画 5.6ms・3.6MB)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md) / 見つけた監査: [513](513-research-pro-con-performance-audit-2026-09-26.md)

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
