# testwait

Go のテストで「条件が成り立つまで上限つきで待つ」を 1 つに持つ module。各 module が `replace testwait => ../testwait` で取り込む (テストからだけ使う)。

- `testwait.Until(t, what, cond, opts...)` — 上限 (既定 10 秒) を超えたら落とす。option は `Within` (上限)・`Tick` (間隔)・`Step` (1 周ごとの処理)・`Diag` (呼び出し側の診断)
- `testwait.Poll(limit, cond, opts...)` — 成り立ったかを返すだけ (落とさない)
- `testwait.Snapshot()` — このテストのプロセスの子孫と goroutine の一覧 (Poll で待って自分で落とすときに添える)

時間切れのときは、待った時間・`Diag` の出力・子孫のプロセス (`ps` の STAT の T = 止まった、+ = 端末の前面) と goroutine の一覧を
`t.Fatalf` の文に出す。CI でしか起きない止まりを、落ちた 1 回のログで調べられるようにするため。
