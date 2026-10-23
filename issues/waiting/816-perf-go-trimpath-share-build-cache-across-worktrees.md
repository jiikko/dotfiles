# 816 (perf): -trimpath で worktree をまたいで Go のビルドキャッシュを共有する

起票日: 2026-10-17

> **waiting**: 入れてから 1 週間後 (2026-10-24 ごろ) の GOCACHE の大きさを待つ。来たら「進捗」の最後の観測で効き目を判定して閉じる。

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

つまり付けない限り worktree ごとに鍵が違い、付けると鍵から場所が消える (付けた版と付けない版の鍵も別になる)。標準ライブラリと
module cache の中の依存は元から場所に依存しないので、worktree ごとに重複するのは repo の中の package (とそれに依存する側) だけ。
ファイルの中身と依存のハッシュは鍵に残るので、**違う版のソースを別々にビルドする分は共有されない** (同じ版を複数の worktree で
ビルドする分だけが減る)。

## 測ること (着手したら最初に)

1. 同じ commit の worktree を 2 つ作り、空の GOCACHE で、worktree A で `go test -race -count=1 -run '^$' ./...` (src/glogx)、
   続けて worktree B で同じものを回し、B の分の GOCACHE の増分を測る。`-trimpath` なし / あり (`GOFLAGS=-trimpath`) の 2 通り。
   期待: なしでは B で repo の中の package の分だけ増え、ありではほとんど増えない (228 MB は標準ライブラリと依存を含む初回の総量なので、B の期待値ではない)
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
- CI: 毎回の新しい runner で worktree の重複は起きないので、付ける利得は無い。付けると Go の中の鍵は変わるが、`.github/workflows/_go-project.yml` の
  キャッシュの保存の鍵は go.sum から作っていて、同じ鍵では保存し直さない。既存のキャッシュに当たると trimpath 版を毎回作っても
  次回へ保存されない。**CI には付けない** (付けるなら保存の鍵も変える)

## 副作用 (受け入れるか着手時に決める)

- スタックトレース・`runtime.Caller` のパスが module 相対 (`glogx/tui.go:123`) になる。ファイルと行は読めるが、絶対パスで
  エディタに飛ぶ・デバッガ (dlv) でソースを対応づけるときは手間が増える。見たいときは、そのコマンドだけ `-trimpath=false` を付ける
  (`go test -trimpath=false ...`)。🚨 `GOFLAGS=` (空) では外れない: 環境変数が空なら go env のファイルの値に戻る
  (`cmd/go/internal/cfg/cfg.go` の Getenv)。外したビルドは付けない版の鍵で別に溜まる
- `runtime.Caller` のファイル名から repo を辿るテストは壊れる。2026-10-17 に repo 全体を grep して 2 本:
  `src/glogx/open_workspace_test.go` の repoRootOracle と `src/ratelimit/usage/pace_drift_test.go` の statuslinePath。
  どちらも `GOFLAGS=-trimpath` で赤になることを確かめた (計測案の `-run '^$'` では見えない)
- 環境変数 GOFLAGS を空でない値で上書きする経路 (`scripts/go_module_graph.sh` の `GOFLAGS=-mod=readonly`) は go env の値を
  まるごと置き換えるので、その経路だけ付けない版になる (go list なので量は小さい)。`GOENV=off` で go env を読まない経路
  (`scripts/go_packages_index.sh`) も同じ

## 関連

- issue 887 (2026-10-23): -trimpath の下でも cgo の生成物は `//line` に絶対パスを残し、それが worktree の間で使い回されて、worktree を消した後の glogx の lint が落ちる (副作用の候補。未確認の部分は 887 に)

- issue 742 — GOCACHE の増え方の実測 (54 GB・worktree のパスが埋まった 26 MB の項目・1 行の変更の増分)
- issue 817 — glogx の main を薄くする (分割の動機はキャッシュではなく設計)。2026-10-18 に完了 (この issue の測り方には影響しない。継続)

## 反証レビュー (codex 1 本。2026-10-17)

P2 4 件を採用して直した: runtime.Caller に頼るテスト 2 本が実際に壊れる・`GOFLAGS=` では go env の値を外せない (`-trimpath=false` で外す)・B の期待値 (228 MB は初回の総量)・CI のキャッシュの保存の鍵。コンパイルの鍵の引用と、同じ入力を worktree 間で共有する中核の仮説は反証できなかった。

## 進捗

- [x] 2026-10-17: 計測 1 (同じ commit の worktree 2 つ。空の GOCACHE で src/glogx の `go test -race -count=1 -run '^$' ./...`。
  `tmp/816-trimpath-2026-10-17/measure-share.sh` で回し、結果をここに移した):

  | | A (1 つ目) | B (2 つ目) | A をもう一度 |
  |---|---|---|---|
  | -trimpath なし | 235.5 MB | **+54.7 MB** | +0.5 MB |
  | -trimpath あり | 235.3 MB | **+0.5 MB** | +0.5 MB |

  標準ライブラリと依存はもともと共有されていて、worktree ごとに重複していたのは repo の中の package (glogx の module・テスト版を含む) の分。
  -trimpath でそれが共有される。計測 2 (1 行の変更の増分が悪化しないか)・3 (今の GOCACHE の worktree 別の内訳) は未実施
