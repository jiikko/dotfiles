#!/usr/bin/env python3
# issue 556 の見本 (使い捨て): カードの地の色 10 色を「普段」と「待っている」の数案で並べる。
# 縦 = パレットの色、横 = 普段と、待っているときの明度の倍率の案。どれが「色なし」(ほぼ黒) に見えるかを指してもらう。
import unicodedata

PALETTE = [52, 17, 22, 53, 58, 23, 54, 94, 24, 89]  # ui/style.go の cardPalette
LEVELS = [0, 95, 135, 175, 215, 255]


def rgb(n):
    n -= 16
    return LEVELS[n // 36], LEVELS[n // 6 % 6], LEVELS[n % 6]


def dim(k):
    return lambda n: tuple(int(v * k + 0.5) for v in rgb(n))


VARIANTS = [
    ("普段", dim(1.0)),
    ("待ち x0.55 (今)", dim(0.55)),
    ("待ち x0.7", dim(0.7)),
    ("待ち x0.8", dim(0.8)),
]

W = 20
RESET = "\x1b[0m"
FG = "\x1b[38;5;252m"


def width(s):
    return sum(2 if unicodedata.east_asian_width(ch) in "WF" else 1 for ch in s)


def pad(s, w):
    return s + " " * (w - width(s))


def cell(c, text):
    r, g, b = c
    return f"\x1b[48;2;{r};{g};{b}m{FG} {pad(text, W - 1)}{RESET}"


print("issue 556: カードの地の色 (縦 = パレットの色 / 横 = 普段と、待っているときの案)")
print("    " + " ".join(pad(name, W) for name, _ in VARIANTS))
for n in PALETTE:
    print(f"{n:>3} " + " ".join(cell(f(n), f"C-{n:03d} カードの題") for _, f in VARIANTS))
    print("    " + " ".join(cell(f(n), "#%02x%02x%02x" % f(n)) for _, f in VARIANTS))
    print()
