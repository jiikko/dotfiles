"""本家 yomiyasu_diff.py の report(d) (人が読む出力。群 AD) に、diff() の dict を通して結果を JSON で返す driver。

標準入力: {"scripts_dir": "..."}
標準出力: {"cases": [{"name": ..., "d": report に渡した dict, "report": report(d) の結果}, ...]}
入力の dict は、本家の diff() が返した値 (実際の組) と、その値の節を書き換えた値 (diff() が作らない形の節も含む: 文面の分岐を 1 つずつ通すため)。
書き換えた値は、本家の diff() が返す欄だけを使う。report が例外を投げたら、この driver を落とす (入力の組み立ての誤り)。
"""
import copy
import json
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import yomiyasu_diff as D  # noqa: E402

CASES = []
# detector_summary の key (本家の docstring の並び)
SUMMARY_KEYS = ("stiffness", "tone", "hedges", "immutable", "politeness", "register")


def add(name, d):
    CASES.append({"name": name, "d": d, "report": D.report(d),
                  "fail_on_line": D._fail_on_line(d["meaning_risk_count"], d["fail_on_meaning_risk_count"], d["meaning_risks"]),
                  "parts": D.report_parts(d),
                  "summaries": {key: D.detector_summary(d, key) for key in SUMMARY_KEYS}})


def real(name, old, new, **kw):
    add("real/" + name, D.diff(old, new, **kw))


BASE = D.diff("元の文です。\n", "書き換えた文です。\n")


def variant(name, **sections):
    d = copy.deepcopy(BASE)
    d["status"] = "has_candidates"
    d["candidate_count"] = 1
    for key, value in sections.items():
        if isinstance(value, dict) and isinstance(d.get(key), dict) and key not in ("full_diff", "context"):
            d[key].update(value)
        else:
            d[key] = value
    add(name, d)
    return d


# ---- 実際の組 ----
real("nochange", "同じ文です。\n", "同じ文です。\n")
real("final-newline-only", "文です。", "文です。\n")
real("bom", "文です。\n", "文です。\n", encoding_notes=[{"kind": "bom", "old": False, "new": True}])
real("eol", "あ\nい\n", "あ\nい\n", encoding_notes=[{"kind": "eol", "old": "lf", "new": "crlf"}, {"kind": "eol", "old": "x", "new": "mixed"}])
real("replace", "設定を開きます。\n", "設定を開きました。\n")
real("insert", "前の文です。\n", "前の文です。\n足した文です。\n")
real("delete", "前の文です。\n消す文です。\n", "前の文です。\n")
real("insert-range", "前です。\n", "前です。\n\n- 項目1\n- 項目2\n")
real("whitespace", "文です。\n", "文です。 \n")
real("blank-lines", "あ\n\nい\n", "あ\n  \nい\n\u3000\n")
real("blank-line-delete", "あ\n\nい\n", "あ\nい\n")
real("heading-level", "# 見出し\n本文です。\n", "## 見出し\n本文です。\n")
real("moved", "A行です。\nB行です。\nC行です。\n", "B行です。\nC行です。\nA行です。\n")
real("blocks", "```\ncode\n```\n> 引用です。\n| a | b |\n|---|---|\n| 1 | 2 |\n", "```\ncode2\n```\n> 引用でした。\n| a | b |\n|---|---|\n| 1 | 3 |\n")
real("zero-width", "文です。\n", "文\u200bです。\u00ad\n")
real("long-line", "保存期間は3日です。" + "これは変わらない文です。" * 30 + "最後の文です。\n", "保存期間は5日です。" + "これは変わらない文です。" * 30 + "最後の文でした。\n")
real("long-line-nobreak", "あ" * 200 + "\n", "あ" * 100 + "い" + "あ" * 99 + "\n")
real("many-places", "あいうえおかきくけこ\n", "あXいYうZえWおVかきくけこ\n")
real("half-changed", "あいうえお\n", "かきくけこ\n")
real("doc-types", "必ず3日で保存できる場合があります。\n", "3日で保存できます。必ず保存します。\n", doc_type="pr", reader="informed")
real("typed-doc", "お世話になっております。\n3日かかります。\n", "お世話になります。\n5日かかります。\n", doc_type="mail")
real("material", "田中さんが対応する。\n", "営業担当が対応する。\n", materials=["田中さんは営業担当です。"])
real("stance", "設定を開く。保存する。\n", "設定を開きます。保存します。\n", stance="formal")
real("exclaim", "結果です。\n", "結果です！\n本当に素晴らしい！？\n", doc_type="pr")
real("intensifier", "結果です。\n", "非常に結果です。とても極めて絶対に確実です！\n", doc_type="article")
real("intensifier-none", "結果です。\n", "非常に結果です。とても極めて絶対に確実です！\n")
real("emoji", "結果です。\n", "結果です。\U0001F600\U0001F600\n", doc_type="slides")
real("heading-short", "## 詳しい手順についての長い見出しです\n本文です。\n", "## 手順\n本文です。\n", doc_type="slides")
real("stiffness", "確認してから決める。\n", "確認の上、決定する。実施の検討を行う。\n")
real("register", "設定を開きます。保存します。閉じます。\n", "設定を開く。保存する。閉じる。\n")
real("register-plain", "設定を開く。保存する。閉じる。\n", "設定を開きます。保存します。閉じます。\n")
real("unified", "設定を開きます。保存する。閉じます。保存します。\n", "設定を開きます。保存します。閉じます。保存します。\n")
real("hedge", "場合によっては失敗することがあるかもしれません。\n", "失敗します。\n")
real("immutable", "田中様\nmail: a@example.com\n", "山田様\nmail: b@example.com\n")
real("politeness", "ご確認いただければ幸いです。お願い申し上げます。\n", "確認してください。お願いします。\n", doc_type="mail")
real("comma", "彼は走った。それで勝った。\n", "彼は、走った。それで、勝った。\n")
real("heading-shape", "## 手順\n本文です。\n", "## 手順を詳しく説明します\n本文です。\n")
real("actors", "設定を保存した。\n", "管理者が設定を保存した。担当者に連絡する。\n")
real("numbers", "3日かかり、10人が参加します。\n", "5日かかり、20人が参加します。約30件です。\n")
real("state", "まだ決まっていません。\n", "決まりました。\n")
real("promise", "確認します。\n", "必ず再発防止に努めます。保証します。確実に対応します。\n")
real("modality", "ここは編集できます。保存してよい。\n", "ここは編集します。保存する。\n")
real("choice", "どれか一つを選ぶ。\n", "すべてを選ぶ。\n")
real("causality", "AとBには関連がある。\n", "AがBを引き起こす。\n")
real("quote", "「開きます」と言った。\n", "「閉じます」と言った。\n")
real("question", "動きますか？\n", "動きます。\n")
real("negation", "使えません。\n", "使えます。\n")
real("swap", "AがBを呼ぶ。\n", "BがAを呼ぶ。\n")
real("contrast", "Aではなく、Bです。\n", "Aです。Bではありません。\n")
real("feeling", "確認しました。\n", "確認できて嬉しいです。\n")
real("certainty", "成功するかもしれない。\n", "必ず成功する。\n")
real("certainty-weak", "必ず成功する。\n", "成功するかもしれない。\n")

