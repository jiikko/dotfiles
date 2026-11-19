# 1087 (bug): tmux の本番サーバが 20 秒ほど CPU 100% で応答せず、操作が固まった（SCRATCH の popup 2 つと resurrect の保存が重なった時刻。原因は未特定）

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-19〜）

起票日: 2026-11-16

## 概要

ユーザー報告「tmux が固まった？」（2026-11-16 10:17 ごろ、ubiregi-server のセッションで作業中）。数十秒後に「動いた」。
固まっている最中のプロセス一覧を 1 回だけ採れた。スタックは戻った後にしか採れていないので、何に CPU を使っていたかは分かっていない。

> 反証レビューは通していない（観測の記録として起票した。原因の主張はまだ無い）。

## 観測（2026-11-16、KOJIm3MBP / macOS 27.0 / tmux 3.7c）

10:17:51 の `ps`（固まっている最中）:

| pid | 何 | 状態 |
|---|---|---|
| 61035 | tmux サーバ（`tmux new-session -d -s __tt_hold_1317`）。起動から 35 日 16 時間 | **R・CPU 99.7%**。累積 CPU 1456 分。RSS 約 735 MB |
| 74782 / 75037 | `tmux display-popup -E … SCRATCH …`（`-c /dev/ttys000` と `/dev/ttys002`） | 22 秒前に起動。2 つの client から同時に開いている |
| 74799 / 75064 | popup の中の `tmux attach -t scratch` | 22 秒前に起動 |
| 72997 / 74779 → 74825 → 74940 → … → 75327 | `tmux_resurrect_debounced_save.sh` が 2 本（43 秒前・22 秒前に起動）。片方の `save.sh quiet` が `tmux show-option -gqv @resurrect-save-command-strategy` で返りを待っている | 待ち |
| 75031 → 75329 | `tmux_ignite_current.sh colour201 ubiregi-server` が `tmux if -F … set -g @ignite colour214 ; refresh-client -S` で返りを待っている | 22 秒前に起動。待ち |
| 76004 → 76006 | `tmux show -gqv @tt-restore-in-progress` | 12 秒前に起動。待ち |

- サーバ以外はどれも CPU 0%。サーバが 1 本の処理を抱えていて、client のコマンド（`show-option`・`if`）が 12〜22 秒返らずに並んでいた
- 数秒後（10:18 ごろ）に `sample 61035 3` を採ったときは、サーバは CPU 7〜8% に戻り、スタックは `select` での待ちがほとんど（2,480 中 2,445）。
  並んでいたコマンドと popup のプロセスは消えていた。固まりは 20〜30 秒で自然に解けた
- `sample` に `libjemalloc.2.dylib` の tcache の GC が 3 回写っていた（`server_accept` → `server_client_create` → malloc）。Homebrew の tmux が jemalloc を使っている。固まりとの関係は不明
- 同じ時刻に ubiregi-server のセッションの Claude Code が `ps`・`sample` を打っていた（固まりを見つけた後なので原因ではない）

## 仮説（どれも未検証）

1. **SCRATCH の popup を 2 つの client から同時に開いた**ことが引き金。popup は中身が変わるたびに全体を描き直し（567）、点火のアニメ（`tmux_ignite_current.sh` の `refresh-client -S`）が popup 全体を何度も送り直す（567: 1 回の切替で約 570KB）。2 つの client で同時に起きた
2. resurrect の保存（`save.sh`）が、ペインの内容を取る大きな処理をサーバに頼んでいた。サーバの RSS 約 735 MB・35 日の稼働で、履歴が大きい可能性
3. 377 と同じ系（floating pane と client の大きさの変化で CPU 100%）。ただし 377 は `kill -9` まで戻らなかった。今回は自然に解けたので、別の形の可能性が高い

## 次に採るもの（また固まったとき）

- 固まっている**最中**に `sample <サーバの pid> 5 -file <置き場>`（どの関数で回っているかが分かる。今回は採り遅れた）
- 同時に `ps -Ao pid,ppid,stat,etime,%cpu,command -ww | grep tmux`（並んでいるコマンドで、何の後に固まったかが分かる）
- 開いている popup の数と、その直前の操作（SCRATCH を開いた・window を移った）
- 繰り返すなら、隔離サーバで「2 つの client から同時に SCRATCH の popup を開く」と「大きい履歴で resurrect の保存」を別々に再現する

## 関連

- 377（floating pane で CPU 100% のハング。`kill -9` のみで復旧）
- 567（scratch の popup の全体の描き直し）・637（開きっぱなしの scratch の popup がサーバの CPU の最大）
- 547（タイピングのもっさり。`tmux -v` / `SIGUSR2` でサーバのログを on / off できる）
- `scripts/tmux_server_watchdog.sh` はサーバの死だけを記録し、固まりは記録しない

## 進捗

- 2026-11-16: 起票（観測 1 回）
- 2026-11-19: 「次に採るもの」の 1・2 を自動にした（commit「feat(tmux): watchdog がサーバの CPU の使い切りが続いたら sample と ps を撮る」）。
  `scripts/tmux_server_watchdog.sh` の `check_busy` が、サーバの累積 CPU 時間の増え方を 5 秒の窓で見て、85% 以上が 2 回続いたら
  `sample <pid> 5` と `ps -ww` を `~/.cache/tt-server-busy-<時刻>-<pid>.{sample,ps}` に撮り、`~/.cache/tt-restore-trigger.log` に `server-busy` 行を書く（600 秒は撮り直さない。直近 10 組を残す）。
  判定に tmux への問い合わせは使わない（固まったサーバへ client を足すため）
  - [x] 本番の watchdog を新しいスクリプトで張り直す（2026-11-19。古い watchdog は TERM / HUP をスクリプトが無視し、INT も run-shell -b の背景ジョブなので無視するため、pid を名指しで USR1 を送って止め、`tmux run-shell -b` で張り直した。残った lock は新しい watchdog が起動時に掃除した）
  - [ ] 次に固まったら `server-busy` 行と sample を読み、仮説 1〜3 を絞る
  - 未着手: 隔離サーバでの再現（「次に採るもの」の 4）
  - 未確認: 閾値（85% × 2 窓）が誤検知でどれだけ撮るか。637 の popup の描き直しで撮りうる。cooldown で 10 分に 1 組までに抑えている
- 2026-11-19: 敵対的レビューを 3 周（opus）。直したもの: health の確認が固まったサーバに並んでループを塞ぐと撮り逃す・撮った直後に並ぶ（直前の窓が忙しい間は health を後回し）/ ループが塞がれた窓で撮ると固まりの後のスタックしか写らず cooldown を潰す（撮らずに `server-busy-missed` 行だけ残す）/ 数でない値・先頭が 0 の値（08）の環境変数で算術エラーになり、while ごと捨てられて生きているサーバに偽の server-death を書く（変異で再現。`watchdog_uint_env` で検査して 10 進数に直す。前からある TT_HEALTH_CHECK_INTERVAL・TT_PSLOG_KEEP・TT_VERDICT_WINDOW も同じ仕組みで落ちたので通した）
  - 受け入れたもの: bash 3.2（`date` の秒刻み）では窓の比が量子化で閾値の近くを外しうる（本番の PATH では brew の bash 5 が先）/ 長く忙しい時間が続くと古い組が押し出されて `sample=` のファイルが消える / sample の成否はログに出ない（ファイルが無ければ失敗）/ cooldown が sample の秒数より短いと `.sample` が取り残される（短くするのはテストだけ）
  - テストが無いもの: health の後回しの 600 秒の上限
