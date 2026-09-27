#!/usr/bin/env bash
# 非 current window のセル (window-status-format の放置フェード) の描画を pin する。
#
# 固定する不変条件:
#   - 見た目: 段 (bucket) ごとの bg / fg、busy・未スタンプ・負の経過・zoom・claude アイコンの組み合わせが
#     docs/tmux-window-fade.md の表どおりに展開される
#   - コスト: 1 セルの展開で @busy (pane ごとに前面コマンドを引く P ループ) を **1 回だけ**評価する。
#     @fade-bucket は鍵の 1 回 + ランプ色の 1 回まで (issue 501)。status は毎秒と set-option のたびに
#     全 window ぶん展開されるので、式の重複はそのまま再描画 1 回の重さになる。
#     時間ではなく「何回展開したか」を `display -v` の展開ログで数える (壁時計に依存しない)
#
# 🚨 socket 隔離: $TMUX は TMUX_TMPDIR より優先されるため必ず unset する。
set -uo pipefail
unset CDPATH
unset TMUX TMUX_PANE

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_BIN_PATH="${TMUX_BIN:-tmux}"
fails=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fails=$((fails + 1)); }

command -v "$TMUX_BIN_PATH" >/dev/null 2>&1 || {
  printf 'SKIP: tmux が無い環境なので実行系の検査は落とした\n'
  exit 77
}

TMUX_TMPDIR="$(mktemp -d)"
export TMUX_TMPDIR
# shellcheck source=tests/tmux/lib/isolate_env.sh
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"
# shellcheck source=tests/tmux/lib/kill_socket.sh
. "$ROOT_DIR/tests/tmux/lib/kill_socket.sh"
SOCK="dffade-$$"
REAL_TMUX="$(command -v "$TMUX_BIN_PATH")"
T=("$REAL_TMUX" -L "$SOCK" -f "$CONF_FILE")
cleanup() { tt_tmux_kill_socket "$SOCK" >/dev/null 2>&1; rm -rf "$TMUX_TMPDIR"; }
trap cleanup EXIT

iso="$("$REAL_TMUX" -L "$SOCK" ls 2>&1)"
case "$iso" in
  *"no server running"*|*"error connecting"*|"") ok "隔離: 専用 socket にサーバが無い ($SOCK)" ;;
  *) ng "隔離できていない (既存サーバが見えた): $iso"; exit 1 ;;
esac

# --- 準備: window 2 = 3 pane の被検体 (非 current) ---------------------------------------
"${T[@]}" new-session -d -s fade -x 160 -y 40 'sh' || { ng "セッションを作れない"; exit 1; }
win="$("${T[@]}" new-window -d -t fade -n w -P -F '#{window_id}' 'sh')" || { ng "window を作れない"; exit 1; }
"${T[@]}" set -w -t "$win" automatic-rename off
for _ in 1 2; do "${T[@]}" split-window -d -t "$win" 'sh'; done
p1="$("${T[@]}" display -t "$win.1" -p '#{pane_id}')"
idx="$("${T[@]}" display -t "$win" -p '#{window_index}')"
# 段の境界を跨がないように 1 段を 1000 秒へ伸ばす (@fade-step-secs はライブ調整できる定数)。
# 経過 1000k+1 秒 = 段 k。テストの実行が数秒かかっても段は変わらない
"${T[@]}" set -g @fade-step-secs 1000
# busy 判定は「前面が *zsh 以外」。被検体の sh は busy 扱いなので、busy でない状態は zsh を名乗る
# 前面へ差し替えて作る (exec -a で argv[0] を zsh にした sleep。pane_current_command は argv[0] を返す)
not_busy() {
  local p
  for p in $("${T[@]}" list-panes -t "$win" -F '#{pane_id}'); do
    "${T[@]}" respawn-pane -k -t "$p" "exec -a zsh sleep 100000"
  done
}
not_busy
sleep 0.3
[ "$("${T[@]}" display -t "$p1" -p '#{pane_current_command}')" = zsh ] \
  || { ng "前面を zsh に見せかけられない (pane_current_command=$("${T[@]}" display -t "$p1" -p '#{pane_current_command}'))"; exit 1; }

fmt="$("${T[@]}" show -gv window-status-format)"
render() { "${T[@]}" display -t "$win" -p "$fmt"; }
touch_ago() { "${T[@]}" set -w -t "$win" @last-touched "$(( $(date +%s) - $1 ))"; }