# ---- 状態の行 ----
variant("status-extra-sections", hedges={"candidate_count": 2}, immutable={"candidate_count": 1}, register={"candidate_count": 1})
variant("status-extra-no-register", politeness={"candidate_count": 1})
variant("status-reviews", stiffness={"candidates": [{"level": "review", "subkind": "no_chain", "new_line": 1, "old_line": None, "detail": {"chains": ["A の B の C"]}}], "candidate_count": 1},
        tone={"candidates": [{"level": "info", "subkind": "emoji", "detail": {"old": 0, "new": 1, "places": []}}], "candidate_count": 1})
d = copy.deepcopy(BASE)
d["status"] = "changed_without_candidates"
d["full_diff"]["hunks"] = []
add("status-changed-no-hunks", d)


# ---- 意味が変わる恐れ ----
def risk(kind, subkind=None, detail=None, old_line=3, new_line=3, old="元の文", new="後の文", basis=None):
    r = {"kind": kind, "subkind": subkind, "old_line": old_line, "new_line": new_line, "old": old, "new": new, "detail": detail or {}}
    if basis:
        r["basis"] = basis
    return r


def risks(name, *items):
    counted = sum(1 for r in items if r["kind"] in D.FAIL_ON_KINDS or (r["kind"] == "unsupported" and r["subkind"] == "number"))
    variant(name, meaning_risks=list(items), meaning_risk_count=len(items), fail_on_meaning_risk_count=counted)


risks("risk-where", risk("negation", None, {"old": "ない", "new": "ある"}, 3, 3), risk("negation", None, {"old": "a", "new": "b"}, 3, 5),
      risk("negation", None, {"old": "a", "new": "b"}, 3, None), risk("negation", None, {"old": "a", "new": "b"}, None, 4),
      risk("negation", None, {"old": "a", "new": "b"}, None, None, old=None, new=None))
risks("risk-state", risk("state", None, {"words": ["未定", "検討中"]}))
risks("risk-promise", risk("promise", "added", {"phrases": ["必ず"]}), risk("promise", "guarantee", {"phrases": ["保証"]}),
      risk("promise", "prevention", {"phrases": ["再発防止", "徹底"]}, old_line=None))
