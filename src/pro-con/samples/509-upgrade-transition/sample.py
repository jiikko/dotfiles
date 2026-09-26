#!/usr/bin/env python3
"""issue 509 の見本: ctrl+r の切り替えのトランジション (旧版が暗くなって消え、新版が明るく戻る)。

本体に入れる前に、長さ・暗くし方・文言を人に選んでもらうための見本 (decide-layout-in-sample-renderer-first)。
画面は本物の pro-con --mock を tmux capture-pane -e で撮った board.ans を使う。

  python3 sample.py --play <暗くし方> <長さms> <文言>   端末で 1 往復を動かして見せる (暗転 → 間 → 明転)
  python3 sample.py --frames                          静止のコマ (.ans) を書き出す (添付用)

暗くし方: fade = 色を背景 (黒) へ寄せる / gray = 色を抜きながら暗くする / sgr = SGR の dim (2) を一度に掛けるだけ
文言:     center = 中央の枠 / foot = 下端に 1 行 / none = 出さない
"""
import os
import re
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
BOARD = os.path.join(HERE, "board.ans")

# 既定の前景 (色を指定していない文字) の仮の色。暗くするには明示の色に直す必要がある (端末の既定色は知れない)。
# 本体では端末に問い合わせる (OSC 10 / 11)。見本は暗い背景の端末を前提にする
DEFAULT_FG = (208, 208, 208)
DEFAULT_BG = (0, 0, 0)

CUBE = [0, 95, 135, 175, 215, 255]
BASIC = [(0, 0, 0), (205, 0, 0), (0, 205, 0), (205, 205, 0), (0, 0, 238), (205, 0, 205), (0, 205, 205), (229, 229, 229),
         (127, 127, 127), (255, 0, 0), (0, 255, 0), (255, 255, 0), (92, 92, 255), (255, 0, 255), (0, 255, 255), (255, 255, 255)]


