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

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
TT_RESTORE_STATE_DIR="${TT_RESTORE_STATE_DIR:-$HOME/.cache/tt-restore-run}"

log_line() {
  { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" \
      && printf '%s\t%s\n' "$(date +%FT%T)" "$1" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
}

# ---- 単一実行ガード -------------------------------------------------------------------
# ⚠️ popup 内同期実行だった旧実装は popup が事実上直列化していたが、detach 化で C-t C-r の
# 連打や auto-restore との重なりで restore.sh が並行実行できるようになった。並行すると
# 復元中フラグ (@tt-restore-*) と pane 生成が競合する (2026-07-30 セルフレビューで検出)。
mkdir -p "$TT_RESTORE_STATE_DIR" 2>/dev/null || true
LOCK_DIR="$TT_RESTORE_STATE_DIR/lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  owner="$(cat "$LOCK_DIR/pid" 2>/dev/null)"
  if [ -n "$owner" ] && kill -0 "$owner" 2>/dev/null; then
    log_line "restore-skipped reason=already-running owner=$owner epoch=$(date +%s)"
    exit 0
  fi
  # owner 不在 = 前回の取り残し。奪って続行する (復元が二度と走らない方が害が大きい)
  rm -rf "$LOCK_DIR" 2>/dev/null
  mkdir "$LOCK_DIR" 2>/dev/null || { log_line "restore-aborted reason=lock-failed epoch=$(date +%s)"; exit 0; }
fi
printf '%s\n' "$$" > "$LOCK_DIR/pid" 2>/dev/null || true
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
release_lock() { rm -rf "$LOCK_DIR" 2>/dev/null; }
trap release_lock EXIT

# restore.sh は @resurrect-restore-script-path から解決する (ハードコードすると vendor
# 移動で silent に壊れる。tmux_restore_confirm.sh / _tt_wait_for_restore と同じ出典)
restore="$(tmux show -gqv @resurrect-restore-script-path 2>/dev/null)"
if [ -z "$restore" ] || [ ! -f "$restore" ]; then
  log_line "restore-aborted reason=no-restore-script epoch=$(date +%s)"
  exit 0
fi

log_line "restore-manual-begin epoch=$(date +%s)"

# 成否判定の基準を自分の実行に閉じる。@tt-restore-complete はグローバルで sticky なため、
# 前回成功の 1 が残っている窓で restore.sh が pre-restore-all 到達前に死ぬと「成功」に見え、
# in-progress フラグの掃除も skip する = runner が消すはずだった silent 途中死そのものになる
# (レビュー指摘 2026-07-30)。開始時に自分で降ろしてから実行する。
tmux set-option -gu @tt-restore-complete 2>/dev/null || true

# サーバ死亡時の SIGTERM でも後始末 (フラグ掃除 + 記録 + lock 解放) を実行してから終わる。
# ⚠️ この trap は上の release_lock の EXIT trap を置き換えるため、lock 解放を内側に含めること
# (trap は同一シグナルで後勝ち。分けると lock が残り復元が二度と走らなくなる)。
finished=0
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
cleanup() {
  if [ "$finished" -ne 1 ]; then
    # post-restore-all 到達済み (@tt-restore-complete=1) なら正常系。未到達なら途中死。
    if [ "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" != "1" ]; then
      tmux set-option -g @tt-restore-in-progress 0 2>/dev/null || true
      log_line "restore-aborted reason=interrupted epoch=$(date +%s)"
    fi
    finished=1
  fi
  release_lock
}
trap cleanup TERM INT EXIT

bash "$restore" >/dev/null 2>&1
rc=$?

if [ "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" = "1" ]; then
  log_line "restore-end rc=$rc epoch=$(date +%s)"
else
  tmux set-option -g @tt-restore-in-progress 0 2>/dev/null || true
  log_line "restore-aborted reason=rc-$rc epoch=$(date +%s)"
fi
finished=1
exit 0
