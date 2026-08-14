# glogx: CI bench に issue scan の metric が無い (起動時 I/O のコスト退行が見えない)

起票日: 2026-08-15
種別: perf
優先度: **P3** (最悪ケースの「全文読み」退行は 052 のテストが既にローカル/CI で red にする。
残っているのは「全文は読まないがコストが増える」形の緩やかな退行だけ)

## 何が起きるか

`tests/glogx/bench_budgets.ci` の metric は描画系 (`view_steady` / `model_init_200` 等) だけで、
**issue scan (`scanIssues` = `issues.Scan` + 全件 `LoadMeta`) を測る metric が無い**。
scan 系の benchmark 自体が `src/glogx` に 1 本も無い (2026-08-15 時点の grep)。

scan は起動時と外部編集の見張り (issues_watch) のたびに走る経路で、050 の実測では
実 repo の issues/ (50 件 395KB) で 1,546,027ns → 753,285ns と 2 倍動いた実績のある
ホットパスだが、この軸の退行は CI で観測されない。

## 既に守られているもの (このissueの残余を正しく見積もるため)

- **「全文を読む」形の退行**は `TestScanIssuesDoesNotReadFullBody` (issue 052) が
  read バイト数で red にする。CI の go test でも走る
- 描画側の時間/確保は bench_budgets.ci がゲート済み (issue 051)

つまり残る穴は「read 量は増やさないが scan の CPU/確保が緩やかに増える」退行
(例: LoadMeta の正規表現追加・PlainLine の多重適用・sort の劣化)。

## 対応方針 (案)

`BenchmarkScanIssues` を新設して bench_glogx.sh の一覧と `bench_budgets.ci` に足す:

- fixture は既存 bench の流儀 (repo コード非依存の合成 issue ディレクトリを tempdir に生成、
  件数は実測分布に合わせて ~50 件) に揃える
- 時間 (`issue_scan`, rel) と確保 (`issue_scan_alloc_kb`, 絶対値 +3%) の 2 本立て
  (051 と同じ判断: コストの本体は確保)
- ⚠️ ファイル I/O を含む benchmark になるため、共有 runner のディスク揺れが時間側に乗る。
  rel 較正 (glogx_calib) は CPU 系の揺れしか吸わないので、時間予算は他 metric より
  粗め (実測の ~20 倍級) で入れて様子を見る。確保側は I/O に依らず決定的なはず (要実測)
- benchmark 追加時は bench_glogx.sh ヘッダの「対象一覧と awk と budgets を同時に更新」
  規約に従う (漏れは checker が CI で fail させる)

## 未確認

- scan benchmark の B/op が run 間・環境間で決定的か (051 の確保 metric は 0.1% 内だったが、
  こちらは tempdir への実 I/O を含むため要実測)
- tempdir 生成を benchmark の計測外 (b.ResetTimer 前) に置いたときの 1 iteration あたりの
  実時間 (CI の bench 予算 -benchtime=200ms に収まるか)

## 関連

- `issues/done/052-perf-glogx-scan-io-invariant-unguarded.md` — 「関連する穴」節がこの issue の起源
- `issues/done/051-perf-glogx-bench-gates-time-only.md` — 確保ゲートの導入判断 (2 本立ての先例)
- `issues/done/050-perf-glogx-issue-list-reads-full-body.md` — scan がホットパスである実測の一次情報
- `tests/glogx/bench_glogx.sh` / `tests/glogx/bench_budgets.ci`
