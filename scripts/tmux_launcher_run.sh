#!/bin/sh
# ランチャー (bind Enter の display-menu) の実行エンジン。
# 選ばれたコマンドを専用セッション launcher 内の新規 window で起動し、専用 popup で開く。
# 引数: $1 = client_name / $2 = session_name (呼び出し元。format 展開で渡る)
#       $3 = cwd / $4 = window 名 / $5 = 実行コマンド
#
# 設計 (2026-07-04): popup 直実行 (終了で消える + scrollback 不可) → new-window 直実行
# (浮遊感がない) → scratch 相乗り (日常作業の scratch に実行結果 window が混ざる) の
# 3 段階のフィードバックを経て、scratch とは別の専用セッション launcher に分離した。
# - 見た目は popup の浮遊感のまま。枠は緑 (colour46) で scratch の青×ピンクと見分ける
# - 実体は本物のセッションなのでバッファが残り、copy-mode でスクロール・検索できる
# - C-t t で閉じても実行は続き、後から覗き直せる (閉じる判定は tmux_scratch_popup.sh 側。
#   popup 系セッション scratch/launcher 共通の「閉じるキー」として C-t t を使う)
# - 永続 popup セッションは scratch / launcher の 2 個まで。これ以上増やさない
#   (増えると popup スタック問題が再発する。docs/claude-fork-popup.md 参照)
#
# ⚠️ セッション作成は tmux_scratch_popup.sh と同じ不変条件に従う:
#   has-session ガード + `new-session -d` のみ。-A は使わない (同スクリプト参照)。

set -eu

client="$1"
session="${2:-}"
cwd="$3"
name="$4"
cmd="$5"

sess=launcher

# 実 default socket 側を操作するため TMUX/TMUX_TMPDIR を落とす
# (tmux_scratch_popup.sh の孤児サーバ予防と同方針)
unset TMUX TMUX_TMPDIR

# 並行レース対策: 2 プロセスが同時に「未存在」判定すると片方の new-session が duplicate
# session で失敗し、AND-OR リスト末尾の失敗が set -e を発火してスクリプトが無言死する
# (メニュー選択が消える)。負けた側は再確認して続行する。
# ⚠️ `new-session -Ad` (attach-or-create) に畳まないこと: 既存セッション時に attach-session
#   -d 相当となり他 client を detach する (冒頭の「-A は使わない」不変条件と同根)。
tmux has-session -t "$sess" 2>/dev/null \
  || tmux new-session -d -s "$sess" 2>/dev/null \
  || tmux has-session -t "$sess"
# コマンド終了後に $SHELL へ降りて出力を残す (new-window は終了と同時に閉じるため)
tmux new-window -t "$sess" -n "$name" -c "$cwd" "$cmd; exec ${SHELL:-zsh}"

# 呼び出し元が既に launcher popup 内なら attach 済みで新 window が前面に出るだけ
[ "$session" = "$sess" ] && exit 0

exec tmux display-popup -E -w 80% -h 75% -b heavy \
  ${client:+-c "$client"} \
  -S "fg=colour46,bg=colour22,bold" \
  -s 'bg=colour232' \
  -T "#[fg=colour231] 🚀 LAUNCHER (C-t t で閉じる) 🚀 " \
  "unset TMUX TMUX_TMPDIR; exec tmux attach -t $sess"
