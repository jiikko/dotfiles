"""str の splitlines / strip / 分類 / len を、区切り・空白・数字・文字を混ぜた文字列に通して JSON で返す driver (internal/pystr の正解)。

標準入力は使わない。入力は決まった算法 (乱数は使わない) で作る。文字列はサロゲートを含むなら {"ws": [cp, ...]}、含まなければ JSON の文字列。
"""
import json
import sys

MAX = 0x110000
SEPARATORS = ["\n", "\r", "\r\n", "\x0b", "\x0c", "\x1c", "\x1d", "\x1e", "\x1f", "\x85", chr(0x2028), chr(0x2029)]


class Lcg:
    def __init__(self, seed):
        self.x = seed

    def below(self, n):
        self.x = (self.x * 6364136223846793005 + 1442695040888963407) % (1 << 64)
        return (self.x >> 33) % n


def has_surrogate(s):
    return any(0xD800 <= ord(c) <= 0xDFFF for c in s)


def enc(s):
    return {"ws": [ord(c) for c in s]} if has_surrogate(s) else s


def sample(pred, count, step):
    out = []
    for cp in range(MAX):
        if pred(chr(cp)) and cp % step == 0:
            out.append(chr(cp))
        if len(out) >= count:
            break
    return out


def alphabet():
    spaces = [chr(c) for c in range(MAX) if chr(c).isspace()]
    first = lambda pred, n, skip=1: [chr(c) for c in range(MAX) if pred(chr(c))][::skip][:n]  # noqa: E731
    letters = first(lambda c: c.isalpha(), 40, 97)
    decimals = first(lambda c: c.isdecimal(), 24, 3)
    digits = first(lambda c: c.isdigit() and not c.isdecimal(), 16, 5)
    numerics = first(lambda c: c.isnumeric() and not c.isdigit(), 20, 11)
    others = first(lambda c: not c.isalnum() and not c.isspace(), 30, 401)
    # Unicode 16 / 17 で増えた字 (本家の Python が最新の表を持つことの確認を兼ねる)
    newer = [chr(c) for c in (0x1E5F1, 0x16D70, 0x11BF1, 0x1CCF0, 0x10D40, 0x1E6C0)]
    return (spaces + letters + decimals + digits + numerics + others + newer + [s for s in SEPARATORS if len(s) == 1]
            + ["x", "y", "-", "#", "。", "\u3000", " ", "\t", "\ud800", "\udfff"])


def record(s, chars):
    out = {
        "s": enc(s),
        "len": len(s),
        "lines": [enc(x) for x in s.splitlines()],
        "lines_keep": [enc(x) for x in s.splitlines(True)],
        "strip": enc(s.strip()),
        "lstrip": enc(s.lstrip()),
        "rstrip": enc(s.rstrip()),
        "isspace": s.isspace(),
        "isalpha": s.isalpha(),
        "isalnum": s.isalnum(),
        "isdecimal": s.isdecimal(),
        "isdigit": s.isdigit(),
        "isnumeric": s.isnumeric(),
    }
    if chars is not None:
        out["chars"] = enc(chars)
        out["strip_chars"] = enc(s.strip(chars))
        out["lstrip_chars"] = enc(s.lstrip(chars))
        out["rstrip_chars"] = enc(s.rstrip(chars))
    return out


def float_hex(s):
    try:
        v = float(s)
    except ValueError:
        return None
    return str(v) if v != v or v in (float("inf"), float("-inf")) else v.hex()


