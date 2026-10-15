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
#   sleep に置き換えて fork を 15 分に 1 回へ落とす (.claude/rules/zsh-hook-return-via-reply.md と同思想)。
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


# ---- 二重起動ガード (conf 再 source で run-shell が毎回走るため) ----------------------
mkdir -p "$TT_PERIODIC_STATE_DIR" 2>/dev/null || exit 0
# stale 掃除と lock 取得は guards.sh に集約 (watchdog と逐語同一だった)。owner 判定を pid だけで
# 行わない理由 (pid 再利用で先任と誤認すると保存が張られない) は guards.sh 側に書いてある。
tt_lock_sweep_stale "$TT_PERIODIC_STATE_DIR"
LOCK_DIR="$TT_PERIODIC_STATE_DIR/$SERVER_PID.lock"
tt_lock_rc=0
tt_lock_acquire "$LOCK_DIR" || tt_lock_rc=$?
if [ "$tt_lock_rc" -eq 1 ]; then
  exit 0   # 先任が (同一プロセスとして) 生きている = 正常。無音で退く
elif [ "$tt_lock_rc" -ne 0 ]; then
  # 🚨 「lock を作れなかった」を rc=1 と畳んで無音にしないこと。状態ディレクトリが書けない等で
  #   ここに落ちると **周期保存が二度と張られない**のに、stdout も観測ログも 0 byte になる
  #   (実測 2026-08-25: chmod 500 で再現。装置不在が完全に無音だった)。
  tt_trigger_log "periodic-save-aborted reason=lock-failed server=$SERVER_PID epoch=$(date +%s)"
  exit 0
fi
# shellcheck disable=SC2329 # trap 経由の間接呼び出し
cleanup() { tt_lock_release_if_owner "$LOCK_DIR"; }
trap cleanup EXIT

interval_seconds() {
  printf '%s' "$(( $(tt_save_interval_minutes "$TT_PERIODIC_DEFAULT_MINUTES") * 60 ))"
}

tt_trigger_log "periodic-save-begin server=$SERVER_PID interval=$(interval_seconds)s epoch=$(date +%s)"

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
  tt_prune_log_lines "$TT_TRIGGER_LOG" "$TT_TRIGGER_LOG_MAX_LINES"
  # socket gate は毎周期見る (サーバ入れ替わりで別環境の last を触らない)
  tt_on_default_server || break

  # 保存の可否は wrapper 側のガードが、最終保存時刻の簿記は tt_run_resurrect_save (guards.sh) が持つ
  save_rc=0
  tt_run_resurrect_save || save_rc=$?
  case "$save_rc" in
    2) tt_trigger_log "periodic-save skipped=no-save-script epoch=$(date +%s)" ;;
    *) tt_trigger_log "periodic-save rc=$save_rc epoch=$(date +%s)" ;;
  esac

  [ -n "$TT_PERIODIC_ONESHOT" ] && break
done

tt_trigger_log "periodic-save-end server=$SERVER_PID epoch=$(date +%s)"
exit 0
