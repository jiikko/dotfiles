#!/usr/bin/env python3
"""? のヘルプの「流れ」タブの見本 (issue 515)。使い捨て。本体 (src/pro-con) には入れていない。

使い方: python3 sample.py <案 A|B|C> [--width N]   (N は画面の幅。既定 120 → 板は 76 桁)
  A = 矢印の図 (本流を縦に、寄り道を右へ枝分かれ)
  B = 文 (移り変わりを 1 つずつ見出しにし、下に誰が・何で)
  C = 表 (移り変わり / 誰が / 何で の 3 列)

見本が再現していないもの (本体との差):
  - 板の影・画面の中央への重ね合わせ (カンバンは描かない)
  - 文面は下の FLOW の写し。本体では card の側に正本を置き、pro-con help flow も同じ文を出す
"""
import sys
import unicodedata

RESET, BOLD, DIM, REV = "\x1b[0m", "\x1b[1m", "\x1b[2m", "\x1b[7m"
YEL = "\x1b[38;5;214m"
LABELS = ["依頼", "分解済み", "作業中", "質問待ち", "レビュー", "完了"]
COLOR = [51, 250, 46, 214, 208, 240]  # ui/style.go の stateColor
REQ, PLAN, RUN, WAIT, REVIEW, DONE = range(6)


def fg(n): return "\x1b[38;5;%dm" % n


def lane(s):
    if s is None:
        return BOLD + "人・外の Claude" + RESET
    if isinstance(s, str):
        return BOLD + s + RESET
    return fg(COLOR[s]) + BOLD + "%d %s" % (s + 1, LABELS[s]) + RESET


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
def pad(s, w): return s + " " * max(w - width(s), 0)


def wrap(text, w):
    """素の文字列 text を表示幅 w で折り返す (語の境目は見ない。本体の ansi.Hardwrap と同じ)。"""
    lines, cur = [], ""
    for ch in text:
        if width(cur) + cw(ch) > w:
            lines.append(cur); cur = ""
        cur += ch
    return lines + [cur] if cur else lines


# 流れの正本の写し: (元, 先, 誰が, 何で)
MAIN = [
    (None, REQ, "人・外の Claude", "画面の n (issue からなら i)・card add"),
    (REQ, PLAN, "PM", "issue に分け、順番と見積もりを付けて積む (card plan)"),
    (PLAN, RUN, "dispatcher", "空いた PG を上から起こす (↻ の再開が先。--after の相手が完了するまで待つ)"),
    (RUN, REVIEW, "PG", "終えて出す (card review)"),
    (REVIEW, DONE, "取り込みの係", "diff とテストを確かめ、master へ push して閉じる (card close)"),
]
DETOURS = [
    (RUN, WAIT, "PG", "質問して turn を終える (card ask)。まず PM が受け、答えるか人に回す"),
    (RUN, WAIT, "dispatcher", "権限の確認で止まった・落ち続けた PG を止めた"),
    (WAIT, PLAN, "PM・人", "答える (r・card answer)。↻ が付き、空いた PG で同じ session を再開"),
    (WAIT, RUN, "人", "権限の確認は a で attach して答えると、そのまま続く"),
    (REQ, WAIT, "PM", "依頼について人に聞く (card ask)"),
    (WAIT, REQ, "人", "答える (r)。PM が回答を読んで分ける"),
    (REVIEW, PLAN, "取り込みの係", "差し戻す (card rework。衝突・テストの失敗)。↻ で同じ PG が直す"),
    (REQ, DONE, "PM", "その場で答えて閉じる・却下する (card close --ending)"),
    ("どこからでも", "削除", "人", "d・card delete (依頼の列はすぐ。ほかは PG を止めてから)"),
]
HUMAN = "PM か取り込みの係が人に回したとき (card handoff)・権限の確認・落ち続けて止めた PG・起こさない設定の役の仕事"
AFTER = [
    "24 時間 (か x) で書庫へ移り、完了から 1 週間で記録から消える",
    "worktree・ブランチ・session は残る。消すのは人が pro-con worktree clean --yes を打ったときだけ",
]
TABS = ["流れ", "レーン", "印とポイント", "役"]


def tail(inner):
    """人の番と完了の後 (どの案も同じ)。"""
    rows = ["", YEL + BOLD + "!人の番" + RESET + " が付くのは"]
    rows += ["  " + l for l in wrap(HUMAN, inner - 2)]
    rows += ["", BOLD + "完了の後" + RESET]
    for a in AFTER:
        rows += ["  " + l for l in wrap(a, inner - 2)]
    return rows


