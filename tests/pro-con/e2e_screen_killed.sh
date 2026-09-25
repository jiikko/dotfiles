#!/usr/bin/env bash
# pro-con の画面が kill -9 で落ちたとき: 偽の PG が動いている状態で画面のプロセスを kill -9 すると、
# 画面が居なくなったことに dispatcher が気づいて (約 60 秒) 偽の PG を止めて抜ける (stop-result=ok)。
# 1 分以上かかるので make test の自動収集 (test_*.sh) からは外している。
set -u
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/pro-con/lib/e2e_helper.sh
. "$ROOT_DIR/tests/pro-con/lib/e2e_helper.sh"
e2e_skip_unless_tools

e2e_setup
e2e_start
TT_WAIT_TICKS=200 tt_wait_until e2e_dispatcher_alive || e2e_fail "画面を開いても dispatcher が起動しない"
# 依頼を出す → 偽の PM が分解 → 偽の PG が起動して質問を置く
tmx send-keys -t 0:0 n
tmx send-keys -t 0:0 -l "画面が落ちたときの停止の確認"
tmx send-keys -t 0:0 Enter
TT_WAIT_TICKS=300 tt_wait_until e2e_screen_has 0 "質問待ち (1)" || e2e_fail "偽の PG が起動しない (質問待ちに来ない)"
pg_running() { jq -e '[.sessions[] | select(.state != null and .state != "stopped")] | length > 0' "$state/e2e-sessions.json" > /dev/null 2>&1; }
pg_running || e2e_fail "偽の PG の session が e2e-sessions.json に無い"

# 画面のプロセスを kill -9 (pane のプロセスが、この置き場の画面だと確かめてから)
screen_pid="$(tmx display-message -p -t 0:0 '#{pane_pid}')"
ps -p "$screen_pid" -o command= | grep -F -- "--e2e $root" | grep -qvF dispatcher \
  || e2e_fail "pane のプロセスがこの置き場の画面でない: $(ps -p "$screen_pid" -o command=)"
kill -9 "$screen_pid"

# 画面が居なくなったことに dispatcher が気づくまで約 60 秒。上限は 3 倍の 180 秒
TT_WAIT_TICKS=1800 tt_wait_until e2e_dispatcher_gone || e2e_fail "画面を kill -9 して 180 秒待っても dispatcher が抜けない"
e2e_stopped_ok || e2e_fail "dispatcher は抜けたが stop-result が ok でない"
e2e_exited_alone || e2e_fail "dispatcher は抜けたが、画面が無い状態が続いたことによる終了ではない (dispatcher.log に「$E2E_ALONE_EXIT」が無い)"
pg_stopped() { jq -e '[.sessions[] | select(.state != "stopped")] | length == 0' "$state/e2e-sessions.json" > /dev/null; }
pg_stopped || { jq -c '.sessions' "$state/e2e-sessions.json"; e2e_fail "偽の PG の state が stopped になっていない"; }
e2e_assert_clean
echo "✓ 画面を kill -9 すると、dispatcher が偽の PG を止めて抜ける (state=stopped / stop-result=ok)"
