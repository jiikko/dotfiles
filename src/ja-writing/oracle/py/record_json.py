"""json.dumps / json.loads / round の振る舞いを、決まった入力の列に通して JSON で返す driver (internal/pyjson の正解)。

標準入力は使わない。入力は全部この driver が決まった算法 (乱数は使わない) で作るので、同じ Python なら同じ記録になる。
値は壊れないように次の形に符号化して返す (Go 側の読み手は internal/pyjson の golden_test.go)。
  null / true / false                そのまま
  文字列                             JSON の文字列 (サロゲートを含むものは {"ws": [cp, ...]})
  整数                               {"i": "0x1f"} (16 進。桁数の上限に当たらないように 10 進を使わない)
  浮動小数点数                       {"f": "<IEEE 754 の 64 ビットを 16 桁の 16 進で>"}
  list / tuple                       配列
  dict                               {"o": [[キー, 値], ...]} (挿入順)
サロゲートの範囲の文字を全部並べるような大きな出力は、本文でなく {"digest": SHA-256, "len": 文字数} で返す。
"""
import hashlib
import json
import math
import struct
import sys

# dumps の組み合わせは本家が使うものに揃える (lint: indent=2 / diff・check: indent=1 / check: sort_keys+separators / 指定なし)。
OPTION_SETS = [
    {},
    {"indent": 1},
    {"indent": 2},
    {"sort_keys": True, "separators": [",", ":"]},
    {"sort_keys": True},
    {"indent": 2, "sort_keys": True},
    {"indent": 0},
    {"indent": 4},
]


class Lcg:
    def __init__(self, seed):
        self.x = seed

    def next(self):
        self.x = (self.x * 6364136223846793005 + 1442695040888963407) % (1 << 64)
        return self.x >> 11

    def below(self, n):
        return self.next() % n

    def bits(self):
        return (self.next() << 11 | self.next() & 0x7FF) & ((1 << 64) - 1)


def float_bits(v):
    return "%016x" % struct.unpack(">Q", struct.pack(">d", v))[0]


def has_surrogate(s):
    return any(0xD800 <= ord(c) <= 0xDFFF for c in s)


def enc_str(s):
    return {"ws": [ord(c) for c in s]} if has_surrogate(s) else s


def enc(v):
    if v is None or v is True or v is False:
        return v
    t = type(v)
    if t is int:
        return {"i": hex(v)}
    if t is float:
        return {"f": float_bits(v)}
    if t is str:
        return enc_str(v)
    if t in (list, tuple):
        return [enc(x) for x in v]
    if t is dict:
        return {"o": [[enc_str(k), enc(x)] for k, x in v.items()]}
    raise TypeError("符号化できない型: " + t.__name__)


def error_of(exc):
    out = {"type": type(exc).__name__, "msg": str(exc)}
    if isinstance(exc, json.JSONDecodeError):
        out.update({"jmsg": exc.msg, "pos": exc.pos, "line": exc.lineno, "col": exc.colno})
    return out


def dumps_case(value, opts, spec=None):
    kwargs = dict(opts)
    if "separators" in kwargs:
        kwargs["separators"] = tuple(kwargs["separators"])
    rec = {"value": spec if spec is not None else enc(value), "opts": opts}
    try:
        text = json.dumps(value, ensure_ascii=True, **kwargs)
    except Exception as exc:  # noqa: BLE001 - 例外も記録する
        rec["error"] = error_of(exc)
        return rec
    if spec is not None:
        rec["digest"] = hashlib.sha256(text.encode("ascii")).hexdigest()
        rec["len"] = len(text)
    else:
        rec["out"] = text
    return rec


