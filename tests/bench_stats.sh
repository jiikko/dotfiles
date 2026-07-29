#!/usr/bin/env bash
# 複数回実行した bench の "metric=<name> ms=<value>" 行を統計集約し、
#   (a) stdout: per-metric min の "metric=<name> ms=<min>" 行 (check_bench_budgets.sh の入力 +
#       ジョブログでの機械取得用。rules/bench-watch-after-push.md が使う)
#   (b) $GITHUB_STEP_SUMMARY: 前回 run との比較つき markdown テーブル
#   (c) $4 (cur_tsv): 今回の全サンプルの TSV (次回 run が前回値として読む。Actions cache で持ち越す)
# を出す。旧 bench_min_agg.sh (min 集約のみ) の後継。
#
# 使い方: <N回分の bench 出力> | tests/bench_stats.sh <name> <budget_file> <prev_tsv> <cur_tsv>
#
# 予算ゲートが min を使う理由 (旧 bench_min_agg.sh から継承): 単発サンプルは混雑 runner で
# 粗い予算すら突き破る (実例 2026-07-17 run 29536560206: 全計測が 2〜5 倍に膨れた)。ノイズは
# 片側性 (遅くなる方にしか出ない) なので run 間の min が真の速度の最良推定。
#
# 前回比較は median + Mann-Whitney U 検定 (正規近似、両側 p<0.05): min は外れ値に強いが
# 分布の位置変化に鈍く、「全サンプルが少し悪化した」を捕まえられない。U 検定はノンパラで
# 混雑ノイズの歪んだ分布でも有意差だけを拾う。有意 かつ |Δmedian| >= 5% のときだけ
# 悪化/改善と表示する (統計的有意でも実務上無意味な微差は誤差圏扱い)。
#
# python3 不在時は (b)(c) を落として (a) だけ出す (予算ゲートは常に生きる縮退)。
set -uo pipefail

name="${1:?usage: bench_stats.sh <name> <budget_file> <prev_tsv> <cur_tsv>}"
budget_file="${2:?budget_file required}"
prev_tsv="${3:?prev_tsv path required (無くてよい)}"
cur_tsv="${4:?cur_tsv output path required}"

input="$(cat)"

if ! command -v python3 >/dev/null 2>&1; then
  # 縮退: min 集約だけ (予算ゲート維持)。summary/持ち越しは出せない
  printf '%s\n' "$input" | awk '
    /^metric=/ {
      n = substr($1, 8); v = substr($2, 4)
      if (!(n in min) || v + 0 < min[n] + 0) { if (!(n in min)) order[++c] = n; min[n] = v }
    }
    END { for (i = 1; i <= c; i++) printf "metric=%s ms=%s\n", order[i], min[order[i]] }
  '
  exit 0
fi

# ⚠️ `python3 -` はスクリプトを stdin (heredoc) から読むため、データをパイプで渡せない
# (heredoc が stdin を占有してパイプが黙って捨てられる。実測でハマった)。データは
# 一時ファイル渡しにする
data_file="$(mktemp)"
trap 'rm -f "$data_file"' EXIT
printf '%s\n' "$input" > "$data_file"
python3 - "$name" "$budget_file" "$prev_tsv" "$cur_tsv" "$data_file" <<'PYEOF'
import math
import os
import statistics
import sys

name, budget_file, prev_tsv, cur_tsv, data_file = sys.argv[1:6]

# --- 今回のサンプル収集 (出現順を保持) --------------------------------------
order: list[str] = []
cur: dict[str, list[float]] = {}
for line in open(data_file):
    line = line.strip()
    if not line.startswith("metric="):
        continue
    try:
        head, ms = line.split(" ", 1)
        metric = head[len("metric="):]
        val = float(ms[len("ms="):])
    except ValueError:
        # 不正値は min 行としてそのまま流し、下流 checker の数値検証で loud に落とす
        print(line)
        continue
    if metric not in cur:
        cur[metric] = []
        order.append(metric)
    cur[metric].append(val)