- [x] 2026-10-17: 付ける場所は**この機械の Go 全体** (go env のファイル) に決めた (ユーザー判断)。setup.sh から `scripts/go_env_trimpath.sh` を呼ぶ
  (setup: go env に GOFLAGS=-trimpath を足し、… (issue 816) / setup: -trimpath を足す処理を scripts/go_env_trimpath.sh へ出し、…)。
  - 規則: -trimpath がどの形 (=false を含む) でも既にあれば触らない・GOFLAGS は cmd/internal/quoted.Split と同じ規則で語に割る・
    読むときは GOFLAGS='' でファイルの値を読む。単体テスト tests/scripts/test_go_env_trimpath.sh (GOENV を一時ファイルへ。7 件 + 冪等)
  - runtime.Caller から repo を辿っていたテスト 2 本 (glogx の repoRootOracle・ratelimit の statuslinePath) を cwd 起点に直した。
    TestOpenEditorAtRoot は期待値を処理の前に固定する。-trimpath あり/なしの両方で緑
  - CI には付けない (上の「対応方針」)
  - 変異: setup.sh の段を消す / 毎回足す / =false の判定を外す / 引用符を無視して割る / オラクルの cmd.Dir を "/" にする、の 5 本で red を確認
- [x] 2026-10-17: **事故と直し**: 変異の検証で tests/setup/test_setup.sh を 2 本並列に回したら、setup.sh の anyenv の段が
  実ユーザーの goenv の version ファイルに同時に書き、「1.26.0」が 2 行になって go が起動しなくなった
  (`go: cannot find GOROOT directory: …/1.26.0:…/1.26.0`)。手で 1 行に戻し、setup.sh に `DOTFILES_SKIP_ANYENV_GLOBAL` を設けて
  tests/setup で立てた (テストの前後で version の更新時刻が変わらないことを確認)。tests/setup は HOME を差し替えても `*env global`
  (GOENV_ROOT の下に書く) と ~/dotfiles の git config (core.hooksPath。同じ値) を隔離できていなかった
- codex の敵対的レビュー: 本文 (P2 4 件採用、上の「反証レビュー」)・実装 1 周目 (P1 1・P2 3・P3 3。P1 と P2 を直した。P3 は下の記録)
  - 記録 (直さない): statuslinePath は別の checkout の package ディレクトリから実行すると、その checkout の shell と比べてしまう
    (通常の go test では起きない)・autobuild (`bin/lib/go_autobuild.zsh`) と保存済みの golangci-lint の指紋に GOFLAGS が入らず、
    -trimpath を入れても既存のバイナリはそのまま使われる (動作は同じでパスの表示だけが違うので害は無い)・`go version -m` から
    ldflags・CGO のフラグが消える (load/pkg.go)
- [x] 実装 2 周目の敵対的レビュー (直した差分に): P1 / P2 なし。P3 2 件を直した (単体テストの 2 回目の rc と読み戻しの成否・
  TestOpenEditorAtRoot で処理の前後に cwd が動かないこと。後者は open_workspace.go に os.Chdir("/") を足す変異で red を確認)
- [x] 2026-10-17: push して (tests: go_env_trimpath の 2 回目の rc と読み戻しの成否も見る・…)、この機械の go env に書いた。
  `GOFLAGS='' go env GOFLAGS` = `-trimpath` (go env のファイル `~/Library/Application Support/go/env` に `GOFLAGS=-trimpath`)。CI は緑
- [x] 2026-10-17: 計測 2 (1 行の変更の増分が悪化しないか。tui.go の先頭に 1 行・`./...`・空の GOCACHE): -trimpath=false で +23.0 MB、
  -trimpath で +23.0 MB (22,976 KB / 22,956 KB)。悪化していない
- 計測 3 (今の GOCACHE の worktree 別の内訳) はやめる: 2026-10-17 の GOCACHE は 2.6 GB まで減っていて (5 日の自動削除か掃除)、
  入れる前の内訳を測っても効き目の判定に使えない。代わりに下の観測で、入れた後の大きさを 54 GB (742 の実測) と比べる
- [ ] **観測 (waiting)**: 2026-10-24 ごろ (入れてから 1 週間、並行セッションの worktree が何本も作られた後) に `du -sh $(go env GOCACHE)` を見る。
  入れる前は 54 GB (2026-10-17 の 742 の実測。今日 15 GB・昨日 23 GB の項目)。数 GB に留まっていれば効いている。
  大きければ、項目の中の worktree のパスの有無を見て、-trimpath を通らない入口 (GOFLAGS を上書きする経路・GOENV=off) が無いかを探す