risks("risk-promise-review", risk("promise_review", None, {"phrases": ["確認します"]}, old_line=None))
risks("risk-modality", risk("modality", "permission_lost", {"from": "許可", "to": "断定", "lost": [{"kind": "許可", "hits": ["てよい"]}], "added": []}),
      risk("modality", "permission_added", {"from": "断定", "to": "許可", "lost": [], "added": [{"kind": "許可", "hits": ["てもよい"]}]}),
      risk("modality", "ability_lost", {"from": "能力・不要", "to": "断定", "lost": [{"kind": "能力", "hits": ["できる", "られる"]}, {"kind": "能力（弱い印）", "hits": ["可能"]}, {"kind": "不要", "hits": ["不要"]}], "added": []}),
      risk("modality_review", "ability_lost", {"from": "義務・禁止・保証", "to": "能力", "lost": [{"kind": "義務", "hits": ["べき"]}, {"kind": "禁止", "hits": []}], "added": [{"kind": "能力", "hits": ["できる"]}]}),
      risk("modality", "ability_lost", {"from": "謎", "to": "謎2", "lost": [{"kind": "謎", "hits": ["x"]}], "added": []}))
risks("risk-choice", risk("choice", "one_to_all", {"from": "どれか", "to": "すべて", "old_hits": ["どれか"], "new_hits": ["すべて", "全部"]}),
      risk("choice", "all_to_one", {"from": "すべて", "to": "どれか", "old_hits": ["すべて"], "new_hits": ["どれか"]}))
risks("risk-causality", risk("causality", None, {"from": "関連", "to": "原因", "old_hits": ["関連"], "new_hits": ["引き起こす"]}))
risks("risk-qualifier", risk("number_qualifier", "lost", {"old_number": 3, "new_number": 3, "from": ["approx", "upper_in"], "to": [], "old_markers": ["約"], "new_markers": []}),
      risk("number_qualifier", "added", {"old_number": 3, "new_number": 3, "from": [], "to": ["lower_in", "bound_verb"], "old_markers": [], "new_markers": ["以上"]}),
      risk("number_qualifier", "changed", {"old_number": None, "new_number": 5, "from": ["lower_in"], "to": ["lower_ex"], "old_markers": ["以上"], "new_markers": ["超"]}),
      risk("number_qualifier", "changed", {"old_number": 3, "new_number": 5, "from": ["upper_ex"], "to": ["upper_in"], "old_markers": [], "new_markers": []}))
risks("risk-claim", risk("claim", None, {"words": ["ため"], "reason": "added"}), risk("claim", None, {"words": ["ので"], "reason": "joined"}),
      risk("claim", None, {"words": ["から"], "reason": "time_to_cause"}))
risks("risk-certainty", risk("certainty", "strengthened", {"steps": [{"from": "かもしれない", "to": "断定", "from_level": 1, "to_level": 4}, {"from": "でしょう", "to": "だ", "from_level": 2, "to_level": 5}]}),
      risk("certainty_review", "strengthened", {"steps": [{"from": "予定", "to": "確実", "from_level": 3, "to_level": 5}]}),
      risk("certainty_review", "weakened", {"steps": [{"from": "断定", "to": "かもしれない", "from_level": 4, "to_level": 1}]}))
risks("risk-quote", risk("quote", "lost", {"dialogue": True, "bracket": "「」", "old_quote": "開く", "new_quote": ""}, new_line=None),
      risk("quote", "changed", {"dialogue": False, "bracket": "『』", "old_quote": "開く", "new_quote": "閉じる"}),
      risk("quote", "changed", {"dialogue": True, "bracket": "「」", "old_quote": "a", "new_quote": "b"}, old_line=2, new_line=2))
risks("risk-number", risk("number", "lost", {"removed": ["3日", "5件"]}, new_line=None), risk("number", "lost", {"removed": ["3日"]}, old_line=None, new_line=4),
      risk("number", "added", {"added": ["5日"]}, old_line=None), risk("number", "added", {"added": ["5日"]}, old_line=2, new_line=None),
      risk("number", "order", {"old_order": ["1", "2"], "new_order": ["2", "1"]}), risk("number", "changed", {"removed": ["3"], "added": ["5"]}, basis="material"),
      risk("number", "changed", {}), risk("number", "lost", {}, basis="document"))
risks("risk-citation", risk("citation", None, {"removed": ["[1]"], "added": []}), risk("citation", None, {"removed": [], "added": ["[2]", "[3]"]}), risk("citation", None, {}))
risks("risk-swap", risk("swap", None, {"pairs": [{"word": "が", "old": "A", "new": "B"}, {"word": "を", "old": "C", "new": "D"}]}), risk("swap", None, {}))
risks("risk-sentence-type", risk("sentence_type", "question", {"old_is_question": True}), risk("sentence_type", "question", {"old_is_question": False}),
      risk("sentence_type", "marks", {}), risk("sentence_type", "marks", {}), risk("sentence_type", "other", {}), risk("sentence_type", None, {}))
risks("risk-protected", risk("protected", "code"), risk("protected", "inline_code"), risk("protected", "url"), risk("protected", "link"),
      risk("protected", "frontmatter"), risk("protected", "html"), risk("protected", "quote"), risk("protected", "unknown-thing"), risk("protected", None))
