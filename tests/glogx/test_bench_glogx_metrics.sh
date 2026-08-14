#!/bin/sh
# test_bench_glogx_metrics.sh — glogx bench の配管 (bench_glogx.sh → bench_budgets.ci) の回帰テスト
# (issue 063)。守る不変条件:
#   1. bench_budgets.ci に載っている全 metric を bench_glogx.sh が emit し、値が数値である
#      (予算と emit の食い違いを CI まで行かずローカルで検出する)
#   2. 列ずれガード: go test の出力列が想定 ($4=ns/op, $6=B/op) と違う行は metric を出さない
#      (issue 051 実測の false green: b.ReportMetric 持ちの benchmark が混ざると列がずれ、
#      ガード無しでは別の数値を予算照合して黙って pass する)
# 実 bench は GLOGX_BENCHTIME=1x で配管だけ速く回す (値の大小は見ない。予算照合は CI の
# check_bench_budgets.sh の責務)。
set -eu
unset CDPATH

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
BENCH="$SCRIPT_DIR/bench_glogx.sh"
BUDGETS="$SCRIPT_DIR/bench_budgets.ci"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

pass=0
ok() { echo "✓ $1"; pass=$((pass + 1)); }

# --- 1. 実 bench (1x) で、予算の全 metric が数値付きで出ること ---
# Tests workflow の CI runner には Go が無い (Makefile の run_tests_parallel 直上コメントが
# 一次情報。glogx の Go テストは src_glogx.yml、bench 本番は bench.yml が Go 入りで回す) ため、
# go 不在ならこの節だけ明示 skip する (列ずれガードの節 2 は合成入力なので常に走る)
if command -v go >/dev/null 2>&1; then
  GLOGX_BENCHTIME=1x "$BENCH" > "$TMP/real.out"

  # 予算ファイルから metric 名を抽出 (コメント・空行・calibrate 宣言行を除く 1 列目)
  awk '!/^[ \t]*(#|$)/ && $1 != "calibrate" { print $1 }' "$BUDGETS" > "$TMP/metrics"
  [ -s "$TMP/metrics" ] || { echo "✗ $BUDGETS から metric を 1 件も抽出できない" >&2; exit 1; }

  while IFS= read -r m; do
    line=$(grep "^metric=$m ms=" "$TMP/real.out" || true)
    if [ -z "$line" ]; then
      echo "✗ 予算にある metric が bench 出力に無い: $m" >&2
      sed 's/^/    /' "$TMP/real.out" >&2
      exit 1
    fi
    val=${line#metric="$m" ms=}
    case "$val" in
      ''|*[!0-9.]*) echo "✗ $m の ms が数値でない: $line" >&2; exit 1 ;;
    esac
  done < "$TMP/metrics"
  ok "予算の全 $(wc -l < "$TMP/metrics" | tr -d ' ') metric が数値付きで emit される"
else
  echo "- go 不在のため実 bench の emit 検査を skip (列ずれガード検査は続行)"
fi

# --- 2. 列ずれガード: 想定列なら emit / ずれたら emit しない ---
run_awk() { # <合成 go test 出力> → stdout に metric 行
  printf '%s\n' "$1" > "$TMP/synth.in"
  GLOGX_BENCH_INPUT="$TMP/synth.in" "$BENCH" 2> "$TMP/synth.err"
}

out=$(run_awk "BenchmarkViewSteady-8 1000 2000000 ns/op 1024 B/op 5 allocs/op")
[ "$out" = "metric=view_steady ms=2.000
metric=view_steady_alloc_kb ms=1.000" ] || {
  echo "✗ 想定列の合成入力で期待の emit にならない:" >&2
  printf '%s\n' "$out" | sed 's/^/    /' >&2
  exit 1
}
ok "想定列 (\$4=ns/op, \$6=B/op) は ms/alloc_kb を正しく換算して emit"

# b.ReportMetric が混ざって列がずれた形 ($4=ms/op)。emit ゼロ + stderr 警告であること
out=$(run_awk "BenchmarkViewSteady-8 1000 0.001 ms/op 2000000 ns/op 1024 B/op 5 allocs/op")
[ -z "$out" ] || {
  echo "✗ 列ずれ入力なのに emit された (false green の再発): $out" >&2
  exit 1
}
grep -q "列が想定と違う" "$TMP/synth.err" || {
  echo "✗ 列ずれ時の stderr 警告が出ていない" >&2
  exit 1
}
ok "列ずれ入力は emit しない (checker の metric 欠落検出で loud に落ちる側へ倒す)"

# 較正器は時間のみ emit (alloc_kb を出さない)
out=$(run_awk "BenchmarkCalibrate-8 100 3000000 ns/op 8 B/op 1 allocs/op")
[ "$out" = "metric=glogx_calib ms=3.000" ] || {
  echo "✗ 較正器の emit が時間のみになっていない: $out" >&2
  exit 1
}
ok "較正器 (glogx_calib) は時間のみ emit"

echo "OK: $pass assertions (test_bench_glogx_metrics)"
