"""新しい関数・型に名前の近い既存の定義を dupscan index で引いて注入する hook の本体。入口は 2 つ。

- edit: PreToolUse(Write / Edit)。書いた後のファイルと書く前のファイルを比べる (`inject-dupscan-candidates-on-new-func.sh`)
- commit: PostToolUse(Bash)。いま作った commit の親の木と HEAD の木を比べる (`inject-dupscan-candidates-on-commit.sh`)。
  Bash (python / sed / heredoc) で足した関数は edit の入口を通らないので、commit の後に拾う

判定 (新しい関数の見つけ方・名前の照合・予算・印・計測のログ) は両方の入口で同じものを使う。照合を入口ごとに写さない。
起こし方: python3 -I -B dupscan_inject.py <edit|commit> <この lib の dir> [commit の候補の dir ...]。stdin に hook の JSON。
🚨 止めない (additionalContext だけ)。どんな失敗も出力なしで終える (shell の側が rc を 0 にする)。
"""
import json, os, re, subprocess, sys, time

# 時間の上限は処理の始まりから数える (宣言の抽出・git の問い合わせも含める。hook の timeout は 10 秒)
START = time.monotonic()
# 印の読み書きは他の hook と共有の lib (hooks/lib/session_marks.py) に置く。読めなければ印なしで続ける (同じ名前を引き直すだけ)
# import で hooks/lib/__pycache__ を作らない (本体の checkout に untracked のファイルが残る。起こす側も -B を付ける)
sys.dont_write_bytecode = True
try:
    sys.path.insert(0, sys.argv[2])
    import session_marks
except Exception:
    session_marks = None

BUDGET = 6.0

def left():
    return BUDGET - (time.monotonic() - START)
SKIP_NAMES = {"init", "main", "string", "error", "close", "body", "description", "hash", "isequal", "equal", "less", "len", "swap", "reset", "unwrap"}
SKIP_PREFIX = ("test", "benchmark", "fuzz", "example")
FUNC_GO = re.compile(r"^func\s+([A-Za-z_][A-Za-z0-9_]*)\s*[\[(]", re.M)
FUNC_SWIFT = re.compile(r"^\s*(?:@\w+\s+|(?:public|internal|private|fileprivate|open|static|class|final|override|mutating|nonisolated)\s+)*func\s+([A-Za-z_][A-Za-z0-9_]*)\s*[<(]", re.M)
TYPE_SWIFT = re.compile(r"^\s*(?:@\w+\s+|(?:public|package|internal|private|fileprivate|open|final|indirect)\s+)*(?:class|struct|actor|enum|protocol)\s+([A-Za-z_][A-Za-z0-9_]*)", re.M)
TYPE_GO = re.compile(r"^type\s+([A-Za-z_][A-Za-z0-9_]*)[\s\[]", re.M)
# `class func` / `class var` は型の宣言ではない (TYPE_SWIFT の名前の位置に来る語)
NOT_TYPE = {"func", "var", "let", "subscript", "init"}
# git grep -E -w に渡す宣言の形 (TYPE_SWIFT / TYPE_GO と同じ形。名前は後ろに足す)
GREP_SWIFT = r"^[[:space:]]*(@[A-Za-z_]+[[:space:]]+|(public|package|internal|private|fileprivate|open|final|indirect)[[:space:]]+)*(class|struct|actor|enum|protocol)[[:space:]]+"
GREP_GO = r"^type[[:space:]]+"
# シェルの単引用符の中なので、単引用符と三重引用符は文字で書かない
SQ, DQ, TRIPLE = chr(39), chr(34), chr(34) * 3