risks("risk-others", risk("contrast_reversal", None, {"word": "A"}), risk("feeling", None, {"words": ["嬉しい", "楽しい"]}), risk("negation", None, {"old": "", "new": "ない"}))
risks("risk-unsupported", risk("unsupported", "number", {"text": "42"}, old_line=None), risk("unsupported", "name", {"text": "田中"}, old_line=None, new_line=None),
      risk("unsupported", "name", {"text": "鈴木"}, old_line=1, new_line=None, new=None))
risks("risk-fallback", risk("mystery", "sub"), risk("mystery", None), risk("mystery", None, old_line=None, new_line=None, old=None, new=None))

# 数える種類と数えない種類 (promise_review など) を混ぜた件数の行 (終了コード 1 の行の「うち」の書き方)。種類の並びの順も確かめる
risks("risk-fail-on-all", risk("causality", None, {"from": "a", "to": "b", "old_hits": [], "new_hits": []}), risk("choice", "one_to_all", {"from": "a", "to": "b", "old_hits": [], "new_hits": []}),
      risk("modality", "permission_lost", {"from": "許可", "to": "断定", "lost": [], "added": []}), risk("promise", "added", {"phrases": ["x"]}), risk("state", None, {"words": ["x"]}),
      risk("certainty", "strengthened", {"steps": []}), risk("number", "lost", {"removed": ["1"]}), risk("promise_review", None, {"phrases": ["x"]}), risk("feeling", None, {"words": ["x"]}))
risks("risk-fail-on-two", risk("state", None, {"words": ["x"]}), risk("causality", None, {"from": "a", "to": "b", "old_hits": [], "new_hits": []}), risk("quote", "lost", {"dialogue": False, "bracket": "「」", "old_quote": "a", "new_quote": ""}))
risks("risk-fail-on-equal", risk("number", "lost", {"removed": ["1"]}), risk("negation", None, {"old": "a", "new": "b"}))

# ---- 規則の見直し候補 ----
variant("rules-all", markers=[{"kind": "推量", "orig": 0, "rewrite": 2, "orig_hits": [], "rewrite_hits": ["だろう", "かも"]}, {"kind": "伝聞", "orig": 1, "rewrite": 0, "orig_hits": ["そうだ"], "rewrite_hits": []}],
        new_words=["非常に", "必ず"], lost_words=["ほとんど"], structure=["見出しが減りました", "箇条書きが増えました"],
        logic=[{"kind": "因果", "sentence": "短い文です。"}, {"kind": "条件", "sentence": "これは四十四字を超える長い文で、短く切られるはずの文になっています。ここから先は出ません。"}],
        endings={"stance": None, "changes": [{"orig": "a", "orig_kind": "です・ます", "rewrite": "短い。", "rewrite_kind": "だ・である"}, {"orig": "a", "orig_kind": "だ", "rewrite": "あ" * 50, "rewrite_kind": "です"}],
                 "flags": [{"note": "文末が混ざっています", "sentences": ["短い。", "い" * 60]}], "orig_flags": []},
        type_changes=[{"orig": "元の文", "rewrite": "後の文", "lost": [{"kind": "伝聞", "hits": ["そうだ", "らしい"]}], "added": [{"kind": "推量", "hits": ["だろう"]}]},
                      {"orig": "あ" * 70, "rewrite": "い" * 70, "lost": [], "added": []}],
        bold=[{"line": 3, "found": "**太字**です", "suggest": "** 太字 **です", "how": "前後に空白"}, {"line": 5, "found": "**x**", "suggest": "", "how": "手で直す"}])
variant("rules-stance", endings={"stance": "formal", "changes": [], "flags": [{"note": "敬体と常体", "sentences": ["あ。"]}], "orig_flags": []})
variant("rules-minimal", new_words=["x"])
variant("rules-lost-only", lost_words=["y"])
variant("rules-structure-only", structure=["見出しの構造が変わりました"])
d = copy.deepcopy(BASE)
d["status"] = "no_change"
d["full_diff"]["hunks"] = []
d["logic"] = [{"kind": "因果", "sentence": "文"}]
d["endings"] = {"stance": None, "changes": [], "flags": [{"note": "n", "sentences": ["s"]}], "orig_flags": []}
d["bold"] = [{"line": 1, "found": "f", "suggest": "s", "how": "h"}]
d["markers"] = [{"kind": "推量", "orig": 0, "rewrite": 1, "orig_hits": [], "rewrite_hits": ["だろう"]}]
add("rules-not-rewritten-only", d)

# ---- 足した人・見出しの形・読点 ----
variant("actors", added_actors=[
    {"old_line": 1, "new_line": 1, "old": "保存した。", "new": "管理者が保存した。", "actors": [{"frame": "subject", "word": "管理者", "narrowed_from": "利用者"}]},
    {"old_line": None, "new_line": 2, "old": None, "new": "担当者に連絡する。", "actors": [{"frame": "recipient", "word": "担当者"}, {"frame": "other", "word": "誰か", "narrowed_from": None}]}])
