#!/usr/bin/env bash
# tmux サーバ死亡の観測 watchdog。_tmux.conf の conf source 時に run-shell -b で起動され、
# サーバ pid の消滅を検知して「死亡時刻・死因分類・全プロセスのスナップショット」を記録する。
# サーバが CPU を使い切って固まっている最中のスタック (sample) と ps も撮る (下の「固まりの撮影」)。
#
# なぜ: tmux に server-exit 相当の hook は存在せず (3.7b の man 全 hook を確認済み 2026-07-30)、
#   サーバ側からは自分の死を記録できない。外部シグナル (kill-cmd shim を通らない SIGTERM 等)
#   による死は、死んだ瞬間のプロセス一覧が唯一の容疑者リストになるため、独立プロセスで看取る。
#
# 死因分類 (verdict) は ~/.cache/tt-restore-trigger.log の直近エントリとの相関で行う:
#   kill-server-command      … shim の kill-cmd cmd=kill-server が直近 (120s) にある
#   kill-session-exit-empty  … shim の kill-cmd cmd=kill-session + session-closed remaining=0
#   exit-empty               … session-closed remaining=0 のみ (pane 死亡連鎖など)
#   external-signal-or-crash … 上記いずれも無し (SIGTERM/SIGKILL/クラッシュ)。pslog を見る
#
# 🚨 生存の生命線: tmux はサーバ終了時 (kill-server / SIGTERM 両方) に run-shell ジョブの
#   子プロセスへ SIGTERM を 1 発送る (3.7b 実測 2026-07-30)。trap '' TERM で無視しないと
#   肝心の死亡瞬間に watchdog 自身が死ぬ。nohup (HUP のみ) では不十分。
#
# 無音契約: run-shell -b 経由のため、失敗時は無音で exit 0 (scripts/CLAUDE.md 参照)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

SERVER_PID="${1:-}"
SOCKET_PATH="${2:-unknown}"
SERVER_START="${3:-unknown}"

TT_WATCHDOG_DIR="${TT_WATCHDOG_DIR:-$HOME/.cache/tt-watchdog}"
TT_PSLOG_DIR="${TT_PSLOG_DIR:-$HOME/.cache}"
TT_PSLOG_KEEP="${TT_PSLOG_KEEP:-10}"
TT_WATCHDOG_INTERVAL="${TT_WATCHDOG_INTERVAL:-2}"
# スナップショット健全性チェックの間隔 (秒)。0 で無効。
# なぜ watchdog が持つか: 周期保存プロセス自身が死んだ場合、それを検出できるのは別プロセスだけ。
# 「保存が 17 日間 silent に止まっていた」の再発を、独立したこのループで見張る (2026-07-30)。
TT_HEALTH_CHECK_INTERVAL="${TT_HEALTH_CHECK_INTERVAL:-300}"
TT_HEALTH_SCRIPT="${TT_HEALTH_SCRIPT:-$SCRIPT_DIR/tmux_snapshot_health.sh}"
TT_TOAST="${TT_TOAST:-$SCRIPT_DIR/../bin/tmux-toast}"
# 死因相関の時間窓 (秒)。kill-cmd/session-closed の epoch がこの範囲内なら「直近」とみなす
TT_VERDICT_WINDOW="${TT_VERDICT_WINDOW:-120}"

case "$SERVER_PID" in ''|*[!0-9]*) exit 0 ;; esac

# default socket のサーバだけを看取る (テスト/scratch サーバまで看取ると、テストのたびに
# watchdog が残留し共有ログを汚す)。conf は -L の隔離サーバでも source されるため必須の gate。
tt_on_default_server || exit 0

# 出力を持たない (run-shell のパイプを掴んだままにしない + サーバ死後の SIGPIPE を避ける)
exec </dev/null >/dev/null 2>&1

