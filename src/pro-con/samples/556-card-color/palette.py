#!/usr/bin/env python3
# issue 556 の見本 2 (使い捨て): 普段の地の色のパレット案を、普段と待ち (x0.7) で並べる。
# A = 今 (cube の最も暗い段 #5f)、B = 1 段明るい段 (#87)。B の茶 (94) は 1 段上げると
# 現在地 202 / 要対応 214 / 未 push 208 の橙に寄るので据え置く。
import unicodedata

LEVELS = [0, 95, 135, 175, 215, 255]
PALETTES = [
    ("A 今", [52, 17, 22, 53, 58, 23, 54, 94, 24, 89]),
    ("B 1 段明るい", [88, 18, 28, 90, 100, 30, 91, 94, 25, 125]),
]
WAIT = 0.7
W = 14
RESET = "\x1b[0m"
FG = "\x1b[38;5;252m"


def rgb(n):
    n -= 16
    return LEVELS[n // 36], LEVELS[n // 6 % 6], LEVELS[n % 6]


def width(s):
    return sum(2 if unicodedata.east_asian_width(ch) in "WF" else 1 for ch in s)


def cell(c, text):
    r, g, b = c
    return f"\x1b[48;2;{r};{g};{b}m{FG} {text}{' ' * (W - 1 - width(text))}{RESET}"


print(f"issue 556 見本 2: 普段の地の色のパレット案 (各案の上段 = 普段 / 下段 = 待ち x{WAIT})")
print("選んだ枠と現在地の橙 (202) を参考に並べる: \x1b[48;5;202m\x1b[38;5;16m\x1b[1m C-001 選択中 \x1b[0m")
for name, pal in PALETTES:
    print()
    print(name)
    for k, label in ((1.0, "普段"), (WAIT, "待ち")):
        cells = []
        for n in pal:
            c = tuple(int(v * k + 0.5) for v in rgb(n))
            cells.append(cell(c, f"{n} カード"))
        print(f"  {label} " + " ".join(cells[:5]))
        print(f"       " + " ".join(cells[5:]))
