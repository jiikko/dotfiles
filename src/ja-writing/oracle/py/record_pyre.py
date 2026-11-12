"""re の字の集合・大文字小文字の対応・関数の振る舞いを Python から読んで JSON で返す driver (internal/pyre の表と正解)。

標準入力は使わない。入力は決まった算法で作る (乱数は使わない)。標準出力は 1 つの JSON で、次のキーを持つ。
  tables   re の IGNORECASE の規則そのもの (internal/pyre の表の出典)
             lower   [[x, 小文字], ...]  _sre.unicode_tolower(x) が x と違うもの
             cased   [[lo, hi], ...]     _sre.unicode_iscased が真の範囲
             extra   [[小文字, [同じ大文字を持つ別の小文字, ...]], ...]  re._casefix._EXTRA_CASES
  chars    全コードポイントを 1 つの文字列にして本物の re に通した結果
             word / space / digit            [[lo, hi], ...]  \\w+ \\s+ \\d+ が当たる範囲 (Unicode)
             word_ascii / space_ascii / digit_ascii          re.A のとき
             boundary / boundary_ascii       [位置, ...]       \\b が当たる位置 (全文字列の添字。\\B は補集合)
             candidates                      [cp, ...]        大文字小文字の照合を試す字 (大小のある字・対応のある字・間引いた字)
             ignorecase                      [[cp, [当たる cp, ...]], ...]  re.escape(chr(cp)) を re.I で candidates に当てた結果
             classes                         [{"src": 文字の集合, "flags": フラグ, "hits": [当たる cp, ...]}, ...]
  funcs    関数ごとの入力と結果 (Go のテストが同じ入力で呼んで比べる)。文字列 (pattern・s・arg と結果の中の全部の文字列) は、
           単独のサロゲートを含むなら {"ws": [cp, ...]}、含まなければ JSON の文字列 (JSON の "\\ud800" は Go の string にできず U+FFFD になって、入力も期待値も別の字に変わるため)
"""
import json
import re
import sys
import warnings
from re import _casefix

import _sre

warnings.simplefilter("ignore")  # 入れ子の集合の FutureWarning は stderr に出るだけで、結果は変わらない
MAX = 0x110000
ALL = "".join(map(chr, range(MAX)))


def enc(s):
    return {"ws": [ord(c) for c in s]} if any(0xD800 <= ord(c) <= 0xDFFF for c in s) else s


def enc_value(v):
    if isinstance(v, str):
        return enc(v)
    if isinstance(v, list):
        return [enc_value(x) for x in v]
    if isinstance(v, dict):
        for k in v:
            if enc(k) is not k:
                raise SystemExit("サロゲートを含む鍵は符号化できない: %r" % k)
        return {k: enc_value(x) for k, x in v.items()}
    return v


def ranges_of(compiled):
    out = []
    for m in compiled.finditer(ALL):
        out.append([m.start(), m.end() - 1])
    return out


def tables():
    lower = [[x, _sre.unicode_tolower(x)] for x in range(MAX) if _sre.unicode_tolower(x) != x]
    cased, start = [], None
    for x in range(MAX):
        if _sre.unicode_iscased(x):
            if start is None:
                start = x
        elif start is not None:
            cased.append([start, x - 1])
            start = None
    if start is not None:
        cased.append([start, MAX - 1])
    extra = [[k, list(v)] for k, v in sorted(_casefix._EXTRA_CASES.items())]
    return {"lower": lower, "cased": cased, "extra": extra}


def candidates(tab):
    special = {x for x, _ in tab["lower"]} | {lo for _, lo in tab["lower"]}
    for lo, ks in tab["extra"]:
        special.add(lo)
        special.update(ks)
    for lo, hi in tab["cased"]:
        special.update(range(lo, hi + 1))
    special.update(range(0, 256))
    special.update(range(0, MAX, 97))
    return sorted(special)


