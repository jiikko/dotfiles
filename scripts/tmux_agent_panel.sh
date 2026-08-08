#!/usr/bin/env bash
# tmux_agent_panel.sh — エージェント常駐パネル (herdr 風ダッシュボードの tmux 版)。
#
# 全セッション横断で @claude_state (⚙ working / 🔔 input / 🔕 seen / ✓ idle) を持つ
# pane を、現在 window の右上に floating pane (tmux 3.7+) で常時表示する。
# floating pane はフォーカスを奪わない (-d) ため作業を邪魔しない。
#
# モード:
#   toggle <window_id>   パネルの表示/非表示を切り替える (bind から)
#   follow <window_id>   window 切替時にパネルを新 window へ作り直す (hook から)
#   render               パネル pane 内で走る描画ループ (内部用)
#
# 構造上の制約 (tmux 3.7b 実測):
#   - floating pane は window 所属 (pane の一般則)。window を跨いで居続けられない
#     ため、after-select-window / client-session-changed hook の follow で
#     「旧 window の panel を kill → 新 window に作成」して追従する。
#   - move-pane は floating 属性を剥がして通常 split に降格させるため使えない
#     (kill + create が唯一の追従手段)。
#
# hook ノイズ抑止 (@agent_panel_busy):
#   panel の kill/create 自体が pane-exited / after-split-window hook を発火させ、
#   放置すると window 切替のたびに (1) tmux-toast の分割/閉じ通知 (2) resurrect の
#   debounce 保存 (1 回 ~20MB のダンプ) が走る。kill/create の直前に
#   @agent_panel_busy へ epoch を書き、bin/tmux-toast と
#   scripts/tmux_resurrect_debounced_save.sh が AGENT_PANEL_QUIET_SECS 秒以内の
#   イベントを無視する。副作用として、その窓の間の実イベント (人間の split 等) の
#   toast / debounce 保存も落ちるが、toast は装飾のみ・保存は次のイベントの
#   フルスナップショットが取り返すため損失は「窓内イベントが最後 + その後クラッシュ」
#   に限られる (周期保存と kill shim が残余をカバー)。
#
# KNOWN LIMITATION:
#   - resurrect のスナップショットに panel pane が写り込む。サーバ復元後は
#     「render コマンドが復元されない素の shell の floating pane」として 1 個残る
#     ことがある (toggle か kill-pane で消す。復元は稀なので許容)。
#   - panel が居る window の pane 数バッジ [N] は +1 される (実 pane なので仕様)。
#   - select-pane の方向移動でパネルにフォーカスが当たりうる (q や C-d で誤って
#     消さない限り実害なし。当たったら別 pane へ移動すればよい)。
set -uo pipefail
unset CDPATH

SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

PANEL_W=38          # パネル幅 (セル)。行の組み立て (build_lines) はこの幅に収まる前提
PANEL_MAX_H=14      # 高さ上限 (超過分は +N more に畳む)
REFRESH_SECS=2      # 描画ループの更新間隔
# busy 窓の秒数 (3 秒) は読み手側 (bin/tmux-toast / tmux_resurrect_debounced_save.sh の
# AGENT_PANEL_QUIET_SECS) が持つ。ここは書くだけ (epoch)

now_epoch() { date +%s; }

mark_busy() { tmux set-option -g @agent_panel_busy "$(now_epoch)" 2>/dev/null || true; }

panel_pane() { tmux show-option -gqv @agent_panel_pane 2>/dev/null; }
panel_on()   { [ "$(tmux show-option -gqv @agent_panel_on 2>/dev/null)" = "1" ]; }

# ⚠️ exit code でなく出力で判定する: display-message -p -t <消滅した pane> は
# stderr に "can't find pane" を吐きつつ exit 0 で空行を返す (tmux 3.7b 実測)
pane_alive() { [ -n "${1:-}" ] && [ -n "$(tmux display-message -p -t "$1" '#{pane_id}' 2>/dev/null)" ]; }

pane_window() { tmux display-message -p -t "$1" '#{window_id}' 2>/dev/null; }

# @claude_state 持ち pane の一覧 (tab 区切り: state, session:index, window_name)
list_agents() {
  tmux list-panes -a -F $'#{?@claude_state,#{@claude_state}\t#{=12:session_name}:#{window_index}\t#{=8:window_name},}' 2>/dev/null |
    awk 'NF'
}

kill_panel() {
  local p
  p="$(panel_pane)"
  if pane_alive "$p"; then
    mark_busy
    tmux kill-pane -t "$p" 2>/dev/null || true
  fi
  tmux set-option -gu @agent_panel_pane 2>/dev/null || true
}

create_panel() {
  local win="$1" n h win_w x pid
  n="$(list_agents | wc -l | tr -d ' ')"
  h=$((n + 2))
  [ "$h" -lt 3 ] && h=3
  [ "$h" -gt "$PANEL_MAX_H" ] && h=$PANEL_MAX_H
  win_w="$(tmux display-message -p -t "$win" '#{window_width}' 2>/dev/null)" || return 1
  case "$win_w" in ''|*[!0-9]*) return 1 ;; esac
  x=$((win_w - PANEL_W))
  [ "$x" -lt 0 ] && x=0
  mark_busy
  pid="$(tmux new-pane -d -P -F '#{pane_id}' -t "$win" \
    -x "$PANEL_W" -y "$h" -X "$x" -Y 0 \
    -s 'fg=colour252,bg=colour233' -R 'fg=colour240' \
    -- "$SELF" render)" || return 1
  tmux set-option -g @agent_panel_pane "$pid"
}

