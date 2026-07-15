#!/usr/bin/env bash
# tmux: ペイン kill の確認 popup ヘルパー。_tmux.conf の bind x (現ペイン) / bind q (他全ペイン) から
# display-popup -E 経由で呼ばれる。誤爆防止のため gum confirm を挟む
# (実例: C-t q の誤爆で実行中の claude セッションごと他ペインが全 kill された 2026-06-10)。
#
#   tmux_kill_confirm.sh pane    : 現在のペインを kill (メッセージに pane_current_command を出す)
#   tmux_kill_confirm.sh others  : 現在以外の全ペインを kill (kill-pane -a)
#
# ⚠️ set -e は使わない: fail-safe は `gum confirm && tmux kill-pane` の && 短絡に依存しており
#    (gum 未導入なら exit 127 で kill されない。zshrc 起動時に brew install gum を催促)、
#    -e を足すと gum の非0終了で kill 前に script が落ちる挙動差が出るため素の && 連鎖を保つ。
# ⚠️ popup 内では #{...} フォーマットが展開されない (tmux 3.6a 実測。TMUX_PANE も無い) ため、
#    対象 pane は popup 内シェルの `tmux display-message -p` で解決する。冒頭で $p に固定してから
#    confirm するので「確認した相手」と「kill する相手」が一致する (popup 直下のアクティブ pane)。
set -uo pipefail

scope="${1:-}"
p="$(tmux display-message -p '#{pane_id}')"
case "$scope" in
  pane)
    c="$(tmux display-message -p -t "$p" '#{pane_current_command}')"
    gum confirm --default=false --affirmative "kill する" --negative "やめる" "このペイン ($c) を kill する？" \
      && tmux kill-pane -t "$p"
    ;;
  others)
    gum confirm --default=false --affirmative "kill する" --negative "やめる" "他の全ペインを kill する？" \
      && tmux kill-pane -a -t "$p"
    ;;
  *) echo "usage: $0 pane|others" >&2; exit 1 ;;
esac
