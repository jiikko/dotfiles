# subproc

外部プロセス実行の安全弁 (WaitDelay と git の timeout) を 1 箇所に置く module。使い方・設計の正本は README.md と `subproc.go` のパッケージ doc / 各定数の doc。

## 入口

- `CommandContext(ctx, name, args...) *exec.Cmd` — glogx / ratelimit では外部コマンドをこれで起こす (素の `exec.CommandContext` は使わない)。WaitDelay を張る
- `WaitDelay` — 上記が張る値。`GitOpTimeout` — 呼び出し側が git の timeout に使う値 (glogx の `gitOpTimeout`)

## ビルド・テスト

- `make -C src/subproc lint` / `test`
- 張り忘れの検出は消費者側にある: glogx は `waitdelay_discipline_test.go`、ratelimit は `exec_boundary_test.go`
