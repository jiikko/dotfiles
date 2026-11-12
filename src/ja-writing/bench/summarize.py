"""pybench.sh の runs.tsv をケースごとにまとめる。

    python3 -I summarize.py <runs.tsv> <manifest.tsv>

列: ケース・run の数・rc の種類・入力のバイト数・中央値・最小・最大 (秒)・処理の時間 (中央値から、同じ道具の
--version の中央値を引いたもの)。--version は interpreter の起動と本家の module の import までを測るので、
引いた残りは「入力を読んでから出力を書き終えるまで」の近似になる (import の中で入力に依らない準備をしていれば、それは起動の側に入る)。
ケースの rc が周によって違うときは rc の列に全部を並べる (測った run の中に失敗が混ざっていないかを読むため)。
"""

import statistics
import sys


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("使い方: python3 -I summarize.py <runs.tsv> <manifest.tsv>")
    sizes = {}
    with open(sys.argv[2], encoding="utf-8") as f:
        for line in f:
            name, size, _ = line.rstrip("\n").split("\t")
            sizes[name.removesuffix(".md")] = int(size)
    times, rcs, order = {}, {}, []
    with open(sys.argv[1], encoding="utf-8") as f:
        next(f)
        for line in f:
            _, case, rc, secs, _, _ = line.rstrip("\n").split("\t")
            if case not in times:
                order.append(case)
                times[case], rcs[case] = [], set()
            times[case].append(float(secs))
            rcs[case].add(rc)
    if not order:
        raise SystemExit("summarize: runs.tsv に run が 1 つも無い")

    def input_bytes(case: str) -> str:
        kind, _, rest = case.partition("-")
        if kind == "lint":
            return str(sizes[rest])
        if kind in ("diff", "check"):
            return str(sizes[rest + "-before"] + sizes[rest + "-after"])
        return "-"

    print("case\truns\trc\tinput_bytes\tmedian_s\tmin_s\tmax_s\tminus_startup_s")
    for case in order:
        t = times[case]
        med = statistics.median(t)
        kind = case.partition("-")[0]
        base = times.get(f"startup-{kind}")
        minus = f"{med - statistics.median(base):.4f}" if base and kind != "startup" else "-"
        print(
            f"{case}\t{len(t)}\t{','.join(sorted(rcs[case]))}\t{input_bytes(case)}\t"
            f"{med:.4f}\t{min(t):.4f}\t{max(t):.4f}\t{minus}"
        )


if __name__ == "__main__":
    main()
