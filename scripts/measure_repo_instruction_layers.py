#!/usr/bin/env python3
"""他の repo で起動したセッションの「repo の指示ファイルの字数」を数え、最も重いセッションを出す (clone は要らない)。

scripts/check_instruction_budget.sh の OTHER_REPO_RESERVE (定数) が古くなっていないかを、手元で確かめるための道具 (dotfiles issue 1044)。
CI からは他の repo を読めないので、予算スクリプトは gh が使える手元でだけこれを呼ぶ。

数えるセッション:
  - umbrella (既定 jiikko/my-products) の root と、`.gitmodules` に載った各 submodule の既定ブランチの先端 (参照している commit ではない。増え方を先に見るため)
  - --extra で渡した umbrella の外の repo
  - 起動する場所は各 repo の root と、root の 1 段下で CLAUDE.md とビルドの入口 (PROJECT_MARKERS) を両方持つディレクトリ (obaket の macOS/ など)。
    実際には起動しない場所も数えうるが、多く数えて赤に倒れるだけ
1 つのセッションの字数 = 起動した場所から umbrella の root までの各ディレクトリの `CLAUDE.md` と `.claude/rules/**/*.md` の合計。
  frontmatter に `paths:` がある rule は数えない (判定は check_instruction_budget.sh の has_paths_frontmatter と同じ)。
  字数は UTF-8 の文字数 (`LC_ALL=en_US.UTF-8 wc -m` と同じ数え方)。

検出しないもの: CLAUDE.local.md (git に無い)・`@path` の import の中身・submodule の中の submodule・symlink の rule (リンク先を辿らないので、見つけたら測れないとして失敗にする)。

出力 (stdout): 1 行 1 セッション `<字数>\t<repo>:<起動したディレクトリ>` を大きい順に。最後の行は `max\t<字数>\t<ラベル>`。
rc: 0 = 測れた / 2 = 測れない (gh の失敗・tree が途中で切れた・数えたセッションが 0)。

使い方:
  scripts/measure_repo_instruction_layers.py [--umbrella OWNER/REPO] [--extra OWNER/REPO ...] [--local DIR]
  --local DIR: gh の代わりに DIR/<owner>/<repo> の git repo の HEAD を読む (テスト用)
"""
import argparse
import base64
import json
import os
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

CACHE_PATH = os.path.join(os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"),
                          "dotfiles", "instruction-budget-blobs.tsv")


class MeasureError(Exception):
    pass


def run(cmd):
    p = subprocess.run(cmd, capture_output=True)
    if p.returncode != 0:
        raise MeasureError(f"{' '.join(cmd)} が rc={p.returncode} で失敗: {p.stderr.decode(errors='replace').strip()[:300]}")
    return p.stdout


class GitHubSource:
    def tree(self, repo):
        data = json.loads(run(["gh", "api", f"repos/{repo}/git/trees/HEAD?recursive=1"]))
        if data.get("truncated"):
            raise MeasureError(f"{repo} の tree が途中で切れた (GitHub API の上限)")
        return [(e["mode"], e["type"], e["sha"], e["path"]) for e in data["tree"]]

    def blob(self, repo, sha):
        data = json.loads(run(["gh", "api", f"repos/{repo}/git/blobs/{sha}"]))
        return base64.b64decode(data["content"])


class LocalSource:
    def __init__(self, root):
        self.root = root

    def _dir(self, repo):
        d = os.path.join(self.root, repo)
        if not os.path.isdir(d):
            raise MeasureError(f"{d} が無い")
        return d

    def tree(self, repo):
        out = run(["git", "-C", self._dir(repo), "ls-tree", "-r", "-t", "-z", "HEAD"]).decode()
        entries = []
        for rec in out.split("\0"):
            if not rec:
                continue
            meta, path = rec.split("\t", 1)
            mode, typ, sha = meta.split()
            entries.append((mode, typ, sha, path))
        return entries

    def blob(self, repo, sha):
        return run(["git", "-C", self._dir(repo), "cat-file", "blob", sha])


def has_paths_frontmatter(text):
    """check_instruction_budget.sh の has_paths_frontmatter と同じ判定: 1 行目が --- で、閉じる --- までに ^paths: の行がある"""
    lines = text.split("\n")
    if not lines or lines[0] != "---":
        return False
    found = False
    for line in lines[1:]:
        if line == "---":
            return found
        if line.startswith("paths:"):
            found = True
    return False


class Counter:
    def __init__(self, source):
        self.source = source
        self.cache = {}
        self.dirty = False
        try:
            with open(CACHE_PATH, encoding="utf-8") as f:
                for line in f:
                    sha, n, p = line.rstrip("\n").split("\t")
                    self.cache[sha] = (int(n), p == "1")
        except (OSError, ValueError):
            self.cache = {}

    def count(self, repo, sha):
        """(字数, paths: がある) を返す。blob は sha で決まるので、cache は古くならない"""
        if sha not in self.cache:
            text = self.source.blob(repo, sha).decode("utf-8")
            self.cache[sha] = (len(text), has_paths_frontmatter(text))
            self.dirty = True
        return self.cache[sha]

    def save(self):
        if not self.dirty:
            return
        try:
            os.makedirs(os.path.dirname(CACHE_PATH), exist_ok=True)
            tmp = CACHE_PATH + f".{os.getpid()}"
            with open(tmp, "w", encoding="utf-8") as f:
                for sha, (n, p) in self.cache.items():
                    f.write(f"{sha}\t{n}\t{'1' if p else '0'}\n")
            os.replace(tmp, CACHE_PATH)
        except OSError as e:
            print(f"⚠ cache を書けなかった ({e})。次も全部取り直す", file=sys.stderr)