variant("heading-shapes", heading_shapes=[{"old_line": 1, "new_line": 1, "old": "## 手順", "new": "## 手順を詳しく説明します"}, {"old_line": 4, "new_line": 6, "old": "# a", "new": "# b"}])
variant("comma", comma_increase={"old_count": 1, "new_count": 4, "old_sentences": 2, "new_sentences": 3, "sentences": [
    {"old_line": 1, "new_line": 1, "old_count": 0, "new_count": 2, "old": "走った。", "new": "彼は、走った、のです。"}, {"old_line": 3, "new_line": 4, "old_count": 1, "new_count": 2, "old": "a、b", "new": "a、b、c"}]})
variant("comma-zero", comma_increase={"old_count": 0, "new_count": 2, "old_sentences": 0, "new_sentences": 0, "sentences": []})

# ---- 硬化と勢い ----
STIFF = lambda **k: {"level": "review", "old_line": 1, "new_line": 1, "old": "元の文", "new": "後の文", **k}  # noqa: E731
variant("stiffness-lines", stiffness={"candidate_count": 8, "candidates": [
    STIFF(subkind="sentence", detail={"old_kanji_ratio": 30.4, "new_kanji_ratio": 50.5, "old_length": 20, "new_length": 18}),
    STIFF(subkind="sentence", old_line=None, detail={"old_kanji_ratio": 0.5, "new_kanji_ratio": 1.5, "old_length": 2, "new_length": 3}, new_line=4),
    STIFF(subkind="sentence_nominal", old_line=2, new_line=5, detail={"added_names": ["実施", "検討"], "old_kanji": 3, "new_kanji": 9}),
    STIFF(subkind="document", old=None, new=None, detail={"old_kanji_ratio": 30.0, "new_kanji_ratio": 45.0, "old_avg_length": 17.5, "new_avg_length": 22.5, "example": {"old_line": 1, "new_line": 1, "old": "a", "new": "b"}}),
    STIFF(subkind="document", detail={"old_kanji_ratio": 30.0, "new_kanji_ratio": 45.0, "old_avg_length": 17.5, "new_avg_length": 18.5}),
    STIFF(subkind="document", detail={"old_kanji_ratio": 30.0, "new_kanji_ratio": 45.0, "old_avg_length": 20.0, "new_avg_length": 15.0, "example": None}),
    STIFF(subkind="nominalization", detail={"added_names": ["実施"], "old": 1, "new": 5, "new_lines": [3, 4, 5, 6, 7]}),
    STIFF(subkind="no_chain", new_line=7, detail={"chains": ["A の B の C", "x"]}),
]})
variant("stiffness-nominal-empty", stiffness={"candidate_count": 2, "candidates": [
    STIFF(subkind="nominalization", detail={"added_names": [], "old": 0, "new": 1, "new_lines": []}), STIFF(subkind="weird", detail={})]})
variant("stiffness-info", context={"doc_type": "manual", "reader": None}, stiffness={"candidate_count": 2, "candidates": [
    STIFF(subkind="no_chain", level="info", detail={"chains": ["a の b の c"]}), STIFF(subkind="no_chain", level="info", detail={"chains": ["x"]})]})
variant("stiffness-mixed", context={"doc_type": "manual", "reader": None}, stiffness={"candidate_count": 2, "candidates": [
    STIFF(subkind="no_chain", level="info", detail={"chains": ["a"]}), STIFF(subkind="no_chain", detail={"chains": ["b"]})]})
variant("stiffness-note", stiffness={"list_problem": "読めません", "candidates": [], "candidate_count": 0})
variant("tone-note", tone={"candidates": [], "candidate_count": 0}, feeling={"list": "references/lists/feelings.txt", "list_problem": "x が壊れています"})
variant("tone-note-with-candidates", tone={"list_problem": "壊れている", "candidate_count": 1, "candidates": [
    {"level": "review", "subkind": "exclamation", "detail": {"old": 0, "new": 2, "places": [{"old_line": None, "new_line": 2, "old": None, "new": "あ！"}]}}]},
        feeling={"list_problem": "f", "list": "x"}, stiffness={"candidates": [], "candidate_count": 0, "list_problem": "s"})


def place(old_line, new_line, old, new):
    return {"old_line": old_line, "new_line": new_line, "old": old, "new": new}


