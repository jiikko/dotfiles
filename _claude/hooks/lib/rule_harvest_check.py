"""Stop hook の本体: このセッションで実装・仕組みを変えた commit があれば、1 回だけ止めて skill rule-harvest を促す。

なぜ: 実装に埋め込んだ決定 (状態の網羅の仕組み・待ちの契約・ユーザー決定) は、次にその領域を変える人が知らないとデグレする。
コンパイラ・lint・テストが止めない部分は rule に書かないと残らないが、作業を終えた時点では書く動機が無い
(2026-11-20 ユーザー指示。vlc-multi-video-player の issue 580 の仕組みは、聞かれるまで rule にしていなかった)。
何を rule にするか・既存の rule との比較・5 つの観点のレビューは skill rule-harvest の手順で、この hook は出口で促すだけ。

数える commit: このセッションの transcript で、Bash の tool_use の入力が `git commit` を含み、失敗しなかったもの。sha は 2 通りで取る:
結果に出た `[branch sha]` の行と、そのコマンドの dir の HEAD の reflog のうち、コマンドを打った時刻から結果が返った時刻までの `commit` の行
(`-q` や出力のリダイレクトで `[branch sha]` の行が出ない commit が大半なので。2026-11-20 に直近 40 の transcript で数えると 248 件中 191 件が `-q`)。
そのうち、変えたファイルが md (issue・rule・skill・文書)・tmp/ の下・submodule の参照 (mode 160000) だけの commit は数えない
(rule を足す commit 自身で止め直さないため。issue の目印の symlink も .md の名前。dir の名前で issues を外すと、`Sources/issues/` のようなコードも外す)。merge は 1 つ目の親との差で見る。
commit の repo が分からない sha (worktree を消した等) は、中身を見られないので数える (止める側に倒す。手間は「候補なし」の 1 行)。

検出しないもの (承知の上): サブエージェント・workflow・別の道具 (bin/bump-submodule・issue_done.sh) が作った commit、未 commit の変更、
`git commit` を変数や別名・絶対パスの git で打った commit、cherry-pick・merge・revert・rebase が作った commit、
`-q` で打ち、その dir を `cd $VAR` のように変数で指した commit と、reflog ごと消えた worktree で `-q` で打った commit。

止める: 新しい sha が 1 つでもあれば {"decision":"block","reason":...}。同じ sha では 1 セッション 1 回 (印は session_marks)。
止めない: 環境変数 CLAUDE_RULE_HARVEST=off (無人で回す session で止めたいとき)。stop_hook_active (block からの続き) では印だけを書く:
この skill の続きで足した lint・テストの commit を、次の turn にもう一度報告しないため (続きで作った実装の commit は報告されない。受け入れた。
ほかの Stop hook (issue-progress-check) の block の続きで作った commit も同じく報告されない)。
印は判定を終えて出力した後に書く (10 秒の上限で切られたとき、報告していない sha に印が残らない)。
git は repo ごとにまとめて呼ぶ (sha ごと・dir ごとに呼ぶと、commit が 90 件・dir が 40 の transcript で 10 秒の上限に届いた)。
🚨 どの失敗も hook を止めない (exit 0)。transcript を読めたのに assistant の行が 1 つも無いときは、形式が変わった疑いとして warn を残す
(黙って死ぬのを見えるようにする。session_marks の claude-hook-marks.log)。
"""
import datetime
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import session_marks  # noqa: E402

MARKS = "claude-rule-harvest.marks"
KIND = "sha"
COMMIT_CMD = re.compile(r"(^|[\s;&|(])git(\s+-[Cc]\s+\S+)*\s+commit\b")
COMMIT_LINE = re.compile(r"^\[[^\]\n]*?\s([0-9a-f]{7,40})\]\s", re.M)
REFLOG_SLACK = 2  # 秒。transcript の時刻と reflog の時刻 (秒の単位) の丸めの差
DIR_IN_CMD = re.compile(r"(?:\bcd|\bgit\s+-C|\bwt\s+-C)\s+[\"']?(~?/[^\s\"';&|)]+)")
MAX_LISTED = 10


