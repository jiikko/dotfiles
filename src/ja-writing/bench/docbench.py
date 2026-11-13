"""本家 (Python) と Go の lint を、実際の Markdown の集まりに通して、答えの一致と所要を比べる。

    python3 -I bench/docbench.py check <本家の scripts の dir> <Go の実行ファイル> <文書の根の dir> <一覧のファイル>
    python3 -I bench/docbench.py time  <本家の scripts の dir> <Go の実行ファイル> <文書の根の dir> <一覧のファイル> [-n <周の数>]

一覧のファイルは、根の dir からの相対パスを 1 行に 1 つ書く (git ls-files の出力など)。cwd は根の dir にして相対パスで渡す。
語の一覧 (.yomiyasu-words) は探させない (根の dir とその上に無いこと。あれば止まる)。

- check: 全ファイルを --json と text と --strict で両方に通し、stdout・stderr・終了コードが同じかを見る。違いは 1 件ごとに出し、rc 1 で終わる。
- time: 1 ファイルずつの起動込みの合計 (全ファイルを 1 回ずつ) と、全ファイルをつないだ 1 つの文書 (起動 1 回) を、周ごとに本家と Go の順を入れ替えて測る。
  本家の動かし方は pybench.sh と同じ固定の環境 (環境変数を空から作る・python3 -P -s -S -B -X utf8)。Go にも同じ環境変数を渡す。--version の所要 (起動の下限) も測る。
"""

import statistics
import subprocess
import sys
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path


def env_for(home: str) -> dict:
    py = sys.executable
    return {"PATH": f"{Path(py).parent}:/usr/bin:/bin", "HOME": home, "TZ": "UTC", "PYTHONHASHSEED": "0"}


def commands(scripts: str, gobin: str):
    py = [sys.executable, "-P", "-s", "-S", "-B", "-X", "utf8", str(Path(scripts) / "yomiyasu_lint.py")]
    return {"python": py, "go": [gobin, "lint"]}


def guard_no_words(root: Path) -> None:
    d = root.resolve()
    while True:
        for m in (".yomiyasu-words", ".yomiyasu"):
            if (d / m).exists():
                raise SystemExit(f"docbench: {d / m} がある。語の一覧を探させない (根の dir を別の場所に写す)")
        if d == d.parent:
            return
        d = d.parent


def run(cmd, args, root, home):
    p = subprocess.run(cmd + args, cwd=root, env=env_for(home), stdin=subprocess.DEVNULL, capture_output=True)
    return p.returncode, p.stdout, p.stderr


def check(scripts, gobin, root, files) -> int:
    cmds = commands(scripts, gobin)
    forms = [["--json"], [], ["--strict"]]
    bad = 0
    stats = {"files": len(files), "rc0": 0, "rc1": 0, "other": 0}
    with tempfile.TemporaryDirectory() as home:

        def one(f):
            out = []
            for form in forms:
                a = run(cmds["python"], form + [f], root, home)
                b = run(cmds["go"], form + [f], root, home)
                out.append((f, form, a, b))
            return out

        with ThreadPoolExecutor(max_workers=4) as ex:
            for res in ex.map(one, files):
                for f, form, a, b in res:
                    if form == ["--strict"]:
                        stats["rc0" if a[0] == 0 else "rc1" if a[0] == 1 else "other"] += 1
                    if a != b:
                        bad += 1
                        print(f"違い: {f} {' '.join(form)}  rc {a[0]} と {b[0]}  stdout {len(a[1])} と {len(b[1])} バイト  stderr {len(a[2])} と {len(b[2])} バイト")
    print(f"確かめた: {len(files)} ファイル x 3 形 = {len(files) * 3} 呼び出し、違い {bad} 件 (--strict の rc: {stats['rc0']} が 0・{stats['rc1']} が 1・その他 {stats['other']})")
    return 1 if bad else 0


def timed(cmd, args, root, home) -> float:
    start = time.perf_counter()
    p = subprocess.run(cmd + args, cwd=root, env=env_for(home), stdin=subprocess.DEVNULL, capture_output=True)
    el = time.perf_counter() - start
    if p.returncode not in (0, 1):
        raise SystemExit(f"docbench: {cmd[-2:]} {args} が rc {p.returncode}: {p.stderr[:200]!r}")
    return el


def bench(scripts, gobin, root, files, rounds) -> int:
    cmds = commands(scripts, gobin)
    names = ["python", "go"]
    big = root / "docbench-all.md"
    with tempfile.TemporaryDirectory() as home:
        big.write_bytes(b"\n\n".join((root / f).read_bytes() for f in files))
        sizes = sum((root / f).stat().st_size for f in files)
        rel_big = big.name
        res = {n: {"version": [], "each_total": [], "big": [], "each": {f: [] for f in files}} for n in names}
        for r in range(rounds):
            order = names if r % 2 == 0 else names[::-1]
            for n in order:
                c = cmds[n]
                res[n]["version"].append(timed(c, ["--version"], root, home))
                total = 0.0
                for f in files:
                    t = timed(c, [f], root, home)
                    res[n]["each"][f].append(t)
                    total += t
                res[n]["each_total"].append(total)
                res[n]["big"].append(timed(c, [rel_big], root, home))
            print(f"周 {r + 1}/{rounds} 済み", file=sys.stderr)
    stat_size = big.stat().st_size
    big.unlink()
    def fmt(xs):
        return f"中央値 {statistics.median(xs):.3f} 秒 (最小 {min(xs):.3f}・最大 {max(xs):.3f})"
    print(f"対象 {len(files)} ファイル・合計 {sizes} バイト ({sizes / 1024:.0f} KB)・全部をつないだ文書 {stat_size} バイト・{rounds} 周 (周ごとに本家と Go の順を入れ替え)")
    for n in names:
        print(f"[{n}] --version              : {fmt(res[n]['version'])}")
        print(f"[{n}] 1 ファイルずつの合計   : {fmt(res[n]['each_total'])}  (1 ファイルあたりの中央値の平均 {statistics.mean(statistics.median(v) for v in res[n]['each'].values()) * 1000:.0f} ms)")
        print(f"[{n}] つないだ 1 文書 (1 回)  : {fmt(res[n]['big'])}")
    for key, label in (("each_total", "1 ファイルずつの合計"), ("big", "つないだ 1 文書")):
        a, b = statistics.median(res["python"][key]), statistics.median(res["go"][key])
        print(f"{label}: 本家 / Go = {a / b:.1f} 倍")
    return 0


def main() -> int:
    args = sys.argv[1:]
    rounds = 7
    if "-n" in args:
        i = args.index("-n")
        rounds = int(args[i + 1])
        del args[i : i + 2]
    if len(args) != 5 or args[0] not in ("check", "time"):
        raise SystemExit(__doc__)
    mode, scripts, gobin, root, listing = args
    root = Path(root).resolve()
    guard_no_words(root)
    files = [l for l in Path(listing).read_text(encoding="utf-8").splitlines() if l]
    if not files:
        raise SystemExit("docbench: 一覧が空")
    return check(scripts, gobin, root, files) if mode == "check" else bench(scripts, gobin, root, files, rounds)


if __name__ == "__main__":
    sys.exit(main())
