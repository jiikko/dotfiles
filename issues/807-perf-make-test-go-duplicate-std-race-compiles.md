# 807 (perf): make test-go の一斉起動で、標準ライブラリの -race 版を 22 プロセスが重複してコンパイルする

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

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
  21〜22 プロセスが開始のずれ 0.5〜2 秒で各自コンパイル)。std 以外は 113 回 (`bubbletea` / `tuikit/toast` / `go/ast` 等)
- 普段の cache ではほぼ重複しない。普段の 80 秒の中身は link (テストバイナリはパッケージごとに別物なので毎回 88 回) とテストの実行
- 冷えた cache になるのは: Go の版を上げた直後 / GOCACHE の自動 trim (長く使われていない項目) の後 / cache を消した後

母集合: `src/*/go.mod` の 22 module の `go test -race ./...` だけ。golangci-lint (`test-go-lint`) と、各 Makefile の 2 本目の
`go test -count=1 -run ...` (glogx / treefiler / tuikit の -race なしの予算検査) は数えていない。

## 対応方針

`run_go_projects` の `test` の前に、**1 プロセスで `go build -race std` を済ませてから**一斉起動する。

- 先に std を作っておけば、その後の `go test -race` の std のコンパイルは 0 回になる
  (空の GOCACHE で `go build -race std` → `src/tuikit` で `go test -race -run '^$' -x ./...` の `-std` 付き compile 行 = 0 件。cache の鍵が一致する)
- 上乗せ: 冷えた cache で 7.4 秒 (wall)、普段の cache で 2.2 秒 (wall)
- 全 module が同じツールチェーンで動く前提 (`go` 行は全部 `go 1.25.0`、`toolchain` 行なし)。module ごとにツールチェーンが分かれると、
  ウォームアップした std は別の cache 鍵になり効かなくなる (害は無い)

### 採らなかった案

- **テストの依存全部を先にビルド** (`go list -deps -test` の非テストパッケージを module ごとに `go build -race`): 普段の cache で 20 秒の上乗せ
  (80 秒に対して 25%)。冷えた cache は稀なので、毎回払う額として割に合わない。std だけで重複の 88% を消せる
- **同時起動数に上限を付ける**: 重複は減るが消えない。普段の cache で効いている並列 (テストの実行) まで遅くなる

## 受け入れ条件

- [ ] `make test-go` が std の -race ウォームアップを 1 回だけ走らせてから一斉起動する
- [ ] ウォームアップの失敗を握り潰さない (失敗したら理由を出す。テストの一斉起動は止めない: ウォームアップは最適化で、正しさの判定ではない)
- [ ] go 未導入のときの skip (既存の挙動) を壊さない
- [ ] 冷えた cache での重複 compile 回数と wall を before / after で測り直し、本文に書く
- [ ] 普段の cache での wall の上乗せを測り、本文に書く

## 関連ファイル

- `Makefile` の `run_go_projects` / `test-go`
- `src/*/Makefile` の `test:`

## 進捗

- 2026-10-16 起票。計測ログは `tmp/go-build-timeline-2026-10-16/` (gitignore。結論は上の表に移した)
