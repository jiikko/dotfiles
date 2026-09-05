# perf: issues viewer のフレームが可視行 × 全 issue 件数で走る (件数比例が復活)

起票日: 2026-09-05
カテゴリ: perf
優先度: 中（体感は 2000 件級の repo + 複数選択で顕在化。機能は壊れない）

## 何が起きているか

`issuesView.rowLine` が **可視行ごとに** `ensureDisplayRows()` を呼び、その中で
`displayRowsSource` と `rows` を **全件** 突き合わせている。可視は 40 行程度なので、
1 フレームあたり `可視行数 × 全 issue 件数` の比較が走る。

- `issues_view.go:rowLine` — 先頭で `v.ensureDisplayRows()`（可視行ごと = 約 37 回/フレーム）
- `issues_view.go:rowLine` — 行ごとに `v.selection()` を呼び、`selection` も先頭で `ensureDisplayRows()`
- `issues_view.go:selection` — 範囲 `lo..hi` を `isIssueDisplayRow(i)` で検査し、
  **`isIssueDisplayRow` も先頭で `ensureDisplayRows()`** を呼ぶ → 選択 k 行で `k × O(n)` が
  さらに可視行ごとに掛かる

`listLines` はループの**前**に一度 `ensureDisplayRows()` を呼んでおり、ループ中に `rows` は
変わらない。したがってループ内の呼び出しは同じ全件走査の繰り返しで、結果は必ず同じになる。

## 実測

環境: darwin/arm64, Apple M3 Max, `go test -run='^$' -bench=... -benchtime=2000x -count=3〜5`
（選択ありは使い捨てベンチ。commit していない）

### 退行前後（同一マシン・同一コマンドで直接比較）

| リビジョン | 40 件 | 2000 件 | 件数比 |
|---|---|---|---|
| `1a1a6425`（`fc0f65ab` の親） | 23.0 µs | **22.1 µs** | **0.96 倍**（比例なし） |
| `eb8d670e`（現 master） | 25.4 µs | **62.4 µs** | **2.46 倍** |

2000 件フレームは 1 commit で **22.1 → 62.4 µs（2.8 倍）**。40 件側はほぼ不変なので、
増えた分はすべて「見えない行のための仕事」。

### 3 箇所の `ensureDisplayRows()` を外した場合（現 master 上での実験）

| ケース | 現状 | 外した場合 |
|---|---|---|
| `BenchmarkIssuesViewFrame` (40 件) | 25.4 µs | 24.7 µs |
| `BenchmarkIssuesViewFrame2000` (2000 件) | **62.4 µs** | **24.0 µs** |
| 40 件 / 20 行選択 | 34.3 µs | 25.0 µs |
| 2000 件 / 20 行選択 | **439 µs** | **27.2 µs** |

- 件数比 2.46 倍 → 0.98 倍、選択ありの件数比 12.8 倍 → 1.09 倍。どちらも比例が消える
- alloc は 213 / 215 allocs・**33.3 / 35.1 KB**（B/op ÷ 1024。`bench_glogx.sh:emit` と同じ換算）で
  ほぼ不変。純粋に CPU 時間だけが伸びている

### CI 実測（退行後、Bench workflow は緑）

run 33964922996 / job 101303286129（2026-09-05T12:04Z, headSha `eb8d670e`）:

```
metric=issues_view_frame ms=0.027
metric=issues_view_2000  ms=0.074      ← 件数比 2.74 倍
metric=status_view_frame ms=0.035
metric=status_view_2000  ms=0.038      ← 件数比 1.09 倍（status 側は健全）
metric=glogx_calib       ms=0.089      ← 較正器の基準 0.15 を下回る = 静穏 run
```

較正器が静穏なのに `issues_view_2000` だけが跳ねている = 混雑ノイズではなく真の回帰。
`status_view_2000` が 1.09 倍のままなので、退行は issues viewer 固有と切り分けられる。

`tests/glogx/bench_budgets.ci` の `issues_view_frame` / `issues_view_2000` の直上コメントが持つ導入時記録（2026-08-15 / issue 062）は
`issues_view_frame 0.022 / issues_view_2000 0.024`（**「件数比例なし」と明記**）。
その不変条件は現在失われている。

### 独立再現（敵対レビュー）

同一マシンで別途測り直した結果（min-of-5）: 現 master 40 件 **25.25 µs** / 2000 件 **61.54 µs**
（比 2.44）、`1a1a6425` 40 件 **21.78 µs** / 2000 件 **20.32 µs**（比 **0.93**）、
選択 20 行 2000 件 **431.66 µs**（比 12.4）。上の表と桁・比とも一致する。
計器の同一性も確認済み（`tui_bench_test.go` は `1a1a6425` と現 master でバイト同一）。

## 発火条件

- issues viewer を開いている（`i`）こと。かつ `issues/` 配下の issue 件数が多いこと
- **毎フレーム**発火する（カーソル移動・トースト・見張りの反映・アニメ tick のたび）
- 選択（`shift+↑↓` / `J/K`）中は選択行数に比例してさらに増幅する。選択 20 行 / 2000 件で
  1 フレーム 0.44ms。100 行選択では **1.884ms**（実測。`-count=3` で 1.878〜1.888）
- **silent に壊れる**: build もテストも通り、機能は正しい。遅くなるだけ

## いつ入ったか

`fc0f65ab` (2026-09-05 03:20:50 +0900, `feat(glogx): issues viewer で epic/<name>/ を親子として
折り畳み表示し、group 内 next/ を claim 先にする`) で `ensureDisplayRows` が新設され、
同時に `rowLine` / `selection` / `isIssueDisplayRow` へ入った。それ以前は `rowLine` に
遅延同期は無い（`a104a16d` 2026-07-31 の初版から fc0f65ab まで）。

