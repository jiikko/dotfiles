# 447 (feat): カードを完了・却下したら、その PG の session を止める

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

カードを閉じても (card close)、その PG の claude --bg の session は止まらず、idle のままプロセスが残る
(2026-09-25 の dogfooding 1 回目で見つけた。state: done・pid あり。440)。終了のときには止まる (427 の終了の保証) が、それまで残る。

## 対応方針 (候補)

- dispatcher が close の依頼を適用したら、そのカードの PG (pro-con の起動の記録にある session と、再開で入れ替わった前の session) を止める。
  止まったかは終了のときと同じく `claude agents --json --all` で確かめる (止まった = pid 無し かつ working でない = 01dbb3b0)
- 止めるのは閉じたカードの session だけ (ほかのカードの PG・pro-con が起動していない session に触らない)
- 🚨 PG の worktree とブランチは消さない (取り込みは PM が cherry-pick で行うので、閉じた後も残す。消すかは別の判断)
