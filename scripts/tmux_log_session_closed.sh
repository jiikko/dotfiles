#!/bin/sh
#
# session-closed フック用の観測ロガー。tmux サーバ突然 exit の真因切り分け用。
#
# 背景 (2026-06-28):
#   tmux サーバが "[server exited]"（全セッション消滅 + exit-empty による graceful shutdown。
#   crash レポートは無い）で落ちる症状の原因がまだ確定していない（popup 仮説 27ffa58 は
#   調査で反証寄り、真因は孤児サーバ + continuum Gate2 + 貧弱 last 上書きの複合と判明）。
#   次に server exit が起きたとき、
#     (a) セッションが 1 つずつ閉じて最後に exit-empty で落ちた（= 正常な連鎖の結果）のか、
#     (b) session-closed ログが無いのにサーバが消えた（= 外因: kill-server / crash / 環境）
#   のかを後追いで切り分けられるよう、session-closed の発火列を記録する。
#   observe-before-second-fix（CLAUDE.md「不具合対応の原則」/ instrument-before-second-fix）。
#
# _tmux.conf の `set-hook -g session-closed` から run-shell -b で非同期に呼ばれる。
# 出力先は Fix B/C と同じ ~/.cache/tt-restore-trigger.log。

remaining=$(tmux list-sessions 2>/dev/null | grep -c .)
{ mkdir -p "$HOME/.cache" && printf '%s\tsession-closed remaining=%s\n' \
    "$(date +%FT%T)" "$remaining" >> "$HOME/.cache/tt-restore-trigger.log"; } 2>/dev/null || true

exit 0
