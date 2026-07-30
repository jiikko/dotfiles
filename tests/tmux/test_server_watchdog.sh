#!/usr/bin/env bash
# scripts/tmux_server_watchdog.sh (サーバ死亡の看取り) の unit テスト。
#
# 「サーバ」は sleep プロセスで代用し、kill して死亡検知〜記録〜分類を検証する。
#   (1) 死亡時に server-death 行 + ps スナップショットが書かれる
#   (2) 直近の kill-cmd 行があれば verdict=kill-server-command、無ければ external-signal-or-crash
#   (3) 同一サーバ pid への二重起動は先任が居る限り退く (conf 再 source 対策)
# 実 tmux サーバ・実 ~/.cache には触れない (socket gate は stub tmux が default を返す)。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_server_watchdog.sh"
TMP_DIR="$(mktemp -d)"
FAKE_PIDS=()
cleanup() {
  for p in "${FAKE_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/pslogs" "$TMP_DIR/wd"
DEFAULT_SOCK="$(realpath /tmp 2>/dev/null || echo /tmp)/tmux-$(id -u)/default"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
printf '%s\n' "${STUB_SOCKET_PATH:-}"
EOS
chmod +x "$TMP_DIR/bin/tmux"
STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

wd_env() {  # 共通 env で watchdog を起動する ($1=fake pid)
  TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
  TT_WATCHDOG_INTERVAL=0.05 TT_VERDICT_WINDOW=120 STUB_SOCKET_PATH="$DEFAULT_SOCK" \
  PATH="$STUB_PATH" "$SCRIPT" "$1" "/fake/socket" "1234567890"
}

wait_for_death_line() {  # $1=pid $2=説明。bounded-wait で server-death 行を待つ
  local i=0
  while [ "$i" -lt 100 ]; do
    grep -q "server-death pid=$1 " "$LOG" 2>/dev/null && return 0
    sleep 0.1; i=$((i + 1))
  done
  printf '✗ %s: server-death 行が 10s 待っても書かれない\n--- log ---\n' "$2"; cat "$LOG" 2>/dev/null
  exit 1
}

# --- (1) 外因死: 空ログ状態でサーバ kill → external-signal-or-crash + pslog ------------
: > "$LOG"
sleep 300 & FAKE=$!; FAKE_PIDS+=("$FAKE")
wd_env "$FAKE" &
WD=$!
sleep 0.5
[ -d "$TMP_DIR/wd/$FAKE.lock" ] || { printf '✗ 監視中に lock dir が無い\n'; exit 1; }
printf '✓ 監視開始で lock dir (%s.lock) が作られる\n' "$FAKE"
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "外因死"
wait "$WD" 2>/dev/null || true
grep -qE "server-death pid=$FAKE start=1234567890 socket=/fake/socket verdict=external-signal-or-crash pslog=" "$LOG" \
  || { printf '✗ 外因死の server-death 行の書式/verdict が想定と違う:\n'; cat "$LOG"; exit 1; }
pslog_path="$(sed -n "s/.*pslog=\(.*\)$/\1/p" "$LOG" | tail -1)"
[ -s "$pslog_path" ] || { printf '✗ ps スナップショット (%s) が無い/空\n' "$pslog_path"; exit 1; }
[ ! -d "$TMP_DIR/wd/$FAKE.lock" ] || { printf '✗ 死亡処理後に lock が残っている\n'; exit 1; }
printf '✓ 外因死: verdict=external-signal-or-crash + ps スナップショット + lock 掃除\n'

# --- (2) kill-cmd 直近あり → verdict=kill-server-command -------------------------------
: > "$LOG"
printf '%s\tkill-cmd cmd=kill-server sessions=9 save=ok epoch=%s issuer=test\n' \
  "$(date +%FT%T)" "$(date +%s)" >> "$LOG"
sleep 300 & FAKE=$!; FAKE_PIDS+=("$FAKE")
wd_env "$FAKE" &
WD=$!
sleep 0.3
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "kill-cmd 相関"
wait "$WD" 2>/dev/null || true
grep -q "server-death pid=$FAKE .*verdict=kill-server-command" "$LOG" \
  || { printf '✗ kill-cmd 直近ありで verdict が kill-server-command にならない:\n'; cat "$LOG"; exit 1; }
printf '✓ 直近の kill-cmd 行と相関して verdict=kill-server-command\n'

# --- (3) 二重起動ガード: 先任が生きている間、後発は即退く ------------------------------
: > "$LOG"
sleep 300 & FAKE=$!; FAKE_PIDS+=("$FAKE")
wd_env "$FAKE" &
WD=$!
sleep 0.5
first_pid="$(cat "$TMP_DIR/wd/$FAKE.lock/pid" 2>/dev/null)"
wd_env "$FAKE"   # 後発 (同期実行): 先任生存なので即 exit 0 するはず
second_pid="$(cat "$TMP_DIR/wd/$FAKE.lock/pid" 2>/dev/null)"
[ "$first_pid" = "$second_pid" ] || { printf '✗ 後発が先任の lock を奪った (%s → %s)\n' "$first_pid" "$second_pid"; exit 1; }
printf '✓ 二重起動は先任の lock を奪わず退く\n'
kill "$FAKE" 2>/dev/null
wait "$WD" 2>/dev/null || true

# --- (4) 非 default socket → 監視しない (lock も作らない) ------------------------------
: > "$LOG"
sleep 300 & FAKE=$!; FAKE_PIDS+=("$FAKE")
TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
  TT_WATCHDOG_INTERVAL=0.05 STUB_SOCKET_PATH="/nowhere/tmux-501/lab" \
  PATH="$STUB_PATH" "$SCRIPT" "$FAKE" "/fake/socket" "1"
[ ! -d "$TMP_DIR/wd/$FAKE.lock" ] || { printf '✗ 非 default socket なのに監視を始めた\n'; exit 1; }
kill "$FAKE" 2>/dev/null
printf '✓ 非 default socket (テストサーバ) は看取らない\n'

printf '\nAll server-watchdog tests passed successfully!\n'
