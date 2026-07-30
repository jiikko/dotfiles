#!/usr/bin/env bash
# スナップショット機構の健全性チェック。「保存/復元が静かに壊れている」を検出する。
#
# なぜ必要か (2026-07-30 の一次事実):
#   周期スナップショット保存が「設定されていただけで一度も動いていなかった」のに 17 日以上
#   誰も気づかなかった。原因は失敗が完全に silent だったこと。手動復元が 29 セッション中 22 で
#   途中死したのも無記録だった。個別のバグは直したが、同型の再発を防ぐには
#   「壊れていることが見える」機構自体が要る。それが本スクリプト。
#
# 判定項目 (どれか 1 つでも NG なら exit 1):
#   1. last スナップショットの鮮度 — 保存間隔の 2 倍 (最低 30 分) を超えて古ければ NG。
#      保存経路が全部死んでいる / ガードで恒久 reject されている等をまとめて捕まえる。
#   2. 常駐プロセスの生存 — 周期保存と watchdog が居るか (lock の owner pid で判定)。
#   3. last と pane_contents.tar.gz の世代整合 — layout より archive が極端に古ければ、
#      復元しても pane 内容が別世代になる (window は出るのに中身が無い silent な喪失)。
#   4. 保存内容と現状の乖離 — last に載っているセッション数と実セッション数の差を報告する
#      (live > saved = 未保存の作業がある / saved > live = 未復元のセッションがある)。
#      差自体は正常な場合もあるので NG にはせず、常に数値を出す。
#
# 使い方:
#   scripts/tmux_snapshot_health.sh          人間向けに全項目を表示 (exit 0/1)
#   scripts/tmux_snapshot_health.sh --quiet  NG のときだけ 1 行出力 (自動チェック用)
#
# 呼び出し元: watchdog の定期チェック (scripts/tmux_server_watchdog.sh) と手動。
# default socket のサーバ以外では判定対象外として exit 0 (隔離テストサーバを NG にしない)。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

TT_WATCHDOG_DIR="${TT_WATCHDOG_DIR:-$HOME/.cache/tt-watchdog}"
TT_PERIODIC_STATE_DIR="${TT_PERIODIC_STATE_DIR:-$HOME/.cache/tt-periodic-save}"
# 鮮度 NG の下限 (秒)。保存間隔が短く設定されていても、これより厳しくは判定しない
TT_HEALTH_MIN_STALE_SECONDS="${TT_HEALTH_MIN_STALE_SECONDS:-1800}"
# archive が layout より古くても許す猶予 (秒)。保存は layout → archive の順なので
# 正常時も僅かにずれる
TT_HEALTH_ARCHIVE_SKEW_SECONDS="${TT_HEALTH_ARCHIVE_SKEW_SECONDS:-600}"

quiet=0
[ "${1:-}" = "--quiet" ] && quiet=1

tt_on_default_server || exit 0

problems=()
lines=()

mtime_of() { stat -f '%m' "$1" 2>/dev/null || stat -c '%Y' "$1" 2>/dev/null; }

rdir="$(tt_resurrect_dir 2>/dev/null)"
[ -n "$rdir" ] || rdir="${XDG_DATA_HOME:-$HOME/.local/share}/tmux/resurrect"
last_link="$rdir/last"
now="$(date +%s)"

# --- 1. last の鮮度 --------------------------------------------------------------------
interval_min="$(tmux show -gqv @continuum-save-interval 2>/dev/null)"
case "$interval_min" in ''|*[!0-9]*|0) interval_min=15 ;; esac
threshold=$(( interval_min * 60 * 2 ))
[ "$threshold" -lt "$TT_HEALTH_MIN_STALE_SECONDS" ] && threshold="$TT_HEALTH_MIN_STALE_SECONDS"

last_target="$(readlink "$last_link" 2>/dev/null || true)"
if [ -z "$last_target" ] || [ ! -f "$rdir/$last_target" ]; then
  problems+=("last スナップショットが無い (保存が一度も成功していない)")
  lines+=("last: なし")
  last_age=-1