TONE = lambda sub, level="review", **d: {"level": level, "subkind": sub, "old_line": None, "new_line": None, "detail": d}  # noqa: E731
variant("tone-lines", tone={"candidate_count": 7, "candidates": [
    TONE("exclamation", old=0, new=3, places=[place(None, 2, None, "あ！")] + [place(3 + i, 3 + i, "い", "う！") for i in range(6)]),
    TONE("question", old=1, new=2, places=[place(2, 2, "え", "え？"), place(3, 3, "お", "お？")]),
    TONE("emoji", old=0, new=1, places=[place(4, None, "絵", None)]),
    TONE("intensifier", old=0, new=2, words=["非常に", "極めて"], places=[place(5, 6, "a", "b")]),
    TONE("intensifier", old=0, new=2, places=[]),
    TONE("heading", old=20, new=5, places=[place(1, 1, "## 長い見出し", "## 短い")]),
    TONE("weird", old=1, new=1),
]})
for doc_type in ("pr", "slides", "promo", "minutes", None, "mail"):
    variant("tone-info-" + str(doc_type), context={"doc_type": doc_type, "reader": None}, tone={"candidate_count": 4, "candidates": [
        TONE("exclamation", "info", old=0, new=1, places=[]), TONE("emoji", "info", old=0, new=1, places=[]),
        TONE("heading", "info", old=9, new=2, places=[]), TONE("question", "review", old=0, new=1, places=[])]})
    variant("tone-info-one-" + str(doc_type), context={"doc_type": doc_type, "reader": None}, tone={"candidate_count": 1, "candidates": [TONE("emoji", "info", old=0, new=1, places=[])]})
    variant("tone-info-heading-" + str(doc_type), context={"doc_type": doc_type, "reader": None}, tone={"candidate_count": 1, "candidates": [TONE("heading", "info", old=9, new=2, places=[])]})
    variant("tone-info-excl-" + str(doc_type), context={"doc_type": doc_type, "reader": None}, tone={"candidate_count": 1, "candidates": [TONE("intensifier", "info", old=0, new=2, words=["a"], places=[])]})
variant("tone-info-weird", context={"doc_type": "pr", "reader": None}, tone={"candidate_count": 1, "candidates": [TONE("weird", "info", old=1, new=1, places=[])]})
variant("tone-info-only", context={"doc_type": "pr", "reader": None}, tone={"candidate_count": 2, "candidates": [TONE("exclamation", "info", old=0, new=1, places=[]), TONE("emoji", "info", old=0, new=1, places=[])]})

# ---- 慎重表現・値・敬語・文体の切り替え ----
variant("hedges", hedges={"candidate_count": 1, "candidates": [{"old_line": 1, "new_line": 1, "old": "かもしれない", "new": "だ", "subkind": "x", "detail": {"old_count": 2, "new_count": 0, "removed": ["かもしれない", "ことがある"]}}]})
variant("hedges-note", hedges={"candidate_count": 0, "candidates": [], "list_problem": "読めない"})
variant("immutable", immutable={"candidate_count": 2, "meaning_risk_count": 0, "candidates": [
    {"old_line": 1, "new_line": 1, "old": "田中様", "new": "山田様", "subkind": "addressee", "detail": {"removed": ["田中様"], "added": ["山田様"]}},
    {"old_line": 2, "new_line": None, "old": "a@b.c", "new": None, "subkind": "email", "detail": {"removed": [], "added": ["x@y.z"]}},
    {"old_line": None, "new_line": 3, "old": None, "new": "p@q.r", "subkind": "email", "detail": {"removed": ["p@q.r"], "added": []}}]})
variant("politeness", politeness={"candidate_count": 2, "candidates": [
    {"old_line": None, "new_line": None, "old": None, "new": None, "subkind": "honorific_rate", "detail": {"old_ratio": 0.5, "new_ratio": 0.125}},
    {"old_line": 1, "new_line": 2, "old": "です", "new": "だ", "subkind": "desu_masu_ratio", "detail": {"old_ratio": 0.9, "new_ratio": 0.3333}}]})
REG = lambda sub, pattern, **d: {"old_line": 1, "new_line": 1, "old": "元", "new": "後", "subkind": sub, "detail": {"pattern": pattern, "switched": 2, "old_counts": {"plain": 1, "polite": 4}, "new_counts": {"plain": 3, "polite": 2}, "lines": [[1, 1], [2, 3], [4, 4]], **d}}  # noqa: E731
variant("register", register={"candidate_count": 4, "candidates": [REG("to_polite", "unified"), REG("to_plain", "unified"), REG("to_polite", "switch"), REG("to_plain", "switch", lines=[[1, 1]])]})
variant("register-two-lines", register={"candidate_count": 1, "candidates": [REG("to_plain", "switch", lines=[[1, 1], [2, 3]])]})
variant("register-label", register={"candidate_count": 1, "candidates": [REG("to_plain", "switch")]}, hedges={"candidate_count": 1, "candidates": []})

# ---- 文脈の行 ----
for doc_type, reader in (("pr", None), ("slides", "informed"), (None, "expert"), ("proposal", "informed"), ("mail", None)):
    d = copy.deepcopy(BASE)
    d["context"] = {"doc_type": doc_type, "reader": reader}
    add("context-%s-%s" % (doc_type, reader), d)
