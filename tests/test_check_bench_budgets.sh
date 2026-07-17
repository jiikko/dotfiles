#!/bin/sh
# test_check_bench_budgets.sh — check_bench_budgets.sh の予算照合と混雑補正 (calibrate/rel) の
# 回帰テスト。特に「混雑 run で誤爆せず、真の回帰は逃さない」の両方向を、実際に起きた flake
# (run 29547499619) の実測値で検証する。
set -eu
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
CHECKER="$ROOT_DIR/tests/check_bench_budgets.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

pass=0
assert_rc() { # <期待rc> <説明> <budget内容> <bench出力>
  want="$1"; desc="$2"
  printf '%s\n' "$3" > "$TMP/budget.ci"
  rc=0; printf '%s\n' "$4" | "$CHECKER" "$TMP/budget.ci" > "$TMP/out.log" 2>&1 || rc=$?
  if [ "$rc" != "$want" ]; then
    echo "✗ $desc (rc=$rc, 期待=$want)" >&2
    sed 's/^/    /' "$TMP/out.log" >&2
    exit 1
  fi
  echo "✓ $desc"
  pass=$((pass + 1))
}

# --- 従来動作の後方互換 (calibrate/rel なしの予算ファイルは挙動不変) ---
assert_rc 0 "後方互換: 予算内は pass" \
"startup 300" \
"metric=startup ms=200"

assert_rc 1 "後方互換: 予算超過は fail" \
"startup 300" \
"metric=startup ms=400"

assert_rc 1 "後方互換: 予算にある metric が出力に無いと fail" \
"startup 300" \
"other line"

assert_rc 1 "後方互換: 非数値 ms は fail" \
"startup 300" \
"metric=startup ms="

# --- 混雑補正: 実 flake (run 29547499619) の再現 ---
# attempt1 (混雑): rtt 741.5 (基準 240 の 3.09 倍) で new_window 1096.2 が旧予算 600 を突破。
# 補正後は 600×3.09=1854 が実効上限になり誤爆しない
assert_rc 0 "混雑 run の実データで誤爆しない (rtt 較正で上限スケール)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 1200
new_window_x20 600 rel
kill_window_x20 600 rel" \
"metric=tmux_rtt_x100 ms=741.5
metric=new_window_x20 ms=1096.2
metric=kill_window_x20 ms=924.1"

# 静穏 run (rtt=基準相当) では従来の厳しさのまま: 同じ 1096.2 は fail する
assert_rc 1 "静穏 run では従来の厳しさ (真の回帰を検出)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 1200
new_window_x20 600 rel" \
"metric=tmux_rtt_x100 ms=230
metric=new_window_x20 ms=1096.2"

# 較正器より速い run (scale<1) で上限を絞らない (floor 1)
assert_rc 0 "scale の下限は 1 (速い runner で上限を絞らない)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 1200
new_window_x20 600 rel" \
"metric=tmux_rtt_x100 ms=120
metric=new_window_x20 ms=599"

# 極端な混雑 (scale > 4) は rel を警告のみに落とす (rc=0)。絶対予算は enforce したまま
assert_rc 0 "極端混雑: rel は警告のみ (rc=0)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 9999
new_window_x20 600 rel" \
"metric=tmux_rtt_x100 ms=1500
metric=new_window_x20 ms=9000"

assert_rc 1 "極端混雑でも絶対予算 metric は enforce (較正器自身の予算超過)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 1200
new_window_x20 600 rel" \
"metric=tmux_rtt_x100 ms=1500
metric=new_window_x20 ms=100"

# --- 構成エラーの検出 ---
assert_rc 1 "較正器自身に rel は構成エラー (自己参照で常に pass する事故防止)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 1200 rel" \
"metric=tmux_rtt_x100 ms=230"

assert_rc 1 "rel があるのに calibrate 宣言なしは構成エラー" \
"new_window_x20 600 rel" \
"metric=new_window_x20 ms=100"

assert_rc 1 "calibrate の基準が非数値は構成エラー" \
"calibrate tmux_rtt_x100 abc
new_window_x20 600 rel" \
"metric=new_window_x20 ms=100"

assert_rc 1 "較正器 metric が出力に無いと fail" \
"calibrate tmux_rtt_x100 240
new_window_x20 600 rel" \
"metric=new_window_x20 ms=100"

echo "[check-bench-budgets] $pass 件すべて pass"
