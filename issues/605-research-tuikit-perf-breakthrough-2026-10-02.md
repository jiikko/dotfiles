# 605 (research): tuikit の性能でブレイクスルーを起こすには何が要るか (2026-10-02)

起票日: 2026-10-02

## 概要

ユーザーの依頼 (2026-10-02): 「src にある tuikit でさらにパフォーマンス改善を進めて、ブレイクスルーを起こすには何が必要かを調べて」。

**結論: tuikit の中の定数倍の改善 (asm・確保の削減) はもう画面の速さを変えない。効くのは「形」を変える 3 つ**:

1. **データの量に比例する仕事をフレームから追い出す** (保持・memo)。今いちばん重いのはここで、pro-con の詳細 (引き出し) が
   活動 1000 件 (上限) で **View 1 回 約 28 ms・24 MB 確保** (盤面だけの View の約 120 倍)。→ [606](606-perf-pro-con-drawer-rerenders-all-activity-markdown-per-frame.md)
2. **測る境界を「View の文字列」から「端末へ出るまで」へ広げ、量に対する伸びを測る**。bubbletea v2 のレンダラ (文字列 → セル → 差分) が
   View の約 2.3 倍の CPU を使っているのに、どのベンチも測っていない。→ [607](607-test-tuikit-frame-bench-misses-renderer-and-data-scaling.md)
3. (tuikit の外) **レンダラが変わっていない行を読み飛ばす**。bubbletea の公開 API (`View.Content` は string だけ。v2.0.10 でも同じ) では
   tuikit から届かない。fork か upstream への提案が要るので、1 と 2 の後で、レンダラが支配的な実例が出たときに再評価する (下の「候補 3」)

## 前提: ここまでに済んでいること

- asm (NEON) と切り詰めの速い道・日本語の受理: epic 523 / 520 / 524 (どれも done)。523 の結論「**asm はここで打ち止め**。次に効くのは確保を減らすこと」
- 595 (done): src 全体で asm の候補 0 件

## 実測 (2026-10-02、この Mac (darwin/arm64、14 core)、go1.26.0、master 715782de。ロードアベレージ 5〜9。各 `-count 3`、benchstat ではない粗い値)

### A. 既存のフレームのベンチ (View だけ)

| benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| glogx ViewSteady / ViewSteadyJA | 27〜28 µs | 30 KB | 127 |
| glogx ViewWithDiffJA | 30 µs | 47 KB | 209 |
| glogx CursorMoveViewJA | 58 µs | 61 KB | 254 |
| glogx IssuesViewFrame2000 | 21 µs | 36 KB | 211 |
| pro-con FrameBump / FrameGlide / FrameMove | 212〜220 / 165〜170 / 79〜85 µs | 353 / 292 / 128 KB | 1341 / 1121 / 943 |
| pro-con DiffBoardView closed / open | 198〜214 / 235〜260 µs | 347 / 359 KB | 1359 / 1426 |

- pro-con の確保 (alloc_space) の内訳: pro-con 自身 (`cellsOf` 31%・`columnBlock` 21%・`boardLines` の `strings.Join`) が大半。
  tuikit は `termwidth.ansiTruncate` 8.4%・`layout.ComposeDrawer` 7.3%・`sgrOnly` 7.1%・`SplitAround` 6.0% (cum。重なりあり)
- GC の上限: 同じテストバイナリを GOGC=100 / 400 / off で回すと FrameBump 210〜239 → 158〜178 µs、FrameMove 88〜105 → 62〜64 µs。
  **確保を 0 にしても View は 2〜3 割しか縮まない** (ベンチは本物の program よりヒープが小さく GC が多めに走るので、これは上限寄りの見積もり)

### B. View の外: bubbletea v2 のレンダラ (どのベンチも測っていない)

bubbletea v2.0.8 の `cursedRenderer.flush` は、View の文字列が前と違えば毎回 `cellbuf.Clear()` → `uv.NewStyledString(content).Draw(...)` で
**画面全体を解析してセルへ書き直し**、`TerminalRenderer.Render` で前のセルと差分を取る。同じ処理を pro-con のフレームに当てた使い捨てのベンチ
(コードは 607 に貼った) の結果 (200 × 50 = 1 万セル、フレームは約 24 KB):

| | Draw だけ (解析 + セル書き込み) | Draw + 差分 + Flush | 端末へ出るバイト/コマ |
|---|---|---|---|
| 揺れ (Bump) | 341〜359 µs | 486〜497 µs | 約 161 B |
| 枠の滑走 (Glide) | 312〜326 µs | 539〜550 µs | 約 2.9 KB |
| 静止画面 (同じ文字列を繰り返す) | 313〜320 µs | 457〜481 µs | 約 4 B |

- **1 コマの CPU は View 約 0.2 ms + レンダラ約 0.5 ms**。レンダラが約 7 割
- 内訳 (CPU profile): `StyledString.Draw` 67% (`printString` 52%・`RenderBuffer.SetCell` 34%・その中の `cellEqual` 20%・`x/ansi.DecodeSequence` 19%)、
  差分 (`TerminalRenderer.Render`) 13%
