"""pybench.sh の runs.tsv をケースごとにまとめる。

    python3 -I summarize.py <runs.tsv> <manifest.tsv>

列: ケース・run の数・入力のバイト数・出力のバイト数・中央値・最小・最大 (秒)・処理の時間 (中央値から、同じ道具の
--version の中央値を引いたもの)。--version は interpreter の起動と本家の module の import までで終わるので、引いた残りには
入力に依らない準備 (lint が語の一覧のファイルを読むなど、--version の判定より後にある準備) も入る。逆に、import の中で
済ませる準備は起動の側に入る。どちらも「入力の処理だけの時間」ではない。

rc が 0 でない run があるか、同じケースの出力のバイト数が周によって違えば、何も出さずに rc 1 で終わる
(失敗した run や、別の答えを出した run の混ざった中央値を出さない)。
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
    times, outs, order, bad = {}, {}, [], []
    with open(sys.argv[1], encoding="utf-8") as f:
        next(f)
        for line in f:
            rnd, case, rc, secs, out_bytes = line.rstrip("\n").split("\t")[:5]
            if case not in times:
                order.append(case)
                times[case], outs[case] = [], set()
            times[case].append(float(secs))
            outs[case].add(out_bytes)
            if rc != "0":
                bad.append(f"周 {rnd} の {case} が rc {rc}")
    if not order:
        raise SystemExit("summarize: runs.tsv に run が 1 つも無い")
    # 周ごとに順をかき混ぜるので、出てきた順ではなく、起動だけのケースを先にして名前の順に並べる
    order.sort(key=lambda c: (not c.startswith("startup-"), c))
    bad += [f"{c} の出力のバイト数が周によって違う ({', '.join(sorted(outs[c]))})" for c in order if len(outs[c]) > 1]
    if bad:
        print("summarize: " + " / ".join(bad), file=sys.stderr)
        raise SystemExit(1)

    def input_bytes(case: str) -> str:
        kind, _, rest = case.partition("-")
        if kind == "lint":
            return str(sizes[rest])
        if kind in ("diff", "check"):
            return str(sizes[rest + "-before"] + sizes[rest + "-after"])
        return "-"

    print("case\truns\tinput_bytes\tstdout_bytes\tmedian_s\tmin_s\tmax_s\tminus_startup_s")
    for case in order:
        t = times[case]
        med = statistics.median(t)
        kind = case.partition("-")[0]
        base = times.get(f"startup-{kind}")
        minus = f"{med - statistics.median(base):.4f}" if base and kind != "startup" else "-"
        print(
            f"{case}\t{len(t)}\t{input_bytes(case)}\t{next(iter(outs[case]))}\t"
            f"{med:.4f}\t{min(t):.4f}\t{max(t):.4f}\t{minus}"
        )


if __name__ == "__main__":
    main()
