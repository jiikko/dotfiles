# shellcheck shell=bash
# tmux 全 window を fzf で曖昧検索して選ぶ共通 picker。
# 利用者: scripts/tmux_fzf_jump.sh (ジャンプ) / scripts/tmux_fzf_pane_move.sh (pane 移動)。
# かつて両者に「list-windows 整形 → 相対時刻 → paste/column → fzf」の骨格が verbatim 複製されて
# いたのを一本化 (2026-07-15)。popup 専用セッション除外パターン TT_POPUP_SESSION_RE は既に
# lib/tmux_popup_sessions.sh へ集約済みで、呼び出し側がそれを source 済みである前提。
#
#   tt_fzf_window_picker <prompt> [exclude-current]
#     exclude-current 非空 → 現在の window を候補から除外し (自 window へは join 不可)、
#                            「← いまここ」マークも出さない (pane 移動用)。
#     空                 → 現在の window も候補に含め「← いまここ」マークを付ける (ジャンプ用)。
#   選ばれた window_id を stdout に出力。候補なし / fzf キャンセルは非 0 で返す。
#
# 候補は「window_id<TAB>整形済み表示」を fzf に渡し、切り出し/プレビューは空白を含まない
# 安定キー window_id で行う (セッション名に空白があっても壊れない。旧実装が column 整形後の
# 空白区切りで target を千切っていた回帰の防止。理由の一次情報はこのコメント)。
tt_fzf_window_picker() {
  local prompt="$1" exclude_current="${2:-}"
  local current now rows list selected
  current=$(tmux display -p '#{session_name}:#{window_index}')
  now=$(date +%s)
  rows=$(tmux list-windows -a \
    -F "#{window_activity}	#{window_id}	#{session_name}:#{window_index}	#{window_name}#{?#{>:#{window_panes},1}, [#{window_panes}],}" \
    | awk -F'\t' -v re="$TT_POPUP_SESSION_RE" -v cur="$current" -v excl="$exclude_current" \
        '$3 !~ re && (excl == "" || $3 != cur)' \
    | sort -t$'\t' -k1,1rn \
    | awk -F'\t' -v now="$now" -v cur="$current" -v excl="$exclude_current" '{
        d = now - $1
        if      (d < 60)    rel = d "秒前"
        else if (d < 3600)  rel = int(d/60) "分前"
        else if (d < 86400) rel = int(d/3600) "時間前"
        else                rel = int(d/86400) "日前"
        if (excl == "") {
          mark = ($3 == cur) ? "\033[1;36m← いまここ\033[0m" : ""
          printf "%s\t%s\t\033[33m%s\033[0m\t%s\t%s\n", $2, $3, rel, $4, mark
        } else {
          printf "%s\t%s\t\033[33m%s\033[0m\t%s\n", $2, $3, rel, $4
        }
      }')
  [ -n "$rows" ] || return 1
  list=$(paste -d'\t' \
    <(printf '%s\n' "$rows" | cut -f1) \
    <(printf '%s\n' "$rows" | cut -f2- | column -ts$'\t'))
  selected=$(printf '%s\n' "$list" \
    | fzf --ansi --reverse --border --prompt="$prompt" \
          --delimiter=$'\t' --with-nth=2 \
          --preview 'tmux capture-pane -ep -t {1} | tail -40' \
          --preview-window=down,60%) || return 1
  printf '%s\n' "$selected" | cut -f1
}