for words in ({"path": "/a/b", "allow_lines": 3, "flag_lines": 0, "allow_sha256": None, "flag_sha256": None},
              {"path": "x", "allow_lines": 1, "flag_lines": 2, "allow_sha256": "a", "flag_sha256": "b", "flag_patterns_lines": 4, "flag_patterns_sha256": "c"}):
    d = copy.deepcopy(BASE)
    d["context"] = {"doc_type": "pr", "reader": None, "words": words}
    add("context-words-%d" % len(words), d)
d = copy.deepcopy(BASE)
d["context"] = {"doc_type": None, "reader": None, "words": {"path": "p", "allow_lines": 0, "flag_lines": 0}}
add("context-words-only", d)

# ---- 全文差分の組み立て (dict を直接) ----
HUNK = {"index": 1, "tag": "replace", "old_start": 1, "old_end": 1, "new_start": 1, "new_end": 1, "old_blocks": ["paragraph"], "new_blocks": ["paragraph"], "whitespace_only": False,
        "notes": [], "moved_to": None, "moved_from": None, "old_lines": ["元"], "new_lines": ["後"], "segments": [["-", "元"], ["+", "後"]], "segments_omitted": None}


def hunk(**k):
    return {**copy.deepcopy(HUNK), **k}


def fulldiff(name, hunks, notes=None, final=(True, True)):
    d = copy.deepcopy(BASE)
    d["full_diff"] = {"encoding_notes": notes or [], "final_newline": {"old": final[0], "new": final[1]}, "limits_applied": [], "hunks": hunks}
    add(name, d)


fulldiff("fd-kinds", [hunk(old_blocks=["frontmatter", "html_block", "reference_definition", "weird", "heading_underline", "heading"], new_blocks=["thematic_break", "blank", "table", "code", "list", "quote"]),
                      hunk(old_blocks=["paragraph"], new_blocks=["paragraph"], tag="insert", new_start=2, new_end=4, old_lines=[], new_lines=["a", "", "  ", "\u3000", "\t", "\ufeff"], segments=None),
                      hunk(tag="delete", old_start=5, old_end=5, new_blocks=[], old_lines=["", " ", "x"], new_lines=[], segments=None)])
fulldiff("fd-notes", [hunk(notes=[{"kind": "whitespace_only"}, {"kind": "heading_level", "old": 1, "new": 2}, {"kind": "moved_to", "hunk": 2, "line": 5}, {"kind": "moved_from", "hunk": 1, "line": 3}, {"kind": "other"}])],
         notes=[{"kind": "eol", "old": "lf", "new": "cr"}, {"kind": "eol", "old": "crlf", "new": "?"}, {"kind": "bom", "old": True, "new": False}, {"kind": "other", "old": 1, "new": 2}], final=(True, False))
fulldiff("fd-newline", [], final=(False, True))
fulldiff("fd-empty", [])
fulldiff("fd-ws-segments", [hunk(whitespace_only=True, segments=[["=", "a"], ["-", " "], ["+", "\u3000"], ["=", "b\nc"], ["+", "\n"], ["-", "\u200b\u00ad\u00a0"]])])
fulldiff("fd-ws-nosegments", [hunk(whitespace_only=True, segments=None, old_lines=["a "], new_lines=["a"])])
fulldiff("fd-nosegments", [hunk(segments=None, segments_omitted="too_large")])
fulldiff("fd-multiline", [hunk(old_lines=["a", "b"], new_lines=["c"], segments=[["-", "a\nb"], ["+", "c"]])])
fulldiff("fd-diffline", [hunk(old_lines=["あいうえおかきくけこ" * 2], new_lines=["あいうえXかきくけこ" * 2], segments=[["=", "あいうえ"], ["-", "お"], ["+", "X"], ["=", "かきくけこ" + "あいうえおかきくけこ"]])])
fulldiff("fd-diffline-five", [hunk(old_lines=["abcdefghijklmnopqrstuvwxyz0123456789"], new_lines=["aXcXeXgXiXklmnopqrstuvwxyz0123456789"],
                                   segments=[["=", "a"], ["-", "b"], ["+", "X"], ["=", "c"], ["-", "d"], ["+", "X"], ["=", "e"], ["-", "f"], ["+", "X"], ["=", "g"], ["-", "h"], ["+", "X"], ["=", "i"], ["-", "j"], ["+", "X"], ["=", "klmnopqrstuvwxyz0123456789"]])])
fulldiff("fd-diffline-ins-only", [hunk(old_lines=["あいう"], new_lines=["あXいう"], segments=[["=", "あ"], ["+", "X"], ["=", "いう"]]), hunk(tag="delete", old_lines=["x"], new_lines=["y"], segments=[["-", "x"], ["+", "y"]])])
LONGS = "変わらない前の文です。" * 20
fulldiff("fd-short-diff", [hunk(old_lines=[LONGS + "保存期間は3日です。" + LONGS + "終わり"], new_lines=[LONGS + "保存期間は5日です。" + LONGS + "終わり"],
                               segments=[["=", LONGS + "保存期間は"], ["-", "3"], ["+", "5"], ["=", "日です。" + LONGS + "終わり"]])])
