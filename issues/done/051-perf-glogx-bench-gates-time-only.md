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

---

## 対応 (2026-08-14)

`bench_glogx.sh` に `-benchmem` を足し、`metric=<name>_alloc_kb ms=<B/op ÷ 1024>` 行を
出して `bench_budgets.ci` に**絶対予算 (実測 +3%)** を持たせた。併せて
`frame_alloc_test.go` の `TestFrameAllocBudget` にバイトの上限を追加した
(回数の上限は据え置き。両方を同時に見る)。

### 「未確認」の 1: checker の書式を増やす影響 → **増やさないことで解いた**

この repo には既に「**単位は metric 名が持ち、行の項目名は常に `ms=`**」という流儀が
ある (tmux の `server_rss_mb` / nvim の `startup_cpu_ms`)。それに合わせて `_alloc_kb` と
命名したので、`check_bench_budgets.sh` / `bench_stats.sh` のゲート論理は**無改変**。
nvim / tmux / zsh への波及も、python3 不在時の縮退経路 (awk が `ms=` を再出力する) の
破綻も構造的に起きない。issue 案の `allocs=<name> n=<value>` (行の種類を増やす形) は
採らなかった。

唯一直したのは Step Summary の表ヘッダ `budget (ms)` → `budget` (既存の `_mb` / `_cpu_ms`
の行も時間に見えていた)。

### 「未確認」の 2: CI (Linux) の実測値 → **測って解いた**

B/op は環境を跨いでもほぼ動かない (実測差 0.15% 以内)。だから +3% で締められる:

| 環境 | view_steady B/op |
|---|---|
| darwin/arm64 go1.25.4 (M3 Max, GOMAXPROCS=14) | 30744 |
| darwin/arm64 go1.25.0 (CI が go.mod から選ぶ版) | 30744 |
| darwin/arm64 GOMAXPROCS=2 + TERM/LANG 未設定 | 30745 |
| linux/arm64 go1.25 (docker) | 30744 |
| linux/amd64 go1.25 (docker、CI と同じ OS/arch) | 30744 |

`-race` の水増しも小さい (list 30744 → 30776 = +0.1%)。回数と違ってバイトは
-race でほぼ変わらないので、test 側の上限も素の実測に近い値で置けている。
test 側のゲート (`make -C src/glogx test` = `go test -race`) は **CI と同じ linux/amd64 の
-race で実測して pass を確認済み** (docker、list 30769 B ≤ 上限 31700)。

### 変異検証 (バイトだけ太らせる形で red を確認)

⚠️ 変異は「確保を**足す**」ではなく「既存の確保を**太らせる**」を選んだ。回数も一緒に
増える変異では、red になっても**既存の回数ゲートを再確認しただけ**でバイトゲートは
未検証のままになる。`tui.go` の `b.Grow(size)` → `b.Grow(size * 2)` (フレーム結合の
pre-grow を倍にする。確保は 1 本のまま太る) を隔離 worktree で実験:

- `TestFrameAllocBudget`: **回数は全ケース据え置きで予算内** (135/135/317/213/157 ≤ 138/138/322/217/162)、
  **バイトは全ケース red** (list 30768 → 38968 B)。`go test -race ./...` を repo 全体で回して
  **落ちたのはこのテストだけ** = 他のどのテストもこの退行を見ていない
- CI bench: `*_alloc_kb` が 7/8 で予算超過 (view_steady 30.023 → 38.023 KB)。同時に
  **時間の metric は全て予算内のまま** (0.026 → 0.028ms / 予算 3ms) = 「時間だけ見ていると
  確保の退行を通す」という本 issue の主張そのものを再現した
- `render_large_patch_alloc_kb` だけ不変 = フレーム結合を通らない経路。ゲートの視界が
  意図どおり分かれている

### awk の列ずれガード (false green を 1 つ潰した)

