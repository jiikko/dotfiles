#!/usr/bin/env python3
"""pro-con のカードの依存を見るビュー (issue 485) の見本。使い捨て。本体 (src/pro-con) には入れていない。

使い方:
  python3 tmp/pro-con-485-sample.py [--at HH:MM] [--sel C-xxx] [--width N] [--cards PATH] [案 ...]
  案: 1A 1B 2 3 4 5 6 (省略すると全部)。--at を省くと今。--at 01:05 は issue 485 の実例の時刻
データ: 本物の記録 (~/.local/state/pro-con/live/cards.json) の After と ParentID を読む (架空のカードは使わない)。
  --at の時刻の列は、各カードの History の文から巻き戻す (「PG を起動した」→ 作業中 等。下の STATE_BY_EVENT)。
  片付けたカード (x で記録から外したもの) は記録に無いので出ない。

見本が再現していないもの (本体との差):
  - 列の判定は History の文から推定している (本体は State を持つ)。人の番は「PM が人に回した」の後だけ。
    レビュー列の人の番 (取り込みの係が居ないとき) は再現していない
  - 選択の枠は上下の横線だけ (本体は赤い二重線)。spinner・待っているカードの地の暗さ (455)・右上の見積もり (490) は描かない
  - 完了の列は最後の 3 枚だけ描く
"""
import argparse
import datetime as dt
import json
import os
import sys
import unicodedata

RESET, BOLD, DIM, UL, NOUL = "\x1b[0m", "\x1b[1m", "\x1b[2m", "\x1b[4m", "\x1b[24m"
FGR = "\x1b[39m\x1b[22m"
YEL, CYAN, RED = "\x1b[38;5;214m", "\x1b[38;5;51m", "\x1b[38;5;196m"
PALETTE = [52, 17, 22, 53, 58, 23, 54, 94, 24, 89]  # ui/style.go の cardPalette
LABELS = ["依頼", "分解済み", "作業中", "質問待ち", "レビュー", "完了"]
STATE_COLOR = [51, 250, 46, 214, 208, 240]  # ui/style.go の stateColor
REQ, PLAN, RUN, WAIT, REVIEW, DONE = range(6)

# History の文の先頭 → 列 (None は列を変えない)
STATE_BY_EVENT = [
    ("依頼を受けた", REQ), ("タスクに分けてキューに積んだ", PLAN), ("PG を起動した", RUN), ("PG を再開した", RUN),
    ("質問", WAIT), ("PM が人に回した", WAIT), ("人間 が回答した", PLAN), ("PM が回答した", PLAN),
    ("PG が終えた", REVIEW), ("差し戻した", PLAN), ("完了にした", DONE), ("pro-con の終了で PG を止めた", PLAN),
    ("PG の session が一覧から消えて戻らない", PLAN),
]


def fg(n): return "\x1b[38;5;%dm" % n
def bg(n): return "\x1b[48;5;%dm" % n


def strip(s):
    out, esc = "", False
    for ch in s:
        if ch == "\x1b": esc = True
        elif esc:
            if ch == "m": esc = False
        else: out += ch
    return out


def cw(ch): return 2 if unicodedata.east_asian_width(ch) in "WF" else 1
def width(s): return sum(cw(ch) for ch in strip(s))


def fit(s, w):
    """SGR を含む s を表示幅 w で切り (溢れたら …)、足りなければ空白で埋める。"""
    over = width(s) > w
    out, cur, esc = "", 0, False
    for ch in s:
        if ch == "\x1b": esc = True
        if esc:
            out += ch
            if ch == "m": esc = False
            continue
        if over and cur + cw(ch) > w - 1:
            out += "…"; cur += 1
            break
        out += ch; cur += cw(ch)
    return out + " " * max(w - cur, 0)


def fnv32a(s):
    h = 0x811c9dc5
    for b in s.encode():
        h = ((h ^ b) * 0x01000193) & 0xffffffff
    return h


def color(cid): return PALETTE[fnv32a(cid) % len(PALETTE)]


