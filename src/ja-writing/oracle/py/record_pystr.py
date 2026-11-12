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
    json.dump({"cases": cases}, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
