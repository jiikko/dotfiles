"""difflib.SequenceMatcher と本家の _ExactSequenceMatcher に同じ入力を通して、結果を JSON で返す driver (internal/difflib の正解)。

標準入力: {"scripts_dir": "...", "methods": [Go が持つメソッドの名前...], "cases": [{"kind": "str"|"list"|"reuse", "a": 並び, "b": 並び, "finds": [[alo, ahi, blo, bhi], ...], "exact_only": bool}, ...]}
  並びは {"text": 文字列} か {"items": [文字列, ...]}。文字列は、単独のサロゲートを含むなら {"ws": [cp, ...]}
標準出力: {"call_sites": [{"line": 行, "callee": 名前}, ...], "methods": [本家が使うメソッドの名前...], "results": [結果, ...]}
  結果は {"blocks", "opcodes", "ratio", "quick", "real_quick", "finds"} (kind "reuse" は {"reuse": [[real_quick, quick, ratio], ...]})

2 つの照合の答えが違えば止まる (本家の前提「difflib と同じ答え」をここで確かめる)。exact_only の入力は本家の照合だけに通す (difflib は長さの 2 乗で遅い)。
本家のソースを ast で読み、次のどれかがあれば止まる (Go が写していない使い方を本家が始めた):
  - 照合を作る呼び出しが (None, a, b, autojunk=False) の形でない
  - difflib. の属性が SequenceMatcher と Match の外
  - SequenceMatcher のメソッドの名前のうち、Go が持たないものを呼んでいる (get_grouped_opcodes など)
"""
import ast
import difflib
import json
import os
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import yomiyasu_diff  # noqa: E402

Exact = yomiyasu_diff._ExactSequenceMatcher
SOURCE = os.path.join(plan["scripts_dir"], "yomiyasu_diff.py")
MATCHER_METHODS = {name for name in vars(difflib.SequenceMatcher) if not name.startswith("_")}
ALLOWED_DIFFLIB = {"SequenceMatcher", "Match"}


def check_source():
    with open(SOURCE, encoding="utf-8") as f:
        tree = ast.parse(f.read())
    sites, used = [], set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name) and node.value.id == "difflib":
            if node.attr not in ALLOWED_DIFFLIB:
                raise SystemExit("本家が difflib.%s を使っている (%d 行)。Go に写していない" % (node.attr, node.lineno))
        if isinstance(node, ast.Attribute) and node.attr in MATCHER_METHODS:
            used.add(node.attr)
        if isinstance(node, ast.FunctionDef) and node.name in MATCHER_METHODS:
            used.add(node.name)
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        name = None
        if isinstance(func, ast.Attribute) and isinstance(func.value, ast.Name) and func.value.id == "difflib" and func.attr == "SequenceMatcher":
            name = "difflib.SequenceMatcher"
        elif isinstance(func, ast.Name) and func.id in ("SequenceMatcher", "_ExactSequenceMatcher"):
            name = func.id
        if name is None:
            continue
        shape_ok = (len(node.args) == 3 and isinstance(node.args[0], ast.Constant) and node.args[0].value is None
                    and len(node.keywords) == 1 and node.keywords[0].arg == "autojunk"
                    and isinstance(node.keywords[0].value, ast.Constant) and node.keywords[0].value.value is False)
        if not shape_ok:
            raise SystemExit("%d 行の %s の呼び出しが (None, a, b, autojunk=False) の形でない" % (node.lineno, name))
        sites.append({"line": node.lineno, "callee": name})
    missing = used - set(plan["methods"])
    if missing:
        raise SystemExit("本家が Go の持たないメソッド %s を使っている" % sorted(missing))
    sites.sort(key=lambda s: s["line"])
    return sites, sorted(used)


def dec(v):
    if isinstance(v, dict):
        return "".join(map(chr, v["ws"]))
    return v


def seq(s):
    if "text" in s:
        return dec(s["text"])
    return [dec(x) for x in s["items"]]


def result(m, finds):
    return {
        "blocks": [list(b) for b in m.get_matching_blocks()],
        "opcodes": [list(o) for o in m.get_opcodes()],
        "ratio": m.ratio(),
        "quick": m.quick_ratio(),
        "real_quick": m.real_quick_ratio(),
        "finds": [list(m.find_longest_match(*f)) for f in finds],
    }


def reuse(cls, olds, news):
    m = cls(None, "", "", autojunk=False)
    out = []
    for old in olds:
        m.set_seq2(old)
        for new in news:
            m.set_seq1(new)
            out.append([m.real_quick_ratio(), m.quick_ratio(), m.ratio()])
    return {"reuse": out}


def run(case):
    a, b = seq(case["a"]), seq(case["b"])
    finds = case.get("finds", [])
    if case["kind"] == "reuse":
        got = [reuse(cls, a, b) for cls in (difflib.SequenceMatcher, Exact)]
    elif case.get("exact_only"):
        return result(Exact(None, a, b, autojunk=False), finds)
    else:
        got = [result(cls(None, a, b, autojunk=False), finds) for cls in (difflib.SequenceMatcher, Exact)]
    if got[0] != got[1]:
        raise SystemExit("difflib と本家の _ExactSequenceMatcher の答えが違う: %r" % ({k: v for k, v in case.items() if k in ("kind", "finds")},))
    return got[0]


sites, methods = check_source()
results = [run(c) for c in plan["cases"]]
json.dump({"call_sites": sites, "methods": methods, "results": results}, sys.stdout, ensure_ascii=True, separators=(",", ":"))
