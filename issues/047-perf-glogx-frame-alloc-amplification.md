# glogx: 1 フレームの確保バイトが出力の 5〜6.5 倍 (枠組み立てが中間文字列を捨て続けている)

起票日: 2026-08-14
種別: perf (実測駆動)
優先度: **P2** (フレームごとの GC 圧。046 の次)

## 観測した事実

1 フレームの**出力バイト数**と、そのフレームで**確保したバイト数** (`MemStats.TotalAlloc` の差分 /
200 フレーム) の比:

| 画面 | 出力 | 確保/frame | 増幅 | allocs/frame |
|---|---|---|---|---|
| 一覧 | 8028 B | 40219 B | **5.01x** | 184 |
| status viewer | 8570 B | 55469 B | **6.47x** | 467 |
| diff popup | 9581 B | 58512 B | **6.11x** | 260 |

確保の内訳 (`BenchmarkViewSteady` の alloc_space。ベンチ別に採り直したもの):

| 関数 | flat | 備考 |
|---|---|---|
| `buildPanelBoxImpl` | 24.1% | 最外周フレーム |
| `strings.(*Builder).grow` | 24.1% | 呼び出し元は 100% `Builder.Grow` |
| `wrapWindowFrame` | 20.5% | cum 50.4% |
| `scrollbarColumn` | 13.1% | |
| `browseModel.viewLines` | 5.5% (cum 99.3%) | |

`CursorMoveView` でもほぼ同じ内訳 (`wrapWindowFrame` 22.6% / `buildPanelBoxImpl` 22.6% /
`grow` 21.6% / `scrollbarColumn` 14.2%) なので、**画面によらずフレーム組み立てが本体**。

CPU 側にも波及している: `ViewSteady` の CPU サンプル 4.0s のうち `runtime.kevent` が 1.68s
(42%) を占め、その経路は `runtime.gcStart.func4 → startTheWorldWithSema → netpoll` =
**GC の start-the-world**。つまり 40KB/frame の churn が GC の回転数として跳ね返っている
(`num_gc` は soak の scroll 連打 30 秒で 10 → 31 に増えた)。

## 何を疑うか (まだ原因は確定していない)

`Builder.grow` の呼び出し元が 100% `Builder.Grow` である = **サイズ指定は既にしてある**。
したがって「Grow を足す」類の修正ではない。疑うのは次の 3 つで、**着手前にどれが効くかを
実測で切り分ける**こと:

1. `Grow` の見積もりが過大 (必要量の数倍を確保している)
2. 同じ内容を段階ごとに作り直している (行 → 窓 → 枠 → 最終結合で中間文字列を毎段捨てる)
3. `scrollbarColumn` が毎フレーム列全体を作っている (可視行数ぶんで足りるのに)

## 未確認 (推測として明記)

- 上記 3 つのどれが支配的かは**まだ切り分けていない**。増幅率と内訳が分かっているだけ
- 「5x が過大」という判断自体は相対評価。TUI の段階組み立てである程度の増幅は避けられない。
  **どこまで下げられるかは実測で決める** (目標値を先に決めて追わない)
- 046 (dispWidth) を先に入れると CPU の内訳が変わるため、**本 issue の再測定は 046 の後に行う**

## 検証条件

- `testing.AllocsPerRun` で allocs/frame の減少を assert するテストを足す
  (ns/op だけだとノイズに埋もれる)
- 出力の**バイト一致**を旧実装と比較するテスト (見た目を変えないことの機械的な保証)。
  枠・スクロールバー・overlay 合成は目視では差分に気づきにくい
- 変異検証: 確保を減らす変更で出力が 1 バイトでも変わったら red になること

## 測定ログ

- `tmp/glogx-perf/perbench/{ViewSteady,CursorMoveView,StatusViewFrame,ViewWithDiff}_mem.prof`
- 増幅率: `tmp/glogx-perf/frame_amplification.txt`
