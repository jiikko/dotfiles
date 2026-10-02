#!/bin/sh
# Claude Code hook: tmux のペインの状態表示 (@claude_state) を「⚙ working」にする。
# PostToolUse (ツール呼び出しのたびに走る) から直接呼ぶ。UserPromptSubmit の `tmux-pane-state.sh working` もここへ exec する
# (値の正本はここ 1 箇所。他の状態と通知・ベル・bg の判定は tmux-pane-state.sh)。
#
# なぜ別の script か: tmux-pane-state.sh を PostToolUse から起こすと、homebrew の bash の起動込みで 1 回中央値 28.7ms が
# ツール呼び出しごとにターンの待ち時間へ直列で乗っていた。/bin/sh で tmux を 1 回起こすだけなら中央値 15.7ms (issue 622 で実測)。
# mod (関数 hook) にすると 12ms だったが、tool.call は失敗・中断・拒否でも発火し、mod が読まれないセッションでは
# @claude_bg が落ちずに承認待ちのベルが鳴らなくなるので採らなかった (issue 622)。
#
# 書くもの: @claude_state "⚙ working" / @claude_state_since (epoch 秒) / @claude_bg を消す (Claude が動いている = bg 待機ではない)。
# 表示側 (_tmux.conf / scripts/tmux_agent_panel.sh / tmux_agent_jump.sh / tmux-mark-seen.sh) が読む契約。
# 🚨 tmux の起動は 1 回にまとめ、bg の unset は列の最後に置く (理由は tmux-pane-state.sh の set_state)。
# stdin (hook の JSON) は読まない。tmux の外 ($TMUX_PANE が無い) では何もしない。常に成功で抜ける (ツールを止めない)。
[ -n "${TMUX_PANE:-}" ] || exit 0
command -v tmux >/dev/null 2>&1 || exit 0
tmux set -p -t "$TMUX_PANE" @claude_state "⚙ working" \; \
  set -p -t "$TMUX_PANE" @claude_state_since "$(date +%s)" \; \
  set -p -u -t "$TMUX_PANE" @claude_bg 2>/dev/null
exit 0
