# 711 (perf): treefiler が巨大なフォルダで 1 フレームに 12 ms・18 MB を使う (配置・描画の map とスライスの作り直し)

起票日: 2026-10-11

## 概要

ユーザー要望 (2026-10-11): treefiler の性能を改善できることが無いか実装を調べ、課題があれば直す。あわせて「アセンブラを使う余地は無いか」。

5 万件のファイル (と dotfile 1 件) を置いたフォルダを開き、カーソルを中へ入れた状態で測ると、1 フレームの View に約 12 ms・18 MB、
`j` 1 回で動きが止まるまで (40 フレーム) に約 890 ms・1.08 GB を使っていた (1 フレーム約 22 ms で、60fps の予算 16 ms を超える)。

## 原因 (プロファイル。`go test -bench HugeFolder -cpuprofile / -memprofile`)

毎フレーム (動きの最中は `Advance` の `step` でも毎 tick) 全ノードに対して走る処理の作り直し:

- `layout()` の目標位置の map を毎回ゼロから作って伸ばしていた (確保の最大)。列ごとのスライスも append で伸ばしていた
- `kids()` は隠しファイルを除くたびにスライスを作る。`layout()` の中で同じ親について 4 回以上 (ループの中の `blen` から何度も) 呼んでいた
- `draw()` の「今の位置」の map と、`lines()` の子の座標のスライスも毎フレーム作り直していた
- 名前の表示幅 (`nameCells` → `termwidth.Of`、切り詰め) を全ノードぶん毎 tick 数え直していた

## 対応 (commit「perf(treefiler): 巨大なフォルダで配置・描画の map とスライスを使い回し、名前の幅を覚える」)

- node が表示の名前と幅を覚える (`tree.go` の `label` / `labelCells`)。覚えたときの列の最大幅 (`labelMax`。設定の MaxName) と組で持ち、変わったら数え直す
- `layout()` の結果の map を使い回す (`reusedMap`。次の layout の呼び出しで中身が変わるので、呼び出し側は持ち越さない。`layout` の doc)。
  1 回の配置の中で `kids()` を親ごとに 1 回だけ作る。列のスライスを事前確保する
- `draw()` の「今の位置」の map と `lines()` の子の座標のスライスも使い回す
- `reusedMap` は要る数が容量の 1/4 を切ったら作り直す (Go の map は clear しても容量が残り、反復は容量に比例する。巨大なフォルダの後に
  小さなフォルダへ移ったとき、毎 tick 空の大きな map を舐めないため)

## 実測 (2026-10-11、Apple M3 Max、go1.26.0、`go test -run '^$' -bench HugeFolder -benchtime 10x -count 3 ./filer/`)

| ベンチ (`filer/perf_bench_test.go`) | 前 | 後 |
|---|---|---|
| View (止まった画面 1 フレーム) | 11.2〜12.5 ms・18.1 MB・828 allocs | 7.6〜8.4 ms・2.0 MB・219 allocs |
| layout 1 回 | 3.4〜3.8 ms・9.0 MB・318 allocs | 1.4 ms・0.8 MB・11 allocs |
| `j` 1 回 (40 フレーム) | 885〜894 ms・1.08 GB・45720 allocs | 563〜599 ms・113 MB・9152 allocs |

途中の段の値 (同じ条件): 名前の幅を覚えた後 View 11.3 ms・layout 3.1 ms・`j` 800 ms / map の事前確保と kids の覚えの後 View 10.1 ms・
layout 1.9 ms・5.2 MB・`j` 680 ms / 使い回しの後が上表の「後」。

確保の回数の予算: treefiler の `TestFrameAllocBudget` が 333 → 274 回 (上限を 335 → 276 に締めた)。

## アセンブラの余地 (結論: 今は無い)

- 残りの CPU は、全ノード (5 万件) の map の読み書きという「ノードの数に比例する処理」。アセンブラが効くのはバイト列を舐めるループ
  (幅の計算・SGR の組み立て) だが、それらは名前の幅を覚える前でもプロファイルの約 3% (`termwidth.fastDispWidth` 2.85% cum) だった
