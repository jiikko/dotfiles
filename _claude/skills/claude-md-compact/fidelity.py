#!/usr/bin/env python3
"""圧縮の前後で、指示ファイルから消えた「規範の部品」と「文」を出す (claude-md-compact の手順 4)。

使い方 (パスは cwd からの相対):
  python3 -I fidelity.py --base <縮める前の rev> [--dest <移し先のファイルかディレクトリ>...] -- <縮めたファイル>...
  改名したファイル (ディレクトリ) は `旧パス=新パス` で渡す (<rev> の版は旧パスで、今の版は新パスで読む)。
  消して中身を移し先へ統合したファイルは `旧パス=` で渡す (今の版を空として数える)。

範囲 = 縮めたファイル + 移し先 (ディレクトリは配下の *.md。<rev> の版と今の版の両方から列挙する)。
  - 部品 (数で照合する): コードスパン・単位つきの数値・日付・`<名前> NNN` / `retro NNN` / `issue NNN` / `#NNN` の番号・URL。
    範囲の全ファイルを合わせた個数が <rev> より減ったら「消えた部品」。行を書き直しても個数は増えないので、
    元から在った記述では消失を隠さない。移した 1 個が 2 か所の消失を覆うこともない
  - 文 (人が読む): 縮めたファイルの <rev> の版の文のうち、照合先に同じ形が無いもの。照合先は、そのファイルの今の版と、
    範囲の他のファイルで <rev> の版に無かった文だけ。箇条書きの継続行は前の行につなぎ、文は「。」で切り、
    12 字未満の文は直前の文につなぐ (短い否定の語の消失を拾うため)。
    実例の括弧 (頭が番号・実測・実例) は本文から外し、括弧の中の「:」より後ろを別の文として照合する
rc: 0 = 消えた部品も読む候補も無い / 1 = 消えた部品がある / 3 = 読む候補だけがある /
    2 = 使い方の誤り・読めないファイル・どの対象も <rev> と今の版が同じ (--base に縮めた後の rev を渡した)・
        対象が <rev> か今の木の片方にしか無い (改名なら `旧=新`、統合して消したなら `旧=` と渡していない)。
    一部の対象だけ同じなら、その対象を ⚠ の行に出して照合から外す
検出しないもの: 移した先での文脈の取り違え・同じ形で残った文の意味の変化・範囲の外へ移したもの (赤に倒れる)
"""
import argparse
import difflib
import os
import pathlib
import re
import subprocess
import sys

KANJI = r"(?![一-鿿])"
UNIT = r"(?:秒|分|時間|日|字|文字|行|回|件|本|周|体|倍|段|ms|MiB|MB|KB|KiB|GB|B|k|%)"
ATOM_RES = [
    re.compile(r"`[^`\n]+`"),
    re.compile(r"(?<![\d.,])\d[\d,.]*\s?" + UNIT + KANJI),
    re.compile(r"\b\d{4}-\d{2}-\d{2}\b"),
    re.compile(r"\b(?:retro|issue) \d+(?!\d)"),
    re.compile(r"\b(?!retro\b|issue\b)[A-Za-z][\w-]* \d+(?![\d.])"),
    re.compile(r"#\d+(?!\d)"),
    re.compile(r"https?://[\x21-\x7e]+"),
]
EXAMPLE = re.compile(r"[(（]\s*(?:[\w-]+ )?(?:retro \d+|issue \d+|実測|実例)([^()（）]*)[)）]")


class Unreadable(Exception):
    pass


class Usage(Exception):
    pass


def git(*args):
    # surrogateescape: `-z` の出力は名前を生のバイトで返す。UTF-8 でない名前を例外にせず、os.path へそのまま渡せる形で読む
    r = subprocess.run(["git", *args], capture_output=True, text=True, encoding="utf-8", errors="surrogateescape")
    return r.returncode, r.stdout, r.stderr


def base_text(base, path):
    # ./ で cwd からの相対にする (素の path は repo root から解決される)。ファイルだけを読む (ディレクトリの show は tree の一覧を rc=0 で返す)
    rc, kind, _ = git("cat-file", "-t", f"{base}:./{path}")
    if rc != 0 or kind.strip() != "blob":
        return None
    rc, out, _ = git("show", f"{base}:./{path}")
    return out if rc == 0 else None


def norm(path):
    """表記の揺れ (`./`・`a/./b`・末尾の `/`) を 1 つにする。同じファイルを別の 1 本として 2 回数えないため"""
    return os.path.normpath(path) if path else path


def now_text(path):
    if not path:
        return None
    p = pathlib.Path(path)
    if not p.exists():
        return None
    try:
        return p.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as e:
        raise Unreadable(f"{path}: {e}") from e


def names_z(out):
    """`-z` の出力をパスに割る。`-z` なしの出力は、日本語や `"` を含む名前を C の形で引用し、今の木に無い字面になる"""
    return [f for f in out.split("\0") if f]


def at_base(base, path):
    """<rev> にそのパスのファイルかディレクトリが在るか"""
    rc, out, _ = git("ls-tree", "-z", "--name-only", base, "--", f"./{path}")
    return rc == 0 and bool(out.strip("\0"))


