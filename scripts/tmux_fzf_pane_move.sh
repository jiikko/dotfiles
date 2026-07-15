#!/usr/bin/env bash
# tmux: window を跨ぐ pane 移動を fzf で行う。_tmux.conf の `bind g` / `bind G` から
# display-popup -E 経由で呼ばれる前提 (popup 内でも $TMUX と display -p による
# 現在地解決が効くのは tmux_fzf_jump.sh で確認済み)。
#
# 使い方: tmux_fzf_pane_move.sh get|give
#   get  : 一覧から選んだ window のアクティブ pane を、現在の window へ持ってくる
#   give : 現在の pane を、一覧から選んだ window へ送る
#
# - 一覧構築 → 相対時刻 → fzf 選択の骨格は lib/tmux_fzf_window_picker.sh に集約 (jump と共通)
# - 自分自身の window は join できない (can't join its own window) ため候補から除外 (exclude-current)
# - popup 専用セッション (scratch / claude-fork / launcher) も除外 (TT_POPUP_SESSION_RE)
set -euo pipefail
unset CDPATH

_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$_dir/lib/tmux_popup_sessions.sh"       # TT_POPUP_SESSION_RE (popup 専用セッション除外)
# shellcheck source=scripts/lib/tmux_fzf_window_picker.sh
. "$_dir/lib/tmux_fzf_window_picker.sh"

mode="${1:-}"
case "$mode" in
  get)  prompt='get>  (選んだ window の pane をここへ)' ;;
  give) prompt='give> (この pane を選んだ window へ)' ;;
  *) echo "usage: $0 get|give" >&2; exit 1 ;;
esac

# popup を開いた時点の「現在地」pane。popup 内の display -p は呼び出し元 pane に解決される。
me_pane=$(tmux display -p '#{pane_id}')

# 自 window は join 不可なので除外 (exclude-current)。選ばれた window_id を得る。
target=$(tt_fzf_window_picker "$prompt " exclude-current) || exit 0

if [ "$mode" = "get" ]; then
  # 選んだ window のアクティブ pane を、自分の pane の下に合流させる
  tmux join-pane -s "$target" -t "$me_pane"
else
  # 自分の pane を選んだ window へ送る (移動先でアクティブになる)
  tmux join-pane -s "$me_pane" -t "$target"
fi