SENT80 = "あ。" * 40
for tag, (o, n) in {"160-160": (160, 160), "161-160": (161, 160), "160-161": (160, 161), "161-161": (161, 161)}.items():
    fulldiff("fd-line-" + tag, [hunk(old_lines=[SENT80 + "x" * (o - 80 - 1) + "A"], new_lines=[SENT80 + "x" * (n - 80 - 1) + "B"],
                                     segments=[["=", SENT80], ["=", "x" * (min(o, n) - 81)], ["-", "A" + "x" * (o - min(o, n))], ["+", "B" + "x" * (n - min(o, n))]])])
fulldiff("fd-short-diff-edges", [hunk(old_lines=["あ" * 170], new_lines=["あ" * 169 + "い"], segments=[["-", "あ"], ["+", "い"], ["=", "。！？" + "あ" * 169 + "」』）) "]])])
fulldiff("fd-short-diff-nosentence", [hunk(old_lines=["あ" * 170], new_lines=["い" + "あ" * 169], segments=[["+", "い"], ["=", "あ" * 169], ["-", "あ"]])])
fulldiff("fd-short-diff-allchanged-sentence", [hunk(old_lines=["あ。" * 90 + "x"], new_lines=["あ。" * 90 + "y"], segments=[["-", "x"], ["+", "y"], ["=", "。" * 5], ["-", "z"]])])
fulldiff("fd-short-diff-leading-equal", [hunk(old_lines=["あ。" * 90 + "x"], new_lines=["あ。" * 90 + "y"], segments=[["=", "あ。い。"], ["-", "x"], ["=", "。！"], ["+", "y"], ["=", "ううう。"]])])
fulldiff("fd-short-diff-no-omit", [hunk(old_lines=["あ" * 170], new_lines=["い" * 170], segments=[["-", "あ" * 3], ["+", "い" * 3], ["-", "あ"]])])
fulldiff("fd-short-diff-full", [hunk(old_lines=["あ。" * 90], new_lines=["あ。" * 90 + "い"], segments=[["+", "い"]])])

# ---- 全文差分が 0 の組の候補の節・行の途中の BOM の名前・unsupported の行の書き分け ----
MIXED = "これは**「重要」**です。\nまた、そのため結果が出ました。だから次も試します。\n"
real("eol-only-with-candidates", MIXED, MIXED.replace("\n", "\r\n"))
real("bom-only-with-candidates", MIXED, "\ufeff" + MIXED)
real("final-newline-only-with-candidates", MIXED.rstrip("\n"), MIXED)
real("midline-bom", "設定を変えます。\n", "設定を\ufeff変えます。\n")
risks("risk-unsupported-both-lines", risk("unsupported", "number", {"text": "42"}, old_line=1, new_line=3))

# ---- detector_summary の分岐 (節が無い・当てはまらない・参考だけ・混ざる・candidates が並びでない・値の恐れ) ----
def summary(name, key, section):
    d = copy.deepcopy(BASE)
    if section is None:
        d.pop(key, None)
    else:
        d[key] = section
    # 候補の中身は summary が読む欄 (level) だけなので report には通さない
    CASES.append({"name": "summary/" + name, "d": d, "summary_only": True,
                  "summaries": {k: D.detector_summary(d, k) for k in SUMMARY_KEYS}})


summary("absent", "hedges", None)
summary("not-applicable", "politeness", {"applicable": False, "candidate_count": 3, "candidates": []})
summary("applicable-none", "politeness", {"applicable": None, "candidate_count": 0, "candidates": []})
summary("zero", "register", {"candidate_count": 0, "candidates": []})
summary("no-list-info", "stiffness", {"candidate_count": 2, "level": "info"})
summary("no-list-warn", "stiffness", {"candidate_count": 2, "level": "warn"})
summary("all-info", "hedges", {"candidate_count": 2, "candidates": [{"level": "info"}, {"level": "info"}]})
summary("more-info-than-count", "hedges", {"candidate_count": 1, "candidates": [{"level": "info"}, {"level": "info"}]})
summary("mixed", "hedges", {"candidate_count": 3, "candidates": [{"level": "info"}, {"level": "warn"}, "x"]})
summary("none-info", "tone", {"candidate_count": 2, "candidates": [{"level": "warn"}, {}]})
summary("immutable-risky-only", "immutable", {"candidate_count": 0, "meaning_risk_count": 2, "candidates": []})
summary("immutable-risky-and-count", "immutable", {"candidate_count": 3, "meaning_risk_count": 1, "candidates": []})
summary("immutable-risky-and-one", "immutable", {"candidate_count": 1, "meaning_risk_count": 1, "candidates": []})
summary("risky-ignored-not-immutable", "hedges", {"candidate_count": 1, "meaning_risk_count": 4, "candidates": [{"level": "warn"}]})

json.dump({"cases": CASES}, sys.stdout, ensure_ascii=True, separators=(",", ":"))
