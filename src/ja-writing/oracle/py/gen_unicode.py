"""Python の unicodedata と str の分類を全コードポイントについて読み、JSON で返す driver (internal/pyunicode の表の出典)。

標準入力は使わない。標準出力は 1 つの JSON で、次のキーを持つ (範囲は両端を含む [lo, hi]、cp はコードポイント)。
  categories      [[開始 cp, 一般カテゴリ], ...]  0 から 0x10FFFF まで切れ目なく (次の開始の手前までがその値)
  props           {名前: [[lo, hi], ...]}         str.isspace / isalpha / isdecimal / isdigit / isnumeric と splitlines の区切り、
                                                  identifier_start (chr(cp).isidentifier()) と identifier_continue (("a" + chr(cp)).isidentifier())
  ccc             [[lo, hi, 結合クラス], ...]     0 でないものだけ
  nfd / nfkd      [[cp, [cp, ...]], ...]          1 字の正規化の結果が元と違うもの (ハングルの音節は算法で決まるので入れない)
  nfc / nfkc      同上 (単独の字を NFC / NFKC にした結果が元と違うもの。サロゲートは Go の文字列に入れられないので除く)
  compose         [[先頭, 続き, 合成した字], ...]  主合成 (除外されたものを含まない)
  digests         {nfd, nfkd, nfc, nfkc: sha256}  サロゲートを除く全 cp の結果を "<cp 16 進>:<結果の cp 16 進を空白区切り>\\n" の列にした SHA-256
  cases           [[元の文字列, nfc, nfd, nfkc, nfkd], ...]  文字列どうしの正規化 (合成・並べ替え・合成を止める並び)
サロゲートを含む文字列は返さない (Go 側の文字列は WTF-8 を使うが、JSON の受け渡しでは扱わない)。
"""
import hashlib
import json
import sys
import unicodedata

MAX = 0x110000
HANGUL_FIRST, HANGUL_LAST = 0xAC00, 0xD7A3


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


def is_surrogate(cp):
    return 0xD800 <= cp <= 0xDFFF


def is_hangul_syllable(cp):
    return HANGUL_FIRST <= cp <= HANGUL_LAST


def cps(s):
    return [ord(c) for c in s]


def categories():
    out, prev = [], None
    for cp in range(MAX):
        cat = unicodedata.category(chr(cp))
        if cat != prev:
            out.append([cp, cat])
            prev = cat
    return out


def ccc_runs():
    out = []
    for cp in range(MAX):
        value = unicodedata.combining(chr(cp))
        if value == 0:
            continue
        if out and out[-1][1] == cp - 1 and out[-1][2] == value:
            out[-1][1] = cp
        else:
            out.append([cp, cp, value])
    return out


def single_forms(form, skip_hangul):
    out = []
    for cp in range(MAX):
        if is_surrogate(cp) or (skip_hangul and is_hangul_syllable(cp)):
            continue
        result = unicodedata.normalize(form, chr(cp))
        if result != chr(cp):
            out.append([cp, cps(result)])
    return out


def digest(form):
    h = hashlib.sha256()
    for cp in range(MAX):
        if is_surrogate(cp):
            continue
        result = unicodedata.normalize(form, chr(cp))
        h.update(("%x:%s\n" % (cp, " ".join("%x" % c for c in cps(result)))).encode("ascii"))
    return h.hexdigest()


def compositions():
    out = []
    for cp in range(MAX):
        if is_surrogate(cp) or is_hangul_syllable(cp):
            continue
        mapping = unicodedata.decomposition(chr(cp))
        if not mapping or mapping.startswith("<"):
            continue
        parts = [int(p, 16) for p in mapping.split()]
        if len(parts) != 2:
            continue
        if unicodedata.normalize("NFC", chr(parts[0]) + chr(parts[1])) == chr(cp):
            out.append([parts[0], parts[1], cp])
    out.sort()
    return out


class Lcg:
    """乱数は version で変わりうるので使わない。決まった算法で決まった列を返す。"""

    def __init__(self, seed):
        self.x = seed

    def below(self, n):
        self.x = (self.x * 6364136223846793005 + 1442695040888963407) % (1 << 64)
        return (self.x >> 33) % n


