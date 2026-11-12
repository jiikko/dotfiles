"""本家の道具を入力に通して使われた re のパターンを集め、入力の単位に当てた一致を記録する driver。

標準入力: {"scripts_dir": "...", "files": ["<corpus 内の相対パス>", ...], "runs": [{"module": "...", "argv": [...], "stdin": "..."|null}, ...]}
cwd の corpus/ に入力が置かれている前提 (本家の道具は作業フォルダの外のファイルを読まない)。標準出力へ JSON を書く。

re.compile・re.search などは内部で re._compile を呼ぶので、そこを包めば、実行時に組み立てたパターンも取れる。
記録の単位は、ファイルごとの全文 ("<path>#doc") と "\n" で割った各行 ("<path>#<行番号>")。
パターンごとに、全単位の一致を並べた SHA-256 (digest) と、当たった単位・一致の数、最初の数単位の中身 (sample) を残す。
digest の並べ方は oracle/patterns.go の Digest と同じ。

本家のソース (scripts_dir の *.py。実行せず ast で読む) の re.xxx("文字列", flags) の literal は、道具を動かして使われなかったものも
パターンに足す ("static": true)。足し漏らさないよう、ast で数えた (パターン, フラグ) の数と記録の数を照合し、違えば止まる。
窓 (pos / endpos) つきの当たりは、パターンごとに最初に当たった WINDOW_UNITS 個の単位の最初の一致の位置のまわりの窓で search / match を呼び、
結果を window_digest にする (並べ方は oracle/patterns.go の WindowDigest と同じ)。
"""
import ast
import contextlib
import hashlib
import os
import io
import json
import re
import sys

SAMPLE_UNITS = 3
SAMPLE_MATCHES = 5
WINDOW_UNITS = 8

# re の関数ごとの、フラグを渡す位置 (0 から数える。キーワードの flags= も見る)
FLAG_POSITION = {"compile": 1, "search": 2, "match": 2, "fullmatch": 2, "finditer": 2, "findall": 2, "sub": 4, "subn": 4, "split": 3}


def literal_flags(node):
    if node is None:
        return 0
    if isinstance(node, ast.Constant) and isinstance(node.value, int):
        return node.value
    if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name) and node.value.id == "re":
        return int(getattr(re, node.attr))
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.BitOr):
        return literal_flags(node.left) | literal_flags(node.right)
    raise SystemExit("flags の式を読めません: " + ast.unparse(node))


def static_literals(scripts_dir):
    """ソースの re.xxx("文字列", ...) の (パターン, フラグ) を、出てくる順 (ファイル名順) に重複なしで返す。呼び出しの数も返す。"""
    found, calls = {}, 0
    for name in sorted(os.listdir(scripts_dir)):
        if not name.endswith(".py"):
            continue
        with open(os.path.join(scripts_dir, name), "rb") as handle:
            tree = ast.parse(handle.read().decode("utf-8"), name)
        for node in ast.walk(tree):
            if not (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and isinstance(node.func.value, ast.Name)
                    and node.func.value.id == "re" and node.func.attr in FLAG_POSITION and node.args):
                continue
            first = node.args[0]
            if not (isinstance(first, ast.Constant) and isinstance(first.value, str)):
                continue
            position = FLAG_POSITION[node.func.attr]
            flag_node = node.args[position] if len(node.args) > position else None
            for keyword in node.keywords:
                if keyword.arg == "flags":
                    flag_node = keyword.value
            calls += 1
            found.setdefault((first.value, literal_flags(flag_node)), (name, node.lineno))
    return found, calls

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])

seen = {}
real_compile = re._compile


def recording_compile(pattern, flags):
    if isinstance(pattern, str):
        key = (pattern, int(flags))
        if key not in seen:
            seen[key] = len(seen)
    return real_compile(pattern, flags)


re._compile = recording_compile


def run_tool(spec):
    module = __import__(spec["module"])
    out, err = io.StringIO(), io.StringIO()
    old_stdin = sys.stdin
    if spec.get("stdin") is not None:
        sys.stdin = io.TextIOWrapper(io.BytesIO(spec["stdin"].encode("utf-8")), encoding="utf-8", newline="")
    rc = None
    try:
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = module.main(list(spec["argv"]))
    except SystemExit as exit_:
        rc = exit_.code if isinstance(exit_.code, int) else 1
    except BaseException as exc:  # noqa: BLE001 - 落ちた run も記録して先へ進む
        rc = -1
        err.write(type(exc).__name__ + ": " + str(exc))
    finally:
        sys.stdin = old_stdin
    return {"module": spec["module"], "argv": spec["argv"], "rc": rc}


runs = [run_tool(spec) for spec in plan["runs"]]
re._compile = real_compile

static, static_calls = static_literals(plan["scripts_dir"])
for key in sorted(static, key=lambda k: static[k]):
    seen.setdefault(key, len(seen))

units = []
for rel in plan["files"]:
    with open("corpus/" + rel, "rb") as handle:
        text = handle.read().decode("utf-8")
    units.append((rel + "#doc", text))
    for number, line in enumerate(text.split("\n")):
        units.append((rel + "#" + str(number), line))


def flat(compiled, match):
    row = [match.start(), match.end()]
    for group in range(1, compiled.groups + 1):
        row.extend(match.span(group))
    return row


def window_digest(compiled, firsts):
    digest = hashlib.sha256()
    calls = 0
    for key, text, (start, end) in firsts:
        length = len(text)
        for pos in sorted({max(0, start - 1), start, min(length, start + 1)}):
            for endpos in sorted({max(0, end - 1), end, min(length, end + 1)}):
                for op in ("search", "match"):
                    match = getattr(compiled, op)(text, pos, endpos)
                    row = "-" if match is None else " ".join(str(v) for v in flat(compiled, match))
                    digest.update(("%s|%d|%d|%s|%s\n" % (key, pos, endpos, op, row)).encode("utf-8"))
                    calls += 1
    return digest.hexdigest(), calls


patterns = []
for (pattern, flags), _ in sorted(seen.items(), key=lambda item: item[1]):
    compiled = real_compile(pattern, flags)
    digest = hashlib.sha256()
    matched_units = matches = 0
    sample = []
    firsts = []
    for key, text in units:
        rows = [flat(compiled, m) for m in compiled.finditer(text)]
        if not rows:
            continue
        if len(firsts) < WINDOW_UNITS:
            firsts.append((key, text, (rows[0][0], rows[0][1])))
        matched_units += 1
        matches += len(rows)
        digest.update((key + "\n").encode("utf-8"))
        for row in rows:
            digest.update((" ".join(str(v) for v in row) + "\n").encode("utf-8"))
        if len(sample) < SAMPLE_UNITS:
            sample.append({"unit": key, "matches": rows[:SAMPLE_MATCHES]})
    window, window_calls = window_digest(compiled, firsts)
    entry = {"pattern": pattern, "flags": flags, "compiled_flags": compiled.flags,
                     "groups": compiled.groups, "groupindex": dict(compiled.groupindex),
                     "matched_units": matched_units, "matches": matches, "digest": digest.hexdigest(),
                     "sample": sample, "window_digest": window, "window_calls": window_calls}
    if (pattern, flags) in static:
        entry["static"] = True
    patterns.append(entry)

if sum(1 for entry in patterns if entry.get("static")) != len(static):
    raise SystemExit("ast で数えた literal のパターン %d 個と、記録の static の %d 個が違います" % (len(static), sum(1 for entry in patterns if entry.get("static"))))

json.dump({"runs": runs, "static_calls": static_calls, "static_patterns": len(static), "patterns": patterns}, sys.stdout, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
