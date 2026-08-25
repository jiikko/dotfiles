#!/usr/bin/env bash
# scripts/tmux_log_restore_hook.sh (復元 hook の観測ログ書き込み) の unit テスト。
#
# なぜ必要か (issue 079): この 2 行 (restore-start / restore-end) は _tmux.conf のインラインに
# あり、**共有の観測ログに書く経路で唯一 default socket ゲートを通っていなかった**。-L の隔離
# テストサーバも同じ conf を source して同じ hook を持つため、テストの復元が本番ログへ
# restore-start / restore-end を注入し、watchdog の死因分類 (verdict) を汚す。
# ここで pin するのは「default socket でだけ書く」= 抽出の目的そのもの。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_log_restore_hook.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"

mkdir -p "$TMP_DIR/bin"
cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
case "$*" in
  *@tt-restore-duration*) printf '%s\n' "${STUB_DURATION:-}" ;;
  *) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"

LOG="$TMP_DIR/trigger.log"
DLOG="$TMP_DIR/duration.log"

hook() {  # $1=pre|post、以降は env で制御
  : > "$LOG"; : > "$DLOG"
  TT_TRIGGER_LOG="$LOG" TT_DURATION_LOG="$DLOG" \
    PATH="$STUB_PATH" "$SCRIPT" "$1" >/dev/null 2>&1 || return $?
}

printf '\n=== restore hook の観測ログ ===\n\n'

# --- (1) default socket: pre が restore-start を書く ------------------------------------
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" hook pre
grep -qE "${TT_TAB}restore-start epoch=[0-9]+$" "$LOG" \
  || { printf '✗ restore-start が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ pre: restore-start を記録する\n'

# --- (2) default socket: post が duration と restore-end を書く -------------------------
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_DURATION=42 hook post
grep -qE "${TT_TAB}restore-end rc=0 epoch=[0-9]+$" "$LOG" \
  || { printf '✗ restore-end が無い:\n'; cat "$LOG"; exit 1; }
grep -qE "${TT_TAB}restore=42s$" "$DLOG" \
  || { printf '✗ duration ログが無い:\n'; cat "$DLOG"; exit 1; }
printf '✓ post: restore-end と所要時間を記録する\n'

# --- (3) 非 default socket では 1 行も書かない (この抽出の本題) --------------------------
# ⚠️ ここが緩むと -L の隔離テストサーバの復元が本番ログへ混ざり、watchdog の verdict が汚れる
for phase in pre post; do
  STUB_SOCKET_PATH="/nowhere/tmux-501/lab" STUB_DURATION=7 hook "$phase"
  [ ! -s "$LOG" ] || { printf '✗ 非 default socket なのに観測ログへ書いた (%s):\n' "$phase"; cat "$LOG"; exit 1; }
  [ ! -s "$DLOG" ] || { printf '✗ 非 default socket なのに duration ログへ書いた (%s):\n' "$phase"; cat "$DLOG"; exit 1; }
done
printf '✓ 非 default socket (テストサーバ) では 1 行も書かない\n'

# --- (4) duration が数値でなければ 0 に落とす ------------------------------------------
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_DURATION="" hook post
grep -qE "${TT_TAB}restore=0s$" "$DLOG" \
  || { printf '✗ duration 未設定時に 0 へ落ちていない:\n'; cat "$DLOG"; exit 1; }
printf '✓ duration が読めないときは 0 として記録する\n'

# --- (5) 未知の引数は無音で無視する (hook の失敗で復元を止めない) -----------------------
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" hook bogus
[ ! -s "$LOG" ] || { printf '✗ 未知の引数で書き込んだ:\n'; cat "$LOG"; exit 1; }
printf '✓ 未知の引数は無音で無視する\n'

printf '\nAll restore-hook log tests passed successfully!\n'
