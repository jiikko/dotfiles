#!/usr/bin/env bash
# tmux: 全セッションのウィンドウを fzf で曖昧検索して選び、そこへジャンプする。
# _tmux.conf の `bind f` から display-popup -E 経由で呼ばれる前提
# (popup 内でも $TMUX が引き継がれ、switch-client / display -p による現在地解決が
# 効くことを確認済み)。
# - 最終アクティビティの新しい順に並べる (現在地が先頭、直前に居た場所が 2 番目)
# - 現在地に「← いまここ」マーク、各行に相対時刻 (◯分前) を表示
# - scratch (スクラッチ popup 用の専用セッション) は候補から除外する
set -euo pipefail
unset CDPATH

current=$(tmux display -p '#{session_name}:#{window_index}')
now=$(date +%s)

list=$(tmux list-windows -a \
  -F "#{window_activity}	#{session_name}:#{window_index}	#{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}" \
  | awk -F'\t' '$2 !~ /^scratch:/' \
  | sort -t$'\t' -k1,1rn \
  | awk -F'\t' -v now="$now" -v cur="$current" '{
      d = now - $1
      if      (d < 60)    rel = d "秒前"
      else if (d < 3600)  rel = int(d/60) "分前"
      else if (d < 86400) rel = int(d/3600) "時間前"
      else                rel = int(d/86400) "日前"
      mark = ($2 == cur) ? "\033[1;36m← いまここ\033[0m" : ""
      printf "%s\t\033[33m%s\033[0m\t%s\t%s\n", $2, rel, $3, mark
    }' \
  | column -ts$'\t')
[ -n "$list" ] || exit 0

selected=$(printf '%s\n' "$list" \
  | fzf --ansi --reverse --border --prompt='jump> ' \
        --preview 'tmux capture-pane -ep -t {1} | tail -40' \
        --preview-window=down,60%) || exit 0

tmux switch-client -t "${selected%% *}"
