#!/usr/bin/env bash
# scripts/tmux_periodic_save.sh (周期スナップショットの長寿命ドライバ) の unit テスト。
#
# continuum の毎秒 fork な interpolation 方式を置き換えた実装なので、置き換えで失われては
# 困る性質を pin する:
#   (1) 1 周で保存 wrapper を quiet 起動し、continuum の最終保存時刻も進める
#   (2) default socket 以外では完全 no-op (共有の last を上書きしない)
#   (3) サーバ pid が消えていたら保存せず終了する (取り残しが本番を触らない)
#   (4) 二重起動は先任が生きている限り退く (conf 再 source 対策)
#   (5) 保存スクリプト未解決でも無害終了 + 記録
# TT_PERIODIC_ONESHOT=1 で無限ループを 1 周に縮め、間隔は @continuum-save-interval を
# stub で 0 に落として即時実行させる (0 は既定へ丸められるので TT_PERIODIC_DEFAULT_MINUTES で制御)。
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_periodic_save.sh"
TMP_DIR="$(mktemp -d)"
FAKE_PIDS=()
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/stub_env.sh"
cleanup() {
  for p in "${FAKE_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT



CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/state"

cat > "$TMP_DIR/bin/tmux" <<'EOS'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$1" in
  display-message) printf '%s\n' "${STUB_SOCKET_PATH:-}" ;;
  show)
    case "$*" in
      *@continuum-save-interval*)        printf '%s\n' "${STUB_INTERVAL:-}" ;;
      *@resurrect-save-script-path*)     printf '%s\n' "${STUB_SAVE_SCRIPT:-}" ;;
    esac ;;
esac
EOS
chmod +x "$TMP_DIR/bin/tmux"

cat > "$TMP_DIR/bin/fake_save.sh" <<'EOS'
#!/bin/sh
echo "save $*" >> "$CALLS"
[ -n "${STUB_SAVE_FAIL:-}" ] && exit 1
exit 0
EOS
chmod +x "$TMP_DIR/bin/fake_save.sh"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
LOG="$TMP_DIR/trigger.log"

. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"

# sleep 0 相当にするため既定間隔を 0 分にはできない (0 は既定へ丸める仕様) ので、
# sleep を即返す stub で置き換える
cat > "$TMP_DIR/bin/sleep" <<'EOS'
#!/bin/sh
echo "sleep $*" >> "$CALLS"
exit 0
EOS
chmod +x "$TMP_DIR/bin/sleep"

run_periodic() {  # $1=fake server pid, 以降は env で制御
  TT_TRIGGER_LOG="$LOG" TT_PERIODIC_STATE_DIR="$TMP_DIR/state" TT_PERIODIC_ONESHOT=1 \
    run "$STUB_PATH" "$SCRIPT" "$1"
}

# --- (1) 正常系: 保存が走り timestamp が進み記録される --------------------------------
reset_calls; : > "$LOG"
tt_spawn_fake_proc; FAKE="$REPLY_PID"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run_periodic "$FAKE"
[[ "$RC" -eq 0 ]] || { printf '✗ 正常系で exit %s\n' "$RC"; exit 1; }
assert_called "save quiet" "保存 wrapper が quiet で起動される"
assert_called "set-option -g @continuum-save-last-timestamp" "continuum の最終保存時刻を進める"
grep -qE '	periodic-save-begin server=[0-9]+ interval=900s epoch=[0-9]+' "$LOG" \
  || { printf '✗ begin 行の書式/既定間隔 (900s) が想定と違う:\n'; cat "$LOG"; exit 1; }
grep -qE '	periodic-save rc=0 epoch=[0-9]+' "$LOG" || { printf '✗ periodic-save rc=0 が無い:\n'; cat "$LOG"; exit 1; }
printf '✓ 1 周で保存 + timestamp 更新 + begin/save の記録\n'

# --- (2) @continuum-save-interval が間隔の出典になっている ----------------------------
reset_calls; : > "$LOG"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_INTERVAL=5 STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run_periodic "$FAKE"
assert_called "sleep 300" "@continuum-save-interval=5 分 → sleep 300 秒"

# --- (3) 保存失敗は rc=1 として記録される (無音で成功扱いにしない) --------------------
reset_calls; : > "$LOG"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" STUB_SAVE_FAIL=1 \
  run_periodic "$FAKE"