expect() { # expect <label> <期待>
  local got
  got="$(render)"
  if [ "$got" = "$2" ]; then ok "$1"; else ng "$1"; printf '    期待: %s\n    実際: %s\n' "$2" "$got"; fi
}

# --- 見た目 (docs/tmux-window-fade.md の表) ------------------------------------------------
"${T[@]}" set -wu -t "$win" @last-touched
expect "未スタンプ = 消灯" "#[bg=default]#[fg=colour240] $idx:#[fg=colour214][3]#[fg=colour240] w "
# 段 k の bg = colour(16 + 37 × (5 − k))、fg は段 0〜1 が黒 / 2〜4 が明灰、段 5 で消灯
bgs=(201 164 127 90 53)
fgs=(16 16 252 252 252)
for k in 0 1 2 3 4; do
  touch_ago $((1000 * k + 1))
  expect "段 $k" "#[bg=colour${bgs[$k]}]#[fg=colour${fgs[$k]}] $idx:#[fg=colour214][3]#[fg=colour${fgs[$k]}] w "
done
touch_ago 5001
expect "段 5 = 消灯" "#[bg=default]#[fg=colour240] $idx:#[fg=colour214][3]#[fg=colour240] w "
touch_ago 100000
expect "段の上限で止まる (消灯)" "#[bg=default]#[fg=colour240] $idx:#[fg=colour214][3]#[fg=colour240] w "
touch_ago -3000
expect "負の経過 (時計ずれ) は段 0" "#[bg=colour201]#[fg=colour16] $idx:#[fg=colour214][3]#[fg=colour16] w "

touch_ago 1001
"${T[@]}" resize-pane -Z -t "$p1"
expect "zoom は fade の後に暗赤を重ねる" "#[bg=colour164]#[fg=colour16]#[bg=colour52] $idx:#[fg=colour214][3]#[fg=colour16] w "
"${T[@]}" resize-pane -Z -t "$p1"

"${T[@]}" set -p -t "$p1" @claude_state '⚙ working'
expect "claude アイコンの後は fade の文字色へ戻す" "#[bg=colour164]#[fg=colour16] $idx:#[fg=colour214][3]#[fg=colour16] #[fg=colour220]⚙#[fg=colour16] w "
"${T[@]}" set -pu -t "$p1" @claude_state

"${T[@]}" respawn-pane -k -t "$p1" 'sleep 100000'
sleep 0.3
touch_ago 100000
expect "busy は経過に関係なく最明" "#[bg=colour201]#[fg=colour16] $idx:#[fg=colour214][3]#[fg=colour16] w "

# --- コスト: 1 セルの展開で @busy を 1 回、@fade-bucket を 2 回までしか展開しない ----------
# `display -v` は format の展開を 1 段ずつログに出す。@busy / @fade-bucket の本体が展開された回数を数える
busy_body="$("${T[@]}" show -gv @busy)"
bucket_body="$("${T[@]}" show -gv @fade-bucket)"
count_expansions() { # count_expansions <式の本体>
  "${T[@]}" display -v -t "$win" -p "$fmt" | grep -cF "expanding format: $1"
}
check_cost() { # check_cost <label>
  local nb nk
  nb="$(count_expansions "$busy_body")"
  nk="$(count_expansions "$bucket_body")"
  if [ "$nb" = 1 ]; then ok "$1: @busy の展開 1 回"; else ng "$1: @busy を $nb 回展開した (期待 1。鍵をセルごとに 1 回だけ計算する形が崩れた)"; fi
  if [ "$nk" -le 2 ] 2>/dev/null; then ok "$1: @fade-bucket の展開 $nk 回 (<= 2)"; else ng "$1: @fade-bucket を $nk 回展開した (期待 2 以下)"; fi
}
if [ -z "$busy_body" ] || [ -z "$bucket_body" ]; then ng "@busy / @fade-bucket が定義されていない"; exit 1; fi
# 展開ログの形が変わって 0 件になると「少ない = 合格」に倒れるので、先に 1 件以上拾えることを確かめる
[ "$(count_expansions "$busy_body")" -ge 1 ] 2>/dev/null \
  || { ng "display -v の展開ログから @busy を拾えない (tmux のログ形式が変わった? 判定不能)"; exit 1; }
check_cost "busy"
not_busy
sleep 0.3
touch_ago 1001
check_cost "段 1"
touch_ago 100000
check_cost "消灯"
"${T[@]}" set -wu -t "$win" @last-touched
check_cost "未スタンプ"

[ "$fails" -eq 0 ] || { printf '%d 件失敗\n' "$fails"; exit 1; }