def code_only(src, swift):
    """文字列リテラルとコメントの中身を空白にした src (改行は残すので、行頭の宣言の判定は変わらない)。閉じない文字列・コメントは末尾まで消す。"""
    blank = lambda t: re.sub(r"[^\n]", " ", t)
    out, i, n = [], 0, len(src)
    while i < n:
        if src.startswith("//", i):
            j = src.find("\n", i)
            j = n if j < 0 else j
        elif src.startswith("/*", i):
            depth, j = 1, i + 2
            while j < n and depth:
                if src.startswith("*/", j):
                    depth, j = depth - 1, j + 2
                elif swift and src.startswith("/*", j):
                    depth, j = depth + 1, j + 2
                else:
                    j += 1
        elif swift and (src.startswith(TRIPLE, i) or (src[i] == "#" and src[i:].lstrip("#").startswith(DQ))):
            # Swift の raw string (#"…"# / #"""…"""#) は同じ数の # で閉じ、中にエスケープが無い。ふつうの三重引用符は \""" を閉じと読まない
            k = len(src[i:]) - len(src[i:].lstrip("#"))
            triple = src.startswith(TRIPLE, i + k)
            close = (TRIPLE if triple else DQ) + "#" * k
            j = i + k + (3 if triple else 1)
            if k:
                j = src.find(close, j)
                j = n if j < 0 else j + len(close)
            else:
                while j < n and not src.startswith(TRIPLE, j):
                    j += 2 if src[j] == "\\" else 1
                j = min(j + 3, n)
        elif not swift and src[i] == "`":
            j = src.find("`", i + 1)
            j = n if j < 0 else j + 1
        elif src[i] == DQ or (not swift and src[i] == SQ):
            q, j = src[i], i + 1
            while j < n and src[j] not in (q, "\n"):
                j += 2 if src[j] == "\\" else 1
            j = min(j + 1, n)
        else:
            out.append(src[i])
            i += 1
            continue
        out.append(blank(src[i:j]))
        i = j
    return "".join(out)

# 印は署名の後ろ・呼び出しの数の前に付く (`… int [test,unexported] (呼び出し ~2)`)。署名の中の `map[string]` を印と読まないよう位置で絞る
TAGS = re.compile(r"\s\[([a-z,]+)\](?=\s+\(呼び出し|\s+//|\s*$)")


def go_module(root, rel_path):
    """rel_path (root からの相対) を含む Go の module の dir (一番近い go.mod の dir)。無ければ ""。"""
    d = os.path.dirname(rel_path)
    while d:
        if os.path.isfile(os.path.join(root, d, "go.mod")):
            return d
        d = os.path.dirname(d)
    return ""


def reachable(root, hit_path, line, rel, ext):
    """書いているファイルの側から使える候補か。書くファイルと違う言語の定義は、呼ぶことも寄せることもできない (dupscan index は
    言語で絞れず、Swift の app と Go の道具が同じ repo にあると、Go の test の fixture に Swift の test の fixture が並ぶ)。
    別の Go の module のテストの非公開の helper も、呼ぶことも、共通の置き場へ寄せることもできない (module ごとに別の道具)。
    同じ module の中なら、別の package でも共通のテストの置き場へ寄せる候補になるので残す。
    Swift は module が dir で決まらないので見ない。"""
    if os.path.splitext(hit_path)[1] != ext:
        return False
    if ext != ".go":
        return True
    tags = TAGS.search(line)
    if not tags or not {"test", "unexported"} <= set(tags.group(1).split(",")):
        return True
    return go_module(root, hit_path) == go_module(root, rel)


LINE = re.compile(r"^(\S+?):(\d+)(?:-\d+)?\s+(?:[A-Za-z_][A-Za-z0-9_]*\.)*([A-Za-z_][A-Za-z0-9_]*)\s*[(<\[]")

def stems(name):
    """name の語の境目 (大文字・_ の前) での前方で、6 文字以上のもの (長い順)。name 自身は含まない。"""
    cuts = [i for i in range(6, len(name)) if name[i].isupper() or name[i] == "_"]
    return [name[:i].rstrip("_") for i in reversed(cuts)]

def words(name):
    """語の数。大文字の連なりはそれに続く数字まで 1 語 (RESUME・BASE64・HTTPGet の HTTP)、大文字 1 つとそれに続く小文字・数字を 1 語にする (loadSettings は 2 語)。"""
    return len(re.findall(r"[A-Z]+[0-9]*(?![a-z])|[A-Z]?[a-z0-9]+", name))

def near(existing, new):
    a, b = existing.lower(), new.lower()
    if a == b:
        return True
    short = existing if len(existing) <= len(new) else new
    return (a.startswith(b) or b.startswith(a)) and len(short) >= 6 and words(short) >= 2