# ---- 二重起動ガード (conf 再 source で毎回 run-shell が走るため) --------------------
# lock は「サーバ pid ごと」の mkdir。既存 lock の watcher が生きていれば自分は退く。
mkdir -p "$TT_WATCHDOG_DIR" 2>/dev/null || exit 0
# 死んだサーバ/watcher の stale lock を掃除する。
# 🚨 owner の判定は pid だけで行わないこと。pid 再利用で「先任が生きている」と誤認すると、
# 新世代の watchdog が無音で退いて **watchdog が 1 つも張られない** (サーバが死んでも死亡記録が
# 残らず観測装置が丸ごと不発になる。2026-07-30 の監査で実証済み。以前ここには「実害は二重
# watchdog 側に倒れるので許容」と書いていたが実測と逆で、装置不在側に倒れる)。
# 起動時刻まで含めた同一性判定は guards.sh の tt_lock_owner_alive / tt_same_proc に集約。
tt_lock_sweep_stale "$TT_WATCHDOG_DIR"
LOCK_DIR="$TT_WATCHDOG_DIR/$SERVER_PID.lock"
tt_lock_rc=0
tt_lock_acquire "$LOCK_DIR" || tt_lock_rc=$?
if [ "$tt_lock_rc" -eq 1 ]; then
  exit 0   # 先任の watchdog が (同一プロセスとして) 生きている = 正常。無音で退く
elif [ "$tt_lock_rc" -ne 0 ]; then
  # 🚨 上と同じ理由で、取得不能を無音にしない (watchdog 不在 = 死因が二度と記録されない)
  tt_trigger_log "watchdog-aborted reason=lock-failed server=$SERVER_PID epoch=$(date +%s)"
  exit 0
fi
# 異常終了 (TERM は無視するが INT / エラー / 正常終了) でも lock を残さない。残すと次世代の
# 起動が上の stale 判定に頼ることになり、pid 再利用の窓を無駄に開ける
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
release_watchdog_lock() { tt_lock_release_if_owner "$LOCK_DIR"; }
trap release_watchdog_lock EXIT

# サーバ終了時の SIGTERM を無視する (冒頭コメント参照)。HUP も終端切断対策で無視。
trap '' TERM HUP

# pid 再利用の誤生存判定を防ぐため、起動時刻も同一性キーに含める (save wrapper の
# owner 同定と同じ方針)。lstart が取れない環境では kill -0 のみで判定する。
lstart0="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"

# スナップショット健全性チェック。異常が続いている間、毎回 toast を出すと騒がしいので
# 「状態が変わったときだけ」通知する (異常になった瞬間と、回復した瞬間)。
health_last_state=ok
health_next_check=0
check_health() {
  local now out state
  now="$(date +%s)"
  [ "$TT_HEALTH_CHECK_INTERVAL" -gt 0 ] || return 0
  [ "$now" -ge "$health_next_check" ] || return 0
  health_next_check=$(( now + TT_HEALTH_CHECK_INTERVAL ))
  [ -x "$TT_HEALTH_SCRIPT" ] || return 0
  if out="$("$TT_HEALTH_SCRIPT" --quiet 2>/dev/null)"; then
    state=ok
  else
    state=ng
  fi
  [ "$state" = "$health_last_state" ] && return 0
  health_last_state="$state"
  if [ "$state" = ng ]; then
    tt_trigger_log "snapshot-health ng epoch=$now detail=$out"
    # 見える通知 (フォーカスを奪わない toast)。落ちても監視は続ける
    [ -x "$TT_TOAST" ] && "$TT_TOAST" -d 8 -b 52 "🚨 スナップショット異常: ${out#スナップショット異常: }" 2>/dev/null || true
  else
    tt_trigger_log "snapshot-health ok epoch=$now"
  fi
}

