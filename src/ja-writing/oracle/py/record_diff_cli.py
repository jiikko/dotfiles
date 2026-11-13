"""本家 yomiyasu_diff.py の CLI を 1 つのプロセスの中で何度も動かして、stdout・stderr・rc を記録する driver (record_lint_cli.py と同じ作り)。

標準入力: {"scripts_dir": "...", "cases": [{"name": "...", "args": [...], "cwd": 作業フォルダ (root からの相対。省くと root), "stdin_b64": 標準入力のバイト列, "keep": 真なら stdout も記録に残す}, ...]}
標準出力: [{"name": ..., "rc": 終了コード, "req": 検査の本体 diff() に渡した引数 (原文・書き直した文・立場・注記・種類・読者・語の一覧の context・資料) の正規の JSON の SHA-256 の先頭 16 字 (本体まで進んだ実行だけ), "out": stdout の SHA-256 の先頭 16 字, "len": stdout のバイト数, "err": stderr (全文), "n": 見直し候補の数か null,
          "kinds": --json の出力で何が出たか (検出の種類の並び。昇順・重複なし。出ない実行は省く), "stdout": keep のときだけ}, ...]

main(argv) を呼ぶ (python の sys.exit 相当の SystemExit は終了コードとして受ける)。stdin・stdout・stderr は 1 回ごとに作り直し、本物の CLI と同じく stdout は main が UTF-8・改行 "\n" に設定する。
文字列の中の root の実パスは "<<ROOT>>" に置き換える (語の一覧のパスなど。置き場所で記録が変わらないように)。
kinds は「当たる入力と当たらない入力が検出の種類ごとにある」ことを、記録から数えるための欄 (入力の集まりの空振りを止める検査が読む)。
"""
import base64
import hashlib
import io
import json
import os
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import yomiyasu_diff as D  # noqa: E402

ROOT = os.path.realpath(os.getcwd())
CAPTURED = []
_DIFF = D.diff


def _capturing_diff(orig_raw, rw_raw, stance=None, *, encoding_notes=None, doc_type=None, reader=None, words=None, materials=None):
    """_run が diff() に渡す値を控えて、本物の diff() をそのまま呼ぶ (出力は変えない)。Go の本体へ渡す値 (prepare の戻り値) と突き合わせる。"""
    CAPTURED.append({"orig": orig_raw, "rw": rw_raw, "stance": stance, "encoding_notes": list(encoding_notes or []), "doc_type": doc_type,
                     "reader": reader, "words": D.words_context(words) if words is not None else None, "materials": list(materials or [])})
    return _DIFF(orig_raw, rw_raw, stance, encoding_notes=encoding_notes, doc_type=doc_type, reader=reader, words=words, materials=materials)


D.diff = _capturing_diff
REAL_STDIN, REAL_STDOUT, REAL_STDERR = sys.stdin, sys.stdout, sys.stderr

RULE_KEYS = ("markers", "new_words", "lost_words", "structure", "spans", "logic", "bold", "type_changes", "added_actors", "heading_shapes")
SECTIONS = ("stiffness", "tone", "hedges", "immutable", "politeness", "register")


def unroot(text):
    return text.replace(ROOT, "<<ROOT>>")


def kinds_of(d):
    found = {"status:" + d["status"]}
    for r in d["meaning_risks"]:
        found.add("risk:%s/%s" % (r["kind"], r["subkind"]))
    for key in RULE_KEYS:
        if d.get(key):
            found.add("rule:" + key)
    for c in d["type_changes"]:
        for side in ("lost", "added"):
            for m in c[side]:
                found.add("type_change:%s/%s" % (side, m["kind"]))
    end = d["endings"]
    for key in ("changes", "flags", "orig_flags"):
        if end[key]:
            found.add("endings:" + key)
    full = d["full_diff"]
    for h in full["hunks"]:
        found.add("hunk:" + h["tag"])
        if h["whitespace_only"]:
            found.add("hunk:whitespace_only")
        if h["moved_to"] is not None or h["moved_from"] is not None:
            found.add("hunk:moved")
    for key in full["limits_applied"]:
        found.add("limit:" + (key if isinstance(key, str) else key.get("kind", "?")))
    if full["encoding_notes"]:
        found.add("encoding_notes")
    for name in SECTIONS:
        for c in d[name]["candidates"]:
            found.add("%s:%s/%s" % (name, c.get("kind", ""), c.get("subkind", "")))
    counts = d["tone"]["counts"]
    for key, value in counts["new"].items():
        if value != counts["old"][key]:
            found.add("tone_count:" + key)
    if d["politeness"]["applicable"]:
        found.add("politeness:applicable")
    if d["immutable"]["meaning_risk_count"]:
        found.add("immutable:meaning_risk")
    if d["comma_increase"] is not None:
        found.add("comma_increase")
    return sorted(found)


out = []
for case in plan["cases"]:
    os.chdir(os.path.join(ROOT, case.get("cwd", "")))
    sys.stdin = io.TextIOWrapper(io.BytesIO(base64.b64decode(case.get("stdin_b64", ""))), encoding="utf-8")
    so = io.TextIOWrapper(io.BytesIO(), encoding="utf-8", newline="\n", write_through=True)
    se = io.TextIOWrapper(io.BytesIO(), encoding="utf-8", newline="\n", write_through=True)
    sys.stdout, sys.stderr = so, se
    CAPTURED.clear()
    try:
        try:
            rc = D.main(list(case["args"]))
        except SystemExit as exc:
            rc = exc.code if isinstance(exc.code, int) else 0
    finally:
        sys.stdin, sys.stdout, sys.stderr = REAL_STDIN, REAL_STDOUT, REAL_STDERR
    so.flush()
    se.flush()
    stdout = unroot(so.buffer.getvalue().decode("utf-8"))
    stderr = unroot(se.buffer.getvalue().decode("utf-8"))
    raw = stdout.encode("utf-8")
    count = None
    kinds = None
    if stdout.startswith("{") and "--json" in case["args"]:
        d = json.loads(stdout)
        count = d["candidate_count"]
        kinds = kinds_of(d)
    entry = {"name": case["name"], "rc": rc, "out": hashlib.sha256(raw).hexdigest()[:16], "len": len(raw), "err": stderr, "n": count}
    if kinds is not None:
        entry["kinds"] = kinds
    if CAPTURED:
        assert len(CAPTURED) == 1, "1 回の実行で diff() が複数回呼ばれた"
        canon = json.dumps(CAPTURED[0], ensure_ascii=True, sort_keys=True, separators=(",", ":"))
        entry["req"] = hashlib.sha256(unroot(canon).encode("utf-8")).hexdigest()[:16]
    if case.get("keep"):
        entry["stdout"] = stdout
    out.append(entry)
os.chdir(ROOT)
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))
