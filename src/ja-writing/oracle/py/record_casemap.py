"""str.casefold / lower / swapcase を全コードポイントについて読み、JSON で返す driver (internal/pyunicode の大文字小文字の表の出典)。

標準入力は使わない。標準出力は 1 つの JSON で、次のキーを持つ (cp はコードポイント、サロゲートは含めない)。
  casefold / lower / swapcase  [[cp, [cp, ...]], ...]  1 字の結果が元と違うものだけ (lower と swapcase は 1 字だけの文字列に当てた結果。語末の Σ の規則は文脈が要るので cased と case_ignorable に分ける)
  cased / case_ignorable       [[lo, hi], ...]        語末の Σ の規則 (str.lower と swapcase が Σ を ς にする条件) に使う字の範囲。Python は性質を直接出さないので、
                               "<字>Σ" と "a<字>Σ" の小文字化から求める。cased は「語末の Σ の前後で大文字小文字のある字として数えられる字 (case_ignorable でないもの)」、
                               case_ignorable は読み飛ばされる字 (Σ の前後の規則は、読み飛ばした字の cased を見ない)
  decimal                      [[cp, 値], ...]        unicodedata.decimal(chr(cp)) が 0〜9 の字 (int() と float() が読む数字。re の \\d)
  digests                      {名前: sha256}          サロゲートを除く全 cp の結果を "<cp 16 進>:<結果の cp 16 進を空白区切り>\\n" の列にした SHA-256
  cases                        [[元, casefold, lower, swapcase], ...]  文字列は cp の列 (lower は語末の Σ を含む文字列も入れる)
"""
import hashlib
import json
import sys
import unicodedata

MAX = 0x110000
SIGMA = 0x3A3


def cps(s):
    return [ord(c) for c in s]


def table(fn):
    rows, lines = [], []
    for cp in range(MAX):
        if 0xD800 <= cp <= 0xDFFF:
            continue
        out = fn(chr(cp))
        lines.append("%x:%s\n" % (cp, " ".join("%x" % ord(c) for c in out)))
        if out != chr(cp):
            rows.append([cp, cps(out)])
    return rows, hashlib.sha256("".join(lines).encode("ascii")).hexdigest()


def ranges(pred):
    out, start = [], None
    for cp in range(MAX):
        if pred(cp):
            if start is None:
                start = cp
        elif start is not None:
            out.append([start, cp - 1])
            start = None
    if start is not None:
        out.append([start, MAX - 1])
    return out


def final_sigma(prefix):
    """prefix + Σ の小文字化で、Σ が語末の ς になったか。"""
    return (prefix + chr(SIGMA)).lower()[-1] == "\u03c2"


def decimals():
    return [[cp, unicodedata.decimal(chr(cp))] for cp in range(MAX) if chr(cp).isdecimal()]


def main():
    fns = {"casefold": str.casefold, "lower": str.lower, "swapcase": str.swapcase}
    out, digests = {}, {}
    for name, fn in fns.items():
        out[name], digests[name] = table(fn)
    fixed = ["", "abc", "ABC", "AbC", "ß", "ẞ", "STRASSE", "straße", "İ", "i̇", "ǅ", "ǆ", "Ǆ", "ﬃ", "ŉ", "ΐ", "ᾈ", "K", "Å", "ς", "σ", "Ω",
             "Ａｂ", "ＡＢＣ", "日本語", "ひらがな", "カタカナ", "é", "é", "É", "É", "/Users/Koji/Docs", "/private/var/Folders/Ab", "~/Foo_Bar", "Σ", "ΑΣ", "ΑΣ ", "ΑΣΑ"]
    cases = []
    for s in fixed:
        cases.append([cps(s), cps(s.casefold()), cps(s.lower()), cps(s.swapcase())])
    for s in ("ΑΣ", "ΑΣ ", "ΑΣΑ", "Σ", "ΑΣΑΣ", "Α.Σ", "ΑΣ.Α", "ΑΣ.", "Α\u0345Σ", "ΑΣ\u0345", "ΑΣ\u0345Α", "Σ\u0345", "1Σ", "ΑΣ1", "ΑΣΣ", "ὈΔΥΣΣΕΎΣ", "ΜΑΊΟΣ ΑΣΤΡΑΊΑ", "ΑʰΣ", "ΑΣʰ", "ΑΣʰΑ"):
        cases.append([cps(s), cps(s.casefold()), cps(s.lower()), cps(s.swapcase())])
    cased = ranges(lambda cp: final_sigma(chr(cp)))
    ignorable = ranges(lambda cp: final_sigma("a" + chr(cp)) and not final_sigma(chr(cp)))
    json.dump({"cased": cased, "case_ignorable": ignorable, "decimal": decimals(), "casefold": out["casefold"], "lower": out["lower"], "swapcase": out["swapcase"], "digests": digests, "cases": cases}, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
