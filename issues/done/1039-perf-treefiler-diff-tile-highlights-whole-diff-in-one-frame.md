# 1039 (perf): treefiler の diff のタイルが、diff 全体を 1 フレームの中でまとめて色付けする

起票日: 2026-11-06

## 概要

タイルで `d` を押して diff を出すと、取得した diff の全行を、描画の中 (`filer/tileview.go` の `view`) で `highlight.Diff` に通して色付けする。
`highlight.Diff` は行ごとに chroma の lexer を回すので、diff の行数に比例して 1 フレームが止まる。

## 実測 (2026-11-06。`make -C src/treefiler probe`、root=`src/`、200×50、go1.26.0、M3 Max 14 コア、load average 6〜7)

| 段 | 1 フレームの中央値 / 最悪 | そのフレームの確保 |
|---|---|---|
| `live-tile-diff` (600 行の code.go で、200 行を書き換えた diff。約 400 行) | 21.5ms / 23.2ms (5 サイクル)、22.1ms / 27.1ms (20 サイクル) | 12.6MB |
| 比べる: `tile-code` (本体のタイル。読んだ分だけ足して色付けする) | 1 打の p95 1.5ms / 最大 6.2ms | |

- CPU のプロファイル (`ARGS=-cpuprofile`) で、diff の段の時間は `highlight.Diff` → `highlight.codeLine` → chroma の `LexerState.Iterator` に集まる
- 🚨 `-memprofile` を付けた run では同じフレームが 466ms に出た。確保ごとの記録の負荷で膨らんだ値なので使わない (プロファイルなしの上の値を使う)
- **見積もり** (測っていない): 行数に比例するので、数千行の diff では 1 フレームが数百 ms 止まる。glogx の `F` の中のタイルでも同じ経路を通る

## 対応方針 (案)

- 動いている側に揃える: glogx (`src/glogx/internal/gitdata/gitlog.go`) と pro-con (`src/pro-con/ui/diffview.go`) は、diff の取得の処理 (UI の外) の中で
  `highlight.Diff` を通している。treefiler も diff の取得の goroutine (`filer/diff.go` の `diffJob`) で色付けまで済ませ、`view` は届いた結果を並べるだけにする
- または、本体のタイルと同じく見える行だけを色付けする (diff の行の種類と、どのファイルの lexer かは前から順に決まるので、種類の判定だけ先にまとめて行い、色付けを行ごとに遅らせる)
- 直す前に、直す前のバイナリと直した後を同じ条件で続けて tfprobe で回し、`live-tile-diff` のフレームを比べる (CLAUDE.md の「実物で測る」の PDCA)

## 関連ファイル

- `src/treefiler/filer/tileview.go` (`view` の diff の枝)・`filer/diff.go` (`diffJob`)・`src/tuikit/highlight/highlight.go` (`Diff`・`codeLine`)
- 計測の道具: `src/treefiler/tools/tfprobe` (`liveCycle` の `live-tile-diff` の段)

## 進捗

- 2026-11-06: 起票 ([1035](../1035-research-treefiler-audit-2026-11-05.md) の監査の後の計測)。反証レビューは通していない (実測と、呼び出し元 3 か所の grep で確かめた範囲)
- 2026-11-08: 対応方針の第 1 案で直した (perf(treefiler): diff のタイルの色付けを取得の goroutine へ移す)。`filer/diff.go` の `startDiff` が `highlight.Diff` まで済ませて
  `diffJob.result()` が (lines, styled, done) を返し、`view` は届いた色を並べるだけにした。止めた取得 (`stopDiff`) は結果を使わないので色付けを飛ばす

## 結果

- [x] 実測 (2026-11-08。`tfprobe` を直す前 / 直した後のバイナリで交互に 2 回ずつ、root=`src/`、5 サイクル、200×50、go1.26.0、load average 約 3.5)

  | `live-tile-diff` | frame_p95 | settle_ms | alloc_kb |
  |---|---|---|---|
  | 直す前 1 / 2 | 22.04 / 23.70ms | 103 / 105 | 12687 / 12625 |
  | 直した後 1 / 2 | 0.41 / 0.40ms | 82 / 83 | 12664 / 12733 |

  確保は goroutine へ移っただけで総量は変わらない。`tile-code` の 1 打 (p95 1.48〜1.65ms) は変わらない
- [x] 新テスト `TestDiffLinesArriveColored` (filer/tileview_test.go): 取得の結果に色が付いて届き、表示の行がそれを使う。変異「`styled = lines` (色付けを外す)」で
  6 行とも red を確認 (`mutate-verify`)
- [x] `go test -race` (treefiler 全体) rc=0
- [x] 敵対的レビュー (opus 1 体。並行性・長さの食い違い・旧実装との表示の差・テストの素通り): P1 / P2 なし。P3 の 2 件は対応しない:
  - P3-1 新テストは「描画で色付けしない」を固定しない (描画で同じ色付けをやり直しても出力が同じなので緑)。速さだけの退行は壁時計で固定しない方針
    (src/treefiler/CLAUDE.md の「実物で測る」) で、tfprobe の `live-tile-diff` の frame で見る。逆戻りの禁止は `startDiff` の 🚨 コメントに書いた
  - P3-2 `ctx.Err()` で色付けを飛ばす枝を通るテストが無い。外れても失うのは捨てる結果への CPU だけで、表示は変わらない
  - レビューの未確認: regexp2 と chroma の個別の lexer の同時実行の安全性はソースまで読んでいない (glogx・pro-con が同じ関数を UI の外で呼んでいる前例はある)

## 残タスク

- スコープ外: `Model.Close` は取得中の diff を止めない (前から。git は `subproc.GitOpTimeout` で終わる)。閉じた後に取得が終わると、捨てる結果に色付けが走る

