# 712 (perf): treefiler の配置・描画・アニメが全ノードに比例する (画面の近くだけを扱う)

起票日: 2026-10-11

## 概要

issue 711 で、巨大なフォルダ (5 万件) の 1 フレームの確保を 18 MB → 2 MB に減らした後も、時間はノードの数に比例して残る:
View 約 7.6 ms、`j` 1 回 (動きが止まるまで 40 フレーム) 約 580 ms (1 フレーム約 14.5 ms。60fps の予算 16 ms ぎりぎり)。
残りの CPU は、毎フレーム・毎 tick に全ノードを舐める処理の map の読み書き:

- `layout()` (`filer/layout.go`): 全ノードの目標位置を `pos` の map に入れる (動きの最中は `Advance` の `step` が毎 tick、`draw` も毎フレーム呼ぶ)
- `draw()` (`filer/model.go`): 全アニメの「今の位置」を `now` の map に入れる
- `sync()` / `step()` (`filer/model.go`): 全アニメの tween を毎 tick 進める・幅を付け直す
- `lines()` (`filer/layout.go`): 全アニメ (`m.order`) を舐め、開いたフォルダごとに子の座標を集める (縦棒の端を決めるため、画面の外の子も見る)
- `step()` の後半: 全アニメを舐めてフォルダの走査を頼む (`m.walker.request`)
- `drawNames()` (`filer/model.go`): 全アニメを 2 周舐める (画面の縦範囲の外は飛ばすが、舐めること自体が全ノードぶん)
- `sync()`: 生まれたノードを深さとパスで並べる (`sortByDepth`)。`snapAll()`: キー入力のたびに全アニメを舐める
- 動きの最中は 1 フレームで `layout()` が 2 回走る (`step` と `draw`)。配置をキャッシュする案はこの 2 回目も消せる

画面に出るのは高々数十行なので、画面 (カメラ) の近くのノードだけを扱えれば、時間はノードの数に依らなくなる。

## 対応方針 (案。未設計)

- 配置の結果をキャッシュし、変わったとき (カーソル・開閉・設定・木の読み直し) だけ作り直す。または、巨大なブロック (1 つの親の子が
  画面の行数を大きく超えるもの) は、画面の縦範囲 ± 余白の子だけに位置とアニメを持たせる
- 🚨 仮想化は意味に触れる: 画面の外から入ってくる項目の湧き方 (spec §4.2 の「いちばん近いアニメ中の祖先の右端から湧く」)・
  消える項目の縮み方・縦棒の端 (画面の外の子まで伸びる)・検索の `column` (画面の外の項目も対象) が、全ノードの位置を前提にしている。
  設計の前に、これらがどの範囲のノードを読むかを列挙する
- 計測はベンチ `filer/perf_bench_test.go` (`BenchmarkViewHugeFolder` / `BenchmarkLayoutHugeFolder` / `BenchmarkMoveHugeFolder`) で
  前後を比べる

## 受け入れ条件 (案)

- [ ] 5 万件のフォルダで、`BenchmarkMoveHugeFolder` の 1 フレームあたりの時間がノードの数に比例しない (5 千件と 5 万件で比べる)
- [ ] 小さなフォルダの見た目・動き (湧き方・消え方・縦棒) が変わらない (既存のテストと、描画の比較)

## 関連ファイル

- `src/treefiler/filer/layout.go` (`layout` / `lines`) / `src/treefiler/filer/model.go` (`draw` / `step` / `sync`) / `src/treefiler/filer/search.go` (`column`)
- `src/treefiler/filer/perf_bench_test.go`
- issue 711 (確保の削減と実測値) / issue 693 (巨大なフォルダの重さ。走査・ライブ更新・格子の切り詰め)

## 進捗

- 2026-10-11: 起票 (711 の残り)
