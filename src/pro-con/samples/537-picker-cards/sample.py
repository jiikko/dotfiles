#!/usr/bin/env python3
"""issue の一覧 (i) で、既にカードになっている issue にその旨を出す見本 (issue 537)。使い捨て。本体には入れていない。

使い方: python3 sample.py <案 A|B|C> [--width N] [--confirm X|Y]
  A = 右に札: 題名・状態の後ろに「C-081 作業中」(ID と列の名前を列の色で)
  B = 行頭の印 + 右寄せ: 番号の前に列の色の ● (カードが無ければ空白)、カードは枠の右端へ寄せる
  C = 題名の前の地色の札: 「#100 [C-081 作業中] タイトル」(札は列の色の地に黒字)
  --confirm: カードがある issue で Enter を押した後の確認
  X = 一覧の下の 1 行 (y/N)   Y = 送る前の確認と同じオレンジの枠を中央に重ねる

見本が再現していないもの (本体との差):
  - カンバン (板の後ろ) は描かない。一覧の板だけ
  - 題名が長いときは、カードの札を残して題名の方を … で切る (本体もそうする)
"""
import sys
import unicodedata

RESET, BOLD, DIM = "\x1b[0m", "\x1b[1m", "\x1b[2m"
LABELS = ["依頼", "着手待ち", "作業中", "質問待ち", "レビュー", "完了"]
COLOR = [51, 250, 46, 214, 208, 240]  # ui/style.go の stateColor
REQ, PLAN, RUN, WAIT, REVIEW, DONE = range(6)


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


def width(s):
    return sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in strip(s))


def cut(s, w):
    """装飾なしの s を表示幅 w に切る (… 付き)。"""
    if width(s) <= w: return s
    out = ""
    for c in s:
        if width(out + c) > w - 1: break
        out += c
    return out + "…"


def pad(s, w): return s + " " * max(0, w - width(s))


# 実物の分布に寄せる: 未完了の issue の多くはカードが無い。1 枚・2 枚・完了だけ、が少し
ROWS = [
    ("epic", "415", "PM / PG 分離の設計", ["511", "536", "537", "540"]),
    ("child", "511", "issue の一覧から足した時点で issue を紐づける", [("C-081", RUN)]),
    ("child", "536", "レビューの列に入ったら PG の session を止める", [("C-089", PLAN)]),
    ("child", "537", "issue の一覧で、既にカードになっている issue にその旨を出す", [("C-090", RUN)]),
    ("child", "540", "設定画面の並びを見直す", []),
    ("plain", "498", "PM が依頼について人に聞く問いを画面に出す", [("C-070", DONE)]),
    ("plain", "520", "tmux のセッション復元で窓の順番が崩れる", []),
    ("plain", "524", "glogx の diff で改名を追う", [("C-075", REVIEW), ("C-088", WAIT)]),
    ("plain", "529", "statusline の利用枠の表示を短くする", []),
    ("plain", "531", "ci-log が paths filter の赤を見落とす", []),
    ("plain", "533", "zsh の起動を 50ms 速くする", []),
]
STATUS = {"537": "next", "511": "next"}
CURSOR = 3  # 537 の行


def cards_of(num):
    for kind, n, _, extra in ROWS:
        if n == num and kind != "epic": return extra
    return []


def tag_a(cards):
    """案 A / B の札: C-081 作業中 (完了は薄く 完了 C-070)。"""
    parts = []
    for cid, st in cards:
        if st == DONE: parts.append(DIM + fg(COLOR[DONE]) + "完了 " + cid + RESET)
        else: parts.append(fg(COLOR[st]) + BOLD + cid + RESET + " " + fg(COLOR[st]) + LABELS[st] + RESET)
    return "  ".join(parts)


def tag_c(cards):
    parts = []
    for cid, st in cards:
        if st == DONE: parts.append(DIM + "[完了 " + cid + "]" + RESET)
        else: parts.append(bg(COLOR[st]) + "\x1b[38;5;16m" + " " + cid + " " + LABELS[st] + " " + RESET)
    return " ".join(parts)


