"""本家の道具を入力に通して使われた re のパターンを集め、入力の単位に当てた一致を記録する driver。

標準入力: {"scripts_dir": "...", "files": ["<corpus 内の相対パス>", ...], "runs": [{"module": "...", "argv": [...], "stdin": "..."|null}, ...]}
cwd の corpus/ に入力が置かれている前提 (本家の道具は作業フォルダの外のファイルを読まない)。標準出力へ JSON を書く。

re.compile・re.search などは内部で re._compile を呼ぶので、そこを包めば、実行時に組み立てたパターンも取れる。
記録の単位は、ファイルごとの全文 ("<path>#doc") と "\n" で割った各行 ("<path>#<行番号>")。
パターンごとに、全単位の一致を並べた SHA-256 (digest) と、当たった単位・一致の数、最初の数単位の中身 (sample) を残す。
digest の並べ方は oracle/patterns.go の Digest と同じ。グループを持つパターンは、同じ並べ方で各一致の lastindex (None は -1) と lastgroup (None は "-") を並べた last_digest も残す
(LastDigest。span の digest には lastindex と lastgroup が入らない)。

本家のソース (scripts_dir の *.py。実行せず ast で読む) の re.xxx("文字列", flags) の literal は、道具を動かして使われなかったものも
パターンに足す ("static": true)。足し漏らさないよう、ast と本家のソースの字面の 2 通りで re.xxx( の呼び出しを数え、ast が読み切れなかった呼び出しは
UNREAD_OK (理由つき) にあるものだけを残してよいことにし、食い違えば止まる (static_literals)。
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
        left, right = literal_flags(node.left), literal_flags(node.right)
        return None if left is None or right is None else left | right
    return None  # 実行時の値 (self._flags など)。呼び出しは読み残しになる


# 本家のソースの re.xxx( の呼び出しの数え方を 2 つ持つ。ast は第 1 引数 (定数・定数どうしの +・同じ関数か module で文字列の定数を代入した名前) を読み、
# 本家のソースの行を字面で数える方 (CALL_TEXT) と行ごとの数が合わなければ止まる。ast が全部は読めない呼び出し (第 1 引数の一部が実行時の値で決まる) は
# ここに (ファイル, 行) と理由を書いたものだけが残ってよい。一覧に無い読み残しも、一覧にあって読み残しの無い (古くなった) 行も止まる。
# 本家は固定の commit (oracle/upstream.sha256) なので行番号で持つ。固定する commit を変えたら一覧を作り直す。
_ESCAPE = "re.escape(入力や語の一覧から来る語) をつなぐ。語は入力ごとに変わる"
_ARG = "関数の引数・表の要素・オブジェクトの属性・変数。値は呼び出し元で決まる"
_JOIN = "語やパターンを '|' でつなぐ。語は語の一覧や入力で変わる"
_PART = "定数に、入力や表から来る値をつなぐ"
UNREAD_OK = {}
for _reason, _places in (
    (_ESCAPE, {"markdown_visibility.py": (569, 100), "yomiyasu_diff.py": (4889, 4896, 4893, 6759, 7744, 7770, 8886, 8891, 8900, 9023), "yomiyasu_hook.py": (556,)}),
    (_ARG, {"markdown_visibility.py": (1235,), "yomiyasu_diff.py": (151, 2689, 6182, 9019), "yomiyasu_lint.py": (139, 1498, 1500, 1501, 5532, 5533, 5534, 5594, 5598, 5755, 5757)}),
    (_JOIN, {"yomiyasu_diff.py": (7294, 9505, 6203), "yomiyasu_lint.py": (1511, 5373, 4716, 4717)}),
    (_PART, {"yomiyasu_lint.py": (1996, 4872)}),
    ("bytes のパターン。記録は str のパターンだけを取る (recording_compile)", {"yomiyasu_check.py": (1174,)}),
    ("html_until の代入のうち '</' + tag + r'\\s*>' だけが実行時の値 (tag は入力の HTML のタグ名)。残りの 4 つの代入 ('-->'・r'\\?>'・r'\\]\\]>'・'>') は定数で集めた",
     {"markdown_visibility.py": (575, 630)}),
):
    for _name, _lines in _places.items():
        for _line in _lines:
            UNREAD_OK[(_name, _line)] = _reason

CALL_TEXT = re.compile(r"\bre\.(?:" + "|".join(FLAG_POSITION) + r")\(")
UNKNOWN = object()


def _names_in(node):
    return {n.id for n in ast.walk(node) if isinstance(n, ast.Name)}


def bindings(nodes, name):
    """nodes の中で name に束縛する式の一覧。代入以外の束縛 (引数・for・with・import・+= など) は UNKNOWN。"""
    out = []
    for node in nodes:
        if isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == name:
                    out.append(node.value)
                elif name in _names_in(target):
                    out.append(UNKNOWN)
        elif isinstance(node, ast.AnnAssign) and name in _names_in(node.target):
            out.append(node.value if node.value is not None and isinstance(node.target, ast.Name) else UNKNOWN)
        elif isinstance(node, ast.AugAssign) and name in _names_in(node.target):
            out.append(UNKNOWN)
        elif isinstance(node, (ast.For, ast.AsyncFor, ast.comprehension)) and name in _names_in(node.target):
            out.append(UNKNOWN)
        elif isinstance(node, ast.NamedExpr) and node.target.id == name:
            out.append(UNKNOWN)
        elif isinstance(node, ast.arg) and node.arg == name:
            out.append(UNKNOWN)
        elif isinstance(node, (ast.Import, ast.ImportFrom)) and any((a.asname or a.name).split(".")[0] == name for a in node.names):
            out.append(UNKNOWN)
        elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)) and node.name == name:
            out.append(UNKNOWN)
        elif isinstance(node, ast.ExceptHandler) and node.name == name:
            out.append(UNKNOWN)
        elif isinstance(node, ast.withitem) and node.optional_vars is not None and name in _names_in(node.optional_vars):
            out.append(UNKNOWN)
    return out


def evaluate(node, scopes, depth=0):
    """式がとりうる文字列の集合と、全部読めたか。読めるのは定数・定数どうしの +・scopes (内側の関数から module の順) で最初に束縛のある名前の代入の全部。"""
    if depth > 8:
        return set(), False
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return {node.value}, True
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        left, left_ok = evaluate(node.left, scopes, depth + 1)
        right, right_ok = evaluate(node.right, scopes, depth + 1)
        return {a + b for a in left for b in right}, left_ok and right_ok
    if isinstance(node, ast.Name):
        for nodes in scopes:
            found = bindings(nodes, node.id)
            if not found:
                continue
            values, ok = set(), True
            for value in found:
                if value is UNKNOWN:
                    ok = False
                    continue
                got, got_ok = evaluate(value, scopes, depth + 1)
                values |= got
                ok = ok and got_ok
            return values, ok
    return set(), False


def static_literals(scripts_dir, unread_ok):
    """ソースの re.xxx(...) の第 1 引数が読める (パターン, フラグ) を、出てくる順 (ファイル名順) に重複なしで返す。読めた呼び出しの数も返す。
    ast と字面の数え方が合わない・一覧に無い読み残し・古くなった一覧の行があれば止まる。"""
    found, calls, problems, unread_seen = {}, 0, [], set()
    for name in sorted(os.listdir(scripts_dir)):
        if not name.endswith(".py"):
            continue
        with open(os.path.join(scripts_dir, name), "rb") as handle:
            source = handle.read().decode("utf-8")
        tree = ast.parse(source, name)
        parent = {child: node for node in ast.walk(tree) for child in ast.iter_child_nodes(node)}
        module_nodes = list(ast.walk(tree))
        ast_count = {}
        for node in ast.walk(tree):
            if not (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and isinstance(node.func.value, ast.Name)
                    and node.func.value.id == "re" and node.func.attr in FLAG_POSITION):
                continue
            ast_count[node.lineno] = ast_count.get(node.lineno, 0) + 1
            scopes, cur = [], node
            while cur in parent:
                cur = parent[cur]
                if isinstance(cur, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    scopes.append(list(ast.walk(cur)))
            scopes.append(module_nodes)
            patterns, complete = evaluate(node.args[0], scopes) if node.args else (set(), False)
            position = FLAG_POSITION[node.func.attr]
            flag_node = node.args[position] if len(node.args) > position else None
            for keyword in node.keywords:
                if keyword.arg == "flags":
                    flag_node = keyword.value
            flags = literal_flags(flag_node)
            if flags is None:
                patterns, complete = set(), False
            calls += 1 if patterns else 0
            for pattern in sorted(patterns):
                found.setdefault((pattern, flags), (name, node.lineno))
            if not complete:
                key = (name, node.lineno)
                unread_seen.add(key)
                if key not in unread_ok:
                    problems.append("%s:%d の re.%s(%s) は第 1 引数を読み切れず、UNREAD_OK にもありません" % (name, node.lineno, node.func.attr, ast.unparse(node.args[0]) if node.args else ""))
        skipped = set()  # 字面の数えでは、コメントだけの行と docstring の行を数えない (呼び出しではない)
        for node in ast.walk(tree):
            if isinstance(node, ast.Expr) and isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
                skipped.update(range(node.lineno, node.end_lineno + 1))
        for number, text in enumerate(source.split("\n"), 1):
            text_count = 0 if number in skipped or text.lstrip().startswith("#") else len(CALL_TEXT.findall(text))
            if text_count != ast_count.get(number, 0):
                problems.append("%s:%d の re.xxx( が字面で %d 個、ast で %d 個: %s" % (name, number, text_count, ast_count.get(number, 0), text.strip()))
    for key in sorted(set(unread_ok) - unread_seen):
        problems.append("UNREAD_OK の %s:%d は読み残しの呼び出しが無い (一覧が古い)" % key)
    if problems:
        raise SystemExit("本家のソースの literal の集め方が、本家の呼び出しを取りこぼしています:\n" + "\n".join(problems))
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

static, static_calls = static_literals(plan["scripts_dir"], UNREAD_OK)
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
    last_digest = hashlib.sha256() if compiled.groups else None
    matched_units = matches = 0
    sample = []
    firsts = []
    for key, text in units:
        found = list(compiled.finditer(text))
        rows = [flat(compiled, m) for m in found]
        if not rows:
            continue
        if len(firsts) < WINDOW_UNITS:
            firsts.append((key, text, (rows[0][0], rows[0][1])))
        matched_units += 1
        matches += len(rows)
        digest.update((key + "\n").encode("utf-8"))
        for row in rows:
            digest.update((" ".join(str(v) for v in row) + "\n").encode("utf-8"))
        if last_digest is not None:
            last_digest.update((key + "\n").encode("utf-8"))
            for m in found:
                last_digest.update(("%d %s\n" % (-1 if m.lastindex is None else m.lastindex, "-" if m.lastgroup is None else m.lastgroup)).encode("utf-8"))
        if len(sample) < SAMPLE_UNITS:
            sample.append({"unit": key, "matches": rows[:SAMPLE_MATCHES]})
    window, window_calls = window_digest(compiled, firsts)
    entry = {"pattern": pattern, "flags": flags, "compiled_flags": compiled.flags,
                     "groups": compiled.groups, "groupindex": dict(compiled.groupindex),
                     "matched_units": matched_units, "matches": matches, "digest": digest.hexdigest(),
                     "sample": sample, "window_digest": window, "window_calls": window_calls}
    if last_digest is not None:
        entry["last_digest"] = last_digest.hexdigest()
    if (pattern, flags) in static:
        entry["static"] = True
    patterns.append(entry)

json.dump({"runs": runs, "static_calls": static_calls, "static_patterns": len(static), "patterns": patterns}, sys.stdout, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
