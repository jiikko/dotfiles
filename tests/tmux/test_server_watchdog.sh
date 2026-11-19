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
# watchdog は health の上限に、作ってある src/runtimeout/runtimeout をそのまま使う (自分ではビルドしない)。
# 新しい checkout には無いので、ここで作っておく。作れなければ (13) だけを飛ばす
# shellcheck source=bin/lib/runtimeout.sh
. "$ROOT_DIR/bin/lib/runtimeout.sh"
HAVE_RUNTIMEOUT=1
runtimeout_resolve "$ROOT_DIR" || HAVE_RUNTIMEOUT=0
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

# bounded-wait の実装は tests/lib/wait_until.sh に一本化 (コピペ禁止の理由はそちら)。
# ここは「待ちたいものごとの診断」だけを持つ薄いラッパ。
# shellcheck source=tests/lib/wait_until.sh
. "$ROOT_DIR/tests/lib/wait_until.sh"

# 🚨 診断は待ちたいものに合わせて出し分ける。lock 系の失敗で見たいのは観測ログではなく
#   watchdog ディレクトリの中身 (誰が owner か / そもそも lock が在るか)。
wait_lock() {   # $1=説明, 残り=条件コマンド
  local desc="$1"; shift
  tt_wait_until "$@" && return 0
  printf '✗ %s: 10s 待っても成立しない\n--- %s ---\n' "$desc" "$TMP_DIR/wd"
  ls -laR "$TMP_DIR/wd" 2>/dev/null
  exit 1
}
wait_log() {    # $1=説明, 残り=条件コマンド
  local desc="$1"; shift
  tt_wait_until "$@" && return 0
  printf '✗ %s: 10s 待っても成立しない\n--- log ---\n' "$desc"
  cat "$LOG" 2>/dev/null
  exit 1
}

# --- 述語 (呼ばれるたびに評価し直される) ---
death_line_present() { grep -q "server-death pid=$1 " "$LOG" 2>/dev/null; }
lock_dir_exists()    { [ -d "$TMP_DIR/wd/$1.lock" ]; }
lock_dir_gone()      { [ ! -d "$TMP_DIR/wd/$1.lock" ]; }
lock_pid_written()   { [ -s "$TMP_DIR/wd/$1.lock/pid" ]; }
# 🚨 「pid ファイルが非空」を先に見る。奪取は rm -rf → mkdir → write_owner の順なので
#   (scripts/lib/tmux_resurrect_guards.sh)、書き込み前の窓では cut が空を返し
#   `"" != "$IMPOSTOR"` が**真**になる。その瞬間に抜けると、後続の判定が空文字を相手に
#   vacuous に通る (レビューで 30 回中 17 回再現した)。
lock_owner_taken_over() {
  lock_pid_written "$1" && [ "$(cut -f1 "$TMP_DIR/wd/$1.lock/pid" 2>/dev/null)" != "$2" ]
}
log_matches()        { grep -qE "$1" "$LOG" 2>/dev/null; }

wait_for_death_line() {  # $1=pid $2=説明
  wait_log "$2: server-death 行が書かれない" death_line_present "$1"
}

# --- (1) 外因死: 空ログ状態でサーバ kill → external-signal-or-crash + pslog ------------
: > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
wd_env "$FAKE" &
WD=$!
wait_lock "監視中に lock dir が作られない" lock_dir_exists "$FAKE"
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
wait_lock "kill-cmd 相関: 監視が張られない" lock_dir_exists "$FAKE"
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
wait_lock "別世代の kill-cmd: 監視が張られない" lock_dir_exists "$FAKE"
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
wait_lock "二重起動ガード: 先任が lock に pid を書かない" lock_pid_written "$FAKE"
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
# 成立しなければ wait_lock が exit する = 「残骸 lock に退いた (watchdog が張られない)」。
# 🚨 ここに owner を読み直す assert を重ねない: 到達した時点で必ず真になる死んだ assert で、
#   しかも述語と同じ判定の二重実装になる (mutation-verify-new-tests.md)。
wait_lock "pid 再利用の残骸 lock に退いた (watchdog が張られない = 死亡記録が残らない)" \
  lock_owner_taken_over "$FAKE" "$IMPOSTOR"
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
#   記録されない**のに 1 行も残らない (chmod 500 で実測)。
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
#   watchdog 側の呼び出し行を消しても他のテストは全部 green だった。
: > "$LOG"
tt_free_pid; DEADSRV="$REPLY_PID"
mkdir -p "$TMP_DIR/wd/$DEADSRV.lock"
tt_free_pid $(( DEADSRV - 1 )); printf '%s\n' "$REPLY_PID" > "$TMP_DIR/wd/$DEADSRV.lock/pid"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
wd_env "$FAKE" & WD=$!
wait_lock "死んだサーバの stale lock を掃除していない (watchdog 側の呼び出しが死んでいる)" \
  lock_dir_gone "$DEADSRV"
