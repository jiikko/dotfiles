# 887 (bug): 消した worktree のパスが go のビルドキャッシュに残り、glogx の lint が手元で落ちる

起票日: 2026-10-23

## 概要

手元の `make lint` (src/glogx) が次の行で落ちる (rc=2)。`make test` の `test-go-lint` も同じ所で赤になる。

```
gofmt: unable to open file /Users/koji/wt-874/src/glogx/ime_tis_darwin.go: open …: no such file or directory
level=error msg="Running error: can't run linter goanalysis_metalinter …"
```

`/Users/koji/wt-874` は issue 874 の作業で使った worktree で、今は消えている。ほかのセッションが同じ checkout で lint を回しても同じ所で落ちる。
CI では落ちていない (2026-10-23 の 1310cbad の Lint・Tests の run は success)。CI も `actions/setup-go` の cache で GOCACHE を run をまたいで戻す
(`.github/workflows/_go-project.yml` の `cache-dependency-path`) が、checkout のパスが毎回同じなので `//line` が指す先が消えない、と推測している (未実測)。

## 詳細 (2026-10-23 の実測)

- 再現: worktree (`/Users/koji/dotfiles-wt-0e-877`) と本体の checkout (`~/dotfiles`) の両方の `src/glogx` で `make lint` を打つと、どちらも rc=2 で、上の行が出る
- 切り分け:
  - `GOLANGCI_LINT_CACHE` だけを新しい dir に替えても、同じ所で落ちる
  - `GOCACHE` と `GOLANGCI_LINT_CACHE` の両方を新しい dir に替えると、rc=0 で `0 issues.` になる
  - `GOCACHE` だけを替える試しはしていない
- 共有の GOCACHE (`go env GOCACHE` = `~/Library/Caches/go-build`) の中で `wt-874` を含むファイルは 30 個 (`grep -rl wt-874 … | wc -l`)。
  そのうち `wt-874/src/glogx/ime_tis_darwin.go` を含むのは 3 個で、1 個 (`35/3539a2c0…-d`、2862 バイト、更新 10/23 09:54) が
  `//line /Users/koji/wt-874/src/glogx/ime_tis_darwin.go:1:1` の行を持つ (cgo の生成物)。残り 27 個は cgo 以外の生成物 (テストのバイナリ等)
- `ime_tis_darwin.go` は `import "C"` (cgo) のファイル
- この機械の `go env GOFLAGS` は `-trimpath` (issue 816 の `scripts/go_env_trimpath.sh` が 2026-10-17 に入れた。worktree の間でキャッシュを共有させるため、
  鍵から package の絶対パスを外す)。上の cgo の生成物は -trimpath の下で作られたのに絶対パスの `//line` を持っている
- 仮説 (一部確認): cgo の生成物は -trimpath でも `//line` に絶対パスを残す。-trimpath で鍵から場所が外れたので、その生成物が別の checkout の同じ内容のソースにも
  使い回され、gofmt の linter が `//line` の先 (消えた worktree) を開きに行っている。`//line` の行があることは確認した。
  「816 (-trimpath) が無ければ起きない」と「gofmt の失敗がこのエントリだけで説明できる」は未確認

## 対応方針 (案。着手のときに決める)

- いま手元を直すだけなら、`wt-874` を含むエントリを消す。`go clean -cache` はキャッシュ全体を消すので、ほかのセッションのビルドが遅くなる
- 再発を止める候補。どれも効くかは未確認。直したら、worktree を作る → lint → worktree を消す → 別の checkout で lint、の順で再現が消えることを確かめる
  - lint (golangci-lint) を回すときだけ `GOFLAGS` から `-trimpath` を外す。外すと 816 の容量の節約を lint では失う
  - lint 用の GOCACHE を checkout ごとに分ける
  - cgo の生成物を共有させない手段が go 側にあるかを調べる
- 816 は worktree の間でキャッシュを共有させる変更なので、cgo のファイルを最初に lint した worktree を消すたびに再発しうる。816 にもこの副作用を 1 行書く

## 関連ファイル

- `src/glogx/ime_tis_darwin.go` (cgo)
- `scripts/golangci_lint.sh`・`src/glogx/Makefile` の lint target
- issue 874 (golangci の設定を合成する形に変えた作業。`wt-874` はその worktree)
- issue 816 (`-trimpath` で worktree の間のキャッシュを共有。`scripts/go_env_trimpath.sh`)
- issue 820 (done。worktree を消した後の古い staticcheck のキャッシュで unused の検査が落ちた。820 は「GOFLAGS を空にしても同じ。trimpath は原因ではない」と結論。
  こちらは cgo の生成物で、原因が同じかは未確認)

## 進捗

- [x] 反証レビュー (sonnet。2026-10-23) — 「`-trimpath` を付ける」を候補に挙げていたが既に適用済み (816) で、その下で再現している、を反映した。
  CI のキャッシュの記述、GOCACHE の 30 個の内訳、820 との関係も直した
- [ ] 仮説の確認: 816 (-trimpath) を外した GOCACHE で起きないか、gofmt の失敗がこの cgo のエントリだけで説明できるか
- [ ] 再発を止める形を決めて直す
