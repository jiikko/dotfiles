#!/usr/bin/env python3
"""dupscan が実際に使われたかを、Claude Code の transcript (~/.claude/projects/*/*.jsonl) から数える。

測るもの (案内を出しただけ・利用が増えただけを効果としない。どれも「使われ方」の数):
  1. 識別子らしい grep / rg と dupscan の探索 (index / callers / def / refs / search / context / evidence / near / explain。gate は数えない) の回数。
     言語は command の拡張子・-t・glob から読み、読めないものは unknown (言語の手掛かりがあるものだけを Go / Swift の効果として読む)。
  2. hook の案内 (PreToolUse の additionalContext に dupscan を含むもの) が出た grep の直後 3 手のうちに、dupscan の探索があったか。
  3. dupscan index の直後 3 手のうちの最初の Read が、範囲指定 (offset / limit) つきか (終端行を Read の範囲に使っているか)。

数えないもの: dupscan 自身を開発しているセッション (src/dupscan を触る操作が 3 回以上)。測定の対象を自分で動かしているため。
-i 付きの grep (hook は案内しない)。パイプの後ろの grep (stdin を絞る形)。

使い方: scripts/dupscan_usage_report.py [--days 14] [--split ISO8601] [--json] [--projects-dir DIR]
  --split を渡すと、その時刻より前 (before) と後 (after) に分けて数える。変更の前後を比べるときに使う。
  --by-day を渡すと、日 (ローカル時刻) ごとに分けて数える。hook や dupscan が入った日の前後で何が変わったかを見るときに使う。
  同じ定義で後から測り直せるよう、集計の条件はこのファイルに置く (issue に貼らない)。
"""
import argparse
import collections
import datetime
import glob
import json
import os
import re
import shlex
import sys

EXPLORE_COMMANDS = "index|callers|def|refs|search|context|evidence|near|explain"
EXPLORE = re.compile(r"(?:^|[\s;&|(/])dupscan(?:\s+-\S+(?:\s+[^-\s]\S*)?)*\s+(" + EXPLORE_COMMANDS + r")\b")
GREP_WORD = re.compile(r"(?:^|[^A-Za-z0-9_-])(grep|rg|egrep)(?:[^A-Za-z0-9_-]|$)")
DECL = r"(?:func|type|struct|class|enum|protocol|extension|actor|init|var|let|case|typealias|const)"
IDENT = r"[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?"
# 値を取るオプション (次の語をパターンと読まない)。hook (suggest-dupscan-for-symbol-grep.sh) と同じ集合
VALUE_OPTIONS = {
    "-t", "--type", "--type-not", "-g", "--glob", "--iglob", "--include", "--exclude", "--exclude-dir",
    "-A", "-B", "-C", "-m", "--max-count", "-f", "--file", "--sort", "--sortr", "-M", "--max-columns",
    "-j", "--threads", "--encoding", "--replace", "--context-separator", "--path-separator", "--colors",
}
NEARBY = 3  # 案内・index の直後に見る tool_use の数
DEV_PATH = re.compile(r"src/dupscan/|dupscan/internal/")
DEV_THRESHOLD = 3


def split_shell(cmd):
    """引用符とバックスラッシュの外にある `||` `&&` `;` 改行 `|` で分ける。区切りも要素として残す。"""
    parts, buf, quote, i = [], [], None, 0
    while i < len(cmd):
        c = cmd[i]
        if quote:
            buf.append(c)
            if c == "\\" and quote == '"' and i + 1 < len(cmd):
                buf.append(cmd[i + 1])
                i += 1
            elif c == quote:
                quote = None
        elif c == "\\" and i + 1 < len(cmd):
            buf.append(c)
            buf.append(cmd[i + 1])
            i += 1
        elif c in "'\"":
            quote = c
            buf.append(c)
        else:
            op = next((o for o in ("||", "&&", ";", "\n", "|") if cmd.startswith(o, i)), None)
            if op:
                parts.append("".join(buf))
                parts.append(op)
                buf = []
                i += len(op)
                continue
            buf.append(c)
        i += 1
    parts.append("".join(buf))
    return parts


