#!/usr/bin/env bash
# tmux hook: window を選択 (アクティブ化) したとき、その window 配下の Claude Code
# "input" (🔔 入力待ち) を "seen" (🔕 既読) に降格する。
#
# 使い方: tmux-mark-seen.sh [window_id]   (after-select-window フックから呼ぶ)
#   引数があればその window、無ければ現在アクティブな window を対象にする。
#
# working (⚙ 作業中) / idle (✓ 完了) は触らない。input だけを既読化することで、
# 「通知に気づいて window を見たが、まだ応答していない」状態を 🔕 で表す。
# 応答すると UserPromptSubmit フック (tmux-pane-state.sh working) が working に
# 上書きするため、seen は「見た〜応答する」までの一時状態として機能する。
#
# 状態の正本は pane 単位のユーザーオプション @claude_state (tmux-pane-state.sh が
# 書き込む)。本スクリプトはその値を input -> seen に書き換えるだけで、表示は
# _tmux.conf の window-status-format / pane-border-format 側が解釈する。
#
# 競合対策: 「現在値を読む」→「seen を書く」を shell 側で 2 段に分けると、その隙間に
# Claude hook が working/idle に更新したものを seen で上書きしうる (codex 指摘)。
# tmux if-shell -F に「@claude_state が厳密に '🔔 input' のときだけ set する」を
# 閉じ込め、読み取りと条件付き書き込みを 1 コマンドにして隙間を無くす。
set -uo pipefail
unset CDPATH

command -v tmux >/dev/null 2>&1 || exit 0

win="${1:-}"
# run-shell の format 展開が効かず "#{window_id}" がそのまま渡るケースや未指定では、
# 現在アクティブな window (= after-select-window 発火直後は選択した window) に倒す。
case "$win" in
  ""|'#{'*) win=$(tmux display -p '#{window_id}' 2>/dev/null) || exit 0 ;;
esac

while IFS= read -r pid; do
  [ -n "$pid" ] || continue
  tmux if-shell -F -t "$pid" '#{==:#{@claude_state},🔔 input}' \
    "set -p -t '$pid' @claude_state '🔕 seen'" 2>/dev/null
done < <(tmux list-panes -t "$win" -F '#{pane_id}' 2>/dev/null)

exit 0