# 戻り型で引かない汎用の型 (引くと同じ型を返す無関係な関数が並ぶ)。比べる前に小文字にする
GENERIC_RETURNS = {"error", "string", "bool", "int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "byte", "rune", "any",
                   "void", "self", "double", "float", "data", "character", "never", "some", "struct", "interface", "func", "map", "chan",
                   "element", "value", "key", "wrapped", "output"}

def index_lines(args, scope, left):
    """dupscan index の stdout の行。時間切れはこの問い合わせだけを空にする (例外で hook 全体が黙り、それまでの候補まで捨てる形にしない)。"""
    try:
        r = subprocess.run(["dupscan", "index"] + args + ["-doc-bytes", "60", scope], capture_output=True, text=True, timeout=left)
    except subprocess.TimeoutExpired:
        return []
    return r.stdout.splitlines()

def return_type(src, start, swift):
    """src の start (関数の名前の位置) から始まる署名の戻り型の字面。取れない・汎用の型なら None。
    Go は引数の閉じ括弧から `{` までで、複数の戻りなら error でない最初のもの。Swift は `->` の後ろ。ポインタ・配列・optional の記号は外す。"""
    i = src.find("(", start)
    if i < 0:
        return None
    depth = 0
    for j in range(i, len(src)):
        c = src[j]
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                break
    else:
        return None
    # 署名の残りは同じ行の `{` まで (本体の無い protocol の要件では、次の行の別の要件の戻り型を読まない)
    tail = re.split(r"[{\n]", src[j + 1:], maxsplit=1)[0].strip()
    if swift:
        if "->" not in tail:
            return None
        t = tail.split("->", 1)[1].split(" where ")[0].strip()
    else:
        t = tail
        if t.startswith("(") and t.endswith(")"):
            parts = [p.strip() for p in t[1:-1].split(",") if p.strip()]
            if any(len(p.split()) > 1 for p in parts):  # 名前付きの戻り (lo, hi int) は名前を飛ばし、型の付いた要素の型だけを見る
                parts = [p.split()[-1] for p in parts if len(p.split()) > 1]
            parts = [p for p in parts if p != "error"]
            t = parts[0] if parts else ""
    t = t.lstrip("*&[]").rstrip("?!").strip()
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_.]*", t or ""):
        return None
    t = t.split(".")[-1]  # 修飾 (Swift.String・context.Context) を外してから汎用の型かを見る
    if t.lower() in GENERIC_RETURNS or len(t) <= 2:  # 1〜2 文字は型引数 (T・U・ID の類) で、dupscan の部分一致では大半の型に当たる
        return None
    return t

def warn(cache, msg):
    """想定外の失敗を stderr と計測ログ (3 列の warn 行。scripts/dupscan_usage_report.py は 4〜5 列の行だけを数える) へ出す。
    rc=0 の hook の stderr はモデルにも画面にも出ないので、stderr だけにしない。"""
    print("dupscan-inject: " + msg, file=sys.stderr)
    try:
        with open(os.path.join(cache, "claude-dupscan-inject.log"), "a") as f:
            f.write("%s\twarn\t%s\n" % (time.strftime("%Y-%m-%dT%H:%M:%S%z"), re.sub(r"[\t\n]", " ", msg)))
    except OSError:
        pass

def new_declarations(before_src, after_src, ext):
    """after にあって before に無い関数の名前 (と戻り型) と型の名前。before / after は code_only を通した文字列。"""
    rx = FUNC_GO if ext == ".go" else FUNC_SWIFT
    trx = TYPE_GO if ext == ".go" else TYPE_SWIFT
    swift = ext == ".swift"
    known = {m.group(1) for m in rx.finditer(before_src)}
    known_types = {m.group(1) for m in trx.finditer(before_src)}
    types = []
    for m in trx.finditer(after_src):
        t = m.group(1)
        if t not in known_types and t not in types and t not in NOT_TYPE:
            types.append(t)
    names, returns = [], {}
    for m in rx.finditer(after_src):
        n = m.group(1)
        if n in known or n in names or len(n) < 5 or n.lower() in SKIP_NAMES or n.lower().startswith(SKIP_PREFIX):
            continue
        names.append(n)
        returns[n] = return_type(after_src, m.start(1), swift)
    return names, returns, types