else
  last_mtime="$(mtime_of "$rdir/$last_target")"
  last_age=$(( now - ${last_mtime:-0} ))
  lines+=("last: $last_target ($(( last_age / 60 )) 分前 / 閾値 $(( threshold / 60 )) 分)")
  if [ "$last_age" -gt "$threshold" ]; then
    problems+=("last が $(( last_age / 60 )) 分前で古い (保存間隔 ${interval_min} 分の 2 倍超) — 保存経路が止まっている疑い")
  fi
fi

# --- 2. 常駐プロセスの生存 --------------------------------------------------------------
daemon_alive() {  # $1=state dir, $2=表示名
  local d owner found=0
  for d in "$1"/*.lock; do
    [ -d "$d" ] || continue
    owner="$(cat "$d/pid" 2>/dev/null)"
    if [ -n "$owner" ] && kill -0 "$owner" 2>/dev/null; then found=1; fi
  done
  if [ "$found" -eq 1 ]; then
    lines+=("$2: 稼働中")
  else
    problems+=("$2 が居ない")
    lines+=("$2: 不在")
  fi
}
daemon_alive "$TT_PERIODIC_STATE_DIR" "周期保存"
daemon_alive "$TT_WATCHDOG_DIR" "watchdog"

# --- 3. last と archive の世代整合 ------------------------------------------------------
archive="$rdir/pane_contents.tar.gz"
if [ "$(tmux show -gqv @resurrect-capture-pane-contents 2>/dev/null)" = "on" ]; then
  if [ ! -f "$archive" ]; then
    problems+=("pane_contents.tar.gz が無い (capture-pane-contents on なのに pane 内容が復元できない)")
    lines+=("archive: なし")
  elif [ "$last_age" -ge 0 ]; then
    a_mtime="$(mtime_of "$archive")"
    skew=$(( ${last_mtime:-0} - ${a_mtime:-0} ))
    lines+=("archive: last との時差 ${skew} 秒 (許容 ${TT_HEALTH_ARCHIVE_SKEW_SECONDS} 秒)")
    if [ "$skew" -gt "$TT_HEALTH_ARCHIVE_SKEW_SECONDS" ]; then
      problems+=("pane_contents.tar.gz が last より ${skew} 秒古い — 復元しても pane 内容が別世代になる")
    fi
  fi
fi

# --- 4. 保存内容と現状の乖離 (報告のみ) -------------------------------------------------
live=0
if sessions="$(tmux list-sessions -F '#{session_name}' 2>/dev/null)"; then
  live="$(printf '%s\n' "$sessions" | grep -cv "^${TT_HOLD_PREFIX}" || true)"
fi
saved=0
if [ "$last_age" -ge 0 ]; then
  saved="$(awk -F'\t' '$1=="pane"{s[$2]=1} END{n=0; for(k in s) n++; print n}' "$rdir/$last_target" 2>/dev/null || echo 0)"
fi
lines+=("セッション数: 実 $live / last に $saved")
if [ "$saved" -gt 0 ] && [ "$live" -gt 0 ] && [ "$saved" -gt "$live" ]; then
  lines+=("  ※ last の方が多い = 未復元のセッションがある可能性 (復元の途中死を疑う)")
fi

# --- 出力 -------------------------------------------------------------------------------
if [ "${#problems[@]}" -eq 0 ]; then
  if [ "$quiet" -eq 0 ]; then
    printf 'スナップショット機構: OK\n'
    printf '  %s\n' "${lines[@]}"
  fi
  exit 0
fi

if [ "$quiet" -eq 1 ]; then
  printf 'スナップショット異常: %s\n' "$(printf '%s / ' "${problems[@]}" | sed 's| / $||')"
else
  printf 'スナップショット機構: 異常 (%s 件)\n' "${#problems[@]}"
  printf '  ✗ %s\n' "${problems[@]}"
  printf '  --- 詳細 ---\n'
  printf '  %s\n' "${lines[@]}"
fi
exit 1
