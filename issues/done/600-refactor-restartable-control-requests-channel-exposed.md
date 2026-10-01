# 600 (refactor): restartable の control.Server.Requests が送受信どちらもできるチャネルのまま公開されている

起票日: 2026-10-01

出典: restartable の audit (codex のコードスキャン、2026-10-01。[601](601-research-restartable-audit-2026-10-01.md))。Claude がコードで裏を取った。

## 問題

`src/restartable/internal/control/control.go` の `Server.Requests` は `chan *Request` のまま公開されていて、外の package から送信も close もできる (encapsulation の E4: 内部表現の露出)。
issue 586 の作業中に、`acceptLoop` がこのチャネルを close したことで、送信を待っていた `serveConn` が `panic: send on closed channel` で落ちた (直した: commit
「fix(restartable): Ctrl-C を保留の列より優先して届け、control の Close と接続の競合で panic しないようにする」)。今の production は close しないが、型がそれを許している。

## 発火条件

新しい呼び出し側が終わりの合図のつもりでチャネルを close し、同時に `serveConn` が要求を送る。compile は通り、実行時に panic する (runner が落ちると子が孤児になる)。

## 推奨対応

所有者 (`Server`) は送受信できるチャネルを非公開で持ち、外には受信専用 (`<-chan *Request`) を返す。close できない型にして、取り違えを compile error にする。

## 確認

コードで確認 (2026-10-01 Claude)。散在は production の受信 1 か所 (actor)・テストの受信 (control_test / integration_test)。

## 進捗

- `Server.Requests` を非公開の `requests` にし、`func (s *Server) Requests() <-chan *Request` で受信専用を返すようにした。受信側 (production は actor.go の 1 か所、テストは control_test 2・integration_test 3。取り込みのときに 598 の main_test 1 か所も直した) は `Requests()` へ書き換えただけ。挙動は変えていない。
- codex の設計の反証: P1 が 1 件 (`Listen` の複合リテラルの `Requests:` も `requests:` に直す必要)。コードで確かめて採用 (どのみちビルドが通らない)。他の指摘なし。
- compile で固定できたことの確認: 一時ファイルで `close(s.Requests())` と `s.Requests() <- nil` を書くと、どちらも `cannot close receive-only channel` / `cannot send to receive-only channel` で compile error になった (確認後に消した)。
- 検証: `go test -race -count=1 ./...` rc=0 (全 package ok)。`go test -v` の PASS 行は変更の前後とも 172 件。golangci_lint v2.5.0 は 0 issues (rc=0)。
- codex の実装レビュー (`codex exec review --uncommitted`): 指摘なし (sandbox で Unix socket の bind が拒否されたというテスト失敗の注記のみ。codex 側の環境の事情で、こちらの実行は通っている)。
- 取り込み (dotfiles-58): 598 が足したテスト (main_test.go) がフィールド server.Requests を使っていたため、598 → 600 の順で並べるとビルドが壊れた (002fd390 で 1 度 push)。Requests() に直した (36fbf91f)
- セルフレビュー (2026-10-01): actor.go の `case req, ok := <-requests` の `!ok` の枝は、受信専用にした後は到達しないので消した (要求の列は閉じない)