def language_hint(text):
    go = re.search(r"\.go\b|(?:^|\s)-t\s*go\b|--type[= ]go\b|type\s*[:=]\s*go\b", text)
    swift = re.search(r"\.swift\b|(?:^|\s)-t\s*swift\b|--type[= ]swift\b|type\s*[:=]\s*swift\b", text)
    if go and not swift:
        return "go"
    if swift and not go:
        return "swift"
    return "unknown"


def is_symbol_pattern(pattern):
    """識別子らしいパターンか (hook の判定の近似)。`func (w *T) Name` や `func.*Name`・識別子だけの alternation も含む。"""
    p = re.sub(r"\\[b<>]", "", pattern.strip())
    p = re.sub(r"^func\s*\.\*\s*", "func ", p)
    unescaped = re.sub(r"\\([()*])", r"\1", p)
    receiver = re.fullmatch(r"func\s+\(\s*(?:\w+\s+)?\*?[\w.\[\]]+\s*\)\s*(.+)", unescaped)
    if receiver:
        p = receiver.group(1)
    for sep in ("\\|", "|"):
        if sep in p:
            parts = p.split(sep)
            return 1 < len(parts) <= 3 and all(is_symbol_pattern(x) for x in parts)
    m = re.fullmatch(r"(?:(" + DECL + r")\s+)?(" + IDENT + r")\s*(?:\\?\()?", p)
    if not m:
        return False
    decl, name = m.group(1), m.group(2)
    base = name.split(".")[-1]
    if len(base) < 3 or base.isupper():
        return False
    return bool(decl or receiver or any(c.isupper() for c in name))


def grep_segments(command):
    """command の中の grep / rg の segment から (パターン, segment の文字列) を取る。-i とパイプの後ろは除く。"""
    out = []
    prev = None
    for seg in split_shell(command):
        if seg in ("||", "&&", ";", "\n", "|"):
            prev = seg
            continue
        after_pipe = prev == "|"
        prev = None
        if after_pipe or not GREP_WORD.search(seg):
            continue
        try:
            words = shlex.split(seg, comments=True)
        except ValueError:
            continue
        i = 0
        while i < len(words) and re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=.*", words[i]):
            i += 1
        if i < len(words) and words[i] == "git":
            i += 1
            while i < len(words) and words[i].startswith("-"):
                i += 2 if words[i] in ("-C", "-c") else 1
        if i >= len(words) or os.path.basename(words[i]) not in ("grep", "rg", "egrep", "git") and words[i] != "grep":
            continue
        rest = words[i + 1:]
        pattern, k, insensitive = None, 0, False
        while k < len(rest):
            w = rest[k]
            if w in ("-e", "--regexp"):
                pattern = pattern or (rest[k + 1] if k + 1 < len(rest) else None)
                k += 2
                continue
            if w in VALUE_OPTIONS:
                k += 2
                continue
            if w in ("-i", "--ignore-case") or (re.fullmatch(r"-[A-Za-z]*i[A-Za-z]*", w) and not re.search(r"[tgefmABCMj]", w)):
                insensitive = True
            elif not w.startswith("-") and pattern is None:
                pattern = w
            k += 1
        if pattern is not None and not insensitive:
            out.append((pattern, seg))
    return out


def classify(tool_use):
    """tool_use を ("explore", subcommand, 言語) / ("search", "symbol"|"other", 言語) / ("read", has_range, None) / None に分ける。"""
    name, inp = tool_use["name"], tool_use.get("input") or {}
    if name == "Bash":
        cmd = inp.get("command", "")
        m = EXPLORE.search(cmd)
        if m:
            return ("explore", m.group(1), language_hint(cmd))
        found = grep_segments(cmd)
        if found:
            symbol = [s for s in found if is_symbol_pattern(s[0])]
            if symbol:
                return ("search", "symbol", language_hint(symbol[0][1]))
            return ("search", "other", language_hint(found[0][1]))
    elif name == "Grep":
        if inp.get("-i") is True:
            return None
        hint = " ".join(str(inp.get(k, "")) for k in ("glob", "path")) + (" -t " + str(inp["type"]) if inp.get("type") else "")
        kind = "symbol" if is_symbol_pattern(str(inp.get("pattern", ""))) else "other"
        return ("search", kind, language_hint(hint))
    elif name == "Read":
        return ("read", "offset" in inp or "limit" in inp, None)
    return None