cmd_toggle() {
  local win="${1:-}"
  [ -n "$win" ] || win="$(tmux display-message -p '#{window_id}')"
  if panel_on; then
    tmux set-option -gu @agent_panel_on 2>/dev/null || true
    kill_panel
  else
    tmux set-option -g @agent_panel_on 1
    kill_panel   # 迷子の旧パネルがあれば掃除してから作る
    create_panel "$win"
  fi
}

cmd_follow() {
  local win="${1:-}" p
  panel_on || exit 0
  [ -n "$win" ] || win="$(tmux display-message -p '#{window_id}')"
  p="$(panel_pane)"
  if pane_alive "$p" && [ "$(pane_window "$p")" = "$win" ]; then
    exit 0   # すでに今の window に居る (同一 window 内の pane 移動等)
  fi
  kill_panel
  # hook (run-shell -b) 経由のため無音契約 (scripts/CLAUDE.md): 作成失敗 (window が
  # 狭すぎる等) でも stderr / 非 0 を返さない。失敗しても次の window 切替で再試行される
  create_panel "$win" >/dev/null 2>&1 || true
  exit 0
}

# ---- render (パネル pane 内で走る) -----------------------------------------

# state 文字列 → ソート優先度 / 256 色。色は _tmux.conf の @claude-state-fg と
# 同じ意味の対応 (input=203 / working=220 / seen=244 / idle=10)
state_rank() {
  case "$1" in
    *input*)   echo 1 ;;
    *working*) echo 2 ;;
    *idle*)    echo 3 ;;
    *)         echo 4 ;;   # seen ほか
  esac
}
state_color() {
  case "$1" in
    *input*)   echo 203 ;;
    *working*) echo 220 ;;
    *idle*)    echo 10 ;;
    *)         echo 244 ;;
  esac
}

draw_once() {
  local rows h body_max shown=0 total=0 n_input=0 n_working=0
  rows="$(list_agents)"
  h="$(tmux display-message -p '#{pane_height}' 2>/dev/null)"
  case "$h" in ''|*[!0-9]*) h=$PANEL_MAX_H ;; esac
  body_max=$((h - 1))

  local out line state loc name _rank color
  out=""
  if [ -n "$rows" ]; then
    # rank を先頭に付けて安定ソート → 表示行を組み立て
    while IFS=$'\t' read -r state loc name; do
      total=$((total + 1))
      case "$state" in *input*) n_input=$((n_input + 1)) ;; *working*) n_working=$((n_working + 1)) ;; esac
    done <<< "$rows"
    out+="$(printf '\e[1m 🤖 AGENTS %d  ⚙%d 🔔%d\e[0m' "$total" "$n_working" "$n_input")"$'\n'
    while IFS=$'\t' read -r _rank state loc name; do
      [ "$shown" -ge "$body_max" ] && break
      color="$(state_color "$state")"
      line="$(printf ' \e[38;5;%sm%s\e[0m %s %s' "$color" "$state" "$loc" "$name")"
      out+="$line"$'\n'
      shown=$((shown + 1))
    done < <(while IFS=$'\t' read -r state loc name; do
               printf '%s\t%s\t%s\t%s\n' "$(state_rank "$state")" "$state" "$loc" "$name"
             done <<< "$rows" | sort -t $'\t' -k1,1 -k3,3)
    if [ "$total" -gt "$shown" ]; then
      out+="$(printf '\e[38;5;240m  +%d more\e[0m' $((total - shown)))"$'\n'
    fi
  else
    out+=$'\e[1m 🤖 AGENTS 0\e[0m\n'
    out+=$'\e[38;5;240m  (no agents)\e[0m\n'
  fi

  # \e[H で先頭へ、各行 \e[K で残骸消去、末尾 \e[J で下を掃除 (全消し clear より無点滅)
  printf '\e[H'
  printf '%s' "$out" | while IFS= read -r line; do
    printf '%s\e[K\n' "$line"
  done
  printf '\e[J'
}

cmd_render() {
  printf '\e[?25l'   # カーソル非表示
  trap 'printf "\e[?25h"' EXIT
  while :; do
    draw_once
    sleep "$REFRESH_SECS"
  done
}

# ---- main -------------------------------------------------------------------

[ -n "${TMUX:-}" ] || { echo "tmux_agent_panel.sh: not inside tmux" >&2; exit 1; }

case "${1:-}" in
  toggle) cmd_toggle "${2:-}" ;;
  follow) cmd_follow "${2:-}" ;;
  render) cmd_render ;;
  *) echo "usage: tmux_agent_panel.sh toggle|follow [window_id] | render" >&2; exit 2 ;;
esac
