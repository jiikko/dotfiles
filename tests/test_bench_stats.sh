#!/bin/sh
# test_bench_stats.sh — bench_stats.sh の集約と比較判定の回帰テスト。
#
# 主眼は「環境差が大きい run では判定を出さない」(2026-08-01 導入)。実際に踏んだ偽悪化
# (run 30683781313: 較正器 ×1.5 の混雑で render_large_patch が換算後も +25% 残り 🔺 悪化 と
# 表示された。同じコミットの再実行で戻り flake と確定) を実測値で再現し、保留になることを pin する。
# 同時に「平常の環境差では従来どおり判定する」逆方向も見る (保留が常時出ると検出装置が死ぬ)。
set -eu
unset CDPATH

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
STATS="$ROOT_DIR/tests/bench_stats.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

if ! command -v python3 >/dev/null 2>&1; then
  echo "skip: python3 が無いので比較テーブルを検証できない (min 集約だけの縮退経路)"
  exit 0
fi

pass=0
ok() { echo "✓ $1"; pass=$((pass + 1)); }
fail() { echo "✗ $1" >&2; shift; sed 's/^/    /' >&2 <<EOF
$*
EOF
  exit 1; }

BUDGET="$TMP/budget.ci"
cat > "$BUDGET" <<'EOF'
calibrate calib 0.15
calib 5
slow 10 rel
EOF

# baseline 台帳: 較正器 0.09 / slow 0.95 の静穏な run が 1 本。
# ⚠️ サンプルを 6 本にするのは Mann-Whitney U の検出力のため: 2 本 vs 3 本では U の取りうる幅が
# 狭く、どれだけ差が開いても |z| が 1.96 に届かない (どんな悪化も誤差圏になりテストが無意味になる)。
PREV="$TMP/prev.tsv"
cat > "$PREV" <<'EOF'
#meta	sha=aaaaaaaa	run=1	repo=o/r
calib	0.090,0.092,0.091,0.090,0.093,0.091
slow	0.950,0.960,0.955,0.945,0.958,0.952
EOF

# row は比較テーブルから 1 metric の行だけ取り出す。⚠️ テーブル全体に grep をかけないこと:
# 較正器自身の行は rel ではないので混雑 run では素直に 🔺 悪化 と出る (環境が遅い事実の表示で
# あって回帰ではない)。表全体を見ると、その行を rel metric の判定と取り違える。
row() { grep -E "^\| $1 " "$TMP/summary.md" || true; }

run_stats() { # <stdin データ> → stdout=min 行 / $TMP/summary.md=比較テーブル
  : > "$TMP/summary.md"
  printf '%s\n' "$1" | GITHUB_STEP_SUMMARY="$TMP/summary.md" \
    "$STATS" test "$BUDGET" "$PREV" "$TMP/cur.tsv" > "$TMP/out.log" 2>&1
}

# --- (1) 混雑 run: 較正器が ×1.5 に膨れ、slow も一緒に膨れる (実 flake の形) -------------
# 換算後も +25% 残るため、従来はここで 🔺 悪化 が出ていた。
run_stats "$(printf 'metric=calib ms=0.13%d\nmetric=slow ms=1.7%d0\n' 5 8 6 9 4 7 7 8 5 8 6 9)"
case "$(row slow)" in
  *判定保留*) ;;
  *) fail "混雑 run で rel metric の判定を保留にしていない" "$(cat "$TMP/summary.md")" ;;
esac
case "$(row slow)" in
  *🔺*) fail "混雑 run で rel metric を悪化と断定した" "$(cat "$TMP/summary.md")" ;;
esac
grep -q "bench_env_shift=1.[45][0-9] trusted=no" "$TMP/out.log" ||
  fail "環境差がジョブログに出ない (CLI から追えない)" "$(cat "$TMP/out.log")"
ok "混雑 run (較正器 ×1.5) では rel metric の判定を出さず、環境差をログにも出す"

# --- (2) 平常 run: 環境差が小さければ従来どおり判定する ---------------------------------
# ⚠️ 保留が常時出ると回帰検出そのものが死ぬので、逆方向を必ず pin する。
run_stats "$(printf 'metric=calib ms=0.09%d\nmetric=slow ms=1.9%d0\n' 2 0 1 5 3 2 0 8 2 1 1 4)"
case "$(row slow)" in
  *判定保留*) fail "平常の環境差で判定を止めた (回帰を見逃す)" "$(cat "$TMP/summary.md")" ;;
esac
case "$(row slow)" in
  *🔺*) ;;
  *) fail "平常 run で 2 倍の悪化を検出できない" "$(cat "$TMP/summary.md")" ;;
esac
grep -q "bench_env_shift=1.0[0-9] trusted=yes" "$TMP/out.log" ||
  fail "平常 run の環境差が trusted=yes にならない" "$(cat "$TMP/out.log")"
ok "平常 run (較正器 ×1.0) では従来どおり悪化を検出する"

# --- (3) 予算ゲート用の min 行は常に出る (判定保留でも桁級の回帰は捕まえる) --------------
run_stats "metric=calib ms=0.135
metric=slow ms=1.782"
grep -q "^metric=slow ms=1.782$" "$TMP/out.log" ||
  fail "min 行が出ていない (予算ゲートの入力が消える)" "$(cat "$TMP/out.log")"
ok "判定保留でも min 行は出る (予算ゲートは生きたまま)"

# --- (4) baseline が無ければ判定も環境差も出さない (初計測) -----------------------------
: > "$TMP/empty.tsv"
: > "$TMP/summary.md"
printf 'metric=calib ms=0.100\nmetric=slow ms=1.000\n' | GITHUB_STEP_SUMMARY="$TMP/summary.md" \
  "$STATS" test "$BUDGET" "$TMP/empty.tsv" "$TMP/cur.tsv" > "$TMP/out.log" 2>&1
grep -q "初計測" "$TMP/summary.md" ||
  fail "baseline なしで初計測にならない" "$(cat "$TMP/summary.md")"
grep -q "bench_env_shift" "$TMP/out.log" &&
  fail "baseline が無いのに環境差を出した (比べる相手が無い)" "$(cat "$TMP/out.log")"
ok "baseline なしでは環境差を出さない (初計測)"

printf '\nAll bench-stats tests passed successfully! (%d)\n' "$pass"
