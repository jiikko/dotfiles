# 816 (perf): -trimpath で worktree をまたいで Go のビルドキャッシュを共有する

起票日: 2026-10-17

## 概要

手元の GOCACHE (`~/Library/Caches/go-build`) は 2026-10-17 に 54 GB あり、最大級の項目は glogx のテスト版のコンパイル結果
(1 つ 26 MB 前後) で、中に **worktree のパスが埋まっていた** (issue 742 の実測)。dotfiles では並行セッションごとに worktree を
作るので (`.claude/rules/worktree-per-session.md`)、同じソースでも worktree ごとに別の項目として溜まっている疑いがある。
`-trimpath` を付けるとパスがキャッシュの鍵から外れ、worktree をまたいで同じ項目を使い回せるはず。効けば、glogx を
package に割る (issue 742 / 817。正味 −1 割程度) よりずっと大きく効きうる。

## 根拠 (Go 1.26.0 のソース)

`$(go env GOROOT)/src/cmd/go/internal/work/exec.go` の `buildActionID` (コンパイルのキャッシュの鍵を作る関数):

- `-trimpath` が無く、GOROOT の外・作業 dir の外の package なら `fmt.Fprintf(h, "dir %s\n", p.Dir)` = **package のディレクトリの
  絶対パスを鍵に入れる** (コメント: "the object file may refer to the absolute directory containing the package")
- `-trimpath` があると dir の代わりに `module <path>@<version>` (module に属する package のとき) と `trimpath` を入れる

つまり付けない限り worktree ごとに鍵が違い、付けると鍵から場所が消える (付けた版と付けない版の鍵も別になる)。
**ここまではソースを読んだだけで、実際に共有されるかは未実測** (リンク・vet・テストのバイナリの側に別の場所依存が無いかも未確認)。

## 測ること (着手したら最初に)

1. 同じ commit の worktree を 2 つ作り、空の GOCACHE で、worktree A で `go test -race -count=1 -run '^$' ./...` (src/glogx)、
   続けて worktree B で同じものを回し、B の分の GOCACHE の増分を測る。`-trimpath` なし / あり (`GOFLAGS=-trimpath`) の 2 通り。
   期待: なしでは B も A とほぼ同じ量 (228 MB 級) 増え、ありではほとんど増えない
2. 1 行の変更 (issue 742 の「計測の手順」) の増分が `-trimpath` で変わらないこと (付けたことで悪化していないか)
3. 今の GOCACHE の中身で、worktree のパス別に何 GB あるか (項目の中の文字列で数える。数え方を決めるところから)

## 対応方針 (測ってから決める)

- 付ける場所の候補:
  - **利用者の go env** (`go env -w GOFLAGS=-trimpath`。`$(go env GOENV)` = `~/Library/Application Support/go/env` に書かれる):
    この機械の全部の go コマンド (他の repo・gopls を含む) に効く。dotfiles で管理するなら setup.sh か dotfile にする
  - repo の入口 (`src/*/Makefile`・root の Makefile の Go の target・`bin/lib/go_autobuild.zsh`) で `GOFLAGS` を付ける:
    dotfiles の中だけに効く。入口が多く、付け忘れた入口が付けない版の鍵で二重に溜める
- 既存の `GOFLAGS` の扱いと衝突しないか: `scripts/go_module_graph.sh` は `GOFLAGS=-mod=readonly` を**上書きで** export し、
  `scripts/go_packages_index.sh` は `GOFLAGS=''` で消している。go env に書く形だと、これらの上書きで `-trimpath` が外れ、
  その経路だけ付けない版になる (検査の go list なので量は小さいはず。未確認)
- CI: 毎回の新しい runner でキャッシュは actions/setup-go の cache 経由。付けると鍵が変わり、初回は全部作り直しになる。CI にも付けるか
  (付けないなら CI と手元で鍵が別でも害は無い) は測ってから決める

## 副作用 (受け入れるか着手時に決める)

- スタックトレース・`runtime.Caller` のパスが module 相対 (`glogx/tui.go:123`) になる。ファイルと行は読めるが、絶対パスで
  エディタに飛ぶ・デバッガ (dlv) でソースを対応づけるときは手間が増える。見たいときは、そのコマンドだけ `GOFLAGS=` で外す
  (外したビルドは付けない版の鍵で別に溜まる)
- `runtime.Caller` や埋め込まれたパスで判定するテストがあれば壊れる (glogx・他の module で grep してから付ける)

## 関連

- issue 742 — GOCACHE の増え方の実測 (54 GB・worktree のパスが埋まった 26 MB の項目・1 行の変更の増分)
- issue 817 — glogx の main を薄くする (分割の動機はキャッシュではなく設計)
