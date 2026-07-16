#!/usr/bin/env bash
# bench スクリプトの "metric=<name> ms=<value>" 出力を予算ファイルと突き合わせ、
# 超過があれば非 0 で終了する (CI のデグレ検出用)。
#
# 使い方: tests/nvim/bench_nvim.sh | tests/check_bench_budgets.sh tests/nvim/bench_budgets.ci
#
# 予算ファイルの形式: "<metric名> <上限ms>" の行 (# 始まりと空行は無視)。
# 各予算値の決め方 (粗い安全網か「現在速度」ゲートか) は予算ファイル側のコメントが一次情報。
#
# 検出するもの:
#   - metric の予算超過
#   - metric の ms が数値でない (bench 側の計測失敗の素通り防止)
#   - 予算ファイルに載っている metric が出力に無い (bench 自体の失敗・metric 改名漏れ)
#   - 予算ファイルの上限が数値でない (予算ファイル破損)
set -uo pipefail

budget_file="${1:?usage: bench.sh | check_bench_budgets.sh <budget-file>}"

declare -A budget seen
while read -r name limit _; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  # 上限が数値でない (予算ファイル破損) は照合を始める前に loud に落とす
  if ! [[ "$limit" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    printf '::error::budget malformed: "%s %s" (%s)\n' "$name" "$limit" "$budget_file"
    exit 1
  fi
  budget[$name]="$limit"
done < "$budget_file"

fail=0
while IFS= read -r line; do
  printf '%s\n' "$line"
  [[ "$line" == metric=* ]] || continue
  name="${line#metric=}"; name="${name%% *}"
  ms="${line##*ms=}"
  seen[$name]=1
  # ms が数値でない (bench 側の計測失敗の素通り) を fail にする。awk の v+0 は
  # 空文字や文字列を 0 に潰すため、ここで弾かないと予算照合が黙って pass する
  if ! [[ "$ms" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    printf '::error::bench metric malformed: %s ms="%s" (数値でない。bench 側の計測失敗)\n' "$name" "$ms"
    fail=1
    continue
  fi
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
