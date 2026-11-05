# 1034 (research): gate は写しを止められるか — 写しが書かれた commit で gate を回し直して数える

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-05〜）

起票日: 2026-11-05

## 概要

gate は 2026-10-29〜11-05 の手元の 106 回で、重複を 1 回も止めていない。「写しを書いていない」のか「書いた写しを拾えていない」のかを、
946 で寄せた写しが**書かれた commit** の木で gate と `near -similar` を回し直して切り分ける (1009 の前提 2・3。hook と gate は触らない)。

## 測り方 (測る前に決めた。2026-11-05)

- 母集合: 946 で寄せた 21 件のうち、関数の名前で指せる 13 組 (lockman `fmtSize`/`fmtAge`・`captureStderr`/`captureStdout`、mac-lan-manager `or` ×2・`DefaultPath` ×2、
  ratelimit `cachePath`/`stampPath`・`paceAdviceUnit`/`paceCellUnit`・`padRight`/`padLeft`・`readStatuslineRaw`/`readWriters`、restartable `envDuration` ×2・`shellQuote`/`ptyShellQuote`・`shortTempDir`/`shortSocketDir`・`outputText`/`text`)。
  クロージャの 3 組 (「Device / Host / 経路の IP が同じ」) と、どの手段も出さなかった 3 件は名前で指せないので外す
- 各組で、後から書かれた側が入った commit C を `git log -S 'func <名前>('` で特定し、今の dupscan で `gate -head C -base C^` を回す (C の木を checkout せずに読む)。
  既定 (L1,L2・下限 20)・`-min-nodes 12 -min-closure-nodes 12`・`-levels L1,L2,L1m` の 3 通り。あわせて C の木で、書かれた側を起点に `near -similar` を当て、相手が上位 3 件に入るかを見る
- 採否の基準: 既定の gate が書かれた時点で止めた数を「今の検出力」とする。変える候補 (下限 12・L1m・near) は、**13 組中 7 組以上を書かれた時点で出し、かつ**
  直近 30 commit (src の Go を触ったもの) で寄せるべきでない組による止まりが 3 commit 以下のときだけ、gate の変更を提案する (988・1009 の保留を解くかの判断材料。解くのはユーザー)