kill "$FAKE" 2>/dev/null
wait "$WD" 2>/dev/null || true
printf '✓ 死んだサーバの stale lock を掃除してから監視を張る\n'

# --- (7) スナップショット健全性の状態遷移を観測ログに残す -------------------------------
# 🚨 この 2 行 (snapshot-health ng / ok) は **どのテストも見ていない**と、
#   共有 seam (tt_trigger_log) へ寄せても移行が無検証になる。状態が変わったときだけ書く、
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
  wait_log "$2 (パターン: $1)" log_matches "$1"
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

# --- (8) サーバが CPU を使い切り続けたら、その最中の sample と ps を撮る -------------------
# 「サーバ」を CPU を使い切るプロセス (yes) にし、sample は呼ばれた引数を -file の先へ書く stub にする。
# watchdog は env bash で起動するので、手元の PATH 次第で bash 3.2 (EPOCHREALTIME が無く date の秒で測る) と
# bash 5 のどちらでも動く。両方の経路を通すため、見つかった bash ごとに回す。
cat > "$TMP_DIR/bin/fake_sample.sh" <<'EOS2'
#!/bin/sh
# 本物の引数の形: <pid> <秒> -file <path>
[ "$3" = "-file" ] || exit 2
[ -n "${SAMPLE_DELAY:-}" ] && sleep "$SAMPLE_DELAY"  # sleep-ok: stub: 長くかかる sample を演じる
echo "sampled pid=$1 secs=$2" > "$4"
EOS2
chmod +x "$TMP_DIR/bin/fake_sample.sh"
BUSY_DIR="$TMP_DIR/busy"
busy_env() {  # $1=bash $2=fake pid、残り=追加の env (NAME=VALUE)。窓を短くして撮影の判定を速く回す
  local sh="$1" pid="$2"; shift 2
  env TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$BUSY_DIR" \
    TT_WATCHDOG_INTERVAL=0.05 STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" TT_HEALTH_CHECK_INTERVAL=0 \
    TT_BUSY_WINDOW=1 TT_BUSY_PCT=50 TT_BUSY_WINDOWS=2 TT_BUSY_SAMPLE_SECS=1 \
    TT_SAMPLE_CMD="$TMP_DIR/bin/fake_sample.sh" PATH="$STUB_PATH" "$@" \
    "$sh" "$SCRIPT" "$pid" "/fake/socket" "1234567890"
}
spawn_busy() {  # CPU を使い切る偽サーバ
  ( trap - EXIT; exec yes ) > /dev/null &
  REPLY_PID="$!"; FAKE_PIDS+=("$REPLY_PID")
}
busy_line_present() { grep -q "server-busy pid=$1 " "$LOG" 2>/dev/null; }
busy_line_count()   { grep -c "server-busy pid=$1 " "$LOG" 2>/dev/null; }
file_written()      { [ -s "$1" ]; }
file_written_or_empty() { [ -e "$1" ]; }
reset_busy() { : > "$LOG"; rm -rf "$BUSY_DIR"; mkdir -p "$BUSY_DIR"; }

BASHES=(/bin/bash)
for b in /opt/homebrew/bin/bash /usr/local/bin/bash; do
  [ -x "$b" ] && [ "$("$b" -c 'echo ${BASH_VERSINFO[0]}')" -ge 5 ] && { BASHES+=("$b"); break; }
done
[ "${#BASHES[@]}" -ge 2 ] || printf '🚨 bash 5 が見つからないので、EPOCHREALTIME の経路は回していない\n'

