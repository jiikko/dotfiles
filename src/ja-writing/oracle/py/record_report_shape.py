"""本家の lint の render_report・render_findings と、diff の report・report_parts・detector_summary に、欄を 1 つずつ書き換えた dict を通し、
書き換え 1 つごとの結果を JSON で返す driver。check はこの 5 つに子の --json を読んだ dict を渡し、例外を「子の JSON から人が読む形を作れません」の
誤りにする。Go の側は「本家が例外 ⇔ Go が error」と、例外にならない呼び出しの戻り値の一致を確かめる。

標準入力: {"scripts_dir": "...", "diff_inputs": [{"name", "old", "new", "doc_type"}], "lint_inputs": [{"name", "text", "doc_type", "reader", "words"}],
          "words": {"allow.txt": 中身, ...}, "words_path": context.words.path に書く文字列, "detail": 省略可 {"base": 名前, "index": 書き換えの番号}}
標準出力: {"bases": [{"name", "d": 書き換える前の dict, "plan": 書き換えの並びの digest, "raised": 本家が例外になった呼び出しの数,
          "compared": 例外にならなかった呼び出しの数, "digests": [書き換えごとの digest]}]}
書き換えは代表を選ばず全部を記録に残す。代表を選ぶ鍵 (位置・節の kind と subkind) は、鍵に入れていない欄 (hunk の tag など) で分かれる分岐の退行を
落とし、欄を足しても次の欄で同じ形が残る。全部の呼び出しの結果 (例外の型か戻り値) をそのまま残すと約 11.6MB になるので、書き換え 1 つごとに、
その書き換えでの全部の呼び出しの結果 (例外なら "E"、ならなければ戻り値の SHA-256 の先頭 16 字) を "\n" でつないだものの SHA-256 の先頭 16 字を 1 つ残す
(例外の型は入れない: check は型を見ず、Go は型を持たない)。plan は書き換えの位置と種類の並び (json.dumps(path) + " " + 種類を "\n" でつないだもの) の
SHA-256 の先頭 16 字で、Go が同じ並びを作れているかを結果と分けて確かめる。
"detail" を渡すと、その 1 件の位置・種類と、呼び出しごとの結果 (例外の型か digest) と戻り値の文字列を返す (食い違った書き換えを調べる用)。

基準の dict は diff_inputs を本家の diff() に、lint_inputs を lint_text() に通した値 (語の一覧は一時フォルダに書いて読み、path を words_path に置き換える) を
JSON に書いて読み直したもの (diff() の値は節どうしで同じ list を共有することがあり、そのまま書き換えると 2 か所が変わる。check が読む子の出力は共有しない)。
書き換えの並びは、書き換えない基準 ("base") のあとに、dict の鍵と list の先頭 (添字 0) を深さ 5 までたどった各位置 (dict は鍵の順、位置ごとに中へ
たどる前) に、VARIANTS の 14 通り (del は鍵か要素を消す) を 1 つずつ当てたもの。
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
    """(例外なら "E:<例外の型>"、ならなければ戻り値の digest, 戻り値の文字列か None)。"""
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
        return "E:" + type(error).__name__, None
    return digest(out), out


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


def plans(base):
    return [((), "base")] + [(path, name) for path in paths(base) for name, _ in VARIANTS]


def run(base_name, base, path, name):
    fns = LINT_FNS if base_name.startswith("lint/") else DIFF_FNS
    d = base if name == "base" else mutate(base, path, name, dict(VARIANTS)[name])
    return [call(fn, key, d) for fn, key in fns]


def summary(results):
    return digest("\n".join("E" if result.startswith("E:") else result for result, _ in results))


detail = plan.get("detail")
if detail:
    path, name = plans(bases[detail["base"]])[detail["index"]]
    results = run(detail["base"], bases[detail["base"]], path, name)
    json.dump({"base": detail["base"], "index": detail["index"], "path": list(path), "variant": name, "digest": summary(results),
               "results": [result for result, _ in results], "outputs": [text for _, text in results]}, sys.stdout, ensure_ascii=True)
    sys.exit(0)

out = []
for base_name, base in bases.items():
    items = plans(base)
    entry = {"name": base_name, "d": base, "plan": digest("\n".join(json.dumps(list(path)) + " " + name for path, name in items)),
             "raised": 0, "compared": 0, "digests": []}
    for path, name in items:
        results = run(base_name, base, path, name)
        raised = sum(1 for result, _ in results if result.startswith("E:"))
        entry["raised"] += raised
        entry["compared"] += len(results) - raised
        entry["digests"].append(summary(results))
    out.append(entry)
json.dump({"bases": out}, sys.stdout, ensure_ascii=True)
