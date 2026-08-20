# 068 bug: snapshot_health の常駐プロセス検出が lock owner の書式変更に追従していない

起票日: 2026-08-20

`/audit` (品質バッチ, forge Standard) の High。**8 エージェントのうち 6 が独立に同じ箇所を
指したうえ、main agent が実験で再現済み**。

## 問題

`scripts/tmux_snapshot_health.sh` の `daemon_alive()` が lock owner を丸ごと読んで
`kill -0` に渡している:

```sh
owner="$(cat "$d/pid" 2>/dev/null)"
if [ -n "$owner" ] && kill -0 "$owner" 2>/dev/null; then found=1; fi
```

一方、書き手の `scripts/lib/tmux_resurrect_guards.sh` `tt_lock_write_owner()` は
**`<pid>\t<ps -o lstart=>` の 2 フィールド**を書く。`kill` はこれを pid として解釈できない。

**実測** (2026-08-20): `kill -0 "16172<TAB>Mon Aug 18 00:00:00 2026"` は
`illegal pid` で rc=1。よって `found` は永遠に 0 = 「周期保存が居ない / watchdog が居ない」
の誤報になる。

同ファイルは `lib/tmux_resurrect_guards.sh` を既に source しており (:31)、正しい parser
`tt_lock_owner_alive()` (guards.sh:85。tab 分割 + 旧 pid-only 形式のフォールバックつき) が
すぐ隣にある。使われていないだけ。

## 発火条件

lock owner が新書式で書かれた状態 (= `tt_lock_write_owner` を持つ世代のコードで
tmux サーバを起動し直した後) で `tmux_snapshot_health.sh` が走ると、周期保存と watchdog が
どちらも生きていても「異常」と判定される。watchdog の定期 check がこれを拾うと
health 状態が ng に張り付き、**以後は状態変化が起きないので本物の保存停止や archive 破損が
起きても二度と通知されない**（「静かな故障を見張る」装置自体が無音化する）。

なお現行の稼働マシンでは lock が旧書式のまま残っている世代のプロセスが動いているため、
まだ顕在化していない（次回サーバ再起動で顕在化する）。

## なぜテストで捕まらないか

`tests/tmux/test_snapshot_health.sh:95-96` の fixture が
`printf '%s\n' "$PERIODIC" > .../pid` と **旧 pid-only 形式を自前で書いている**。
production の書き手を呼ばずに書式をテストへコピペしているため、書式変更に追従しない
（実物とずれた stub。`mutation-verify-new-tests.md` の「実物とずれたモック」）。

## 推奨対応

1. `daemon_alive()` の判定を `tt_lock_owner_alive "$d"` に置き換える
2. テスト fixture も `tt_lock_write_owner` を呼んで書く（書式の出典をテストへコピペしない）
3. 「生存プロセスの lock → 稼働中」「その pid を kill → 不在」の 2 方向で red/green を見る
4. owner の read/write を guards.sh の 2 関数に集約し、シリアライズ形式の出典を 1 つにする
