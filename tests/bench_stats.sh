#!/usr/bin/env bash
# 複数回実行した bench の "metric=<name> ms=<value>" 行を統計集約し、
#   (a) stdout: per-metric min の "metric=<name> ms=<min>" 行 (check_bench_budgets.sh の入力 +
#       ジョブログでの機械取得用。rules/bench-watch-after-push.md が使う)
#   (b) $GITHUB_STEP_SUMMARY: 直近 run 群との比較つき markdown テーブル
#   (c) $4 (cur_tsv): 今回 + 直近 run のサンプル台帳 (次回 run が baseline として読む。
#       Actions cache で持ち越す。詳細は下の「rolling baseline」)
# を出す。旧 bench_min_agg.sh (min 集約のみ) の後継。
#
# 使い方: <N回分の bench 出力> | tests/bench_stats.sh <name> <budget_file> <prev_tsv> <cur_tsv>
#
# 予算ゲートが min を使う理由 (旧 bench_min_agg.sh から継承): 単発サンプルは混雑 runner で
# 粗い予算すら突き破る (実例 2026-07-17 run 29536560206: 全計測が 2〜5 倍に膨れた)。ノイズは
# 片側性 (遅くなる方にしか出ない) なので run 間の min が真の速度の最良推定。
#
# 比較は median + Mann-Whitney U 検定 (正規近似、両側 p<0.05)。有意 かつ |Δmedian| >= 5% の
# ときだけ 悪化/改善 と表示する (統計的有意でも実務上無意味な微差は誤差圏扱い)。
#
# rolling baseline (2026-07-29 導入): baseline は「直前 1 run」でなく「直近 BASELINE_RUNS run の
# サンプルプール」。台帳 TSV は run ごとのブロック (#meta 行 + metric 行、新しい順) を持ち、
# 書き出し時に古いブロックを落として自動ローテーションする。単一 run 比較は runner 世代の
# 当たり外れがそのまま結果に出るため、プールで世代ばらつきを均す (n も 20→最大 100 に増え
# U 検定の検出力が上がる)。
#
# 較正 (⚖): prev と cur は別 runner (別 CPU 世代・別混雑度)。予算ゲートの rel と同じ思想で、
# rel 指定の metric は「各 prev ブロックの較正器 p50 と cur の較正器 p50 の比」でブロック単位に
# cur 環境へ換算してからプールする (run ごとに環境が違うため一律スケールは誤り。無変更 push が
# 軒並み悪化表示になった実例 run 30453173113 への対処)。絶対予算の metric (RSS 等) は素のまま。
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

BASELINE_RUNS = 5  # プールする直近 run 数 (今回のブロックを含む台帳の保持数)

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

# --- baseline 台帳の読み込み ----------------------------------------------------
# 形式: run ごとのブロック (新しい順)。ブロック = "#meta\tsha=..\trun=..\trepo=.." 行 +
# "name\tv1,v2,..." 行の並び。旧形式 (メタなし単一ブロック / メタ付き単一ブロック) も
# 「1 ブロックの台帳」としてそのまま読める。
class Block:
    def __init__(self) -> None:
        self.meta: dict[str, str] = {}
        self.metrics: dict[str, list[float]] = {}

blocks: list[Block] = []
try:
    with open(prev_tsv) as f:
        blk: Block | None = None
        for row in f:
            parts = row.rstrip("\n").split("\t")
            if parts and parts[0] == "#meta":
                blk = Block()
                blk.meta = dict(kv.split("=", 1) for kv in parts[1:] if "=" in kv)
                blocks.append(blk)
                continue
            if len(parts) == 2 and parts[1]:
                if blk is None:  # 旧々形式 (メタ行なし): 先頭に匿名ブロックを立てる
                    blk = Block()
                    blocks.append(blk)
                try:
                    blk.metrics[parts[0]] = [float(x) for x in parts[1].split(",")]
                except ValueError:
                    pass
except OSError:
    pass

# --- (c) 台帳の更新 (今回ブロックを先頭に、直近 BASELINE_RUNS 件へローテーション) ----
with open(cur_tsv, "w") as f:
    f.write("#meta\tsha={}\trun={}\trepo={}\n".format(
        os.environ.get("GITHUB_SHA", "?")[:12],
        os.environ.get("GITHUB_RUN_ID", "?"),
        os.environ.get("GITHUB_REPOSITORY", "?")))
    for m in order:
        f.write(f"{m}\t{','.join(f'{v:.3f}' for v in cur[m])}\n")
    for b in blocks[: BASELINE_RUNS - 1]:
        f.write("#meta\t" + "\t".join(f"{k}={v}" for k, v in b.meta.items()) + "\n")
        for m, vals in b.metrics.items():
            f.write(f"{m}\t{','.join(f'{v:.3f}' for v in vals)}\n")

