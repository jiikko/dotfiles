#!/usr/bin/env bash
# CI bench ハーネス: 指定ベンチを BENCH_RUNS 回 (既定 20) 実行して統計集約
# (tests/bench_stats.sh) し、前回 run との比較テーブルを Step Summary に書き出してから
# 予算ファイルで超過をゲートする。bench.yml の nvim / zsh / tmux / glogx が共用する。
#
# 多数回実行 → per-metric min 照合の理由: 単発サンプルは混雑した共有 runner で粗い予算すら
# 突き破る (2026-07-17 run 29536560206: 全計測が 2〜5 倍に膨れた)。min 集約でノイズ耐性を
# 持たせる。予算を緩める対処は bufload 678ms 級の実回帰を見逃すため採らない。
# 前回比較 (median + Mann-Whitney U) の設計は bench_stats.sh ヘッダ参照。
#
# 前回サンプルの持ち越し: BENCH_PREV_FILE (前回 run の TSV。bench.yml が Actions cache で
# restore) を読み、BENCH_STATS_OUT に今回分を書く (同 cache が save。green run のみ save
# されるため、赤 run が比較基準を汚さない)。未設定/不在なら比較列は 🆕 になるだけ。
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
runs="${BENCH_RUNS:-20}"
prev_tsv="${BENCH_PREV_FILE:-/dev/null}"
cur_tsv="${BENCH_STATS_OUT:-$(mktemp)}"

bench_rc=0
# 集約結果 (min 行) はジョブログにも出す: Step Summary は API 非公開 (check-runs の
# output.summary は空) のため、過去 run との数値比較を CLI
# (`gh run view <id> --log | grep 'metric='`) で機械的に行う経路がログになる
# (rules/bench-watch-after-push.md が使う)
out="$(for _ in $(seq 1 "$runs"); do "$bench" || exit 1; done \
  | "$here/bench_stats.sh" "$name" "$budget" "$prev_tsv" "$cur_tsv")" || bench_rc=$?
printf '%s\n' "$out"
[ "$bench_rc" -eq 0 ] || { echo "::error::$bench failed (rc=$bench_rc)"; exit 1; }
echo "$out" | "$here/check_bench_budgets.sh" "$budget"
