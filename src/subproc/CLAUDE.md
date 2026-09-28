# subproc

外部プロセス実行の安全弁 (WaitDelay と git の timeout) を 1 箇所に置く module。使い方・設計の正本は README.md と `subproc.go` のパッケージ doc / 各定数の doc。

## 入口

- `CommandContext(ctx, name, args...) *exec.Cmd` — 新しい外部コマンド実行はこれを使う (素の `exec.CommandContext` は使わない)
- `WaitDelay` / `GitOpTimeout` — 上記が使う定数。単体で参照することもある

## ビルド・テスト

- `make -C src/subproc lint` / `test`
- 張り忘れの検出は消費者側にある: glogx は `waitdelay_discipline_test.go`、ratelimit は `exec_boundary_test.go`
