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
# - scratch (スクラッチ popup 用セッション) も除外
set -euo pipefail
unset CDPATH

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

list=$(tmux list-windows -a \
  -F "#{window_activity}	#{session_name}:#{window_index}	#{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}" \
  | awk -F'\t' -v cur="$current" '$2 !~ /^scratch:/ && $2 != cur' \
  | sort -t$'\t' -k1,1rn \
  | awk -F'\t' -v now="$now" '{
      d = now - $1
      if      (d < 60)    rel = d "秒前"
      else if (d < 3600)  rel = int(d/60) "分前"
      else if (d < 86400) rel = int(d/3600) "時間前"
      else                rel = int(d/86400) "日前"
      printf "%s\t\033[33m%s\033[0m\t%s\n", $2, rel, $3
    }' \
  | column -ts$'\t')
[ -n "$list" ] || exit 0

selected=$(printf '%s\n' "$list" \
  | fzf --ansi --reverse --border --prompt="$prompt " \
        --preview 'tmux capture-pane -ep -t {1} | tail -40' \
        --preview-window=down,60%) || exit 0
target="${selected%% *}"

if [ "$mode" = "get" ]; then
  # 選んだ window のアクティブ pane を、自分の pane の下に合流させる
  tmux join-pane -s "$target" -t "$me_pane"
else
  # 自分の pane を選んだ window へ送る (移動先でアクティブになる)
  tmux join-pane -s "$me_pane" -t "$target"
fi
