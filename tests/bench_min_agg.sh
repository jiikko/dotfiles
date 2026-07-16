#!/usr/bin/env bash
# 複数回実行した bench の "metric=<name> ms=<value>" 行を per-metric の min に集約する。
#
# 使い方: for i in 1 2 3; do bench.sh; done | tests/bench_min_agg.sh | tests/check_bench_budgets.sh <budget>
#
# なぜ: 単発サンプルの metric は混雑 runner で粗い予算すら突き破る (実例 2026-07-17 run
# 29536560206: 全計測が 2〜5 倍に膨れ bufload 661/new_window_x20 684 が予算 600 を超過。
# 同じ run で min-of-5 集約済みの nvim startup だけは生き残った)。ノイズは片側性 (遅くなる方に
# しか出ない) なので run 間の min が真の速度の最良推定 = bench 内部の min-of-5 と同じ思想の
# run 階層版。予算を緩める対処は bufload 678ms 級の実回帰 (この予算が捕まえた実績) を見逃すため
# 採らない。
#
# 出力: metric ごとに「<name> runs (ms): v1 v2 ...」の注記と「metric=<name> ms=<min>」
# (初出順)。metric= 以外の行 (bench 内部の samples 注記等) は集約表示の重複を避けるため落とす。
# 不正な値はそのまま通し、下流 checker の数値検証で loud に落とす (ここで握り潰さない)。
set -uo pipefail

awk '
/^metric=/ {
  name = substr($1, 8)
  v = substr($2, 4)
  if (!(name in min)) { order[++n] = name; min[name] = v; vals[name] = v }
  else {
    vals[name] = vals[name] " " v
    if (v + 0 < min[name] + 0) min[name] = v
  }
}
END {
  for (i = 1; i <= n; i++) {
    name = order[i]
    printf "%s runs (ms): %s\n", name, vals[name]
    printf "metric=%s ms=%s\n", name, min[name]
  }
}
'