def rgb256(n):
    if n < 16:
        return BASIC[n]
    if n >= 232:
        v = 8 + (n - 232) * 10
        return (v, v, v)
    n -= 16
    return (CUBE[n // 36], CUBE[(n // 6) % 6], CUBE[n % 6])


SGR = re.compile(r"\x1b\[([0-9;:]*)m")


def parse_colors(params, fg, bg):
    """SGR の引数を読んで (fg, bg, 残す装飾) を返す。色は RGB か None (既定)。"""
    ps = [p for p in params.replace(":", ";").split(";")]
    if ps == [""]:
        ps = ["0"]
    keep = []
    i = 0
    while i < len(ps):
        p = int(ps[i] or 0)
        if p == 0:
            fg, bg = None, None
            keep.append("0")
        elif p in (38, 48):
            if ps[i + 1] == "5":
                c = rgb256(int(ps[i + 2]))
                i += 2
            else:
                c = (int(ps[i + 2]), int(ps[i + 3]), int(ps[i + 4]))
                i += 4
            if p == 38:
                fg = c
            else:
                bg = c
            i += 1
            continue
        elif p == 39:
            fg = None
        elif p == 49:
            bg = None
        elif 30 <= p <= 37:
            fg = BASIC[p - 30]
        elif 90 <= p <= 97:
            fg = BASIC[p - 90 + 8]
        elif 40 <= p <= 47:
            bg = BASIC[p - 40]
        elif 100 <= p <= 107:
            bg = BASIC[p - 100 + 8]
        else:
            keep.append(str(p))
        i += 1
    return fg, bg, keep


def mix(c, to, t):
    return tuple(int(a + (b - a) * t + 0.5) for a, b in zip(c, to))


def grayish(c, t):
    y = int(0.299 * c[0] + 0.587 * c[1] + 0.114 * c[2])
    return mix(c, (y, y, y), t)


def dim_frame(text, how, t):
    """t = 0 (元のまま) .. 1 (いちばん暗い)。how の暗くし方で、SGR の色を書き換える。"""
    if t <= 0:
        return text
    if how == "sgr":  # 一段だけ (端末の dim。途中の段が無い)
        return "\x1b[2m" + SGR.sub(lambda m: m.group(0) + "\x1b[2m", text)
    floor = 0.30  # いちばん暗いときの明るさ (0 = 真っ黒)
    k = 1 - (1 - floor) * t
    fg, bg = None, None
    out = []

    def conv(c, default, is_bg):
        if c is None:
            if is_bg:
                return None  # 既定の地はそのまま (暗い背景の前提)
            c = default
        if how == "gray":
            c = grayish(c, min(1, t * 1.4))
        return mix(DEFAULT_BG, c, k)

    def repl(m):
        nonlocal fg, bg
        fg, bg, keep = parse_colors(m.group(1), fg, bg)
        f = conv(fg, DEFAULT_FG, False)
        b = conv(bg, DEFAULT_BG, True)
        s = [*keep]
        s.append("38;2;%d;%d;%d" % f)
        s.append("49" if b is None else "48;2;%d;%d;%d" % b)
        return "\x1b[" + ";".join(s) + "m"

    # 行頭の既定色も暗くする (SGR が 1 つも無い行がある)
    lines = []
    for line in text.split("\n"):
        f = conv(fg, DEFAULT_FG, False)
        head = "\x1b[38;2;%d;%d;%dm" % f
        lines.append(head + SGR.sub(repl, line))
        out.append(None)
    return "\n".join(lines)


def visible_width(s):
    w = 0
    for ch in s:
        w += 2 if ord(ch) > 0x2E80 else 1
    return w


LABEL = "新版へ切り替え中…"
LABEL_IN = "新版に切り替えた"


def overlay_label(frame, where, label, alpha):
    """文言を重ねる。alpha は文言の明るさ 0..1 (暗転に合わせて浮かび上がる)。"""
    if where == "none" or alpha <= 0:
        return frame
    lines = frame.split("\n")
    rows, cols = len(lines), 150
    v = int(255 * alpha)
    col = "\x1b[0;1;38;2;%d;%d;%dm" % (v, int(v * 0.55), 0)  # 現在地の 202 (オレンジ) 寄り
    body = " ↻ " + label + " "
    w = visible_width(body)
    if where == "center":
        x = (cols - w - 2) // 2
        y = rows // 2 - 1
        box = [
            "╭" + "─" * w + "╮",
            "│" + body + "│",
            "╰" + "─" * w + "╯",
        ]
        for i, b in enumerate(box):
            lines[y + i] = splice(lines[y + i], x, col + b + "\x1b[0m", w + 2)
    else:  # foot: 下端の 1 行を置き換える
        lines[rows - 1] = col + body + "\x1b[0m" + "\x1b[K"
    return "\n".join(lines)


ANSI = re.compile(r"\x1b\[[0-9;:?]*[A-Za-z]")


def splice(line, x, ins, w):
    """表示の x セル目から w セルを ins で置き換える (SGR を保つ)。"""
    out, cells, i, sgr_state = [], 0, 0, ""
    # 左
    while i < len(line) and cells < x:
        m = ANSI.match(line, i)
        if m:
            out.append(m.group(0))
            sgr_state += m.group(0)
            i = m.end()
            continue
        cw = 2 if ord(line[i]) > 0x2E80 else 1
        if cells + cw > x:
            out.append(" ")
            cells += 1
            i += 1
            break
        out.append(line[i])
        cells += cw
        i += 1
    out.append(ins)
    # 右: w セルぶん飛ばす
    skip = 0
    while i < len(line) and skip < w:
        m = ANSI.match(line, i)
        if m:
            sgr_state += m.group(0)
            i = m.end()
            continue
        skip += 2 if ord(line[i]) > 0x2E80 else 1
        i += 1
    out.append(sgr_state)
    if skip > w:
        out.append(" ")
    out.append(line[i:])
    return "".join(out)


def ease_out(p):
    q = 1 - p
    return 1 - q * q * q


def frame_at(board, how, label, t, which):
    f = dim_frame(board, how, t)
    return overlay_label(f, label, LABEL if which == "out" else LABEL_IN, t if which == "out" else t)


def play(board, how, ms, label):
    out = sys.stdout
    out.write("\x1b[?1049h\x1b[?25l")
    try:
        for _ in range(2):
            draw(board)
            time.sleep(1.2)
            steps = max(1, ms // 16)
            for s in range(steps + 1):  # 旧版: 暗くなる
                draw(frame_at(board, how, label, ease_out(s / steps), "out"))
                time.sleep(ms / 1000 / steps)
            time.sleep(0.35)  # exec と新版の起動の間 (暗いまま止まる。実測の見込み)
            for s in range(steps + 1):  # 新版: 明るく戻る
                draw(frame_at(board, how, label, 1 - ease_out(s / steps), "in"))
                time.sleep(ms / 1000 / steps)
        time.sleep(1.0)
    finally:
        out.write("\x1b[0m\x1b[?25h\x1b[?1049l")
        out.flush()


def draw(frame):
    sys.stdout.write("\x1b[?2026h\x1b[H" + frame.replace("\n", "\x1b[K\r\n") + "\x1b[0m\x1b[?2026l")
    sys.stdout.flush()


def main():
    board = open(BOARD, encoding="utf-8").read().rstrip("\n")
    if sys.argv[1:2] == ["--play"]:
        how, ms, label = sys.argv[2], int(sys.argv[3]), sys.argv[4]
        play(board, how, ms, label)
        return
    if sys.argv[1:2] == ["--frames"]:
        for how in ("fade", "gray", "sgr"):
            for label in ("center", "foot"):
                name = os.path.join(HERE, "%s-%s-dark.ans" % (how, label))
                with open(name, "w", encoding="utf-8") as f:
                    f.write(frame_at(board, how, label, 1.0, "out") + "\x1b[0m\n")
            name = os.path.join(HERE, "%s-half.ans" % how)
            with open(name, "w", encoding="utf-8") as f:
                f.write(frame_at(board, how, "none", 0.5, "out") + "\x1b[0m\n")
        return
    print(__doc__)


if __name__ == "__main__":
    main()
