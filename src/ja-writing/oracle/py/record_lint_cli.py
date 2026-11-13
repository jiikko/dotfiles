"""本家 yomiyasu_lint.py の CLI を 1 つのプロセスの中で何度も動かして、stdout・stderr・rc を記録する driver (起動のたびの費用を避ける)。

標準入力: {"scripts_dir": "...", "cases": [{"name": "...", "args": [...], "cwd": 作業フォルダ (root からの相対。省くと root), "stdin_b64": 標準入力のバイト列, "keep": 真なら stdout も記録に残す}, ...]}
標準出力: [{"name": ..., "rc": 終了コード, "out": stdout の SHA-256 の先頭 16 字, "len": stdout のバイト数, "err": stderr (全文), "n": 見直し候補の数か null,
          "raw": 検出の生の並び (種類・allow・ダッシュの上限で外す前) の件数 (lint_text まで進まなかった呼び出しは省く), "stdout": keep のときだけ}, ...]

main(argv) を呼ぶ (python の sys.exit 相当の SystemExit は終了コードとして受ける)。stdin・stdout・stderr は 1 回ごとに作り直し、本物の CLI と同じく stdout は main が UTF-8・改行 "\n" に設定する。
文字列の中の root の実パスは "<<ROOT>>" に置き換える (語の一覧のパスなど。置き場所で記録が変わらないように)。
"""
import base64
import hashlib
import io
import json
import os
import re
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import yomiyasu_lint as L  # noqa: E402

captured = {}
original = L._project_term_findings


def wrapper(analysis, words):
    found = original(analysis, words)
    # analysis["_findings"] はこれまでの全部の候補で、found (project_term の候補) は lint_text があとで足す
    captured["raw"] = len(analysis["_findings"]) + len(found)
    return found


L._project_term_findings = wrapper

ROOT = os.path.realpath(os.getcwd())
REAL_STDIN, REAL_STDOUT, REAL_STDERR = sys.stdin, sys.stdout, sys.stderr
COUNT = re.compile(r"見直し候補: (\d+)件")


def unroot(text):
    return text.replace(ROOT, "<<ROOT>>")


out = []
for case in plan["cases"]:
    os.chdir(os.path.join(ROOT, case.get("cwd", "")))
    captured.clear()
    sys.stdin = io.TextIOWrapper(io.BytesIO(base64.b64decode(case.get("stdin_b64", ""))), encoding="utf-8")
    so = io.TextIOWrapper(io.BytesIO(), encoding="utf-8", newline="\n", write_through=True)
    se = io.TextIOWrapper(io.BytesIO(), encoding="utf-8", newline="\n", write_through=True)
    sys.stdout, sys.stderr = so, se
    try:
        try:
            rc = L.main(list(case["args"]))
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
    if stdout.startswith("{") and "--json" in case["args"]:
        count = json.loads(stdout)["candidate_count"]
    else:
        found = COUNT.search(stdout)
        if found:
            count = int(found.group(1))
    entry = {"name": case["name"], "rc": rc, "out": hashlib.sha256(raw).hexdigest()[:16], "len": len(raw), "err": stderr, "n": count}
    if "raw" in captured:
        entry["raw"] = captured["raw"]
    if case.get("keep"):
        entry["stdout"] = stdout
    out.append(entry)
os.chdir(ROOT)
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))
