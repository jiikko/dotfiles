"""本家の check_pattern と search の所要を測る driver (記録を作る道具ではない。1074 の最悪の組の測定用)。

標準入力: {"scripts_dir": "...", "patterns": [...], "prefix": "a", "repeat": 100000, "suffix": "d"}
標準出力: {"inner": driver 全体の秒, "import": module を import した秒, "patterns": [{"check": 秒, "search": 秒, "cost": 試す数, "matched": bool}, ...]}
行は prefix * repeat + suffix (10 万字の行)。search は先頭から全部を探す。時間は time.perf_counter で、起動の時間 (inner の外) は呼び出し側が壁時計の差で出す。
"""
import json
import sys
import time

begin = time.perf_counter()
plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
start = time.perf_counter()
import markdown_visibility as mv  # noqa: E402

import_seconds = time.perf_counter() - start
line = plan["prefix"] * plan["repeat"] + plan["suffix"]
rows = []
for pattern in plan["patterns"]:
    start = time.perf_counter()
    compiled, _, cost = mv.check_pattern(pattern)
    check_seconds = time.perf_counter() - start
    start = time.perf_counter()
    matched = compiled.search(line) is not None
    search_seconds = time.perf_counter() - start
    rows.append({"check": check_seconds, "search": search_seconds, "cost": cost, "matched": matched})
json.dump({"inner": time.perf_counter() - begin, "import": import_seconds, "patterns": rows}, sys.stdout)