## なぜ既存のゲートが止めなかったか

これは別 issue（→ 269）に切り出したが、要点だけ:

- 時間の予算 `issues_view_2000 3 rel`（3ms。較正器が基準を下回る静穏 run ではスケール 1 倍）に
  対し **CI 実測 0.074ms = 約 40 倍の余裕**。2.8 倍の悪化では赤くならない
- alloc の予算 `issues_view_2000_alloc_kb 36.6` に対し CI 実測 35.069 KB（余裕 +4.4%）。
  🚨 予算が見ているのは **allocs/op ではなく B/op ÷ 1024**。退行前後の B/op は
  **35784 → 35912 = +0.36%** なので、余裕 +4.4% に対して桁が足りず観測できない
  （allocs/op の +0.94% は予算の対象ですらない）
- 選択ありのフレーム（12.8 倍と最も悪い形）は**ベンチが存在しない**

## 推奨対応

**`ensureDisplayRows` の全件比較そのものをやめる。** これだけで利得の 100% が取れる
（下の実測）。描画ループ内の呼び出しを外す必要は無い。

今は `displayRowsSource` と `rows` を毎回**全要素**突き合わせているが、`rows` を書き換える側で
世代（`rowsGen int`）を進め、`displayRows` の世代と突き合わせれば **O(1)** になる。

### なぜ「呼び出しを外す」ではなく「比較を O(1) にする」か（敵対レビューの実測）

3 箇所の呼び出しを**全部残したまま**、`ensureDisplayRows` の要素比較ループだけを O(1)
（長さ一致で即 return）に変えた実測:

| ケース | 現状 | 呼び出しを 3 箇所削除 | **比較を O(1) 化（呼び出しは残す）** |
|---|---:|---:|---:|
| 2000 件 | 61.5 µs | 24.3 µs | **21.95 µs** |
| 2000 件 / 20 行選択 | 431.7 µs | 24.8 µs | **24.28 µs** |

**削除より速い。** 呼び出し回数はそのままで件数比例が完全に消えるので、コストは
`ensureDisplayRows` の全件比較ループ以外にありえない（プロファイルを取るまでもなく確定）。
さらに alloc が 3 条件すべてで完全一致（40 件 213 allocs / 34104 B、2000 件 215 allocs /
35912 B）なので、`rebuildDisplayRows` は毎フレーム走っていないことも実測で確定する。

したがって **「描画ループ内の呼び出しを外す」案は不要**（対症であるうえ、利得も劣る）。

### 直接差し替えるテスト/helper は存在しない

`ensureDisplayRows` の doc は役目を「`rows` を直接差し替えるテスト/helper と通常の refresh の
両方を支える遅延同期」と書いているが、**その「テスト/helper」は実在しない**（敵対レビューが
grep で確認）:

- `issuesView.rows` への代入は production に **1 箇所だけ** — `issues_view.go:refresh` の
  `v.rows = v.visibleIssues()`。その 6 行下で同じ関数が `v.rebuildDisplayRows()` を呼ぶ
- テスト側も `issuesView.rows` を直接差し替えるものは無い（`loadedView` は `receive` → `refresh`
  を通る。`.rows =` を書くテストは全部 doctor / 一覧側の別の型）

つまり機構を残す唯一の理由として doc が挙げているものが実在しない。世代化するときは
**この単一書き込み点（`refresh`）で世代を進める**だけでよく、「直接差し替える人のための遅延同期」は
落としてよい。落とすなら doc も同時に直すこと（嘘が残る）。

🚨 **`rowLine` は先頭で `row := v.displayRows[i]` を境界検査なしで読む。** この不変条件
（`displayRows` が `rows` と同期している）が将来壊れたときの失敗モードは「描画がずれる」ではなく
**描画ループ内の index panic**。世代化と同じ変更で、この不変条件をテストで固定すること。

🚨 **鍵を `rows` だけにすると決めた事実を doc に書くこと。** 現行の `ensureDisplayRows` は
`rows` しか比べないので `expandedGroups` / `collapsedGroups` / `autoExpandedGroups` の変化を
構造的に観測できない（実例: `issues_view.go:finishClose` は `refresh()` の**後**に
`v.collapsedGroups = nil` する。`shown=false` の直後で再表示時に再スキャンが走るため現状は無害）。
世代化は同じ鍵なので同じ盲点を引き継ぐ — 等価なので退行ではないが、「遅延同期」の語から
展開状態も同期されると次の人が読む。

## 検証の作法（修正時）

- 修正後に `BenchmarkIssuesViewFrame` / `BenchmarkIssuesViewFrame2000` の比が 1 倍付近へ
  戻ることを実測で示す（数字を commit message に残す）
- 🚨 **「`go test ./...` が green」を安全の根拠にしない**。green が言えるのは「守っている
  テストが無い」までで、production の安全は上記の**単一書き込み点の不変条件**が根拠。
  変更と同じ commit でその不変条件をテストに固定すること
- **選択ありのベンチを恒久化する**（このケースが一番悪い）。`v := &m.issuesOv` の
  ポインタ受けにすること — 値でコピーすると `marked` の設定が本体へ届かず、
  ベンチが何も測らない（監査中に実際に踏み、12.8 倍を 1.0 倍と誤読しかけた）
- 予算は 269 で締める

## 関連

- `issues/done/062-*`（issues viewer のフレームベンチ導入。「件数比例なし」の出典）
- `tests/glogx/bench_budgets.ci` / `tests/glogx/bench_glogx.sh`
- 269（この退行をベンチ予算が観測できない件）
