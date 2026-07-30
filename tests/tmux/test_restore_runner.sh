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

printf '\nAll restore-runner tests passed successfully!\n'