def plan_a(inner):
    """A: 本流を縦の矢印で、各レーンの寄り道を右に枝分かれで。"""
    rows = [BOLD + "通常の流れ" + RESET + DIM + "  (縦が本流・右が寄り道)" + RESET, ""]
    col = 4  # 縦の矢印の柱の位置
    branches = {}
    for d in DETOURS:
        if isinstance(d[0], int) and d[0] in (REQ, RUN, REVIEW):
            branches.setdefault(d[0], []).append(d)
    for i, (src, dst, who, how) in enumerate(MAIN):
        if i == 0:
            rows.append(lane(None))
            rows += [" " * col + DIM + "│ " + l + RESET for l in wrap(how, inner - col - 2)]
        # 矢印の区間: │ 誰が 何で
        for j, l in enumerate(wrap(who + ": " + how if i else "", inner - col - 3)):
            rows.append(" " * col + DIM + "│ " + RESET + (BOLD + l[:len(who)] + RESET + l[len(who):] if j == 0 else l))
        rows.append(" " * col + DIM + "▼" + RESET)
        head = lane(dst)
        bs = branches.get(dst, [])
        if not bs:
            rows.append(head)
            continue
        hw = 12
        for k, (_, to, bwho, bhow) in enumerate(bs):
            glyph = "─┬─▶ " if k == 0 and len(bs) > 1 else ("─── ▶ " if k == 0 else "")
            if k == 0:
                prefix = pad(head, hw) + DIM + ("──┬▶ " if len(bs) > 1 else "───▶ ") + RESET
            else:
                prefix = " " * col + DIM + "│" + RESET + " " * (hw - col - 1) + DIM + ("  ├▶ " if k < len(bs) - 1 else "  └▶ ") + RESET
            text_w = inner - hw - 5
            first = lane(to) + ("  " if width(lane(to) + bwho) + 2 <= text_w else "") + BOLD + (bwho if width(lane(to) + bwho) + 2 <= text_w else "") + RESET
            if not strip(first).endswith(bwho):
                bhow = bwho + ": " + bhow
            body = wrap(bhow, text_w)
            # 先の名前と誰がを 1 行目に、何でを次の行から
            rows.append(prefix + first)
            cont = " " * col + DIM + "│" + RESET + " " * (hw - col - 1) + DIM + ("  │  " if k < len(bs) - 1 else "     ") + RESET
            rows += [cont + l for l in body]
    rows += ["", BOLD + "ほかの寄り道" + RESET]
    for src, dst, who, how in DETOURS:
        if isinstance(src, int) and src in (REQ, RUN, REVIEW):
            continue
        rows.append(lane(src) + DIM + " → " + RESET + lane(dst) + "  " + BOLD + who + RESET)
        rows += ["    " + l for l in wrap(how, inner - 4)]
    return rows + tail(inner)


def plan_b(inner):
    """B: 移り変わりを 1 つずつ見出しにし、下に 誰が: 何で。"""
    rows = []
    for title, items in (("通常の流れ", MAIN), ("寄り道", DETOURS)):
        rows += [BOLD + title + RESET]
        for src, dst, who, how in items:
            rows.append(lane(src) + DIM + " → " + RESET + lane(dst))
            text = who + ": " + how
            for j, l in enumerate(wrap(text, inner - 4)):
                rows.append("    " + (BOLD + l[:len(who)] + RESET + l[len(who):] if j == 0 else l))
        rows.append("")
    return rows[:-1] + tail(inner)


def plan_c(inner):
    """C: 表 (移り変わり / 誰が / 何で)。何での列だけ折り返す。狭い画面では B に落とす。"""
    c1, c2 = 24, 16
    if inner < 60:  # 狭い画面では表を諦め、B の形に落とす
        return plan_b(inner)
    rows = []
    for title, items in (("通常の流れ", MAIN), ("寄り道", DETOURS)):
        rows += [BOLD + title + RESET, DIM + pad("移り変わり", c1) + pad("誰が", c2) + "何で" + RESET]
        for src, dst, who, how in items:
            body = wrap(how, inner - c1 - c2)
            rows.append(pad(lane(src) + DIM + "→" + RESET + lane(dst), c1) + pad(BOLD + who + RESET, c2) + body[0])
            rows += [" " * (c1 + c2) + l for l in body[1:]]
        rows.append("")
    return rows[:-1] + tail(inner)


def panel(title, rows, w):
    inner = w - 4
    out = [DIM + "╭─" + RESET + title + DIM + "─" * max(w - 3 - width(title), 0) + "╮" + RESET]
    for r in rows:
        out.append(DIM + "│ " + RESET + pad(r, inner) + DIM + " │" + RESET)
    out.append(DIM + "╰" + "─" * (w - 2) + "╯" + RESET)
    return out


def main():
    args = sys.argv[1:]
    total = 120
    if "--width" in args:
        i = args.index("--width"); total = int(args[i + 1]); del args[i:i + 2]
    which = (args or ["A"])[0].upper()
    w = min(total - 4, 76)
    inner = w - 4
    tabs = "  ".join((REV + BOLD if t == "流れ" else DIM) + " " + t + " " + RESET for t in TABS)
    hint = DIM + "   tab で切り替え" + RESET
    head = [tabs + (hint if width(tabs + hint) <= inner else ""), ""]
    body = {"A": plan_a, "B": plan_b, "C": plan_c}[which](inner)
    for l in panel(" カードの流れと意味 (案 %s) " % which, head + body, w):
        print(l)


main()