for sh in "${BASHES[@]}"; do
  ver="$("$sh" -c 'echo $BASH_VERSION')"
  reset_busy
  spawn_busy; BUSY="$REPLY_PID"
  busy_env "$sh" "$BUSY" & WD=$!
  wait_log "[$ver] CPU を使い切るサーバで server-busy 行が書かれない" busy_line_present "$BUSY"
  grep -qE "${TT_TAB}server-busy pid=$BUSY cpu=[0-9]+% windows=2 window=[0-9]+s sample=[^ ]+\.sample ps=[^ ]+\.ps epoch=[0-9]+$" "$LOG" \
    || { printf '✗ [%s] server-busy 行の書式が想定と違う:\n' "$ver"; cat "$LOG"; exit 1; }
  sample_path="$(sed -n "s/.*server-busy pid=$BUSY .* sample=\([^ ]*\) ps=.*/\1/p" "$LOG" | head -1)"
  ps_path="$(sed -n "s/.*server-busy pid=$BUSY .* ps=\([^ ]*\) epoch=.*/\1/p" "$LOG" | head -1)"
  wait_log "[$ver] sample の出力 ($sample_path) が書かれない" file_written "$sample_path"
  grep -q "sampled pid=$BUSY secs=1" "$sample_path" \
    || { printf '✗ [%s] sample にサーバの pid と秒数が渡っていない:\n' "$ver"; cat "$sample_path"; exit 1; }
  awk -v p="$BUSY" '$1 == p { f = 1 } END { exit !f }' "$ps_path" \
    || { printf '✗ [%s] ps の出力 (%s) にサーバの pid が写っていない\n' "$ver" "$ps_path"; exit 1; }
  sleep 2  # sleep-ok: negative: cooldown の間に撮り直さないことには成立条件が無い。窓 1 秒が 1 回は回る長さ
  kill "$BUSY" 2>/dev/null
  wait_for_death_line "$BUSY" "[$ver] 撮影の後もサーバの死を看取る"
  wait "$WD" 2>/dev/null || true
  [ "$(busy_line_count "$BUSY")" -eq 1 ] \
    || { printf '✗ [%s] cooldown の間に 2 回以上撮った:\n' "$ver"; cat "$LOG"; exit 1; }
  printf '✓ [%s] CPU を使い切る窓が続いたら sample と ps を撮り、cooldown の間は撮り直さない\n' "$ver"
done

# --- (8b) sample を待たない (待つと、その間はサーバの死を看取れない) ------------------------
reset_busy
spawn_busy; BUSY="$REPLY_PID"
busy_env /bin/bash "$BUSY" SAMPLE_DELAY=5 & WD=$!
wait_log "背景起動: server-busy 行が書かれない" busy_line_present "$BUSY"
sample_path="$(sed -n "s/.*server-busy pid=$BUSY .* sample=\([^ ]*\) ps=.*/\1/p" "$LOG" | head -1)"
kill "$BUSY" 2>/dev/null
wait_for_death_line "$BUSY" "背景起動"
# sample の stub は 5 秒かかる。死の看取りがそれより先に済んでいれば、sample を待っていない
[ ! -e "$sample_path" ] || { printf '✗ sample が終わるまで死の看取りが待たされた\n'; exit 1; }
wait "$WD" 2>/dev/null || true
printf '✓ sample の終わりを待たずにサーバの死を看取る\n'

# --- (9) 暇なサーバでは撮らない ------------------------------------------------------------
reset_busy
tt_spawn_fake_proc; FAKE="$REPLY_PID"
busy_env /bin/bash "$FAKE" & WD=$!
wait_lock "暇なサーバ: 監視が張られない" lock_dir_exists "$FAKE"
sleep 3  # sleep-ok: negative: 撮らないことには待つべき成立条件が無い。窓 1 秒 × 2 回の判定が 1 回は回る長さ
kill "$FAKE" 2>/dev/null
wait_for_death_line "$FAKE" "暇なサーバ"
wait "$WD" 2>/dev/null || true
! grep -q "server-busy" "$LOG" || { printf '✗ CPU を使っていないサーバで撮った:\n'; cat "$LOG"; exit 1; }
printf '✓ CPU を使っていないサーバでは撮らない\n'

