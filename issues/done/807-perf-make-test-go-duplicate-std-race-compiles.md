# 807 (perf): make test-go の一斉起動で、標準ライブラリの -race 版を 22 プロセスが重複してコンパイルする

起票日: 2026-10-16

## 概要

root の `make test` (`test-src` → `test-go`) は、`Makefile` の `run_go_projects` で `src/*/go.mod` の 22 プロジェクトの
`make -C <dir> test` (= `go test -race ./...`) を一斉に background 起動する。Go の build cache は中身をキーにして全プロセスで共有されるが、
**プロセス間で「今ビルド中」を共有しない**ので、cache に無いパッケージを同時に必要としたプロセスはそれぞれ自分でコンパイルする。

## 実測 (2026-10-16、14 コア、Go 1.26.0、全 module `go 1.25.0`)

22 プロジェクトで `GOFLAGS="-mod=readonly -x" go test -race -count=1 ./...` を一斉に起動し、`-x` の各行に時刻とプロジェクト名を付けて記録した。
重複は compile 行の `-buildid` の action ID (中身から決まるキー) が同じものを数えた。

| 状態 | 所要 (wall) | compile 回数 | 異なる中身 | 重複して作った回数 | link 回数 |
|---|---|---|---|---|---|
| 冷えた cache (空の GOCACHE) | 110 秒 | 1523 | 581 種 | 942 (62%) | 88 |
| 普段の cache | 80 秒 | 21 | 19 種 | 2 | 88 |

- 冷えた cache の重複 942 回のうち **829 回 (88%) が標準ライブラリ** (`runtime` / `os` / `syscall` / `reflect` / `testing` / `encoding/json` 等を
  21〜22 プロセスが開始のずれ 0.5〜2 秒で各自コンパイル)。std 以外は 113 回 (`bubbletea` / `tuikit/toast` / `regexp2` / `chroma` 等。`go/ast` 等の `go/*` は `-std` 付きで std 側に数えている)
- 普段の cache ではほぼ重複しない。普段の 80 秒の中身は link (テストバイナリはパッケージごとに別物なので毎回 88 回) とテストの実行
- 冷えた cache になるのは: Go の版を上げた直後 / GOCACHE の自動 trim (長く使われていない項目) の後 / cache を消した後

母集合: `src/*/go.mod` の 22 module の `go test -race ./...` だけ。golangci-lint (`test-go-lint`) と、各 Makefile の 2 本目の
`go test -count=1 -run ...` (glogx / treefiler / tuikit の -race なしの予算検査) は数えていない。

## 対応方針

`test-go` の一斉起動の前に、**1 プロセスで `go build -race std` を済ませる**。

- 先に std を作っておけば、その後の `go test -race` の std のコンパイルは 0 回になる
  (空の GOCACHE で `go build -race std` → `src/tuikit` で `go test -race -run '^$' -x ./...` の `-std` 付き compile 行 = 0 件。cache の鍵が一致する)
- 上乗せ: 冷えた cache で 7〜10 秒 (wall)、普段の cache で 0.43 秒 (wall。3 回とも同じ。起票時に書いた 2.2 秒は負荷の中で測った値で再現しなかった)
- 全 module が同じツールチェーンで動く前提 (`go` 行は全部 `go 1.25.0`、`toolchain` 行なし)。module ごとにツールチェーンが分かれると、
  ウォームアップした std は別の cache 鍵になり効かなくなる (害は無い)

### 採らなかった案

- **テストの依存全部を先にビルド** (`go list -deps -test` の非テストパッケージを module ごとに `go build -race`): 普段の cache で 20 秒の上乗せ
  (80 秒に対して 25%)。冷えた cache は稀なので、毎回払う額として割に合わない。std だけで重複の 88% を消せる
- **同時起動数に上限を付ける**: 重複は減るが消えない。普段の cache で効いている並列 (テストの実行) まで遅くなる

## 受け入れ条件

- [x] `make test-go` が std の -race ウォームアップを 1 回だけ走らせてから一斉起動する
- [x] ウォームアップの失敗を握り潰さない (失敗したら理由を出す。テストの一斉起動は止めない: ウォームアップは最適化で、正しさの判定ではない)
- [x] go 未導入のときの skip (既存の挙動) を壊さない
- [x] 冷えた cache での重複 compile 回数と wall を before / after で測り直し、本文に書く
- [x] 普段の cache での wall の上乗せを測り、本文に書く

## 関連ファイル

- `Makefile` の `run_go_projects` / `test-go`
- `src/*/Makefile` の `test:`