def base_file_same_as(base, path, exclude):
    """今の木の path と同じ実体 (inode) の、<rev> に在るファイル (exclude を除く) を 1 つ返す。無ければ None。
    パスの文字列で比べない: 大小を区別しないファイルシステム (`B.md` で `b.md`・`Docs/` で `docs/`) と symlink は、
    <rev> に無い字面で <rev> に在るファイルを読ませる"""
    if not os.path.exists(path):
        return None
    _, top, _ = git("rev-parse", "--show-toplevel")
    _, listed, _ = git("ls-tree", "-r", "-z", "--name-only", "--full-tree", base)
    for f in names_z(listed):
        here = os.path.relpath(os.path.join(top.strip(), f))
        if norm(here) != exclude and os.path.exists(here) and os.path.samefile(here, path):
            return here
    return None


def exists_exact(path):
    """今の木に、大小まで同じ名前で在るか (Path.exists は大小を区別しないファイルシステムでは `A.md` で `a.md` も真になる)"""
    parent, leaf = os.path.split(path)
    try:
        return leaf in os.listdir(parent or ".")
    except OSError:
        return False


def pair(base, spec):
    """`旧=新` / `旧=` / `パス` を (<rev> の版を読むパス, 今の版を読むパス) にする。今の版のパスが空なら、消したものとして空で数える。
    名前に `=` を含むパスは、今の木か <rev> に在れば素のパスとして読む"""
    if "=" not in spec or pathlib.Path(spec).exists() or at_base(base, spec):
        return norm(spec), norm(spec)
    o, n = (norm(x) for x in spec.split("=", 1))
    if not o:
        raise Usage(f"`{spec}`: `=` の左に <rev> の版のパスが要る")
    # 改名・統合の組でないものを組にすると、無関係な今のファイルや空の前の版が照合先に入り、消失を隠す (偽の緑)
    if not at_base(base, o):
        raise Usage(f"`{spec}`: {o} が {base} に無い (`=` の左は <rev> の版のパス)")
    if exists_exact(o):
        raise Usage(f"`{spec}`: {o} が今の木にまだ在る (改名・統合したファイルではない)")
    same = base_file_same_as(base, n, o) if n else None
    if same:
        raise Usage(f"`{spec}`: {n} は {base} に在る {same} と同じファイル (改名先ではない。中身を移した先なら --dest に素のパスで渡す)")
    return o, n


def expand(base, specs):
    """移し先を (旧, 新) の組に展開する。ディレクトリは配下の *.md を、<rev> の版 (旧の下) と今の版 (新の下) から列挙し、
    同じ相対パスどうしを組にする"""
    out = []
    for spec in specs:
        o, n = pair(base, spec)
        _, listed, _ = git("ls-tree", "-r", "-z", "--name-only", base, "--", f"./{o}")
        was = [f for f in names_z(listed) if f.endswith(".md")]
        if (n and pathlib.Path(n).is_dir()) or len(was) > 1 or (was and was[0] != o):
            rels = [str(pathlib.PurePath(f).relative_to(o)) for f in was]
            if n:
                rels += [str(f.relative_to(n)) for f in sorted(pathlib.Path(n).rglob("*.md"))]
            out.extend((str(pathlib.PurePath(o, r)), str(pathlib.PurePath(n, r)) if n else "") for r in dict.fromkeys(rels))
        else:
            out.append((o, n))
    return [(norm(x), norm(y)) for x, y in out]


def count_atoms(text):
    out = {}
    for r in ATOM_RES:
        for m in r.finditer(text or ""):
            k = re.sub(r"\s+", "", m.group(0)).rstrip("。、,.")
            out[k] = out.get(k, 0) + 1
    return out


def squeeze(s):
    return re.sub(r"[*_>`|]|\s+", "", s)


