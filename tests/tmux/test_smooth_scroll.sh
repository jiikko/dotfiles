#!/usr/bin/env zsh
# vendor/tmux-plugins/tmux-smooth-scroll (補強パッチ入り) の headless テスト。検証項目:
#   1. conf ロードで copy-mode-vi の C-u/C-d が scroll.sh に re-bind される
#      (@smooth-scroll-mouse=false なので Wheel 系は re-bind されない)
#   2. 単発 C-u: アニメ完了後 scroll_position がちょうど halfpage (pane_height/2) 上がる
#   3. 連打 (リピート相当): 素通し + 世代打ち切りで重畳しない
#      (3 連打の合計が [2*half, 3*half] に収まる。1 打目のアニメは途中で打ち切られうる)
#   4. conf 再 source の冪等性: C-u の bind が二重にならず、単発が引き続き half ちょうど動く
#   5. 押下直後の pane 切替: 押下した pane だけがスクロールし、切替先は動かない
#      (init.sh の TMUX_PANE=#{pane_id} fire 時展開 = 押下時 pane 捕捉の回帰テスト)
#
# 検証ロジックの出典: 補強パッチ (VERSIONS.txt の local patches / src/scroll.sh コメント)。
# nvim 側の同型テストは tests/nvim/smooth_scroll_check.lua。

set -euo pipefail
unset CDPATH

TMUX_BIN_PATH=${TMUX_BIN:-tmux}
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)
CONF_FILE="$ROOT_DIR/_tmux.conf"
TMUX_TMPDIR=$(mktemp -d)
export TMUX_TMPDIR
# socket 名は短く保つ (TMUX_TMPDIR のフルパス込みで macOS の sun_path ~104byte 制限に
# かかると "File name too long" で起動できない)
SOCKET_NAME="dss-$$"

# resurrect / debounce 保存と smooth-scroll の状態ファイルを実データから隔離する
# (test_tmux.sh と同じ HOME 隔離 + TMPDIR 隔離。scroll.sh の状態ファイルは
#  ${TMPDIR:-/tmp}/tmux-smooth-scroll-<uid>/ に置かれるため TMPDIR ごと逃がす)
export HOME="$TMUX_TMPDIR/home"
export DOTFILES_DIR="$ROOT_DIR"
export XDG_DATA_HOME="$HOME/.local/share"
export TT_DEBOUNCE_STATE_DIR="$HOME/.cache/tt-debounce"
export TMPDIR="$TMUX_TMPDIR/tmp"
mkdir -p "$HOME" "$XDG_DATA_HOME" "$TT_DEBOUNCE_STATE_DIR" "$TMPDIR"

if ! command -v "$TMUX_BIN_PATH" >/dev/null 2>&1; then
  print -u2 "Error: tmux binary not found. Install tmux or set \$TMUX_BIN."
  exit 1
fi

TMUX_CMD=("$TMUX_BIN_PATH" -L "$SOCKET_NAME")

