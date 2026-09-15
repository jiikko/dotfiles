#!/usr/bin/env bash
# scripts/lib/tmux_float_geometry.sh (tt_float_geom) の unit テスト。
#
# この判定器が守るのは「tmux は 幅 == window 幅 / 高さ == window 高さ の floating pane を
# 受理しない (rc=1 / "size or position too large")」という実測 (幅 2026-08-21 / 高さ 2026-09-15)。
# 失敗は呼び出し側で無音になる (toast は `|| exit 0`、panel は `|| return 1`) ので、
# 退行するとパネルや通知が「黙って出なくなる」形で壊れる。実際、共通化する前は panel 側だけが
# `-gt` + `w=$win_w` で境界を許しており、幅 150 以下の window で panel が一度も出なかった。
#
# 判定はケース名ごとに出す (スイート全体の rc で読まないこと。1 ケースだけ緑で残る形が
# 変異検証で最も起こりやすい)。
set -uo pipefail
unset CDPATH

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIB="$ROOT_DIR/scripts/lib/tmux_float_geometry.sh"
fail=0
ok() { printf '✓ %s\n' "$1"; }
ng() { printf '✗ %s\n' "$1"; fail=1; }

[ -r "$LIB" ] || { ng "lib が無い: $LIB"; exit 1; }
# shellcheck source=scripts/lib/tmux_float_geometry.sh
. "$LIB"

# name | win_w win_h want_w want_h anchor | 期待 W H X Y (rc!=0 を期待するなら期待欄に "ERR")
run_case() {
  local name="$1" win_w="$2" win_h="$3" w="$4" h="$5" anchor="$6" want="$7"
  local got rc
  TT_FLOAT_W='' TT_FLOAT_H='' TT_FLOAT_X='' TT_FLOAT_Y=''
  tt_float_geom "$win_w" "$win_h" "$w" "$h" "$anchor"; rc=$?
  if [ "$want" = "ERR" ]; then
    [ "$rc" -ne 0 ] && ok "$name (rc=$rc で拒否)" || ng "$name: 非数値を受理した (rc=0 / W=$TT_FLOAT_W)"
    return
  fi
  if [ "$rc" -ne 0 ]; then ng "$name: rc=$rc (成功を期待)"; return; fi
  got="$TT_FLOAT_W $TT_FLOAT_H $TT_FLOAT_X $TT_FLOAT_Y"
  [ "$got" = "$want" ] && ok "$name → $got" || ng "$name: 期待 [$want] 実際 [$got]"
}

# --- 通常ケース (clamp が働かない) ---
run_case "panel 相当 (200x50 に 150x10 を右上)"        200 50 150 10 top-right     "150 10 50 0"
run_case "toast 相当 (200x50 に 8x3 を右下)"           200 50   8  3 bottom-right  "8 3 192 47"

# --- 🚨 境界: 幅 == window 幅 は tmux が拒否する (共通化前は panel 側が許していた) ---
run_case "幅 == window 幅 → 1 引く"                    200 50 200 10 top-right     "199 10 1 0"
run_case "幅 > window 幅 → 1 引く"                     200 50 300 10 top-right     "199 10 1 0"
run_case "panel の実幅 150 が window 幅と同じ"          150 50 150 10 top-right     "149 10 1 0"

# --- 🚨 境界: 高さ == window 高さ も拒否される ---
run_case "高さ == window 高さ → 1 引く"                 80 24  40 24 top-right     "40 23 40 0"
run_case "高さ > window 高さ → 1 引く"                  80 10  40 14 top-right     "40 9 40 0"
run_case "右下で高さ clamp (y も追随する)"              80  3  40  3 bottom-right  "40 2 40 1"

# --- 極端な window ---
run_case "1x1 の window (下限 1 で止まる)"                1  1  10 10 top-right     "1 1 0 0"
run_case "2x2 の window"                                 2  2  10 10 top-right     "1 1 1 0"

# --- 入力の縮退 (呼び出し側が縮退できるよう rc!=0) ---
run_case "window 幅が空"                                ""  50 150 10 top-right     ERR
run_case "window 高さが空"                             200  "" 150 10 top-right     ERR
run_case "非数値 (全角数字は [0-9] を通るので明示列挙)" "２００" 50 150 10 top-right ERR
run_case "希望幅が非数値"                              200  50 "abc" 10 top-right    ERR

[ "$fail" = 0 ] && printf '✓ tt_float_geom: 全ケース通過\n' || printf '✗ tt_float_geom: 失敗あり\n'
exit "$fail"