CLASSES = [
    "[a-z]", "[A-Z]", "[^a-z]", "[a-zA-Z0-9_]", "[\\w]", "[\\wK]", "[^\\W\\d]", "[\\u00e0-\\u00ff]", "[\\u03b1-\\u03c9]",
    "[\\u0391-\\u03a9]", "[\U00010400-\U0001044f]", "[\U00010428]", "[\U00010400]", "[\U00010000-\U00010500]", "\u0130", "[\u0130]",
    "[\u0131]", "i", "I", "k", "K", "\u212a", "s", "\u017f", "\u00df", "\u1e9e", "\u03c3", "\u03c2", "\u03a3", "\u01c5", "\u00b5", "\u03bc",
    "[^i]", "[^\u0130]", "[a-z\u00df]", "[\\s\\S]", "[^\\s\\S]", "[\\d\\u0660-\\u0669]", "[\\u0345]", "\u0345", "\u03b9", "\u1fbe",
]


def match_positions(compiled, text):
    return [m.start() for m in compiled.finditer(text)]


def chars(tab):
    cand = candidates(tab)
    cand_text = "".join(chr(x) for x in cand)
    out = {
        "word": ranges_of(re.compile(r"\w+")), "space": ranges_of(re.compile(r"\s+")), "digit": ranges_of(re.compile(r"\d+")),
        "word_ascii": ranges_of(re.compile(r"\w+", re.A)), "space_ascii": ranges_of(re.compile(r"\s+", re.A)),
        "digit_ascii": ranges_of(re.compile(r"\d+", re.A)),
        "boundary": match_positions(re.compile(r"\b"), ALL), "boundary_ascii": match_positions(re.compile(r"\b", re.A), ALL),
        "candidates": cand,
    }
    cased = {x for lo, hi in tab["cased"] for x in range(lo, hi + 1)}
    ignorecase = []
    for c in range(MAX):
        if c in cased or (c % 1009 == 0):
            pat = re.compile(re.escape(chr(c)), re.I)
            ignorecase.append([c, [cand[i] for i in match_positions(pat, cand_text)]])
    out["ignorecase"] = ignorecase
    classes = []
    for src in CLASSES:
        for flags in (re.I, re.I | re.A):
            pat = re.compile(src, flags)
            classes.append({"src": src, "flags": int(flags), "hits": [cand[i] for i in match_positions(pat, cand_text)]})
    out["classes"] = classes
    return out


def match_json(m):
    pat = m.re
    groups = []
    for g in range(pat.groups + 1):
        groups.append(None if m.start(g) < 0 else [m.start(g), m.end(g), m.group(g)])
    return {"groups": groups, "lastindex": m.lastindex, "lastgroup": m.lastgroup, "groupdict": m.groupdict()}


def error_json(exc):
    out = {"error": type(exc).__name__, "msg": getattr(exc, "msg", str(exc))}
    pos = getattr(exc, "pos", None)
    if pos is not None:
        out["pos"] = pos
    return out


def brackets(m):
    return "<" + m.group(0) + ">"


def run_case(case):
    op = case["op"]
    pattern, flags, s = case["pattern"], case.get("flags", 0), case.get("s", "")
    pos, endpos = case.get("pos", 0), case.get("endpos", sys.maxsize)
    try:
        if op == "escape":
            return re.escape(pattern)
        pat = re.compile(pattern, flags)
        if op == "compile":
            return {"flags": pat.flags, "groups": pat.groups, "groupindex": dict(pat.groupindex)}
        if op in ("search", "match", "fullmatch"):
            m = getattr(pat, op)(s, pos, endpos)
            return None if m is None else match_json(m)
        if op == "finditer":
            return [match_json(m) for m in pat.finditer(s, pos, endpos)]
        if op == "findall":
            return pat.findall(s, pos, endpos)
        if op == "sub" or op == "subn":
            repl = case["arg"]
            if case.get("func"):
                repl = brackets
            result = getattr(pat, op)(repl, s, case.get("n", 0))
            return list(result) if op == "subn" else result
        if op == "split":
            return pat.split(s, case.get("n", 0))
        if op == "expand":
            m = pat.search(s)
            return None if m is None else m.expand(case["arg"])
    except (re.error, ValueError, IndexError, OverflowError, RecursionError) as exc:
        return error_json(exc)
    raise SystemExit("未知の op: " + op)