def local_day(ts):
    return datetime.datetime.fromisoformat(ts.replace("Z", "+00:00")).astimezone().date().isoformat()


def load_session(path):
    """transcript 1 本を、時刻つきの tool_use の列と、案内が出た tool_use の id の集合にする。"""
    uses, nudged = [], set()
    for line in open(path, errors="ignore"):
        if '"tool_use"' not in line and "hook_additional_context" not in line:
            continue
        try:
            o = json.loads(line)
        except ValueError:
            continue
        att = o.get("attachment") or {}
        if att.get("type") == "hook_additional_context" and att.get("hookEvent") == "PreToolUse" and "dupscan" in json.dumps(att, ensure_ascii=False):
            nudged.add(att.get("toolUseID"))
            continue
        content = (o.get("message") or {}).get("content")
        if o.get("type") != "assistant" or not isinstance(content, list):
            continue
        for c in content:
            if isinstance(c, dict) and c.get("type") == "tool_use":
                uses.append({"id": c.get("id"), "name": c.get("name"), "input": c.get("input"), "ts": o.get("timestamp", "")})
    return uses, nudged


def is_dupscan_dev(uses):
    touched = 0
    for u in uses:
        inp = u.get("input") or {}
        if u["name"] in ("Edit", "Write", "MultiEdit") and DEV_PATH.search(str(inp.get("file_path", ""))):
            touched += 1
        elif u["name"] == "Bash" and DEV_PATH.search(inp.get("command", "")):
            touched += 1
    return touched >= DEV_THRESHOLD


def empty_bucket():
    return {"sessions": 0, "search_symbol": collections.Counter(), "search_other": 0, "explore": collections.Counter(),
            "explore_by_command": collections.Counter(), "nudged": collections.Counter(), "nudged_followed": collections.Counter(),
            "index_then_read": 0, "index_read_with_range": 0}


def count(uses, nudged, bucket_of):
    """uses の各 tool_use を、時刻で選んだ bucket に数える。bucket_of(ts) が None なら数えない。"""
    kinds = [classify(u) for u in uses]
    seen = set()
    for i, (u, k) in enumerate(zip(uses, kinds)):
        b = bucket_of(u["ts"])
        if b is None or k is None:
            continue
        seen.add(id(b))
        if k[0] == "search":
            if k[1] == "symbol":
                b["search_symbol"][k[2]] += 1
            else:
                b["search_other"] += 1
            if u["id"] in nudged:
                b["nudged"][k[2]] += 1
                if any(x and x[0] == "explore" for x in kinds[i + 1:i + 1 + NEARBY]):
                    b["nudged_followed"][k[2]] += 1
        elif k[0] == "explore":
            b["explore"][k[2]] += 1
            b["explore_by_command"][k[1]] += 1
            if k[1] == "index":
                nxt = next((x for x in kinds[i + 1:i + 1 + NEARBY] if x and x[0] == "read"), None)
                if nxt:
                    b["index_then_read"] += 1
                    b["index_read_with_range"] += 1 if nxt[1] else 0
    return seen


