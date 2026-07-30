#!/usr/bin/env bash
#
# session-closed フック用の観測ロガー。tmux サーバ突然 exit の真因切り分け用。
#
# 背景 (2026-06-28 起点、2026-07-30 強化):
#   tmux サーバが "[server exited]" で落ちる症状の切り分けに、session-closed の発火列を
#   記録する。2026-07-30 の隔離 3.7b での実測で判明した重要事実:
#     - run-shell -b (非同期) だと「最後のセッション close (exit-empty)」と「kill-server」
#       ではサーバがジョブごと SIGTERM で刈るため一切記録できない (= 肝心の死亡直前だけが
#       観測不能になる)。同期 run-shell なら両ケースとも確実に書ける。
#       このため _tmux.conf の hook は同期 (-b なし) で本スクリプトを呼ぶ。
#     - 同期 hook 内からのネスト tmux コマンド (list-sessions) はデッドロックしない (実測)。
#   記録は死因分類の一次証拠: remaining=0 が直近にあれば exit-empty (連鎖 close)、
#   無ければ外因 (kill-server / シグナル)。分類の実装は scripts/tmux_server_watchdog.sh。
#   observe-before-second-fix（CLAUDE.md「不変条件の観測」/ instrument-before-second-fix）。
#
# default socket ゲート: conf は -L の隔離テストサーバでも source されるため、gate 無しだと
#   テストのセッション close が共有ログに混ざり、watchdog の死因分類 (remaining=0 の直近性)
#   を汚す。default socket のサーバ以外では無音で何もしない。
#
# 無音契約: 縮退時 (依存不在・書き込み失敗) は無音で exit 0 (scripts/CLAUDE.md 参照)。
#   同期 hook になったため実行時間にも注意 — list-sessions + printf のみで完結させる。
set -uo pipefail
unset CDPATH

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck source=scripts/lib/tmux_resurrect_guards.sh
. "$SCRIPT_DIR/lib/tmux_resurrect_guards.sh"

tt_on_default_server || exit 0

remaining=$(tmux list-sessions 2>/dev/null | grep -c .)
{ mkdir -p "$HOME/.cache" && printf '%s\tsession-closed remaining=%s epoch=%s\n' \
    "$(date +%FT%T)" "$remaining" "$(date +%s)" >> "$HOME/.cache/tt-restore-trigger.log"; } 2>/dev/null || true

exit 0
