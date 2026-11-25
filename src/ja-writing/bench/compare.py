"""pybench.sh -g の runs.tsv で、2 つの Go のバイナリをケースごとに比べる (issue 1079 の採否)。

    python3 -I compare.py <runs.tsv> <after の名前> <before の名前> [<揺れを見る名前> <その相手の名前>]

列: ケース・after と before の中央値 (秒)・比 (after / before。1 より小さければ速い)・after が速かった周の数 / 周の数・
揺れ (同じバイナリを 2 つの名前で流した組の中央値の比。4 つ目と 5 つ目の名前を渡したときだけ)・判定。
判定は、比が揺れの帯 (1 ± |揺れ - 1|) の外で、かつ過半数の周で同じ向きのときだけ「速い」「遅い」、ほかは「-」。
起動を引いた値は使わない (準備を起動の側へ移すと、全体は変わらないのに速く見えるため)。
"""

import statistics
import sys


def main() -> None:
    if len(sys.argv) not in (4, 6):
        raise SystemExit("使い方: python3 -I compare.py <runs.tsv> <after> <before> [<揺れ a> <揺れ b>]")
    path, after, before = sys.argv[1:4]
    noise = sys.argv[4:6]
    times: dict[str, dict[str, float]] = {}
    with open(path, encoding="utf-8") as f:
        next(f)
        for line in f:
            rnd, case = line.split("\t")[:2]
            times.setdefault(case, {})[rnd] = float(line.split("\t")[3])
    bases = sorted({c.partition("@")[0] for c in times if c.endswith("@" + after)})
    if not bases:
        raise SystemExit(f"compare: @{after} のケースが runs.tsv に無い")
    print("case\tafter_s\tbefore_s\tratio\tafter_faster\tnoise\tverdict")
    for base in bases:
        a, b = times.get(f"{base}@{after}"), times.get(f"{base}@{before}")
        if not a or not b:
            raise SystemExit(f"compare: {base} の @{after} か @{before} が無い")
        ratio = statistics.median(a.values()) / statistics.median(b.values())
        rounds = sorted(set(a) & set(b))
        faster = sum(1 for r in rounds if a[r] < b[r])
        band = 0.0
        noise_col = "-"
        if noise:
            x, y = times.get(f"{base}@{noise[0]}"), times.get(f"{base}@{noise[1]}")
            if not x or not y:
                raise SystemExit(f"compare: {base} の揺れを見る組が無い")
            n = statistics.median(x.values()) / statistics.median(y.values())
            band = abs(n - 1)
            noise_col = f"{n:.3f}"
        verdict = "-"
        if ratio < 1 - band and faster * 2 > len(rounds):
            verdict = "速い"
        elif ratio > 1 + band and faster * 2 < len(rounds):
            verdict = "遅い"
        print(f"{base}\t{statistics.median(a.values()):.4f}\t{statistics.median(b.values()):.4f}\t{ratio:.3f}\t"
              f"{faster}/{len(rounds)}\t{noise_col}\t{verdict}")


if __name__ == "__main__":
    main()
