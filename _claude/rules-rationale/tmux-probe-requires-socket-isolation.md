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

## 「kill-server は socket ファイルを消さない」の実測（2026-09-10 / issue 305）

```
tmux -L probe -f /dev/null new-session -d 'sleep 30'
tmux -L probe kill-server
→ /private/tmp/tmux-501/probe が残る   （SIGKILL で殺した場合も同じ）
```

つまり中断時だけでなく**正常終了のたびに 1 個ずつ**漏れる。実測当日、`/private/tmp/tmux-501/` に
**536 ファイル**あり、`lsof` で生きているのは `default`（本番）の 1 個だけだった（最古 2026-07-05）。
内訳の大半は `ctrlv-test-*` / `pane-state-bell-*` = テストが `-L <name>-$$` で起こしたもの。

起票時の見立ては「trap が中断で走らないから残る」＝**掃除機構（走査して消す）が要る**だったが、
実際は正常系で漏れていたので**発生源を断つ**側が取れた（`adversarial-review-own-safeguards.md` §0-A）。
走査しない形なら、母集合の取り違えで本番の socket に届く経路が原理的に無い。

**掃除機構を作りかけた側の記録も残す**: 母集合は「その dir 直下の socket で、①名前が `default`
でない ②`lsof` がどのプロセスからも開いていない」で書ける（prefix には依存しない）。
537 個の実削除はこの形で行い、本番は無傷だった。今は使う場面が無いが、
手で回収するときはこの 2 条件を使う。

🚨 **残骸を「溜まっている」と報告する前に、その置き場を誰がいつ掃除するかを確かめる**。
`/private/tmp` は**起動時に一掃される**（実測 2026-09-10: uptime 67 日 / 起動より古いエントリ 0 件 /
`/etc/periodic` に掃除の仕掛け無し）。`HOME` 配下は誰も掃除しない。今回はそれを確かめずに
削除の承認を求めていた。判断軸は「今いくつあるか」ではなく**「増加が止まる機構があるか」**。
