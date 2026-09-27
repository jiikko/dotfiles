#!/usr/bin/env python3
"""持ち主の画面が無い join の画面で、ヘッダにその旨と dispatcher の状態を出す見本 (issue 543)。使い捨て。本体には入れていない。

画面は本物の pro-con --mock を tmux capture-pane -e で撮った board.ans を使い、ヘッダだけ差し替える。

使い方: python3 sample.py <案 A|B|C> <run|stop>
  run  = 持ち主 0・dispatcher は動いている (止まっても誰も起こさない)
  stop = 持ち主 0・dispatcher が止まっている (カードは進まない)
  A = 罫線の上に全幅の帯を 1 行足す (ヘッダ 5 行)
  B = タイトル行を帯に置き換える (ヘッダ 4 行のまま)
  C = 全幅の帯を 2 行 (見出し + 起こし方) 足す (ヘッダ 6 行)

見本が再現していないもの (本体との差):
  - 下の方の行は、足した行の分だけ切って高さを揃える (本体はボードの高さが縮む)
"""
import os
import re
import sys
import unicodedata

RESET, BOLD = "\x1b[0m", "\x1b[1m"
W = 150


def fg(n): return "\x1b[38;5;%dm" % n
def bg(n): return "\x1b[48;5;%dm" % n


def width(s):
    s = re.sub(r"\x1b\[[0-9;]*m", "", s)
    return sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in s)


def band(text, back, front):
    return bg(back) + fg(front) + BOLD + text + " " * max(0, W - width(text)) + RESET


RUN_HEAD = " ⚠ 持ち主の画面が無い — dispatcher は今は動いているが、止まっても誰も起こさない"
RUN_WAKE = "   持ち主の画面 (pro-con) を開くと見張りが戻る。この画面 (join) からは起こせない"
STOP_HEAD = " ■ カードは進まない — dispatcher が止まっている (最後の Tick 3分前)。持ち主の画面が無いので誰も起こさない"
STOP_WAKE = "   持ち主の画面 (pro-con) を開くと起きる。この画面 (join) からは起こせない"


def main():
    plan, state = sys.argv[1], sys.argv[2]
    here = os.path.dirname(os.path.abspath(__file__))
    lines = open(os.path.join(here, "board.ans"), encoding="utf-8").read().split("\n")
    while lines and lines[-1] == "":
        lines.pop()
    h = len(lines)
    title, tabs, gauge, rule, rest = lines[0], lines[1], lines[2], lines[3], lines[4:]
    title = title.replace("mock: claude は起動しない。表示は模擬データ", "join (読み書き・dispatcher を起こさない・quit で何も止めない) / mock")
    if state == "stop":
        gauge = gauge.replace("dispatcher 0秒前", "\x1b[31mdispatcher が動いていない (最後の Tick 3分前。起こすのは持ち主の画面か pro-con dispatcher)\x1b[39m")
        gauge = gauge.replace("PM 1/1 idle", "\x1b[2mPM 様子不明 (dispatcher が回っていない)\x1b[22m")
        back, front, head, wake = 196, 231, STOP_HEAD, STOP_WAKE
    else:
        back, front, head, wake = 214, 16, RUN_HEAD, RUN_WAKE
    if plan == "A":
        header = [title, tabs, gauge, band(head + "。" + wake.strip(), back, front), rule]
    elif plan == "B":
        header = [band(head + "。" + wake.strip(), back, front), tabs, gauge, rule]
    else:
        header = [title, tabs, gauge, band(head, back, front), band(wake, back, front), rule]
    sys.stdout.write("\n".join((header + rest)[:h]) + "\n")


main()
