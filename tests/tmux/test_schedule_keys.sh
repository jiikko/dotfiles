#!/usr/bin/env bash
# scripts/tmux_schedule_keys.sh (prefix+m 予約入力) の unit テスト。PATH stub 方式 (偽 tmux / gum / date
# が呼び出しを記録) で、実 tmux サーバには触れない。tty 必須の gum TUI 描画は対象外。
#
# 固定する不変条件:
#   - 対象の固定: 冒頭で解決した pane_id がそのまま job に書かれ、fire の send-keys -t に渡ること
#   - いつ送るか: プリセット (choose の行) / 自由入力 (90・1h30m・1:30 → 秒) / 時刻指定 (HH:MM、過去なら翌日)
#     が正しい発火 epoch になる。0・非数値・桁超・不正な時刻・choose キャンセル・空文字・confirm 拒否・
#     gum 未導入 では job も run-shell も生まれない
#   - fire はリテラル送信 (send-keys -l) + 別呼び出しの Enter。"Enter" という文字列がキー名に化けない
#   - fire は発火時刻まで送らない (早すぎる送信 = 予約の意味が無い)
#   - 送り先 pane が消えていたら (send-keys が失敗したら) Enter を送らず、無音 (stdout/stderr 空・
#     exit 0) で job を掃き、成功ログを書かない
#   - .pid の数字だけで kill しない: 無関係なプロセスが同じ pid を持っていても kill せず、stale として掃く
#   - 一覧の取消は表示文字列の逆引きでなく行頭の連番で選ぶ (表示が一致する 2 件で先頭に化けない)
#   - list からの取消は sleeper を kill し job/pid を消す。pid が死んだ stale job は list が掃く
#   - _tmux.conf: bind m / Enter / C-m が本スクリプトを指し、旧 launcher (display-menu) が復活していない
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
  "send-keys -t %5 "*)
    # 実 tmux は pane 不在で stderr にエラーを出して rc=1 (この stderr が run-shell 経由で view-mode に積まれる)
    [ "${STUB_PANE_GONE:-0}" = 1 ] && { echo "can't find pane: %5" >&2; exit 1; } ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/tmux"; cp "$TMP_DIR/bin/tmux" "$TMP_DIR/bin_nogum/tmux"

