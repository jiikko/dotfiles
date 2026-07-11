#!/usr/bin/env bash
# bench スクリプトの "metric=<name> ms=<value>" 出力を予算ファイルと突き合わせ、
# 超過があれば非 0 で終了する (CI のデグレ検出用)。
#
# 使い方: tests/nvim/bench_nvim.sh | tests/check_bench_budgets.sh tests/nvim/bench_budgets.ci
#
# 予算ファイルの形式: "<metric名> <上限ms>" の行 (# 始まりと空行は無視)。
# 予算は「shared runner のノイズで flake しない」ことを優先し、桁級の回帰
# (例: foldexpr の 678ms 級) を捕まえる粗い上限にする。素の性能改善の追跡は
# Step Summary の経時比較 (人間の目) が担当で、ここは安全網。
#
# 検出するもの:
#   - metric の予算超過
#   - 予算ファイルに載っている metric が出力に無い (bench 自体の失敗・metric 改名漏れ)
set -uo pipefail

budget_file="${1:?usage: bench.sh | check_bench_budgets.sh <budget-file>}"

declare -A budget seen
while read -r name limit _; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  budget[$name]="$limit"
done < "$budget_file"

fail=0
while IFS= read -r line; do
  printf '%s\n' "$line"
  [[ "$line" == metric=* ]] || continue
  name="${line#metric=}"; name="${name%% *}"
  ms="${line##*ms=}"
  seen[$name]=1
  limit="${budget[$name]:-}"
  [[ -z "$limit" ]] && continue
  if awk -v v="$ms" -v l="$limit" 'BEGIN { exit !(v + 0 > l + 0) }'; then
    printf '::error::bench regression: %s %sms > budget %sms (%s)\n' "$name" "$ms" "$limit" "$budget_file"
    fail=1
  fi
done

for name in "${!budget[@]}"; do
  if [[ -z "${seen[$name]:-}" ]]; then
    printf '::error::bench metric missing: %s (bench 失敗か metric 改名漏れ。%s も更新すること)\n' "$name" "$budget_file"
    fail=1
  fi
done

exit "$fail"
