# subproc

外部プロセス実行の安全弁 (WaitDelay と git の timeout) を 1 箇所に置く module。glogx と ratelimit が
replace で取り込む。**新しい外部コマンド実行は `subproc.CommandContext` を使う** (理由と経緯は subproc.go の doc)。

- クリップボードへの書き込みは `subproc/clipboard` の `Copy` (glogx と treefiler の両方が使う。WaitDelay が要る stdin のパイプを持つので subproc に置いた。issue 718)
- 張り忘れの検出は消費者側にある: glogx は `waitdelay_discipline_test.go`、ratelimit は `exec_boundary_test.go`
