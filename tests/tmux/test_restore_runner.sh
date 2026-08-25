#!/usr/bin/env bash
# scripts/tmux_restore_runner.sh (手動復元の detach 実行体) の unit テスト。
#
# runner の存在理由は「復元の途中死を silent にしない」(2026-07-30 の 22/29 部分復元が
# 完全に無記録だった)。よって pin するのは:
#   (1) 正常完了 (post-restore-all 到達 = @tt-restore-complete=1) → restore-end を記録
#   (2) 途中死 (complete 未設定) → @tt-restore-in-progress を掃除し restore-aborted を記録
#   (3) restore.sh 未解決 → restore-aborted reason=no-restore-script
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_restore_runner.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$*" in
  *@resurrect-restore-script-path*) printf '%s\n' "${STUB_RESTORE:-}" ;;
  *"show -gqv @tt-restore-complete"*) printf '%s\n' "${STUB_COMPLETE:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"

cat > "$TMP_DIR/bin/fake_restore.sh" <<'EOS'
#!/bin/sh
echo "restore ran" >> "$CALLS"
EOS
chmod +x "$TMP_DIR/bin/fake_restore.sh"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
# owner は production と同じ書式で作る (書式をテスト側へ写すと、書式変更に追従できない fixture になる)
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$ROOT_DIR/scripts/lib/tmux_resurrect_guards.sh"

LIVE_PIDS=()
spawn_live() { ( trap - EXIT; exec sleep 300 ) & LIVE_PIDS+=("$!"); REPLY_PID="$!"; }
cleanup_all() { local p; for p in ${LIVE_PIDS+"${LIVE_PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done; rm -rf "$TMP_DIR"; }
trap cleanup_all EXIT

# --- (1) 正常完了 -----------------------------------------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 正常系で exit %s\n' "$RC"; exit 1; }
assert_called "restore ran" "restore.sh が実行される"
grep -qE '	restore-manual-begin epoch=[0-9]+' "$LOG" || { printf '✗ restore-manual-begin が無い:\n'; cat "$LOG"; exit 1; }
grep -qE '	restore-end rc=0 epoch=[0-9]+' "$LOG" || { printf '✗ restore-end が無い:\n'; cat "$LOG"; exit 1; }
assert_not_called "set-option -g @tt-restore-in-progress 0" "正常完了ではフラグ掃除しない (post hook の責務)"
printf '✓ 正常完了: begin/end が記録される\n'

# --- (2) 途中死 (complete 未設定のまま restore が返った) --------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE="" \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 途中死系で exit %s\n' "$RC"; exit 1; }
assert_called "set-option -g @tt-restore-in-progress 0" "途中死で in-progress フラグを掃除する"
grep -qE '	restore-aborted reason=rc-0 epoch=[0-9]+' "$LOG" \
  || { printf '✗ restore-aborted (reason=rc-N) が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ 途中死: フラグ掃除 + restore-aborted を記録\n'

# --- (3) restore.sh 未解決 ---------------------------------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_RESTORE="" run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 未解決系で exit %s\n' "$RC"; exit 1; }
grep -q 'restore-aborted reason=no-restore-script' "$LOG" \
  || { printf '✗ no-restore-script が記録されない:\n'; cat "$LOG"; exit 1; }
assert_not_called "restore ran" "未解決時は何も実行しない"
printf '✓ restore.sh 未解決: 記録のみで無害終了\n'

# --- (4) 先任が実行中なら復元しない (tt_lock_acquire rc=1) -------------------------------
# ⚠️ この rc 分岐は issue 078 の統合で新設したもので、**どのテストからも踏まれていなかった**
#   (敵対レビューの指摘)。`|| tt_lock_rc=$?` を `&& ...` に変える 1 文字のタイポで lock を
#   取らないまま restore.sh を走らせる = 二重復元になるが、旧 3 ケースは全部 green のまま通る。
reset_calls; : > "$LOG"
STATE="$TMP_DIR/rstate"; rm -rf "$STATE"; mkdir -p "$STATE/lock"
spawn_live; tt_lock_write_owner "$STATE/lock" "$REPLY_PID"
TT_TRIGGER_LOG="$LOG" TT_RESTORE_STATE_DIR="$STATE" \
  STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
  run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 先任生存時に exit %s\n' "$RC"; exit 1; }
assert_not_called "restore ran" "先任が実行中なら restore.sh を走らせない"
grep -q 'restore-skipped reason=already-running' "$LOG" \
  || { printf '✗ restore-skipped reason=already-running が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ 先任が実行中: 復元せず skip を記録\n'

# --- (5) lock を取れないなら復元しない (tt_lock_acquire rc=2) ----------------------------
if [ "$(id -u)" = 0 ]; then
  printf '⚠ root では書き込み不可ディレクトリを作れないため lock 取得失敗のテストを skip した\n'
else
  reset_calls; : > "$LOG"
  RO_STATE="$TMP_DIR/rstate_ro"; rm -rf "$RO_STATE"; mkdir -p "$RO_STATE"; chmod 500 "$RO_STATE"
  TT_TRIGGER_LOG="$LOG" TT_RESTORE_STATE_DIR="$RO_STATE" \
    STUB_RESTORE="$TMP_DIR/bin/fake_restore.sh" STUB_COMPLETE=1 \
    run "$STUB_PATH" "$SCRIPT"
  chmod 700 "$RO_STATE"
  [[ "$RC" -eq 0 ]] || { printf '✗ lock 取得失敗時に exit %s\n' "$RC"; exit 1; }
  assert_not_called "restore ran" "lock を取れないなら restore.sh を走らせない"
  grep -q 'restore-aborted reason=lock-failed' "$LOG" \
    || { printf '✗ restore-aborted reason=lock-failed が無い:\n'; cat "$LOG"; exit 1; }
  printf '✓ lock を取れない: 復元せず理由を記録\n'
fi

printf '\nAll restore-runner tests passed successfully!\n'