def float_values():
    rng = Lcg(20261112)
    vals = [0.0, -0.0, 1.0, -1.0, 0.5, 0.1, 0.2 + 0.1, 1 / 3, 2 / 3, 100.0, 1e15, 1e16, 1e17, 123456789012345678.0, 9007199254740992.0,
            9007199254740993.0, 1e21, 1e22, 1e23, 1.7976931348623157e308, 5e-324, 2.2250738585072014e-308, 2.225073858507201e-308,
            1e-4, 1e-5, 1e-7, 0.001, 123456789.123456789, 3.14159, 2.675, 4.35, 0.07, 0.3, 0.6, 1e100, 1.5e300, 12345.678e-10,
            float("inf"), float("-inf"), float("nan"), 33.33, 66.67, 0.333, 14.29, 7.5, 0.15, 99.99, 100.0 / 3, 5.0, 0.0001, 0.00001234]
    for i in range(1, 41):
        for j in (3, 7, 9, 11, 13, 60, 97):
            vals.append(i / j)
            vals.append(i * 100 / j)
    for _ in range(2500):
        bits = rng.bits()
        v = struct.unpack(">d", struct.pack(">Q", bits))[0]
        if not math.isnan(v):
            vals.append(v)
    for _ in range(500):
        vals.append(rng.below(10 ** 6) / 10 ** rng.below(8))
    return vals


def string_values():
    vals = ["", "a", "abc", '"', "\\", "/", "\n", "\r", "\t", "\b", "\f", "\x7f", "\x80", "\xff", "é", "日本語", "\u2028\u2029",
            "\ud7ff", "\ue000", "\uffff", "\U00010000", "\U0001f600", "\U0010ffff", "\ud800", "\udc00", "\udfff",
            "\ud83d\ude00", "\ude00\ud83d", "\ud83d", "a\ud83db", "<>&'", "\u0000", "\x1f", " ", "~", "\u00a0", "\ufeff", "\u3000",
            "😀" * 3, "e\u0301", "\u212b", "line1\nline2\r\nline3"]
    vals += [chr(c) for c in range(0x20)]
    return vals


def build_dumps():
    cases = []
    for v in [None, True, False, 0, 1, -1, 255, 2 ** 31, 2 ** 53, 2 ** 63, -(2 ** 63), 2 ** 64, -(2 ** 100), 10 ** 30, 10 ** 4299, 10 ** 4300 - 1, 10 ** 4300]:
        cases.append(dumps_case(v, {}))
    for v in float_values():
        cases.append(dumps_case(v, {}))
    for v in string_values():
        cases.append(dumps_case(v, {}))
    for v in string_values()[:20]:
        cases.append(dumps_case(v, {"indent": 1}))
    # すべてのコードポイントを 4096 字ずつの文字列にして、出力の SHA-256 で比べる (サロゲートも入れる)
    for lo in range(0, 0x110000, 4096):
        hi = min(lo + 4096, 0x110000)
        cases.append(dumps_case("".join(chr(c) for c in range(lo, hi)), {}, {"r": [lo, hi]}))
    # 容器
    containers = [
        [], {}, [[]], [{}], {"a": []}, {"a": {}}, [1, 2, 3], {"b": 1, "a": 2}, {"z": 1, "Z": 2, "é": 3, "\U0001f600": 4, "\uffff": 5, "\ue000": 6, "\u00e9": 7, "": 8},
        {"nested": {"x": [1, [2, [3, {}]], {"y": None}]}}, [None, True, False, 0, 1.5, "s", [], {}],
        {"日本語": "値", "emoji": "😀", "k\n": "v\t"},
        {"format": "yomiyasu-lint", "format_version": 1, "issues": [{"line": 3, "rule": "R1", "message": "文を短くしてください。", "snippet": "これは「長い」文です。"}],
         "metrics": {"list_ratio": round(0.3333333, 3), "bold_count": 2, "bold_per_1000": round(12.345678, 2)}},
        {"checks": [{"name": "lint", "summary": {"errors": 0, "warnings": 2}}, {"name": "diff", "summary": None}], "result": "ok", "ratio": 0.5},
        [[[[[[[[1]]]]]]]], {"a": {"b": {"c": {"d": {}}}}},
        (1, 2, (3, 4)), {"k": (1,)},
    ]
    for c in containers:
        for opts in OPTION_SETS:
            cases.append(dumps_case(c, opts))
    # 重複して見える鍵・数の並び
    cases.append(dumps_case({"a": 1, "b": 2, "a2": 3, "B": 4, "10": 5, "9": 6}, {"sort_keys": True, "indent": 1}))
    cases.append(dumps_case(list(range(300)), {"indent": 1}))
    return cases


