#!/usr/bin/env bash
# run-shell -b で detach 起動され、**長時間 tmux のパイプを掴む**スクリプトが
# 無音契約 (`exec </dev/null >/dev/null 2>&1`) を実装しているかを固定する (issue 111)。
#
# なぜ必要か: この不変条件は一度ドリフトした。3 本のうち tmux_restore_runner.sh だけが
# リダイレクトを持たず、呼び出しごとの `>/dev/null` で代用していた。それでは
# **runner 自身が復元の間 (実測 4〜10 秒) パイプを掴み続ける**ので、run-shell がその間
# active のままになり、途中でサーバが死ぬと SIGPIPE を受ける。
#
# ⚠️ 対象は「run-shell 経由の全スクリプト」ではない。短命なロガー
# (tmux_log_*.sh) や対話 popup (tmux_scratch_popup.sh 等) は出力を持つ / すぐ終わるので
# この契約の対象外。**基準は「detach されて長く生きるか」**で、機械的には導出できないため
# 明示列挙する。
#
# ⚠️ **同じ基準に当てはまるのに未対応の 2 本がある** (issue 129):
#   scripts/tmux_schedule_keys.sh (fire は最大 30 日 sleep する) と
#   scripts/tmux_resurrect_debounced_save.sh (既定 10 秒)。
#   ここに足すには先に本体へリダイレクトを入れる必要があり、111 の範囲外なので分けた。
#   **「この 3 本で全部」ではない**ことを、リストを読む人に伝えるためここに書いておく。
set -euo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
targets=(
  scripts/tmux_periodic_save.sh   # 周期保存 (viewer を開いている間ずっと生きる)
  scripts/tmux_server_watchdog.sh # 死亡監視 (サーバの寿命だけ生きる)
  scripts/tmux_restore_runner.sh  # 手動復元 (復元が終わるまで生きる)
)

fails=0
for rel in "${targets[@]}"; do
  f="$ROOT_DIR/$rel"
  [ -f "$f" ] || { printf '✗ 対象が存在しない: %s\n' "$rel" >&2; fails=$((fails + 1)); continue; }
  # ⚠️ 字面の存在だけを見ると、末尾の死んだコードや heredoc 本文に同じ 32 バイトがあるだけで
  #   緑になる (敵対的レビューが変異で実証)。**実処理より前にあること**まで見る。
  #   実処理の目印は tt_trigger_log — 3 本とも exec の後にしか現れない。
  exec_line="$(grep -n '^exec </dev/null >/dev/null 2>&1$' "$f" | head -1 | cut -d: -f1)"
  work_line="$(grep -n 'tt_trigger_log ' "$f" | grep -v '^[0-9]*:#' | head -1 | cut -d: -f1)"
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
