#!/usr/bin/env bash
# 手動復元の実行体。tmux_restore_confirm.sh (確認 popup) の confirm 後に run-shell -b で
# detach 起動され、popup の寿命と独立に resurrect の restore.sh を実行する。
#
# なぜ分離するか: display-popup -E の中で restore.sh を同期実行すると「popup を閉じる /
# popup へのキー入力 = 復元プロセスの kill」という失敗モードを内蔵する。実発 (2026-07-30):
# 15:54 の手動復元が pane 60/93 で途中死し、6 セッションが未復元のまま気づかれず、
# @tt-restore-in-progress の残置で以後 TTL(120s) の間は保存も抑止された。途中死は完全に
# silent だった。復元の寿命を popup から切り離し、異常終了を観測可能にする。
#
# 責務:
#   1. restore.sh を popup と独立した寿命で実行する
#   2. 異常終了 (post-restore-all 未到達) 時に @tt-restore-in-progress を掃除し、
#      restore-aborted を trigger ログに記録する (silent な途中死を無くす)
#   3. 正常終了は restore-end を記録する (開始は _tmux.conf の pre-restore-all hook が
#      restore-start を記録する。auto/manual 共通)
#
# 無音契約: run-shell -b 経由のため stdout/stderr は汚さない (scripts/CLAUDE.md 参照)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

# 共有の観測ログに書くスクリプトは default socket ゲートを通す (scripts/CLAUDE.md の不変条件)。
# -L の隔離サーバで C-t C-r を押したときに本番の観測ログを汚さない。
tt_on_default_server || exit 0

# 出力を持たない (run-shell -b の無音契約)。他 2 本 (tmux_periodic_save.sh /
# tmux_server_watchdog.sh) と揃える。**同一コミット e089e7e で watchdog にだけ入り、
# ここへ入れ忘れていた**もので、意図的な非対称ではない (issue 111)。
#
# 🚨 効くのは「将来この経路が stdout へ 1 行でも出したとき」への防御。個々の呼び出し側でも
#   塞いでいる (`bash "$restore" >/dev/null 2>&1` 等) が、**runner 自身は復元の間ずっと
#   run-shell のパイプを掴み続ける** (実測: 復元は 4〜10 秒 — ~/.cache/tt-restore-duration.log。
#   `sleep 3; echo` を run-shell -b すると 3.5 秒後に pane が view-mode になる)。
#   その間に stdout へ出た 1 行は、**復元中のユーザーのアクティブ pane を view-mode にする**。
#
# 🚨 実測で崩れた「もっともらしい理由」を書かないこと (2026-08-28 に隔離サーバで測定):
#   - **stderr は view-mode を開かない**。tmux サーバの fd2 は /dev/null (本番 pid でも実測)。
#     つまり `2>&1` は view-mode 対策としては空振りで、揃えるためだけに付けている
#   - **SIGPIPE は来ない**。書いたときにしか飛ばず、このスクリプトは stdout に 1 バイトも書かない
#   - **stdin ブロックも起きない**。run-shell の子の stdin は即 EOF (`read -t 2` が rc=1。
#     本物の timeout は 142)。resurrect の restore.sh も inherited stdin を読まない
#     (12 箇所の `while read` は全て自前の入力を持つ)
#
# 🚨 **view-mode を開く支配的な要因は rc≠0 の方**で、これは exec では塞げない
#   (無音でも rc=1 なら開く。実測)。この経路は全て `exit 0` で終わるので現状は問題ないが、
#   終了コードを変えるときはそこが本体だと思うこと。
exec </dev/null >/dev/null 2>&1

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
TT_RESTORE_STATE_DIR="${TT_RESTORE_STATE_DIR:-$HOME/.cache/tt-restore-run}"


