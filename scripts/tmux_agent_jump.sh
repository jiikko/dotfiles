#!/usr/bin/env bash
# tmux_agent_jump.sh — @claude_state 持ち pane (エージェント) を fzf で選んでジャンプする。
# _tmux.conf の `bind A` から display-popup -E 経由で呼ばれる前提。
# 常駐パネル (tmux_agent_panel.sh) の「見る」に対する「飛ぶ」側。
#
# - 並び順は注意が必要な順: 🔔 input → ⚙ working → ✓ idle → 🔕 seen (同順位は場所順)
# - 名前は pane_title を使う (window_name だと同一 window の複数エージェントが
#   全行同じ名前になる。tmux_agent_panel.sh の list_agents コメント参照)
# - popup 専用セッション (scratch / claude-fork / launcher) は候補から除外する
#   (switch-client で飛ぶと popup 前提のセッションへ full attach してしまうため。
#   fzf_jump と同じ判断。scratch のエージェントへは prefix+t で入る)
# - 選択キーは空白を含まない安定キー pane_id (fzf_window_picker と同じ流儀)
set -euo pipefail
unset CDPATH

_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/tmux_popup_sessions.sh
. "$_dir/lib/tmux_popup_sessions.sh"       # TT_POPUP_SESSION_RE

# 場所は pane 番号まで (同一 window の複数エージェントを特定できるように)。
# タイトルは切り詰めない (popup は幅 85% あり、fzf は長い行を自前で丸める)。
# 経過時間は @claude_state_since (epoch。_claude/hooks/tmux-pane-state.sh が書く。
# 未設定なら空欄) から算出
rows=$(tmux list-panes -a \
  -F $'#{?@claude_state,#{@claude_state}\t#{pane_id}\t#{session_name}:#{window_index}.#{pane_index}\t#{@claude_state_since}\t#{pane_title},}' \
  | awk -F'\t' -v re="$TT_POPUP_SESSION_RE" 'NF && $3 !~ re' \
  | awk -F'\t' -v now="$(date +%s)" '{
      r = 4; c = 244                               # 既定 (seen ほか): 灰
      if      ($1 ~ /input/)   { r = 1; c = 203 }  # 入力待ち: 赤
      else if ($1 ~ /working/) { r = 2; c = 220 }  # 作業中: 黄
      else if ($1 ~ /idle/)    { r = 3; c = 10 }   # 完了: 緑
      rel = ""
      if ($4 ~ /^[0-9]+$/) {
        d = now - $4; if (d < 0) d = 0
        if      (d < 60)   rel = d "s"
        else if (d < 3600) rel = int(d/60) "m"
        else               rel = int(d/3600) "h"
      }
      printf "%d\t%s\t\033[38;5;%dm%s\033[0m\t\033[33m%s\033[0m\t%s\t%s\n", r, $2, c, $1, rel, $3, $5
    }' \
  | sort -t$'\t' -k1,1n -k6,6)

if [ -z "$rows" ]; then
  # popup 内表示。すぐ閉じると読めないので一拍置く
  printf '  🤖 エージェント (@claude_state 持ち pane) はいません\n'
  sleep 1.2
  exit 0
fi

# rank 列は並び替え専用なので落とし、pane_id を先頭キーにして fzf へ。
# 候補は少数 (エージェント数) なのでインクリメンタルサーチは不要 → --disabled で
# 絞り込みを切り、j/k をカーソル移動に割り当てる (vi 風。矢印キーも従来どおり効く)。
# プレビューは 300 行取得し follow で末尾 (最新出力) を表示、J/K で遡れる。
# ⚠️ 末尾表示に +999999 オフセットを使わないこと: clamp は「最終行がプレビューの
# 先頭に来る」位置で止まり、以降が全て空白 = 何も表示されないように見える (実測
# 2026-08-08。follow は最終行が最下部に来る)
selected=$(printf '%s\n' "$rows" | cut -f2- \
  | fzf --ansi --reverse --border --disabled --no-info \
        --prompt='agent (j/k: 移動, J/K: プレビュー, q: 閉じる)> ' \
        --delimiter=$'\t' --with-nth=2.. \
        --bind 'j:down,k:up,q:abort' \
        --bind 'J:preview-half-page-down,K:preview-half-page-up' \
        --preview 'tmux capture-pane -ep -t {1} -S -300' \
        --preview-window=down,60%,follow) || exit 0

target=$(printf '%s\n' "$selected" | cut -f1)
[ -n "$target" ] || exit 0

# pane 単位まで確実に選ぶ (switch-client の pane target 解決だけに頼らない)
tmux switch-client -t "$target"
tmux select-window -t "$target"
tmux select-pane -t "$target"
