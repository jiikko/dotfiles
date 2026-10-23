# proctree

プロセスの子孫を集めて止める共有 module (Go のライブラリ。CLI は持たない)。

```go
proctree.Target{Root: cmd.Process.Pid, Group: true}.Stop(5 * time.Second)
```

- 集めるのは、`Root` のプロセスグループの全員 (`Group` が true のとき) と、`Root` から ppid でたどれる子孫。
  `setsid` / `setpgid` でグループを抜けた子 (puppeteer が detached で起こす Chrome など) も、親が生きていれば木でたどれる
- 凍らせて (SIGSTOP) 集め直してから TERM → CONT → 猶予 → KILL。止め終えるまで返らない
- `Group` が false (根が呼び出し元のグループに居る) のときは、グループ宛てに撃たない (呼び出し元ごと止めるため)
- `Forward(sig)` は凍らせずにそのまま伝える (子に後始末の機会を渡す)

止められないもの: 止める前に親を離れて init の子になり、かつグループも抜けたもの (daemon 化した子・`tmux -L` のサーバ等)。

使う側: `src/runtimeout` (時間の上限付き実行。issue 640) / `src/zundamon-kaisetsu` の mermaid の描画 (issue 649)。

## `childrun` (同じ module の package)

子を 1 本起こして待つ道具 (runtimeout・serialrun) の共通部。写しを 2 つの module に持つと片方だけ直る (QUIT の死に直しで実際に起きた) ので、ここに 1 つだけ置く。

- `ParseSeconds`: 秒の引数 (小数可・0 以上)。0 より大きいのに 1ns 未満になる値は 1ns に切り上げる (0 は「上限なし」に使われるので)
- `StartErrorRC` / `WaitStatus`: 起動の失敗と子の終わり方をシェルと同じ rc に (127 見つからない / 126 実行できない / シグナルなら 128+n)
- `ExitAs`: 道具がシグナルを受けて子もシグナルで死んだら、道具も同じシグナルで死に直す (bash が「子が処理した」と読まないため)。QUIT では死に直さない
