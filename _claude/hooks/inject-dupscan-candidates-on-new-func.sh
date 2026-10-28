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
#   - 新しい関数 = 書く文字列 (Edit は new_string、Write は content) にある `func name(` のうち、
#     Edit の old_string にも、書き換え前のファイルにも `func name(` が無いもの
#   - 除く名前: 5 文字未満・Test/Benchmark/Fuzz/Example 始まり・init/main/String/Error/Close/body ほか決まり文句、Go のメソッド (func (r T) name)
#   - 候補 = index の行のうち、名前が大小無視で等しいもの、または一方が他方の前方で共通部分が 6 文字以上のもの
# 検出しない形 (silent): Bash の書き込み・NotebookEdit・名前が違うが同じ役割の関数 (署名の近さは見ない。dupscan search / index -returns で引く)・
#   Swift のメソッドと Go の関数の区別 (Swift は字下げのある func も新しい関数として扱う)
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
LINE = re.compile(r"^(\S+?):(\d+)(?:-\d+)?\s+(?:[A-Za-z_][A-Za-z0-9_]*\.)*([A-Za-z_][A-Za-z0-9_]*)\s*[(<\[]")

def stems(name):
    """name の語の境目 (大文字・_ の前) での前方で、6 文字以上のもの (長い順)。name 自身は含まない。"""
    cuts = [i for i in range(6, len(name)) if name[i].isupper() or name[i] == "_"]
    return [name[:i].rstrip("_") for i in reversed(cuts)]

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
        before = ti.get("old_string") or ""
        try:
            with open(path, encoding="utf-8", errors="replace") as f:
                before += "\n" + f.read()
        except OSError:
            pass
        known = {m.group(1) for m in rx.finditer(before)}
        names = []
        for m in rx.finditer(new):
            n = m.group(1)
            if n in known or n in names or len(n) < 5 or n.lower() in SKIP_NAMES or n.lower().startswith(SKIP_PREFIX):
                continue
            names.append(n)
        if not names:
            return
        dirpath = os.path.dirname(path)
        while not os.path.isdir(dirpath) and dirpath != "/":
            dirpath = os.path.dirname(dirpath)
        root = subprocess.run(["git", "-C", dirpath, "rev-parse", "--show-toplevel"], capture_output=True, text=True, timeout=3).stdout.strip()
        if not root:
            return
        scope = os.path.join(root, "src") if ext == ".go" and os.path.isdir(os.path.join(root, "src")) else root
        session = re.sub(r"[^A-Za-z0-9_-]", "_", str(d.get("session_id") or "unknown"))
        tmp = os.environ.get("TMPDIR") or "/tmp"
        start = time.monotonic()
        out, logged = [], []
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
                    a, b = m.group(3).lower(), n.lower()
                    if a == b or ((a.startswith(b) or b.startswith(a)) and min(len(a), len(b)) >= 6):
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
        if out:
            msg = "dupscan: これから足す関数に名前が近い既存の定義がある。再利用・既存の拡張で足りるか見てから書く (別の役割なら無視してよい。署名の近さは `dupscan index -returns 型` / `search` で引く):\n" + "\n".join(out)
            print(json.dumps({"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": msg}}, ensure_ascii=False))
    except Exception:
        return

main()
'
