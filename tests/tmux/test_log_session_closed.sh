#!/usr/bin/env bash
# scripts/tmux_log_session_closed.sh (session-closed hook の観測ロガー) の unit テスト。
#
# このログはサーバ突然 exit の切り分け (正常な連鎖 exit か外因か) の一次証拠なので、
# 「書式が壊れて後追い不能」「hook 内で非 0 になり run-shell エラーが出る」の 2 つを
# stub tmux + 隔離 HOME で pin する。実 tmux サーバ・実 ~/.cache には触れない。
# 2026-07-30 から: epoch フィールド (watchdog の死因分類が直近性判定に使う) と
# default socket ゲート (テストサーバのイベント混入防止) も対象。
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
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"

# display-message (socket gate) と list-sessions で応答を分ける stub
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display-message)
    case "$*" in
      *socket_path*) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
      *)             printf '%s\n' "99999" ;;   # #{pid} (サーバ世代の同定用)
    esac ;;
  *)
    [ -n "${STUB_LS_EXIT:-}" ] && exit "$STUB_LS_EXIT"
    printf '%b' "${STUB_SESSIONS:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/home/.cache/tt-restore-trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

HOME="$TMP_DIR/home" STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\nb: 2 windows\nc: 1 windows\n' run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 正常系で exit %s (hook 用に常時 0 のはず)\n' "$RC"; exit 1; }
grep -qE '^[0-9T:-]+	session-closed pid=[0-9]+ remaining=3 epoch=[0-9]+$' "$LOG" \
  || { printf '✗ ログ書式が想定と違う:\n'; cat "$LOG" 2>/dev/null; exit 1; }
printf '✓ サーバ pid + 残セッション数 + epoch がタブ区切り書式で追記される (remaining=3)\n'

HOME="$TMP_DIR/home" STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_LS_EXIT=1 run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ list-sessions 失敗で exit %s (0 のはず)\n' "$RC"; exit 1; }
grep -qE 'session-closed pid=[0-9]+ remaining=0' "$LOG" \
  || { printf '✗ list-sessions 失敗時に remaining=0 が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ list-sessions 失敗 (サーバ消滅レース) でも exit 0 + remaining=0 を記録\n'

rm -f "$LOG"
HOME="$TMP_DIR/home" STUB_SOCKET_PATH="/nowhere/tmux-501/lab" \
  STUB_SESSIONS='a: 1 windows\n' run "$STUB_PATH" "$SCRIPT"
[[ "$RC" -eq 0 ]] || { printf '✗ 非 default socket で exit %s (0 のはず)\n' "$RC"; exit 1; }
[[ ! -s "$LOG" ]] || { printf '✗ 非 default socket なのにログが書かれた:\n'; cat "$LOG"; exit 1; }
printf '✓ 非 default socket (テストサーバ) ではログを書かない\n'

mkdir -p "$TMP_DIR/ro"; chmod 555 "$TMP_DIR/ro"
HOME="$TMP_DIR/ro/home" STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SESSIONS='a: 1 windows\n' run "$STUB_PATH" "$SCRIPT"
chmod 755 "$TMP_DIR/ro"
[[ "$RC" -eq 0 ]] || { printf '✗ ログ書き込み不能でも exit 0 のはず (RC=%s)\n' "$RC"; exit 1; }
printf '✓ ログ書き込み失敗 (HOME 作成不能) でも exit 0 (hook を汚さない || true ガード)\n'

printf '\nAll log-session-closed tests passed successfully!\n'