- **SGR を減らしても縮まない**: 同じフレームを uv で解析して書き戻した文字列 (CSI 1762 → 970 個、23.6 → 19.8 KB) の Draw は 326 µs で、元の 316 µs と同等。
  支配的なのはセル 1 つずつの `SetCell` (約 32 ns/セル) で、文字列の形ではない
- 揺れのコマでは 50 行のうち平均 **4.4 行**しか変わらない (枠の滑走は最初の 3 コマが 39 行、以降 5 行)。変わっていない行を飛ばせればレンダラは約 9 割減る見込み (未実測)

### C. データの量に比例する仕事 (今いちばん重い)

pro-con の詳細を開いたカードの活動 (PG の応答の markdown・道具の呼び出し。上限 `live.activityKeep` = 1000) の件数を変えた View (606 に詳細):

| 活動 | 本文の行 | View | B/op |
|---|---|---|---|
| 0 | 9 | 0.24 ms | 380 KB |
| 30 | 128 | 1.2 ms | 1.16 MB |
| 300 | 1208 | 8.5 ms | 7.5 MB |
| 1000 | 4014 | **28 ms** | **24 MB** |

- View は Update のたびに呼ばれる (bubbletea v2.0.8 `tea.go` の `eventLoop`: `model.Update(msg)` の直後に `p.render(model)`)。処理中のカードを開いていれば
  spinner (100 ms) だけで毎秒 10 回、j/k の glide (33 ms) では毎コマ走る。1000 件ならフレームの枠 (33 ms) をほぼ使い切る
- 時間の 2/3 は `tuikit/highlight.Lang` (chroma の字句解析を code block の 1 行ごとに) で、残りは markdown の解析と折り返し

## ブレイクスルーの候補と判断

| # | 候補 | 見込み | 判断 |
|---|---|---|---|
| 1 | データ量に比例する整形をフレームの外へ (保持・memo) | 詳細を開いた View が 28 ms → 盤面と同程度 (未実測) | **やる** → 606 |
| 2 | フレームのベンチを「端末へ出るまで」と「量に対する伸び」で測る | それ自体は速くしない。1 と同じ形の退行 (268 / 270 / 274 / 275 と今回) を量で捕まえる | **やる** → 607 |
| 3 | レンダラで変わっていない行の解析を飛ばす (行のハッシュで比較) | レンダラ約 0.5 ms → 0.1 ms 前後 (未実測) | 保留。下記 |
| 4 | tuikit の確保をさらに減らす (`ansiTruncate` / `ComposeDrawer` / `SplitAround`) | View の数 % (A の内訳と GC の上限から) | 採らない (1 コマ全体では誤差) |
| 5 | SGR を最小化して出す | なし (B で実測。Draw は同等) | 採らない |
| 6 | GOGC を上げる / メモリ上限を置く | View の 2〜3 割 = 1 コマの 1 割以下 | 採らない (常駐の program のメモリと引き換えにする価値が無い) |
| 7 | asm の追加 | 523 の打ち止めのまま | 採らない |
| 8 | FPS (既定 60) を上げて入力から表示までを縮める | 待ちの平均 約 8 ms → 4 ms (計算) | 採らない (CPU の話ではなく、遅いという報告も無い) |

### 候補 3 (保留) の再評価の trigger と、やるなら何が要るか

- trigger: 606 の後で、ユーザーの操作で「重い」と分かる場面の profile で、レンダラ (`ultraviolet.(*StyledString).Draw` + `TerminalRenderer.Render`) が
  CPU の半分以上を占めたとき。または 400 × 100 のような大きな端末で 1 コマが 5 ms を超えたとき (セル数に比例するので、200 × 50 の約 4 倍 = 約 2 ms の見積もり)
- tuikit からは届かない: `tea.View` は `Content string` だけを持ち、bubbletea 側が毎回全体を解析する (v2.0.10 の `tea.go` でも同じ)。
  やるなら (a) ultraviolet / bubbletea へ「前の行と同じ文字列なら Draw を飛ばす」を提案する (b) replace で fork を持つ、のどちらか。
  (b) は 4 つの消費者 (glogx・pro-con・schedkeys・restartable) の依存を握ることになるので、(a) を先に試す

## 調べ方

- 既存のベンチを `-benchmem -cpuprofile -memprofile` で回し、`go tool pprof -top` で読んだ
- B と C は master の使い捨ての worktree に `_test.go` を足して回した (commit していない。コードの要点は 606 / 607 に貼った)
- 反証レビュー: 未 (下の進捗)

## 関連

- 523 / 520 / 524 / 494 / 595 (どれも done) — asm と確保の前段
- 268 / 270 / 274 / 275 (done) — フレームの中でデータ量に比例する走査を消した前例 (606 と同じ形)
- `docs/glogx-bubbletea-v2.md` — bubbletea v2 の前提

## 進捗

- [x] 実測と候補の判断 (この issue)
- [x] 606 / 607 の起票
- [ ] 反証レビュー