ROUND_NS = [-4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 10, 15, 17, 20, 100, 300, 323, 324, 400, -300, -308, -309, -400]


def round_value(x, n):
    return round(x, n)


def build_round():
    rng = Lcg(77)
    xs = [0.5, 1.5, 2.5, -0.5, -1.5, 0.125, 0.375, 2.675, 1.005, 0.285, 1.45, 4.35, 0.1, 0.01, 0.001, 123.456, 1e15, 1.5e15, 5e-324, 1e-300,
          1.7976931348623157e308, 8.5e307, 0.0, -0.0, 1e22, 1e23, 12345678.9, 0.45, 0.55, 33.333333, 66.666667, 12.5, 0.00049, 0.0005, 0.00051,
          float("inf"), float("-inf"), float("nan"), 5.0, 25.0, 35.0, 150.0, 1234.5678, 0.3333333333333333]
    for _ in range(300):
        xs.append(rng.below(10 ** 7) / 10 ** (1 + rng.below(6)))
    for _ in range(150):
        v = struct.unpack(">d", struct.pack(">Q", rng.bits()))[0]
        if math.isfinite(v):
            xs.append(v)
    out = []
    for x in xs:
        for n in ROUND_NS:
            if len(out) % 7 and not (-4 <= n <= 6) and abs(x) > 1e-3 and abs(x) < 1e10:
                continue
            rec = {"x": float_bits(x), "n": n}
            try:
                rec["result"] = {"f": float_bits(round(x, n))}
            except Exception as exc:  # noqa: BLE001
                rec["error"] = error_of(exc)
            out.append(rec)
    ints = []
    for x in xs + [0.5, 1.5, 2.5, 3.5, -0.5, -2.5, 0.49999999999999994, 4503599627370497.5, 1e300]:
        rec = {"x": float_bits(x)}
        try:
            rec["result"] = {"i": hex(round(x))}
        except Exception as exc:  # noqa: BLE001
            rec["error"] = error_of(exc)
        ints.append(rec)
    return out, ints


def summarize(value):
    """深い入れ子は再帰で符号化できないので、反復で数えた形 (節点の数・最大の深さ・葉の種類ごとの数) で返す。"""
    nodes = depth = 0
    leaves = {}
    stack = [(value, 1)]
    while stack:
        v, d = stack.pop()
        nodes += 1
        depth = max(depth, d)
        if isinstance(v, list):
            stack.extend((x, d + 1) for x in v)
        elif isinstance(v, dict):
            stack.extend((x, d + 1) for x in v.values())
        else:
            kind = type(v).__name__
            leaves[kind] = leaves.get(kind, 0) + 1
    return {"nodes": nodes, "depth": depth, "leaves": dict(sorted(leaves.items()))}


def loads_case(spec, raw):
    rec = dict(spec)
    try:
        value = json.loads(raw.decode("utf-8"))
    except (json.JSONDecodeError, ValueError, UnicodeError) as exc:
        rec["error"] = error_of(exc)
    except RecursionError as exc:
        rec["error"] = error_of(exc)
    else:
        rec["summary" if "rep" in spec else "v"] = summarize(value) if "rep" in spec else enc(value)
    return rec


def text_spec(text):
    return {"text": text}