cleanup() {
  "${TMUX_CMD[@]}" kill-server >/dev/null 2>&1 || true
  rm -rf "$TMUX_TMPDIR"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

fail() {
  print -u2 "[test-smooth-scroll-tmux] FAIL: $1"
  exit 1
}

print "[test-smooth-scroll-tmux] starting isolated server"
start_log="$TMUX_TMPDIR/start.log"
# pane は対話シェルでなく直接コマンドで起動する。zsh (zle) は初期化時に端末問い合わせ・
# 入力 flush を行うため、send-keys で流し込むテキストが食われて不安定になる (実測)。
# スクロールバック素材は起動コマンド自身が出力し、以降シェル対話は一切使わない
# (copy-mode / C-u 押下は tmux レベルで pane プロセスと無関係)。
# TMUX / TMUX_PANE を消して起動する: このテスト自身が tmux 内で走ると、サーバが
# stale な TMUX_PANE を継承し、run-shell 子プロセス (scroll.sh) が実在しない pane を
# 叩いて全て空振りする (実測。tmux は run-shell に TMUX は設定するが TMUX_PANE は
# 上書きしないため、継承値が残ると勝ってしまう)
if ! env -u TMUX -u TMUX_PANE \
    "${TMUX_CMD[@]}" -f "$CONF_FILE" new-session -d -x 100 -y 30 -s scrolltest \
    'seq 1 300; exec sleep 3600' >"$start_log" 2>&1; then
  if grep -qiE "operation not permitted|permission denied" "$start_log"; then
    print -u2 "[test-smooth-scroll-tmux] skipped: tmux cannot create sockets in this environment"
    exit 0
  fi
  cat "$start_log" >&2
  fail "could not start tmux server"
fi

pane_id=$("${TMUX_CMD[@]}" display-message -p -t scrolltest '#{pane_id}')

# 1. re-bind の確認 (init.sh は conf ロード中の run-shell で走る。少し待って安定を取る)
bound=""
for _ in {1..50}; do
  bound=$("${TMUX_CMD[@]}" list-keys -T copy-mode-vi 2>/dev/null | grep -F "scroll.sh" || true)
  [[ -n "$bound" ]] && break
  sleep 0.1
done
[[ -n "$bound" ]] || fail "scroll keys were not rebound to scroll.sh"
print -r -- "$bound" | grep -q "C-u" || fail "C-u not rebound to scroll.sh"
print -r -- "$bound" | grep -q "C-d" || fail "C-d not rebound to scroll.sh"
print -r -- "$bound" | grep -q "Wheel" && fail "Wheel bindings were rebound despite @smooth-scroll-mouse=false"

# スクロールバック素材 (起動コマンドの seq 出力) が溜まるのを待って copy-mode に入る
for _ in {1..50}; do
  hist=$("${TMUX_CMD[@]}" display-message -p -t "$pane_id" '#{history_size}')
  [[ "$hist" -ge 100 ]] && break
  sleep 0.1
done
[[ "${hist:-0}" -ge 100 ]] || fail "history did not fill (history_size=${hist:-0})"
"${TMUX_CMD[@]}" copy-mode -t "$pane_id"

pane_height=$("${TMUX_CMD[@]}" display-message -p -t "$pane_id" '#{pane_height}')
half=$((pane_height / 2))

# 基準値から動き出すのを待ってから、動かなくなるまで待って返す。
# アニメは run-shell -b の非同期で、spawn レイテンシ (数百 ms) の間は基準値のまま
# 静止しているため、「安定 = 完了」だけの判定では開始前を完了と誤読する (実測)
wait_settled() {
  local baseline=$1 prev=-1 cur same=0
  for _ in {1..40}; do
    cur=$("${TMUX_CMD[@]}" display-message -p -t "$pane_id" '#{scroll_position}')
    [[ "$cur" != "$baseline" ]] && break
    sleep 0.1
  done
  for _ in {1..60}; do
    cur=$("${TMUX_CMD[@]}" display-message -p -t "$pane_id" '#{scroll_position}')
    if [[ "$cur" == "$prev" ]]; then
      same=$((same + 1))
      [[ "$same" -ge 3 ]] && { print -r -- "$cur"; return 0; }
    else
      same=0
    fi
    prev="$cur"
    sleep 0.1
  done
  print -r -- "$cur"
}

# 2. 単発押下: half ちょうど上がる
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
pos1=$(wait_settled 0)
[[ "$pos1" -eq "$half" ]] || fail "single press: expected scroll_position=$half, got $pos1"

# 3. 3 連打 (間隔ほぼ 0ms): 1 打目のアニメは打ち切られうる (部分 0〜half)、2-3 打目は素通しで
#    half ずつ。合計は [2*half, 3*half]。重畳 (押下数を超える加算) が無いことが本質の assert
sleep 0.3 # 直前の押下からリピート判定 (150ms) を跨ぐ
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
pos2=$(wait_settled "$pos1")
moved=$((pos2 - pos1))
if [[ "$moved" -lt $((2 * half)) || "$moved" -gt $((3 * half)) ]]; then
  fail "rapid presses: moved $moved, expected within [$((2 * half)), $((3 * half))]"
fi

# 4. conf 再 source の冪等性 (init.sh は re-bind 済みキーも同じ scroll.sh 引数へマッチさせる)
"${TMUX_CMD[@]}" source-file "$CONF_FILE" >/dev/null 2>&1 || fail "conf re-source failed"
sleep 0.5
cu_count=$("${TMUX_CMD[@]}" list-keys -T copy-mode-vi | grep -c "C-u" || true)
[[ "$cu_count" -eq 1 ]] || fail "after re-source: C-u bound $cu_count times (expected 1)"
"${TMUX_CMD[@]}" list-keys -T copy-mode-vi | grep "C-u" | grep -q "scroll.sh" \
  || fail "after re-source: C-u no longer bound to scroll.sh"
sleep 0.3
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
pos3=$(wait_settled "$pos2")
[[ $((pos3 - pos2)) -eq "$half" ]] || fail "post re-source press: moved $((pos3 - pos2)), expected $half"

# 5. 押下直後に pane を切り替える: 押下した pane だけが half スクロールし、切替先の
#    pane は動かない。横 split (高さ不変) で copy-mode の pane をもう 1 枚用意し、
#    C-u 送出直後 (run-shell -b の scroll.sh 起動前) に select-pane で切り替える
"${TMUX_CMD[@]}" split-window -h -d -t scrolltest 'seq 1 300; exec sleep 3600'
pane2_id=$("${TMUX_CMD[@]}" list-panes -t scrolltest -F '#{pane_id}' | grep -v "^${pane_id}$")
for _ in {1..50}; do
  hist2=$("${TMUX_CMD[@]}" display-message -p -t "$pane2_id" '#{history_size}')
  [[ "$hist2" -ge 100 ]] && break
  sleep 0.1
done
[[ "${hist2:-0}" -ge 100 ]] || fail "pane2 history did not fill (history_size=${hist2:-0})"
"${TMUX_CMD[@]}" copy-mode -t "$pane2_id"
pane2_before=$("${TMUX_CMD[@]}" display-message -p -t "$pane2_id" '#{scroll_position}')
# split-window は copy-mode 中の既存 pane の scroll_position を 0 にリセットする
# (tmux の resize 仕様、実測)。押下前の基準値はここで取り直す
pane1_base=$("${TMUX_CMD[@]}" display-message -p -t "$pane_id" '#{scroll_position}')
sleep 0.3 # リピート判定 (150ms) を跨ぐ
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" select-pane -t "$pane2_id" # scroll.sh 起動より先に切替を仕掛ける
pos4=$(wait_settled "$pane1_base")
[[ $((pos4 - pane1_base)) -eq "$half" ]] \
  || fail "pane switch: pressed pane moved $((pos4 - pane1_base)), expected $half"
pane2_after=$("${TMUX_CMD[@]}" display-message -p -t "$pane2_id" '#{scroll_position}')
[[ "$pane2_after" -eq "$pane2_before" ]] \
  || fail "pane switch: switched-to pane scrolled $((pane2_after - pane2_before)) lines (expected 0)"

print "[test-smooth-scroll-tmux] OK (half=$half, single=$pos1, rapid=+$moved, resourced=+$((pos3 - pos2)), paneswitch=+$((pos4 - pane1_base))/pane2±$((pane2_after - pane2_before)))"
