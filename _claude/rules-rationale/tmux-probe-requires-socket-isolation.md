# 破壊的 tmux コマンドは、隔離を実証してから打つ（$TMUX は TMUX_TMPDIR に優先する） — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/tmux-probe-requires-socket-isolation.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## 認識がどう破れたか（起源: 2026-07-30 の本番サーバ誤殺）

別セッションの Claude が popup 計測のため以下を実行し、`$TMUX` 優先の仕様により全コマンドが本番サーバへ向かい、サーバごと落とした。

```sh
export TMUX_TMPDIR=$(mktemp -d); tmux -f /dev/null new-session -d -s probe ...
# ...計測...
tmux kill-server 2>/dev/null; rm -rf $TMUX_TMPDIR
```

「本番を触るとまずい」という認識はあった。それでも打てたのは 3 段の誤認が重なったため（当人の追記より）:

1. `TMUX_TMPDIR=$(mktemp -d)` を書いた時点で「隔離した」と**認識が完了**した（以降、隔離は前提扱いになり疑う対象から外れた）
2. `-f /dev/null` を「素の設定の新サーバが立つ」と誤解した
3. `probe session ok` の成功出力を「隔離できている証拠」として受け取った

損失: 直前の完全な保存は 7/29 20:02（29 sessions / 90 windows）。死亡時のライブ状態は記録が無く（13:28:22 に発火した save は 0 sessions で regression guard が reject）、15:54 にこの保存から復元された（セッション一覧が保存内容と一致することを実測済み）。**つまり失われたのは 7/29 20:02 以降・約 17 時間分の変化**。保存と guard が無ければ全損だった。

2026-07-07 にも tests/tmux/test_fork_scratch.sh の bare `kill-server` が本番を直撃する同型事故がある。**07-07 の教訓はテストには実装で落ちた**（`tests/tmux/*.sh` 冒頭の `unset TMUX TMUX_PANE`）が、「テストではないアドホックな 1 コマンド」の経路には落ちていなかった。それを埋めるのが下記の hook。
