"""本家 yomiyasu_lint.py の検出の生の並び (種類・allow・ダッシュの上限で外す前の候補) を記録する driver。

標準入力: {"scripts_dir": "...", "cases": [{"name": "...", "path": "corpus/....md", "doc_type": null か名前, "words": 語の一覧のフォルダ名か null}, ...]}
標準出力: [{"name": ..., "n": 候補の数, "findings": "規則:行:digest ...", "metrics": 結果の metrics の欄の digest}, ...] (cases と同じ順)

lint_text の最後の検出 _project_term_findings を包み、呼ばれた時点の analysis["_findings"] (それまでの全部の候補。内部の欄 _words などつき) と、
自分の戻り値 (project_term の候補) をつなぐ。本家のファイルには手を入れない (module の名前を差し替える。lint_text は module の global から名前を引く)。
candidate の digest は、内部の欄を含む候補の dict を正規の JSON (sort_keys・ensure_ascii・separators=(",", ":")。tuple は配列) にした SHA-256 の先頭 8 字。
"""
import copy
import hashlib
import json
import os
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import markdown_visibility as V  # noqa: E402
import yomiyasu_lint as L  # noqa: E402

captured = {}
original = L._project_term_findings


def wrapper(analysis, words):
    captured["before"] = copy.deepcopy(analysis["_findings"])
    found = original(analysis, words)
    captured["after"] = copy.deepcopy(found)
    return found


L._project_term_findings = wrapper

ROOT = os.getcwd()


def digest(finding):
    text = json.dumps(finding, sort_keys=True, ensure_ascii=True, separators=(",", ":"))
    return hashlib.sha256(text.encode("ascii")).hexdigest()[:8]


out = []
for case in plan["cases"]:
    with open(os.path.join(ROOT, case["path"]), "rb") as handle:
        text = V.decode_source(handle.read())[0]
    words = None
    if case.get("words"):
        words = V.resolve_words(V.current_directory(), case["words"], False)
    captured.clear()
    result = L.lint_text(text, doc_type=V.normalize_doc_type(case.get("doc_type")), reader=None, words=words)
    findings = captured["before"] + captured["after"]
    out.append({"name": case["name"], "n": len(findings), "metrics": digest(result["metrics"]),
                "findings": " ".join("%s:%d:%s" % (f["rule"], f["line"], digest(f)) for f in findings)})
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))
