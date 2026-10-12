# subproc

外部プロセス実行の安全弁 (WaitDelay と git の timeout) を 1 箇所に置く module。使い方・設計の正本は README.md と `subproc.go` のパッケージ doc / 各定数の doc。

## 入口

- `CommandContext(ctx, name, args...) *exec.Cmd` — glogx / ratelimit では外部コマンドをこれで起こす (素の `exec.CommandContext` は使わない)。WaitDelay を張る
- `clipboard.Copy(text)` (`subproc/clipboard`) — 文字列を OS のクリップボードへ (pbcopy / xclip が本命、tmux 内は tmux バッファへもおまけ)。glogx と treefiler が使う。失敗モードはパッケージの doc
- git を起こすとき (`tests/scripts/test_git_calls_hardened.sh` が素の起動を止める):
  - `GitArgs` / `GitCommand` — 開いた repo の設定からコマンドを走らせない引数を付ける (fsmonitor・署名の表示・遅延 fetch など。issue 692・739)
  - `GitRepoArgs` / `GitRepoCommand` — 上に加えて repo のローカルの filter を止める。作業ツリーを表示のために読む status・diff 用 (issue 732)
  - `GitArgsLazyFetch` / `GitCommandLazyFetch` — 遅延 fetch だけ許す。push / pull などキー操作と自分の管理する repo 用。使う場所は hardened の検査の許可リスト
- `UnsetGitLocationEnv` / `IsGitLocationEnv` — 継承すると `git -C` の先ではなく別の repo を書かせる環境変数 (GIT_DIR など)。本物の git を起こすテストのパッケージは
  TestMain で外す (`tests/scripts/test_git_tests_unset_location_env.sh` が呼び忘れを止める。issue 740)。pro-con の gitx は production で同じ一覧を外す
- `WaitDelay` — 上記が張る値。`GitOpTimeout` — 呼び出し側が git の timeout に使う値 (glogx の `gitOpTimeout`)

## ビルド・テスト

- `make -C src/subproc lint` / `test`
- 張り忘れの検出は消費者側にある: glogx は `waitdelay_discipline_test.go`、ratelimit は `exec_boundary_test.go`
