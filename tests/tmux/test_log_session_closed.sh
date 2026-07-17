#!/usr/bin/env bash
# scripts/tmux_log_session_closed.sh (session-closed hook の観測ロガー) の unit テスト。
#
# このログはサーバ突然 exit の切り分け (正常な連鎖 exit か外因か) の一次証拠なので、
# 「書式が壊れて後追い不能」「hook 内で非 0 になり run-shell エラーが出る」の 2 つを
# stub tmux + 隔離 HOME で pin する。実 tmux サーバ・実 ~/.cache には触れない。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_log_session_closed.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/home"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
[ -n "${STUB_LS_EXIT:-}" ] && exit "$STUB_LS_EXIT"
printf '%b' "${STUB_SESSIONS:-}"
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/home/.cache/tt-restore-trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

HOME="$TMP_DIR/home" STUB_SESSIONS='a: 1 windows\nb: 2 windows\nc: 1 windows\n' run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 正常系で exit %s (hook 用に常時 0 のはず)\n' "$RC"; exit 1; }
grep -qE '^[0-9T:-]+	session-closed remaining=3$' "$LOG" \
  || { printf '✗ ログ書式が想定と違う:\n'; cat "$LOG" 2>/dev/null; exit 1; }
printf '✓ 残セッション数がタブ区切り書式で追記される (remaining=3)\n'

HOME="$TMP_DIR/home" STUB_LS_EXIT=1 run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ list-sessions 失敗で exit %s (0 のはず)\n' "$RC"; exit 1; }
grep -q 'session-closed remaining=0' "$LOG" \
  || { printf '✗ list-sessions 失敗時に remaining=0 が記録されない\n'; exit 1; }
printf '✓ list-sessions 失敗 (サーバ消滅レース) でも exit 0 + remaining=0 を記録\n'

mkdir -p "$TMP_DIR/ro"; chmod 555 "$TMP_DIR/ro"
HOME="$TMP_DIR/ro/home" STUB_SESSIONS='a: 1 windows\n' run "$STUB_PATH" "$SCRIPT"
chmod 755 "$TMP_DIR/ro"
[[ "$RC" -eq 0 ]] || { printf '✗ ログ書き込み不能でも exit 0 のはず (RC=%s)\n' "$RC"; exit 1; }
printf '✓ ログ書き込み失敗 (HOME 作成不能) でも exit 0 (hook を汚さない || true ガード)\n'

printf '\nAll log-session-closed tests passed successfully!\n'
