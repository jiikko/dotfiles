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
# 状態隔離 (HOME/XDG/TT_DEBOUNCE) は lib へ集約 (test_tmux/bench と共通)。
source "$ROOT_DIR/tests/tmux/lib/isolate_env.sh"
# smooth-scroll の状態ファイルは ${TMPDIR}/tmux-smooth-scroll-<uid>/ なので TMPDIR ごと隔離する (本テスト固有)。
export TMPDIR="$TMUX_TMPDIR/tmp"
mkdir -p "$TMPDIR"

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
    # ⚠️ 丸ごと skip は **exit 77** (automake の慣例)。0 で抜けると runner が [ok] と数え、
    # 以降の assert が 1 本も走っていないことが緑に埋もれる (tests/CLAUDE.md / issue 139)。
    exit 77
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
grep -q "C-u" <<< "$bound" || fail "C-u not rebound to scroll.sh"
grep -q "C-d" <<< "$bound" || fail "C-d not rebound to scroll.sh"
grep -q "Wheel" <<< "$bound" && fail "Wheel bindings were rebound despite @smooth-scroll-mouse=false"
# @smooth-scroll-scopes='halfpage fullpage' なので normal (C-e/C-y) は native のまま
grep -qE "C-e|C-y" <<< "$bound" && fail "normal scope keys were rebound despite scopes"

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

# scroll.sh の per-pane 状態ファイル (内容: "gen last_press_ms anim_until_ms")。
# gen は arbiter.pl が flock 下で押下 1 回につき必ず +1 する = 「送った押下が処理された」
# ことの唯一の直接観測点。位置の静止から完了を推測すると、run-shell -b の spawn レイテンシ
# (数百 ms) が静止判定窓 (300ms) を超えたときに「まだ処理されていない押下」を
# 「完了」と誤読する (CI で連打ケースが moved=half 相当で落ちた実測。ローカルは spawn が
# 速いため顕在化しない)。閾値を緩める対処は再発するので、押下の処理完了は gen で待つ。
STATE_DIR_TEST="$TMPDIR/tmux-smooth-scroll-$(id -u)"

read_gen() {
  local f="$STATE_DIR_TEST/$1"
  [[ -r "$f" ]] || { print -r -- 0; return 0; }
  awk 'NR==1 {print ($1 ~ /^[0-9]+$/) ? $1 : 0; exit} END {if (NR==0) print 0}' "$f"
}

# 指定 pane の gen が target 以上になるまで待つ (= 送った押下がすべて arbiter を通過した)
wait_gen() {
  local pane=$1 target=$2 cur=0
  for _ in {1..100}; do
    cur=$(read_gen "$pane")
    [[ "$cur" -ge "$target" ]] && return 0
    sleep 0.1
  done
  fail "presses were not processed (gen=$cur, expected >= $target)"
}

# 基準値から動き出すのを待ってから、動かなくなるまで待って返す。
# ⚠️ 呼ぶ前に wait_gen で「押下が処理済み」を確かめること。未処理の押下が残った状態で
# 静止を見ると、その押下ぶんを取りこぼした位置を完了値として返す
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
gen0=$(read_gen "$pane_id")
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
wait_gen "$pane_id" $((gen0 + 1))
pos1=$(wait_settled 0)
[[ "$pos1" -eq "$half" ]] || fail "single press: expected scroll_position=$half, got $pos1"

# 3. 3 連打 (間隔ほぼ 0ms): 1 打目のアニメは打ち切られうる (部分 0〜half)、2-3 打目は素通しで
#    half ずつ。合計は [2*half, 3*half]。重畳 (押下数を超える加算) が無いことが本質の assert
sleep 0.3 # 直前の押下からリピート判定 (150ms) を跨ぐ
gen1=$(read_gen "$pane_id")
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
wait_gen "$pane_id" $((gen1 + 3)) # 3 打すべてが処理されるまで位置を測らない
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
grep -q "scroll.sh" <<< "$("${TMUX_CMD[@]}" list-keys -T copy-mode-vi | grep "C-u")" \
  || fail "after re-source: C-u no longer bound to scroll.sh"
sleep 0.3
gen2=$(read_gen "$pane_id")
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
wait_gen "$pane_id" $((gen2 + 1))
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
gen3=$(read_gen "$pane_id")
"${TMUX_CMD[@]}" send-keys -t "$pane_id" C-u
"${TMUX_CMD[@]}" select-pane -t "$pane2_id" # scroll.sh 起動より先に切替を仕掛ける
wait_gen "$pane_id" $((gen3 + 1))
pos4=$(wait_settled "$pane1_base")
[[ $((pos4 - pane1_base)) -eq "$half" ]] \
  || fail "pane switch: pressed pane moved $((pos4 - pane1_base)), expected $half"
pane2_after=$("${TMUX_CMD[@]}" display-message -p -t "$pane2_id" '#{scroll_position}')
[[ "$pane2_after" -eq "$pane2_before" ]] \
  || fail "pane switch: switched-to pane scrolled $((pane2_after - pane2_before)) lines (expected 0)"

print "[test-smooth-scroll-tmux] OK (half=$half, single=$pos1, rapid=+$moved, resourced=+$((pos3 - pos2)), paneswitch=+$((pos4 - pane1_base))/pane2±$((pane2_after - pane2_before)))"
