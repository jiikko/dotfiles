#!/usr/bin/env bash
# PreToolUse(Write / Edit) フック: 新しい関数 (Go / Swift) を足す書き込みの直前に、同じ名前の既存の定義を dupscan index で引いて注入する (push 型)。
#
# なぜ: 「書く前に dupscan を引け」と案内するだけでは、agent は呼ばない (30 日で識別子検索 1,680 回に対し dupscan の探索 2 回)。
# 呼ぶかどうかを agent の判断に任せると効果が出ない。書く直前に候補を目の前へ出す。
#
# 🚨 止めない (additionalContext だけ)。permissionDecision を返さない。どんな失敗も無音で rc=0 (fail-open)。
# 🚨 出しすぎない: session × 名前で 1 回。候補が無ければ何も出さない。名前の一致だけを出す (部分一致の雑音は出さない)。
# 🚨 遅延: 名前 1 つにつき dupscan index を 1 回 (全体で 6 秒の予算。超えたら残りは引かない)。
#
# 判定 (python):
#   - file_path が .go / .swift。dupscan と git が在る。repo の root は書くファイルの属する git repo
#   - 新しい関数 = 書いた後のファイル (Write は content、Edit は今のファイルの old_string を new_string に置き換えたもの) にある
#     `func name(` のうち、Edit の old_string にも、書き換え前のファイルにも `func name(` が無いもの。書いた後のファイルを作れない
#     (ファイルが無い・old_string が見つからない) ときは new_string だけを見る
#   - 文字列リテラルとコメントの中は見ない (Go の raw string・"…"・rune、Swift の """…"""・"…"、// と /* */)。test が Go のソースを
#     fixture として文字列に持つと、その中の func を新しい関数と読んでいた。Edit の new_string は文字列の途中から始まりうるので、
#     書いた後のファイル全体を頭から読んで決める
#   - git が ignore するパス (tmp/ の使い捨ての道具など) では引かない
#   - 除く名前: 5 文字未満・Test/Benchmark/Fuzz/Example 始まり・init/main/String/Error/Close/body ほか決まり文句、Go のメソッド (func (r T) name)
#   - 候補 = index の行のうち、名前が大小無視で等しいもの、または一方が他方の前方で共通部分が 6 文字以上で、短い方の名前が 2 語以上のもの
#     (短い方が 1 語 (`resume`) だと、`resumeReady` に対して語が同じだけの無関係な `resume()` が並ぶ。失うもの: 1 語の既存の関数
#     (`upload`) を、それに語を足した新しい名前 (`uploadAll`) で書くときは出ない)
#   - 新しい型 = 書く文字列にある型の宣言 (Swift の class / struct / actor / enum / protocol、Go の行頭の `type 名前`) のうち、書き換え前に無いもの。
#     同じ言語のファイルに同じ名前の型の宣言があれば (private を含む) 出す。git grep の字句の完全一致で、dupscan は使わない
#     (index は型を返さず、Go では def が使えない)。型は計測ログに書かない (ログは関数の注入の A-B を数える)
# 検出しない形 (silent): Bash の書き込み・NotebookEdit・名前が違うが同じ役割の関数 (署名の近さは見ない。dupscan search / index -returns で引く)・
#   Swift のメソッドと Go の関数の区別 (Swift は字下げのある func も新しい関数として扱う)・Go の `type (` の中の型・
#   引数付きの属性 (`@available(…)`) の付いた Swift の型・typealias
# 計測: 注入のたびに ${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-inject.log へ「時刻 session 名前 候補数」を 1 行足す
#   (scripts/dupscan_usage_report.py が数える)

set -uo pipefail
unset CDPATH

# 実験用の opt-out: DUPSCAN_INJECT=off の run は注入しない (A-B で処置の有無だけを変えるため。本番の既定は on。名前の候補の判定は変えない)
[ "${DUPSCAN_INJECT:-}" = off ] && exit 0
command -v python3 >/dev/null 2>&1 || exit 0
command -v git >/dev/null 2>&1 || exit 0
command -v dupscan >/dev/null 2>&1 || exit 0

# shellcheck disable=SC2016 # python のソースを単引用符で渡す (シェルの展開は要らない)
exec python3 -I -c '
import json, os, re, subprocess, sys, time

BUDGET = 6.0
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