# ---- 単一実行ガード -------------------------------------------------------------------
# 🚨 popup 内同期実行だった旧実装は popup が事実上直列化していたが、detach 化で C-t C-r の
# 連打や auto-restore との重なりで restore.sh が並行実行できるようになった。並行すると
# 復元中フラグ (@tt-restore-*) と pane 生成が競合する (2026-07-30 セルフレビューで検出)。
mkdir -p "$TT_RESTORE_STATE_DIR" 2>/dev/null || true
LOCK_DIR="$TT_RESTORE_STATE_DIR/lock"
# 🚨 ここでは tt_lock_sweep_stale を呼ばない。この経路の lock は `<dir>/lock` の 1 個だけで
#   pid を名前に持たず、掃除を後付けすると「今まで掃除しなかった経路が掃除を始める」= 挙動変更に
#   なる (issue 078 で意図的に分けた)。取り残しは下の「owner 不在なら奪う」で回収される。
# `|| tt_lock_rc=$?` の形にしておく (素の `rc=$?` は後から `set -e` が入ると、rc を読む前に
# スクリプトごと死ぬ = 復元が無音で止まる)。
tt_lock_rc=0
tt_lock_acquire "$LOCK_DIR" || tt_lock_rc=$?
if [ "$tt_lock_rc" -eq 1 ]; then
  # owner 判定は pid だけで行わない (pid 再利用で「実行中」と誤認すると手動復元が永久に拒否される)
  tt_trigger_log "restore-skipped reason=already-running owner=$(cat "$LOCK_DIR/pid" 2>/dev/null | cut -f1) epoch=$(date +%s)"
  exit 0
elif [ "$tt_lock_rc" -ne 0 ]; then
  tt_trigger_log "restore-aborted reason=lock-failed epoch=$(date +%s)"
  exit 0
fi
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
release_lock() { tt_lock_release_if_owner "$LOCK_DIR"; }
trap release_lock EXIT

# restore.sh は @resurrect-restore-script-path から解決する (ハードコードすると vendor
# 移動で silent に壊れる。tmux_restore_confirm.sh / _tt_wait_for_restore と同じ出典)
restore="$(tmux show -gqv @resurrect-restore-script-path 2>/dev/null)"
if [ -z "$restore" ] || [ ! -f "$restore" ]; then
  tt_trigger_log "restore-aborted reason=no-restore-script epoch=$(date +%s)"
  exit 0
fi

tt_trigger_log "restore-manual-begin epoch=$(date +%s)"

# 復元前に archive の完全性を確かめる。壊れていても復元は続行するが (layout だけでも戻す価値が
# ある)、記録は残す。upstream は archive 展開の失敗を検証せず rc=0 で完走するため、これが無いと
# 「window は全部戻ったのに全 pane の scrollback が空」が完全に silent になる (実証 2026-07-30)。
tt_archive="$(tt_resurrect_dir)/pane_contents.tar.gz"
if [ "$(tmux show -gqv @resurrect-capture-pane-contents 2>/dev/null)" = "on" ] \
   && [ -f "$tt_archive" ] && ! gzip -t "$tt_archive" 2>/dev/null; then
  tt_trigger_log "restore-archive-broken path=$tt_archive epoch=$(date +%s)"
fi

# 成否判定の基準を自分の実行に閉じる。@tt-restore-complete はグローバルで sticky なため、
# 前回成功の 1 が残っている窓で restore.sh が pre-restore-all 到達前に死ぬと「成功」に見え、
# in-progress フラグの掃除も skip する = runner が消すはずだった silent 途中死そのものになる
# (レビュー指摘 2026-07-30)。開始時に自分で降ろしてから実行する。
tmux set-option -gu @tt-restore-complete 2>/dev/null || true

# サーバ死亡時の SIGTERM でも後始末 (フラグ掃除 + 記録 + lock 解放) を実行してから終わる。
# 🚨 この trap は上の release_lock の EXIT trap を置き換えるため、lock 解放を内側に含めること
# (trap は同一シグナルで後勝ち。分けると lock が残り復元が二度と走らなくなる)。
finished=0
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
cleanup() {
  if [ "$finished" -ne 1 ]; then
    # post-restore-all 到達済み (@tt-restore-complete=1) なら正常系。未到達なら途中死。
    if [ "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" != "1" ]; then
      tmux set-option -g @tt-restore-in-progress 0 2>/dev/null || true
      tt_trigger_log "restore-aborted reason=interrupted epoch=$(date +%s)"
    fi
    finished=1
  fi
  release_lock
}
trap cleanup TERM INT EXIT

bash "$restore" >/dev/null 2>&1
rc=$?

if [ "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" = "1" ]; then
  tt_trigger_log "restore-end rc=$rc epoch=$(date +%s)"
  # 「完走した」と「全部復元された」は別。突合して欠落を記録する (rc=0 でも部分復元はありうる)
  "$SCRIPT_DIR/tmux_verify_restore.sh" >/dev/null 2>&1 || true
else
  tmux set-option -g @tt-restore-in-progress 0 2>/dev/null || true
  tt_trigger_log "restore-aborted reason=rc-$rc epoch=$(date +%s)"
fi
finished=1
exit 0
