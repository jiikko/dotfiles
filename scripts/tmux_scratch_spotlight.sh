#!/bin/sh
# scratch popup のスポットライト効果 (擬似立体):
# popup 表示中だけ背後の全ペイン (window-style / window-active-style) を暗転させ、
# 明るいピンク枠 popup が「浮いて」見えるようにする。tmux に drop shadow 機能は無い
# (3.7 でも無い。セル描画で半透明が存在しない) ため、figure-ground のコントラストで
# 立体感を出す代替。
#
# 使い方: tmux_scratch_spotlight.sh dim|undim
# 呼び出し元:
# - dim   : scripts/tmux_scratch_popup.sh の popup shell-command 内 (scratch 作成後・attach 前)
# - undim : (a) 同 shell-command の attach 終了後 (popup を閉じた/中で detach した時) と
#           (b) _tmux.conf の bind t detach 側 — の二重化で undim 漏れを防ぐ
#
# 設計:
# - 暗転前に現値を @spotlight_saved_* に退避し、undim で書き戻す (conf 値をハードコード
#   した二重管理をしない。conf の色を変えてもここは追従する)
# - 二重 dim ガード: 保存値が残っている間は再退避しない (「暗転値を保存してしまい
#   永久に暗いまま」事故の防止)。undim は保存値が無ければ何もしない (冪等)
# - scratch session 自体には dim 直前の値を session スコープで固定するため、
#   グローバル暗転の巻き添えにならない (popup の中身は明るいまま)。undim で unset する
# - 異常系で dim が残った場合の復帰: もう一度 C-t t で popup を開閉するか、
#   `tmux_scratch_spotlight.sh undim` を手で叩く

case "$1" in
  dim)
    saved="$(tmux show -gv @spotlight_saved_window_style 2>/dev/null)"
    if [ -z "$saved" ]; then
      cur_style="$(tmux show -gv window-style)"
      cur_active="$(tmux show -gv window-active-style)"
      tmux set -g @spotlight_saved_window_style "$cur_style"
      tmux set -g @spotlight_saved_window_active_style "$cur_active"
      # scratch の中身は元の色を session スコープで固定 (グローバル暗転の対象外にする)
      if tmux has-session -t scratch 2>/dev/null; then
        tmux set -t scratch window-style "$cur_style"
        tmux set -t scratch window-active-style "$cur_active"
      fi
    fi
    tmux set -g window-style 'fg=colour240,bg=colour233'
    tmux set -g window-active-style 'fg=colour240,bg=colour233'
    ;;
  undim)
    saved="$(tmux show -gv @spotlight_saved_window_style 2>/dev/null)"
    if [ -n "$saved" ]; then
      tmux set -g window-style "$saved"
      tmux set -g window-active-style "$(tmux show -gv @spotlight_saved_window_active_style)"
      tmux set -gu @spotlight_saved_window_style
      tmux set -gu @spotlight_saved_window_active_style
      if tmux has-session -t scratch 2>/dev/null; then
        tmux set -u -t scratch window-style
        tmux set -u -t scratch window-active-style
      fi
    fi
    ;;
  *)
    echo "usage: $0 dim|undim" >&2
    exit 1
    ;;
esac
