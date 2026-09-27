# 555 (ux): 止めきれなかったときに同じ行が 14 本並び、案内の `claude stop <id>` では直らない

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27) で、終了のときに C-089 の PG を止めきれなかった (原因は 551 で直した)。そのとき見えたもの:

- dispatcher.log に「C-089 の PG (4bbb588c) がまだ動いていたので止め直した」が同じ時刻 (14:39:20) で 14 行並んだ。
  確かめの周 (`dispatcher/shutdown.go` の `ensurePolls` = 15) ごとに 1 行ずつ積み、まとめて書くので同じ時刻になる
- 画面の終わりに出た案内は「止める: pro-con dispatcher --stop / claude stop <id>」。`claude stop 4bbb588c` は `stopped` と rc=0 を返したが、
  一覧は working のままで、案内どおりにしても直らなかった

## 対応方針 (案)

- 同じ PG の止め直しは 1 行にまとめる (「C-089 の PG (4bbb588c) を 14 回止め直したが、一覧で止まったと確かめられない」)
- 止めきれなかったときの案内に、そのとき見えている様子 (pid の有無・一覧の state・Claude Code の記録の state) を添える。
  次に読む人が「プロセスは居ない」と分かれば、kill や再起動に進まずに済む
- 案内する操作は、その状態で効くものだけにする (効かないと分かっている `claude stop` を並べない)

## 受け入れ条件

- [ ] 止め直しの記録が PG ごとに 1 行になる
- [ ] 止めきれなかったときの案内に、pid の有無と一覧・記録の state が出る

## 関連

- 551 / `dispatcher/shutdown.go` の `ensureStopped`
