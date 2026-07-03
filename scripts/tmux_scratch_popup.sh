#!/bin/sh
# scratch popup のトグル (bind t / C-t から `run-shell '... "#{client_name}" "#{session_name}"'`
# で呼ばれる)。開閉判定ごとここに集約する (旧実装は if-shell -F 込みの 200 文字超の bind
# 文字列が t / C-t に複製されており、二重エスケープで壊れやすかった)。
# 引数: $1 = client_name / $2 = session_name (いずれも run-shell の format 展開で渡る)
#
# - session_name == scratch (= popup 内で押された) → detach で popup を閉じ、全クライアント
#   を再描画。refresh は旧 bind の run-shell -b と同じくバックグラウンドで走らせ、
#   tmux サーバをブロックしない
# - それ以外 → scratch popup を開く
#
# ここに集約した演出:
# - 2 色枠: 罫線 fg=colour33 (電子ブルー) × 地 bg=colour201 (ショッキングピンク) の固定。
#   (開くたびに色が変わる「枠色ガチャ」を一度実装したが、毎回同じ青×ピンクという視覚
#   アイデンティティを優先してユーザー判断で固定に戻した 2026-07-02)。中身は濃紺 (-s bg=colour17)。
# - 動的タイトル: 開いた時刻 (#{t/f/%H#:%M:client_activity} = C-t t の keypress 時刻) を
#   スナップショット表示 (生時計にはならない。per-draw のタイトル再展開は 3.5a に無い)。
#   ⚠️ bare の %H:%M は -T では strftime 展開されない (実測)。t/f 修飾子経由が必須。
# - status 2 行化: scratch セッションだけ status 2 にして演出 2 行目 (status-format[1],
#   _tmux.conf 側で定義) を出す。通常セッションは 1 行のまま。
#
# ⚠️ 以下の不変条件は _tmux.conf の bind t コメント (孤児サーバ予防の経緯) 由来。壊さないこと:
# - `unset TMUX TMUX_TMPDIR`: nested attach ガード越え + 呼び出し元が継承 TMUX_TMPDIR を
#   持つ環境 (テストサーバ等) でも scratch を必ず実 default socket 側に作り孤児を生まない
# - has-session ガードで「無ければだけ新規作成」。既存 scratch に `new-session -d -A` を
#   打ってはいけない (popup 内の最初の C-t t が 1 回効かず「閉じるのに 2 回押す」回帰。実測)
# - status 2 は attach 前に設定 (点滅/スピナーの駆動は global の status-interval=1)

client="$1"
session="${2:-}"

if [ "$session" = "scratch" ]; then
  # shellcheck disable=SC2086 # ${client:+...} は client 空のとき引数ごと消す意図の word splitting
  tmux detach-client ${client:+-t "$client"}
  script_dir=$(cd "$(dirname "$0")" && pwd)
  "$script_dir/tmux_refresh_all_clients.sh" > /dev/null 2>&1 &
  exit 0
fi

exec tmux display-popup -E -w 80% -h 75% -b heavy \
  ${client:+-c "$client"} \
  -S "fg=colour33,bg=colour201,bold" \
  -s 'bg=colour17' \
  -T "#[fg=colour16] ⚡ SCRATCH #{t/f/%H#:%M:client_activity}〜 — nested tmux (C-t t で閉じる) ⚡ " \
  'unset TMUX TMUX_TMPDIR; tmux has-session -t scratch 2>/dev/null || tmux new-session -d -s scratch; tmux set -t scratch status 2; exec tmux attach -t scratch'
