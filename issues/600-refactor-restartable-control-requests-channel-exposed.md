# 600 (refactor): restartable の control.Server.Requests が送受信どちらもできるチャネルのまま公開されている

> 🚨 **担当中: dotfiles-58**（2026-10-01〜）

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
