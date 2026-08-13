# glogx: CI の bench 予算が時間だけを見ていて、確保 (allocs/B) の退行を止められない

起票日: 2026-08-14
種別: perf
優先度: **P2** (046-048 の成果を守る仕組みが無い)

## 何が起きるか

`tests/glogx/bench_glogx.sh` は `go test -bench` の **ns/op だけ**を
`metric=<name> ms=<value>` へ変換し、`tests/check_bench_budgets.sh` が
`tests/glogx/bench_budgets.ci` の上限と比べる。`-benchmem` は取っておらず、
**B/op と allocs/op は CI のどこにも出てこない**。

046-048 で分かったのは「フレームのコストの本体は確保だった」ということ:

| 修正 | 時間の変化 | 確保の変化 |
|---|---|---|
| 047 (フレームの 4 重コピー) | −10〜14% | **回数 −43/frame・バイト −23%** |
| 048 (displayIndex の memo) | −34% (2000 件) | **バイト −53%** |

つまり **今の CI は、確保が 2 倍になっても時間が予算内なら通してしまう**。
フレームの確保は 12.5〜30fps で回るので GC の回転数に直結する
(047 の実測で `runtime.kevent` の 42% が `gcStart` 由来だった)。

## 現状の代替手段とその限界

047/048 で `TestFrameAllocBudget` / `TestStatusFrameAllocDoesNotScaleWithFileCount` を
足したので**確保の回数**にはゲートがある。ただし:

- `AllocsPerRun` は **回数**しか見ない。バイト数の退行 (1 回の確保が大きくなる形) は素通り。
  実測: 046 で allocs は完全一致のまま B/op が +0.8〜1.0% 増えた (機構は未特定) — この形は捕まらない
- 上限値は darwin/arm64 の実測から採っている。**CI (Linux) の -race の水増し量は未確認**
  (2026-08-14 の push では通ったが、余裕は list で 3)
- ベンチ側 (`bench_glogx.sh`) と test 側 (`*_test.go`) で確保のゲートが二重管理になっている

## 追記 (2026-08-14): `AllocsPerRun` は「回数」しか見ない — 047 のガードにも同じ穴

048 の敵対的レビュー R1/R2 が実証した罠。`testing.AllocsPerRun` は確保の**回数**を数えるので、
**「1 回の確保が大きくなる」形の退行を捕まえられない**。

- 048 の当初のガードは回数ベースで、**メモ化を丸ごと revert しても PASS** した
  (40 件と 2000 件の回数の差は最適化の前後どちらも +5 で動かない)。
  `testing.Benchmark(...).AllocedBytesPerOp()` に変えたら差が 48,745 B → 682 B と
  71 倍分離し、変異で確実に red になった
- **`src/glogx/frame_alloc_test.go` の `TestFrameAllocBudget` (047 で新設) も回数ベース**。
  あちらが削ったのは「行ごとの確保 = 回数」なので回数で正しく効くが、
  **バイトだけ増える退行は素通りする** (実際 046 で allocs 完全一致のまま B/op が
  +0.8〜1.0% 増えたが、機構は特定できていない)
- 併せて `TestFrameAllocBudget` は `RUNEWIDTH_EASTASIAN=1` で落ちる (issue 054)。
  **安全機構が 2 つの穴を持っている**ことになる

→ 本 issue の対応時に「CI の予算」だけでなく **`frame_alloc_test.go` にバイトの
ゲートを足す**ことも同時に扱うこと (048 の
`TestStatusFrameAllocBytesDoNotScaleWithFileCount` が書き方の見本)。

## 対応方針 (案)

`bench_glogx.sh` に `-benchmem` を足し、`allocs=<name> n=<value>` のような行を出して
`bench_budgets.ci` に確保の予算も持たせる。時間と同じく `rel` (較正器) で正規化する必要は
無い (確保は機械に依存しない = むしろ時間より安定した指標)。

⚠️ **確保はマシン非依存なので、時間より厳しい上限を置ける**。時間の予算が「桁級の回帰だけ
捕まえる」粗さなのに対し、確保は実測 +数% で締められる。

## 未確認

- `check_bench_budgets.sh` の書式を増やす影響 (nvim / tmux 側の bench も同じ checker を
  使っているなら、そちらへの波及を確認する必要がある)
- CI の Linux + `-race` での allocs/op の実測値 (上限を決めるのに要る)

## 関連

- `issues/done/047-perf-glogx-frame-alloc-amplification.md` (確保が本体だった実測)
- `issues/done/048-perf-glogx-status-displayindex-per-frame.md`
- `src/glogx/frame_alloc_test.go` (test 側のゲートと、上限の決め方の規律)