def float_cases():
    """float(str) が受ける形と受けない形 (空白・符号・数字の間の _・指数・inf と nan・アラビア・インドと全角の数字)。"""
    fixed = ["", " ", "1", "-1", "+1", "--1", "+-1", "1.", ".1", ".", "1.5", "1e5", "1E5", "1e+5", "1e-5", "1e", "e5", ".e5", "1.e5", "1_0", "1__0", "_1", "1_", "1_.5", "1._5", "1e_5", "1_e5", "1e1_0", "0_0",
             "inf", "Inf", "INF", "-inf", "+inf", "infinity", "Infinity", "-INFINITY", "infinit", "infinityx", "nan", "NaN", "-nan", "+nan", "nan_", "n a n", "0x10", "0b1", "1j", "1d5", "1f", "0.1", "0.30000000000000004", "1e999", "-1e999", "1e-999", "5e-324",
             "2.2250738585072014e-308", "1.7976931348623157e308", "1.7976931348623159e308", "9007199254740993", "123456789012345678901234567890", "٣", "٣.٥", "١٢٣", "１２３", "１.５", "𝟏𝟐", "1٣", "٣e٢", "٣_٣", "  5  ", "\t5\n", "\x0b5\x0c", "\r5",
             "\x1c5", "5\x1d", "\x1f5", "\x855", "5\x85", "\xa05", "5\xa0", "\u30005", "5\u3000", "\u20285", "5\u2029", "5 5", "5\u30005", "１ ２", "\u00b2", "②", "½", "Ⅷ", "日", "5日", "e", "E", "+e5", "5e+", "5e-", "+.5", "-.5e-3", "-0", "-0.0", "+0e0",
             "\ud800", "5\ud800", "\U0001d7ce", "\u0660\u0661", "1\u0660"]
    pool = ["0", "1", "5", "9", "٣", "１", "𝟓", "_", ".", "e", "E", "+", "-", " ", "\t", "\x1c", "\u3000", "\xa0", "inf", "nan", "i", "N", "x", "a", "日", "\u2029"]
    rng = Lcg(2026)
    out = [list(map(ord, s)) for s in fixed]
    seen = set(fixed)
    digits = ["0", "1", "5", "9", "\u0663", "\uff11", "\U0001d7d3"]

    def run(n):
        return "".join(digits[rng.below(len(digits))] for _ in range(n)) if rng.below(5) else ""

    while len(out) < 4000:
        if rng.below(2):
            s = "".join(pool[rng.below(len(pool))] for _ in range(rng.below(9)))
        else:
            # 形の整った数に、空白・符号・_・指数を足し、1 字を差し替える
            s = ["", "+", "-", " ", "\u3000"][rng.below(5)] + run(rng.below(4) + 1) + [".", "_", "", "._", "_."][rng.below(5)] + run(rng.below(4))
            s += ["", "e", "E", "e+", "e-", "e_"][rng.below(6)] + (run(rng.below(3) + 1) if rng.below(3) else "") + [" ", "", "", "\xa0", "\x1c"][rng.below(5)]
            if s and rng.below(3) == 0:
                at = rng.below(len(s))
                s = s[:at] + pool[rng.below(len(pool))] + s[at + 1:]
        if s not in seen:
            seen.add(s)
            out.append([ord(c) for c in s])
    # 仮数の前後に 0 が 800 個を超えて並び、指数で打ち消す形。乱数の形は 4 字程度までなので、ここで別に足す
    for n in (799, 800, 801, 1000):
        for s in ("1" + "0" * n + "e-" + str(n), "12" + "0" * n + "e-" + str(n), "1_" + "0" * n + "e-" + str(n), "0" * n + "1e0", "1." + "0" * n + "e0",
                  "1" + "0" * n + "." + "0" * n + "e-" + str(n), "123456789012345678901" + "0" * n + "e-" + str(n), "0." + "0" * n + "1e" + str(n + 1), "-1" + "0" * n + "e-" + str(n + 400)):
            out.append([ord(c) for c in s])
    return [[cps, float_hex("".join(map(chr, cps)))] for cps in out]


def main():
    rng = Lcg(31337)
    alpha = alphabet()
    cases = []
    fixed = ["", " ", "a", "\n", "\r", "\r\n", "\n\r", "\r\r\n", "\r\n\r\n", "\n\n", "a\n", "\na", "a\n\n", "a\rb\r", "a\r\nb\r\n", "a\r\n\nb", "abc", "a b", "  a  ", "\t a \n",
             "x\u3000", "\u3000x\u3000", "\x1fx\x1f", "a\x1fb", "a\x1cb", "a\x85b", "a" + chr(0x2028) + "b", "a" + chr(0x2029) + "b", "\ufeffa", "\u200ba", "\u00a0a\u00a0", "１２３", "²", "①", "〇", "Ⅷ", "½", "٣", "a1", "a_1",
             "é", "é", "日本語", "😀", "😀", "\ud800", "a\ud800b", "\n" * 5, "a\n" * 3, " \t\n\r\x0b\x0c"]
    for s in SEPARATORS:
        fixed += ["a" + s + "b", s, s + s, "a" + s, s + "a", s + "\n"]
    for s in fixed:
        cases.append(record(s, " \t" if rng.below(2) else "a\n"))
    for _ in range(1500):
        n = rng.below(9)
        s = "".join(alpha[rng.below(len(alpha))] for _ in range(n))
        chars = "".join(alpha[rng.below(len(alpha))] for _ in range(rng.below(5))) if rng.below(3) else None
        cases.append(record(s, chars))
    json.dump({"cases": cases, "floats": float_cases()}, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
