#!/bin/sh
# ランチャー (bind Enter の display-menu) の実行エンジン。
# 選ばれたコマンドを scratch セッション内の新規 window で起動し、scratch popup で開く。
# 引数: $1 = client_name / $2 = session_name (呼び出し元。format 展開で渡る)
#       $3 = cwd / $4 = window 名 / $5 = 実行コマンド
#
# 設計 (2026-07-04): popup 直実行は「終了で消える + scrollback 不可」、new-window 直実行は
# 「浮遊感がなくいまいち」という 2 段階のフィードバックを経てこの形に落ちた。
# scratch セッションに相乗りすることで:
# - 見た目は scratch popup (迫力・浮遊感)
# - 実体は本物のセッションなのでバッファが残り、copy-mode でスクロール・検索できる
# - C-t t で閉じても実行は続き、後から覗き直せる
# - 永続 popup セッションは scratch 1 個のまま (収拾がつかなくなる問題を避ける。
#   docs/claude-fork-popup.md の popup スタック問題も参照)
#
# ⚠️ scratch セッションの作成は tmux_scratch_popup.sh と同じ不変条件に従う:
#   has-session ガード + `new-session -d` のみ。-A は使わない (同スクリプト参照)。

set -eu

client="$1"
session="${2:-}"
cwd="$3"
name="$4"
cmd="$5"

# scratch 側 (default socket) を操作するため TMUX/TMUX_TMPDIR を落とす
# (tmux_scratch_popup.sh の孤児サーバ予防と同方針)
unset TMUX TMUX_TMPDIR

tmux has-session -t scratch 2>/dev/null || tmux new-session -d -s scratch
# コマンド終了後に $SHELL へ降りて出力を残す (new-window は終了と同時に閉じるため)
tmux new-window -t scratch -n "$name" -c "$cwd" "$cmd; exec ${SHELL:-zsh}"

# 呼び出し元が既に scratch popup 内なら attach 済みで新 window が前面に出るだけ。
# それ以外なら scratch popup を開く (開閉トグルは既存スクリプトに委譲)
script_dir=$(cd "$(dirname "$0")" && pwd)
if [ "$session" != "scratch" ]; then
  exec "$script_dir/tmux_scratch_popup.sh" "$client" "$session"
fi
