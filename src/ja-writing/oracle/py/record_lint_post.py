"""本家 lint_text の検出より後の処理 (種類で外す・allow・ダッシュの上限・点数・件数・context・metrics) と、出力 (render_report・--json の形) を、作った候補で記録する driver。

標準入力: {"scripts_dir": "...", "cases": [{"name": "...", "text": 入力の文書, "doc_type": 名前か null, "reader": 名前か null, "words": 語の一覧のフォルダ名か null,
          "inject": [候補の dict, ...]}, ...]}
標準出力: [{"name": ..., "natural": 本家の検出が自分で出した候補の数 (0 でなければ、作った候補だけの結果にならない), "result": lint_text の戻り値の正規の JSON の SHA-256 の先頭 16 字,
           "report": render_report の戻りの SHA-256 の先頭 16 字,
           "json": --json が出す json.dumps(result, ensure_ascii=True, indent=2) の SHA-256 の先頭 16 字 (鍵の順を並べ直さない), "score": rule_penalty_score}, ...]

lint_text の最後の検出 _project_term_findings を、「作った候補を返す」関数に差し替える (本家のファイルには手を入れない)。そのあとの処理は本家のまま走る。
作った候補の内部の欄 (_words など) の list は tuple にする (本家は tuple で作る)。
"""
import hashlib
import json
import os
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import markdown_visibility as V  # noqa: E402
import yomiyasu_lint as L  # noqa: E402

state = {"inject": [], "natural": 0}
original_project_term_findings = L._project_term_findings


def injected(analysis, words):
    # 差し替えた最後の検出 (project_term) が本来出す候補も数える (差し替えで消えた候補を「出さなかった」と記録しない)
    state["natural"] = len(analysis["_findings"]) + len(original_project_term_findings(analysis, words))
    return [fix(dict(f)) for f in state["inject"]]


def fix(finding):
    for key in ("_words", "_objects", "_flag_hits"):
        if key in finding:
            finding[key] = tuple(finding[key])
    if "_word_types" in finding:
        finding["_word_types"] = tuple(tuple(pair) for pair in finding["_word_types"])
    if "_pattern_matches" in finding:
        finding["_pattern_matches"] = {name: tuple(strings) for name, strings in finding["_pattern_matches"].items()}
    return finding


L._project_term_findings = injected


ROOT = os.path.realpath(os.getcwd())


def short(text):
    # 語の一覧のパスは作業フォルダの実パスを含むので、置き場所で記録が変わらないよう印にしてから数える
    return hashlib.sha256(text.replace(ROOT, "<<ROOT>>").encode("utf-8")).hexdigest()[:16]


out = []
for case in plan["cases"]:
    words = V.resolve_words(V.current_directory(), case["words"], False) if case.get("words") else None
    state["inject"] = case.get("inject", [])
    result = L.lint_text(case["text"], doc_type=case.get("doc_type"), reader=case.get("reader"), words=words)
    canonical = json.dumps(result, sort_keys=True, ensure_ascii=True, separators=(",", ":"))
    out.append({"name": case["name"], "natural": state["natural"], "result": short(canonical), "report": short(L.render_report(result)),
                "json": short(json.dumps(result, ensure_ascii=True, indent=2)), "score": result["rule_penalty_score"]})
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))
