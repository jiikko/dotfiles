"""本家の lint の render_report・render_findings と、diff の report・report_parts・detector_summary に、欄を 1 つずつ書き換えた dict を通し、
例外になったか (ならなければ戻り値の SHA-256 の先頭 16 字) を JSON で返す driver。check はこの 5 つに子の --json を読んだ dict を渡し、例外を
「子の JSON から人が読む形を作れません」の誤りにする。Go の側は「本家が例外 ⇔ Go が error」を確かめる。

標準入力: {"scripts_dir": "...", "diff_inputs": [{"name", "old", "new", "doc_type"}], "lint_inputs": [{"name", "text", "doc_type", "reader", "words"}],
          "words": {"allow.txt": 中身, ...}, "words_path": context.words.path に書く文字列}
標準出力: {"bases": {名前: 書き換える前の dict}, "mutations": [{"base", "path", "variant", "results": [関数ごとに "E:<例外の型>" か 16 字]}]}
記録に残す書き換えは、どれかの関数が例外になったもの全部と、書き換えない基準の dict と、例外にならなかったもののうち (lint か diff, 位置から list の
添字を外した鍵の並び全体, 位置をたどる途中の dict の kind と subkind, 書き換えの種類) ごとに最初の 1 つ (全部を残すと約 11MB になる)。
位置の最後の 2 段で束ねない: 別の節の同じ名前の欄 (stiffness と tone の候補の subkind) が 1 つに束ねられ、片方の節だけを壊す退行が記録から消える。
kind と subkind を鍵から外さない: 同じ位置の欄が subkind ごとに別の分岐で読まれ (stiffness の sentence と sentence_nominal)、代表に選ばれなかった
subkind の分岐だけを壊す退行が記録から消える。"all" が真なら全部を返す (手で回す突き合わせ用)。

基準の dict は diff_inputs を本家の diff() に、lint_inputs を lint_text() に通した値 (語の一覧は一時フォルダに書いて読み、path を words_path に置き換える) を
JSON に書いて読み直したもの (diff() の値は節どうしで同じ list を共有することがあり、そのまま書き換えると 2 か所が変わる。check が読む子の出力は共有しない)。
書き換えは、dict の鍵と list の先頭 (添字 0) を深さ 5 までたどった各位置に、VARIANTS の 14 通り (del は鍵か要素を消す) を 1 つずつ当てる。
huge は float に変える所で OverflowError になる int、case_str は upper() が複数字に展開する字 (ß・ﬁ・ŉ) と単独のサロゲートを含む文字列。
"""
import copy
import hashlib
import json
import os
import sys
import tempfile

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import markdown_visibility as M  # noqa: E402
import yomiyasu_diff as D  # noqa: E402
import yomiyasu_lint as L  # noqa: E402

VARIANTS = (("del", None), ("null", None), ("empty_str", ""), ("str", "3"), ("empty_list", []), ("empty_dict", {}), ("zero", 0),
            ("neg", -2), ("true", True), ("false", False), ("float", 2.0), ("big", 10 ** 25), ("huge", 10 ** 400),
            ("case_str", "\u00df\ufb01\u0149\ud800x"))
SUMMARY_KEYS = ("stiffness", "tone", "hedges", "immutable", "politeness", "register")
LINT_FNS = (("render_report", None), ("render_findings", None))
DIFF_FNS = (("report_parts", None), ("report", None)) + tuple(("detector_summary", key) for key in SUMMARY_KEYS)
MAX_DEPTH = 4


def paths(value, prefix=(), depth=0):
    if depth > MAX_DEPTH:
        return
    if isinstance(value, dict):
        for key in value:
            yield prefix + (key,)
            yield from paths(value[key], prefix + (key,), depth + 1)
    elif isinstance(value, list) and value:
        yield prefix + (0,)
        yield from paths(value[0], prefix + (0,), depth + 1)


def at(d, path):
    for key in path:
        d = d[key]
    return d


def mutate(base, path, name, value):
    d = copy.deepcopy(base)
    parent = at(d, path[:-1])
    if name == "del":
        if isinstance(parent, dict):
            del parent[path[-1]]
        else:
            parent.pop(path[-1])
    else:
        parent[path[-1]] = copy.deepcopy(value)
    return d


def digest(text):
    return hashlib.sha256(text.encode("utf-8", "surrogatepass")).hexdigest()[:16]


def call(fn, key, d):
    try:
        if fn == "render_report":
            out = L.render_report(d)
        elif fn == "render_findings":
            out = L.render_findings(d)
        elif fn == "report":
            out = D.report(d)
        elif fn == "report_parts":
            out = json.dumps(D.report_parts(d))
        else:
            summary = D.detector_summary(d, key)
            out = "None" if summary is None else "str:" + summary
    except Exception as error:  # noqa: BLE001 -- check と同じく、例外の型を問わず誤りとして受ける
        return "E:" + type(error).__name__
    return digest(out)


def lint_base(item, words_dir):
    words = M.load_words(words_dir) if item["words"] else None
    d = L.lint_text(item["text"], doc_type=item["doc_type"], reader=item["reader"], words=words)
    if item["words"]:
        d["context"]["words"]["path"] = plan["words_path"]
    return d


with tempfile.TemporaryDirectory() as words_dir:
    for name, body in plan["words"].items():
        with open(os.path.join(words_dir, name), "w", encoding="utf-8") as handle:
            handle.write(body)
    bases = {}
    for item in plan["lint_inputs"]:
        bases["lint/" + item["name"]] = json.loads(json.dumps(lint_base(item, words_dir)))
for item in plan["diff_inputs"]:
    name = item["name"] + ("@" + item["doc_type"] if item["doc_type"] else "")
    bases["diff/" + name] = json.loads(json.dumps(D.diff(item["old"], item["new"], doc_type=item["doc_type"])))

mutations = []
for base_name, base in bases.items():
    fns = LINT_FNS if base_name.startswith("lint/") else DIFF_FNS
    plans = [((), "base")] + [(path, name) for path in paths(base) for name, _ in VARIANTS]
    for path, name in plans:
        d = base if name == "base" else mutate(base, path, name, dict(VARIANTS)[name])
        mutations.append({"base": base_name, "path": list(path), "variant": name, "results": [call(fn, key, d) for fn, key in fns]})


def sections(base, path):
    """path をたどる途中の dict (書き換える欄を持つ dict を含む) の kind と subkind (str のもの)。"""
    out, d = [], base
    for key in path:
        if isinstance(d, dict):
            out.extend((name, d[name]) for name in ("kind", "subkind") if isinstance(d.get(name), str))
        d = d[key]
    return tuple(out)


def kept(items):
    seen = set()
    for item in items:
        if item["variant"] == "base" or any(result.startswith("E:") for result in item["results"]):
            yield item
            continue
        group = (item["base"].split("/")[0], tuple(key for key in item["path"] if not isinstance(key, int)),
                 sections(bases[item["base"]], item["path"]), item["variant"])
        if group not in seen:
            seen.add(group)
            yield item


json.dump({"bases": bases, "mutations": mutations if plan.get("all") else list(kept(mutations))}, sys.stdout, ensure_ascii=True)
