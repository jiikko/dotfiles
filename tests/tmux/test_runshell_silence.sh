#!/usr/bin/env bash
# run-shell -b で detach 起動され、**長時間 tmux のパイプを掴む**スクリプトが
# 無音契約 (`exec </dev/null >/dev/null 2>&1`) を実装しているかを固定する (issue 111)。
#
# なぜ必要か: この不変条件は一度ドリフトした。3 本のうち tmux_restore_runner.sh だけが
# リダイレクトを持たず、呼び出しごとの `>/dev/null` で代用していた。それでは
# **runner 自身が復元の間 (実測 4〜10 秒) パイプを掴み続ける**ので、run-shell がその間
# active のままになり、途中でサーバが死ぬと SIGPIPE を受ける。
#
# 🚨 対象は「run-shell 経由の全スクリプト」ではない。短命なロガー
# (tmux_log_*.sh) や対話 popup (tmux_scratch_popup.sh 等) は出力を持つ / すぐ終わるので
# この契約の対象外。**基準は「detach されて長く生きるか」**で、機械的には導出できないため
# 明示列挙する。
#
# 🚨 **リストは手で維持する**。基準 (「detach されて長く生きるか」) は機械的に導出できないので、
#   run-shell -b で起動する長命なスクリプトを足したら、ここにも足すこと。
#
# 🚨 exec の**置き場所はスクリプトごとに違う**。ファイル先頭に置けないものがある:
#   - tmux_schedule_keys.sh は対話 popup (wizard) も兼ねる。先頭で塞ぐと gum が端末を掴めない
#     → 長命なのは fire だけなので、その関数の中に置く
#   - tmux_resurrect_debounced_save.sh はテストが source して関数だけ使う。先頭で塞ぐと
#     テスト自身の出力まで消える → 直接実行の枝に置く
#   そのため下の検査は**行頭の空白を許す**。代わりに「実処理より前にあること」を目印で見る。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# 「対象:実処理の目印」。目印は「exec より後に来るはず」の行で、スクリプトごとに違う
# (共通の tt_trigger_log を持たないものがあるため)。目印が見つからない場合は exec の存在だけを見る。
targets=(
  "scripts/tmux_periodic_save.sh:tt_trigger_log "           # 周期保存 (viewer を開いている間ずっと生きる)
  "scripts/tmux_server_watchdog.sh:tt_trigger_log "         # 死亡監視 (サーバの寿命だけ生きる)
  "scripts/tmux_restore_runner.sh:tt_trigger_log "          # 手動復元 (復元が終わるまで生きる)
  "scripts/tmux_schedule_keys.sh:sleep \"\$wait_s\""          # 予約入力の fire (最長 30 日 sleep する)
  "scripts/tmux_resurrect_debounced_save.sh:tt_debounced_save_main$" # debounce 保存 (既定 10 秒)
)

fails=0
for spec in "${targets[@]}"; do
  rel="${spec%%:*}"
  marker="${spec#*:}"
  f="$ROOT_DIR/$rel"
  [ -f "$f" ] || { printf '✗ 対象が存在しない: %s\n' "$rel" >&2; fails=$((fails + 1)); continue; }
  # 🚨 字面の存在だけを見ると、末尾の死んだコードや heredoc 本文に同じ 32 バイトがあるだけで
  #   緑になる (敵対的レビューが変異で実証)。**実処理より前にあること**まで見る。
  #   行頭の空白は許す (関数の中 / if の枝に置くスクリプトがある。冒頭の注記を参照)。
  exec_line="$(grep -n '^[[:space:]]*exec </dev/null >/dev/null 2>&1$' "$f" | head -1 | cut -d: -f1)"
  work_line="$(grep -n -- "$marker" "$f" | grep -v '^[0-9]*:[[:space:]]*#' | head -1 | cut -d: -f1)"
  if [ -z "$work_line" ]; then
    printf '✗ %s の目印 (%s) が見つからない (検査が空振りしている)\n' "$rel" "$marker" >&2
    fails=$((fails + 1))
    continue
  fi
  if [ -n "$exec_line" ] && { [ -z "$work_line" ] || [ "$exec_line" -lt "$work_line" ]; }; then
    printf '✓ %s が無音契約を実装している (%s 行目、実処理より前)\n' "$rel" "$exec_line"
  elif [ -n "$exec_line" ]; then
    printf '✗ %s の exec が実処理より後にある (%s 行目 > %s 行目)\n' "$rel" "$exec_line" "$work_line" >&2
    fails=$((fails + 1))
    continue
  else
    printf '✗ %s に `exec </dev/null >/dev/null 2>&1` が無い\n' "$rel" >&2
    printf '  run-shell のパイプを掴んだままになり、サーバ死亡時に SIGPIPE を受ける\n' >&2
    fails=$((fails + 1))
  fi
done

# 対象 0 件を緑にしない (配列が空になったら赤にする)
if [ "${#targets[@]}" -eq 0 ]; then
  printf '✗ 対象が 0 件 (列挙が壊れている)\n' >&2
  exit 1
fi

if [ "$fails" -gt 0 ]; then
  printf '\n[test-runshell-silence] %d 件失敗\n' "$fails" >&2
  exit 1
fi
printf '\nAll run-shell silence tests passed successfully!\n'
