# 1035 (research): src/treefiler の全観点の監査 (2026-11-05。直接実行)

起票日: 2026-11-05

## 概要

`/all-audit-on-diy treefiler`。対象は src/treefiler (`git ls-files src/treefiler` で 103 ファイル・22,222 行、`bfa26dc34` 時点)。
前回 ([1019](done/1019-research-treefiler-audit-2026-11-04.md)) の範囲の終わり `f47ecf97e` 以降の差分と、1019 が「読んでいない」と書いたファイルを重点にする。
直したのは 1 点 (README のキー表に `p` が無かった。ずれを止めるテストつき)、起票は 0 本。

## 走査した母集合

- 対象: `git ls-files src/treefiler` で 103 ファイル (`filer/` 82・`tools/` 4・`termsync/` 2・ほか直下)。
- 前回 (1019) からの差分: `git diff --stat f47ecf97e..1b9fa13c8 -- src/treefiler` で 14 ファイル (+574 / -122)。中身は ? の一覧の 2 段組 (`filer/help.go` 新規)、
  shebang での言語の判定 (`preview.go` の `firstLine`・`pane.go`・`tileview.go`)、1019 の修正 (`liveAncestorEdge`)、1020 の修正 (最初のカーソルを並びの後で決める)、
  tfperf の修正 (1022。敵対的レビュー 4 周済み)、lint の設定の共通化 (`.golangci.local.yml` から exhaustive・fatcontext・makezero を外す)。
- 全部を読んだ: `filer/help.go`・差分の全部 (tfperf は 1022 で 4 周済みなので差分の目通しだけ)・1019 が「読んでいない」と書いたファイルのうち
  `filer/tile.go`・`ask.go`・`debug.go`・`direntries.go`・`direntries_darwin.go`・`gorules/rules.go`・`filer/load.go`・`filer/model.go` の `New` 〜 `takeBackground` (`Close`・`Busy`・`Animating`・`Advance`)。
  `tools/tfprobe/main.go` は冒頭の約束 (本物を書き換えない台本・記録をメモリに溜めない) だけ。
- 機械の検査 (worktree、`1b9fa13c8` + 下の修正): `go vet ./...` rc=0 / `go test -race -count=1 ./...` は修正前の新しいテスト 1 本だけが赤で他は全部 ok /
  `make lint` 0 issues / `deadcode ./...`・`deadcode -test ./...` ともに 0 件 (`GOENV_VERSION=1.25.4`) / `bin/dupscan sweep src/treefiler` (単位 817・96 ファイル) / `make test-dupscan-gate` 新しい重複なし

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| ux | 1 (直した) | README の「キー (木)」の表に `p` (右のプレビューの欄の開閉。`59a9fbbc3` で足した) が無かった。spec §0.1 と ? の一覧には載っている。`treeKeys` の全キーを README と一覧に照らすテストを足した (`TestEveryTreeKeyIsListedInHelpAndReadme`)。一覧の新しい 2 段組は、各キーの表記を `treeKeys` (`model.go` の `init`) とタイルの `tileKey` に照らして一致。狭い端末で描かない下限 (幅 22 未満) は前の 1 列の版と同じ |
| broken-code | 0 | shebang: `firstLine` は読めている最初の行を返すので、読む前に呼ぶと空になりうる → `openText` が `sniffBytes` を必ず先に読み、プレビューの欄も `ensure` の後に呼ぶので届いている。`.golangci.local.yml` から外した 3 つは `src/.golangci.yml` へ寄せた (`823fd3861` が違反を 1 つずつ置いて 4 つとも報告されたと記録)。`parseBulkRecord` は大きさの欄を読む前に `l < fixed+8` を見る。名前の位置は `start >= fixed` で固定部と重ならない |
| duplication | 0 | sweep の節 2 の `init` の `o` / `O` の 2 行・settings の表・tfprobe の 1 呼び出しのクロージャ、節 3 の explode / diff / watch の lock 付きの小さい関数は、どれも 1019 / 992 と同じ判断 (表の行どうし・3 行ずつで寄せる価値が無い) |
| test-helpers | 0 | sweep の節 1・5・6 は「なし」 |
| concurrency | 0 | `load.go`: 止まったマウントで諦めた読み込みは `m.loads` に残り Busy を立て続けるが、`loadHot` のコメントに「過ぎたら Busy の遅い周期」と明記された意図した形。`loadMax` を超えたら同期で読む。`readEntriesVia` は頼んだ時点の `readBulk` を受け取る (goroutine の中で読まない) |
| resource-leaks | 0 | `readEntriesBulk` の fd は defer で閉じ、受け皿は `sync.Pool` に戻す。tile の diff は `stopDiff` が取り消す (差し替え・モードの切り替え) |
| error-handling | 0 | `replaceTile` は開けないファイル (バイナリ以外の読めないものも) を黙って飛ばすが、端まで行けば「これが最後のファイルです」が出る。`restoreTile` が開けないファイルを知らせずに飛ばすのは理由コメントつき (起動の直後に打っていない操作の失敗を出さない) |
| security | 0 | shebang の 1 行は言語を選ぶだけで実行しない。リンクの解決は `os.Stat` の結果を鍵 (root・base・字面) でキャッシュし、読み直しとファイル操作で `clear` する |
| performance | 0 | `helpColumns` は描くたびに節の割り方を総当たりするが、8 節で ? を開いている間だけ (見積もり。測っていない) |
| false-green | 0 | 新しいテストは件数の下限 (30) と「README の節が無い」「1 つも拾えない」を落ちる側にしている。tfperf / tfprobe は合否を返さない計測 |
| dead-code | 0 | `deadcode` / `deadcode -test` で 0 |
| ci | 0 | `src_treefiler.yml` の paths は go.mod の replace 7 本を全部覆う。新しいテストが読む `src/treefiler/README.md` も `src/treefiler/**` に入る |
| dependency | 0 | go.mod は 1019 から変わっていない |
| design / responsibility / polymorphism / leaky-abstraction / encapsulation / ui-components | 0 | ? の一覧は `model.go` から `help.go` へ出て、表 (`helpSections`) と組み方 (`helpColumns`) と描き方に分かれた。1019 から構造は変わっていない |
| test-cleanup | 0 | `help_test.go` の既存 5 本は節の見出し・1 段への戻り・割り方の最小・残りの数・閉じ方を別々に見ている |
| lint-from-done | 0 | 今回の README のずれは lint ではなくテストで止めた (上の ux) |
| general | 0 | 上に含めた |

全数: 直した 1 (ux)、起票 0、却下 0。

## 対応

- `docs(treefiler): README のキー表に p を足し、木のキーが ? の一覧と README に載っていることをテストで守る`
  - 変異: README を直す前は `p` だけで赤 (他の 30 余りのキーは通る)。`help.go` の一覧から `'` の行を消すと「`'` が ? のキー一覧に無い」で赤
  - 検出しない形: 載っていても説明が実際の動作と食い違うもの / タイルのキー (`tileKey`) の載せ漏れ

## 進捗

- 2026-11-05: 監査・対応
