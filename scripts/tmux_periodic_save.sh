#!/usr/bin/env bash
# resurrect の周期スナップショットを長寿命プロセスで駆動する (continuum の
# status-right interpolation 方式の置き換え)。
#
# なぜ置き換えるか (2026-07-30 実測):
#   continuum の周期保存は status-right に #(continuum_save.sh) を仕込み、tmux が status を
#   描画するたびに実行する方式。この環境は status-interval 1 なので **毎秒** fork され、
#   1 回 50ms・内部で更に check_tmux_version.sh と tmux show-option 数回を起こす
#   (実測: トップレベル起動 0.8 回/秒、12 秒中 13% の時間走っている、概算 5-10 fork/秒)。
#   実際に保存するのは 15 分に 1 回で、残りは全部「まだ 15 分経っていない」の判定コスト。
#   zsh hook の fork 1 個を削る repo の基準に合わないため、判定を長寿命プロセスの
#   sleep に置き換えて fork を 15 分に 1 回へ落とす (rules/zsh-hook-return-via-reply.md と同思想)。
#   interpolation 側は vendor/tmux-plugins/tmux-continuum/continuum.tmux のパッチで無効化済み。
#
# 役割分担 (保存経路は 3 系統。全て choke point wrapper @resurrect-save-script-path 経由):
#   - 構成変化 (window/pane 増減) → scripts/tmux_resurrect_debounced_save.sh (秒オーダー)
#   - kill-server / 最後の kill-session の直前 → scripts/tmux_log_kill_command.sh
#   - 変化のない状態の定期スナップショット → 本スクリプト (既定 15 分)
#
# 寿命: サーバ終了時に run-shell の子として SIGTERM を受けて死ぬ (watchdog と違い TERM を
#   trap しない。サーバが居なくなったら保存する意味がないため、これが正しい挙動)。
#   サーバが SIGKILL された等でシグナルを受け損ねた場合も、次の周期で pid 生存を確認して抜ける。
#
# 無音契約: run-shell -b 経由なので stdout/stderr は汚さない (scripts/CLAUDE.md 参照)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

SERVER_PID="${1:-}"
TT_TRIGGER_LOG="${TT_TRIGGER_LOG:-$HOME/.cache/tt-restore-trigger.log}"
TT_PERIODIC_STATE_DIR="${TT_PERIODIC_STATE_DIR:-$HOME/.cache/tt-periodic-save}"
# 既定間隔 (分)。continuum と同じ @continuum-save-interval を出典にする (設定の二重化を避ける)
TT_PERIODIC_DEFAULT_MINUTES="${TT_PERIODIC_DEFAULT_MINUTES:-15}"
# テスト用: 1 周で抜ける (無限ループを回さずに 1 回分の判断と保存を検証する)
TT_PERIODIC_ONESHOT="${TT_PERIODIC_ONESHOT:-}"
# 観測ログの上限行数。超えたら末尾だけ残す。周期保存が 15 分毎に 1 行書くため、rotate が無いと
# 単調増加する (実測 96 行/日 ≒ 8KB/日)。5000 行 ≒ 50 日分で、resurrect のスナップショット保持
# (@resurrect-delete-backup-after、現在 7 日) より十分長いので forensics を失わない。
# 既に走っているこのプロセスで刈るので追加の fork は無い。
TT_TRIGGER_LOG_MAX_LINES="${TT_TRIGGER_LOG_MAX_LINES:-5000}"

case "$SERVER_PID" in ''|*[!0-9]*) exit 0 ;; esac

# default socket のサーバだけが保存主体 (保存先は HOME 共有。-L の隔離サーバまで保存すると
# 本番の last を上書きする。debounce / wrapper と同じ gate)
tt_on_default_server || exit 0

exec </dev/null >/dev/null 2>&1

log_line() {
  { mkdir -p "$(dirname "$TT_TRIGGER_LOG")" \
      && printf '%s\t%s\n' "$(date +%FT%T)" "$1" >> "$TT_TRIGGER_LOG"; } 2>/dev/null || true
}

