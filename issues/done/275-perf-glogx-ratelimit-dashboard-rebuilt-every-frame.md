# perf: ratelimit ダッシュボードが毎フレーム全面再構築される（メモ化もゲートも無い）

起票日: 2026-09-06
カテゴリ: perf
優先度: 中（1 フレーム 306µs / 377KB。doctor の実機値 223µs より重く、同じ扱いにすべき）

## 由来: issue 273 の却下を撤回して起票したもの

2026-09-05 の監査（273）は、この件を**却下**していた。却下理由は:

> データ着信後は `spinnerActive()` の `rlDashLoading()` が偽になり、`View()` は打鍵と
> 1 分周期の usage 更新でしか回らない。doctor と違って 12.5fps の描画経路に乗らない。

🚨 **この機構は誤り**（273 の敵対的レビューが実験で反証）。`rlDashLoading()` は
`tui.go:spinnerActive` の **18 項の OR のうちの 1 項**でしかなく、それが偽であることは
`spinnerActive()` について何も言わない。

## 何が起きているか

`toggleRatelimitDash` が触るのは `rlDash.toggle()` / `usageOv.dismiss()` / `fetchCmd` /
`maybeTick()` だけで、**長時間持続する 2 項をどちらも落とさない**:

- `len(m.awaitCI) > 0` — push 直後。`settleAwaitCI` は CI が見えるか SHA が `commits` から
  外れるまで降ろさない
- `m.panelHasRunningJob()` — `m.details[panelSHA]` に実行中 job がある間ずっと真

したがって **push 直後や CI 実行中にダッシュボードを開くと、12.5fps で全面再構築が回る**。

メモ化も無い: `tui.go:viewLines` が `m.rlDash.lines(m.ratelimitOpts())` を毎 View で直接呼び、
`ratelimitDash` の状態は `shown bool` **だけ**（`ratelimit_dashboard.go`）。
`usage.RenderDashboard` は毎回 `newBraille` でセル数ぶんのスライスを確保し直す。

## 実測

`d.lines(ratelimitRenderOpts{width, page, colored:true, snap:…})`、darwin/arm64 M3 Max、
`-benchtime 500x -count 3`:

| 幅 × 高 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| 120×40 | 203 µs | 182 KB | 1,189 |
| **200×50** | **306 µs** | **377 KB** | **1,263** |
| 240×60 | 411 µs | 594 KB | 1,437 |

12.5fps 換算で **CPU 0.38% / 4.7 MB/s**。

🚨 **コストは「ダイアルの枚数」ではなく `newBraille` のセル数（端末の幅 × 高）に比例する**
（120×40 → 240×60 で 2.0 倍）。273 の却下欄が書いた「ダイアルの枚数も枠数ぶんの小さい定数」も
外していた。

### 索引が残したものとの比較

| | 1 フレーム | 12.5fps 換算 | 扱い |
|---|---:|---|---|
| doctor 実機（issue 270） | 223 µs / 205 KB | 0.28% / 2.6 MB/s | **起票（中）** |
| `lines()` の 163 却下値 | 172 µs | — | 却下（今も妥当） |
| **ratelimit（200×50）** | **306 µs / 377 KB** | **0.38% / 4.7 MB/s** | ← 本 issue |

**270 として残したものより 1.37 倍重い。** 同じ基準なら起票側。

### この repo が「毎フレームのコストを受容した」前例との比較

`tui.go` の 2026-07-25 perf 監査のコメントが受容している値は **332µs / 733KB**。ただし
その条件は「**fetch 中の数秒だけ**」。ratelimit はダッシュボードを開いている間ずっとなので、
**前例の条件を満たしていない**。

## 発火条件

- ratelimit ダッシュボードを開いている（`u` 系のキー）
- かつ `spinnerActive()` の 18 項のどれかが真。長時間持続するのは
  `len(m.awaitCI) > 0`（push 直後）と `panelHasRunningJob()`（CI 実行中）
- **silent に壊れる**: 機能は正しい。build も lint もテストも緑
- ゲートは**時間も確保もゼロ**（`frame_alloc_test.go` の 8 ケースにも
  `tests/glogx/bench_budgets.ci` にも ratelimit の metric は無い）

## 対応 (2026-09-06)

**1 秒粒度のメモ化を採用**（ユーザー選定）。鍵は `width` / `page` / `colored` / `snap`
ポインタ / `now.Unix()`。`spinner` と `err` は盤を描く分岐で使われないので鍵に入れない
（`spinner` は毎フレーム変わるので、入れると必ずキャッシュを外す）。

🚨 **これは見た目の挙動変更**。針とゲージは `now` の連続関数で描かれる設計
（`usage/dial.go:cardPace` の「1% 刻みに丸めると針がガタつく」）なので、
針の更新粒度が 80ms → 1s になる。枠は時間単位で動くので目には分からない想定だが、
「性能のために見た目の粒度を落とした」ことは記録しておく。

実測 (120×40, `-race`): **1065 allocs / 239,319 B → 173 allocs / 82,055 B**
（6.2 倍 / 2.9 倍）。フレーム時間は 80µs 前後。

確保予算 (`frame_alloc_test.go`) も**新実測へ締め直した** (177 / 84,600)。
締めずに残すと「6 倍悪化しても緑」になり、issue 269 と同じ「観測できない予算」を
新設したことになる。

## 推奨対応（起票時）

1. **`lines()` の結果をメモ化する**。`ratelimitDash` は `shown bool` しか持たないので、
   `snap` / 幅 / 高 / 色を鍵にしたキャッシュを足すのが素直
   （`status_view.go` の `idxCache`、`tui.go` の `linesValid` が前例）
2. **退行防止のゲートを足す**。`frame_alloc_test.go` に ratelimit ケース、および
   **幅を変えた 2 点の比**（`ratelimit_scale_x100`）— 絶対時間の予算は issue 269 の理由で
   足さないこと
3. 🚨 **1 と 2 は doctor（270 / 274）と同じ問題なので、まとめて設計する価値がある**。
   共通するのは「`spinnerActive` に載る全画面ビューアが毎フレーム全面再構築する」形

## 反証の試み

`spinnerActive` から `len(m.awaitCI) > 0 || ` を外す変異（`go build` 通過を確認）で
当該 assert が red になり、`panelHasRunningJob` 側は独立に true のままであることを確認済み
（2 項がそれぞれ単独で効いている）。

`src/glogx/CLAUDE.md` / `README.md` / `ratelimit_dashboard.go` / `usage/` のコメント /
`issues/` と `issues/done/` に「毎フレーム再構築は意図的」と書いた箇所は見つからなかった。

## 関連

- 273（この件を誤った機構で却下していた索引。撤回済み）
- 270 / 274（doctor 側の同型。「全画面ビューアの毎フレーム再構築」ファミリー）
- `tui.go` の 2026-07-25 perf 監査コメント（受容の前例と、その条件）
