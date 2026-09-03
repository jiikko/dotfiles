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

# mtime 取得は guards.sh の tt_mtime_of に集約 (GNU / BSD の stat 方言差の罠はあちらに記録)

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
archive="$rdir/pane_contents.tar.gz"
if [ -z "$last_target" ] || [ ! -f "$rdir/$last_target" ]; then
  problems+=("last スナップショットが無い (保存が一度も成功していない)")
  lines+=("last: なし")
  last_age=-1
else
  last_mtime="$(tt_mtime_of "$rdir/$last_target")"
  # 🚨 鮮度の入力は「last の mtime」だけにしないこと。upstream は状態無変化のとき新 layout を
  #   捨てて last を据え置く (dedup) ため、保存が成功していても last の mtime は前進しない。
  #   構成変化のない夜間・週末に「保存経路が止まっている」と誤検知する (レビュー指摘 2026-07-30)。
  #   保存が走ったことは archive の mtime に現れるので、両者の新しい方を「最後に保存が成功した
  #   時刻」として使う。
  fresh_mtime="${last_mtime:-0}"
  if [ -f "$archive" ]; then
    a_m="$(tt_mtime_of "$archive")"
    [ "${a_m:-0}" -gt "$fresh_mtime" ] && fresh_mtime="$a_m"
  fi
  last_age=$(( now - fresh_mtime ))
  lines+=("last: $last_target (最後の保存 $(( last_age / 60 )) 分前 / 閾値 $(( threshold / 60 )) 分)")
  if [ "$last_age" -gt "$threshold" ]; then
    problems+=("最後の保存が $(( last_age / 60 )) 分前で古い (保存間隔 ${interval_min} 分の 2 倍超) — 保存経路が止まっている疑い")
  fi
fi

# --- 2. 常駐プロセスの生存 --------------------------------------------------------------
daemon_alive() {  # $1=state dir, $2=表示名
  local d found=0
  for d in "$1"/*.lock; do
    [ -d "$d" ] || continue
    # 🚨 owner ファイルを自分で読んで kill -0 に渡さないこと。書式 ("pid<TAB>lstart") を
    # 知っているのは guards.sh 側だけで、tab 以降が混ざると kill が illegal pid で必ず失敗し、
    # 常駐プロセスが生きていても「不在」と誤報する (実測 2026-08-20)。判定は書き手と対の
    # tt_lock_owner_alive に委ねる (pid 再利用の照合もそちらが持つ)。
    if tt_lock_owner_alive "$d"; then found=1; fi
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

# --- 3. archive の中身の健全性 (mtime ではなく実際に読めるかで判定) ---------------------
# 🚨 mtime 比較では今回の破損を検出できない。truncate された archive は mtime が新しくなるため
#   「archive の方が新しい = OK」に見えてしまう (レビューで実証 2026-07-30: 200 byte の壊れた
#   archive でも mtime 判定は OK を返した)。中身を読んで last の pane 集合を包含しているかだけが
#   唯一この破損を捕まえる検査。壊れていれば「window は復元されるのに全 pane の scrollback が
#   空」という silent なデータ喪失が確定している状態。
if [ "$(tmux show -gqv @resurrect-capture-pane-contents 2>/dev/null)" = "on" ]; then
  if [ ! -f "$archive" ]; then
    problems+=("pane_contents.tar.gz が無い (capture-pane-contents on なのに pane 内容が復元できない)")
    lines+=("archive: なし")
  elif ! gzip -t "$archive" 2>/dev/null; then
    problems+=("pane_contents.tar.gz が壊れている (gzip として読めない) — 復元すると全 pane の scrollback が失われる")
    lines+=("archive: 破損 ($(wc -c < "$archive" | tr -d ' ') bytes)")
  else
    entries="$(tar tzf "$archive" 2>/dev/null | grep -c 'pane-' || true)"
    lines+=("archive: entry $entries 個")
    if [ "${entries:-0}" -eq 0 ]; then
      problems+=("pane_contents.tar.gz に pane が 1 つも入っていない — scrollback が復元されない")
    elif [ "$last_age" -ge 0 ]; then
      # last の pane 行が archive の entry に含まれているか (世代不一致・部分欠落の検出)
      want="$(awk -F'\t' '$1=="pane"{printf "pane-%s:%s.%s\n", $2, $3, $6}' "$rdir/$last_target" 2>/dev/null | sort -u)"
      have="$(tar tzf "$archive" 2>/dev/null | sed -E 's|^\./pane_contents/||' | sort -u)"
      miss="$(comm -23 <(printf '%s\n' "$want") <(printf '%s\n' "$have") 2>/dev/null | grep -c . || true)"
      lines+=("archive: last の pane のうち未収録 $miss 個")
      if [ "${miss:-0}" -gt 0 ]; then
        problems+=("archive に last の pane が $miss 個入っていない — その pane は復元しても中身が空になる")
      fi
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
