#!/usr/bin/env bash
# tmux: 全セッションのウィンドウを fzf で曖昧検索して選び、そこへジャンプする。
# _tmux.conf の `bind f` から display-popup -E 経由で呼ばれる前提
# (popup 内でも $TMUX が引き継がれ、switch-client / display -p による現在地解決が
# 効くことを確認済み)。
# - 最終アクティビティの新しい順に並べる (現在地が先頭、直前に居た場所が 2 番目)
# - 現在地に「← いまここ」マーク、各行に相対時刻 (◯分前) を表示
# - popup 専用セッション (scratch / claude-fork / launcher) は候補から除外する
#   (パターンは lib/tmux_popup_sessions.sh に一本化。pane_move と共通)
set -euo pipefail
unset CDPATH

# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmux_popup_sessions.sh"

current=$(tmux display -p '#{session_name}:#{window_index}')
now=$(date +%s)

# 候補は「window_id<TAB>整形済み表示」の 2 フィールドで fzf に渡す。target の切り出しと
# プレビューは window_id (空白を含まない安定キー) で行い、セッション名に空白があっても
# 壊れない (旧実装は column 整形後の行を ${selected%% *} と fzf の空白区切り {1} で
# 切り出しており、空白入りセッション名で target が千切れて誤ジャンプ / プレビュー失敗した)。
rows=$(tmux list-windows -a \
  -F "#{window_activity}	#{window_id}	#{session_name}:#{window_index}	#{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}" \
  | awk -F'\t' -v re="$TT_POPUP_SESSION_RE" '$3 !~ re' \
  | sort -t$'\t' -k1,1rn \
  | awk -F'\t' -v now="$now" -v cur="$current" '{
      d = now - $1
      if      (d < 60)    rel = d "秒前"
      else if (d < 3600)  rel = int(d/60) "分前"
      else if (d < 86400) rel = int(d/3600) "時間前"
      else                rel = int(d/86400) "日前"
      mark = ($3 == cur) ? "\033[1;36m← いまここ\033[0m" : ""
      printf "%s\t%s\t\033[33m%s\033[0m\t%s\t%s\n", $2, $3, rel, $4, mark
    }')
[ -n "$rows" ] || exit 0
list=$(paste -d'\t' \
  <(printf '%s\n' "$rows" | cut -f1) \
  <(printf '%s\n' "$rows" | cut -f2- | column -ts$'\t'))

selected=$(printf '%s\n' "$list" \
  | fzf --ansi --reverse --border --prompt='jump> ' \
        --delimiter=$'\t' --with-nth=2 \
        --preview 'tmux capture-pane -ep -t {1} | tail -40' \
        --preview-window=down,60%) || exit 0

tmux switch-client -t "$(printf '%s\n' "$selected" | cut -f1)"