def parse_time(s):
    import re
    return dt.datetime.fromisoformat(re.sub(r"(\.\d{6})\d*", r"\1", s))


class Card:
    def __init__(self, raw, at):
        self.id, self.parent, self.after = raw["ID"], raw.get("ParentID") or "", raw.get("After") or []
        t = raw["Title"]
        self.title = t
        self.state, self.human, self.stalled, self.born = None, False, False, None
        for e in raw["History"]:
            when = parse_time(e["At"])
            if at and when > at: break
            if self.born is None: self.born = when
            txt = e["Text"]
            if txt.startswith("watchdog"): self.stalled = True
            for pre, st in STATE_BY_EVENT:
                if txt.startswith(pre):
                    if st != self.state: self.stalled = False
                    self.state = st
                    self.human = pre == "PM が人に回した"
                    break
        if not at:  # 今は記録の State が正本
            self.state = raw["State"]
            self.stalled = raw.get("Stalled", False)
        if self.state == REQ:  # After は分解のときに付く
            self.after = []


class Graph:
    def __init__(self, cards, keep_done):
        self.cards = cards
        self.by = {c.id: c for c in cards}
        live = lambda i: i in self.by and (keep_done or self.by[i].state != DONE)
        self.before = {c.id: [a for a in c.after if live(a)] for c in cards if live(c.id)}
        self.nexts = {i: [] for i in self.before}
        for i, bs in self.before.items():
            for b in bs: self.nexts[b].append(i)
        self.edges = sum(len(v) for v in self.before.values())

    def blockers(self, cid):  # card.Blockers と同じ: 完了していない前のカード
        return [a for a in self.by[cid].after if a in self.by and self.by[a].state != DONE]

    def downstream(self, cid):  # 完了していない、その先まで辿った後のカード
        seen, st = [], [n for n in self.nexts.get(cid, [])]
        while st:
            x = st.pop(0)
            if x not in seen and self.by[x].state != DONE:
                seen.append(x); st += self.nexts.get(x, [])
        return seen

    def roots(self):
        return [i for i in self.before if not self.before[i] and self.nexts[i]]


def node(c, short=False):
    """ノード = カード固有の地の ID + 状態の色の列の名前 (ボードと同じカードを目で追える)。"""
    s = bg(color(c.id)) + fg(252) + " " + c.id + " " + RESET + " " + fg(STATE_COLOR[c.state]) + LABELS[c.state] + FGR
    if c.human: s += " " + YEL + BOLD + "!人" + FGR
    if c.stalled: s += " " + RED + BOLD + "停滞" + FGR
    if c.state == DONE: s = DIM + strip(s) + RESET
    return s


# ---------- 案 1A: 層 (左 = 先に終わるべき → 右 = 後) ----------
def view_1a(g):
    NW = 24
    lines, placed = [], set()

    def walk(i):
        c = g.by[i]
        label = fit(node(c), NW)
        if i in placed:
            return [fit(node(c) + DIM + " ↑" + RESET, NW)]
        placed.add(i)
        kids = g.nexts.get(i, [])
        if not kids:
            return [label]
        out = []
        for k, kid in enumerate(kids):
            sub = walk(kid)
            last = k == len(kids) - 1
            if k == 0:
                conn = "──→ " if len(kids) == 1 else "─┬→ "
                out.append(label + fg(244) + conn + RESET + sub[0])
            else:
                out.append(" " * NW + fg(244) + (" └→ " if last else " ├→ ") + RESET + sub[0])
            for s in sub[1:]:
                out.append(" " * NW + fg(244) + ("    " if last else " │  ") + RESET + s)
        return out

    for r in g.roots():
        lines += walk(r)
    return lines or [DIM + "(依存のあるカードが無い)" + RESET]


