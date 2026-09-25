#!/usr/bin/env bash
# pro-con の複数の画面 (同じ置き場で 2 つ開く) の終了: 最後の画面を閉じたときだけ dispatcher を止める。
#   1. B を閉じても dispatcher は残る (stop-result が無い) → A を閉じると止まる (stop-result=ok)
#   (dispatcher を kill -9 した後の形は test_e2e_multi_screen_dispatcher_killed.sh)
# 画面は置き場ごとの隔離 tmux サーバで動き、PG は偽物 (claude は起動しない)。共通部品は lib/e2e_helper.sh
set -u
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/pro-con/lib/e2e_helper.sh
. "$ROOT_DIR/tests/pro-con/lib/e2e_helper.sh"
e2e_skip_unless_tools

# --- 1. B を閉じても dispatcher は残り、A を閉じると止まる -------------------
e2e_setup
e2e_start
TT_WAIT_TICKS=200 tt_wait_until e2e_dispatcher_alive || e2e_fail "1: 画面を開いても dispatcher が起動しない"
e2e_open_second_screen
e2e_quit 1
TT_WAIT_TICKS=200 tt_wait_until e2e_windows_are 1 || e2e_fail "1: B で quit しても B が閉じない"
e2e_dispatcher_alive || e2e_fail "1: 画面 A が残っているのに dispatcher が止まった"
[ -z "$(e2e_stop_result)" ] || e2e_fail "1: 画面 A が残っているのに stop-result が書かれた"
e2e_quit 0
TT_WAIT_TICKS=600 tt_wait_until e2e_server_gone || e2e_fail "1: A で quit しても画面が閉じない"
TT_WAIT_TICKS=1800 tt_wait_until e2e_dispatcher_gone || e2e_fail "1: 最後の画面を閉じても dispatcher が止まらない"
e2e_stopped_ok || e2e_fail "1: 最後の画面を閉じたのに stop-result が ok でない"
! e2e_exited_alone || e2e_fail "1: 最後の画面の quit ではなく、画面が無い状態が続いたことで dispatcher が抜けた (quit が止めていない)"
e2e_assert_clean
echo "✓ 1: B を閉じても dispatcher は残り、最後の A を閉じると止まる (stop-result=ok)"
