#!/usr/bin/env bash
# tmux サーバ死亡の観測 watchdog。_tmux.conf の conf source 時に run-shell -b で起動され、
# サーバ pid の消滅を検知して「死亡時刻・死因分類・全プロセスのスナップショット」を記録する。
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
# ⚠️ 生存の生命線: tmux はサーバ終了時 (kill-server / SIGTERM 両方) に run-shell ジョブの
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

TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
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
# ⚠️ owner の判定は pid だけで行わないこと。pid 再利用で「先任が生きている」と誤認すると、
# 新世代の watchdog が無音で退いて **watchdog が 1 つも張られない** (サーバが死んでも死亡記録が
# 残らず観測装置が丸ごと不発になる。2026-07-30 の監査で実証済み。以前ここには「実害は二重
# watchdog 側に倒れるので許容」と書いていたが実測と逆で、装置不在側に倒れる)。
# 起動時刻まで含めた同一性判定は guards.sh の tt_lock_owner_alive / tt_same_proc に集約。
for d in "$TT_WATCHDOG_DIR"/*.lock; do
  [ -d "$d" ] || continue
  spid="$(basename "$d" .lock)"
  if ! kill -0 "$spid" 2>/dev/null && ! tt_lock_owner_alive "$d"; then
    rm -rf "$d" 2>/dev/null
  fi
done
LOCK_DIR="$TT_WATCHDOG_DIR/$SERVER_PID.lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  if tt_lock_owner_alive "$LOCK_DIR"; then
    exit 0   # 先任の watchdog が (同一プロセスとして) 生きている
  fi
  rm -rf "$LOCK_DIR" 2>/dev/null
  mkdir "$LOCK_DIR" 2>/dev/null || exit 0
fi
tt_lock_write_owner "$LOCK_DIR"
# 異常終了 (TERM は無視するが INT / エラー / 正常終了) でも lock を残さない。残すと次世代の
# 起動が上の stale 判定に頼ることになり、pid 再利用の窓を無駄に開ける
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
release_watchdog_lock() { rm -rf "$LOCK_DIR" 2>/dev/null; }
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
  [ "$TT_HEALTH_CHECK_INTERVAL" -gt 0 ] 2>/dev/null || return 0
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
    { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" && printf '%s\tsnapshot-health ng epoch=%s detail=%s\n' \
        "$(date +%FT%T)" "$now" "$out" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
    # 見える通知 (フォーカスを奪わない toast)。落ちても監視は続ける
    [ -x "$TT_TOAST" ] && "$TT_TOAST" -d 8 -b 52 "⚠️ スナップショット異常: ${out#スナップショット異常: }" 2>/dev/null || true
  else
    { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" && printf '%s\tsnapshot-health ok epoch=%s\n' \
        "$(date +%FT%T)" "$now" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
  fi
}

while :; do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    break
  fi
  if [ -n "$lstart0" ]; then
    cur="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"
    [ "$cur" = "$lstart0" ] || break   # pid 再利用 = 元のサーバは死んでいる
  fi
  check_health
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
# ⚠️ (2) が無いと世代を跨いで誤分類する。tmux は kill-server 直後に新サーバが立つため、
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

{ mkdir -p "$(dirname "$TT_TRIGGER_LOG")" && printf '%s\tserver-death pid=%s start=%s socket=%s verdict=%s pslog=%s\n' \
    "$ts" "$SERVER_PID" "$SERVER_START" "$SOCKET_PATH" "$verdict" "$pslog" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true

# pslog の世代管理 (直近 TT_PSLOG_KEEP 件だけ残す)
# shellcheck disable=SC2012 # mtime 降順が要件で、対象は自前命名 (空白なし) のファイルのみ
ls -1t "$TT_PSLOG_DIR"/tt-server-death-*.pslog 2>/dev/null | tail -n +"$((TT_PSLOG_KEEP + 1))" \
  | while IFS= read -r f; do rm -f "$f" 2>/dev/null; done

rm -rf "$LOCK_DIR" 2>/dev/null
exit 0