`-benchmem` の列を位置 (`$5`) で読むと、`b.ReportMetric` を持つ benchmark が混ざった
だけで**別の数値を予算照合して黙って pass する**。実測: `frames/op` を 1 本足すと
`BenchmarkViewSteady-14  8654  26932 ns/op  1.000 frames/op  30744 B/op  ...` となり、
ガード無しなら `view_steady_alloc_kb ms=0.001` (真値 30.023) が予算内で通る。
`$4 == "ns/op" && $6 == "B/op"` を確認してから emit する形にし、ずれたら emit しない
= checker の「予算にある metric が出力に無い」で loud に落ちることを実験で確認した。

### 敵対的レビュー (観点を分けた 3 視点。codex はこのマシンでは使わない運用)

**採用して直したもの**:

- **R1-5 awk の前方一致**: `/^BenchmarkViewSteady/` は `BenchmarkViewSteadyJA` にも一致する。
  実測で「JA の行が `view_steady` として出る」ことを確認 (兄弟 benchmark を `-bench` に足した
  瞬間、安い方の数値で予算が恒久的に固定される)。`-<GOMAXPROCS>` を落とした**完全一致**へ変更。
  `emit()` は時間と `_alloc_kb` を対で吐くので、名前衝突 1 件が 2 metric を汚す = 本 diff が
  影響を広げていた
- **R2 の留保「cross-env 実証が view_steady 1 本の外挿」**: 全 8 metric で darwin/arm64 と
  linux/amd64 を突き合わせ直し (最大差 0.09%)、予算ファイルに表として記載
- **R3-4 `RUNEWIDTH_EASTASIAN=1`**: バイト上限も issue 054 の env 前提を継ぐことを実測
  (list 135 回 30776 B → 322 回 41139 B)。test 側のコメントに痕跡を残した
  (issue が移動しても現場に残るように)

**記録に留めたもの (対処を入れない判断)**:

- **R1-4 min 集約**: 確保に min は原理的に誤った推定量 (20 run 中 1 run 静かなら沈黙)。
  ただし (a) B/op はこのベンチでは決定的、(b) `B/op = 総バイト ÷ N` なので遅い runner ほど
  初期化残差が乗り min の方が安定、の 2 点で min を維持した。根拠と残る穴を予算ファイルに明記
- **R1-1 ベンチ最適化 (フレームのメモ化)**: フィクスチャは毎 iteration 同一フレームを描くので、
  「前フレームと同じなら使い回す」実装を入れると両ゲートが「大幅改善」として緑になり、
  以後どんな確保退行も見えなくなる。ベンチ由来のゲート一般の性質で、本 diff 固有ではない
- **R1-3 1 回きりの確保は N で割られて余白に吸収される** / 200ms より長い周期の確保は
  窓に入らない。B/op の定義そのもの。フレーム 1 枚あたりのコストを守る道具として使う

### 残った限界

- **ベンチ側と test 側の二重管理は解消していない** (本 issue が挙げていた点)。役割が違う
  ので統合しなかった: test 側は `make test` 一発で回る -race 付きの門番 (回数 + バイト)、
  bench 側は経時比較つきで `render_large_patch` / `model_init_200` など test 側が持たない
  経路も見る。確保が増える変更を意図して入れるときは両方を同じ commit で更新する
  (その旨を両ファイルのコメントに書いた)
- `TestFrameAllocBudget` の実行時間が +6 秒 (`testing.Benchmark` を 5 fixture 分回すため。
  glogx パッケージ 12s → 18.7s)
- issue 054 (`RUNEWIDTH_EASTASIAN=1` で落ちる) は本 issue の範囲外。バイト上限も同じ
  env 前提を継いでいる (測定条件は `bench_budgets.ci` のコメントに明記)
- CI 実測はまだ 0 run。push 後に Step Summary で `🆕 初計測` の値を確認し、+3% の内側に
  あることを見ること (`rules/bench-watch-after-push.md`)
- **ゲートが一度も見ていないフレーム経路がある** (R1-2): issues ドロワー (`issuesOv`)・
  起動直後の usage グランス (フィクスチャが `m.usageOv = usageOverlay{}` で消している)・
  toast・PR status オーバーレイ・zoom/glide。048 が status viewer で潰したのと同型の退行を
  issues 側に入れても、動く metric が 1 つも無い。フィクスチャを足す作業なので別 issue 向き