## 進捗

- 2026-10-16 起票。計測ログは `tmp/go-build-timeline-2026-10-16/` (gitignore。結論は上の表に移した。2026-10-17 に `tmp/807-measure-2026-10-16/`・`tmp/go-build-dup*-2026-10-16/` と一緒に削除)
- 2026-10-17 実装。`Makefile` の `go_test_warm_std` を `test-go` の先頭の (`+` の付かない) レシピ行で走らせる。
  `tests/scripts/test_no_failfast_entrypoints.sh` のスタブの go が引数を記録し、test-go では一斉起動より前に `go build -race std` を 1 回だけ /
  test-go-lint では呼ばない / `make -n test-go` では表示だけで呼ばない / 失敗を出して一斉起動へ進む、を assert する

## 結果

`make test-go` (GOFLAGS=-mod=readonly、22 プロジェクト、14 コア、Go 1.26.0)。冷えた cache は毎回新しい空の GOCACHE。

| | 変更前 | 変更後 |
|---|---|---|
| compile 回数 (冷えた cache、`-x` で計数) | 1394 | 875 (うち 354 はウォームアップ自身) |
| 重複して作った回数 (同じ action ID の 2 回目以降) | 770 | 127 |
| ウォームアップ後にテスト側が作った std | — | 13 (テスト実行にだけ使う `testing/internal/testdeps` / `runtime/pprof` / `go/*` 等。各 1 回で重複なし) |
| CPU 時間 user+sys (冷えた cache、`/usr/bin/time`) | 833 秒 | 609 秒 (-27%) |
| wall (冷えた cache) | 111 秒 / 139 秒 (2 回) | 108 秒 / 113 秒 (2 回) |
| ウォームアップの上乗せ (普段の cache) | — | 0.43 秒 |

- 効くのは冷えた cache のときの **CPU 時間** (全コアを食う時間) で、wall はテストの実行 (`lanmgr` 81 秒など) が律速なので 1〜2 割未満しか縮まない
- 普段の cache の wall の前後比較は取れていない: worktree はソースの置き場所が違うので自前のパッケージの action ID が `~/dotfiles` と別になり、
  条件がそろわない (変更前 118 秒 / 変更後 85 秒と出たが比較に使わない)。上乗せはウォームアップ単体の 0.43 秒で評価した

## レビュー

- 方針 (fable、反証): P2 1 件・P3 3 件
  - 採用: ウォームアップを root ではなく module の中で走らせる (GOTOOLCHAIN=auto で手元の go が go 行より古いと root と module でツールチェーンが分かれる) /
    上乗せの数字を測り直す / `go/ast` の分類の誤記 / lint では走らせない
  - 反証できなかった: cache の鍵の一致 (2 module で std の compile 0 件) / 失敗で止めない設計 / 一斉起動は `run_go_projects` だけ
    (`scripts/test_changed.sh` は直列、CI は 1 job 1 module)
- 実装 (opus、敵対的) 1 周目: P2 1 件・P3 2 件。全部対応
  - P2: `test-go` のレシピは `+` 付きで `make -n` でもシェルが走り、ウォームアップが本当にビルドした → `+` の付かない別のレシピ行へ移した。
    MAKEFLAGS から -n を判定する案は捨てた: make 3.81 は上書きだけのとき MAKEFLAGS が `V=…` から始まり定番の `$(firstword -$(MAKEFLAGS))` が値の中の n を拾う /
    長いオプションがあると ` --x -n -- …` になり先頭の語では -n を取りこぼす (どちらも実測)
  - P3: テストが順序・対象を固定していない → スタブの go に引数を記録させて assert / スタブのコメントの前提を直した
- 実装 (opus、敵対的) 2 周目: 壊せなかった。P3 2 件 (`make -n` の assert が test-go まで届いたかを見ていない / スタブの stderr の文言) を対応
- 変異 (全部 revert 済み): ウォームアップの失敗で中断させる / 失敗の文言を消す / ウォームアップを一斉起動の後ろへ移す /
  ウォームアップの行に `+` を付ける / lint にもウォームアップを足す → いずれも `tests/scripts/test_no_failfast_entrypoints.sh` が赤

## 残タスク

- なし (日常の負荷の本体は別の話: `test-runtime` の直列の腕 (tmux / nvim、約 170 秒を 1〜2 コアで回す) と、
  `lanmgr` / `treefiler/filer` / `glogx` の長いテスト。この issue のスコープ外で、起票はしていない)