# stub gum: input は STUB_GUM_QUEUE のファイルから先頭 1 行ずつ返す / confirm は STUB_GUM_EXIT /
# choose は stdin の STUB_GUM_CHOOSE_INDEX 行目 (1 始まり) を返す (STUB_GUM_CHOOSE_EXIT でキャンセル) /
# style は素通し
cat > "$TMP_DIR/bin/gum" <<'EOS2'
#!/bin/sh
echo "gum $*" >> "$CALLS"
case "$1" in
  input)
    q="${STUB_GUM_QUEUE:?}"
    [ -s "$q" ] || exit 1
    head -n1 "$q"; tail -n +2 "$q" > "$q.tmp"; mv "$q.tmp" "$q"; exit 0 ;;
  confirm) exit "${STUB_GUM_EXIT:-1}" ;;
  choose)  [ -n "${STUB_GUM_CHOOSE_EXIT:-}" ] && exit "$STUB_GUM_CHOOSE_EXIT"; sed -n "${STUB_GUM_CHOOSE_INDEX:-1}p"; exit 0 ;;
  style)   shift; while [ $# -gt 0 ] && [ "${1#-}" != "$1" ]; do shift 2; done; printf '%s\n' "$*"; exit 0 ;;
esac
exit 0
EOS2
chmod +x "$TMP_DIR/bin/gum"

# stub date: `date +%s` を STUB_NOW、`date +%H:%M` を STUB_NOW_HM で固定 (予約時刻の算術を決定論化)。他は実 date へ
cat > "$TMP_DIR/bin/date" <<'EOS2'
#!/bin/sh
[ -n "${STUB_NOW:-}" ] && [ "$*" = "+%s" ] && { printf '%s\n' "$STUB_NOW"; exit 0; }
[ -n "${STUB_NOW_HM:-}" ] && [ "$*" = "+%H:%M" ] && { printf '%s\n' "$STUB_NOW_HM"; exit 0; }
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
queue() { : > "$STUB_GUM_QUEUE"; local a; for a in "$@"; do printf '%s\n' "$a" >> "$STUB_GUM_QUEUE"; done; }
# 送る文字列は gum でなく read -e (readline) で受けるので、stdin (ヒアストリング) で渡す。
# IME の未確定文字が入力位置に出るか (本物のカーソル) は pty 必須で自動検証できない → 配線を静的に pin し、
# 挙動は issue 125 (human) へ回す
TEXT_IN="make test"
RUN_OUT="$TMP_DIR/out.log"; RUN_ERR="$TMP_DIR/err.log"
# shellcheck source=tests/tmux/lib/stub_assert_helper.sh
. "$ROOT_DIR/tests/tmux/lib/stub_assert_helper.sh"
jobs_count() { find "$TMUX_SCHEDULE_KEYS_DIR" -name '*.job' 2>/dev/null | wc -l | tr -d ' '; }
reset_state() { rm -rf "$TMUX_SCHEDULE_KEYS_DIR"; reset_calls; }

# choose の行: 1..8 = プリセット (5m 10m 15m 30m 1h 2h 4h 8h) / 9 = 時刻指定 / 10 = 自由入力
IDX_CLOCK=9; IDX_FREE=10

printf '## new: 予約の生成 (プリセット)\n'
reset_state; queue
STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=5 run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
[[ "$RC" -eq 0 ]] || { printf '✗ new が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ job が 1 件でない (%s)\n' "$(jobs_count)"; exit 1; }
job="$(ls "$TMUX_SCHEDULE_KEYS_DIR"/*.job)"; id="$(basename "$job" .job)"
[[ "$(sed -n 1p "$job")" == "%5" ]] || { printf '✗ job の pane が固定した %%5 でない: %s\n' "$(sed -n 1p "$job")"; exit 1; }
[[ "$(sed -n 2p "$job")" == "4600" ]] || { printf '✗ 「1 時間後」の発火 epoch が now+3600 (4600) でない: %s\n' "$(sed -n 2p "$job")"; exit 1; }
[[ "$(sed -n 3p "$job")" == "make test" ]] || { printf '✗ 文字列が保存されていない: %s\n' "$(sed -n 3p "$job")"; exit 1; }
printf '✓ job = 固定 pane / プリセット 5 行目 (1 時間後) = now+3600 / 文字列 (空白保持)\n'
assert_called "tmux run-shell -b '$SCRIPT' fire '$id'" "sleeper を run-shell -b (サーバの子) として起動"
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 確認が --default=false でない\n'; exit 1; }
printf '✓ 確認は --default=false\n'
assert_not_called "gum input" "文字列は gum input を通らない (IME の未確定文字が下にずれる。read -e で受ける)"
grep -Eq '^\s*IFS= read -e -r -p .* REPLY_TEXT' "$SCRIPT" || { printf '✗ 文字列入力が readline (read -e) でない\n'; exit 1; }
printf '✓ 文字列入力は read -e (readline = 本物のカーソル) の配線\n'
# プリセットの両端も pin (行と値の対応が崩れると別の時刻に予約される)
for pair in "1:1300" "8:29800"; do
  reset_state; queue
  STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX="${pair%%:*}" run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
  [[ "$(sed -n 2p "$TMUX_SCHEDULE_KEYS_DIR"/*.job 2>/dev/null)" == "${pair##*:}" ]] \
    || { printf '✗ プリセット %s 行目の発火 epoch が %s でない\n' "${pair%%:*}" "${pair##*:}"; exit 1; }
done
printf '✓ プリセット 1 行目 = 5 分後 / 8 行目 = 8 時間後\n'

printf '\n## new: 自由入力 (相対時間の書式)\n'
for pair in "1h30m:6400" "90:6400" "1:30:6400" "45m:3700" "2h:8200" "1H30:6400" "1:05:4900"; do
  in="${pair%:*}"; want="${pair##*:}"
  reset_state; queue "$in"
  STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=$IDX_FREE run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
  got="$(sed -n 2p "$TMUX_SCHEDULE_KEYS_DIR"/*.job 2>/dev/null)"
  [[ "$RC" -eq 0 && "$got" == "$want" ]] || { printf '✗ 自由入力 [%s] → epoch %s (期待 %s, RC=%s)\n' "$in" "$got" "$want" "$RC"; exit 1; }
done
printf '✓ 90 (分) / 1h30m / 1:30 / 45m / 2h / 1H30 / 1:05 が正しい秒になる\n'
for bad in "0" "0h0m" "abc" "123456" "1h30x" "-5" "" "1:60"; do
  reset_state; queue "$bad"
  STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=$IDX_FREE run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
  [[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ 自由入力 [%s] が予約された (RC=%s)\n' "$bad" "$RC"; exit 1; }
  assert_not_called "run-shell" "自由入力 [$bad] → 予約されない"
done

printf '\n## new: 時刻指定 (HH:MM。過去なら翌日)\n'
for pair in "10:30:2800" "09:00:83800" "10:00:87400" "23:59:51340" "0:00:51400"; do
  in="${pair%:*}"; want="${pair##*:}"
  reset_state; queue "$in"
  STUB_NOW=1000 STUB_NOW_HM=10:00 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=$IDX_CLOCK run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
  got="$(sed -n 2p "$TMUX_SCHEDULE_KEYS_DIR"/*.job 2>/dev/null)"
  [[ "$RC" -eq 0 && "$got" == "$want" ]] || { printf '✗ 時刻 [%s] (now 10:00) → epoch %s (期待 %s, RC=%s)\n' "$in" "$got" "$want" "$RC"; exit 1; }
done
printf '✓ 10:30 → +30m / 09:00・10:00 (過去・同時刻) → 翌日 / 23:59 / 0:00\n'
for bad in "25:00" "10:60" "1030" "10:5" "abc"; do
  reset_state; queue "$bad"
  STUB_NOW=1000 STUB_NOW_HM=10:00 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=$IDX_CLOCK run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
  [[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ 時刻 [%s] が予約された (RC=%s)\n' "$bad" "$RC"; exit 1; }
done
printf '✓ 25:00 / 10:60 / 1030 / 10:5 / abc は弾く\n'

printf '\n## new: 中断・拒否・未導入\n'
reset_state; queue
STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_EXIT=1 run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
[[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ choose キャンセルで予約された\n'; exit 1; }
assert_not_called "gum input" "choose キャンセル → 以降の入力に進まない"
reset_state; queue; TEXT_IN=""
STUB_NOW=1000 STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=4 run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
[[ "$RC" -ne 0 && "$(jobs_count)" == 0 ]] || { printf '✗ 空文字が予約された\n'; exit 1; }
printf '✓ 空文字は予約されない\n'
TEXT_IN="make test"
reset_state; queue
STUB_NOW=1000 STUB_GUM_EXIT=1 STUB_GUM_CHOOSE_INDEX=4 run "$STUB_PATH" "$SCRIPT" new <<< "$TEXT_IN"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ confirm 拒否なのに job ができた\n'; exit 1; }
assert_not_called "run-shell" "confirm 拒否 → 予約されない"
reset_state; queue
run "$NOGUM_PATH" "$SCRIPT" new <<< "$TEXT_IN"
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

printf '\n## fire: 壊れた job (文字列行なし) は送らず掃く\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\n' > "$TMUX_SCHEDULE_KEYS_DIR/j4.job"
STUB_NOW=1000 run "$STUB_PATH" "$SCRIPT" fire j4
[[ "$RC" -eq 0 && ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ 壊れた job で無音 exit 0 でない (RC=%s)\n' "$RC"; exit 1; }
assert_not_called "send-keys" "文字列行の無い job → 素の Enter を送らない"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ 壊れた job が残っている\n'; exit 1; }
printf '✓ 壊れた job は掃く\n'

printf '\n## fire: 送り先 pane 消滅 → 無音で破棄\n'
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n900\nmake test\n' > "$TMUX_SCHEDULE_KEYS_DIR/j3.job"
rm -f "$XDG_CACHE_HOME/tt-schedule-keys.log"   # 前ブロックの成功ログを持ち越さない
STUB_NOW=1000 STUB_PANE_GONE=1 run "$STUB_PATH" "$SCRIPT" fire j3
[[ "$RC" -eq 0 ]] || { printf '✗ pane 消滅で exit %s (run-shell 経由なら view-mode が積まれる)\n' "$RC"; exit 1; }
[[ ! -s "$RUN_OUT" && ! -s "$RUN_ERR" ]] || { printf '✗ pane 消滅で stdout/stderr に出力 (無音契約違反)\n'; cat "$RUN_OUT" "$RUN_ERR"; exit 1; }
assert_called 'tmux send-keys -t %5 -l -- make test' "pane 消滅は send-keys の失敗で検知する (事前チェックとの TOCTOU を作らない)"
[[ "$(jobs_count)" == 0 ]] || { printf '✗ pane 消滅の job が残っている\n'; exit 1; }
grep -q 'pane %5 gone' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 破棄がログに残らない\n'; exit 1; }
! grep -q 'sent to' "$XDG_CACHE_HOME/tt-schedule-keys.log" || { printf '✗ 送れていないのに成功ログ\n'; exit 1; }
assert_not_called "send-keys -t %5 Enter" "文字列の送信に失敗したら Enter も送らない"
printf '✓ 無音 exit 0 + job 掃除 + ログ記録 (成功ログ無し)\n'

printf '\n## list: 取消と stale 掃除\n'
# shellcheck source=tests/tmux/lib/stub_env.sh
. "$ROOT_DIR/tests/tmux/lib/stub_env.sh"
# 本物の sleeper を起こす (pid_is_sleeper は ps の command line で「自分の fire <id>」を確かめるため、
# 偽 pid では代用できない)。at は十分先、sleep は実物
spawn_sleeper() {  # $1=id  → SLEEPER_PID
  printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/$1.job"
  ( trap - EXIT; PATH="$STUB_PATH" STUB_REAL_SLEEP=1 exec "$SCRIPT" fire "$1" ) >/dev/null 2>&1 &
  SLEEPER_PID=$!; FAKE_PIDS+=("$SLEEPER_PID")
  local i=0; while [[ ! -s "$TMUX_SCHEDULE_KEYS_DIR/$1.pid" && $i -lt 50 ]]; do /bin/sleep 0.1; i=$((i+1)); done
  [[ "$(cat "$TMUX_SCHEDULE_KEYS_DIR/$1.pid")" == "$SLEEPER_PID" ]] || { printf '✗ sleeper %s の .pid が書かれない\n' "$1"; exit 1; }
}
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=1 run "$STUB_PATH" "$SCRIPT" list
[[ "$RC" -eq 0 ]] || { printf '✗ list が exit %s\n' "$RC"; cat "$RUN_ERR"; exit 1; }
/bin/sleep 0.3
kill -0 "$live_pid" 2>/dev/null && { printf '✗ 取消したのに sleeper が生きている\n'; exit 1; }
[[ "$(jobs_count)" == 0 && ! -f "$TMUX_SCHEDULE_KEYS_DIR/live.pid" ]] || { printf '✗ 取消後に job/pid が残っている\n'; exit 1; }
printf '✓ 取消 → sleeper kill + job/pid 削除\n'
grep -E '^gum confirm .*--default=false' "$CALLS" >/dev/null || { printf '✗ 取消確認が --default=false でない\n'; exit 1; }
printf '✓ 取消確認も --default=false\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper live; live_pid=$SLEEPER_PID
STUB_GUM_EXIT=1 STUB_GUM_CHOOSE_INDEX=1 run "$STUB_PATH" "$SCRIPT" list
kill -0 "$live_pid" 2>/dev/null || { printf '✗ 取消を拒否したのに sleeper が死んだ\n'; exit 1; }
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 取消拒否で job が消えた\n'; exit 1; }
printf '✓ 取消拒否 → 何もしない\n'
kill "$live_pid" 2>/dev/null || true

# 表示が一致する 2 件 (同 pane・同文字列・同じ残り時間バケツ)。2 行目を選んだら 2 件目が消えること
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
spawn_sleeper 1000-a; pid_a=$SLEEPER_PID
spawn_sleeper 2000-b; pid_b=$SLEEPER_PID
STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=2 run "$STUB_PATH" "$SCRIPT" list
/bin/sleep 0.3
[[ -f "$TMUX_SCHEDULE_KEYS_DIR/1000-a.job" && ! -f "$TMUX_SCHEDULE_KEYS_DIR/2000-b.job" ]] \
  || { printf '✗ 表示が同じ 2 件で、選んでいない方 (先頭) が取り消された\n'; ls "$TMUX_SCHEDULE_KEYS_DIR"; exit 1; }
kill -0 "$pid_a" 2>/dev/null || { printf '✗ 選んでいない方の sleeper が死んだ\n'; exit 1; }
kill -0 "$pid_b" 2>/dev/null && { printf '✗ 選んだ方の sleeper が生きている\n'; exit 1; }
printf '✓ 表示が同じ 2 件でも選んだ行 (連番) の予約だけ取り消す\n'
kill "$pid_a" 2>/dev/null || true

# pid 再利用: .pid が無関係な生きたプロセスを指す → kill せず、stale として掃く
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_spawn_fake_proc; foreign_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/reused.job"
printf '%s\n' "$foreign_pid" > "$TMUX_SCHEDULE_KEYS_DIR/reused.pid"
STUB_GUM_EXIT=0 STUB_GUM_CHOOSE_INDEX=1 run "$STUB_PATH" "$SCRIPT" list
[[ "$RC" -eq 0 ]] || { printf '✗ list (pid 再利用) が exit %s\n' "$RC"; exit 1; }
kill -0 "$foreign_pid" 2>/dev/null || { printf '✗ 無関係なプロセス (pid 再利用) を kill した\n'; exit 1; }
[[ "$(jobs_count)" == 0 ]] || { printf '✗ sleeper 不在 (pid 再利用) の job が掃かれない\n'; exit 1; }
assert_not_called "gum choose" "pid 再利用 → sleeper 不在として掃き、一覧には出さない"
printf '✓ 無関係なプロセスは kill しない\n'

reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
tt_free_pid; dead_pid=$REPLY_PID
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/stale.job"
printf '%s\n' "$dead_pid" > "$TMUX_SCHEDULE_KEYS_DIR/stale.pid"
run "$STUB_PATH" "$SCRIPT" list
[[ "$RC" -eq 0 && "$(jobs_count)" == 0 ]] || { printf '✗ pid が死んだ stale job が掃かれない\n'; exit 1; }
assert_not_called "gum choose" "stale だけ → 一覧を出さず「予約はありません」"
printf '✓ stale job (pid 死亡) を掃く\n'

# .pid 未作成 + 作成直後の job は掃かない (fire が書く前の猶予)
reset_state; mkdir -p "$TMUX_SCHEDULE_KEYS_DIR"
printf '%%5\n%s\nmake test\n' "$(( $(/bin/date +%s) + 3600 ))" > "$TMUX_SCHEDULE_KEYS_DIR/fresh.job"
run "$STUB_PATH" "$SCRIPT" list
[[ "$(jobs_count)" == 1 ]] || { printf '✗ 作成直後 (.pid 未作成) の job が掃かれた\n'; exit 1; }
printf '✓ 作成直後の .pid 未作成 job は掃かない\n'

printf '\n## 表示文字列に絵文字・曖昧幅の記号が無い (幅計算のずれでノイズになる)\n'
# 検査対象は「コードの引用文字列」(コメントは除く)。絵文字ブロック + 記号ブロック (✗ ✓ ⚠ ⏰ 等) +
# 矢印・中黒 (曖昧幅) + VS16。コメント行 (#) は説明に ⚠️ を使ってよいので落とす。
# 判定は perl -CSD (BSD/GNU grep の -P と UTF モードの差に依存しない)
WIDE_RE='[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2300}-\x{23FF}\x{2190}-\x{21FF}\x{00B7}\x{FE0F}]'
code_strings="$(grep -v '^[[:space:]]*#' "$SCRIPT" | grep -oE '"[^"]*"|'"'"'[^'"'"']*'"'"'' || true)"
[[ -n "$code_strings" ]] || { printf '✗ 引用文字列が 1 つも抽出できない (検査が空振り)\n'; exit 1; }
bad="$(printf '%s\n' "$code_strings" | perl -CSD -ne "print if /$WIDE_RE/")"
[[ -z "$bad" ]] || { printf '✗ 表示文字列に絵文字/曖昧幅の記号:\n%s\n' "$bad"; exit 1; }
printf '✓ script の引用文字列に絵文字・曖昧幅記号なし\n'
bad="$(grep -E '^bind (m|Enter|C-m) ' "$CONF" | perl -CSD -ne "print if /$WIDE_RE/")"
[[ -z "$bad" ]] || { printf '✗ popup タイトル (bind) に絵文字:\n%s\n' "$bad"; exit 1; }
printf '✓ popup タイトルに絵文字なし\n'

printf '\n## _tmux.conf: bind m / Enter / C-m が同じウィザードを指す\n'
for k in m Enter C-m; do
  grep -Eq "^bind $k +display-popup .*tmux_schedule_keys\.sh" "$CONF" || { printf '✗ bind %s が tmux_schedule_keys.sh を指していない\n' "$k"; exit 1; }
  printf '✓ bind %s → display-popup → tmux_schedule_keys.sh\n' "$k"
done
# 旧 launcher (display-menu) が復活していないこと。prefix+Enter の席はウィザードが上書きしている
! grep -Eq '^bind(-key)? +(-T prefix +)?Enter +display-menu' "$CONF" || { printf '✗ prefix+Enter に launcher (display-menu) が復活している\n'; exit 1; }
printf '✓ prefix+Enter は launcher ではない\n'

printf '\n[test-schedule-keys] all ok\n'
