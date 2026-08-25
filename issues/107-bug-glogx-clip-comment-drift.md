# 107 bug: glogx の切り詰め関数の doc が、実装と逆のことを言っている

起票日: 2026-08-25 / priority: medium (読んだ人が選択を誤る)

## 事実

`src/glogx/render.go` の 3 つの doc が、現在の実装と食い違っている。

### 1. `clipToWidth` (`render.go:497`) — 同じコメント内で自己矛盾

冒頭は「超過時は**色を落として**切り詰める (色コードを保ったままの部分切りは複雑さに見合わない)」
と書いているが、2 段落下は
「`ansi.Truncate` は ANSI を解しつつ幅で切り、**SGR を保持する** (旧実装は色を落としていたが…)」
と書いている。実装は後者 (`truncateDisp` → `ansi.Truncate`、`width.go:201`)。
**冒頭が旧実装の説明のまま残っている。**

### 2. `truncateKeepANSI` (`render.go:588`) — 存在しない対比

「`clipToWidth` が切り詰め時に**色を捨てるのと対照的に**」と書いているが、上記のとおり
`clipToWidth` も色を保持する。両者とも `truncateDisp` に委譲しており、
**実際の差は末尾の `…` を付けるか (`"…"` vs `""`) だけ**。

これは選択を誤らせる: 「色を残したいから `truncateKeepANSI`」という理由で選ぶと、
本当は `…` の有無で選ぶべきところを間違える。呼び出しは `clipToWidth` 35 / `truncateKeepANSI` 6。

### 3. `isANSITerminator` (`render.go:581`) — 利用者リストが古い

「`truncateKeepANSI` / `dropToColumn` / `stripANSI` が同じ終端判定を共有し」と書いているが、
`truncateKeepANSI` は自前で走査せず `ansi.Truncate` に委譲しているので**もう呼んでいない**。
実際の呼び出しは `render.go:617` と `:652` の 2 箇所。

## 直し方

1. `clipToWidth` の冒頭 1 文を実装に合わせる (色は保持する)
2. `truncateKeepANSI` の対比を「`…` を付けない版」に書き直す。
   2 つの違いが `tail` 引数だけなら、**片方を薄いラッパにして差を 1 行にする**のも手
   (ただし `clipToWidth` の fast-path は残すこと。`clipMeasure` の存在理由 =
   幅の二度測りを避ける hot path 最適化も別物なので潰さない)
3. `isANSITerminator` の利用者リストを実態に直す

## 補足

3 件とも「実装で強制できない情報 (関数の選び分けの理由)」なので、
コメント自体は残すべきもの。直すのは**内容が古い**ことだけ。