def main():
    try:
        d = json.load(sys.stdin)
        ti = d.get("tool_input") or {}
        path = ti.get("file_path") or ""
        ext = os.path.splitext(path)[1]
        if ext not in (".go", ".swift") or not os.path.isabs(path):
            return
        new = ti.get("new_string") if "new_string" in ti else ti.get("content")
        if not new:
            return
        rx = FUNC_GO if ext == ".go" else FUNC_SWIFT
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
        new = code_only(after, swift) if after is not None else code_only(new, swift)
        before = code_only(old, swift) + "\n" + (code_only(current, swift) if current is not None else "")
        known = {m.group(1) for m in rx.finditer(before)}
        trx = TYPE_GO if ext == ".go" else TYPE_SWIFT
        known_types = {m.group(1) for m in trx.finditer(before)}
        types = []
        for m in trx.finditer(new):
            t = m.group(1)
            if t not in known_types and t not in types and t not in NOT_TYPE:
                types.append(t)
        names = []
        for m in rx.finditer(new):
            n = m.group(1)
            if n in known or n in names or len(n) < 5 or n.lower() in SKIP_NAMES or n.lower().startswith(SKIP_PREFIX):
                continue
            names.append(n)
        if not names and not types:
            return
        dirpath = os.path.dirname(path)
        while not os.path.isdir(dirpath) and dirpath != "/":
            dirpath = os.path.dirname(dirpath)
        root = subprocess.run(["git", "-C", dirpath, "rev-parse", "--show-toplevel"], capture_output=True, text=True, timeout=3).stdout.strip()
        if not root:
            return
        if subprocess.run(["git", "-C", root, "check-ignore", "-q", "--", path], capture_output=True, timeout=3).returncode == 0:
            return
        scope = os.path.join(root, "src") if ext == ".go" and os.path.isdir(os.path.join(root, "src")) else root
        session = re.sub(r"[^A-Za-z0-9_-]", "_", str(d.get("session_id") or "unknown"))
        tmp = os.environ.get("TMPDIR") or "/tmp"
        start = time.monotonic()
        out, logged, type_out = [], [], []
        rel = os.path.relpath(path, root)
        for t in types[:3]:
            stamp = os.path.join(tmp, "claude-dupscan-inject-%s-type-%s" % (session, t))
            left = BUDGET - (time.monotonic() - start)
            if os.path.exists(stamp) or left <= 0.5:
                continue
            try:
                open(stamp, "w").close()
            except OSError:
                pass
            pat = (GREP_GO if ext == ".go" else GREP_SWIFT) + t
            r = subprocess.run(["git", "-C", root, "grep", "-n", "-I", "-E", "-w", "--untracked", "-e", pat, "--", "*" + ext], capture_output=True, text=True, timeout=left)
            hits = [h for h in r.stdout.splitlines() if not h.startswith(rel + ":")]
            if hits:
                type_out.append("`%s` と同じ名前の型の宣言が既にある:\n%s" % (t, "\n".join("  " + h.strip() for h in hits[:3])))
        for n in names[:3]:
            stamp = os.path.join(tmp, "claude-dupscan-inject-%s-%s" % (session, n))
            if os.path.exists(stamp):
                continue
            try:
                open(stamp, "w").close()
            except OSError:
                pass
            left = BUDGET - (time.monotonic() - start)
            if left <= 0.5:
                break
            hits = []
            # index -name は「既存の名前が引く語を含む」ものを返すので、引く名前より短い既存の名前 (collectWidgets に対する collectWidgetsFast) は
            # 名前そのものでは引けない。名前の語の境目の前方 (長い順に最大 2 つ) も引き、既存の名前が前方になっているものを拾う。
            for q in [n] + stems(n)[:2]:
                left = BUDGET - (time.monotonic() - start)
                if left <= 0.5:
                    break
                r = subprocess.run(["dupscan", "index", "-name", q, "-limit", "50" if q != n else "20", "-doc-bytes", "60", scope], capture_output=True, text=True, timeout=left)
                for line in r.stdout.splitlines():
                    m = LINE.match(line)
                    if not m or line in hits:
                        continue
                    if near(m.group(3), n):
                        hits.append(line)
                if hits:
                    break
            logged.append((n, len(hits)))
            if hits:
                out.append("`%s` に近い既存の定義:\n%s" % (n, "\n".join("  " + h for h in hits[:3])))
        if logged:
            cache = os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")
            try:
                os.makedirs(cache, exist_ok=True)
                with open(os.path.join(cache, "claude-dupscan-inject.log"), "a") as f:
                    for n, c in logged:
                        f.write("%s\t%s\t%s\t%d\n" % (time.strftime("%Y-%m-%dT%H:%M:%S%z"), session, n, c))
            except OSError:
                pass
        parts = []
        if out:
            parts.append("dupscan: これから足す関数に名前が近い既存の定義がある。再利用・既存の拡張で足りるか見てから書く (別の役割なら無視してよい。署名の近さは `dupscan index -returns 型` / `search` で引く):\n" + "\n".join(out))
        if type_out:
            parts.append("これから足す型と同じ名前の型が repo にある (Go は同じ package、Swift は同じ module の top-level どうしか同じ型の中なら build で衝突する。Swift の private / fileprivate の型は同じファイルの中だけ。別の型なら名前を変え、同じものなら再利用する):\n" + "\n".join(type_out))
        if parts:
            msg = "\n".join(parts)
            print(json.dumps({"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": msg}}, ensure_ascii=False))
    except Exception:
        return

main()
'
