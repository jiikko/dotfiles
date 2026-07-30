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

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"

log_line() {
  { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" \
      && printf '%s\t%s\n' "$(date +%FT%T)" "$1" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
}

# restore.sh は @resurrect-restore-script-path から解決する (ハードコードすると vendor
# 移動で silent に壊れる。tmux_restore_confirm.sh / _tt_wait_for_restore と同じ出典)
restore="$(tmux show -gqv @resurrect-restore-script-path 2>/dev/null)"
if [ -z "$restore" ] || [ ! -f "$restore" ]; then
  log_line "restore-aborted reason=no-restore-script epoch=$(date +%s)"
  exit 0
fi

log_line "restore-manual-begin epoch=$(date +%s)"

# サーバ死亡時の SIGTERM でも後始末 (フラグ掃除 + 記録) を実行してから終わる
finished=0
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
cleanup() {
  [ "$finished" -eq 1 ] && return
  # post-restore-all 到達済み (@tt-restore-complete=1) なら正常系。未到達なら途中死。
  if [ "$(tmux show -gqv @tt-restore-complete 2>/dev/null)" != "1" ]; then
    tmux set-option -g @tt-restore-in-progress 0 2>/dev/null || true
    log_line "restore-aborted reason=interrupted epoch=$(date +%s)"
  fi
  finished=1
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