# ---- 固まりの撮影 -------------------------------------------------------------------
# サーバが CPU を使い切って client のコマンドが返らない固まりは 20〜30 秒で自然に解け、
# 人が気づいて sample を打つ頃には消えている。固まっている最中のスタックを撮るために、
# サーバの累積 CPU 時間の増え方が TT_BUSY_PCT 以上の窓 (TT_BUSY_WINDOW 秒) が
# TT_BUSY_WINDOWS 回続いたら、sample と ps を撮る。
# 🚨 判定に tmux への問い合わせ (display -p 等) を使わない。固まったサーバへ client を
#   足すことになり、並んだコマンドを増やすうえ、問い合わせ自体が watchdog の loop を止める。
# 🚨 sample は背景で起こして待たない。待つと、その間はサーバの死を看取れない。
# 判定はループの刻み (TT_WATCHDOG_INTERVAL) ごとなので、実際の窓は TT_BUSY_WINDOW を超えた最初の刻み
# (既定で約 6 秒) になり、検出まで 12〜18 秒かかる。
TT_BUSY_WINDOW="${TT_BUSY_WINDOW:-5}"
TT_BUSY_PCT="${TT_BUSY_PCT:-85}"
TT_BUSY_WINDOWS="${TT_BUSY_WINDOWS:-2}"
TT_BUSY_COOLDOWN="${TT_BUSY_COOLDOWN:-600}"
TT_BUSY_KEEP="${TT_BUSY_KEEP:-10}"
TT_BUSY_SAMPLE_SECS="${TT_BUSY_SAMPLE_SECS:-5}"
TT_SAMPLE_CMD="${TT_SAMPLE_CMD:-/usr/bin/sample}"
# 🚨 数の環境変数をそのまま算術に通さない。bash は算術エラー (10m・先頭が 0 の 08 は 8 進数として読んで
#   value too great for base) で今の while ループごと捨てて次のコマンドへ進むので、生きているサーバに
#   死亡処理が走り、偽の server-death を書いて watchdog が消える。
#   数字だけで範囲内なら 10 進数に直し、外れたら決めた値に戻して、変数名だけをログに残す
#   (値はそのまま書かない。改行を含む値は観測ログに偽の行を書ける)。
# shellcheck source=bin/lib/uint.sh
. "$SCRIPT_DIR/../bin/lib/uint.sh"
watchdog_uint_env() {  # $1=変数名 $2=下限 $3=上限 $4=外れたときに使う値
  local val="${!1}"
  if uint_ok "$val" 6 && [ "$((10#$val))" -ge "$2" ] && [ "$((10#$val))" -le "$3" ]; then
    printf -v "$1" '%d' "$((10#$val))"
  else
    tt_trigger_log "watchdog-bad-env var=$1 fallback=$4 pid=$SERVER_PID epoch=$(date +%s)"
    printf -v "$1" '%s' "$4"
  fi
}
watchdog_uint_env TT_HEALTH_CHECK_INTERVAL 0 86400 300
watchdog_uint_env TT_PSLOG_KEEP 1 1000 10
watchdog_uint_env TT_VERDICT_WINDOW 0 86400 120  # 数でないと [ -le ] が常に偽になり、全部の死を外因死に分類する
watchdog_uint_env TT_BUSY_WINDOW 0 3600 5   # 0 で撮影を止める
watchdog_uint_env TT_BUSY_PCT 1 100 85      # 0 を許すと全部の窓が忙しくなり、health がずっと後回しになる
watchdog_uint_env TT_BUSY_WINDOWS 1 100 2
watchdog_uint_env TT_BUSY_COOLDOWN 0 86400 600
watchdog_uint_env TT_BUSY_KEEP 1 1000 10    # 0 を許すと今撮った .ps まで消え、後から書かれる .sample が残り続ける
# 同じ取り残しは、cooldown が sample の秒数より短く連続して撮るときにも起きる (古い組を消した後に、走っている
# sample が .sample を書く)。cooldown を短くするのはテストだけなので、ここでは塞がない
watchdog_uint_env TT_BUSY_SAMPLE_SECS 1 60 5