class Marks:
    """1 セッションで 1 回だけ引く印 (edit と commit の入口で共有する)。"""
    def __init__(self, d):
        self.session = re.sub(r"[^A-Za-z0-9_-]", "_", str(d.get("session_id") or "unknown"))
        self.cache = os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")
        self.path = os.path.join(self.cache, "claude-dupscan-inject.seen")
        self.on_warn = lambda m: warn(self.cache, m)
        self.seen = {k + "\t" + v for k, v in session_marks.load(self.path, self.session, self.on_warn)} if session_marks else set()

    def take(self, key):
        """key を初めて見たなら印を付けて True。"""
        if key in self.seen:
            return False
        self.seen.add(key)
        if session_marks:
            session_marks.put(self.path, self.session, *key.split("\t", 1), on_warn=self.on_warn)
        return True


def lookup(root, rel, ext, names, returns, types, marks, exclude_self):
    """名前・型ごとに既存の候補を引く。(関数の候補の段落, 型の候補の段落, 計測の行)。
    exclude_self: 足した定義そのもの (rel の中の同じ名前) を候補から除く。commit の後は index が足した定義も読むので、
    除かないと自分が最初に当たり、短い既存の名前を引く段に進まない。除くのは打ち切りの判定の前。"""
    scope = os.path.join(root, "src") if ext == ".go" and os.path.isdir(os.path.join(root, "src")) else root
    out, logged, type_out = [], [], []
    for t in types[:3]:
        if left() <= 0.5:
            break
        if not marks.take("type\t" + t):
            continue
        pat = (GREP_GO if ext == ".go" else GREP_SWIFT) + t
        try:
            r = subprocess.run(["git", "-C", root, "grep", "-n", "-I", "-E", "-w", "--untracked", "-e", pat, "--", "*" + ext], capture_output=True, text=True, timeout=left())
        except subprocess.TimeoutExpired:
            continue
        hits = [h for h in r.stdout.splitlines() if not h.startswith(rel + ":")]
        if hits:
            type_out.append("`%s` と同じ名前の型の宣言が既にある:\n%s" % (t, "\n".join("  " + h.strip() for h in hits[:3])))
    def is_self(m):
        return exclude_self and m.group(1) == rel and m.group(3) in names
    for n in names[:3]:
        if left() <= 0.5:
            break
        if not marks.take("name\t" + n):
            continue
        hits, how = [], "name"
        # index -name は「既存の名前が引く語を含む」ものを返すので、引く名前より短い既存の名前 (collectWidgets に対する collectWidgetsFast) は
        # 名前そのものでは引けない。名前の語の境目の前方 (長い順に最大 2 つ) も引き、既存の名前が前方になっているものを拾う。
        for q in [n] + stems(n)[:2]:
            if left() <= 0.5:
                break
            for line in index_lines(["-name", q, "-limit", "50" if q != n else "20"], scope, left()):
                m = LINE.match(line)
                if not m or line in hits or is_self(m):
                    continue
                if near(m.group(3), n) and reachable(root, m.group(1), line, rel, ext):
                    hits.append(line)
            if hits:
                break
        # 戻り型が取れたら、名前の照合の後に残りの予算で、名前と戻り型の組み合わせでも引いて先頭へ置く。名前だけだと、同じ名前で
        # 別の型を返す定義 (型ごとの location など) が上位 20 件を埋めて目的の定義が落ちる。名前の照合より先に引かない: 1 回が 2 秒かかる
        # 大きい repo では、先に引くと名前の照合が予算切れで走らなくなる
        t = returns.get(n)
        if t and left() > 0.5:
            typed = []
            for line in index_lines(["-name", n, "-returns", t, "-limit", "20"], scope, left()):
                m = LINE.match(line)
                if m and line not in typed and not is_self(m) and near(m.group(3), n) and reachable(root, m.group(1), line, rel, ext):
                    typed.append(line)
            if any(line not in hits[:3] for line in typed[:3]):
                how = "returns"
            hits = typed + [h for h in hits if h not in typed]
        logged.append((n, len(hits), how))
        if hits:
            out.append("`%s` に近い既存の定義:\n%s" % (n, "\n".join("  " + h for h in hits[:3])))
    return out, type_out, logged


