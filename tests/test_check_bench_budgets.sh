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

# --- 超過文言の表示単位 (issue 064): 単位は metric 名の接尾辞から引く ---
# *_kb / *_mb の超過が「〜ms」と出ると時間の回帰に見え、対処 (確保 = 絶対値 / 時間 = rel)
# の入口を誤らせる。エラー経路と警告経路 (極端混雑) の両方を見る (片方だけ直すのが典型的な
# 取りこぼし、と issue が警告している形)
assert_msg() { # <期待部分文字列> <説明> <budget内容> <bench出力>
  want="$1"; desc="$2"
  printf '%s\n' "$3" > "$TMP/budget.ci"
  printf '%s\n' "$4" | "$CHECKER" "$TMP/budget.ci" > "$TMP/out.log" 2>&1 || true
  if ! grep -qF "$want" "$TMP/out.log"; then
    echo "✗ $desc (期待部分文字列: $want)" >&2
    sed 's/^/    /' "$TMP/out.log" >&2
    exit 1
  fi
  echo "✓ $desc"
  pass=$((pass + 1))
}

assert_msg "x_alloc_kb 50KB > budget 40KB" "エラー経路: *_kb の超過は KB で出る" \
"x_alloc_kb 40" \
"metric=x_alloc_kb ms=50"

assert_msg "server_rss_mb 42MB > budget 30MB" "エラー経路: *_mb の超過は MB で出る" \
"server_rss_mb 30" \
"metric=server_rss_mb ms=42"

assert_msg "startup 400ms > budget 300ms" "エラー経路: 時間 metric は従来どおり ms" \
"startup 300" \
"metric=startup ms=400"

# 警告経路 (較正 > CALIB_MAX_SCALE=4 で rel の gating が警告に落ちる) でも単位が引かれる
assert_msg "leak_alloc_kb 90KB > 50KB" "警告経路 (極端混雑): *_kb の超過も KB で出る" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 3000
leak_alloc_kb 10 rel" \
"metric=tmux_rtt_x100 ms=1200
metric=leak_alloc_kb ms=90"

# 🚨 rel の実効上限を 0.1 刻みに丸めないこと (issue 269)。glogx のフレーム系は
# 0.027〜0.12 ms の帯にいるので、丸めると予算を締めた瞬間に全部が量子化に落ちる。
# 較正スケール 1.0 (静穏 run) で、宣言した小数がそのまま実効上限になることを固定する。
assert_msg "frame_ms 0.2ms > budget 0.15ms" "rel の実効上限が 0.1 刻みに丸められない (小数が生きる)" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 240
frame_ms 0.15 rel" \
"metric=tmux_rtt_x100 ms=240
metric=frame_ms ms=0.2"

# 丸めがあると 0.04 -> 0.0 になり、予算内の値まで超過扱いになっていた
assert_rc 0 "rel の小さな上限が 0 に潰れない" \
"calibrate tmux_rtt_x100 240
tmux_rtt_x100 240
tiny_ms 0.04 rel" \
"metric=tmux_rtt_x100 ms=240
metric=tiny_ms ms=0.03"

echo "[check-bench-budgets] $pass 件すべて pass"
