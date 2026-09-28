# process_supervisor

子プロセスを 1 つ起こして見張るワンショット supervisor (foreman / supervisord の 1 本ぶん)。使い方・設計・失敗モードの正本は README.md と `supervisor.go` のパッケージ doc。

## ファイルの地図

- `supervisor.go` — 実装の全部。`Spec` (呼び出し側が渡す関数群)・`Result` と、`Run` / `Start` / `ExitCode`

## 入口

- `Run(ctx, Spec) Result` — 同期実行 (呼び出しが結果を待つ)
- `Start(ctx, Spec) (stop func() Result)` — 裏で回す。`stop` は何度呼んでもよい
- `ExitCode(err) int` — `Spec.Classify` から呼ぶヘルパー
- 消費者: `src/pro-con` の `supervise.go` (dispatcher を子に持つ) と `monitorsup.go`

## ビルド・テスト

- `make -C src/process_supervisor lint` / `test`

## 詳しくは

- README.md の「すること / しないこと」表 (`Spec` の各関数の責務分担) と「気をつけること」節 (生命線の CLOEXEC 罠・プロセスグループへ送らない・`CrashLimit` の意味)