# ---------- 案 1B / 4: 縦の木 (タイトル付き。狭い幅・CLI の text) ----------
def outline(g, plain=False):
    out, placed = [], set()

    def walk(i, pre, tail):
        c = g.by[i]
        others = [b for b in g.before[i] if b != tail]
        extra = ("  (%s の完了も待つ)" % ", ".join(others)) if tail and others else ""
        if plain:
            head = "%s %s%s" % (c.id, LABELS[c.state], " !人の番" if c.human else "")
            title = c.title
        else:
            head = node(c)
            title = (DIM if c.state == DONE else "") + c.title + RESET
        seen = i in placed
        out.append((pre + head, ("↑ 上に出た" if seen else title) + extra))
        if seen: return
        placed.add(i)
        kids = g.nexts.get(i, [])
        base = pre.replace("└─ ", "   ").replace("├─ ", "│  ")
        for k, kid in enumerate(kids):
            walk(kid, base + ("└─ " if k == len(kids) - 1 else "├─ "), i)

    for r in g.roots():
        walk(r, "", None)
    lw = max((width(l) for l, _ in out), default=0) + 2  # タイトルの桁を揃える
    return [fit(l, lw) + t for l, t in out] or ["(依存のあるカードが無い)"]


def mermaid(g):
    out = ["flowchart LR"]
    for i in g.before:
        if g.before[i] or g.nexts[i]:
            c = g.by[i]
            out.append('  %s["%s %s"]' % (i.replace("-", ""), i, LABELS[c.state]))
    for i, bs in g.before.items():
        for b in bs: out.append("  %s --> %s" % (b.replace("-", ""), i.replace("-", "")))
    for c in g.cards:
        if c.parent and c.id in g.before and c.parent in g.before:
            out.append("  %s -.-> %s" % (c.parent.replace("-", ""), c.id.replace("-", "")))
    return out


def dot(g):
    out = ["digraph cards {", "  rankdir=LR;"]
    for i, bs in g.before.items():
        for b in bs: out.append('  "%s" -> "%s";' % (b, i))
    for c in g.cards:
        if c.parent and c.id in g.before and c.parent in g.before:
            out.append('  "%s" -> "%s" [style=dashed];' % (c.parent, c.id))
    return out + ["}"]


# ---------- 案 5: 止まっている鎖の先頭と理由 ----------
def reason(c):
    if c.stalled: return RED + BOLD + "停滞" + FGR
    if c.state == WAIT: return "人の回答待ち" if c.human else "質問待ち (PM の番)"
    if c.state == REVIEW: return "取り込みの係の番"
    if c.state == PLAN: return "PG の空き待ち"
    if c.state == RUN: return "PG が作業している"
    return LABELS[c.state]


def heads(g):
    hs = [i for i in g.before if g.by[i].state != DONE and not g.blockers(i) and g.downstream(i)]
    return sorted(hs, key=lambda i: -len(g.downstream(i)))


def view_5a(g):
    out = []
    for h in heads(g):
        d = g.downstream(h)
        out.append("⛓ " + node(g.by[h]) + " " + reason(g.by[h]) + DIM + " → %d 枚が待つ: %s" % (len(d), ", ".join(d)) + RESET)
    return out or [DIM + "⛓ 止まっている鎖は無い" + RESET]


def view_5b(g):
    hs = heads(g)
    if not hs: return [DIM + "⛓ 0" + RESET]
    return ["⛓ " + "  ".join("%s %s %d" % (fg(STATE_COLOR[g.by[h].state]) + h + FGR,
                                          (YEL + BOLD + "!人" + FGR) if g.by[h].human else LABELS[g.by[h].state],
                                          len(g.downstream(h))) for h in hs)]