def write_log(marks, logged, entry):
    """計測の行: 時刻 session 名前 候補数 引き方 (name / returns) 入口 (edit / commit)。scripts/dupscan_usage_report.py が数える。"""
    if not logged:
        return
    try:
        os.makedirs(marks.cache, exist_ok=True)
        with open(os.path.join(marks.cache, "claude-dupscan-inject.log"), "a") as f:
            for n, c, how in logged:
                f.write("%s\t%s\t%s\t%d\t%s\t%s\n" % (time.strftime("%Y-%m-%dT%H:%M:%S%z"), marks.session, n, c, how, entry))
    except OSError:
        pass


TYPES_NOTE = "(Go は同じ package、Swift は同じ module の top-level どうしか同じ型の中なら build で衝突する。Swift の private / fileprivate の型は同じファイルの中だけ。別の型なら名前を変え、同じものなら再利用する)"

def emit(event, parts):
    if parts:
        print(json.dumps({"hookSpecificOutput": {"hookEventName": event, "additionalContext": "\n".join(parts)}}, ensure_ascii=False))


def git_out(args, timeout=3):
    """git の stdout (rc≠0 なら None)。バイトで受けて surrogateescape で読む: text モードは UTF-8 でないパスで例外を出して全体を黙らせ、
    パスの CR を LF に変える。読んだパスはそのまま git の引数に戻せる (subprocess が同じ形でバイトに戻す)。"""
    r = subprocess.run(["git"] + args, capture_output=True, timeout=timeout)
    return r.stdout.decode("utf-8", "surrogateescape") if r.returncode == 0 else None


def edit_main(d):
    ti = d.get("tool_input") or {}
    path = ti.get("file_path") or ""
    ext = os.path.splitext(path)[1]
    if ext not in (".go", ".swift") or not os.path.isabs(path):
        return
    new = ti.get("new_string") if "new_string" in ti else ti.get("content")
    if not new:
        return
    swift = ext == ".swift"
    old = ti.get("old_string") or ""
    current = None
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            current = f.read()
    except OSError:
        pass
    if "new_string" not in ti:
        after = new
    elif current is not None and old in current:
        after = current.replace(old, new, -1 if ti.get("replace_all") else 1)
    else:
        after = None
    after_src = code_only(after, swift) if after is not None else code_only(new, swift)
    before_src = code_only(old, swift) + "\n" + (code_only(current, swift) if current is not None else "")
    names, returns, types = new_declarations(before_src, after_src, ext)
    if not names and not types:
        return
    dirpath = os.path.dirname(path)
    while not os.path.isdir(dirpath) and dirpath != "/":
        dirpath = os.path.dirname(dirpath)
    # rc を見ずに出力を使う (root を出して rc≠0 を返す git のラッパーでも引く)
    root = subprocess.run(["git", "-C", dirpath, "rev-parse", "--show-toplevel"], capture_output=True, text=True, timeout=3).stdout.strip()
    if not root:
        return
    # git の root は symlink を解いた実パス (macOS の /var → /private/var) なので、書くファイルも実パスにしてから比べる
    real = os.path.realpath(path)
    if subprocess.run(["git", "-C", root, "check-ignore", "-q", "--", real], capture_output=True, timeout=3).returncode == 0:
        return
    marks = Marks(d)
    out, type_out, logged = lookup(root, os.path.relpath(real, root), ext, names, returns, types, marks, exclude_self=False)
    write_log(marks, logged, "edit")
    parts = []
    if out:
        parts.append("dupscan: これから足す関数に名前が近い既存の定義がある。再利用・既存の拡張で足りるか見てから書く (別の役割なら無視してよい。署名の近さは `dupscan index -returns 型` / `search` で引く):\n" + "\n".join(out))
    if type_out:
        parts.append("これから足す型と同じ名前の型が repo にある " + TYPES_NOTE + ":\n" + "\n".join(type_out))
    emit("PreToolUse", parts)


# reflog の先頭が commit で、この秒数より新しいときだけ「今の commit」とみなす (heredoc の本文の `git -C … commit` が行き先を足しても、
# その repo で今 commit していなければ何もしない)
FRESH_SECONDS = 120

