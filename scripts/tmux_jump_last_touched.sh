#!/bin/sh
# 「最後に作業した window」へジャンプする (prefix+u)。
#
# 起点は放置フェード (_tmux.conf の @fade 群) と同じ @last-touched (zsh preexec/precmd が
# コマンド実行時にスタンプする epoch 秒)。フェードで光っている window を目で探して番号を
# 打つ代わりに、スタンプ最大の window へ一発で飛ぶ。
# - 現在 window は候補から除外する (押して何も起きないより「次に新しい所」へ動く方が有用)
# - 未スタンプ window (コマンド未実行) は候補外
# - 候補なし (単一 window / 全未スタンプ) は無言で何もしない
set -eu

best=$(tmux list-windows -F '#{window_id} #{@last-touched} #{window_active}' |
  awk '$2 != "" && $3 != 1 { if ($2 + 0 > max) { max = $2 + 0; id = $1 } } END { if (id != "") print id }')

# 候補なしでも exit 0 で終える (`[ -n ] &&` の短絡だと exit 1 になり、tmux run-shell が
# "returned 1" のエラー表示を status line に出してしまう)
if [ -n "$best" ]; then
  tmux select-window -t "$best"
fi