# ---------- ボード (案 2 / 3 / 6) ----------
def board(g, w, sel, count=None, mark=None, nest=False, cols=(PLAN, RUN, WAIT, REVIEW, DONE)):
    before = g.before.get(sel, [])
    after = g.nexts.get(sel, [])
    related = set([sel] + before + after)

    def cell(c):
        base = bg(color(c.id)) + fg(252)
        t = c.title
        idpart = c.id
        if mark == "2B" and c.id in before + after:
            idpart = bg(51) + fg(16) + BOLD + c.id + FGR + base
        pre = fg(231) + BOLD + UL if c.id == sel else ""
        head = []
        if c.human: head.append(YEL + BOLD + "!人の番" + FGR)
        if mark == "2A":
            if c.id in before: head.append(CYAN + BOLD + "前" + FGR)
            if c.id in after: head.append(CYAN + BOLD + "後" + FGR)
        n = len(g.downstream(c.id)) if c.id in g.before else 0
        tail = []
        if n and c.state != DONE:
            tint = YEL if c.human else ""
            if count == "3A": head.append(tint + BOLD + "塞%d" % n + FGR)
            if count == "3B": head.append(tint + BOLD + "%d枚を塞ぐ" % n + FGR)
            if count == "3C": tail.append(tint + BOLD + "塞ぐ%d" % n + FGR)
        bl = g.blockers(c.id)
        why = ("…%s の後" % ", ".join(bl)) if bl and not nest else ""
        if c.stalled: why = RED + BOLD + "🚨停滞" + FGR
        elif c.state == WAIT: why = "?質問"
        body = (YEL if c.human else DIM) + why + FGR + RESET
        badge = " ".join(head + ([body] if why else []) + tail)
        # タイトル 2 行 (本体の cardCell と同じ 3 行のカード)。ID を除いた幅で折る
        room, l1, cur = w - 1 - len(c.id) - 1, "", 0
        for ch in t:
            if cur + cw(ch) > room: break
            l1 += ch; cur += cw(ch)
        lines = [" " + pre + idpart + " " + l1 + NOUL, "  " + t[len(l1):].lstrip(), " " + badge]
        dimmed = mark == "2C" and sel in g.before and (before or after) and c.id not in related
        out = []
        for l in lines:
            if dimmed: l = fg(242) + strip(l)
            out.append(base + fit(l.replace(RESET, RESET + base), w) + RESET)
        return out

    V = fg(240) + "│" + RESET
    blocks = []
    for col in cols:
        cs = [c for c in g.cards if c.state == col]
        if col == DONE: cs = cs[-3:]
        n = len([c for c in g.cards if c.state == col])
        name = "%s (%d)" % (LABELS[col], n)
        rows = [fg(240) + "╭─ " + fg(STATE_COLOR[col]) + BOLD + name + RESET + fg(240) + " " + "─" * max(0, w - 3 - width(name)) + "╮" + RESET]
        order = cs
        if nest and col == PLAN:  # 案 6: 前のカードごとに見出しを付けて束ねる
            groups = {}
            for c in cs:
                key = ", ".join(g.blockers(c.id)) or ""
                groups.setdefault(key, []).append(c)
            order = []
            for key in sorted(groups, key=lambda k: (k != "", k)):
                order.append(("hdr", key))
                order += groups[key]
        for c in order:
            if isinstance(c, tuple):
                key = c[1]
                if key:
                    ks = ", ".join(fg(STATE_COLOR[g.by[k].state]) + k + fg(244) for k in key.split(", "))  # ID を前のカードの列の色で
                    txt = fg(244) + "┄ " + ks + " の後 ┄"
                else:
                    txt = fg(244) + "┄ すぐ起動できる ┄"
                rows.append(V + fit(txt + RESET, w) + RESET + V)
                continue
            gap = fg(196) + "═" * w + RESET if c.id == sel else " " * w
            rows.append(V + gap + V)
            rows += [V + l + V for l in cell(c)]
            if c.id == sel:
                rows.append(V + fg(196) + "═" * w + RESET + V)
        blocks.append(rows)
    h = max(len(b) for b in blocks)
    for b in blocks:
        while len(b) < h: b.append(V + " " * w + V)
        b.append(fg(240) + "╰" + "─" * w + "╯" + RESET)
    return [" ".join(r) for r in zip(*blocks)]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--at")
    ap.add_argument("--sel")
    ap.add_argument("--width", type=int, default=22)
    ap.add_argument("--cards", default=os.path.expanduser("~/.local/state/pro-con/live/cards.json"))
    ap.add_argument("views", nargs="*")
    a = ap.parse_args()
    views = a.views or ["1A", "1B", "2", "3", "4", "5", "6"]
    raw = json.load(open(a.cards))["cards"]
    at = None
    if a.at:
        last = max(parse_time(e["At"]) for r in raw for e in r["History"])  # HH:MM は最後の出来事から遡って 24 時間以内
        hh, mm = map(int, a.at.split(":"))
        at = last.replace(hour=hh, minute=mm, second=0, microsecond=0)
        if at > last: at -= dt.timedelta(days=1)
    cards = [c for c in (Card(r, at) for r in raw) if c.state is not None]
    live = Graph(cards, keep_done=False)
    full = Graph(cards, keep_done=True)
    sel = a.sel
    if not sel:  # 前も後もあるカードのうち、塞いでいる枚数の多いもの
        cand = [i for i in live.before if live.before[i] and live.nexts[i]] or [i for i in live.before if live.nexts[i]] or list(live.before)
        sel = max(cand, key=lambda i: len(live.downstream(i))) if cand else ""
    when = a.at or "今"
    nplan = len([c for c in cards if c.state != DONE])
    print(BOLD + "### 時刻 %s  カード %d 枚 (完了していない %d 枚)  After の線 %d 本 (完了していないものの間 %d 本)  ParentID %d 本  選択中 %s" % (
        when, len(cards), nplan, full.edges, live.edges, len([c for c in cards if c.parent]), sel or "-") + RESET)
    print()
    W = a.width
    if "1A" in views:
        print(BOLD + "=== 案 1A: 別ビュー (g で切り替え) の層。左が先に終わるべきカード。完了したカードを外す" + RESET)
        print("\n".join(view_1a(live))); print()
        print(BOLD + "=== 案 1A': 同じで、完了したカードも暗く残す (決めること 2)" + RESET)
        print("\n".join(view_1a(full))); print()
    if "1B" in views:
        print(BOLD + "=== 案 1B: 別ビューを縦の木で (タイトル付き。狭い端末でも崩れない)" + RESET)
        print("\n".join(outline(live))); print()
    if "2" in views and sel:
        for m, d in (("2A", "前後のカードのバッジの先頭に「前」「後」"), ("2B", "前後のカードの ID をシアンの地で反転"), ("2C", "前後でも選択中でもないカードを暗くする")):
            print(BOLD + "=== 案 %s: ボードのまま、選択中 %s の%s" % (m, sel, d) + RESET)
            print("\n".join(board(live, W, sel, mark=m))); print()
    if "3" in views:
        for m, d in (("3A", "バッジの先頭に「塞3」"), ("3B", "バッジの先頭に「3枚を塞ぐ」")):
            print(BOLD + "=== 案 %s: 塞いでいる枚数 (その先まで辿った、完了していない後のカード) を%s" % (m, d) + RESET)
            print("\n".join(board(live, W, sel, count=m))); print()
    if "4" in views:
        print(BOLD + "=== 案 4: pro-con card graph (読むだけの CLI。既定は text)" + RESET)
        print("\n".join(outline(live, plain=True))); print()
        print(BOLD + "=== 案 4: pro-con card graph --mermaid (完了も含める。ParentID は点線)" + RESET)
        print("\n".join(mermaid(full))); print()
        print(BOLD + "=== 案 4: pro-con card graph --dot" + RESET)
        print("\n".join(dot(full))); print()
    if "5" in views:
        print(BOLD + "=== 案 5A: 止まっている鎖の先頭と理由を、ボードの上に鎖ごとに 1 行" + RESET)
        print("\n".join(view_5a(live))); print()
        print(BOLD + "=== 案 5B: 同じものをヘッダの 1 行に詰める (先頭の ID / 理由 / 待っている枚数)" + RESET)
        print("\n".join(view_5b(live))); print()
    if "6" in views:
        print(BOLD + "=== 案 6 (追加): 分解済みの列を「前のカードごと」に束ねる (バッジの「…の後」を見出しへ移す)" + RESET)
        print("\n".join(board(live, W, sel, nest=True))); print()


main()