# --- (a) 予算ゲート用の min 行 ------------------------------------------------
for m in order:
    print(f"metric={m} ms={min(cur[m]):.3f}")

# --- 前回サンプル (TSV: name \t v1,v2,...) ------------------------------------
prev: dict[str, list[float]] = {}
try:
    with open(prev_tsv) as f:
        for row in f:
            parts = row.rstrip("\n").split("\t")
            if len(parts) == 2 and parts[1]:
                try:
                    prev[parts[0]] = [float(x) for x in parts[1].split(",")]
                except ValueError:
                    pass
except OSError:
    pass

# --- (c) 今回サンプルの持ち越し -----------------------------------------------
with open(cur_tsv, "w") as f:
    for m in order:
        f.write(f"{m}\t{','.join(f'{v:.3f}' for v in cur[m])}\n")

# --- 予算表示用 ---------------------------------------------------------------
budgets: dict[str, str] = {}
try:
    with open(budget_file) as f:
        for row in f:
            parts = row.split()
            if not parts or parts[0].startswith("#") or parts[0] == "calibrate":
                continue
            budgets[parts[0]] = parts[1] + (" rel" if "rel" in parts[2:] else "")
except OSError:
    pass

# --- Mann-Whitney U (正規近似・同順位は平均ランク) ------------------------------
def mwu_z(a: list[float], b: list[float]) -> float:
    combined = sorted((v, 0) for v in a) + sorted((v, 1) for v in b)
    combined.sort(key=lambda t: t[0])
    ranks: list[float] = [0.0] * len(combined)
    i = 0
    while i < len(combined):
        j = i
        while j + 1 < len(combined) and combined[j + 1][0] == combined[i][0]:
            j += 1
        avg = (i + j) / 2 + 1
        for k in range(i, j + 1):
            ranks[k] = avg
        i = j + 1
    r_a = sum(r for r, (_, g) in zip(ranks, combined) if g == 0)
    na, nb = len(a), len(b)
    u = r_a - na * (na + 1) / 2
    mean = na * nb / 2
    sd = math.sqrt(na * nb * (na + nb + 1) / 12)
    return (u - mean) / sd if sd > 0 else 0.0

# --- (b) Step Summary の比較テーブル -------------------------------------------
summary = os.environ.get("GITHUB_STEP_SUMMARY")
if summary:
    rows = [f"### {name} bench (n={max(len(v) for v in cur.values()) if cur else 0} runs, "
            "gate=min / 比較=median + Mann-Whitney U p<0.05)",
            "",
            "| metric | budget (ms) | prev p50 | cur p50 | Δ | 判定 | cur min |",
            "|---|---:|---:|---:|---:|:---|---:|"]
    for m in order:
        c = cur[m]
        c_p50, c_min = statistics.median(c), min(c)
        p = prev.get(m)
        if not p:
            rows.append(f"| {m} | {budgets.get(m, '-')} | - | {c_p50:.1f} | - | 🆕 初計測 | {c_min:.1f} |")
            continue
        p_p50 = statistics.median(p)
        delta = (c_p50 - p_p50) / p_p50 * 100 if p_p50 else 0.0
        z = mwu_z(p, c)
        if abs(z) >= 1.96 and abs(delta) >= 5:
            verdict = f"🔺 悪化 (|z|={abs(z):.1f})" if delta > 0 else f"✅ 改善 (|z|={abs(z):.1f})"
        else:
            verdict = "➖ 誤差圏"
        rows.append(f"| {m} | {budgets.get(m, '-')} | {p_p50:.1f} | {c_p50:.1f} | "
                    f"{delta:+.1f}% | {verdict} | {c_min:.1f} |")
    with open(summary, "a") as f:
        f.write("\n".join(rows) + "\n\n")
PYEOF