def inject_summary(path, since):
    """hook inject-dupscan-candidates-on-new-func.sh の計測ログ (時刻 session 名前 候補数) を数える。無ければ None。"""
    try:
        lines = open(path, encoding="utf-8").read().splitlines()
    except OSError:
        return None
    total = with_hits = 0
    sessions = set()
    for line in lines:
        f = line.split("\t")
        if len(f) != 4 or not f[3].isdigit():
            continue
        try:
            t = datetime.datetime.fromisoformat(f[0])
        except ValueError:
            continue
        if t < since:
            continue
        total += 1
        with_hits += int(f[3]) > 0
        sessions.add(f[1])
    return {"injections": total, "with_candidates": with_hits, "sessions": len(sessions)}


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--days", type=int, default=14)
    ap.add_argument("--split", help="この時刻 (ISO 8601) より前を before、後を after に分ける")
    ap.add_argument("--by-day", action="store_true", help="日ごとに分けて数える")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--projects-dir", default=os.path.expanduser("~/.claude/projects"))
    ap.add_argument("--inject-log", default=os.path.join(os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"), "claude-dupscan-inject.log"),
                    help="新しい関数の直前に候補を注入する hook の計測ログ")
    ap.add_argument("--include-dupscan-dev", action="store_true", help="dupscan 自身を開発しているセッションも数える")
    args = ap.parse_args(argv)

    now = datetime.datetime.now(datetime.timezone.utc)
    since = now - datetime.timedelta(days=args.days)
    split = datetime.datetime.fromisoformat(args.split) if args.split else None
    if split is not None and split.tzinfo is None:
        split = split.astimezone()
    buckets = {"before": empty_bucket(), "after": empty_bucket()} if split else {"all": empty_bucket()}
    if args.by_day:
        buckets = collections.defaultdict(empty_bucket)

    def bucket_of(ts):
        try:
            t = datetime.datetime.fromisoformat(ts.replace("Z", "+00:00"))
        except ValueError:
            return None
        if t < since:
            return None
        if args.by_day:
            return buckets[t.astimezone().date().isoformat()]
        if split is None:
            return buckets["all"]
        return buckets["before"] if t < split else buckets["after"]

    skipped_dev = 0
    for path in glob.glob(os.path.join(args.projects_dir, "*", "*.jsonl")):
        if datetime.datetime.fromtimestamp(os.path.getmtime(path), datetime.timezone.utc) < since:
            continue
        uses, nudged = load_session(path)
        if not args.include_dupscan_dev and is_dupscan_dev(uses):
            skipped_dev += 1
            continue
        for b_id in count(uses, nudged, bucket_of):
            for b in buckets.values():
                if id(b) == b_id:
                    b["sessions"] += 1
    buckets = dict(sorted(buckets.items()))
    report = {"days": args.days, "split": args.split, "skipped_dupscan_dev_sessions": skipped_dev, "inject": inject_summary(args.inject_log, since), "buckets": {
        name: {**b, "search_symbol": dict(b["search_symbol"]), "explore": dict(b["explore"]), "explore_by_command": dict(b["explore_by_command"]),
               "nudged": dict(b["nudged"]), "nudged_followed": dict(b["nudged_followed"])} for name, b in buckets.items()}}
    if args.json:
        json.dump(report, sys.stdout, ensure_ascii=False, indent=2)
        print()
        return 0
    print(f"直近 {args.days} 日 (dupscan 自身を開発しているセッション {skipped_dev} 本は除く)")
    for name, b in report["buckets"].items():
        sym, exp = sum(b["search_symbol"].values()), sum(b["explore"].values())
        print(f"\n[{name}] セッション {b['sessions']} 本")
        print(f"  識別子らしい grep/rg: {sym} 回 {b['search_symbol']} / dupscan の探索: {exp} 回 {b['explore']}"
              f" → dupscan の割合 {exp * 100 // (sym + exp) if sym + exp else 0}%")
        print(f"  探索のサブコマンド: {b['explore_by_command']}")
        n, f = sum(b["nudged"].values()), sum(b["nudged_followed"].values())
        print(f"  案内が出た grep: {n} 回 {b['nudged']} → 直後 {NEARBY} 手以内に dupscan の探索: {f} 回 ({f * 100 // n if n else 0}%)")
        r, w = b["index_then_read"], b["index_read_with_range"]
        print(f"  dupscan index の直後 {NEARBY} 手以内の Read: {r} 回のうち範囲指定 (offset/limit) あり {w} 回")
    inj = report["inject"]
    if inj is None:
        print("\n候補の注入 (hook inject-dupscan-candidates-on-new-func): 計測ログなし")
    else:
        print(f"\n候補の注入 (hook inject-dupscan-candidates-on-new-func): {inj['injections']} 回 / 候補あり {inj['with_candidates']} 回 / セッション {inj['sessions']} 本")
    return 0


if __name__ == "__main__":
    sys.exit(main())