# 1 段下のディレクトリを起動する場所と見なす目印 (ビルドの入口)。bin/ や docs/ の CLAUDE.md はそのディレクトリを Read したときに読まれるだけで、そこでは起動しない
PROJECT_MARKERS = ("Makefile", "package.json", "Package.swift", "project.yml", "go.mod")


class Repo:
    def __init__(self, name, source):
        self.name = name
        self.entries = source.tree(name)
        self.by_path = {path: (mode, typ, sha) for mode, typ, sha, path in self.entries}

    def layer_files(self, d):
        """ディレクトリ d の CLAUDE.md と .claude/rules/**/*.md のパス"""
        prefix = f"{d}/" if d else ""
        files = []
        if f"{prefix}CLAUDE.md" in self.by_path:
            files.append(f"{prefix}CLAUDE.md")
        rules = f"{prefix}.claude/rules/"
        files += sorted(p for p, (mode, typ, _) in self.by_path.items()
                        if p.startswith(rules) and p.endswith(".md") and typ == "blob")
        return files

    def start_dirs(self):
        dirs = [""]
        for p, (mode, typ, _) in self.by_path.items():
            if typ == "blob" and p.count("/") == 1 and p.endswith("/CLAUDE.md"):
                d = p.split("/")[0]
                if not d.startswith(".") and any(f"{d}/{m}" in self.by_path for m in PROJECT_MARKERS):
                    dirs.append(d)
        return sorted(dirs)


def layer_chars(repo, d, counter):
    total = 0
    for path in repo.layer_files(d):
        mode, _, sha = repo.by_path[path]
        if mode == "120000":
            raise MeasureError(f"{repo.name}:{path} は symlink。リンク先を数える経路が無い")
        n, has_paths = counter.count(repo.name, sha)
        if not has_paths:
            total += n
    return total


def ancestors(path):
    """'apps/obaket' -> ['', 'apps', 'apps/obaket']"""
    out = [""]
    parts = [x for x in path.split("/") if x]
    for i in range(len(parts)):
        out.append("/".join(parts[: i + 1]))
    return out


def submodules(umbrella, counter):
    if ".gitmodules" not in umbrella.by_path:
        raise MeasureError(f"{umbrella.name} に .gitmodules が無い")
    text = counter.source.blob(umbrella.name, umbrella.by_path[".gitmodules"][2]).decode("utf-8")
    subs, path = [], None
    for line in text.splitlines():
        m = re.match(r"\s*path\s*=\s*(\S+)", line)
        if m:
            path = m.group(1)
        m = re.match(r"\s*url\s*=\s*\S*?github\.com[:/]([^/\s]+/[^/\s]+?)(?:\.git)?\s*$", line)
        if m and path:
            subs.append((path, m.group(1)))
            path = None
    return subs


def measure(source, umbrella_name, extras):
    counter = Counter(source)
    sessions = []
    try:
        umbrella = Repo(umbrella_name, source)
        umbrella_layers = {}

        def umbrella_part(path):
            return sum(umbrella_layers.setdefault(a, layer_chars(umbrella, a, counter)) for a in ancestors(path)[:-1])

        for d in umbrella.start_dirs():
            sessions.append((sum(layer_chars(umbrella, a, counter) for a in ancestors(d)), f"{umbrella_name}:{d or '.'}"))
        subs = submodules(umbrella, counter)
        # tree の取得 (repo ごとに API 1 回) が所要の大半なので並べて取る
        with ThreadPoolExecutor(max_workers=8) as pool:
            repos = dict(zip([n for _, n in subs] + extras,
                             pool.map(lambda n: Repo(n, source), [n for _, n in subs] + extras)))
        for path, name in subs:
            repo = repos[name]
            base = umbrella_part(path) + umbrella_layers.setdefault(path, layer_chars(umbrella, path, counter))
            for d in repo.start_dirs():
                own = sum(layer_chars(repo, a, counter) for a in ancestors(d))
                sessions.append((base + own, f"{name}:{d or '.'} ({umbrella_name} の {path})"))
        for name in extras:
            repo = repos[name]
            for d in repo.start_dirs():
                sessions.append((sum(layer_chars(repo, a, counter) for a in ancestors(d)), f"{name}:{d or '.'}"))
    finally:
        counter.save()
    if not sessions:
        raise MeasureError("数えたセッションが 0")
    return sorted(sessions, reverse=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--umbrella", default="jiikko/my-products")
    ap.add_argument("--extra", action="append", default=[])
    ap.add_argument("--local")
    args = ap.parse_args()
    source = LocalSource(args.local) if args.local else GitHubSource()
    try:
        sessions = measure(source, args.umbrella, args.extra)
    except (MeasureError, KeyError, ValueError, UnicodeDecodeError) as e:
        print(f"✗ 他の repo の指示ファイルを数えられない: {e}", file=sys.stderr)
        return 2
    for n, label in sessions:
        print(f"{n}\t{label}")
    print(f"max\t{sessions[0][0]}\t{sessions[0][1]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