# --- (10) 直近 TT_BUSY_KEEP 組だけ残し、古い組は .ps と .sample の両方を消す ----------------
reset_busy
spawn_busy; BUSY="$REPLY_PID"
busy_env /bin/bash "$BUSY" TT_BUSY_COOLDOWN=0 TT_BUSY_KEEP=1 & WD=$!
three_lines() { [ "$(busy_line_count "$1")" -ge 3 ]; }
wait_log "世代管理: 3 回撮らない" three_lines "$BUSY"
kill "$BUSY" 2>/dev/null
wait_for_death_line "$BUSY" "世代管理"
wait "$WD" 2>/dev/null || true
n_ps="$(find "$BUSY_DIR" -name 'tt-server-busy-*.ps' | wc -l | tr -d ' ')"
n_sample="$(find "$BUSY_DIR" -name 'tt-server-busy-*.sample' | wc -l | tr -d ' ')"
# .ps は撮るたびに同期で消すので、ちょうど KEEP 個。.sample は最後の sample が書き終わったかで 0〜1 個ずれる
[ "$n_ps" -eq 1 ] && [ "$n_sample" -le 2 ] \
  || { printf '✗ 古い組を消していない (.ps %s 個 / .sample %s 個):\n' "$n_ps" "$n_sample"; ls -l "$BUSY_DIR"; exit 1; }
printf '✓ 古い組は .ps と .sample の両方を消す (残り .ps %s / .sample %s)\n' "$n_ps" "$n_sample"

# --- (11) ループが固まりの間ずっと塞がれたら、撮らずに「塞がれた間に忙しかった」とだけ残す ----
# ループを塞ぐものが解けた後に撮っても固まりの後のスタックしか写らず、cooldown を立てると 10 分以内の次の
# 固まりを撮れなくなる。塞ぐ役は health の stub で演じる (本番の health は 5 秒で打ち切るので、窓の 2 倍まで
# 塞ぐのは窓の短いこのテストの設定だから)。偽サーバは 4〜5 秒だけ CPU を使い切ってから暇になり、stub は 6 秒塞ぐ。
# 閾値を 20% に下げて、CI の負荷で CPU を半分しかもらえない実行でも塞がれた窓を忙しいと読む。
reset_busy
cat > "$TMP_DIR/bin/slow_health.sh" <<'EOS2'
#!/bin/sh
sleep 6  # sleep-ok: stub: 固まったサーバへの問い合わせが返らない時間を演じる
exit 0
EOS2
chmod +x "$TMP_DIR/bin/slow_health.sh"
# 組み込みだけで回す ($(date) を回すと CPU を使うのは子の date で、偽サーバ自身は暇に見える)
( trap - EXIT; end=$(( SECONDS + 5 )); while [ "$SECONDS" -lt "$end" ]; do :; done; exec sleep 300 ) &  # sleep-ok: dummy: kill される前提の常駐プロセス
BURST="$!"; FAKE_PIDS+=("$BURST")
busy_env /bin/bash "$BURST" TT_BUSY_PCT=20 TT_HEALTH_CHECK_INTERVAL=100 TT_HEALTH_SCRIPT="$TMP_DIR/bin/slow_health.sh" & WD=$!
wait_log "塞がれた間に忙しかったことが残らない" log_matches "${TT_TAB}server-busy-missed pid=$BURST cpu=[0-9]+% blocked=[0-9]+s epoch=[0-9]+$"
sleep 1  # sleep-ok: negative: 撮らないことには成立条件が無い。撮るなら missed 行と同じ判定の直後 (0.1 秒以内) に撮る
! busy_line_present "$BURST" || { printf '✗ 固まりが解けた後に撮った:\n'; cat "$LOG"; exit 1; }
kill "$BURST" 2>/dev/null
wait "$WD" 2>/dev/null || true
printf '✓ ループが塞がれた間に忙しかったら、撮らずにその事実だけ残す\n'