def commit_main(d, dirs):
    """dirs (commit の呼び出しの行き先。cwd は `.`) のうち、今 commit した repo の HEAD の差分から新しい関数を拾って引く。"""
    cwd = d.get("cwd") or os.getcwd()
    roots, seen_dirs = [], set()
    for x in dirs:
        if not x:
            continue
        x = os.path.normpath(x if os.path.isabs(x) else os.path.join(cwd, x))
        # 同じ行き先を何度も問い合わせない (heredoc の本文に git commit の行を並べると、行の数だけ rev-parse を打って時間の上限を使い切る)
        if x in seen_dirs or not os.path.isdir(x) or left() <= 0.5:
            continue
        seen_dirs.add(x)
        r = (git_out(["-C", x, "rev-parse", "--show-toplevel"]) or "").strip()
        if r and r not in roots:
            roots.append(r)
    marks = None
    commit_parts, type_parts, logged_all = [], [], []
    for root in roots:
        if left() <= 0.5:
            break
        entry = (git_out(["-C", root, "reflog", "-1", "--date=unix", "--format=%gd%x09%gs"]) or "").strip()
        m = re.match(r"HEAD@\{(\d+)\}\t(commit(?: \((?:amend|initial)\))?):", entry)
        # 未来の時刻 (GIT_COMMITTER_DATE を先の日付にした commit) も今の commit と読まない。数秒の時計のずれは許す
        if not m or not -5 <= time.time() - int(m.group(1)) <= FRESH_SECONDS:
            continue
        info = (git_out(["-C", root, "rev-list", "--parents", "-n", "1", "HEAD"]) or "").split()
        if not info or len(info) > 2:  # merge の commit は見ない
            continue
        sha, parent = info[0], (info[1] if len(info) == 2 else None)
        marks = marks or Marks(d)
        if not marks.take("commit\t" + sha):
            continue
        # 移動したファイルは移動元と比べる (-M。比べないと、移しただけの関数を足したものと読む)。
        # -z で受け取る: 日本語・タブ・引用符を含むパスは、-z が無いと引用符で囲まれて出て、移動元を引けない
        diff_args = ["-C", root, "diff-tree", "-r", "-M", "-z", "--no-commit-id", "--name-status", "--diff-filter=AMR"]
        fields = (git_out(diff_args + ([parent, sha] if parent else ["--root", sha])) or "").split("\0")
        rows, i = [], 0
        while i < len(fields) and fields[i]:
            status = fields[i]
            if status.startswith("R") and i + 2 < len(fields):
                rows.append((fields[i + 2], fields[i + 1]))
                i += 3
            elif i + 1 < len(fields):
                rows.append((fields[i + 1], fields[i + 1]))
                i += 2
            else:
                break
        for rel, old_rel in rows:
            ext = os.path.splitext(rel)[1]
            if ext not in (".go", ".swift") or left() <= 0.5:
                continue
            swift = ext == ".swift"
            after = git_out(["-C", root, "show", "%s:%s" % (sha, rel)])
            if after is None:
                continue
            before = git_out(["-C", root, "show", "%s:%s" % (parent, old_rel)]) if parent else ""
            names, returns, types = new_declarations(code_only(before or "", swift), code_only(after, swift), ext)
            if not names and not types:
                continue
            out, type_out, logged = lookup(root, rel, ext, names, returns, types, marks, exclude_self=True)
            commit_parts += out
            type_parts += type_out
            logged_all += logged
    if marks:
        write_log(marks, logged_all, "commit")
    parts = []
    if commit_parts:
        parts.append("dupscan: いま commit した関数に名前が近い既存の定義がある。push の前に、再利用・既存の拡張で足りたかを見る (別の役割なら無視してよい。署名の近さは `dupscan index -returns 型` / `search` で引く):\n" + "\n".join(commit_parts))
    if type_parts:
        parts.append("いま commit した型と同じ名前の型が repo にある " + TYPES_NOTE + ":\n" + "\n".join(type_parts))
    emit(d.get("hook_event_name") or "PostToolUse", parts)


def main():
    try:
        d = json.load(sys.stdin)
        if sys.argv[1] == "edit":
            edit_main(d)
        elif sys.argv[1] == "commit":
            commit_main(d, sys.argv[3:])
    except Exception:
        return

main()
