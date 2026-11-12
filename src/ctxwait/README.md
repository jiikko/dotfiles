# ctxwait

取り消しで抜けられる待ちを 1 か所に持つ module。各 module が `replace github.com/jiikko/dotfiles/src/ctxwait => ../ctxwait` で取り込む。

- `ctxwait.Sleep(ctx, d)` — d だけ待つ。ctx が先に終われば `ctx.Err()` を返す (0 以下は待たない)。slack-bbs・doctor・process_supervisor が使う
- `ctxwait.Flock(fd, how, poll, wait)` — 他のプロセスが持つ flock を、取れるまで `wait(poll)` で待って取り直す。
  `wait` が誤りを返したら (取り消し) それを返す。待ち方 (ctx・Ctrl-C の合図・テストの時計) は呼び出し元が `wait` で決める。
  slack-bbs (Slack の呼び出しの列)・ratelimit (利用枠の共有ファイル)・zundamon-kaisetsu (エンジンの起動のロック) が使う

素の (LOCK_NB でない) flock は ctx でも Ctrl-C でも抜けられないので、待つ flock はここを通す。
