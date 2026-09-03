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

# --- 3. 兄弟 benchmark の遮蔽 (issue 117) ---
# awk の振り分けは完全一致 (bench == "...") でなければならない。前方一致に戻すと、
# JA 対照 (BenchmarkViewSteadyJA 等) や桁違いの版 (StatusViewFrame2000) が既存 metric を
# 上書きし、**予算ゲートが別の benchmark の数字で判定する** (数値は出るので気づけない)。
# issue 051 の敵対的レビュー R1-5 が直した形だが、それを守るテストが無かった。
#
# 🚨 benchmark 名はここに列挙せず grep で導出する。列挙すると、兄弟を足したときに
#   追随を忘れる = まさにこの検査が守りたい事故を、検査自身が踏む。
SRC_DIR="$SCRIPT_DIR/../../src/glogx"
all_names=$(grep -h '^func Benchmark' "$SRC_DIR"/*_test.go |
  sed 's/^func \(Benchmark[A-Za-z0-9_]*\)(.*/\1/' | sort -u)
[ -n "$all_names" ] || { echo "✗ Benchmark を 1 件も列挙できない (走査が壊れている)" >&2; exit 1; }

registered=$(sed -n "s/.*-bench '^(\(.*\))\$'.*/\1/p" "$BENCH" | tr '|' ' ')
[ -n "$registered" ] || { echo "✗ bench_glogx.sh の -bench から登録名を抽出できない" >&2; exit 1; }

unreg=0
reg=0
for n in $all_names; do
  case " $registered " in
    *" $n "*)
      out=$(run_awk "$n-8 1000 2000000 ns/op 1024 B/op 5 allocs/op")
      [ -n "$out" ] || {
        echo "✗ 登録済みの $n が metric を 1 行も出さない (予算に載らず黙って未計測になる)" >&2
        exit 1
      }
      reg=$((reg + 1))
      ;;
    *)
      out=$(run_awk "$n-8 1000 2000000 ns/op 1024 B/op 5 allocs/op")
      [ -z "$out" ] || {
        echo "✗ 未登録の $n が metric を出した (兄弟 benchmark を遮蔽している): $out" >&2
        exit 1
      }
      unreg=$((unreg + 1))
      ;;
  esac
done
# 走査対象 0 件を緑にしない (導出が壊れたら赤にする)
[ "$unreg" -gt 0 ] || { echo "✗ 未登録の benchmark が 0 件 = 遮蔽の検査が空回りしている" >&2; exit 1; }
[ "$reg" -gt 0 ] || { echo "✗ 登録済みの benchmark が 0 件 = 導出が壊れている" >&2; exit 1; }
ok "未登録の benchmark ($unreg 本) は metric を出さず、登録済み ($reg 本) は必ず出す"

# 桁違いの版が無印を上書きしないこと (前方一致に戻すと 2000 版が status_view_frame も出す)
out=$(run_awk "BenchmarkStatusViewFrame2000-8 1000 2000000 ns/op 1024 B/op 5 allocs/op")
case "$out" in
  *status_view_2000*) : ;;
  *) echo "✗ StatusViewFrame2000 が自分の metric を出していない: $out" >&2; exit 1 ;;
esac
case "$out" in
  *status_view_frame*)
    echo "✗ StatusViewFrame2000 が無印の metric まで出した (前方一致の遮蔽): $out" >&2
    exit 1
    ;;
esac
ok "桁違いの版 (2000) は無印の metric を上書きしない"

echo "OK: $pass assertions (test_bench_glogx_metrics)"
