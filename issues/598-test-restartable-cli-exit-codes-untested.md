# 598 (test): restartable の status / restart の終了コードをテストしていない (obaket の dev-restart が rc 2 に依存している)

> 🚨 **担当中: dotfiles-58**（2026-10-01〜）

起票日: 2026-10-01

出典: restartable の audit (codex のコードスキャン、2026-10-01。[601](601-research-restartable-audit-2026-10-01.md))。Claude がコードで裏を取った。

## 問題

`src/restartable/main.go` の `controlCommand` は応答を終了コードに写す (socket が無い = 2、要求の失敗 / ok:false = 1、時間切れ = 124)。
テストは `main_test.go` の SIGPIPE の 2 本だけで、この写しを検査するものが無い (`control.Call` を直接叩くテストはあるが、CLI の rc は通らない)。
`!response.OK` の判定を外しても、今のテストは赤くならない見込み (変異は未確認)。

## 発火条件

写しの退行 (例: ok:false を rc 0 にする、socket が無いのを rc 1 にする)。obaket の `macOS/bin/dev-restart` は「`restartable status` の rc 2 = ループが動いていない (アプリを止めずに人へ頼む)」に依存しているので、
写しが崩れると dev-restart の判断が黙って変わる。

## 推奨対応

CLI を実際に起動して (テストバイナリの helper か `go run`)、socket が無い / ok:false / ok:true / 時間切れ の各場合の rc と出力を検査するテストを足す。`!response.OK` を外す変異と、rc 2 を 1 にする変異で red を確かめる。

## 確認

テストの走査で確認 (2026-10-01 Claude: main_test.go の Test は SIGPIPE の 2 本だけ)。

## 進捗

2026-10-01 (dotfiles-58)

- 主張を確認: `controlCommand` の写し (socket 無し / connection refused = 2、ok:false = 1、時間切れ = 124) は `main_test.go` に検査が無かった。
- 追加: `TestControlCLIExitCodes` (`src/restartable/main_test.go`)。テストバイナリ自身を子プロセスで起こして `runMain` を通し、rc / stdout / stderr を別々に見る。サーバは本物の `control.Listen`。
  ケース: socket 無し (status / restart)、socket ファイルだけ残って listener 無し、ok:true (status / restart)、ok:false (status / restart)、応答しないサーバ + `--timeout 200ms` = 124、不正 flag / 余分な引数 / timeout 0 = 2。stdout は `control.Response` に decode して値を比べ、stderr の有無も assert する。経過時間は assert しない (timeout の値を入力に渡すだけ)。
- 🚨 本番の不具合を 1 件直した: 応答しないサーバへの時間切れは、実測で **rc 1** になっていた (stderr は `read unix ...: i/o timeout`)。`control.Call` が接続に ctx と同じ期限の I/O deadline を置くため、`os.ErrDeadlineExceeded` が `context.DeadlineExceeded` より先に返るため。`main.go` で `os.ErrDeadlineExceeded` も 124 に写すようにした。
- codex の設計反証: P1 (引数を NUL 区切りで環境変数に入れられない) を採用して JSON にした。P2 (124 が競合で rc 1 になりうる) は上の不具合として実測で確認して採用。
- codex の実装レビュー: P2 (stdout を部分一致でしか見ていない / stderr 未検査) を採用して decode と stderr の assert にした。P3 (サーバ側の受け取り goroutine が残る) を採用して終了用チャネルで止める。
- 変異 (mutate-verify、いずれも red を確認): `!response.OK` → `false` で `status:_ok:false` が red / 「connection refused」側の `return 2` → 1 で `status:_socket_が無い` が red / `return 124` → 125 で `status:_時間切れ` が red。
- 変異 `os.ErrDeadlineExceeded` の分岐を外す: 時間切れの競合次第なので 1 回の run では緑のことがある (30 回繰り返すと red になる回と緑の回があった)。この分岐の検出は確率的で、決定的には守れていない。
- 検証: `go test -race -count=1 ./...` rc=0 (4 package ok)、`golangci_lint.sh v2.5.0 run` 0 issues。
