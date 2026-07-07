# shellcheck shell=sh
# popup 専用セッション (fzf の候補一覧に混ぜないもの) の除外パターン。
# "#{session_name}:#{window_index}" 形式のフィールド先頭にマッチする ERE として使う。
# 利用者: scripts/tmux_fzf_jump.sh / scripts/tmux_fzf_pane_move.sh (source して awk -v re= に渡す)。
# かつて両スクリプトに別々の除外リストが直書きされ、launcher 新設時に両方とも追従漏れした
# (jump は claude-fork のみ・pane_move は scratch のみで食い違い)。ここに一本化した (2026-07-08)。
# 名前の出典 (セッションが増減したらここだけ直せば両方に効く):
#   scratch     = scripts/tmux_scratch_popup.sh
#   launcher    = scripts/tmux_launcher_run.sh
#   claude-fork = scripts/tmux_fork_popup.sh (/fork-scratch が作成)
# shellcheck disable=SC2034  # source 先の awk -v で参照される
TT_POPUP_SESSION_RE='^(scratch|claude-fork|launcher):'