grep -q 'periodic-save rc=1' "$LOG" || { printf '✗ 保存失敗が rc=1 で記録されない:\n'; cat "$LOG"; exit 1; }
assert_not_called "set-option -g @continuum-save-last-timestamp" "保存失敗時は timestamp を進めない"

# --- (4) 非 default socket → 完全 no-op ------------------------------------------------
reset_calls; : > "$LOG"
STUB_SOCKET_PATH="/nowhere/tmux-501/lab" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run_periodic "$FAKE"
[[ "$RC" -eq 0 ]] || { printf '✗ 非 default socket で exit %s\n' "$RC"; exit 1; }
assert_not_called "save quiet" "非 default socket では保存しない"
[[ ! -s "$LOG" ]] || { printf '✗ 非 default socket でログが書かれた:\n'; cat "$LOG"; exit 1; }
printf '✓ 非 default socket (テストサーバ) では完全 no-op\n'

# --- (5) サーバ pid 消滅 → 保存せず終了 ------------------------------------------------
reset_calls; : > "$LOG"
tt_free_pid; DEAD="$REPLY_PID"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run_periodic "$DEAD"
assert_not_called "save quiet" "サーバ消滅時は保存しない (取り残しが本番を触らない)"
grep -q 'periodic-save-end' "$LOG" || { printf '✗ 終了が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ サーバ pid 消滅で保存せず終了\n'

# --- (6) 二重起動ガード ----------------------------------------------------------------
reset_calls; : > "$LOG"
mkdir -p "$TMP_DIR/state/$FAKE.lock"
tt_spawn_fake_proc; HOLDER="$REPLY_PID"
printf '%s\n' "$HOLDER" > "$TMP_DIR/state/$FAKE.lock/pid"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run_periodic "$FAKE"
[[ "$RC" -eq 0 ]] || { printf '✗ 二重起動で exit %s (0 のはず)\n' "$RC"; exit 1; }
assert_not_called "save quiet" "先任が生きている間は後発が保存しない"
[[ "$(cat "$TMP_DIR/state/$FAKE.lock/pid")" == "$HOLDER" ]] \
  || { printf '✗ 後発が先任の lock を奪った\n'; exit 1; }
printf '✓ 二重起動は先任の lock を奪わず退く\n'
kill "$HOLDER" 2>/dev/null; rm -rf "$TMP_DIR/state/$FAKE.lock"

# --- (7) 保存スクリプト未解決 ----------------------------------------------------------
reset_calls; : > "$LOG"
STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="" run_periodic "$FAKE"
[[ "$RC" -eq 0 ]] || { printf '✗ 未解決で exit %s\n' "$RC"; exit 1; }
grep -q 'periodic-save skipped=no-save-script' "$LOG" \
  || { printf '✗ skipped=no-save-script が記録されない:\n'; cat "$LOG"; exit 1; }
printf '✓ 保存スクリプト未解決でも無害終了 + 記録\n'

# --- (8) 観測ログの上限刈り (rotate 無しの単調増加を止める) ----------------------------
reset_calls
: > "$LOG"
for i in $(seq 1 40); do printf 'line-%s\n' "$i" >> "$LOG"; done
TT_TRIGGER_LOG="$LOG" TT_PERIODIC_STATE_DIR="$TMP_DIR/state" TT_PERIODIC_ONESHOT=1 \
  TT_TRIGGER_LOG_MAX_LINES=10 \
  STUB_SOCKET_PATH="$TT_DEFAULT_SOCK" STUB_SAVE_SCRIPT="$TMP_DIR/bin/fake_save.sh" \
  run "$STUB_PATH" "$SCRIPT" "$FAKE"
n="$(wc -l < "$LOG" | tr -d ' ')"
# 刈った 10 行 + その後に自分が書く periodic-save / periodic-save-end の 2 行
[ "$n" -le 13 ] || { printf '✗ ログが刈られていない (%s 行):\n' "$n"; head -3 "$LOG"; exit 1; }
grep -q 'line-40' "$LOG" || { printf '✗ 末尾の新しい行が失われた (古い方を残してしまった)\n'; exit 1; }
grep -q 'line-1$' "$LOG" && { printf '✗ 古い行が残っている (刈れていない)\n'; exit 1; }
printf '✓ 観測ログを上限行数に刈る (新しい側を残す。%s 行)\n' "$n"

printf '\nAll periodic-save tests passed successfully!\n'
