# testwait

Go のテストで「条件が成り立つまで上限つきで待つ」を 1 つに持つ module。各 module が `replace github.com/jiikko/dotfiles/src/testwait => ../testwait` で取り込む (テストからだけ使う)。
module 名をドット付きのパスにしているのは、dotfiles の外の repo が取り込む module (chromecookie) のテストもこれを使うため。ドットの無い名前だと、取り込む側の `go mod tidy` が依存グラフを辿るときに `malformed module path` で落ちる (取り込む側は replace を見ない)。

- `testwait.Until(t, what, cond, opts...)` — 上限 (既定 10 秒 = 定数 `testwait.Limit`) を超えたら落とす。option は `Within` (上限)・`Tick` (間隔)・`Step` (1 周ごとの処理)・`Diag` (呼び出し側の診断)
- `testwait.Recv(t, what, ch, opts...)` / `RecvOK` — channel から 1 つ受け取る。上限までに届かなければ落とす (`RecvOK` は閉じていたかも返す)
- `testwait.Poll(limit, cond, opts...)` — 成り立ったかを返すだけ (落とさない)
- `testwait.Snapshot()` — このテストのプロセスの子孫と goroutine の一覧 (Poll で待って自分で落とすときに添える)。`testwait.ProcessTree(pid)` は任意の pid の子孫だけ

時間切れのときは、待った時間・`Diag` の出力・子孫のプロセス (`ps` の STAT の T = 止まった、+ = 端末の前面) と goroutine の一覧を
`t.Fatalf` の文に出す。CI でしか起きない止まりを、落ちた 1 回のログで調べられるようにするため。
