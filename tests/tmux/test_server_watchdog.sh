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
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"

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
  TT_WATCHDOG_INTERVAL=0.05 TT_VERDICT_WINDOW=120 STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" \
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
tt_spawn_fake_proc; FAKE="$REPLY_PID"
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

# --- (2) 同一サーバ pid の kill-cmd が直近にある → verdict=kill-server-command ---------
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
printf '%s\tkill-cmd cmd=kill-server pid=%s sessions=9 save=ok epoch=%s issuer=test\n' \
  "$(date +%FT%T)" "$FAKE" "$(date +%s)" >> "$LOG"
wd_env "$FAKE" &
WD=$!
sleep 0.3
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "kill-cmd 相関"
wait "$WD" 2>/dev/null || true
grep -q "server-death pid=$FAKE .*verdict=kill-server-command" "$LOG" \
  || { printf '✗ 同一 pid の kill-cmd 直近ありで verdict が kill-server-command にならない:\n'; cat "$LOG"; exit 1; }
printf '✓ 同一サーバ pid の kill-cmd と相関して verdict=kill-server-command\n'

# --- (2b) 別サーバ世代の kill-cmd では kill と誤分類しない (世代跨ぎ誤分類の回帰テスト) --
# レビューで実証された欠陥: 時間窓だけで相関していたため、前世代を kill-server で落とした後に
# 新サーバが外因死すると verdict=kill-server-command になり、捜査方向が反転していた。
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
printf '%s\tkill-cmd cmd=kill-server pid=%s sessions=9 save=ok epoch=%s issuer=other-generation\n' \
  "$(date +%FT%T)" "$((FAKE + 100000))" "$(date +%s)" >> "$LOG"
wd_env "$FAKE" &
WD=$!
sleep 0.3
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "別世代の kill-cmd"
wait "$WD" 2>/dev/null || true
grep -q "server-death pid=$FAKE .*verdict=external-signal-or-crash" "$LOG" \
  || { printf '✗ 別サーバ pid の kill-cmd を自分のものとして誤分類した:\n'; cat "$LOG"; exit 1; }
printf '✓ 別サーバ世代の kill-cmd では誤分類せず external-signal-or-crash\n'

# --- (3) 二重起動ガード: 先任が生きている間、後発は即退く ------------------------------
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
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

# --- (3b) pid 再利用の残骸 lock で「watchdog が 1 つも張られない」ことを防ぐ -------------
# 2026-07-30 の監査で実証された欠陥の回帰テスト: owner を pid の生存だけで判定していたため、
# 残骸 lock の watcher pid が別プロセスに再利用されると新世代の watchdog が無音で退き、
# サーバが死んでも死亡記録が残らなかった (観測装置が丸ごと不発)。
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
# 「生きているが watchdog ではない別プロセス」を owner に持つ残骸 lock を作る。
# 起動時刻を偽の値にしておくと、同一性判定が pid 再利用を見抜けるはず。
tt_spawn_fake_proc; IMPOSTOR="$REPLY_PID"
mkdir -p "$TMP_DIR/wd/$FAKE.lock"
printf '%s\t%s\n' "$IMPOSTOR" "Mon Jan  1 00:00:00 2000" > "$TMP_DIR/wd/$FAKE.lock/pid"
wd_env "$FAKE" &
WD=$!
sleep 0.5
owner_now="$(cut -f1 "$TMP_DIR/wd/$FAKE.lock/pid" 2>/dev/null)"
[ "$owner_now" != "$IMPOSTOR" ] \
  || { printf '✗ pid 再利用の残骸 lock に退いた (watchdog が張られない = 死亡記録が残らない)\n'; exit 1; }
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "pid 再利用の残骸 lock を乗り越えて看取る"
wait "$WD" 2>/dev/null || true
kill "$IMPOSTOR" 2>/dev/null
printf '✓ pid 再利用の残骸 lock を見抜いて看取りを開始する (装置不在に倒れない)\n'

# --- (4) 非 default socket → 監視しない (lock も作らない) ------------------------------
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
  TT_WATCHDOG_INTERVAL=0.05 STUB_SOCKET_PATH="/nowhere/tmux-501/lab" \
  PATH="$STUB_PATH" "$SCRIPT" "$FAKE" "/fake/socket" "1"
[ ! -d "$TMP_DIR/wd/$FAKE.lock" ] || { printf '✗ 非 default socket なのに監視を始めた\n'; exit 1; }
kill "$FAKE" 2>/dev/null
printf '✓ 非 default socket (テストサーバ) は看取らない\n'

# --- (5) lock を作れないときは無音で消えない -------------------------------------------
# 🚨 rc=2 (取得不能) を rc=1 (先任生存) と畳むと **watchdog が張られない = 死因が二度と
#   記録されない**のに 1 行も残らない (敵対レビューが chmod 500 で実測)。
if [ "$(id -u)" = 0 ]; then
  printf '🚨 root では書き込み不可ディレクトリを作れないため lock 取得失敗のテストを skip した\n'
