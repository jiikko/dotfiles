#!/usr/bin/env bash
# CI bench ハーネス: 指定ベンチを 3 回実行して per-metric min を集約し、Step Summary に
# 整形出力してから予算ファイルで超過をゲートする。bench.yml の nvim / zsh / tmux 3 job が
# 同じ「3 回実行 → min 集約 → Step Summary → 予算チェック」を三重化していたのを一元化。
#
# 3 回実行 → per-metric min 照合の理由: 単発サンプルは混雑した共有 runner で粗い予算すら
# 突き破る (2026-07-17 run 29536560206: 全計測が 2〜5 倍に膨れた)。min 集約でノイズ耐性を
# 持たせる。予算を緩める対処は bufload 678ms 級の実回帰を見逃すため採らない。
#
# 予算超過で fail する前に必ず Step Summary を書き出す (結果を残してから落とす)。
#
# 使い方: tests/run_bench.sh <name> <bench-script> <budget-file>
# 例:     tests/run_bench.sh nvim tests/nvim/bench_nvim.sh tests/nvim/bench_budgets.ci
set -o pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <name> <bench-script> <budget-file>" >&2
  exit 2
fi

name=$1
bench=$2
budget=$3
here=$(cd "$(dirname "$0")" && pwd)  # tests/ を絶対パスに (集約/チェックスクリプトの解決用)

bench_rc=0
out="$(for _ in 1 2 3; do "$bench" || exit 1; done | "$here/bench_min_agg.sh")" || bench_rc=$?
{
  echo "### $name bench (min of 3 runs)"
  echo '```'
  echo "$out"
  echo '```'
} >> "$GITHUB_STEP_SUMMARY"
[ "$bench_rc" -eq 0 ] || { echo "::error::$bench failed (rc=$bench_rc)"; exit 1; }
echo "$out" | "$here/check_bench_budgets.sh" "$budget"