def normalize_cases(comp):
    cases = []

    def add(s):
        if any(is_surrogate(ord(c)) for c in s):
            return
        cases.append([s] + [unicodedata.normalize(f, s) for f in ("NFC", "NFD", "NFKC", "NFKD")])

    # 主合成の全部: 合成の前の組・組の逆順・合成した字そのもの
    for a, b, c in comp:
        add(chr(a) + chr(b))
        add(chr(b) + chr(a))
        add(chr(c))
    # 結合クラスの違う記号を 2 つ挟んで並べ替えと合成の阻止を試す
    marks = [cp for cp in range(MAX) if unicodedata.combining(chr(cp))]
    starters = sorted({a for a, _, _ in comp if unicodedata.combining(chr(a)) == 0})
    rng = Lcg(12345)
    alphabet = sorted(set(marks) | {a for a, _, _ in comp} | {b for _, b, _ in comp})
    alphabet += [ord(c) for c in "aeiouAEIOUnNcC"] + [0x1100, 0x1161, 0x11A8, 0xAC00, 0xAC01, 0xD7A3, 0x00A0, 0x2126, 0x212B, 0xFB01, 0x3000, 0xFF21]
    for _ in range(2500):
        n = 2 + rng.below(4)
        add("".join(chr(alphabet[rng.below(len(alphabet))]) for _ in range(n)))
    for _ in range(1500):
        base = chr(starters[rng.below(len(starters))])
        add(base + "".join(chr(marks[rng.below(len(marks))]) for _ in range(1 + rng.below(3))))
    # 互換分解の字の並び
    compat = [cp for cp in range(MAX) if not is_surrogate(cp) and unicodedata.normalize("NFKC", chr(cp)) != unicodedata.normalize("NFC", chr(cp))]
    for _ in range(500):
        n = 1 + rng.below(3)
        add("".join(chr(compat[rng.below(len(compat))]) for _ in range(n)) + chr(marks[rng.below(len(marks))]))
    # 結合クラスが同じ字が 13 個以上並ぶ列 (並べ替えは同じクラスの字の順を保つ。短い列は挿入ソートで安定のまま通るので、長い列で試す)
    same = {}
    for cp in marks:
        same.setdefault(unicodedata.combining(chr(cp)), []).append(cp)
    for cls, members in sorted(same.items()):
        if len(members) < 13:
            continue
        for n in (13, 14, 20, min(len(members), 60)):
            pick = members[:n]
            for seq in (pick, pick[::-1], pick[1::2] + pick[0::2]):
                add("a" + "".join(chr(c) for c in seq))
                add("a" + "".join(chr(c) for c in seq) + chr(0x0323) + "".join(chr(c) for c in seq[:3]))
    # ハングルの合成の境界: 終声の前の U+11A7 (終声の範囲の外) と U+11A8 (範囲の内)、LV と LVT、先頭の子音・母音の両端
    for first in (0x1100, 0x1112, 0x1113, 0xAC00, 0xAC01, 0xD7A3, 0xD7A4, 0xABFF):
        for second in (0x1160, 0x1161, 0x1175, 0x1176, 0x11A7, 0x11A8, 0x11C2, 0x11C3):
            add(chr(first) + chr(second))
            add(chr(first) + chr(second) + chr(0x11A8))
    return cases


def main():
    comp = compositions()
    lines = {"\n", "\r", "\x0b", "\x0c", "\x1c", "\x1d", "\x1e", "\x85", chr(0x2028), chr(0x2029)}
    props = {
        "space": ranges(lambda cp: chr(cp).isspace()),
        "alpha": ranges(lambda cp: chr(cp).isalpha()),
        "decimal": ranges(lambda cp: chr(cp).isdecimal()),
        "digit": ranges(lambda cp: chr(cp).isdigit()),
        "numeric": ranges(lambda cp: chr(cp).isnumeric()),
        "linebreak": ranges(lambda cp: len(("a" + chr(cp) + "b").splitlines()) > 1),
        "identifier_start": ranges(lambda cp: chr(cp).isidentifier()),
        "identifier_continue": ranges(lambda cp: ("a" + chr(cp)).isidentifier()),
    }
    # splitlines の区切りは決まった一覧のはず。一覧と違えば本家の Python が変わっている
    got = {cp for lo, hi in props["linebreak"] for cp in range(lo, hi + 1)}
    if got != {ord(c) for c in lines}:
        raise SystemExit("splitlines の区切りが想定と違う: %r" % sorted(got ^ {ord(c) for c in lines}))
    out = {
        "categories": categories(),
        "props": props,
        "ccc": ccc_runs(),
        "nfd": single_forms("NFD", True),
        "nfkd": single_forms("NFKD", True),
        "nfc": single_forms("NFC", False),
        "nfkc": single_forms("NFKC", False),
        "compose": comp,
        "digests": {f.lower(): digest(f) for f in ("NFD", "NFKD", "NFC", "NFKC")},
        "cases": normalize_cases(comp),
    }
    json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
