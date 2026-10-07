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