# ---- 二重起動ガード (conf 再 source で run-shell が毎回走るため) ----------------------
mkdir -p "$TT_PERIODIC_STATE_DIR" 2>/dev/null || exit 0
# owner 判定は pid だけで行わない (pid 再利用で先任と誤認すると保存が張られない)。
# 起動時刻まで含む同一性判定は guards.sh に集約 (watchdog / restore_runner と共有)
for d in "$TT_PERIODIC_STATE_DIR"/*.lock; do
  [ -d "$d" ] || continue
  spid="$(basename "$d" .lock)"
  if ! kill -0 "$spid" 2>/dev/null && ! tt_lock_owner_alive "$d"; then
    rm -rf "$d" 2>/dev/null
  fi
done
LOCK_DIR="$TT_PERIODIC_STATE_DIR/$SERVER_PID.lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  if tt_lock_owner_alive "$LOCK_DIR"; then
    exit 0   # 先任が (同一プロセスとして) 生きている
  fi
  rm -rf "$LOCK_DIR" 2>/dev/null
  mkdir "$LOCK_DIR" 2>/dev/null || exit 0
fi
tt_lock_write_owner "$LOCK_DIR"
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
cleanup() { rm -rf "$LOCK_DIR" 2>/dev/null; }
trap cleanup EXIT

# 観測ログを上限行数に刈る。書き手は全員 `>> file` の open-append-close なので、
# tmp + mv で inode を差し替えても書き込みを取りこぼさない (fd を握り続ける書き手が居ない)。
prune_trigger_log() {
  local n tmp
  [ -f "$TT_TRIGGER_LOG" ] || return 0
  n="$(wc -l < "$TT_TRIGGER_LOG" 2>/dev/null | tr -d ' ')"
  case "$n" in ''|*[!0-9]*) return 0 ;; esac
  [ "$n" -gt "$TT_TRIGGER_LOG_MAX_LINES" ] || return 0
  tmp="$TT_TRIGGER_LOG.prune.$$"
  if tail -n "$TT_TRIGGER_LOG_MAX_LINES" "$TT_TRIGGER_LOG" > "$tmp" 2>/dev/null; then
    mv -f "$tmp" "$TT_TRIGGER_LOG" 2>/dev/null || rm -f "$tmp" 2>/dev/null
  else
    rm -f "$tmp" 2>/dev/null
  fi
}

interval_seconds() {
  local v
  v="$(tmux show -gqv @continuum-save-interval 2>/dev/null)"
  case "$v" in
    ''|*[!0-9]*) v="$TT_PERIODIC_DEFAULT_MINUTES" ;;
    0)           v="$TT_PERIODIC_DEFAULT_MINUTES" ;;   # 0 = 無効は continuum の意味。ここでは既定へ
  esac
  printf '%s' "$((v * 60))"
}

log_line "periodic-save-begin server=$SERVER_PID interval=$(interval_seconds)s epoch=$(date +%s)"

# pid 再利用の誤認防止に起動時刻も同一性キーに含める (watchdog と同方針)。サーバ死亡から
# 次の周期までは最大 interval 秒あり、その間に pid が再利用されると「別サーバに対して保存を
# 続ける第 2 の saver」になりうる (wrapper の lock があるので壊れはしないが冗長)。
# lstart が取れない環境では kill -0 のみで判定する。
server_lstart="$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)"

while :; do
  sleep "$(interval_seconds)"

  # サーバが消えていたら終わる (SIGTERM を受け損ねた場合の backstop)
  kill -0 "$SERVER_PID" 2>/dev/null || break
  if [ -n "$server_lstart" ]; then
    [ "$(ps -o lstart= -p "$SERVER_PID" 2>/dev/null)" = "$server_lstart" ] || break
  fi
  prune_trigger_log
  # socket gate は毎周期見る (サーバ入れ替わりで別環境の last を触らない)
  tt_on_default_server || break

  save_script="$(tmux show -gqv @resurrect-save-script-path 2>/dev/null)"
  if [ -z "$save_script" ] || [ ! -x "$save_script" ]; then
    log_line "periodic-save skipped=no-save-script epoch=$(date +%s)"
  else
    # 実際の保存可否 (復元中 / hold のみ / 退行) は wrapper 側ガードが判断する。
    # lock も wrapper の bounded-wait が持つので、ここでは同期実行して結果だけ記録する。
    if "$save_script" quiet >/dev/null 2>&1; then
      # continuum の最終保存時刻を進める (debounce 経路と同じ簿記。interpolation を止めた後も
      # continuum_save.sh を手動で叩く経路が残るため、意味論を揃えておく)
      tmux set-option -g @continuum-save-last-timestamp "$(date +%s)" 2>/dev/null || true
      log_line "periodic-save rc=0 epoch=$(date +%s)"
    else
      log_line "periodic-save rc=1 epoch=$(date +%s)"
    fi
  fi

  [ -n "$TT_PERIODIC_ONESHOT" ] && break
done

log_line "periodic-save-end server=$SERVER_PID epoch=$(date +%s)"
exit 0