def number_texts():
    rng = Lcg(4242)
    texts = ["0", "-0", "1", "-1", "10", "123456789012345678901234567890", "-123456789012345678901234567890", "0.0", "-0.0", "1.0", "1.5", "0.1",
             "1e5", "1E5", "1e+5", "1e-5", "1.5e3", "1.5E-3", "1e0", "1e00", "-1e5", "1e999", "-1e999", "1e-999", "1e-400", "5e-324", "2e-324", "2.4703282292062327e-324",
             "2.4703282292062328e-324", "1.7976931348623157e308", "1.7976931348623159e308", "1.797693134862315807e308", "9007199254740993", "9007199254740993.0",
             "0.1000000000000000055511151231257827", "0.30000000000000004", "123456789.123456789e-5", "4.35", "2.675", "100000000000000000000", "1e22", "1e23",
             "00", "01", "-01", "+1", "1.", ".5", "-", "--1", "1e", "1e+", "1.e5", "0x10", "1_000", "١٢٣", "1.5.2", "1e5.5", "NaN", "Infinity", "-Infinity", "-NaN", "nan", "inf",
             "+Infinity", "Infinit", "-Infinit", "NaN1", "nul", "tru", "fals", "null", "true", "false", "nulll", "True", "None", "- 1", "1 2", "[1 2]", "1e1000000000000", "1" * 4300, "1" * 4301, "-" + "1" * 4300, "-" + "1" * 4301]
    for _ in range(600):
        parts = []
        if rng.below(4) == 0:
            parts.append("-")
        parts.append(str(rng.below(10 ** (1 + rng.below(18)))))
        if rng.below(2):
            parts.append("." + str(rng.below(10 ** (1 + rng.below(20)))).zfill(1 + rng.below(20)))
        if rng.below(3) == 0:
            parts.append(rng.below(2) * "e" or "E")
            parts.append(("+", "-", "")[rng.below(3)])
            parts.append(str(rng.below(400)))
        texts.append("".join(parts))
    for _ in range(300):
        v = struct.unpack(">d", struct.pack(">Q", rng.bits()))[0]
        if math.isfinite(v):
            texts.append(repr(v))
            texts.append("%.17g" % v)
            texts.append("%.20e" % v)
    return texts


def string_texts():
    texts = ['""', '"a"', '"\\n"', '"\\u0041"', '"\\u00e9"', '"\\ud83d\\ude00"', '"\\ud83d"', '"\\ude00"', '"\\ud83d\\u0041"', '"\\ude00\\ud83d"', '"\\ud83d\\ud83d\\ude00"',
             '"\\ud83d\\ude00\\ud83d\\ude00"', '"\\ud800\\udc00"', '"\\uD83D\\uDE00"', '"\\ud83d\\u00"', '"\\ud83d\\uzzzz"', '"\\ud83d\\ude0"', '"\\ud83d\\ude0z"', '"\\u12"', '"\\u12"x', '"\\u123"',
             '"\\u123z"', '"\\uzzzz"', '"\\u"', '"\\', '"\\x"', '"\\/"', '"\\b\\f\\n\\r\\t\\\\\\""', '"\\a"', '"\\\'"', '"\\0"', '"\\u0000"', "\"\x00\"", '"\x1f"', '"\x7f"', '"\x80"',
             '"\n"', '"\t"', '"\r"', '"a', '"', '"abc\\', '"abc\\"', "'a'", '"é"', '"😀"', '"\u2028"', '"\ufeff"', '"\\ud83d" ', '[ "\\ud83d" ]', '"\\u0041', '"\\ud83d\\ude00', '"\\ud83d', '["\\u0041', '"\\u004', '"\\u00', '"\\u', '"\\ud83d\\u', '"ab\\u0041', '"\\ud83dx', '"\\ud83d\\', '"\\ud83d\\n', '{"a":"\\u0041', '{"\\u0041', '"\\ude00\\ud83d', '"\\u0041\\u0042', '"\\u0000\\u001f\\u007f\\u0080\\u00ff\\u0100\\uffff"']
    for c in range(0x20):
        texts.append('"' + chr(c) + '"')
    return texts


