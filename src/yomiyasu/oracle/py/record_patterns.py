"""本家の道具を入力に通して使われた re のパターンを集め、入力の単位に当てた一致を記録する driver。

標準入力: {"scripts_dir": "...", "files": ["<corpus 内の相対パス>", ...], "runs": [{"module": "...", "argv": [...], "stdin": "..."|null}, ...]}
cwd の corpus/ に入力が置かれている前提 (本家の道具は作業フォルダの外のファイルを読まない)。標準出力へ JSON を書く。

re.compile・re.search などは内部で re._compile を呼ぶので、そこを包めば、実行時に組み立てたパターンも取れる。
記録の単位は、ファイルごとの全文 ("<path>#doc") と "\n" で割った各行 ("<path>#<行番号>")。
パターンごとに、全単位の一致を並べた SHA-256 (digest) と、当たった単位・一致の数、最初の数単位の中身 (sample) を残す。
digest の並べ方は oracle/patterns.go の Digest と同じ。
"""
import contextlib
import hashlib
import io
import json
import re
import sys

SAMPLE_UNITS = 3
SAMPLE_MATCHES = 5

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


patterns = []
for (pattern, flags), _ in sorted(seen.items(), key=lambda item: item[1]):
    compiled = real_compile(pattern, flags)
    digest = hashlib.sha256()
    matched_units = matches = 0
    sample = []
    for key, text in units:
        rows = [flat(compiled, m) for m in compiled.finditer(text)]
        if not rows:
            continue
        matched_units += 1
        matches += len(rows)
        digest.update((key + "\n").encode("utf-8"))
        for row in rows:
            digest.update((" ".join(str(v) for v in row) + "\n").encode("utf-8"))
        if len(sample) < SAMPLE_UNITS:
            sample.append({"unit": key, "matches": rows[:SAMPLE_MATCHES]})
    patterns.append({"pattern": pattern, "flags": flags, "compiled_flags": compiled.flags,
                     "groups": compiled.groups, "groupindex": dict(compiled.groupindex),
                     "matched_units": matched_units, "matches": matches, "digest": digest.hexdigest(),
                     "sample": sample})

json.dump({"runs": runs, "patterns": patterns}, sys.stdout, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
