#!/usr/bin/env bash
# Claude Code hook: tmux ペイン境界 (@claude_state) に作業状態を反映する。
#
# 使い方: tmux-pane-state.sh working|idle|clear
#   working : "⚙ working" を表示 (UserPromptSubmit)
#   idle    : "✓ idle" を表示     (Stop / SessionStart)
#   clear   : 状態を消す          (SessionEnd — Claude 終了後は通常シェルへ戻す)
#
# pane 単位のユーザーオプション @claude_state に書き込む。未設定なら
# pane-border-format 側の #{?@claude_state,...,} が空に展開されるため、
# Claude を起動していないペイン (通常シェル等) には一切影響しない。
# tmux 外で起動された場合 ($TMUX_PANE 未設定) は何もしない。
set -uo pipefail
unset CDPATH

[ -n "${TMUX_PANE:-}" ] || exit 0
command -v tmux >/dev/null 2>&1 || exit 0

case "${1:-}" in
  working) tmux set -p -t "$TMUX_PANE" @claude_state "⚙ working" 2>/dev/null ;;
  idle)    tmux set -p -t "$TMUX_PANE" @claude_state "✓ idle" 2>/dev/null ;;
  clear)   tmux set -p -u -t "$TMUX_PANE" @claude_state 2>/dev/null ;;
esac

# hook が状態を返さないよう常に成功で抜ける (Stop/UserPromptSubmit を block しない)
exit 0
