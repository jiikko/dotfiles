# 693 (perf): treefiler が巨大なフォルダで重い (裏の走査・ライブ更新・描画) と、確保の予算が treefiler に無い

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

- [x] 1 は 691 の walker の作り直しで解消 (2026-10-09。結果を差分で渡し、取り出すときに数え終わったものを飛ばす)。残りは 693 の作業で
- [x] 2 ライブ更新: 5000 項目を超えるフォルダは、フォルダ自身の mtime が変わらない限り 5 回に 1 回 (= 5 秒に 1 回) だけ読む (`filer/watch.go` の `dirMeta.next`)。
  mtime が動く変更 (追加・削除・改名) は次の周で拾い、動かない変更 (中のファイルの書き換え) は最大 5 秒遅れる。1 回の `readSig` (5 万件で約 154ms) は
  変わらず、回数が 1/5 になる。`reload` の 184ms は手を入れていない (変化があったときだけ)
- [x] 3 描画: `lines(now, ylo, yhi)` で罫線の格子を画面の縦範囲 ±1 行に切り詰める (ブロックごと外なら飛ばす・縦線を clip・範囲外の子の横線を飛ばす)。
  5 万件のフォルダで View 25.09 → 13.77 ms/フレーム (go test の bench、手元の M 系 Mac)
- [x] 4 確保の予算: `filer/alloc_budget_test.go` の `TestFrameAllocBudget` (上限 335、実測 333。go1.25.0 と go1.26 で同じ)。3 つのフォルダを全部開いた
  fixture にした (開いたフォルダの数だけ効く退行を、余裕の数回で見逃さないため。1 つだけ開いた fixture では f612c696 の退行を戻しても 207 / 208 で緑だった)。
  `-race` では確保が増えるので件数を見ず、Makefile の test で `-race` なしにもう 1 回走らせる
- [x] 5 diff の取り消し: diff の job を context で取り消せるようにし、diff の切り替え・タイルを閉じる・ソースの差し替えで `stopDiff` を呼ぶ
- 変異 (bin/mutate-verify、7 本すべて red): 子の横線の clip を外す / 縦線の clip を外す / clip を ±0 行にする / ブロックごと飛ばす範囲を狭める
  → `TestLinesClippedToViewport` (切り詰めた格子の画面内のセルが全体の格子と一致するかを、窓を 3 行ずつずらして見る)、`next()` を常に読む → `TestBigDirPollThrottle`、
  toggleDiff の `stopDiff` を外す → `TestDiffToggleCancelsInflightFetch`、芽の判定を `len(m.kids(n)) == 0` に戻す → `TestFrameAllocBudget`
- 敵対レビュー (sonnet、1 周): P1 / P2 なし。採った P3: 画面内のセルの一致を見るテストが無い → 上の差分の検査を足した。記録のみの P3:
  (a) `metas` は閉じたフォルダの分を掃除しない (loop の寿命のあいだ残る。再展開で mtime が変われば読み直すので実害は小さい)
  (b) mtime の精度が粗い FS (SMB・exFAT) では同じ窓の中の変更が最大 5 秒遅れる (c) Model の終了時に diff を取り消さない
  (git は `GitOpTimeout` まで残る。glogx から閉じたときの経路は未確認) (d) 予算の余裕 2 回は Go の版の差で flaky になりうる (2 版で同じ値なのは確認した)
- `make test` / `make lint` (src/treefiler) rc=0
