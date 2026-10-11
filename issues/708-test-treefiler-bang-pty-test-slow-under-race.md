# 708 (test): treefiler の `!` の pty テストが -race で約 42 秒かかる

起票日: 2026-10-11

## 概要

commit「fix(treefiler): ! の後に端末の前面が戻らず treefiler / glogx の F が固まるのを直す」で足した
`src/treefiler/filer/shell_pty_test.go` の `TestBangGivesTerminalBackOnPTY` (22 ケース) が、`make -C src/treefiler test`
(`go test -race ./...`) の下で約 42 秒かかる。`treefiler/filer` package の `go test -race -count=1` は、このテストを除くと 5.06 秒、
含めると 45.63 秒 (2026-10-11、下と同じ条件)。package の所要のほぼ 9 割がこのテスト。

あわせて、このテストでは守れていない並び (下の「守れていない不変条件」) を記録する。

## 実測 (2026-10-11, Apple M3 Max 14 コア, go1.26.0, `go test -count=1 -v -run TestBangGivesTerminalBackOnPTY ./filer/`)

| | 合計 | 1 ケース |
|---|---|---|
| `-race` あり | 41.55 秒 | leader の形 1.12〜1.19 秒、job の形 2.21〜2.28 秒、slow 5.25 秒 |
| `-race` なし | 4.50 秒 | (未記録) |

- job の形は helper を 2 段 (session の先頭 → 前景ジョブ) 起こすので、leader のほぼ 2 倍。1 ケースの時間は helper の起動の回数に
  比例して見える。**helper 1 回の起動に約 1.1 秒かかっているという読みは推定** (内訳は未計測。-race で計装した
  テストバイナリの起動・`TestMain` の準備・pty の受け渡しのどれが主かは分けて測っていない)
- サブテストは `t.Setenv` (`SHELL` / `HOME` / `ZDOTDIR` など) を使うので `t.Parallel` にできず、直列に走る
- hang guard (`waitFor` の 10 秒) に対して、いちばん長い slow ケース (5.25 秒) の余裕は約 1.9 倍。CI の runner (macos-15) が遅いと縮む (未計測)

## 対応方針 (案。どれも未着手・未計測)

- まず内訳を測る (helper の起動だけを空で回す・`TestMain` を通さない起動と比べる)。測る前に直さない
- 並列化: `t.Setenv` をやめ、`ExecRequest.Env` を組み立てた後に `SHELL` などを書き換える形にして `t.Parallel` にする
  (`userShell()` は `TakeExec` の時点の `$SHELL` を読むので、Argv の中のシェルも合わせて差し替える必要がある)
- helper を -race で計装しない別のバイナリにする (テストの中で `go build` すると、その時間が増える)
- ケースを減らす: 変異で red になった役割ごとに 1 ケースを残す (2026-10-11 の変異の記録は上の commit message にある)

## 守れていない不変条件

- キー待ちは「端末を `-icanon -echo -isig` にしてから案内を出す」順にしている (案内を見てすぐ押した Ctrl-\ が、設定の前に届いて
  SIGQUIT にならないように)。**stty を案内の後へ戻す変異は、このテストで緑のまま通る** (キーを送る時刻と stty の競合に依存する)。
  守るなら、テストが案内を見てから送るのではなく、送る時刻を stty の直前に固定できる観測点 (stty の前に印を出す等) が要る

## 関連ファイル

- `src/treefiler/filer/shell_pty_test.go` / `src/treefiler/filer/shell.go` (`waitIfQuick`)
- `src/treefiler/Makefile` (`test` が `go test -race ./...`)
- issue 707 (Go 側で前面を取り返す。入れると、このテストに足すケースが増える)

## 進捗

- 2026-10-11: 起票