def cases():
    out = []

    def add(op, pattern, s="", **kw):
        out.append(dict(op=op, pattern=pattern, s=s, **kw))

    for flavor in ("search", "match", "fullmatch"):
        for pattern, s in [
            ("a*", ""), ("a*", "baaa"), ("a+", "baaa"), ("(a)|b", "b"), ("(a)|(b)", "xb"), ("(?P<x>a)(?P<y>b)?", "ac"), ("^a", "ba"), ("a$", "a\n"),
            ("a$", "a\n\n"), ("(?m)^b", "a\nb"), ("(?m)a$", "a\nb"), ("\\bfoo\\b", "a foo b"), ("\\Bo", "foo"), ("\\B", ""), ("\\b", ""),
            ("(a)(b)?", "a"), ("((a)(b))", "ab"), ("(a)()", "a"), ("(?:(a)|b)*", "ab"), ("(?:(a)|(b))+", "ab"),
            ("(?<=a)b", "ab"), ("(?<!a)b", "ab b"), ("a(?=b)", "ab"), ("a(?!b)", "ab ac"), ("\\Z", "a\n"), ("\\z", "a"), ("\\A", "a"),
            ("\u3042+", "\u3042\u3042\u3044"), ("(?i)\u00df", "SS \u1e9e"), ("(?i)k", "\u212a K"), ("(?i)[k]", "\u212a"), ("(?a)(?i)k", "\u212a"),
            ("(?i)\u03c3", "\u03a3\u03c2"), ("\\w+", "caf\u00e9 \u4e00\u4e8c_3"), ("(?a)\\w+", "caf\u00e9 x"), ("\\d+", "\u0663\u0664 12"), ("(?a)\\d+", "\u0663\u0664 12"),
            ("\\s+", "a\u3000\u00a0 b\x1c"), ("(?a)\\s+", "a\u3000 \x1c\x0b"), (".+", "a\nb"), ("(?s).+", "a\nb"), (".", "\r"),
            ("a{2}", "aaa"), ("a{2,}", "aaaa"), ("a{,2}", "aaaa"), ("a{2,3}?", "aaaa"), ("a{", "a{"), ("a{1", "a{1"), ("a{}", "a{}"), ("x*+y", "xxy"),
            ("(?>a+)b", "aaab"), ("(?>a+)ab", "aaab"), ("(a)?(?(1)b|c)", "ab c"), ("(a)\\1", "aa"), ("(?P<q>a)(?P=q)", "aa"), ("(?i)(a)\\1", "aA"),
            ("(?x) a b # c\n c", "abc"), ("(?x) [ ] b", " b"), ("[\\d\\-x]+", "1-x"), ("[]a]+", "]a"), ("[^]a]+", "b]"), ("[a\\]]+", "a]"),
            ("\\x41\\u0042\\U00000043", "ABC"), ("\\101\\0", "A\x00"), ("(?#c)a", "a"), ("(?i:a)b", "Ab AB"), ("a(?i:b)c", "aBc aBC"),
            ("(?s:.)\\n", "a\n"), ("(?-i:a)", "A"), ("(?i)(?-i:a)", "A"), ("(?i)a(?-i:b)", "AB"), ("[\\b]", "\x08"), ("\\.", "."),
            ("(?:)", "abc"), ("()", "abc"), ("a|", "b"), ("|a", "a"), ("(|a)+", "aa"), ("(a*)*", "aaa"), ("(a*)+", "b"), ("(a|b)*?c", "abc"),
            ("(?=a)*", "a"), ("(a)|(b)|(c)", "c"),
        ]:
            add(flavor, pattern, s)
    for pattern, s, pos, endpos in [
        ("^a", "ba", 1, 2), ("a$", "ab", 0, 1), ("a$", "ab", 0, 2), ("\\ba", "ba", 1, 2), ("(?<=b)a", "ba", 1, 2), ("a", "aaa", 1, 2), ("a*", "aaa", 1, 2),
        ("a*", "aaa", 5, 9), ("a*", "aaa", -3, 2), ("a", "aaa", 2, 1), ("\\b", "ab", 1, 2), ("a\\b", "ab", 0, 1), ("$", "a\n", 0, 2), ("$", "ab\n", 0, 3),
        ("(?m)^", "a\nb", 2, 3), ("(?m)^", "a\nb", 1, 3), ("\\B", "ab", 2, 2), ("\\B", "ab", 1, 1), ("(?=b)", "ab", 0, 1), ("(?<=a)", "ab", 1, 2),
    ]:
        add("search", pattern, s, pos=pos, endpos=endpos)
        add("match", pattern, s, pos=pos, endpos=endpos)
        add("fullmatch", pattern, s, pos=pos, endpos=endpos)
        add("finditer", pattern, s, pos=pos, endpos=endpos)
    for pattern, s in [
        ("a*", "baac"), ("a*?", "baac"), ("", "abc"), ("\\b", "ab cd"), ("^", "a\nb"), ("(?m)^", "a\nb\n"), ("$", "a\n"), ("(?m)$", "a\nb\n"), ("(?=a)", "aaa"),
        ("(a)", "aXa"), ("(a)(b)", "abab"), ("(a)|(b)", "ab"), ("(?P<n>a+)", "aa a"), ("x*", "\u3042\U0001f600x"), ("\\s*", " a  b "), ("(a*)", "baac"),
        ("\\w+", "foo bar"), ("\\d", "1a2"), ("[\u3042-\u3093]+", "\u3042\u3044x\u3046"), ("(?i)a", "aAbB"), ("a|ab", "abab"), ("ab|a", "abab"),
        ("(?<=,)|(?=,)", "a,b"), ("(?<![a-z])x", "ax x"), ("", ""), ("a*", ""),
    ]:
        add("finditer", pattern, s)
        add("findall", pattern, s)
        add("split", pattern, s)
        add("split", pattern, s, n=1)
        add("split", pattern, s, n=2)
        add("sub", pattern, s, arg="-")
        add("subn", pattern, s, arg="[\\g<0>]")
        add("sub", pattern, s, arg="", func=True)
        add("subn", pattern, s, arg="", func=True, n=2)
        add("sub", pattern, s, arg="-", n=1)
    # 開始位置の字・BMP の外の字の否定・inline の (?a:) の中の分類・空になれる本体の {m,n}・pos > endpos・自分のグループを参照する条件
    for pattern, s in [
        ("x*[^\U0001f600]", "x_"), ("x|[^\U0001f600]", "x_"), ("(?:\\A|;)[^\U0001f600]", ";a"), ("x*[^\U0010ffff]", "x_"), ("x*[^\U0001f600-\U0001f601]", "x_"),
        ("x*[^\U0001f600a]", "x_"), ("(?i)x*[^\U0001f600]", "xX_"), ("[^\U0001f600]+", "a\U0001f600b"), ("(?:x|[^\U00010000-\U0010ffff])+", "xy\U0001f600"),
    ]:
        add("search", pattern, s)
        add("finditer", pattern, s)
    for pattern in ["(?a:\\W)", "(?a:\\S)", "(?a:\\D)", "(?a:[^\\d])", "(?a:[\\W])", "(?a:\\w)", "(?a:\\d)", "(?a:\\s)", "(?a:\\B)", "(?a:\\b)", "(?a:\\Wx)", "(?a:\\W|y)",
                    "(?a:(\\W))", "x(?a:\\W)", "(?a:[^a])", "(?a:\\W)+", "(?u:\\w)", "(?u:\\W)", "(?i:\\W)", "(?a:\\W)?x", "(?:(?a:\\W))", "(?a:\\D)(?u:\\D)", "(?a:[\\W\\d])"]:
        for text in ["\u00c9", "\u3000", "\u0663", "!", "a", "x\u00c9", "\u00c9x", "y", "\u00c9\u0663 a1"]:
            for flags in (0, int(re.A)):
                add("search", pattern, text, flags=flags)
                add("finditer", pattern, text, flags=flags)
                add("match", pattern, text, flags=flags)
    for pattern, s in [
        ("(a*?){1,2}", "a"), ("(?:\\S*|.){1,2}", "a b"), ("(a*){2,3}", "aa"), ("(a|b*?){2,3}c", "abc"), ("(a*?){2,4}", "aaa"), ("(a*?){1,3}?", "aa"), ("(a*){1,2}?b", "aab"),
        ("(a?){1,2}", "a"), ("(a??){1,3}", "aa"), ("((a)|b*?){1,2}", "ab"), ("(?:a*?){1,2}", "aa"), ("(?:(a)|(b)*?){2,3}", "ab"), ("(\\b){1,2}", "a b"), ("(^|a){1,2}", "aa"),
        ("(a*?){1,2}+", "a"), ("(a*?){3}", "aa"), ("(a*?){0,2}", "a"), ("(a*?){1,}", "a"), ("((a*?)b?){1,2}", "ab"), ("(?:x*){1,2}y", "xxy"), ("(a*?|b){1,3}c", "abc"),
        ("(?:(?:a*?){1,2}b?){1,2}", "ab"), ("(a*?){1,2}(b)", "ab"), ("(?P<n>a*?){1,2}(?P=n)", "aa"),
    ]:
        for flavor in ("search", "match", "fullmatch", "finditer", "findall"):
            add(flavor, pattern, s)
        add("sub", pattern, s, arg="[\\g<0>]")
    for pattern, s, pos, endpos in [
        ("(?!a)", "bk", 2, 1), ("(?<!x)", "bk", 2, 1), ("(?!x)", "bk", 3, 1), ("$", "ab", 2, 1), ("", "ab", 2, 1), ("\\b", "ab", 2, 1), ("a*", "aaa", 3, 1), ("(?<=a)", "ab", 2, 0),
        ("\\B", "ab", 2, 1), ("^", "ab", 2, 1), ("(?m)^", "a\nb", 2, 1), ("\\Z", "ab", 2, 1),
    ]:
        for flavor in ("search", "match", "fullmatch", "finditer", "findall"):
            add(flavor, pattern, s, pos=pos, endpos=endpos)
    # 自分のグループの中から自分を参照する条件 ((?:(x?(?(1)_))[^a])*) は Go と違う。README の「揃わない範囲」で、ここには入れない
    for pattern, s in [("(?:(x)?(?(1)_|-)y)*", "x_y-yx_y"),
                  ("(x?(?(1)_))", "x_"), ("(?:(a?)(?(1)b|c))+", "abc")]:
        for flavor in ("search", "match", "fullmatch", "finditer"):
            add(flavor, pattern, s)
    # 本家の flag-patterns.txt の検査器 (markdown_visibility.check_pattern) が受ける部分集合のパターン (本家のテストと語の一覧の見本から)
    flag_patterns = [
        "(?<![一-龥々])枝(?![一-龥々])", "枝{2,3}葉", "ba{1,3}c", "ba{2}c", "依存先(?=に)", "鍵穴|(?<!公開)鍵", "(?<=の)枝(?![一-龥々])|(?<![一-龥々])枝(?=[のでにをはが]|ごと)",
        "背景(?=で(?:実行|回|走|待)|実行|ジョブ|タスク)", "(?<!公開)(?<!秘密)(?<!署名)(?<!暗号)(?<!SSH)鍵(?![括盤穴])", "偽実装を[^。、]{0,6}通[さしすせ]",
        "(?:main|ブランチ) ?に入れ", "\\b(?:PR|issue) ?の題\\b", "\\.\\.\\.", "キャッシュ(?!制御)", "背景(?=実行|ジョブ)", "(?:枝|葉)", "[^。]{0,30}[^、]{0,30}[^！]{0,30}枝",
        "(?<=の|ので)枝", "^\\s*枝$", "\\A枝\\Z", "枝?葉{1,2}?", "\\d{1,3}\\D\\w\\W\\s\\S", "[\\t\\n]枝",
    ]
    flag_texts = [
        "枝の枝が葉", "公開鍵と鍵穴と秘密鍵 SSH鍵 鍵盤", "背景で実行 背景ジョブ 背景に", "偽実装を通す、偽実装を押し通した", "main に入れ ブランチに入れ PR の題 issue の題を",
        "bc bac baac baaac baaaac", "a...b ...", "キャッシュ制御 キャッシュ", "依存先に 依存先で", "のので枝\n枝\n 枝", "\t枝\n葉葉葉", "123x_ y!", "",
    ]
    for pattern in flag_patterns:
        for text in flag_texts:
            add("finditer", pattern, text)
            add("search", pattern, text, flags=int(re.M))
    for template, pattern, s in [
        ("\\1\\2", "(a)(b)", "ab"), ("\\g<2>\\g<1>", "(a)(b)", "ab"), ("\\g<n>", "(?P<n>a)", "a"), ("\\g<0>\\g<0>", "a", "a"), ("\\1", "(a)|b", "b"),
        ("\\n\\t\\\\", "a", "a"), ("\\0", "a", "a"), ("\\012", "a", "a"), ("\\101", "a", "a"), ("\\10", "(a)", "a"), ("\\g<1", "(a)", "a"), ("\\g<x>", "(a)", "a"),
        ("\\2", "(a)", "a"), ("\\q", "a", "a"), ("\\g<>", "(a)", "a"), ("\\", "a", "a"), ("\\g<1>2", "(a)", "a"), ("x\\1y", "(a)(b)?", "a"), ("\\19", "(a)", "a"),
        ("\\777", "a", "a"), ("\\8", "(a)", "a"), ("\u3042\\1", "(\u3044)", "\u3044"), ("\\g<a b>", "(?P<a>x)", "x"), ("\\g<\u00e9>", "(?P<\u00e9>x)", "x"),
    ]:
        add("sub", pattern, s, arg=template)
        add("expand", pattern, s, arg=template)
        add("subn", pattern, s + s, arg=template)
    for pattern in [
        "(", ")", "a)", "(?<=a+)b", "(?<=a|bc)d", "(?<=a{2})b", "(?<=a{1,2})b", "(?<!ab?)c", "(?<=\\w)x", "(?<=(a))b", "a**", "a*+*", "*a", "+", "?", "a{2,1}",
        "[b-a]", "[a", "[]", "(?P<a>x)(?P<a>y)", "(?P<1a>x)", "(?P<>x)", "(?P<a", "(?P=a)", "(?P=a>x)", "\\8", "\\1", "(a)\\2", "(?i)a(?i)", "a(?i)", "(?z)", "(?i-a:x)",
        "(?-i)", "(?ia)", "(?au)", "(?L)", "(?x-s:a)", "(?-x)a", "\\y", "\\xg", "\\x4", "\\u12", "\\U0011ffff", "\\U00110000", "[\\y]", "[\\d-z]", "[z-\\d]", "(?<a)", "(?<=)b",
        "(?<!)b", "(?(1)a|b|c)", "(?(1)a)", "(?(a)b)", "(?(0)a)", "(?(1", "(?", "(?#a", "a{99999999999}", "a{4294967295}", "(?P", "(?Pa)", "(?'a'x)", "\\", "[\\", "a|*",
        "\\A*", "^*", "$+", "\\b?", "(?=a)*", "(?i:", "(?i:a", "(?>a", "(?s-s:a)", "(?a-a:x)", "(?u:a)(?a:b)", "[[:alpha:]]", "[a-z&&[^b]]", "x{1,2}{3}",
    ]:
        add("compile", pattern)
    # search の開始位置の絞り込み (searchFilter) の IGNORECASE の断り・グループつきの繰り返しの pos > endpos・分岐の共通の先頭 (sameNode) の集合
    for pattern, texts in [
        ("(?i)[\\Wk]", ["K", "k", "\u212a", "!", "a"]), ("(?i)[\\Wa-z]", ["A", "a", "\u212a", "!", "1"]), ("(?i)[\\Wk]x", ["Kx", "kx", "!x"]),
        ("(?i)[\\W\u00e9]", ["\u00c9", "\u00e9", "x"]), ("(?i)[\\W\U00010428]", ["\U00010400", "\U00010428", "x"]),
        ("(?a:\\Wx|\\Wy)", ["\u00c9x", "\u00c9y", "!x", "!y", "x", "ax", "\u00c9\u00c9y"]), ("(?a:\\W|y)x", ["\u00c9x", "yx", "!x"]),
        ("(?a:[\\W]x|[\\W]y)", ["\u00c9x", "!y", "x"]), ("(?a:\\bx|\\by)", ["x", " x", "\u00c9x", "y"]), ("(?a:\\b\\Wx|\\b\\Wy)", ["\u00c9x", "!y", " !x"]),
        ("(?a:^\\Wx|^\\Wy)", ["\u00c9x", "!y", "a!x"]), ("(?:\\Wx|\\Wy)", ["\u00c9x", "!y"]),
    ]:
        for text in texts:
            for flags in (0, int(re.A)):
                for op in ("search", "match", "finditer"):
                    add(op, pattern, text, flags=flags)
    for pattern, s in [("(?i:a)*", "aaa"), ("(?:a)*", "aaa"), ("(?i:a)+", "aaa"), ("(?i:ab)*", "abab"), ("(a)*", "aaa"), ("(?i:(a))*", "aAa"), ("(?:(?i:a))*", "aaa"), ("(?s:.)*", "a\nb"),
                       ("(?i:a)?", "aa"), ("(?i:a){2}", "aaa"), ("(?i:\\W)*", "!!x"), ("(?a:\\w)*", "ab")]:
        for pos, endpos in [(2, 1), (3, 1), (3, 2), (2, 0), (1, 2), (0, 3), (3, 3)]:
            for flavor in ("search", "match", "fullmatch", "finditer"):
                add(flavor, pattern, s, pos=pos, endpos=endpos)
    for name in ["a\u00b7", "\u1885", "\u037a", "a\u0e33", "\u2e2f", "\u2118", "\u1369a", "a\u1369", "\u212e", "a\u0387", "\u00b7", "\u0e33", "a\u2e2f", "a\U00016d70", "\U00016d70"]:
        add("compile", "(?P<" + name + ">x)")
        add("compile", "x(?P=" + name + ")")
    for pattern in ["(?i)a", "(?a)b", "(?u)c", "(?s)d", "(?m)e", "(?x)f", "(?aim)g", "(?P<n>x)(y)(?P<m>z)", "(a)(b)(c)", "(?:a)", "a(?=b)(c)"]:
        add("compile", pattern)
        add("compile", pattern, flags=int(re.A | re.I))
        add("compile", pattern, flags=int(re.X | re.S | re.M))
    # 単独のサロゲートを含む入力・パターン・置き換えの文字列と結果 (Go の string は WTF-8 で持つ)
    for pattern, s in [(".", "\ud800"), ("a.b", "a\udfffb"), ("(.)", "\ud83d\ude00x"), ("[\ud800-\udfff]+", "a\ud800\udc00b"), ("\\w", "\ud800"), ("\\W+", "a\ud800b"),
                       ("(?i)\ud800", "\ud800"), ("\ud800|x", "x\ud800"), ("(?P<n>.)", "\udfff"), ("[^a]", "\ud800a"), ("\ud800*", "\ud800\ud800"), ("", "\ud800")]:
        for flavor in ("search", "match", "fullmatch", "finditer", "findall", "split"):
            add(flavor, pattern, s)
        add("sub", pattern, s, arg="-")
        add("sub", pattern, s, arg="\ud800\\g<0>")
        add("sub", pattern, s, arg="", func=True)
        add("expand", pattern, s, arg="\ud800\\g<0>")
    for pattern in ["(?P<a\ud800>x)", "(?P=\ud800)", "x(?P=a\ud800)", "(?(\ud800)a)", "[\ud800-\udc00]", "\ud800{2,1}"]:
        add("compile", pattern)
    # エラーの文の repr (引用符の選び方・制御文字・印字できない字の escape)。名前に使える字は identifier だけなので、不正な名前で repr を通す
    for name in ["a\n", "a\t", "a\r", "a\x00", "a\x1f", "a\x7f", "a\x80", "a\x85", "a\xa0", "a\xad", "a'b", 'a"b', "a'\"b", "a\\b", "a b", "a\u2028", "a\u2029",
                 "a\u200b", "a\u3000", "a\ufeff", "a\ue000", "a\ufffe", "a\U0001f600", "a\U000e0001", "a\U0010ffff", "a\u0378", "a\ud800", "a\udfff", "a-b", "1a", "a\u00e9\u3042"]:
        add("compile", "(?P<" + name + ">x)")
        add("compile", "(?P=" + name + ")")
        add("compile", "(?(" + name + ")a)")
        add("sub", "(x)", "x", arg="\\g<" + name + ">")
    add("compile", "(?P<a>x)(?P<a>y)")
    add("compile", "(?P<a\n>x)")
    add("sub", "(?P<a>x)", "x", arg="\\g<b\n>")
    add("sub", "(?P<a>x)", "x", arg="\\g<b'c>")
    # int() の桁数の上限 (sys.get_int_max_str_digits() の 4300。先頭の 0 も数える)
    for n in (4299, 4300, 4301, 5000):
        z = "0" * n
        add("compile", "a{" + z + "1}")
        add("compile", "a{1," + z + "1}")
        add("compile", "a{" + z + "1,2}")
        add("compile", "a{" + z + "}")
        add("compile", "(a)(?(" + z + "1)b)")
        add("compile", "(a)(?(" + z + ")b)")
        add("sub", "(a)", "a", arg="\\g<" + z + "1>")
        add("sub", "(a)", "a", arg="\\g<" + z + ">")
        add("sub", "(a)", "a", arg="\\g<" + z + "2>")
        add("compile", "a{4294967294," + "0" * n + "}")
    for pattern in ["a{4294967295}", "a{1,4294967295}", "(a)(?(1073741823)b)", "(a)(?(1073741822)b)", "(a)(?(00001073741823)b)", "(a)(?(0)b)", "(a)(?(00)b)"]:
        add("compile", pattern)
    add("sub", "(a)", "a", arg="\\g<1073741823>")
    add("sub", "(a)", "a", arg="\\g<00001073741823>")
    # 条件付きの参照の検査は参照の初出の順 (dict の挿入順)。番号の順でも、位置の順の逆でもない
    for pattern in ["(?(2)a)(?(3)b)", "(?(3)a)(?(2)b)", "(?(2)a)(?(3)b)(?(4)c)(?(5)d)", "(?(5)a)(?(4)b)(?(3)c)(?(2)d)", "(?(3)a)(?(2)b)(?(3)c)", "(a)(?(2)a)(?(3)b)", "(a)(?(3)a)(?(2)b)",
                    "(?(9)a)(?(2)b)(?(7)c)(?(2)d)", "(?(2)a|(?(5)b))(?(3)c)", "(?(4)a)(?(3)b)(a)(b)", "(a)(b)(?(5)x)(?(4)y)(?(3)z)", "(?(7)a)(?(6)b)(?(8)c)(?(5)d)(?(9)e)"]:
        add("compile", pattern)
    for s in ["", "a.b", "a b", "\u3042-\u3044", "[x]{y}", "#&~ \t\n\r\x0b\x0c", "\u00e9\U0001f600", "()*+?^$|\\"]:
        add("escape", s)
    return out


def main():
    tab = tables()
    out = {"tables": tab, "chars": chars(tab)}
    funcs = []
    for case in cases():
        enc_case = dict(case, pattern=enc(case["pattern"]), s=enc(case["s"]))
        if "arg" in case:
            enc_case["arg"] = enc(case["arg"])
        funcs.append(dict(enc_case, result=enc_value(run_case(case))))
    out["funcs"] = funcs
    json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))


main()
