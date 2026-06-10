#!/usr/bin/env bash
# Claude Code hook: tmux ペイン境界 (@claude_state) に作業状態を反映する。
#
# 使い方: tmux-pane-state.sh working|input|idle|start|clear
#   working : "⚙ working" を表示 (UserPromptSubmit / PostToolUse=承認後の自動復帰)
#   input   : "⏳ input" を表示   (Notification — permission 承認待ち・質問への回答待ち)
#             + ペインが画面に見えていなければ macOS 通知 (音あり)
#   idle    : "✓ idle" を表示     (Stop = 応答完了)
#             + ペインが画面に見えていなければ macOS 通知 (音なし)
#   start   : "✓ idle" を表示     (SessionStart — 起動直後なので通知しない)
#   clear   : 状態を消す          (SessionEnd — Claude 終了後は通常シェルへ戻す)
#
# 状態は pane 単位のユーザーオプション @claude_state に書き込む。未設定なら
# pane-border-format 側の #{?@claude_state,...,} が空に展開されるため、
# Claude を起動していないペイン (通常シェル等) には一切影響しない。
# tmux 外で起動された場合 ($TMUX_PANE 未設定) は何もしない。
#
# 通知条件 = 「画面で状態表示が見えていないとき」だけ:
# - そのペインのウィンドウがどのクライアントでも前面でない (window_active_clients=0)
# NOTE: かつては「ターミナル自体が非アクティブ (@term_unfocused)」も条件だったが、
# 供給元の client-focus-in/out フックを誤判定のため削除した (_tmux.conf 側の
# NOTE 参照、2026-06-10) のに伴い本条件も撤去。ターミナルが背面でも claude
# ウィンドウが tmux 内で前面なら通知は出ない (許容)
set -uo pipefail
unset CDPATH

[ -n "${TMUX_PANE:-}" ] || exit 0
command -v tmux >/dev/null 2>&1 || exit 0

set_state() { tmux set -p -t "$TMUX_PANE" @claude_state "$1" 2>/dev/null; }

notify_if_hidden() {
  local message="$1" sound="${2:-}"
  command -v terminal-notifier >/dev/null 2>&1 || return 0
  local visible target
  visible=$(tmux display -p -t "$TMUX_PANE" '#{window_active_clients}' 2>/dev/null) || return 0
  if [ "${visible:-0}" = "0" ]; then
    target=$(tmux display -p -t "$TMUX_PANE" '#{session_name}:#{window_index}' 2>/dev/null)
    # -group で同一ペインの通知を上書き (通知センターに溜めない)。
    # バックグラウンド起動で hook の応答を遅らせない
    terminal-notifier -title "Claude Code (${target:-tmux})" -message "$message" \
      -group "tmux-claude-$TMUX_PANE" ${sound:+-sound "$sound"} >/dev/null 2>&1 &
  fi
}

case "${1:-}" in
  working) set_state "⚙ working" ;;
  input)   set_state "⏳ input"; notify_if_hidden "⏳ 入力待ち (承認 or 回答が必要)" "default" ;;
  idle)    set_state "✓ idle";  notify_if_hidden "✓ 応答完了" ;;
  start)   set_state "✓ idle" ;;
  clear)   tmux set -p -u -t "$TMUX_PANE" @claude_state 2>/dev/null ;;
esac

# hook が状態を返さないよう常に成功で抜ける (Stop/UserPromptSubmit を block しない)
exit 0
