#!/usr/bin/env bash
# tmux: 全セッションのウィンドウを fzf で曖昧検索して選び、そこへジャンプする。
# _tmux.conf の `bind f` から display-popup -E 経由で呼ばれる前提
# (popup 内でも $TMUX が引き継がれ、switch-client がそのまま効くことを確認済み)。
# scratch (スクラッチ popup 用の専用セッション) は候補から除外する。
set -euo pipefail
unset CDPATH

list=$(tmux list-windows -a \
  -F '#{session_name}:#{window_index} #{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}' \
  | grep -v '^scratch:' || true)
[ -n "$list" ] || exit 0

selected=$(printf '%s\n' "$list" \
  | fzf --reverse --border --prompt='jump> ' \
        --preview 'tmux capture-pane -ep -t {1} | tail -40' \
        --preview-window=down,60%) || exit 0

tmux switch-client -t "${selected%% *}"
