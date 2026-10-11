# 708 (test): treefiler の `!` の pty テストが -race で約 42 秒かかる

起票日: 2026-10-11

## 概要

commit「fix(treefiler): ! の後に端末の前面が戻らず treefiler / glogx の F が固まるのを直す」で足した
`src/treefiler/filer/shell_pty_test.go` の `TestBangGivesTerminalBackOnPTY` (22 ケース) が、`make -C src/treefiler test`
(`go test -race ./...`) の下で約 42 秒かかる。`treefiler/filer` package の `go test -race -count=1` は、このテストを除くと 5.06 秒、
含めると 45.63 秒 (2026-10-11、下と同じ条件)。package の所要のほぼ 9 割がこのテスト。

あわせて、このテストでは守れていない並び (下の「守れていない不変条件」) を記録する。

## 原因 (codex の反証レビューの指摘を実測で確認)

race detector は、計装したバイナリが終わる前に既定で 1 秒待つ (`GORACE` の `atexit_sleep_ms`、既定 1000。
[Go の race detector の資料](https://go.dev/doc/articles/race_detector))。helper (`TestBangHelperOnPTY`) は -race で計装した
テストバイナリそのもので、`os.Exit(0)` で終わるたびに 1 秒待つ。helper の終了は leader の形で 1 回、job の形で 2 回
(session の先頭 → 前景ジョブの 2 段) なので、zsh がある構成では 10 + 12 × 2 = 34 回、約 34 秒になる。

## 実測 (2026-10-11, Apple M3 Max 14 コア, go1.26.0, `go test -count=1 -v -run TestBangGivesTerminalBackOnPTY ./filer/`)

| | 合計 | 1 ケース |
|---|---|---|
| `-race` | 41.55 秒 | leader の形 1.12〜1.19 秒、job の形 2.21〜2.28 秒、slow 5.25 秒 |
| `-race` + `GORACE=atexit_sleep_ms=0` | 6.79 秒 | leader/sh/external 0.11 秒、job/sh/external 0.19 秒、job/sh/slow 3.21 秒 |
| `-race` なし | 4.50 秒 | (未記録) |

起票時の版には「helper の起動に約 1.1 秒かかっている (推定)」と書いていたが、誤り。1 ケースの時間が helper の数に比例して
見えたのは、終了時の待ちが helper ごとに 1 回ずつ入るため。

## 対応方針

- helper を起こすとき (`runBangOnPTY` の `cmd.Env` と、job の形で helper がもう 1 段起こすときの `c.Env`) に
  `GORACE=atexit_sleep_ms=0` を足す。上の実測では親の環境で渡して 41.55 秒 → 6.79 秒。helper の終了時の待ちは、
  race の報告を出し切るためのもので、helper の中で race が起きたときの報告が消えないかを足す前に確かめる
- これで足りなければ、次を検討する (どれも未計測):
  - 並列化: サブテストは `t.Setenv` (`SHELL` / `HOME` / `ZDOTDIR` など) を使うので `t.Parallel` にできない。`newTest` → `fixture` の
    `t.Setenv("TREEFILER_CONFIG_DIR", …)` (`filer_test.go`) も同じ。親プロセスの Model もこの設定の置き場所を読むので、
    子の `ExecRequest.Env` を書き換えるだけでは代替できない。`userShell()` は Enter で `ExecRequest` を作る時点の `$SHELL` を
    読むので、Argv の中のシェルも合わせて差し替える必要がある
  - ケースを減らす: 変異で red になった役割ごとに 1 ケースを残す (2026-10-11 の変異の記録は上の commit message にある)
- hang guard: `waitFor` は呼ぶたびに 10 秒の期限を作り、テストは「案内」「READY」「helper の終了」を別々に待つ。ケース全体の
  所要 (slow の 5.25 秒) と 10 秒を比べても余裕にはならない。待ちの区間ごとの所要は未計測

## 守れていない不変条件

- キー待ちは「端末を `-icanon -echo -isig` にしてから案内を出す」順にしている (案内を見てすぐ押した Ctrl-\ が、設定の前に届いて
  SIGQUIT にならないように)。**stty を案内の後へ戻す変異は、このテストで緑のまま通る** (キーを送る時刻と stty の競合に依存する)。
  守るなら、テストが案内を見てから送るのではなく、送る時刻を stty の直前に固定できる観測点 (stty の前に印を出す等) が要る

## 関連ファイル

- `src/treefiler/filer/shell_pty_test.go` / `src/treefiler/filer/shell.go` (`waitIfQuick`)
- `src/treefiler/filer/filer_test.go` (`fixture` の `t.Setenv`) / `src/treefiler/filer/helpers_test.go` (`waitFor`)
- `src/treefiler/Makefile` (`test` が `go test -race ./...`)
- issue 707 (Go 側で前面を取り返す。入れると、このテストに足すケースが増える)

## 進捗

- 2026-10-11: 起票
- 2026-10-11: codex の反証レビュー。採用: 遅さの原因は race の終了時の待ち (P2。`GORACE=atexit_sleep_ms=0` で 41.55 → 6.79 秒を実測) /
  並列化には fixture の `t.Setenv` も障害になる (P2) / `userShell()` を読む時点は Enter (P3) / hang guard の余裕の比較は区間が違う (P3)。
  反証できなかった主張: 42 秒の測定値・22 ケース・`t.Setenv` による並列化の制限・stty の順序の変異が緑になる記録
- 2026-10-11: 対応 (commit「test(treefiler): ! の pty テストの helper に GORACE=atexit_sleep_ms=0 を渡し、-race の所要を 42 秒から 7 秒にする」)。
  `runBangOnPTY` の `cmd.Env` に足した 1 か所だけ (job の形の 2 段目は `os.Environ()` を受け継ぐので届く)。
  race の報告が消えないかの確認: helper の終了コードはもともと見ていない (`cmd.Wait()` の結果を捨てる) ので、helper の中の race は
  待ちの有無に関係なく出力にしか出ない。待ちを外して失うものは無い
  - [x] 実測 (同じマシン、`-race -count=1`): `-v -run TestBangGivesTerminalBackOnPTY` で 22 本が PASS、合計 6.64 秒
    (leader/sh/external 0.12 秒、job/sh/slow 3.21 秒)。`make -C src/treefiler test` で `treefiler/filer` が 11.6 秒 (起票時 45.63 秒)
  - 並列化・ケースを減らす案は採らない (足りたため)
  - 「守れていない不変条件」(stty を案内の後へ戻す変異が緑) は未対応のまま。上の節と、commit「fix(treefiler): ! の後に端末の前面が戻らず…」の message に記録がある。
    守るには包みに観測用の印を足す必要があり、利用者の画面に出る出力を増やすことになるので採らない