# --- 予算 (表示 + rel/較正器の宣言) ---------------------------------------------
budgets: dict[str, str] = {}
rel_metrics: set[str] = set()
calib_name = ""
try:
    with open(budget_file) as f:
        for row in f:
            parts = row.split()
            if not parts or parts[0].startswith("#"):
                continue
            if parts[0] == "calibrate":
                calib_name = parts[1] if len(parts) > 1 else ""
                continue
            budgets[parts[0]] = parts[1] + (" rel" if "rel" in parts[2:] else "")
            if "rel" in parts[2:]:
                rel_metrics.add(parts[0])
except OSError:
    pass

# --- ブロック単位の較正スケール (block → cur 環境への換算倍率) -------------------
cur_calib_p50 = statistics.median(cur[calib_name]) if calib_name and calib_name in cur else 0.0
block_scales: list[float] = []
for b in blocks:
    scale = 0.0
    if cur_calib_p50 > 0 and b.metrics.get(calib_name):
        b_cal = statistics.median(b.metrics[calib_name])
        if b_cal > 0:
            scale = cur_calib_p50 / b_cal
    block_scales.append(scale)

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

# --- metric ごとの baseline プール (rel はブロック単位で cur 環境へ換算) ----------
def pooled_baseline(m: str) -> tuple[list[float], bool]:
    pool: list[float] = []
    normalized = False
    for b, scale in zip(blocks, block_scales):
        vals = b.metrics.get(m)
        if not vals:
            continue
        if m in rel_metrics and scale > 0:
            pool.extend(v * scale for v in vals)
            normalized = True
        else:
            pool.extend(vals)
    return pool, normalized

# --- (b) Step Summary の比較テーブル -------------------------------------------
summary = os.environ.get("GITHUB_STEP_SUMMARY")
if summary:
    rows = [f"### {name} bench (n={max(len(v) for v in cur.values()) if cur else 0} runs, "
            "gate=min / 比較=直近 run プールの median + Mann-Whitney U p<0.05)"]
    if blocks:
        srcs = []
        for b in blocks:
            sha = b.meta.get("sha", "?")
            run_id, repo = b.meta.get("run", ""), b.meta.get("repo", "")
            if run_id and repo and "?" not in (run_id + repo):
                srcs.append(f"[`{sha}`](https://github.com/{repo}/actions/runs/{run_id})")
            else:
                srcs.append(f"`{sha}`")
        rows.append(f"baseline = 直近 {len(blocks)} run のプール (新しい順): " + ", ".join(srcs))
    if calib_name:
        scales = [s for s in block_scales if s > 0]
        if scales:
            rng = (f"×{scales[0]:.2f}" if len(scales) == 1
                   else f"×{min(scales):.2f}〜×{max(scales):.2f}")
            rows.append(f"⚖ = rel metric は較正器 {calib_name} の p50 比 ({rng}) で"
                        " 各 baseline run を cur 環境へ換算して判定")
        elif blocks:
            rows.append(f"較正器 {calib_name} の baseline 値が無いため正規化なし (環境差がそのまま出る)")
    rows += ["",
            "| metric | budget (ms) | base p50 | cur p50 | Δ | 判定 | cur min |",
            "|---|---:|---:|---:|---:|:---|---:|"]
    for m in order:
        c = cur[m]
        c_p50, c_min = statistics.median(c), min(c)
        pool, normalized = pooled_baseline(m)
        if not pool:
            rows.append(f"| {m} | {budgets.get(m, '-')} | - | {c_p50:.1f} | - | 🆕 初計測 | {c_min:.1f} |")
            continue
        b_p50 = statistics.median(pool)
        delta = (c_p50 - b_p50) / b_p50 * 100 if b_p50 else 0.0
        z = mwu_z(pool, c)
        if abs(z) >= 1.96 and abs(delta) >= 5:
            verdict = f"🔺 悪化 (|z|={abs(z):.1f})" if delta > 0 else f"✅ 改善 (|z|={abs(z):.1f})"
        else:
            verdict = "➖ 誤差圏"
        if normalized:
            verdict += " ⚖"
        rows.append(f"| {m} | {budgets.get(m, '-')} | {b_p50:.1f} | {c_p50:.1f} | "
                    f"{delta:+.1f}% | {verdict} | {c_min:.1f} |")
    with open(summary, "a") as f:
        f.write("\n".join(rows) + "\n\n")
PYEOF
