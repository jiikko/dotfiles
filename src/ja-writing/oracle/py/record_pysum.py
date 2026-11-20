"""組み込みの sum を、int・bool・float・C long に収まらない int・float に収まらない int・数でない値を混ぜた list に通し、JSON で返す driver
(internal/pyops の Sum の正解。internal/pyops/testdata/sum.json に書く: python3 -P -s -S -B oracle/py/record_pysum.py > internal/pyops/testdata/sum.json)。

標準入力は使わない。入力は決まった算法 (乱数は使わない) で作る。値の符号化は record_json.py と同じ: 整数は {"i": 16 進}、浮動小数点数は
{"f": IEEE 754 の 64 ビットを 16 桁の 16 進で}、bool と None と文字列はそのまま。結果は同じ符号化か、例外なら {"e": 例外の型}。
"""
import json
import math
import struct
import sys

POOL = [0, 1, -1, 3, 2, True, False, 2 ** 62, 2 ** 63 - 1, -(2 ** 63), -(2 ** 63) + 1, 2 ** 63, 2 ** 64, -(2 ** 64), 10 ** 25, 10 ** 400, -(10 ** 400),
        0.1, 0.2, 0.3, -0.1, 1e16, -1e16, 1.0, 0.0, -0.0, 1e308, -1e308, 1e-300, 5e-324, float("inf"), float("-inf"), 2.0 ** 53, 0.5, None, "a"]


class Lcg:
    def __init__(self, seed):
        self.x = seed

    def below(self, n):
        self.x = (self.x * 6364136223846793005 + 1442695040888963407) % (1 << 64)
        return (self.x >> 33) % n


def enc(v):
    if isinstance(v, bool) or v is None or isinstance(v, str):
        return v
    if isinstance(v, int):
        return {"i": hex(v)}
    return {"f": "%016x" % struct.unpack("<Q", struct.pack("<d", v))[0]}


def random_float(rng):
    bits = 0
    for _ in range(4):
        bits = (bits << 16) | rng.below(1 << 16)
    v = struct.unpack("<d", struct.pack("<Q", bits))[0]
    return 0.25 if math.isnan(v) else v


cases = [[], [0.1, 0.2, 0.3], [1e16, 3, 2], [1, 0.1, 0.2, 0.3], [0.1, 2 ** 64, -(2 ** 64), 0.2, 0.3], [2 ** 63 - 1, 1, -(2 ** 63), 0.1, 0.2, 0.3],
         [1e16, True, 1.0], [-0.0], [-0.0, -0.0], [0.5, -0.5, -0.0], [float("inf"), float("-inf")], [1e308, 1e308, -1e308], [0.1, 10 ** 400],
         [10 ** 400, 0.1], [0.1, 0.2, "a"], [1, None], [True, True, 3]]
rng = Lcg(1077)
for n in range(3000):
    length = 3 + rng.below(6)
    items = []
    for _ in range(length):
        pick = rng.below(len(POOL) + 6)
        if pick < len(POOL):
            v = POOL[pick]
            # 数でない値 (例外の道) は少なめに混ぜる
            if (v is None or isinstance(v, str)) and rng.below(4):
                v = 0.1
            items.append(v)
        elif pick < len(POOL) + 3:
            items.append(random_float(rng))
        else:
            items.append((rng.below(2001) - 1000) / 7)
    cases.append(items)

out = []
for items in cases:
    try:
        result = enc(sum(items))
    except Exception as error:  # noqa: BLE001 -- 例外の型だけを記録する
        result = {"e": type(error).__name__}
    out.append({"items": [enc(v) for v in items], "sum": result})
sys.stdout.write('{"python": %s,\n"cases": [\n' % json.dumps(sys.version.split()[0]))
sys.stdout.write(",\n".join(json.dumps(case, ensure_ascii=True, separators=(",", ":")) for case in out))
sys.stdout.write("\n]}\n")
