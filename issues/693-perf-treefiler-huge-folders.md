# 693 (perf): treefiler が巨大なフォルダで重い (裏の走査・ライブ更新・描画) と、確保の予算が treefiler に無い

> 🚨 **担当中: Claude Code (dotfiles-53。監査の issue を順に直すセッション)**（2026-10-09〜）

起票日: 2026-10-09

## 概要

697 の監査の実測 (5 万件・直下に数千のフォルダ)。いずれも黙って遅くなる。

## 詳細

1. **裏の走査が子フォルダの数の 2 乗で遅い** — `filer/walk.go` の `walker.loop` が結果を 1 件書くたびに map 全体を複製する
   (`next := make(map…, len(w.results)+len(found))`)。`request` は「親が complete なら飛ばす」が、全部を先に queue へ積むので効かない (pop 時に
   見直さない)。直下に N 個のフォルダで Busy が落ちるまで: N=1000 80ms / 2000 194ms / 4000 565ms / 8000 1.87s。その間 80ms の tick と View が回り続ける。
   直し方: pop 時に complete を見直す・1 件ごとの全件の複製をやめる
2. **ライブ更新が開いている巨大なフォルダを毎秒全部 lstat する** — `filer/watch.go` の `readSig`。5 万件で 158〜177ms/回 (裏の CPU 16% 前後)。
   変化があると UI の `applyChange` の `reload` が約 184ms 止める。直し方: フォルダ自身の mtime を先に見る・件数が大きいフォルダは周期を落とす
3. **画面の外の線まで毎フレーム格子にする** — `filer/layout.go` の `lines()` (`drawLines` の 55%)。5 万件・200×50 で View 13〜23ms、Advance 7ms
   (アニメ中は約 30fps)。直し方: カメラの矩形で切る
4. **treefiler に確保の予算のテストが無い** — 描画の確保の退行は glogx の `TestFrameAllocBudget` (filer の行) でしか赤くならない。56c93c8f では
   src/treefiler の CI (run 37863501923) が success、src/glogx (run 37863501927) が `filer: 1 フレームの確保が 164 回 (上限 158)` で failure だった。
   直し方: `src/treefiler/filer` にも AllocsPerRun の予算を置く (glogx の値は組み込み後の上限として残す)。
   きっかけの 164 / 158 の退行そのものは f612c696 (芽の判定で子のスライスを作らない) で直した。予算が treefiler に無いことは残っている
5. `d` を連打すると git diff を重ねて起こす (前の job を取り消さない。各々 `GitOpTimeout` で有界。起動数は未実測)

## 関連

- issue 662 の「記録のみ」の「巨大なフォルダを毎秒 ReadDir する重さ (未計測)」は 2 で実測した
- 監査の記録: 697

## 進捗

- [ ] 未着手
