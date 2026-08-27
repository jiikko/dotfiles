#!/usr/bin/env bash
# scripts/tmux_schedule_keys.sh (prefix+m 予約入力) の unit テスト。PATH stub 方式 (偽 tmux / gum / date
# が呼び出しを記録) で、実 tmux サーバには触れない。tty 必須の gum TUI 描画は対象外。
#
# 固定する不変条件:
#   - 対象の固定: 冒頭で解決した pane_id がそのまま job に書かれ、fire の send-keys -t に渡ること
#   - 入力検証: 0h0m / 非数値 / 空文字 / confirm 拒否 / gum 未導入 では job も run-shell も生まれない
#   - fire はリテラル送信 (send-keys -l) + 別呼び出しの Enter。"Enter" という文字列がキー名に化けない
#   - fire は発火時刻まで送らない (早すぎる送信 = 予約の意味が無い)
#   - 送り先 pane が消えていたら送らず、無音 (stdout/stderr 空・exit 0) で job を掃く
#   - list からの取消は sleeper を kill し job/pid を消す。pid が死んだ stale job は list が掃く
#   - _tmux.conf: bind m が本スクリプトを指し、撤去済み launcher の unbind Enter が残っている
set -euo pipefail
unset CDPATH
unset TMUX TMUX_PANE 2>/dev/null || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/tmux_schedule_keys.sh"
CONF="$ROOT_DIR/_tmux.conf"
TMP_DIR="$(mktemp -d)"
FAKE_PIDS=()
cleanup() {
  for p in "${FAKE_PIDS[@]:-}"; do [[ -n "$p" ]] && kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

[[ -x "$SCRIPT" ]] || { printf '✗ スクリプトが存在しない/実行不可: %s\n' "$SCRIPT"; exit 1; }

CALLS="$TMP_DIR/calls.log"; : > "$CALLS"; export CALLS
export HOME="$TMP_DIR/home"; mkdir -p "$HOME"
export TMUX_SCHEDULE_KEYS_DIR="$TMP_DIR/state"
export XDG_CACHE_HOME="$TMP_DIR/cache"

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/bin_nogum"
# stub tmux: 呼び出しを記録し、スクリプトが使う照会にだけ応える。STUB_PANE_GONE=1 で %5 消滅を模す
cat > "$TMP_DIR/bin/tmux" <<'EOS2'
#!/bin/sh
echo "tmux $*" >> "$CALLS"
case "$*" in
  "display-message -p #{pane_id}") printf '%%5\n' ;;
  "display-message -p -t %5 "*)
    [ "${STUB_PANE_GONE:-0}" = 1 ] && exit 1
    case "$*" in *pane_id*) printf '%%5\n' ;; *) printf 'main:3 claude\n' ;; esac ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/tmux"; cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin_nogum/tmux"

