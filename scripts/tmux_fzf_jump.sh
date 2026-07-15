#!/usr/bin/env bash
# tmux: 全セッションのウィンドウを fzf で曖昧検索して選び、そこへジャンプする。
# _tmux.conf の `bind f` から display-popup -E 経由で呼ばれる前提
# (popup 内でも $TMUX が引き継がれ、switch-client / display -p による現在地解決が
# 効くことを確認済み)。
# - 最終アクティビティの新しい順に並べる (現在地が先頭、直前に居た場所が 2 番目)
# - 現在地に「← いまここ」マーク、各行に相対時刻 (◯分前) を表示
# - popup 専用セッション (scratch / claude-fork / launcher) は候補から除外する
# 一覧構築 → 相対時刻 → fzf 選択の骨格は lib/tmux_fzf_window_picker.sh に集約 (pane_move と共通)。
set -euo pipefail
unset CDPATH

_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$_dir/lib/tmux_popup_sessions.sh"       # TT_POPUP_SESSION_RE (popup 専用セッション除外)
# shellcheck source=scripts/lib/tmux_fzf_window_picker.sh
. "$_dir/lib/tmux_fzf_window_picker.sh"

# 現在地も候補に含め「← いまここ」マークを付ける (第 2 引数なし)。
target=$(tt_fzf_window_picker 'jump> ') || exit 0
tmux switch-client -t "$target"