def sentences(text):
    """箇条書きの継続行をつなぎ、実例の括弧を中身ごと別の文に分けてから「。」で切る"""
    logical = []
    for line in (text or "").splitlines():
        if line.startswith((" ", "\t")) and logical and not line.lstrip().startswith(("- ", "* ", "|", "```")):
            logical[-1] += line.strip()
        else:
            logical.append(line.strip())
    out = []
    for line in logical:
        if not line or line.startswith(("```", "#")):
            continue
        inner = [m.group(1).split(":", 1)[-1].split("：", 1)[-1].strip() for m in EXAMPLE.finditer(line)]
        body = EXAMPLE.sub("", line).strip(" -|")
        parts = []
        for s in re.split(r"(?<=。)", body):
            s = s.strip()
            if not s:
                continue
            if parts and len(s) < 12:
                parts[-1] += s
            else:
                parts.append(s)
        out.extend(p for p in parts if len(p) >= 4)
        out.extend(i for i in inner if len(i) >= 4)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--dest", nargs="*", default=[])
    ap.add_argument("targets", nargs="+")
    a = ap.parse_args()
    # 範囲の 1 本 = (旧, 新) の組。名前は今の版のパス (消したものは旧パス)。改名の旧パスや新パスを素の 1 本としても数えると、
    # <rev> の版の部品を 2 回数えるか今の版の空を照合先に入れるか、同じ今の版を 2 本にするので、組の両端と同じ素のパスは落とす
    tpairs = [pair(a.base, t) for t in dict.fromkeys(a.targets)]
    pairs = list(dict.fromkeys(tpairs + expand(a.base, a.dest)))
    moved = {o for o, n in pairs if o != n} | {n for o, n in pairs if o != n and n}
    pairs = [(o, n) for o, n in pairs if not (o == n and o in moved)]
    tpairs = [pr for pr in tpairs if pr in pairs]
    name = {pr: pr[1] or pr[0] for pr in pairs}
    names = list(dict.fromkeys(name[pr] for pr in pairs))
    if len(names) != len(pairs):
        print("✗ 同じ今の版のパスが 2 つの組に出ている (`旧=新` の新を確かめる)", file=sys.stderr)
        return 2
    scope = names
    targets = [name[pr] for pr in tpairs]
    was_at = {name[pr]: pr[0] for pr in pairs if pr[0] != name[pr]}
    old = {name[pr]: base_text(a.base, pr[0]) for pr in pairs}
    new = {name[pr]: now_text(pr[1]) for pr in pairs}
    hint = "改名したなら `旧パス=新パス`、消して中身を移し先へ統合したなら `旧パス=` で渡す"
    for (o, n) in tpairs:
        t = name[(o, n)]
        if old[t] is None:
            print(f"✗ {a.base}:{o} を読めない ({hint})", file=sys.stderr)
            return 2
        if n and new[t] is None:
            print(f"✗ {n} が今の木に無い ({hint})", file=sys.stderr)
            return 2
    same = [t for t in targets if old[t] == new[t]]
    for t in same:
        print(f"⚠ {t} は {a.base} と今の版が同じ (照合しない。縮めたファイルなら --base を確かめる)", file=sys.stderr)
    if len(same) == len(targets):
        print("✗ どの対象も今の版と同じ (--base には縮める前の rev を渡す)", file=sys.stderr)
        return 2
    targets = [t for t in targets if t not in same]

    # 部品: 範囲の全ファイルを合わせた個数で比べる
    before, after = {}, {}
    for f in scope:
        for k, n in count_atoms(old[f]).items():
            before[k] = before.get(k, 0) + n
        for k, n in count_atoms(new[f]).items():
            after[k] = after.get(k, 0) + n
    lost_atoms = {k: before[k] - after.get(k, 0) for k in before if before[k] > after.get(k, 0)}

    # 文: そのファイルの今の版 + 範囲の他のファイルで新しく現れた文
    fresh = []
    for f in scope:
        was = {squeeze(s) for s in sentences(old[f])}
        fresh.extend(s for s in sentences(new[f]) if squeeze(s) not in was)
    fresh_pool = squeeze("\n".join(fresh))

    cand_total = 0
    for t in targets:
        own = new[t] or ""
        pool = squeeze(own) + fresh_pool
        lost_here = {k: d for k, d in lost_atoms.items() if count_atoms(old[t]).get(k, 0) > count_atoms(new[t]).get(k, 0)}
        cands = [s for s in sentences(old[t]) if squeeze(s) not in pool]
        cand_total += len(cands)
        if lost_here or cands:
            print(f"== {t}" + (f" (<rev> の版: {was_at[t]})" if t in was_at else ""))
        for k, d in sorted(lost_here.items()):
            print(f"  消えた部品: {k} (-{d})")
        near_lines = [l.strip() for l in own.splitlines() if l.strip()] + fresh
        for s in cands:
            short = [l for l in near_lines if difflib.SequenceMatcher(None, s, l).quick_ratio() > 0.3]
            near = difflib.get_close_matches(s, short, n=1, cutoff=0.0)
            print(f"  読む候補: {s[:200]}")
            print(f"    最も近い行: {near[0][:200] if near else '(無し)'}")
    orphan = {k: d for k, d in lost_atoms.items() if not any(count_atoms(old[t]).get(k, 0) > count_atoms(new[t]).get(k, 0) for t in targets)}
    for k, d in sorted(orphan.items()):
        print(f"  消えた部品 (移し先から): {k} (-{d})")
    print(f"対象 {len(targets)} 本 (範囲 {len(scope)} 本) / 消えた部品 {len(lost_atoms)} 件 / 読む候補 {cand_total} 件")
    if lost_atoms:
        return 1
    return 3 if cand_total else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Unreadable as e:
        print(f"✗ 読めない: {e}", file=sys.stderr)
        sys.exit(2)
    except Usage as e:
        print(f"✗ {e}", file=sys.stderr)
        sys.exit(2)
    except Exception as e:  # noqa: BLE001 — 想定外の失敗を python の既定の rc=1 (= 消えた部品がある) と読ませない
        print(f"✗ 判定できない: {type(e).__name__}: {e}", file=sys.stderr)
        sys.exit(2)
