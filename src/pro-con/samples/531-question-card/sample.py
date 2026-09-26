#!/usr/bin/env python3
"""確認のカード (問いだけ。issue 531) をボードで作業のカードと見分ける見本。使い捨て。本体 (src/pro-con) には入れていない。

使い方: python3 sample.py <案 A|B|C> > <案>.ans
  A = 1 行目の番号の後に「?確認」(題名の頭。桃色の太字)
  B = バッジ行 (待ちの理由の行) の先頭に「確認」(桃色)
  C = カードの左端の 1 桁を、3 行とも桃色の縦の帯にする (字は足さない)

見本が再現していないもの (本体との差):
  - 列の枠・ゲージ・詳細の欄 (列 2 本とカードだけ描く)。選択中の枠・spinner も描かない
  - 地の色はカード固有の色 (ui/style.go の cardPalette) から手で選んだ。待っているカードの地を暗くするのも省いた
"""
import sys
import unicodedata

RESET, BOLD, DIM = "\x1b[0m", "\x1b[1m", "\x1b[2m"
FGRESET = "\x1b[39m\x1b[22m"
YEL = "\x1b[38;5;214m"
CYAN = "\x1b[38;5;51m"
PINK = "\x1b[38;5;213m"
W = 40  # 列の内側の幅


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


def cut(text, w):
    """素の文字列 text の頭から表示幅 w まで (全角の途中では切らない) と残り。"""
    head = ""
    for i, ch in enumerate(text):
        if width(head) + cw(ch) > w:
            return head, text[i:].lstrip(" ")
        head += ch
    return head, ""


def title_lines(prefix, title, w):
    """prefix (色つき可) + title を 2 行に折り返す。2 行目に収まらない分は … で切る。"""
    l1, rest = cut(title, w - width(prefix))
    l2, rest = cut(rest, w)
    if rest:
        l2, _ = cut(l2, w - 1)
        l2 += "…"
    return [prefix + l1, l2]


def paint(base, s, w):
    return base + s.replace(RESET, RESET + base) + " " * max(w - width(s), 0) + RESET


# (ID, 地の色, 題名, 確認か, バッジ, 人の番か)
CARDS = [
    [("C-090", 22, "PG が起票するときの採番が衝突するのを直す", False, "1m", False),
     ("C-091", 53, "pro-con に足りない機能のうち、どれを issue にするか", True, "PM 分解中 ▸ Bash: pro-con card show C-091", False)],
    [("C-088", 94, "再開のキャッシュ外れを直す issue を起票するか", True, "?質問 3m", True),
     ("C-089", 24, "画面の幅が狭いとき詳細が切れる", False, "?質問 12m", False)],
]
LANES = [("1 依頼", 51), ("4 質問待ち", 214)]


def card_cell(plan, c):
    cid, color, title, question, badge, human = c
    base = bg(color) + fg(252)
    edge = " "
    prefix = cid + " "
    if human:
        b = YEL + BOLD + "!人の番" + FGRESET + " " + YEL + badge + FGRESET
    elif badge.startswith("PM"):
        b = CYAN + badge + FGRESET
    else:
        b = DIM + badge + RESET
    if question and plan == "A":
        prefix = cid + " " + PINK + BOLD + "?確認" + FGRESET + " "
    if question and plan == "B":
        b = PINK + BOLD + "確認" + FGRESET + " " + b
    if question and plan == "C":
        edge = PINK + "▌" + FGRESET
    if width(b) > W - 1:  # 本体と同じく列の幅で切る (末尾の色の戻しは残す)
        plain, _ = cut(strip(b), W - 2)
        b = b[:b.index(plain[-4:]) + 4] + "…" + RESET
    lines = title_lines(prefix, title, W - 1) + [b]
    return [paint(base, edge + l, W) for l in lines]


def main():
    plan = (sys.argv[1:] or ["A"])[0].upper()
    out = [BOLD + "案 %s" % plan + RESET, ""]
    out.append("  ".join(fg(col) + BOLD + name + RESET + " " * (W - width(name)) for name, col in LANES))
    cells = [[card_cell(plan, c) for c in lane] for lane in CARDS]
    for k in range(2):
        for row in range(3):
            out.append("  ".join(cells[i][k][row] for i in range(len(LANES))))
        out.append("")
    print("\n".join(out))


if __name__ == "__main__":
    main()