# --- (12) 数の設定値は算術に通す前に検査する ------------------------------------------------
# 数でない値・先頭が 0 の値 (08 は 8 進数として読まれて算術エラー) をそのまま算術に通すと、bash は
# while ループごと捨てて死亡処理へ進み、生きているサーバに偽の server-death を書く。
# (12a) 先頭が 0 の値は 10 進数に直して使う (cooldown の計算は撮る直前に走る)
reset_busy
spawn_busy; BUSY="$REPLY_PID"
busy_env /bin/bash "$BUSY" TT_BUSY_COOLDOWN=08 & WD=$!
wait_log "先頭が 0 の cooldown で撮れない" busy_line_present "$BUSY"
! grep -q "server-death pid=$BUSY " "$LOG" || { printf '✗ 生きているサーバに server-death を書いた (08):\n'; cat "$LOG"; exit 1; }
kill "$BUSY" 2>/dev/null
wait_for_death_line "$BUSY" "先頭が 0 の値でも看取る"
wait "$WD" 2>/dev/null || true
printf '✓ 先頭が 0 の設定値は 10 進数に直して撮る\n'
# (12b) 数でない値は決めた値に戻し、変数名だけを残す。cooldown は撮る直前の算術に入るので、戻さなければ
#   撮る前に while ごと捨てられて、撮れずに偽の server-death を書く
reset_busy
spawn_busy; BUSY="$REPLY_PID"
busy_env /bin/bash "$BUSY" TT_BUSY_COOLDOWN=10m & WD=$!
wait_log "数でない cooldown で撮れない" busy_line_present "$BUSY"
! grep -q "server-death pid=$BUSY " "$LOG" || { printf '✗ 生きているサーバに server-death を書いた (10m):\n'; cat "$LOG"; exit 1; }
kill -0 "$WD" 2>/dev/null || { printf '✗ 数でない値で watchdog が消えた:\n'; cat "$LOG"; exit 1; }
grep -qE "${TT_TAB}watchdog-bad-env var=TT_BUSY_COOLDOWN fallback=600 pid=$BUSY epoch=[0-9]+$" "$LOG" \
  || { printf '✗ 数でない値を戻した理由が観測ログに無い (値は書かない):\n'; cat "$LOG"; exit 1; }
kill "$BUSY" 2>/dev/null
wait_for_death_line "$BUSY" "数でない値でも看取る"
wait "$WD" 2>/dev/null || true
printf '✓ 数でない設定値は決めた値に戻し、看取りを続ける\n'

# --- (13) health の確認には時間の上限がある (固まったサーバに並んでも、看取りと撮影を止めない) ----
# health は tmux に何度も問い合わせるので、固まったサーバでは返らない。上限が無いと、その間は死を看取れない。
# 1 回の時間切れは異常に数えないが、続いたら異常として知らせる (黙って流し続けると保存経路の停止に気づけない)。
# health の stub は、印のファイルが無い間は返らず (固まり)、印があれば本物の異常を返す (固まりが解けた後)。
cat > "$TMP_DIR/bin/hung_health.sh" <<'EOS2'
#!/bin/sh
if [ -f "$HEALTH_REAL_NG_FLAG" ]; then echo "スナップショット異常: 本物の異常"; exit 1; fi
[ -n "${HEALTH_RAN_MARK:-}" ] && : > "$HEALTH_RAN_MARK"
[ -n "${HEALTH_RAN_MARK:-}" ] && exit 0
sleep 60  # sleep-ok: stub: 固まったサーバへの問い合わせが返らない
EOS2
chmod +x "$TMP_DIR/bin/hung_health.sh"
# toast も tmux に問い合わせるので、固まったサーバでは返らない。返らない toast を渡し、異常を知らせた後も看取れることを見る。
# 起こされたら pid を印に書く (起こしたことを確かめ、テストの終わりに止めるため)
cat > "$TMP_DIR/bin/hung_toast.sh" <<'EOS2'
#!/bin/sh
echo $$ >> "$TOAST_MARK"
exec sleep 20  # sleep-ok: stub: 固まったサーバに並んだ toast が返らない
EOS2
chmod +x "$TMP_DIR/bin/hung_toast.sh"
TOAST_MARK="$TMP_DIR/toast.pids"; export TOAST_MARK
health_env() {  # $1=fake pid、残り=追加の env
  local pid="$1"; shift
  env TT_TRIGGER_LOG="$LOG" TT_WATCHDOG_DIR="$TMP_DIR/wd" TT_PSLOG_DIR="$TMP_DIR/pslogs" \
    TT_WATCHDOG_INTERVAL=0.05 STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" \
    TT_HEALTH_CHECK_INTERVAL=1 TT_HEALTH_TIMEOUT=1 TT_HEALTH_SCRIPT="$TMP_DIR/bin/hung_health.sh" \
    HEALTH_REAL_NG_FLAG="$TMP_DIR/health_real_ng" TT_TOAST="$TMP_DIR/bin/hung_toast.sh" PATH="$STUB_PATH" "$@" \
    "$SCRIPT" "$pid" "/fake/socket" "1234567890"
}
# toast は watchdog から TERM の無視を引き継ぐので KILL で止める
stop_toasts() { [ -f "$TOAST_MARK" ] && while read -r p; do kill -KILL "$p" 2>/dev/null; done < "$TOAST_MARK"; rm -f "$TOAST_MARK"; }
toast_started() { [ -s "$TOAST_MARK" ]; }
if [ "$HAVE_RUNTIMEOUT" = 1 ]; then
  : > "$LOG"; rm -f "$TMP_DIR/health_real_ng"
  tt_spawn_fake_proc; FAKE="$REPLY_PID"
  health_env "$FAKE" & WD=$!
  wait_log "health の時間切れが残らない (上限なしで待っている)" log_matches "${TT_TAB}snapshot-health timeout secs=1 count=1 pid=$FAKE epoch=[0-9]+$"
  ! grep -q "snapshot-health ng" "$LOG" || { printf '✗ 1 回の時間切れを異常 (ng) に数えた:\n'; cat "$LOG"; exit 1; }
  wait_log "時間切れが 3 回続いても異常に数えない" log_matches "${TT_TAB}snapshot-health ng epoch=[0-9]+ detail=.*3 回続けて 1 秒で終わらない"
  tt_wait_until toast_started || { printf '✗ 異常を知らせる toast を起こしていない\n'; exit 1; }
  : > "$TMP_DIR/health_real_ng"
  wait_log "時間切れの異常の後、本物の異常の理由が書かれない" log_matches "${TT_TAB}snapshot-health ng epoch=[0-9]+ detail=スナップショット異常: 本物の異常$"
  kill "$FAKE" 2>/dev/null
  wait_for_death_line "$FAKE" "health の時間切れと返らない toast の後も看取る"
  wait "$WD" 2>/dev/null || true
  stop_toasts
  ! grep -q "snapshot-health-unbounded" "$LOG" || { printf '✗ runtimeout があるのに上限なしで待った:\n'; cat "$LOG"; exit 1; }
  printf '✓ health の確認は上限で打ち切り、3 回続いたら異常として知らせ、固まりが解けた後の本物の異常も書く\n'
