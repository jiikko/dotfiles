#!/usr/bin/env python3
"""ボードの / の検索 (issue 532) の見本。使い捨て。本体 (src/pro-con) には入れていない。

使い方: python3 sample.py <案 A|B|C> <typing|done> > <案>-<段>.ans
  A = 検索の欄は最下段 (ほかの入力欄と同じ場所)。確定したら件数の行の頭に「/ glogx 3/11」の印。一致しないカードは隠す
  B = 検索の欄はヘッダの区切り線の行 (ボードの真上)。確定してもその行に残り、それが印を兼ねる。一致しないカードは隠す
  C = 欄と印は A と同じ。一致しないカードを隠さず暗く描く (カードの位置が動かない)
  typing = 「glogx」と打っている途中 / done = enter で確定した後 (ボードのキーが効く)

土台は board.txt (2026-09-27 に --mock を 160x40 の隔離 tmux で撮った文字だけの画面)。
見本が再現していないもの (本体との差):
  - 色はレーンの枠・カードの地を省いた (検索の欄と印だけ色を付けた)
  - 選択中のカードの二重枠は外した。隠したときは残ったカードを上へ詰める (本体も同じ)
"""
import sys
import unicodedata

RESET, BOLD, DIM = "\x1b[0m", "\x1b[1m", "\x1b[2m"
YEL = "\x1b[38;5;214m"
ORANGE = "\x1b[38;5;202m"
GREY = "\x1b[38;5;239m"
WHITE = "\x1b[38;5;231m"
FIELD = "\x1b[48;5;236m"
HIT = "\x1b[38;5;214m\x1b[1m"
Q = "glogx"
W = 160
LANE = 25  # レーンの枠の幅 (枠の字を含む)。レーンの間は空白 1 つ


def cw(ch): return 2 if unicodedata.east_asian_width(ch) in "WF" else 1
def width(s): return sum(cw(c) for c in s)


def cut(s, start, n):
    """表示幅で s[start:start+n] を切り出す。"""
    out, x = "", 0
    for ch in s:
        if start <= x < start + n:
            out += ch
        x += cw(ch)
    return out


def pad(s, n): return s + " " * max(n - width(s), 0)


def load():
    lines = [l.rstrip("\n").replace("\t", " ") for l in open(sys.path[0] + "/board.txt", encoding="utf-8")]
    return lines


def lanes_of(lines):
    """ボードの行 (4 行目から) を 6 レーンのカードの列に分ける。カード = 3 行。"""
    body = lines[4:]
    end = next(i for i, l in enumerate(body) if l.startswith("╰"))
    body = body[:end + 1]
    lanes = []
    for k in range(6):
        seg = [cut(l, k * (LANE + 1), LANE) for l in body]
        inner = [s[1:-1] for s in seg[1:-1]]  # 枠の字を外す
        cards, i = [], 1
        while i + 2 < len(inner):
            block = inner[i:i + 3]
            if block[0].strip().startswith("C-"):
                cards.append(block)
                i += 4
            else:
                i += 1
        lanes.append((seg[0], cards))
    return lanes, len(body)


def hit(card): return any(Q in l for l in card)


def mark_hit(line):
    return line.replace(Q, HIT + Q + RESET)


def board(lanes, height, plan):
    inner_w = LANE - 2
    rows = [[] for _ in range(height)]
    for top, cards in lanes:
        shown = [c for c in cards if hit(c)] if plan in "AB" else cards
        n = len(shown)
        head = top
        if plan in "AB":  # 見出しの枚数を一致した数にする
            head = head.replace("(%d)" % len(cards), "(%d/%d)" % (n, len(cards))).rstrip("─╮ ") + " "
            head = head + "─" * (LANE - 1 - width(head)) + "╮"
        col = [head] + ["│" + " " * inner_w + "│"]
        for c in shown:
            for l in c:
                if plan == "C" and not hit(c):
                    col.append("│" + GREY + l + RESET + "│")
                else:
                    col.append("│" + mark_hit(l) + "│")
            col.append("│" + " " * inner_w + "│")
        while len(col) < height - 1:
            col.append("│" + " " * inner_w + "│")
        col.append("╰" + "─" * inner_w + "╯")
        for r in range(height):
            rows[r].append(col[r])
    return [" ".join(r) for r in rows]


def field(typing):
    head = " " + BOLD + ORANGE + "/ 検索 (題名・カード ID・issue 番号・依頼の原文)" + ": " + RESET + WHITE
    text = Q + ("▏" if typing else "")
    return FIELD + head + text + " " * (W - width(strip(head)) - width(text)) + RESET


def strip(s):
    out, esc = "", False
    for ch in s:
        if ch == "\x1b":
            esc = True
        elif esc:
            if ch == "m":
                esc = False
        else:
            out += ch
    return out


HINT_TYPING = " enter 確定  esc やめる  ctrl+u 消す  (打つたびに絞る)"
HINT_DONE = " / 検索を直す  esc 絞り込みをやめる  hjkl 選択  enter 詳細  a attach  r 回答  e issue を開く  Q 終了"


def main():
    plan = (sys.argv[1:] or ["A"])[0].upper()
    stage = (sys.argv[2:] or ["done"])[0]
    typing = stage == "typing"
    lines = load()
    lanes, height = lanes_of(lines)
    total = sum(len(c) for _, c in lanes)
    hits = sum(1 for _, cs in lanes for c in cs if hit(c))
    header = lines[:4]
    mark = YEL + BOLD + "/ " + Q + RESET + YEL + " %d/%d 枚" % (hits, total) + RESET
    if plan in "AC" and not typing:
        header[2] = " " + mark + "  │" + header[2]
    if plan == "B":
        if typing:
            header[3] = field(True)
        else:
            header[3] = FIELD + " " + mark + FIELD + "   esc で戻る" + " " * (W - 14 - width(strip(mark))) + RESET
    out = [BOLD + "案 %s (%s)" % (plan, "打っている途中" if typing else "enter で確定した後") + RESET] + header
    out += board(lanes, height, plan)
    out += [""] * 4
    if typing and plan in "AC":
        out.append(field(True))
    out.append(HINT_TYPING if typing else HINT_DONE)
    print("\n".join(out))


if __name__ == "__main__":
    main()