def line_of(style, row, inner):
    kind, num, title, extra = row
    if kind == "epic":
        with_card = sum(1 for k in extra if any(c[1] != DONE for c in cards_of(k)))
        note = DIM + "(未完了の子 %d · カードあり %d)" % (len(extra), with_card) + RESET
        return cut(" ▸ epic %s #%s %s" % (num, num, title), inner - width(note) - 2) + "  " + note
    cards = extra
    indent = "     " if kind == "child" else " "
    status = DIM + STATUS.get(num, "open") + RESET
    if style == "A":
        tail = "  " + status + ("   " + tag_a(cards) if cards else "")
        return cut("%s#%s %s" % (indent, num, title), inner - width(tail)) + tail
    if style == "B":
        live = [c for c in cards if c[1] != DONE]
        dot = " "
        if live: dot = fg(COLOR[live[0][1]]) + "●" + RESET
        elif cards: dot = fg(240) + "○" + RESET
        pre = indent[:-1] + dot + " "
        right = (tag_a(cards) + " ") if cards else ""
        room = inner - width(pre) - 2 - width(status) - width(right) - 1
        left = pre + cut("#%s %s" % (num, title), room) + "  " + status
        return left + " " * max(1, inner - width(left) - width(right)) + right
    t = (tag_c(cards) + " ") if cards else ""
    base = "%s#%s " % (indent, num)
    room = inner - width(base) - width(t) - width(status) - 2
    return base + t + cut(title, room) + "  " + status


def render(style, w, confirm):
    inner = w - 2
    border = fg(202)
    title = "issue を選んで依頼する — dotfiles"
    out = [border + "╭─ " + RESET + BOLD + title + RESET + border + " " + "─" * max(0, inner - 4 - width(title)) + "╮" + RESET]
    for i, row in enumerate(ROWS):
        line = line_of(style, row, inner)
        if i == CURSOR:
            line = fg(202) + "▌" + RESET + BOLD + line[1:]
        out.append(border + "│" + RESET + pad(line, inner) + RESET + border + "│" + RESET)
    if confirm == "X":
        q = " " + BOLD + fg(214) + "#537 は C-090 (作業中) が担当している。それでも足す? y/N" + RESET
        out.append(border + "│" + RESET + pad(q, inner) + border + "│" + RESET)
    out.append(border + "╰" + "─" * inner + "╯" + RESET)
    if confirm == "Y":
        bw = min(58, w - 4)
        bi = bw - 4
        head = "#537 はもうカードがある"
        box = [border + "╭─ " + RESET + head + border + " " + "─" * max(0, bw - 5 - width(head)) + "╮" + RESET]
        for t in ["", fg(46) + BOLD + "C-090" + RESET + " " + fg(46) + "作業中" + RESET + "  " + cut("issue の一覧で、既にカードになっている", bi - 14),
                  "", BOLD + fg(214) + "それでも新しい依頼として足す?" + RESET, "",
                  fg(244) + "y 足す (補足の入力へ)   他のキー 一覧へ戻る" + RESET]:
            box.append(border + "│ " + RESET + pad(t, bi) + border + " │" + RESET)
        box.append(border + "╰" + "─" * (bw - 2) + "╯" + RESET)
        top = (len(out) - len(box)) // 2
        left = (w - bw) // 2
        for j, b in enumerate(box):
            out[top + j] = " " * left + b  # 見本では後ろの板の行を消して重ねる (本体は layout.OverlayCentered)
    return "\n".join(out)


def main():
    args = sys.argv[1:]
    style = args[0] if args else "A"
    w = int(args[args.index("--width") + 1]) if "--width" in args else 100
    confirm = args[args.index("--confirm") + 1] if "--confirm" in args else ""
    print("案 %s (幅 %d)%s" % (style, w, " / 確認 " + confirm if confirm else ""))
    print(render(style, w, confirm))


main()
