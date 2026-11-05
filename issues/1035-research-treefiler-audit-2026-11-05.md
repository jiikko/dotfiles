# 1035 (research): src/treefiler の全観点の監査 (2026-11-05。直接実行)

起票日: 2026-11-05

## 概要

`/all-audit-on-diy treefiler`。対象は src/treefiler (`git ls-files src/treefiler` で 103 ファイル・22,222 行、`bfa26dc34` 時点)。
前回 ([1019](done/1019-research-treefiler-audit-2026-11-04.md)) の範囲の終わり `f47ecf97e` 以降の差分と、1019 が「読んでいない」と書いたファイルを重点にする。
直したのは 4 点 (README のキー表の `p`・`e` / `s` / `!` から戻ってもタイルの本文が古いまま・タイル側のリンクの解決が古いまま・木から外れたファイルの `J` / `K`)、
起票は 1 本 ([1037](1037-bug-treefiler-tile-stale-on-live-update-and-jump-rebuild.md))。後ろの 3 点は、記録への反証レビューが見落としとして出した。

## 走査した母集合

- 対象: `git ls-files src/treefiler` で 103 ファイル (`filer/` 82・`tools/` 4・`termsync/` 2・ほか直下)。
- 前回 (1019) からの差分: `git diff --stat f47ecf97e..1b9fa13c8 -- src/treefiler` で 14 ファイル (+574 / -122)。中身は ? の一覧の 2 段組 (`filer/help.go` 新規)、
  shebang での言語の判定 (`preview.go` の `firstLine`・`pane.go`・`tileview.go`)、1019 の修正 (`liveAncestorEdge`)、1020 の修正 (最初のカーソルを並びの後で決める)、
  tfperf の修正 (1022。敵対的レビュー 4 周済み)、lint の設定の共通化 (`.golangci.local.yml` から exhaustive・fatcontext・makezero を外す)。
- 全部を読んだ: `filer/help.go`・差分の全部 (tfperf は 1022 で 4 周済みなので差分の目通しだけ)・1019 が「読んでいない」と書いたファイルのうち
  `filer/tile.go`・`ask.go`・`debug.go`・`direntries.go`・`direntries_darwin.go`・`gorules/rules.go`・`filer/load.go`・`filer/model.go` の `New` 〜 `takeBackground` (`Close`・`Busy`・`Animating`・`Advance`)。
  `tools/tfprobe/main.go` は冒頭の約束 (本物を書き換えない台本・記録をメモリに溜めない) だけ。
- 機械の検査 (監査の始め、`1b9fa13c8`): `go vet ./...` rc=0 / `make lint` 0 issues / `deadcode ./...`・`deadcode -test ./...` ともに 0 件 (`GOENV_VERSION=1.25.4`) /
  `bin/dupscan sweep src/treefiler` (単位 817・96 ファイル)
- 機械の検査 (修正の後、`c48506d47` に載せた全 commit): treefiler の `go test -race -count=1 ./...` 全 package ok / `make lint` 0 issues /
  root の `make test` rc=0 / `make test-dupscan-gate` 新しい重複なし (変わった単位 16)

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| ux | 1 (直した) | README の「キー (木)」の表に `p` (右のプレビューの欄の開閉。`59a9fbbc3` で足した) が無かった。spec §0.1 と ? の一覧には載っている。`treeKeys` の全キーを README と一覧に照らすテストを足した (`TestEveryTreeKeyIsListedInHelpAndReadme`)。**? の一覧に無いキーは残る** (記録のみ): 移動の `f` `b` (半ページ)・終了の `^C`・タイルの `h` `←` `enter` (閉じる)・`shift+↓` `shift+↑`、`home` `end` `pgup` `pgdown` `shift+space` (README にも無い)。一覧は要約で README の表が全部を持つ (`f` `b` `^C` は README にある)。足すかは一覧の見た目の判断なので直さない。テストは `treeKeys` だけを見る (移動のキーは `listnav.MotionOf`、`ctrl+c` は `HandleInput` の入口で受け、`treeKeys` に無い)。狭い端末で描かない下限 (幅 22 未満) は前の 1 列の版と同じ |
| broken-code | 2 (直した) | **`e` / `s` / `!` から戻ってもタイルの本文が古いまま**: spec §0.1 の `e` は「戻ったら読み直す (`ExecDone`)」だが、`ExecDone` は `Refresh` (木だけ) を呼び、タイルの `textSource` は差し替えなかった。読み足しは古い読み位置から続くので、書き換えた後に先へ送ると行が崩れた → `rereadTiles`。**木から外れたファイルのタイルの `J` / `K`**: 起点が見つからず先頭から数え、`K` は「これが最初のファイルです」と嘘を言い、`J` は 2 番目へ飛んだ → 起点を `m.inTree` で確かめ、隠すものも含む全部の子の中で数える。ほか、shebang: `firstLine` は読めている最初の行を返すので、読む前に呼ぶと空になりうる → `openText` が `sniffBytes` を必ず先に読み、プレビューの欄も `ensure` の後に呼ぶので届いている。`.golangci.local.yml` から外した 3 つは `src/.golangci.yml` へ寄せた (`823fd3861` が違反を 1 つずつ置いて 4 つとも報告されたと記録)。`parseBulkRecord` は大きさの欄を読む前に `l < fixed+8` を見る。名前の位置の確かめ `start >= fixed` は長さの欄〜mtime とは重ならないことしか言わない (大きさの欄 56..64 と重なる壊れた記録は通す。kernel が正しい記録を返す限り起きないので直さない) |
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
| encapsulation | 1 (直した) | E2: リンクの解決を `m.linkCache` (字面ごと) とタイルの `t.links` (行ごと) の 2 段で覚えていて、`Refresh` と `reloadDir` は `linkCache` だけを捨てていた (タイルで一度 Tab を押した後に作ったファイルがリンクにならない) → `forgetLinks` に寄せ、迂回を ruleguard (`linkCacheViaForgetLinks`) で止める。ジャンプ中のタイルは抜けるまで据え置く (下の表) |
| design / responsibility / polymorphism / leaky-abstraction / ui-components | 0 | ? の一覧は `model.go` から `help.go` へ出て、表 (`helpSections`) と組み方 (`helpColumns`) と描き方に分かれた。1019 から構造は変わっていない |
| test-cleanup | 0 | `help_test.go` の既存 5 本は節の見出し・1 段への戻り・割り方の最小・残りの数・閉じ方を別々に見ている |
| lint-from-done | 0 | 今回の README のずれは lint ではなくテストで止めた (上の ux) |
| general | 0 | 上に含めた |

