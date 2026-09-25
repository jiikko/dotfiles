#!/usr/bin/env bash
# pro-con の画面の中継 (issue 443): e2e モードの画面が描くたびに置く最新の 1 枚を、pro-con screen が外から読める。
# 2 つ目の画面 (見ているだけの --view) も中継し、画面を閉じたら中継も消える。共通部品は lib/e2e_helper.sh
set -u
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tests/pro-con/lib/e2e_helper.sh
. "$ROOT_DIR/tests/pro-con/lib/e2e_helper.sh"
e2e_skip_unless_tools

e2e_setup
e2e_start
screen_out() { "$PC" screen --e2e "$root" "$@" 2>&1; }
relay_shows() { screen_out | grep -qF -- "$1"; }
TT_WAIT_TICKS=200 tt_wait_until relay_shows producer-consumer || e2e_fail "pro-con screen が画面を読めない: $(screen_out)"
screen_out | grep -q $'\e\[' && e2e_fail "既定で色 (エスケープ) が残っている"
screen_out --json | jq -e '.state.mode == "board" and .width == 200' > /dev/null || e2e_fail "--json の画面の状態・幅が違う: $(screen_out --json | head -c 300)"
echo "✓ pro-con screen が e2e の画面を読める (色を落とした文字 / --json の状態)"

# 2 つ目の画面 (見ているだけ) も中継する → 一覧に 2 つ並ぶ
tmx new-window -d -t 0: "$PC --view --e2e '$root'"
two_screens() { screen_out | grep -q "画面が 2 個ある"; }
TT_WAIT_TICKS=200 tt_wait_until two_screens || e2e_fail "2 つ目の画面が中継に並ばない: $(screen_out)"
screen_out | grep -q "見ているだけ (--view)" || e2e_fail "見ているだけの画面の印が一覧に無い: $(screen_out)"
echo "✓ 見ているだけの画面も中継する (一覧に 2 つ並ぶ)"

# 2 つ目を閉じたら、中継は 1 つに戻る (閉じた画面の中継のファイルは消える)
e2e_quit 1
one_screen() { screen_out | grep -qF producer-consumer && ! screen_out | grep -q "画面が 2 個ある"; }
TT_WAIT_TICKS=200 tt_wait_until one_screen || e2e_fail "閉じた画面が中継に残る: $(screen_out --all)"
e2e_quit 0
TT_WAIT_TICKS=600 tt_wait_until e2e_server_gone || e2e_fail "A で quit しても画面が閉じない"
TT_WAIT_TICKS=600 tt_wait_until e2e_dispatcher_gone || e2e_fail "最後の画面を閉じても dispatcher が止まらない"
[ -d "$state/relay" ] || e2e_fail "前提: 中継の置き場 $state/relay が無い (残りの確認が空振りする)"
leftover="$(find "$state/relay" -type f | head -3)"
[ -z "$leftover" ] || e2e_fail "閉じた後も中継のファイルが残る: $leftover"
e2e_assert_clean
echo "✓ 画面を閉じたら中継も消える"