# stub gum: input は STUB_GUM_INPUTS (改行区切り) を先頭から 1 つずつ返す / confirm は STUB_GUM_EXIT /
# choose は stdin の STUB_GUM_CHOOSE_INDEX 行目 (1 始まり) を返す / style は素通し
cat > "$TMP_DIR/bin/gum" <<'EOS2'
#!/bin/sh
echo "gum $*" >> "$CALLS"
case "$1" in
  input)
    q="${STUB_GUM_QUEUE:?}"
    [ -s "$q" ] || exit 1
    head -n1 "$q"; tail -n +2 "$q" > "$q.tmp"; mv "$q.tmp" "$q"; exit 0 ;;
  confirm) exit "${STUB_GUM_EXIT:-1}" ;;
  choose)  sed -n "${STUB_GUM_CHOOSE_INDEX:-1}p"; exit 0 ;;
  style)   shift; while [ $# -gt 0 ] && [ "${1#-}" != "$1" ]; do shift 2; done; printf '%s\n' "$*"; exit 0 ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/gum"

# stub date: `date +%s` だけ STUB_NOW で固定 (予約時刻の算術を決定論化)。他は実 date へ
cat > "$TMP_DIR/bin/date" <<'EOS2'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && [ "$*" = "+%s" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
exec /bin/date "$@"
EOS2
chmod +x "$TMP_DIR/bin/date"; cp "$TMP_DIR/bin/date" "$TMP_DIR/bin_nogum/date"

# sleep も stub: fire の待機は STUB_SLEEP_LOG に記録するだけ (実時間を待たない)。
# 「発火時刻まで送らない」は実 sleep で別に 1 回だけ測る (下の F2)
cat > "$TMP_DIR/bin/sleep" <<'EOS2'
#!/bin/sh
echo "sleep $*" >> "$CALLS"
[ -n "${STUB_REAL_SLEEP:-}" ] && exec /bin/sleep "$@"
exit 0
EOS2
chmod +x "$TMP_DIR/bin/sleep"; cp "$TMP_DIR/bin/sleep" "$TMP_DIR/bin_nogum/sleep"

STUB_PATH="$TMP_DIR/bin:/usr/bin:/bin"
NOGUM_PATH="$TMP_DIR/bin_nogum:/usr/bin:/bin"
export STUB_GUM_QUEUE="$TMP_DIR/gum_queue"
queue() { printf '%s\n' "$@" > "$STUB_GUM_QUEUE"; }
RUN_OUT="$TMP_DIR/out.log"; RUN_ERR="$TMP_DIR/err.log"
# shellcheck source=tests/tmux/lib/stub_assert_helper.sh
. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
jobs_count() { find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.job' 2>/dev/null | wc -l | tr -d ' '; }
reset_state() { rm -rf "$TMUX_SCHEDULE_KEYS_DIR"; reset_calls; }

printf '## new: 予約の生成\n'
reset_state; queue 1 30 "make test"
STUB_NOW=1000 STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" new
[[ "$RC" -eq 0 ]] || { printf '✗ new が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ job が 1 件でない (%s)\n' "$(jobs_count)"; exit 1; }
job="$(ls "$TMUX_SCHEDULE_KEYS_DIR"/*.job)"; id="$(basename "$job" .job)"
[[ "$(sed -n 1p "$job")" == "%5" ]] || { printf '✗ job の pane が固定した %%5 でない: %s\n' "$(sed -n 1p "$job")"; exit 1; }
[[ "$(sed -n 2p "$job")" == "6400" ]] || { printf '✗ 発火 epoch が now+1h30m (6400) でない: %s\n' "$(sed -n 2p "$job")"; exit 1; }
[[ "$(sed -n 3p "$job")" == "make test" ]] || { printf '✗ 文字列が保存されていない: %s\n' "$(sed -n 3p "$job")"; exit 1; }
printf '✓ job = 固定 pane / now+1h30m / 文字列 (空白保持)\n'
assert_called "tmux run-shell -b '$SCRIPT' fire '$id'" "sleeper を run-shell -b (サーバの子) として起動"
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 確認が --default=false でない\n'; exit 1; }
printf '✓ 確認は --default=false\n'

printf '\n## new: 弾かれる入力\n'
for bad in "0 0 make" "x 5 make" "1 -5 make"; do
  reset_state
  # shellcheck disable=SC2086 # 意図的に単語分割して 3 入力にする
  queue $bad
  STUB_NOW=1000 STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" new
  [[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ 入力 [%s] が予約された (RC=%s)\n' "$bad" "$RC"; exit 1; }
  assert_not_called "run-shell" "入力 [$bad] → run-shell も呼ばれない"
done
reset_state; queue 0 5 ""
STUB_NOW=1000 STUB_GUM_EXIT=0 run "$STUB_PATH" "$SCRIPT" new
[[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ 空文字が予約された\n'; exit 1; }
printf '✓ 空文字は予約されない\n'
reset_state; queue 0 5 "make test"
STUB_NOW=1000 STUB_GUM_EXIT=1 run "$STUB_PATH" "$SCRIPT" new
[[ "$(jobs_count)" == 0 ]] || { printf '✗ confirm 拒否なのに job ができた\n'; exit 1; }
assert_not_called "run-shell" "confirm 拒否 → 予約されない"
reset_state; queue 0 5 "make test"
run "$NOGUM_PATH" "$SCRIPT" new
[[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ gum 未導入で予約された\n'; exit 1; }
assert_not_called "run-shell" "gum 未導入 (exit 127) → 予約されない"

printf '\n## fire: 送信\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nEnter C-c \\ "q"\n' > "$TMUX_SCHEDULE_KEYS_DIR/j1.job"
STUB_NOW=1000 run "$STUB_PATH" "$SCRIPT" fire j1
[[ "$RC" -eq 0 ]] || { printf '✗ fire が exit %s\n' "$RC"; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- Enter C-c \ "q"' "リテラル送信 (-l): \"Enter\" がキー名に化けない"
assert_called 'tmux send-keys -t %5 Enter' "末尾の Enter は別呼び出し"
first_sk="$(grep 'send-keys' "$CALLS" | head -n1)"
[[ "$first_sk" == *' -l -- '* ]] || { printf '✗ -l 送信が Enter より先でない: %s\n' "$first_sk"; exit 1; }
printf '✓ 文字列 → Enter の順\n'
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/j1.pid" ]] || { printf '✗ 送信後に job/pid が残っている\n'; exit 1; }
printf '✓ 送信後は job/pid を掃く\n'
assert_not_called "sleep" "発火時刻を過ぎていれば待たない"

printf '\n## fire: 発火時刻まで送らない (実 sleep)\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n%s\nls\n' "$(( $(/bin/date +%s) + 2 ))" > "$TMUX_SCHEDULE_KEYS_DIR/j2.job"
( trap - EXIT; PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire j2 ) >/dev/null 2>&1 &
fire_pid=$!
/bin/sleep 0.5
assert_not_called "send-keys" "起動 0.5 秒後: まだ送っていない"
[[ "$(cat "$TMUX_SCHEDULE_KEYS_DIR/j2.pid" 2>/dev/null)" == "$fire_pid" ]] || { printf '✗ .pid が sleeper 自身の pid でない\n'; exit 1; }
printf '✓ .pid = sleeper の pid (取消の kill 先)\n'
wait "$fire_pid" || { printf '✗ fire が非 0 で終了\n'; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- ls' "発火時刻後に送信"

printf '\n## fire: 送り先 pane 消滅 → 無音で破棄\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nmake test\n' > "$TMUX_SCHEDULE_KEYS_DIR/j3.job"
STUB_NOW=1000 STUB_PANE_GONE=1 run "$STUB_PATH" "$SCRIPT" fire j3
[[ "$RC" -eq 0 ]] || { printf '✗ pane 消滅で exit %s (run-shell 経由なら view-mode が積まれる)\n' "$RC"; exit 1; }
[[ ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ pane 消滅で stdout/stderr に出力 (無音契約違反)\n'; cat "$RUN_OUT" "$RUN_ERR"; exit 1; }
assert_not_called "send-keys" "pane 消滅 → 送らない"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ pane 消滅の job が残っている\n'; exit 1; }
grep -q 'pane %5 gone' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 破棄がログに残らない\n'; exit 1; }
printf '✓ 無音 exit 0 + job 掃除 + ログ記録\n'

printf '\n## list: 取消と stale 掃除\n'
# shellcheck source=tests/tmux/lib/stub_env.sh
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_spawn_fake_proc; live_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/live.job"
printf '%s\n' "$live_pid" > "$TMUX_SCHEDULE_KEYS_DIR/live.pid"
STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=1 run "$STUB_PATH" "$SCRIPT" list
[[ "$RC" -eq 0 ]] || { printf '✗ list が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
/bin/sleep 0.2
kill -0 "$live_pid" 2>/dev/null && { printf '✗ 取消したのに sleeper が生きている\n'; exit 1; }
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/live.pid" ]] || { printf '✗ 取消後に job/pid が残っている\n'; exit 1; }
printf '✓ 取消 → sleeper kill + job/pid 削除\n'
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 取消確認が --default=false でない\n'; exit 1; }
printf '✓ 取消確認も --default=false\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_spawn_fake_proc; live_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/live.job"
printf '%s\n' "$live_pid" > "$TMUX_SCHEDULE_KEYS_DIR/live.pid"
STUB_GUM_EXIT=1 STUB_GUM_CHOOSE_INDEX=1 run "$STUB_PATH" "$SCRIPT" list
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 取消を拒否したのに sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 取消拒否で job が消えた\n'; exit 1; }
printf '✓ 取消拒否 → 何もしない\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_free_pid; dead_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/stale.job"
printf '%s\n' "$dead_pid" > "$TMUX_SCHEDULE_KEYS_DIR/stale.pid"
run "$STUB_PATH" "$SCRIPT" list
[[ "$RC" -eq 0 && "$(jobs_count)" == 0 ]] || { printf '✗ pid が死んだ stale job が掃かれない\n'; exit 1; }
assert_not_called "gum choose" "stale だけ → 一覧を出さず「予約はありません」"
printf '✓ stale job (pid 死亡) を掃く\n'

printf '\n## _tmux.conf: bind と撤去 bind の unbind\n'
grep -Eq "^bind m display-popup .*tmux_schedule_keys\.sh" "$CONF" || { printf '✗ bind m が tmux_schedule_keys.sh を指していない\n'; exit 1; }
printf '✓ bind m → display-popup → tmux_schedule_keys.sh\n'
grep -Eq '^unbind -T prefix Enter' "$CONF" || { printf '✗ 撤去した launcher (prefix+Enter) の unbind が無い (reload で旧 bind が残る)\n'; exit 1; }
printf '✓ unbind -T prefix Enter が残っている\n'
! grep -Eq '^bind(-key)? +(-T prefix +)?Enter ' "$CONF" || { printf '✗ prefix+Enter に bind が復活している\n'; exit 1; }
printf '✓ prefix+Enter への bind は無い\n'

printf '\n[test-schedule-keys] all ok\n'