def structure_texts():
    texts = ["", " ", "\n", "\t", " \t\n\r", "\x0b", "\x0c", "\u00a0", "\u3000", "\ufeff", "\ufeff1", "1\ufeff", " 1 ", "\n[\n]\n", "[]", "{}", "[ ]", "{ }", "[1]", "[1,2]", "[1,]", "[,1]", "[,]", "[1,,2]", "[1 2]",
             "[1", "[1,", "[", "]", "{", "}", '{"a"', '{"a":', '{"a":1', '{"a":1,', '{"a":1,}', '{,}', '{"a":}', '{"a" 1}', '{"a":1 "b":2}', '{a:1}', "{'a':1}", '{1:2}', '{"a":1,"b"}', '{"a":1,"a":2}',
             '{"a":1,"b":2,"a":3}', '{"":1}', '{"\\u0061":1,"a":2}', '{"a":{"a":1},"a":[]}', '{"b":1,"a":2}', '{"é":1}', '{"\\ud83d\\ude00":1}', '{"\\ud83d":1,"\\ud83d":2}',
             '[1] x', '[1]x', '1 2', '{} {}', '[] []', "// c\n1", "/* c */1", "1 // c", "#c\n1", '"a" "b"', "NaN", "[NaN]", "[Infinity,-Infinity]", "{\"a\":NaN}", "[NaN,Infinity]x",
             "nul", "[nul]", "[true,false,null]", "[tru]", "[truee]", '{"a":true}', '{"a":nulll}', "[1.5e3,-0,-0.0,0e0]", "[01]", "[1.]", "[.5]", "[-]", "[+1]", "[-a]",
             '{"a":[{"b":[1,2,{"c":null}]}]}', '{"session_id":"abc","hook_event_name":"UserPromptSubmit","cwd":"/tmp"}', '{"session_id":"\\ud83d","cwd":"/"}', '{"session_id":" "}', '[[[[[[[[[[1]]]]]]]]]]',
             '{"a":1}}', '[1]]', '[[1]', '{"a":[1,2}', '[{"a":1]', '"\\u0041\\u3042"', '{"x":"\\u3042"}', "[\"a\",\n  \"b\"]", "{\n  \"a\":\n  1\n}", "\n\n[1,\n 2,\n x]", "\r\n[1,\r\n 2,\r\n x]", "  \n\n  ", '{"a":"\n"}',
             "[" * 10 + "]" * 10, "[" * 10, '{"a":' * 5 + "1" + "}" * 5, '{"a":' * 5 + "1" + "}" * 4, "[1,\u3042]", "あ", '"あ', "[\"a\"\n\"b\"]"]
    return texts


def invalid_bytes():
    return ["ff", "c0af", "eda080", "edbfbf", "e2", "e282", "e282ac", "f09f9880", "f09f98", "f4908080", "f8888080", "c3", "c328", "80", "bf", "e0 80 80", "e08080", "efbbbf31", "efbbbf", "3100", "00", "5b00", "22e282", "22eda080 22", "7b2261223a22ff227d"]


def build_loads():
    out = []
    for t in number_texts() + string_texts() + structure_texts():
        raw = t.encode("utf-8", "surrogatepass") if has_surrogate(t) else t.encode("utf-8")
        spec = {"text": t} if not has_surrogate(t) else {"hex": raw.hex()}
        out.append(loads_case(spec, raw))
    for h in invalid_bytes():
        h = h.replace(" ", "")
        out.append(loads_case({"hex": h}, bytes.fromhex(h)))
    # 大きな入力・深い入れ子 (繰り返しの指定で記録する)
    reps = [
        [["[", 100], ["]", 100]], [["[", 1000], ["]", 1000]], [["[", 10000], ["]", 10000]], [["[", 100000], ["]", 100000]],
        [["[", 100000]], [['{"a":', 10000], ["1", 1], ["}", 10000]], [['{"a":', 100000], ["1", 1], ["}", 100000]],
        [["[1,", 100000], ["1]", 1]], [["[", 1000000], ["]", 1000000]], [["[", 1000000]], [['{"a":', 1000000], ["1", 1], ["}", 1000000]],
        [["[{", 40000], ["}]" * 1, 1]], [["1", 5000]], [["0.", 1], ["1", 5000]], [['"', 1], ["a", 100000], ['"', 1]], [['"', 1], ["\\u00e9", 100000], ['"', 1]],
        [["[", 1], ['"x",', 50000], ['"x"]', 1]], [[" ", 100000], ["1", 1], [" ", 100000]],
    ]
    for rep in reps:
        raw = "".join(u * n for u, n in rep).encode("utf-8")
        out.append(loads_case({"rep": rep}, raw))
    return out


def main():
    dumps = build_dumps()
    rounds, round_ints = build_round()
    json.dump({"dumps": dumps, "loads": build_loads(), "round": rounds, "round_int": round_ints}, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
