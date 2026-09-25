#!/usr/bin/env bash
# pro-con の複数の画面 (同じ置き場で 2 つ開く) の終了: 最後の画面を閉じたときだけ dispatcher を止める。
#   dispatcher を kill -9 してから B を閉じても止めない → A を閉じると止める
#   (画面の数は presence で数え、dispatcher にも socket にも頼らない。dispatcher が居ない形だけを分けて確かめる)
#   (dispatcher が居る形は test_e2e_multi_screen.sh)
# 画面は置き場ごとの隔離 tmux サーバで動き、PG は偽物 (claude は起動しない)。共通部品は lib/e2e_helper.sh
set -u
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/pro-con/lib/e2e_helper.sh
. "$ROOT_DIR/tests/pro-con/lib/e2e_helper.sh"
e2e_skip_unless_tools

# --- 2. dispatcher を kill -9 した後でも、止めるのは最後の画面だけ -----------
e2e_setup
e2e_start
TT_WAIT_TICKS=200 tt_wait_until e2e_dispatcher_alive || e2e_fail "2: 画面を開いても dispatcher が起動しない"
e2e_open_second_screen
pid="$(e2e_dispatcher_pid)"
[ -n "$pid" ] || e2e_fail "2: この置き場の dispatcher の pid を取れない"
kill -9 "$pid"
dead_pid() { ! kill -0 "$1" 2>/dev/null || ps -p "$1" -o stat= | grep -q Z; }
TT_WAIT_TICKS=100 tt_wait_until dead_pid "$pid" || e2e_fail "2: kill -9 した dispatcher が消えない"
e2e_quit 1
TT_WAIT_TICKS=200 tt_wait_until e2e_windows_are 1 || e2e_fail "2: B で quit しても B が閉じない"
[ -z "$(e2e_stop_result)" ] || e2e_fail "2: 画面 A が残っているのに B の quit が dispatcher を止めた (stop-result=$(e2e_stop_result))"
e2e_quit 0
TT_WAIT_TICKS=600 tt_wait_until e2e_server_gone || e2e_fail "2: A で quit しても画面が閉じない"
TT_WAIT_TICKS=1800 tt_wait_until e2e_dispatcher_gone || e2e_fail "2: 最後の画面を閉じても dispatcher が止まらない"
e2e_stopped_ok || e2e_fail "2: 最後の画面を閉じたのに stop-result が ok でない"
! e2e_exited_alone || e2e_fail "2: 最後の画面の quit ではなく、画面が無い状態が続いたことで dispatcher が抜けた (quit が止めていない)"
e2e_assert_clean
echo "✓ 2: dispatcher を kill -9 した後でも、B の quit は止めず、最後の A の quit が止める"