else
  : > "$LOG"
  RO_WD="$TMP_DIR/wd_ro"; rm -rf "$RO_WD"; mkdir -p "$RO_WD"; chmod 500 "$RO_WD"
  tt_spawn_fake_proc; FAKE="$REPLY_PID"
  TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$RO_WD" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
    TT_WATCHDOG_INTERVAL=0.05 STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" \
    PATH="$STUB_PATH" "$SCRIPT" "$FAKE" "/fake/socket" "1"
  chmod 700 "$RO_WD"
  kill "$FAKE" 2>/dev/null
  grep -qE "${TT_TAB}watchdog-aborted reason=lock-failed server=[0-9]+ epoch=[0-9]+" "$LOG" \
    || { printf '✗ lock 取得失敗が観測ログに残っていない (装置不在が無音):\n'; cat "$LOG"; exit 1; }
  printf '✓ lock を取れないときは理由を観測ログに残す\n'
fi

# --- (6) 死んだサーバの stale lock を掃除してから張る -----------------------------------
# 🚨 掃除の呼び出し**そのもの**を pin する。関数の中身は guards.sh の unit テストが見ているが、
#   watchdog 側の呼び出し行を消しても他のテストは全部 green だった (敵対レビューの指摘)。
: > "$LOG"
tt_free_pid; DEADSRV="$REPLY_PID"
mkdir -p "$TMP_DIR/wd/$DEADSRV.lock"
tt_free_pid $(( DEADSRV - 1 )); printf '%s\n' "$REPLY_PID" > "$TMP_DIR/wd/$DEADSRV.lock/pid"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
wd_env "$FAKE" & WD=$!
i=0
while [ "$i" -lt 100 ]; do
  [ -d "$TMP_DIR/wd/$DEADSRV.lock" ] || break
  sleep 0.1; i=$((i + 1))
done
[ ! -d "$TMP_DIR/wd/$DEADSRV.lock" ] \
  || { printf '✗ 死んだサーバの stale lock を掃除していない (watchdog 側の呼び出しが死んでいる)\n'; exit 1; }
kill "$FAKE" 2>/dev/null
wait "$WD" 2>/dev/null || true
printf '✓ 死んだサーバの stale lock を掃除してから監視を張る\n'

# --- (7) スナップショット健全性の状態遷移を観測ログに残す -------------------------------
# 🚨 この 2 行 (snapshot-health ng / ok) は **どのテストも見ていなかった**ため、issue 079 で
#   共有 seam (tt_trigger_log) へ寄せても移行が無検証だった。状態が変わったときだけ書く、
#   という契約ごとここで固定する (毎回書くと toast も観測ログも騒がしくなる)。
: > "$LOG"
rm -f "$TMP_DIR/health_ok"
cat > "$TMP_DIR/bin/fake_health.sh" <<'EOS'
#!/bin/sh
[ -f "$HEALTH_OK_FLAG" ] && exit 0
echo "スナップショット異常: セッション 0 件"
exit 1
EOS
chmod +x "$TMP_DIR/bin/fake_health.sh"

wait_for_line() {  # $1=grep パターン $2=説明
  local i=0
  while [ "$i" -lt 100 ]; do
    grep -qE "$1" "$LOG" 2>/dev/null && return 0
    sleep 0.1; i=$((i + 1))
  done
  printf '✗ %s: 10s 待っても出ない (パターン: %s)\n--- log ---\n' "$2" "$1"; cat "$LOG" 2>/dev/null
  exit 1
}

tt_spawn_fake_proc; FAKE="$REPLY_PID"
TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
  TT_WATCHDOG_INTERVAL=0.05 TT_VERDICT_WINDOW=120 STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" \
  TT_HEALTH_CHECK_INTERVAL=1 TT_HEALTH_SCRIPT="$TMP_DIR/bin/fake_health.sh" \
  TT_TOAST="$TMP_DIR/bin/nonexistent-toast" HEALTH_OK_FLAG="$TMP_DIR/health_ok" \
  PATH="$STUB_PATH" "$SCRIPT" "$FAKE" "/fake/socket" "1234567890" & WD=$!
wait_for_line "${TT_TAB}snapshot-health ng epoch=[0-9]+ detail=.+" "異常を検知したら記録する"
printf '✓ スナップショット異常を観測ログに記録する\n'
: > "$TMP_DIR/health_ok"
wait_for_line "${TT_TAB}snapshot-health ok epoch=[0-9]+" "回復も記録する"
printf '✓ 回復したときも観測ログに記録する (状態遷移だけ書く)\n'
kill "$FAKE" 2>/dev/null
wait "$WD" 2>/dev/null || true

printf '\nAll server-watchdog tests passed successfully!\n'
