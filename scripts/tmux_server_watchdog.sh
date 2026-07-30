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
# 死んだサーバ/watcher の stale lock を先に掃除する (pid 再利用の誤保護は稀で、実害は
# 「watchdog 不在」ではなく「二重 watchdog」側に倒れるため許容)
for d in "$TT_WATCHDOG_DIR"/*.lock; do
  [ -d "$d" ] || continue
  spid="$(basename "$d" .lock)"
  wpid="$(cat "$d/pid" 2>/dev/null)"
  if ! kill -0 "$spid" 2>/dev/null && { [ -z "$wpid" ] || ! kill -0 "$wpid" 2>/dev/null; }; then
    rm -rf "$d" 2>/dev/null
  fi
done
LOCK_DIR="$TT_WATCHDOG_DIR/$SERVER_PID.lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  wpid="$(cat "$LOCK_DIR/pid" 2>/dev/null)"
  if [ -n "$wpid" ] && kill -0 "$wpid" 2>/dev/null; then
    exit 0   # 先任の watchdog が生きている
  fi
  rm -rf "$LOCK_DIR" 2>/dev/null
  mkdir "$LOCK_DIR" 2>/dev/null || exit 0
fi
printf '%s\n' "$$" > "$LOCK_DIR/pid" 2>/dev/null || true

# サーバ終了時の SIGTERM を無視する (冒頭コメント参照)。HUP も終端切断対策で無視。
trap '' TERM HUP

# pid 再利用の誤生存判定を防ぐため、起動時刻も同一性キーに含める (save wrapper の
# owner 同定と同じ方針)。lstart が取れない環境では kill -0 のみで判定する。
lstart0="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"

while :; do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    break
  fi
  if [ -n "$lstart0" ]; then
    cur="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"
    [ "$cur" = "$lstart0" ] || break   # pid 再利用 = 元のサーバは死んでいる
  fi
  sleep "$TT_WATCHDOG_INTERVAL"
done

# ---- 死亡処理 -----------------------------------------------------------------------
now_epoch="$(date +%s)"
ts="$(date +%FT%T)"
pslog="$TT_PSLOG_DIR/tt-server-death-$(date +%Y%m%dT%H%M%S)-$SERVER_PID.pslog"
ps -axo pid,ppid,user,lstart,command > "$pslog" 2>/dev/null || pslog=none

# 直近ログとの相関で死因を分類する。epoch= フィールドを持つ行だけを時間窓で採用する。
recent_has() {  # $1: grep パターン。時間窓内の epoch を持つ行があれば真
  local line epoch
  line="$(grep -E "$1" "$TT_TRIGGER_LOG" 2>/dev/null | tail -1)"
  [ -n "$line" ] || return 1
  epoch="$(printf '%s' "$line" | sed -n 's/.*epoch=\([0-9]*\).*/\1/p')"
  [ -n "$epoch" ] || return 1
  [ $((now_epoch - epoch)) -le "$TT_VERDICT_WINDOW" ]
}

if recent_has 'kill-cmd cmd=kill-server'; then
  verdict=kill-server-command
elif recent_has 'kill-cmd cmd=kill-session' && recent_has 'session-closed remaining=0'; then
  verdict=kill-session-exit-empty
elif recent_has 'session-closed remaining=0'; then
  verdict=exit-empty
else
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
