#!/usr/bin/env bash
# scripts/tmux_log_kill_command.sh (kill-server/kill-session alias shim) の unit テスト。
#
# この shim は kill コマンドの実行経路上に同期で挟まるため、
#   (1) どんな入力でも exit 0 (非 0 だと run-shell エラーが pane に積まれ、kill も阻害しうる)
#   (2) 発火条件 (kill-server は常に保存 / kill-session は残り 1 以下のみ保存)
#   (3) default socket 以外では完全 no-op (共有ログを汚さない)
# の 3 点を stub tmux + 隔離ログで pin する。実 tmux サーバ・実 ~/.cache には触れない。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_log_kill_command.sh"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin"
DEFAULT_SOCK="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"

# subcommand を見て応答を変える stub tmux (display-message は socket_path / pid、
# list-sessions はセッション一覧、show は保存スクリプトパス)
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display-message)
    case "$*" in
      *socket_path*) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
      *) printf '%s\n' "99999" ;;
    esac ;;
  list-sessions) printf '%b' "${STUB_SESSIONS:-}" ;;
  show) printf '%s\n' "${STUB_SAVE_SCRIPT:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"

# 呼び出しを記録するだけの stub 保存スクリプト
cat > "$TMP_DIR/bin/fake_save.sh" <<'EOS'
#!/bin/sh
echo "save $*" >> "$CALLS"
EOS
chmod +x "$TMP_DIR/bin/fake_save.sh"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

# --- (a) default socket で kill-server → 保存が走り kill-cmd 行が書かれる -------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 \
  STUB_SOCKET_PATH="$DEFAULT_SOCK" STUB_SESSIONS='a: 1 windows\nb: 2 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ kill-server 正常系で exit %s (0 のはず)\n' "$RC"; exit 1; }
assert_called "save quiet" "kill-server で直前保存が quiet 起動される"
grep -qE '	kill-cmd cmd=kill-server sessions=2 save=ok epoch=[0-9]+ issuer=' "$LOG" \
  || { printf '✗ kill-cmd 行の書式が想定と違う:\n'; cat "$LOG"; exit 1; }
printf '✓ kill-cmd 行 (cmd/sessions/save/epoch/issuer) がタブ区切りで追記される\n'

# --- (b) 残り 3 セッションへの kill-session → 保存しない (サーバは存続するため) --------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\nb: 2 windows\nc: 1 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
[[ "$RC" -eq 0 ]] || { printf '✗ kill-session で exit %s\n' "$RC"; exit 1; }
assert_not_called "save quiet" "複数セッション時の kill-session は保存しない"
grep -q 'kill-cmd cmd=kill-session sessions=3 save=skipped' "$LOG" \
  || { printf '✗ kill-session の記録が無い/書式相違:\n'; cat "$LOG"; exit 1; }
printf '✓ kill-session (残 3) は記録のみで保存 skip\n'

# --- (c) 最後の 1 セッションへの kill-session → exit-empty でサーバごと死ぬので保存 ----
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" TT_KILL_SAVE_WAIT_SECONDS=2 STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='solo: 1 windows\n' \
  STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-session
assert_called "save quiet" "最後の 1 セッションへの kill-session は直前保存する"

# --- (d) default socket 以外 → 完全 no-op (ログも保存も無し) ---------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="/nowhere/tmux-501/testsock" \
  STUB_SESSIONS='a: 1 windows\n' STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ 非 default socket で exit %s (0 のはず)\n' "$RC"; exit 1; }
assert_not_called "save quiet" "非 default socket では保存しない"
[[ ! -s "$LOG" ]] || { printf '✗ 非 default socket でログが書かれた:\n'; cat "$LOG"; exit 1; }
printf '✓ 非 default socket (テストサーバ) では完全 no-op\n'

# --- (e) 保存スクリプト未解決でも kill を阻害しない ------------------------------------
reset_calls; : > "$LOG"
TT_TRIGGER_LOG="$LOG" STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  STUB_SESSIONS='a: 1 windows\n' STUB_SAVE_SCRIPT="" \
  run "$STUB_PATH" "$SCRIPT" kill-server
[[ "$RC" -eq 0 ]] || { printf '✗ 保存スクリプト不在で exit %s (0 のはず)\n' "$RC"; exit 1; }
grep -q 'save=no-save-script' "$LOG" \
  || { printf '✗ save=no-save-script が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ 保存スクリプト不在でも exit 0 + save=no-save-script を記録\n'

printf '\nAll log-kill-command tests passed successfully!\n'