def result_text(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(c.get("text", "") for c in content if isinstance(c, dict))
    return ""


def epoch(ts):
    try:
        return datetime.datetime.fromisoformat(str(ts).replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def read_transcript(path):
    """(commit のコマンドの列, transcript に出た全部の dir, assistant の行の数)。読めなければ None。
    commit のコマンドは (出力の sha の列, そのコマンドを打った所の dir の列, 打った時刻, 結果が返った時刻)"""
    commands, all_dirs, commits, assistants = {}, [], [], 0
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.readlines()
    except OSError:
        return None
    for line in lines:
        try:
            entry = json.loads(line)
        except ValueError:
            continue  # 書き込み途中の最終行
        if not isinstance(entry, dict):
            continue
        cwd = entry.get("cwd")
        if cwd:
            all_dirs.append(cwd)
        message = entry.get("message") or {}
        content = message.get("content") if isinstance(message, dict) else None
        if not isinstance(content, list):
            continue
        if entry.get("type") == "assistant":
            assistants += 1
            for c in content:
                if isinstance(c, dict) and c.get("type") == "tool_use" and c.get("name") == "Bash":
                    cmd = str((c.get("input") or {}).get("command", ""))
                    here = [os.path.expanduser(d) for d in DIR_IN_CMD.findall(cmd)]
                    all_dirs.extend(here)
                    commands[c.get("id")] = (cmd, here + ([cwd] if cwd else []), epoch(entry.get("timestamp")))
        elif entry.get("type") == "user":
            for c in content:
                if not (isinstance(c, dict) and c.get("type") == "tool_result"):
                    continue
                found = commands.get(c.get("tool_use_id"))
                if found is None or c.get("is_error") or not COMMIT_CMD.search(found[0]):
                    continue
                commits.append((COMMIT_LINE.findall(result_text(c.get("content"))), found[1], found[2], epoch(entry.get("timestamp"))))
    return commits, all_dirs, assistants


def git(d, args, stdin=None):
    try:
        # quotepath を外す: 外さないと日本語名のパスが引用符と 8 進で出て、.md の判定から外れる
        p = subprocess.run(["git", "-c", "core.quotepath=off", "-C", d, *args], input=stdin, capture_output=True, text=True, timeout=4)
    except (OSError, subprocess.SubprocessError):
        return None
    return p.stdout if p.returncode == 0 else None


_COMMON = {}


def common_dir(d):
    """dir の repo の共通の git dir (同じ repo の worktree は同じ値)。repo でなければ None。dir ごとに 1 回だけ git を呼ぶ"""
    if d not in _COMMON:
        out = git(d, ["rev-parse", "--path-format=absolute", "--git-common-dir"]) if os.path.isdir(d) else None
        _COMMON[d] = out.strip() if out else None
    return _COMMON[d]


def repos_of(dirs):
    """dir の列を、在る repo の (共通の git dir → 代表の dir) にまとめる (順を保つ)"""
    repos = {}
    for d in dirs:
        common = common_dir(d)
        if common and common not in repos:
            repos[common] = d
    return repos


_REFLOG = {}


def reflog_commits(d, t0, t1):
    """dir の HEAD の reflog のうち、t0〜t1 (秒) の commit の行の完全な sha。dir ごとに 1 回だけ git を呼ぶ"""
    if t0 is None or t1 is None or not os.path.isdir(d):
        return []
    if d not in _REFLOG:
        out = git(d, ["log", "-g", "-n", "500", "--date=unix", "--format=%H%x09%gd%x09%gs", "HEAD"]) or ""
        rows = []
        for line in out.splitlines():
            parts = line.split("\t", 2)
            m = re.search(r"@\{(\d+)\}$", parts[1]) if len(parts) == 3 else None
            if m and parts[2].startswith("commit"):
                rows.append((parts[0], int(m.group(1))))
        _REFLOG[d] = rows
    return [h for h, t in _REFLOG[d] if t0 - REFLOG_SLACK <= t <= t1 + REFLOG_SLACK]


def lookup(rep, shas):
    """repo の中で引ける sha → 完全な sha。1 回の cat-file で引く"""
    out = git(rep, ["cat-file", "--batch-check=%(objectname) %(objecttype)"], "".join(s + "\n" for s in shas)) or ""
    found = {}
    for sha, line in zip(shas, out.splitlines()):
        parts = line.split()
        if len(parts) == 2 and parts[1] == "commit":
            found[sha] = parts[0]
    return found


def is_doc_path(path):
    return path.endswith(".md") or path.split("/")[0] == "tmp"


def summaries(rep, fulls):
    """完全な sha → (subject, 実装を変えたか)。1 回の git log で引く"""
    # 区切りは NUL (件名に入りえない)。件名に入りうる字で区切ると、件名の途中で commit が分かれて実装の commit を数え損ねる
    out = git(rep, ["log", "--no-walk=unsorted", "--raw", "--no-renames", "--diff-merges=first-parent", "--format=%x00%H%x00%s", *fulls])
    result = {}
    if out is None:
        return result
    tokens = out.split("\x00")[1:]
    for full, rest in zip(tokens[0::2], tokens[1::2]):
        subject, _, body = rest.partition("\n")
        impl = False
        for line in body.splitlines():
            if not line.startswith(":"):
                continue
            meta, _, path = line.partition("\t")
            if "160000" in meta[1:].split()[:2] or is_doc_path(path):
                continue
            impl = True
        result[full] = (subject, impl)
    return result


def main():
    if os.environ.get("CLAUDE_RULE_HARVEST", "").lower() in ("off", "0", "false"):
        return 0
    try:
        data = json.load(sys.stdin)
    except ValueError:
        return 0
    if not isinstance(data, dict):
        return 0
    session = session_marks.safe_session(data.get("session_id"))
    transcript = data.get("transcript_path") or ""
    if not transcript or not os.path.isfile(transcript):
        return 0
    read = read_transcript(transcript)
    if read is None:
        return 0
    commits, all_dirs, assistants = read
    if assistants == 0:
        session_marks.warn("rule-harvest-check: transcript に assistant の行が無い (形式が変わった疑い): %s" % transcript)
        return 0
    marks_path = session_marks.path_of(MARKS)
    seen = {v for k, v in session_marks.load(marks_path, session) if k == KIND}
    home = {}
    for printed, dirs, t0, t1 in commits:
        for d in dirs:
            for full in reflog_commits(d, t0, t1):
                home.setdefault(full, []).append(d)
        for sha in printed:
            home.setdefault(sha, []).extend(dirs)
    shas = [s for s in home if s not in seen]
    if not shas:
        return 0
    repos = repos_of(list(dict.fromkeys(sum((home[s] for s in shas), []) + ([data["cwd"]] if data.get("cwd") else []) + all_dirs)))
    # sha ごとに、その commit を打った所の repo を先に当てる (短い sha が別の repo の commit と重なっても、打った所を正にする)
    home_repos = {s: list(repos_of(home[s]).keys()) for s in shas}
    found_in = {common: lookup(rep, shas) for common, rep in repos.items()}
    resolved, unresolved = {}, []
    for sha in shas:
        order = home_repos[sha] + [c for c in repos if c not in home_repos[sha]]
        hit = next((c for c in order if sha in found_in.get(c, {})), None)
        if hit is None:
            unresolved.append(sha)
        else:
            resolved.setdefault(hit, []).append((sha, found_in[hit][sha]))
    lines, marks, done = [], [], set()
    for common, pairs in resolved.items():
        rep = repos[common]
        info = summaries(rep, [full for _, full in pairs])
        top = (git(rep, ["rev-parse", "--show-toplevel"]) or rep).strip()
        for sha, full in pairs:
            if full in seen or full in done:
                continue  # 報告済み。印を書き足さない (書き足すと印のファイルが Stop のたびに伸び、上限で他の session の印を押し出す)
            done.add(full)  # 出力の短い sha と reflog の完全な sha は同じ commit を指しうる
            marks.append(full)
            subject, impl = info.get(full, ("", True))
            if not impl:
                continue
            lines.append("- %s %s %s" % (os.path.basename(top), full[:9], subject))
    for sha in unresolved:
        marks.append(sha)
        lines.append("- %s (repo を特定できなかった。中身は git show で確かめる)" % sha[:9])
    if lines and data.get("stop_hook_active") is not True:
        listed = lines[:MAX_LISTED]
        if len(lines) > MAX_LISTED:
            listed.append("- ほか %d 件" % (len(lines) - MAX_LISTED))
        reason = (
            "このセッションで実装・仕組みを変えた commit がある (Stop hook rule-harvest-check)。"
            "skill rule-harvest の手順で、今後の改修でデグレさせないための恒久的な決定事項を rule に落とせないかを検討する。"
            "既存の rule と比べ (追記を先に考える。既存の規範・ユーザー決定と衝突する候補は足さずに聞く)、"
            "5 つの観点の反証レビューを通った候補だけを足す。候補が無ければ「rule 候補なし」と理由を 1 行述べて終える:\n"
            + "\n".join(listed)
            + "\n\nこの hook は commit の有無と変えたファイルの種類しか見ない (md・issues・submodule の参照だけの commit は数えない。"
            "サブエージェントや別の道具が作った commit と、未 commit の変更は数えない)。"
        )
        print(json.dumps({"decision": "block", "reason": reason}, ensure_ascii=False))
        sys.stdout.flush()
    for m in dict.fromkeys(marks):
        session_marks.put(marks_path, session, KIND, m)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as e:  # 想定外の失敗も Stop を止めない
        session_marks.warn("rule-harvest-check: 想定外の失敗: %r" % (e,))
        sys.exit(0)
