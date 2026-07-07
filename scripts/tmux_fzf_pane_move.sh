#!/usr/bin/env bash
# tmux: window を跨ぐ pane 移動を fzf で行う。_tmux.conf の `bind g` / `bind G` から
# display-popup -E 経由で呼ばれる前提 (popup 内でも $TMUX と display -p による
# 現在地解決が効くのは tmux_fzf_jump.sh で確認済み)。
#
# 使い方: tmux_fzf_pane_move.sh get|give
#   get  : 一覧から選んだ window のアクティブ pane を、現在の window へ持ってくる
#   give : 現在の pane を、一覧から選んだ window へ送る
#
# - 一覧は tmux_fzf_jump.sh と同じ見た目 (アクティビティ順 + 相対時刻 + プレビュー)
# - 自分自身の window は join できない (can't join its own window) ため候補から除外
# - popup 専用セッション (scratch / claude-fork / launcher) も除外
#   (パターンは lib/tmux_popup_sessions.sh に一本化。jump と共通)
set -euo pipefail
unset CDPATH

# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/tmux_popup_sessions.sh"

mode="${1:-}"
case "$mode" in
  get)  prompt='get>  (選んだ window の pane をここへ)' ;;
  give) prompt='give> (この pane を選んだ window へ)' ;;
  *) echo "usage: $0 get|give" >&2; exit 1 ;;
esac

# popup を開いた時点の「現在地」。popup 内の display -p は呼び出し元 pane に解決される
me_pane=$(tmux display -p '#{pane_id}')
current=$(tmux display -p '#{session_name}:#{window_index}')
now=$(date +%s)

# 候補は「window_id<TAB>整形済み表示」の 2 フィールドで fzf に渡す (方式の理由は
# tmux_fzf_jump.sh の同箇所コメント参照。空白入りセッション名対策)。
rows=$(tmux list-windows -a \
  -F "#{window_activity}	#{window_id}	#{session_name}:#{window_index}	#{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}" \
  | awk -F'\t' -v re="$TT_POPUP_SESSION_RE" -v cur="$current" '$3 !~ re && $3 != cur' \
  | sort -t$'\t' -k1,1rn \
  | awk -F'\t' -v now="$now" '{
      d = now - $1
      if      (d < 60)    rel = d "秒前"
      else if (d < 3600)  rel = int(d/60) "分前"
      else if (d < 86400) rel = int(d/3600) "時間前"
      else                rel = int(d/86400) "日前"
      printf "%s\t%s\t\033[33m%s\033[0m\t%s\n", $2, $3, rel, $4
    }')
[ -n "$rows" ] || exit 0
list=$(paste -d'\t' \
  <(printf '%s\n' "$rows" | cut -f1) \
  <(printf '%s\n' "$rows" | cut -f2- | column -ts$'\t'))

selected=$(printf '%s\n' "$list" \
  | fzf --ansi --reverse --border --prompt="$prompt " \
        --delimiter=$'\t' --with-nth=2 \
        --preview 'tmux capture-pane -ep -t {1} | tail -40' \
        --preview-window=down,60%) || exit 0
target="$(printf '%s\n' "$selected" | cut -f1)"

if [ "$mode" = "get" ]; then
  # 選んだ window のアクティブ pane を、自分の pane の下に合流させる
  tmux join-pane -s "$target" -t "$me_pane"
else
  # 自分の pane を選んだ window へ送る (移動先でアクティブになる)
  tmux join-pane -s "$me_pane" -t "$target"
fi