# 1/100 秒単位の今の時刻。date +%s の 1 秒刻みだと 5 秒の窓で比が ±20% ぶれるので、
# bash 5 の EPOCHREALTIME があれば使う (小数点はロケールで , になる)。
now_cs() {
  local s f
  if [ -n "${EPOCHREALTIME:-}" ]; then
    s="${EPOCHREALTIME%%[.,]*}"; f="${EPOCHREALTIME#*[.,]}"
    REPLY=$(( s * 100 + 10#${f:0:2} ))
  else
    REPLY=$(( $(date +%s) * 100 ))
  fi
}
# サーバの累積 CPU 時間 (1/100 秒)。macOS の ps の time は「分:秒.百分の一」で、分は桁が増え続ける。
# 小数点はロケールで , になりうる。形が違えば空を返し、その窓は判定しない。
server_cpu_cs() {
  REPLY="$(ps -o time= -p "$SERVER_PID" 2>/dev/null \
    | awk -F'[:.,]' 'NF == 3 && $1 ~ /^ *[0-9]+$/ { print ($1 * 60 + $2) * 100 + $3 }')"
}

busy_win_start=""; busy_win_cpu=""; busy_streak=0; busy_next_allowed=0
HEALTH_MAX_DEFER=600
start_busy_window() {
  now_cs; busy_win_start="$REPLY"
  server_cpu_cs; busy_win_cpu="$REPLY"
}
capture_busy() {  # $1=窓の CPU 使用率 (%) $2=最後の窓の長さ (秒)
  local stamp base now
  now="$(date +%s)"
  stamp="$(date +%Y%m%dT%H%M%S)"
  base="$TT_PSLOG_DIR/tt-server-busy-$stamp-$SERVER_PID"
  "$TT_SAMPLE_CMD" "$SERVER_PID" "$TT_BUSY_SAMPLE_SECS" -file "$base.sample" >/dev/null 2>&1 &
  ps -Ao pid,ppid,stat,etime,%cpu,rss,command -ww > "$base.ps" 2>/dev/null
  # sample の成否は待たないので分からない。sample= のファイルが無ければ sample が失敗している
  tt_trigger_log "server-busy pid=$SERVER_PID cpu=$1% windows=$busy_streak window=$2s sample=$base.sample ps=$base.ps epoch=$now"
  # shellcheck disable=SC2012 # mtime 降順が要件で、対象は自前命名 (空白なし) のファイルのみ
  ls -1t "$TT_PSLOG_DIR"/tt-server-busy-*.ps 2>/dev/null | tail -n +"$((TT_BUSY_KEEP + 1))" \
    | while IFS= read -r f; do rm -f "$f" "${f%.ps}.sample" 2>/dev/null; done
}
check_busy() {
  local now cpu dt pct
  [ "$TT_BUSY_WINDOW" -gt 0 ] || return 0
  if [ -z "$busy_win_start" ] || [ -z "$busy_win_cpu" ]; then start_busy_window; return 0; fi
  now_cs; now="$REPLY"
  dt=$(( now - busy_win_start ))
  [ "$dt" -ge $(( TT_BUSY_WINDOW * 100 )) ] || return 0
  server_cpu_cs; cpu="$REPLY"
  if [ -z "$cpu" ]; then busy_streak=0; start_busy_window; return 0; fi
  pct=$(( (cpu - busy_win_cpu) * 100 / dt ))
  busy_win_start="$now"; busy_win_cpu="$cpu"
  if [ "$pct" -lt "$TT_BUSY_PCT" ]; then busy_streak=0; return 0; fi
  # 窓が 2 倍以上に伸びた = ループが塞がれていた (固まったサーバへの health の問い合わせ等)。
  # 問い合わせが返るのは固まりが解けた後なので、今撮っても固まりの後のスタックしか写らない。
  # 撮らず、cooldown も立てず (10 分以内の次の固まりを撮れるように)、忙しかったことだけを残す
  if [ "$dt" -ge $(( TT_BUSY_WINDOW * 200 )) ]; then
    tt_trigger_log "server-busy-missed pid=$SERVER_PID cpu=$pct% blocked=$(( dt / 100 ))s epoch=$(( now / 100 ))"
    busy_streak=1
    return 0
  fi
  busy_streak=$(( busy_streak + 1 ))
  [ "$busy_streak" -ge "$TT_BUSY_WINDOWS" ] || return 0
  [ "$(( now / 100 ))" -ge "$busy_next_allowed" ] || return 0
  busy_next_allowed=$(( now / 100 + TT_BUSY_COOLDOWN ))
  capture_busy "$pct" "$(( dt / 100 ))"
}

while :; do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    break
  fi
  if [ -n "$lstart0" ]; then
    cur="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"
    [ "$cur" = "$lstart0" ] || break   # pid 再利用 = 元のサーバは死んでいる
  fi
  # 窓の始まりを health の確認より前に置く (health が塞いだ時間を窓に含めるため)。
  # 直前の窓が忙しい間は health を後回しにする: health は tmux に問い合わせるので、固まったサーバに並び、
  # 撮った直後なら sample を撮っている最中のサーバに並ぶ。ただし予定から HEALTH_MAX_DEFER 秒過ぎたら走らせる
  # (長く忙しいだけのサーバで、スナップショット異常の通知が止まり続けないように)
  check_busy
  if [ "$busy_streak" -eq 0 ] || [ "$(date +%s)" -ge $(( health_next_check + HEALTH_MAX_DEFER )) ]; then
    check_health
  fi
  sleep "$TT_WATCHDOG_INTERVAL"
done

# ---- 死亡処理 -----------------------------------------------------------------------
now_epoch="$(date +%s)"
ts="$(date +%FT%T)"
pslog="$TT_PSLOG_DIR/tt-server-death-$(date +%Y%m%dT%H%M%S)-$SERVER_PID.pslog"
ps -axo pid,ppid,user,lstart,command > "$pslog" 2>/dev/null || pslog=none

# 直近ログとの相関で死因を分類する。採用条件は 3 つ全て:
#   (1) epoch= が時間窓 (TT_VERDICT_WINDOW) 内
#   (2) pid= が「今死んだサーバ」と一致する
#   (3) パターンに一致する行のうち最新のもの
# 🚨 (2) が無いと世代を跨いで誤分類する。tmux は kill-server 直後に新サーバが立つため、
#   「前世代を kill-server で落とし、新サーバが 2 分以内に外因死」で外因死が
#   verdict=kill-server-command になる (レビューで実証 2026-07-30)。pid は kill shim と
#   session-closed ロガーが各行に刻む。pid フィールドが無い行 (旧形式) は採用しない
#   = 分類は「不明なら外因」に倒れる (誤って kill と断定しない安全側)。
recent_has() {  # $1: grep パターン。窓内 + pid 一致の行があれば真
  local line epoch pid
  line="$(grep -E "$1" "$TT_TRIGGER_LOG" 2>/dev/null | grep -E "pid=$SERVER_PID( |$)" | tail -1)"
  [ -n "$line" ] || return 1
  epoch="$(printf '%s' "$line" | sed -n 's/.*epoch=\([0-9]*\).*/\1/p')"
  [ -n "$epoch" ] || return 1
  pid="$(printf '%s' "$line" | sed -n 's/.*[^a-z]pid=\([0-9]*\).*/\1/p')"
  [ "$pid" = "$SERVER_PID" ] || return 1
  [ $((now_epoch - epoch)) -le "$TT_VERDICT_WINDOW" ]
}

if recent_has 'kill-cmd cmd=kill-server'; then
  verdict=kill-server-command
elif recent_has 'kill-cmd cmd=kill-session' && recent_has 'session-closed .*remaining=0'; then
  verdict=kill-session-exit-empty
elif recent_has 'session-closed .*remaining=0'; then
  verdict=exit-empty
else
  # 「コマンド由来の記録が無い」= 外部シグナル / クラッシュ / shim を通らない経路。
  # pslog は死亡検知後 (最大 TT_WATCHDOG_INTERVAL 遅れ) の撮影なので、シグナルを撃って即 exit した
  # 短命な送信元は写らない (macOS はシグナル送信元を記録しないため、そこは原理的な限界)。
  verdict=external-signal-or-crash
fi

# 打刻は死亡を検知した時刻 ($ts)。verdict の計算はこの後なので、書く時刻ではずれる
tt_trigger_log "server-death pid=$SERVER_PID start=$SERVER_START socket=$SOCKET_PATH verdict=$verdict pslog=$pslog" "$ts"

# pslog の世代管理 (直近 TT_PSLOG_KEEP 件だけ残す)
# shellcheck disable=SC2012 # mtime 降順が要件で、対象は自前命名 (空白なし) のファイルのみ
ls -1t "$TT_PSLOG_DIR"/tt-server-death-*.pslog 2>/dev/null | tail -n +"$((TT_PSLOG_KEEP + 1))" \
  | while IFS= read -r f; do rm -f "$f" 2>/dev/null; done

tt_lock_release_if_owner "$LOCK_DIR"
exit 0