全数: 直した 4 (ux 1・broken-code 2・encapsulation 1)、起票 1 ([1037](1037-bug-treefiler-tile-stale-on-live-update-and-jump-rebuild.md)。変更前からの 2 件)、記録のみ 2 (ux の一覧に無いキー・`parseBulkRecord` の確かめの範囲)。

## タイルの「状態 × 出来事」の表 (修正の決め方)

指摘を 1 件ずつ直したら別のセルが壊れた (2 周目 → 3 周目で退行 4 件) ので、表を先に決めて直した。

| 出来事 \ タイル | 本体・変わっていない | 本体・変わった | 本体・途中で読めなくなった | diff | ジャンプ中 |
|---|---|---|---|---|---|
| `ExecDone` (`e` / `s` / `!` から戻る) | 開き直さない (大きさと mtime) | 開き直す。scroll・sy を保って上限で切る | 開き直す (chmod は大きさも mtime も変えない) | 本文を開けなくても必ず取り直す (git add は本文を変えない)。先頭へ戻る | 起きない (`e` はジャンプを抜けてから起こす) |
| `reloadDir` (ライブ更新・ファイル操作) | `linkCache` はいつも捨てる (項目の増減で絞らない: 同じ名前のまま種類が変わる)。ジャンプしていないタイルの `links` を捨てる | 同左 (本文は読み直さない = 1037) | 同左 | 同左 | `links` を据え置いて古い印 (`stale`) を付け、抜けた後の `tileLinks` で捨てる (選択を黙って移さない・次の enter / e の意味を変えない) |

## 対応 (どれも回帰テストで赤を見てから直し、変異で赤を確かめた。変異の中身は各 commit の message)

- `docs(treefiler): README のキー表に p を足し、木のキーが ? の一覧と README に載っていることをテストで守る`
- `test(treefiler): 木のキーの照合で、一覧の区切りの / を検索のキーと取り違えない・タイルの節を外し損ねたら止める`
- `fix(treefiler): エディタ・シェル・! から戻ったら開いているタイルの本文も読み直す`
- `fix(treefiler): フォルダを読み直したら、タイルが行ごとに覚えたリンクの解決も捨てる`
- `fix(treefiler): タイルのファイルが木から外れたら、J / K は隣を推測せず理由を知らせる`
- `fix(treefiler): タイルの読み直し・リンクの破棄・J/K の起点を、2 周目の敵対的レビューの指摘に合わせて直す`
- `fix(treefiler): タイルの読み直しとリンクの破棄を、状態 × 出来事の表で決め直す (3 周目の敵対的レビュー)`
- `fix(treefiler): 差分のタイルは本文を開けなくても取り直す・J/K の起点の判定を木に繋がっているかで取る (4 周目の敵対的レビュー)`

## 反証・敵対的レビュー (opus の読み取りのみのサブエージェント、各 1 本)

- 1 周目 (この記録と README のテストを反証): 採った 7 = 記録の誤り 2 (ux の行の「一覧は tileKey とも一致」・`parseBulkRecord` の言い過ぎ) /
  テストの穴 2 (区切りの `/` を検索のキーと取り違える・タイルの節を題の字面で外す) / 見落とし 3 (上の broken-code 2・encapsulation 1)。
  記録のみ 1 (一覧に無いキー。ux の行)。反証できなかった: 母集合・差分の件数・`firstLine`・`load.go` の Busy・CI の paths
- 2 周目 (修正 4 commit): 採った 5 (変わっていないファイルを毎回読み直す・縮んだときの sy・ジャンプの選択が移る・隠したファイルの `J` で偽の理由・テストの穴)
- 3 周目 (2 周目の修正): 退行 4 (diff を取り直さない・ジャンプを抜けて enter がタイルを閉じる・種類の変化でリンクが古い・chmod の後に開き直さない) → 上の表で決め直した
- 4 周目 (表と修正の全体): P1 なし。採った 3 (開けない diff の取り直し・親ごと外れたときの `J` / `K`・同じ大きさの書き換えのテスト)、
  表に書いた 1 (diff は取り直すと先頭へ戻る)、起票 2 (1037)。**4 周目の修正 (最後の commit) には 5 周目を回していない** (各修正は変異で赤を確かめた)

## 進捗

- 2026-11-05: 監査・対応
- 2026-11-06: 反証・敵対的レビュー 4 周の対応・1037 の起票
