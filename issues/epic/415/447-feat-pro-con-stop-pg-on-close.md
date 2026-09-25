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

## 進捗

- 2026-09-25 (C-005): 実装した (ブランチ `worktree-pc-c-005`。master へは未取り込み)
  - close の適用で、PG の session を持つカードに `StopAfterClose` を付ける (`store` の close。適用と同じ書き込みなので、dispatcher が止める前に落ちても印は残る)
  - dispatcher の Tick (register の後) が印の付いたカードを止める (`dispatcher/close.go` の `stopClosed`)。止める・止まったかの判定は終了の `ensureStopped` をカードで絞って 0 周 (待たない) で呼ぶ。判定は 1 つのまま (`agents.Session.Stopped`)
  - 止まったのを確かめたら印を外して履歴に「止めた / 既に止まっていた」。止まらなければ Tick ごとに止め直し、閉じてから `closeStopWait` (1 分) を過ぎたら失敗を履歴に書いて諦める (close は成立したまま)
  - 止めるのは起動の記録と sessions-retired.json の行のうちそのカードのものだけ。worktree とブランチには触らない。画面は触らない
- 確かめたこと: `dispatcher/close_test.go` の 5 本 (偽の launcher / lister・t.TempDir)。そのカードだけ止める / 入れ替わった前の session も止める / 失敗は close を保ったまま止め直し → 諦めて履歴 / 一覧で止まっていなければ止めた扱いにしない / PG の無いカードは何もしない。`bin/mutate-verify` で 5 つの変異 (印を付けない・カードで絞らない・待たずに諦める・一覧を見ずに止めた扱い・諦めても印を外さない) がすべて red
- 残り: 実機 (本物の claude) で閉じたカードの PG が stopped になるかは未確認 (dogfooding で見る)。この変更より前に閉じて残っている PG は印が無いので止めない (終了のときに止まる)