- Go の標準ライブラリは既に SIMD を使う (`internal/bytealg`)。arm64 と amd64 の両方を保守する費用に見合う箇所が無い
- 効くのは処理の数を減らす方 (画面の近くのノードだけを扱う仮想化)。issue 712 に切り出した

## 残り

- 時間はまだノードの数に比例する (View 7.6 ms、`j` 1 回 580 ms = 1 フレーム約 14.5 ms で予算ぎりぎり) → issue 712
- 名前の幅の覚えそのものを外しても、テストと予算は緑のまま (`termwidth.Of` は確保しないので予算に出ない)。守るのはベンチだけ。
  CPU の削減の一部 (layout 3.6 → 3.1 ms) なので、ゲートは置かずに受け入れる (退行はベンチで見える)
- bubbletea の版を上げる余地の調査 → issue 713 (結論: 今は上げない)

## 関連: glogx の `TestFrameAllocBudget` の filer の行の揺れ (同じ作業で直した)

2745215c の CI で `filer: 1 フレームの確保が 164 回 (上限 158)` が 1 回出た (同じコードの次の commit の CI は success)。原因は fixture
(`budgetFilerModel`) が毎回「実時計 + 1 秒」を `Advance` に渡していたこと: `Advance` は 1 回の経過を 0.05 秒で頭打ちにするので、2 回目
以降は呼び出しの間の実時間しか進まず、動きの途中の画面を測っていた (どこまで進むかはマシンの速さで変わる)。固定の時計を 50 ms ずつ進め、
動きと裏の処理が止まるまで回す形に直した。止まった画面は 220 回・50799 B (-race) で 3 回とも同じ値 (前の 156 回より多いのは、
動きの途中では薄くて描かれないノード (alpha が小さい) があったため。止まった画面は全ノードと線を描く)。上限を 158 / 46200 → 222 / 51500 にした。
issue 693 の「56c93c8f の CI で glogx が 164 / 158 で failure」も同じ値で、同じ揺れが混ざっていた可能性がある (未確認)。

## 進捗

- [x] 計測の道具: `filer/perf_bench_test.go` (前提として巨大なフォルダの中を描いていることを確かめる)。`settleBackground` / `waitFor` を `testing.TB` に広げた
- [x] 名前の幅を覚える / layout の map の使い回し・kids の覚え・列の事前確保 / draw・lines の使い回し
- [x] glogx の filer の予算の fixture を固定の時計にした
- 変異 (bin/mutate-verify。どれも red): layout の map を毎回作る形に戻す → treefiler の `TestFrameAllocBudget` / `reusedMap` の作り直しの条件を外す →
  `TestReusedMapShrinksAfterBigFolder` / 名前の覚えが labelMax の変化で捨てる条件を外す → `TestLabelRetruncatesWhenMaxNameChanges`
- 敵対レビュー (opus 1 本。壊す・素通り・回帰): 挙動を壊す手順は無し (変更の前後で同じ操作の全フレームを比べて差 0 行。2500 件の巨大なフォルダ・
  MaxName 12 / 60 / 28・`.`・Columns・巨大なフォルダから小さなフォルダへの移動を含む)。採った指摘: MaxName を変えたときの切り詰め直しを
  守るテストが無い (P2) → `TestLabelRetruncatesWhenMaxNameChanges` / 予算の注記の版の書き方 (P3)。受け入れた指摘: 名前の覚えそのものは
  ベンチだけが守る (上の「残り」)
- 反証レビュー (711 / 712 / 713 の本文。sonnet、read-only): 数値・シンボル・参照の誤りは無し。採った指摘: 712 の「全ノードに比例する処理」の漏れ
  (step の後半・drawNames の 2 周・snapAll・sync の並べ替え・1 フレームに layout 2 回) / `tree.go` の注記が実測より強い / glogx の予算が 156 → 220 に
  上がった理由。`make -C src/treefiler lint test`・`make -C src/glogx lint test`・root の `make test` rc=0 (commit の前に回した)