else
  printf '🚨 runtimeout を作れない (go が無い) ので (13) を飛ばした\n'
fi

# --- (13b) runtimeout が無い・動かなければ、上限なしで health を回し、そのことを残す (watchdog はビルドしない) ----
cat > "$TMP_DIR/bin/broken_runtimeout.sh" <<'EOS2'
#!/bin/sh
exit 126
EOS2
chmod +x "$TMP_DIR/bin/broken_runtimeout.sh"
for case_ in "no-runtimeout:$TMP_DIR/nonexistent-runtimeout" "runtimeout-broken:$TMP_DIR/bin/broken_runtimeout.sh"; do
  reason="${case_%%:*}"; rt="${case_#*:}"
  : > "$LOG"; rm -f "$TMP_DIR/health_ran" "$TMP_DIR/health_real_ng"
  tt_spawn_fake_proc; FAKE="$REPLY_PID"
  health_env "$FAKE" TT_RUNTIMEOUT_BIN="$rt" HEALTH_RAN_MARK="$TMP_DIR/health_ran" & WD=$!
  wait_log "[$reason] 上限なしで待つことが残らない" log_matches "${TT_TAB}snapshot-health-unbounded reason=$reason pid=$FAKE epoch=[0-9]+$"
  wait_log "[$reason] health を上限なしで回していない" file_written_or_empty "$TMP_DIR/health_ran"
  kill "$FAKE" 2>/dev/null
  wait_for_death_line "$FAKE" "[$reason] runtimeout が使えなくても看取る"
  wait "$WD" 2>/dev/null || true
  stop_toasts
  ! grep -q "snapshot-health ng" "$LOG" || { printf '✗ [%s] 使えない runtimeout の失敗を異常 (ng) に読んだ:\n' "$reason"; cat "$LOG"; exit 1; }
  printf '✓ [%s] 上限なしで health を回し、そのことを観測ログに残す\n' "$reason"
done

printf '\nAll server-watchdog tests passed successfully!\n'

