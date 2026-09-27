# 565 (feat): kernel-alloc-watch を定期的に回す仕組みが無い

起票日: 2026-09-27

> **pending (2026-09-27〜)**: ユーザーの判断で凍結。再開の trigger: macOS 27 へ上げた後も漏れが再発し (issue 500)、日をまたいだ推移を手で回さずに取りたくなったとき。

## 概要

`bin/kernel-alloc-watch` (500) は叩いたときに 1 行記録するだけ。日をまたいだ推移を取るには、人が tmux の pane で
`while :; do kernel-alloc-watch >/dev/null; sleep 600; done` を回すか、Claude の session の裏で回すしかない (session を閉じると止まる)。
500 の「macOS を上げた後に 1 日回して確かめる」も、この手で回すことに頼っている。

あわせて、`count_tmux_clients` は tmux のサーバが固まると返らず、その回の記録が終わらない (timeout が無い。コードのコメントに記録済み)。
定期的に回すなら、固まった回が積もらないよう上限が要る。

## 対応方針 (案)

- launchd の LaunchAgent で 10 分ごとに回す (plist を dotfiles に置き、setup.sh で入れる)。あるいは pro-con の予定 (スケジューラージョブ) に載せる
- 1 回の上限を持たせる (tmux・zprint の呼び出しに timeout)
- 案内は `kernel-alloc-watch --help` と README の「カーネルメモリの漏れの記録」に書く

## 受け入れ条件

- [ ] 人が何もしなくても 10 分おきに記録が溜まる (再起動の後も)
- [ ] tmux が固まっても、記録のプロセスが積もらない
