package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 X (慎重表現・動かしてはいけない値・敬語) と群 W (かぎかっこ・文体の切り替え) の記録: 段 _hedges・_immutable_values・_politeness・_quote_risks・_register_changes と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/30〜34) の組を足したもの。

// DiffReviewFunctions は群 X・W の関数 (_value_places・_Masker・_Body は覚えるだけの呼び出し)。
var DiffReviewFunctions = []string{
	"_hedges", "_immutable_values", "_value_tokens", "_value_places", "_uncancelled_tokens", "_citation_number_covered", "_citation_key",
	"_politeness_metrics", "_politeness", "_quote_risks", "_line_quotes", "_quote_normalize", "_blank_masked", "_register_of", "_contained", "_neighbor_pairs", "_register_changes",
	"_Masker", "_Body",
}

// diffReviewConstants は群 X・W の Go の表と比べる本家の定数。_CITATION_KEY は int の鍵を持つ辞書で、JSON の鍵の並びが Go と合わないので、_citation_key の呼び出しで確かめる。
var diffReviewConstants = []string{
	"_CITATION", "_SCIENTIFIC_VALUE", "_SIGNED_VALUE", "_STATISTIC_VALUE", "_EMAIL", "_ADDRESSEE", "_ADDRESSEE_FAMILY", "_VALUE_SKIP_KINDS",
	"POLITENESS_THRESHOLD", "_MAIL_HONORIFIC", "_MAIL_HONORIFIC_NAMES", "_POLITE_END", "_PLAIN_END", "_DIALOGUE", "_QUOTE_CLOSE", "_QUOTE_DROP", "_CITATION_LATIN", "_CITATION_JA",
}

// DiffReviewConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffReviewConstantNames() []string { return diffReviewConstants }

// DiffReviewValueKinds は動かしてはいけない値の種類を、本家の検査の順に並べたもの (組は種類の名前と正規表現の定数の名前)。
var DiffReviewValueKinds = [][2]string{
	{"citation", "_CITATION"}, {"addressee", "_ADDRESSEE"}, {"email", "_EMAIL"}, {"scientific_unit", "_SCIENTIFIC_VALUE"}, {"statistic", "_STATISTIC_VALUE"}, {"number_sign", "_SIGNED_VALUE"},
}

type diffReviewParts int

const (
	reviewHedges diffReviewParts = 1 << iota
	reviewImmutable
	reviewPoliteness
	reviewQuote
	reviewRegister
	reviewAll = reviewHedges | reviewImmutable | reviewPoliteness | reviewQuote | reviewRegister
)

// diffReviewDocTypes は _politeness と _register_changes に渡す文書の種類 (nil は指定なし。mail だけ敬語を比べる)。
var diffReviewDocTypes = []any{nil, "mail"}

// diffReviewStageCalls は組 1 つを、本家の diff() と同じ順で作らせて (覚える)、parts の段に通す呼び出し。
func diffReviewStageCalls(old, new string, parts diffReviewParts, keep bool) []Call {
	save := diffSave
	sides, groups := diffFullRef(1), diffRef("groups")
	ops := dcall("_ExactSequenceMatcher", nil, diffRef("o"), diffRef("r"))
	ops.Kwargs = map[string]any{"autojunk": false}
	ops.Method = "get_opcodes"
	calls := []Call{
		save(dcall("_build_full_diff", old, new, nil), "full"),
		save(dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0)), "groups"),
	}
	if parts&reviewHedges != 0 {
		calls = append(calls,
			save(dcall("normalize", old), "o"), save(dcall("normalize", new), "r"), save(ops, "ops"),
			save(dcall("_type_changes", diffRef("o"), diffRef("r"), diffRef("ops")), "tc"),
			save(dcall("_marks_risks", sides, diffRef("tc")), "marks"),
			save(dcall("_certainty_risks", sides, groups, diffRef("marks"), nil, nil), "cert"),
			save(dcall("_certainty_risks", sides, groups, diffRef("marks"), nil, "article"), "certA"),
		)
		for _, risks := range []any{
			map[string]any{"$concat": []any{diffRef("marks"), diffRef("cert")}}, map[string]any{"$concat": []any{diffRef("marks"), diffRef("certA")}},
			diffRef("marks"), diffRef("cert"),
		} {
			calls = append(calls, withKeep(keep, dcall("_hedges", sides, groups, risks)))
		}
	}
	if parts&reviewImmutable != 0 {
		calls = append(calls,
			save(dcall("_number_risks", sides, diffFullRef(0), groups), "nrisks"),
			save(dcall("_value_places", sides, groups), "places"),
			withKeep(keep, dcall("_immutable_values", sides, map[string]any{"$column": diffRef("nrisks"), "n": 2}, groups)))
		for _, k := range DiffReviewValueKinds {
			re := Call{Module: DiffModule, Function: k[1], Get: true, Digest: true, Quiet: true, Save: "re_" + k[0]}
			calls = append(calls, re)
			for side := range 2 {
				c := dcall("_value_tokens", sides, side, diffRef("re_"+k[0]))
				c.Save = "tok_" + k[0] + "_" + strconv.Itoa(side)
				calls = append(calls, withKeep(keep, c))
			}
			calls = append(calls, withKeep(keep, dcall("_uncancelled_tokens", diffRef("tok_"+k[0]+"_0"), diffRef("tok_"+k[0]+"_1"), diffRef("places"), groups)))
		}
	}
	if parts&(reviewPoliteness|reviewRegister) != 0 {
		calls = append(calls, save(dcall("_all_bodies", sides, 0), "b0"), save(dcall("_all_bodies", sides, 1), "b1"),
			withKeep(keep, dcall("_politeness_metrics", diffRef("b0"))), withKeep(keep, dcall("_politeness_metrics", diffRef("b1"))))
		for _, dt := range diffReviewDocTypes {
			name := "polNone"
			if dt != nil {
				name = "pol" + strings.ToUpper(dt.(string)[:1]) + dt.(string)[1:]
			}
			c := dcall("_politeness", sides, dt)
			c.Save = name
			calls = append(calls, withKeep(keep, c))
			if parts&reviewRegister != 0 {
				calls = append(calls, withKeep(keep, dcall("_register_changes", sides, groups, diffRef(name))))
			}
		}
	}
	if parts&reviewQuote != 0 {
		calls = append(calls, withKeep(keep, dcall("_quote_risks", sides, groups)))
		for side, text := range [2]string{old, new} {
			m := dcall("_Masker", sides, side)
			m.Save, m.Quiet = "mask"+strconv.Itoa(side), true
			calls = append(calls, m)
			start := 0
			for n, line := range strings.Split(text, "\n") {
				if n < 8 && (strings.Contains(line, "`") || strings.Contains(line, "http") || strings.Contains(line, "「")) {
					calls = append(calls, dcall("_blank_masked", diffRef("mask"+strconv.Itoa(side)), line, start))
				}
				start += len([]rune(line)) + 1
			}
		}
	}
	return calls
}

// diffReviewHedgeBank は慎重な言い方の語 (と考えられる・と思われる・可能性がある・示唆する・推測される・の範囲では・かもしれない) を持つ文と、近くて当たらない文。
var diffReviewHedgeBank = []string{
	"遅れると考えられる。", "遅れると思われる。", "遅れる可能性がある。", "効果を示唆する。", "原因が推測される。", "この範囲では動く。", "遅れるかもしれない。",
	"遅れると考えられ、遅れる可能性がある。", "遅れるかもしれないし、止まると思われる。", "遅れるかもしれず、遅れるかもしれない。", "考えられる。", "可能性が高い。", "示唆した。", "推測する。", "範囲を広げる。",
	"遅れると考えられるが、確認する。", "確認すると考えられます。", "設定を開きます。", "結果は3件でした。", "確認します。", "必ず遅れる。", "遅れる見込みです。", "遅れると思います。", "遅れるでしょう。",
}

// diffReviewValueBank は動かしてはいけない値を持つ文 (引用・宛名・メール・科学の単位・統計・符号)。{v} を値に置き換える。
var diffReviewValueBank = []string{
	"{v}を参照する。", "詳しくは{v}を見る。", "{v}", "宛先: {v}", "{v}、お疲れさまです。", "{v}に連絡します。",
}

var diffReviewValues = []string{
	"[1]", "[1, 2]", "[1,2]", "[1、2]", "[1-3]", "[1–3]", "[1—3]", "[12]", "[1][2]", "[1](https://a.example/)", "[1] (注)", "[a]", "[]",
	"(田中ほか, 2019)", "（田中ほか，2019）", "(田中ら 2019)", "(田中, 2019a)", "(山田, 2020)", "（山田、2020）", "(Smith, 2019)", "(Smith et al. 2019)", "(Smith et al., 2019)", "(Smith & Jones, 2020a)",
	"(Smith and Jones, 2020)", "(Smith 2019)", "(Windows 2000)", "(RFC 2119)", "(2019)", "(田中)",
	"田中様", "田中さん", "山田様、", "皆様", "みなさん", "お客様", "仕様", "模様", "同様", "母さん", "お父さん", "山田 太郎様", "ＡＢＣ商事様", "田中様の件", "山田さんは",
	"a@example.com", "foo.bar@example.co.jp", "foo+bar@example.com", "a@b", "@example.com", "a@example..com", "mailto:a@example.com", "<a@example.com>",
	"5 mg", "5mg", "10 mL", "3.5 kHz", "20℃", "5°C", "100 g", "2 mol", "5 mgx", "5 kg", "1,000 mg", "5 mg/dl", "5 Pa", "5 W", "5 s", "5 ms",
	"p < 0.05", "p<0.05", "p = .03", "p ≤ 0.05", "P>0.01", "r=0.5", "F>=3", "SD = 2", "SE=1.5", "α = 0.05", "t > 2e-3", "p<0.05と報告", "a < 0.05",
	"-5", "+3", "−2", "前年比-5%", "10月-12月", "9時-17時", "1万円-2万円", "(-5)", "x=-5", "-5.5", ".5", "5", "3.14", "-.5",
}

func diffReviewHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "hedge-same", old: doc("遅れると考えられる。"), new: doc("遅れると考えられる。")},
		{name: "hedge-removed", old: doc("遅れると考えられる。"), new: doc("遅れます。")},
		{name: "hedge-removed-line", old: doc("遅れると考えられる。", "設定を開きます。"), new: doc("設定を開きます。")},
		{name: "hedge-moved", old: doc("遅れると考えられる。", "設定を開きます。"), new: doc("設定を開きます。", "遅れると考えられる。")},
		{name: "hedge-two-to-one", old: doc("遅れると考えられ、遅れる可能性がある。"), new: doc("遅れると考えられる。")},
		{name: "hedge-two-to-zero", old: doc("遅れると考えられ、遅れる可能性がある。"), new: doc("遅れます。")},
		{name: "hedge-split", old: doc("遅れると考えられるが、確認する。"), new: doc("確認する。遅れると考えられる。")},
		{name: "hedge-split-twice", old: doc("遅れるかもしれないし、止まると思われる。"), new: doc("遅れるかもしれない。止まると思われる。")},
		{name: "hedge-merged", old: doc("遅れると考えられる。確認する。"), new: doc("確認します。")},
		{name: "hedge-added", old: doc("遅れます。"), new: doc("遅れると考えられる。")},
		{name: "hedge-same-count", old: doc("遅れると考えられる。"), new: doc("遅れると思われる。")},
		{name: "hedge-longest-first", old: doc("遅れる可能性があるかもしれない。"), new: doc("遅れる。")},
		{name: "hedge-marks-covered", old: doc("遅れるかもしれない。"), new: doc("遅れます。")},
		{name: "hedge-certainty-covered", old: doc("遅れる可能性がある。"), new: doc("遅れます。")},
		{name: "hedge-heading", old: doc("# 遅れると考えられる"), new: doc("# 遅れる")},
		{name: "hedge-table", old: doc("| a | b |", "|---|---|", "| 遅れると考えられる | x |"), new: doc("| a | b |", "|---|---|", "| 遅れる | x |")},
		{name: "hedge-inline-code", old: doc("`と考えられる`と遅れると考えられる。"), new: doc("`と考えられる`と遅れる。")},
		{name: "hedge-code-in-new", old: doc("遅れると考えられる。"), new: doc("```", "遅れると考えられる。", "```")},
		{name: "hedge-other-line", old: doc("遅れると考えられる。", "遅れると考えられる。"), new: doc("遅れると考えられる。", "遅れます。")},
		{name: "hedge-skip-kind-partner", old: doc("遅れると考えられる。"), new: doc("# 遅れる")},
		{name: "hedge-run-shifted", old: doc("遅れると考えられる。", "", "確認する。"), new: doc("遅れる。確認する。", "", "遅れると考えられる。")},
		{name: "value-citation-lost", old: doc("詳しくは[1]を見る。"), new: doc("詳しくは見る。")},
		{name: "value-citation-added", old: doc("詳しく見る。"), new: doc("詳しく[1]見る。")},
		{name: "value-citation-number", old: doc("詳しくは[1]を見る。"), new: doc("詳しくは[2]を見る。")},
		{name: "value-citation-reorder", old: doc("詳しくは[1, 2]を見る。"), new: doc("詳しくは[2, 1]を見る。")},
		{name: "value-citation-spelling", old: doc("詳しくは（田中ほか，2019）を見る。"), new: doc("詳しくは(田中ほか, 2019)を見る。")},
		{name: "value-citation-author", old: doc("詳しくは(田中, 2019)を見る。"), new: doc("詳しくは(山田, 2019)を見る。")},
		{name: "value-citation-year", old: doc("詳しくは(Smith, 2019)を見る。"), new: doc("詳しくは(Smith, 2020)を見る。")},
		{name: "value-citation-moved", old: doc("詳しくは[1]を見る。", "設定を開く。"), new: doc("設定を開く。", "詳しくは[1]を見る。")},
		{name: "value-citation-twice-to-once", old: doc("[1]と[1]を見る。"), new: doc("[1]を見る。")},
		{name: "value-citation-unchanged-elsewhere", old: doc("[1]を見る。", "", "[1]を読む。"), new: doc("[1]を見る。", "", "[2]を読む。")},
		{name: "value-addressee-changed", old: doc("田中様、", "確認します。"), new: doc("山田様、", "確認します。")},
		{name: "value-addressee-lost", old: doc("田中様、", "確認します。"), new: doc("確認します。")},
		{name: "value-addressee-label", old: doc("宛先: 田中様", "確認します。"), new: doc("宛先: 山田様", "確認します。")},
		{name: "value-addressee-excluded", old: doc("仕様、", "皆様、", "お母様、"), new: doc("確認します。")},
		{name: "value-email-changed", old: doc("a@example.com に送る。"), new: doc("b@example.com に送る。")},
		{name: "value-email-in-url", old: doc("[a](mailto:a@example.com) を使う。"), new: doc("使う。")},
		{name: "value-unit-changed", old: doc("5 mg を投与する。"), new: doc("5 g を投与する。")},
		{name: "value-unit-spacing", old: doc("5 mg を投与する。"), new: doc("5mg を投与する。")},
		{name: "value-unit-reported-by-number", old: doc("5 mg を投与する。"), new: doc("6 mg を投与する。")},
		{name: "value-statistic-sign", old: doc("p < 0.05 だった。"), new: doc("p > 0.05 だった。")},
		{name: "value-statistic-number", old: doc("p < 0.05 だった。"), new: doc("p < 0.01 だった。")},
		{name: "value-statistic-same", old: doc("p < 0.05 だった。"), new: doc("p<0.05 だった。")},
		{name: "value-statistic-letter", old: doc("p = 0.05 だった。"), new: doc("P = 0.05 だった。")},
		{name: "value-statistic-letter-r-t", old: doc("r = 0.5 だった。"), new: doc("t = 0.5 だった。")},
		{name: "value-sign-flip", old: doc("温度は-5度。"), new: doc("温度は5度。")},
		{name: "value-sign-flip-plus", old: doc("差は+3。"), new: doc("差は-3。")},
		{name: "value-sign-flip-minus-char", old: doc("差は−3。"), new: doc("差は3。")},
		{name: "value-sign-range", old: doc("10月-12月に行う。"), new: doc("10月12月に行う。")},
		{name: "value-sign-many", old: doc("-1 と -2 と -3 を使う。"), new: doc("1 と 2 と 3 を使う。")},
		{name: "value-table", old: doc("| a | b |", "|---|---|", "| [1] | x |"), new: doc("| a | b |", "|---|---|", "| [2] | x |")},
		{name: "value-heading", old: doc("# 結果[1]"), new: doc("# 結果")},
		{name: "value-code-skipped", old: doc("```", "[1] a@example.com", "```"), new: doc("```", "[2] b@example.com", "```")},
		{name: "value-inline-code-skipped", old: doc("`[1]` を使う。"), new: doc("`[2]` を使う。")},
		{name: "value-url-skipped", old: doc("https://a.example/[1] を見る。"), new: doc("https://a.example/[2] を見る。")},
		{name: "value-multi-hunk", old: doc("[1]を見る。", "同じ行です。", "[2]を見る。"), new: doc("[3]を見る。", "同じ行です。", "[4]を見る。")},
		{name: "value-run-shifted", old: doc("[1]を見る。", "", "[2]を見る。"), new: doc("[1]を見る。[2]を見る。", "", "確認する。")},
		{name: "value-unordered-sentences", old: doc("a[1]b[2]c[3]。"), new: doc("a[3]b[2]c[1]。")},
		{name: "politeness-up", old: doc("確認する。送る。見る。読む。"), new: doc("確認いたします。送らせていただきます。ご覧くださいます。お読みください。")},
		{name: "politeness-down", old: doc("確認いたします。送らせていただきます。ご覧くださいます。"), new: doc("確認する。送る。見る。")},
		{name: "politeness-ratio", old: doc("確認します。送ります。見ます。"), new: doc("確認する。送る。見る。")},
		{name: "politeness-ratio-up", old: doc("確認する。送る。見る。"), new: doc("確認します。送ります。見ます。")},
		{name: "politeness-both", old: doc("確認する。送る。見る。"), new: doc("確認させていただきます。送らせていただきます。ご覧いただきます。")},
		{name: "politeness-honorifics", old: doc("申し上げます。存じます。くださいます。いただきます。させていただきます。"), new: doc("言う。知る。")},
		{name: "politeness-no-ending", old: doc("体言止め", "もう一つ"), new: doc("確認します。")},
	}
}

// diffQuoteBank はかぎかっこを持つ文 (入れ子・閉じない・『』・会話・句点をまたぐ会話・伏せる範囲の中)。
var diffQuoteBank = []string{
	"彼は「はい」と答えた。", "彼は「いいえ」と答えた。", "彼は「必要」と言った。", "彼は「不必要」と言った。", "「行く」と言った。", "「行かない」と言った。", "「うん。うん。そう。」と言った。",
	"『確認する』と言った。", "『確認しない』と言った。", "彼は「外側「内側」外側」と言った。", "彼は「閉じない", "閉じない」と言った。", "彼は「あ」と言った。", "彼は「 は い 」と言った。", "彼は「、。」と言った。",
	"「ビール」と「ビル」", "「ABC」と言った。", "「ａｂｃ」と言った。", "「確認する？」と言った。", "「確認する?」と言った。", "`「コード」`と書く。", "[「リンク」](https://a.example/)を開く。",
	"田中「はい」", "田中：「はい」", "「はい」", "「はい」。", "『はい』。", "田中「はい」と言った。", "「はい」「いいえ」", "「はい」と「いいえ」と「はい」", "「はい」、「はい」、「はい」。",
	"設定を開きます。", "結果は3件でした。", "確認します。", "彼は確認すると言った。",
}

func diffQuoteHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("彼は「はい」と答えた。"), new: doc("彼は「はい」と答えた。")},
		{name: "changed", old: doc("彼は「はい」と答えた。"), new: doc("彼は「いいえ」と答えた。")},
		{name: "changed-similar", old: doc("彼は「必要」と言った。"), new: doc("彼は「不必要」と言った。")},
		{name: "lost", old: doc("彼は「はい」と答えた。"), new: doc("彼は答えた。")},
		{name: "brackets-dropped", old: doc("彼は「はい」と答えた。"), new: doc("彼ははいと答えた。")},
		{name: "moved", old: doc("彼は「はい」と答えた。", "設定を開く。"), new: doc("設定を開く。", "彼は「はい」と答えた。")},
		{name: "moved-same-document", old: doc("彼は「はい」と答えた。", "彼は「はい」と答えた。"), new: doc("彼は「はい」と答えた。", "彼は答えた。")},
		{name: "partner-order", old: doc("「行く」と「戻る」と言った。"), new: doc("「行かない」と「戻らない」と言った。")},
		{name: "partner-count-differs", old: doc("「行く」と言った。"), new: doc("「行かない」と「戻らない」と言った。")},
		{name: "partner-wins-low-ratio", old: doc("「はい」と言った。"), new: doc("「いいえ」と言った。")},
		{name: "neighbor-unpaired", old: doc("彼は「はい」と答えた。それで終わりです。"), new: doc("彼は答えた。それで終わりです。「はい」と言った。")},
		{name: "unpaired-old-sentence", old: doc("確認する。彼は「はい」と答えた。", "次に進む。"), new: doc("確認します。", "次に進みます。")},
		{name: "unpaired-far-away", old: doc("a。b。c。d。e。f。g。h。", "彼は「はい」と答えた。"), new: doc("a。b。c。d。e。f。g。h。", "彼は答えた。")},
		{name: "unpaired-window", old: doc("彼は「はい」と答えた。", "あ。い。う。え。お。か。き。く。け。こ。"), new: doc("あ。い。う。え。お。か。き。く。け。こ。「はい」。")},
		{name: "unpaired-window-middle", old: doc("あ。い。う。え。お。か。き。く。け。こ。", "彼は「はい」と答えた。", "さ。し。す。せ。そ。"), new: doc("あ。い。う。え。お。か。き。く。け。こ。", "さ。し。す。せ。そ。「はい」。")},
		{name: "window-edge", old: doc("A。彼は「はい」と答えた。B「いいえ」。"), new: doc("A。あ。い。う。え。お。B「いいえ」。")},
		{name: "window-edge-five", old: doc("A。彼は「はい」と答えた。B「いいえ」。"), new: doc("A。あ。い。う。え。B「いいえ」。")},
		{name: "window-edge-seven", old: doc("A。彼は「はい」と答えた。B「いいえ」。"), new: doc("A。あ。い。う。え。お。か。B「いいえ」。")},
		{name: "no-new-sentences", old: doc("彼は「はい」と答えた。"), new: ""},
		{name: "nested", old: doc("彼は「外側「内側」外側」と言った。"), new: doc("彼は「外側外側」と言った。")},
		{name: "unclosed", old: doc("彼は「はい", "と言った。"), new: doc("彼は「いいえ", "と言った。")},
		{name: "double-bracket", old: doc("『確認する』と言った。"), new: doc("『確認しない』と言った。")},
		{name: "dialogue", old: doc("田中「はい」"), new: doc("田中「いいえ」")},
		{name: "dialogue-line", old: doc("「はい」"), new: doc("「いいえ」")},
		{name: "quote-row", old: doc("> 彼は「はい」と答えた。"), new: doc("> 彼は「いいえ」と答えた。")},
		{name: "heading", old: doc("# 彼は「はい」と答えた"), new: doc("# 彼は「いいえ」と答えた")},
		{name: "table", old: doc("| a | b |", "|---|---|", "| 「はい」 | x |"), new: doc("| a | b |", "|---|---|", "| 「いいえ」 | x |")},
		{name: "inline-code", old: doc("`「コード」`と「はい」。"), new: doc("`「コード」`と「いいえ」。")},
		{name: "link", old: doc("[「リンク」](https://a.example/)と「はい」。"), new: doc("[「リンク」](https://a.example/)と「いいえ」。")},
		{name: "short-key", old: doc("彼は「あ」と言った。"), new: doc("彼は「い」と言った。")},
		{name: "punctuation-only-key", old: doc("彼は「、。」と言った。"), new: doc("彼は「！」と言った。")},
		{name: "width-variants", old: doc("「ａｂｃ」と言った。"), new: doc("「abc」と言った。")},
		{name: "question-variants", old: doc("「確認する？」と言った。"), new: doc("「確認する?」と言った。")},
		{name: "kept-elsewhere", old: doc("「はい」と言った。", "同じ行。", "「はい」と言った。"), new: doc("答えた。", "同じ行。", "「はい」と言った。")},
		{name: "sentence-boundary", old: doc("「うん。うん。そう。」と言った。"), new: doc("「うん。そう。」と言った。")},
		{name: "two-quotes-two-sentences", old: doc("「はい」と言った。「いいえ」と言った。"), new: doc("「はい」と言った。答えた。")},
		{name: "run-shifted", old: doc("「はい」と言った。", "", "確認する。"), new: doc("答えた。確認する。", "", "確認する。")},
		{name: "crlf", old: "彼は「はい」と答えた。\r\n", new: "彼は「いいえ」と答えた。\r\n"},
	}
}

// diffRegisterStems は文体を切り替える文の組 (常体, 敬体)。「下さい」の表記・体言止め・長い文も含む。
var diffRegisterStems = [][2]string{
	{"設定を開く。", "設定を開きます。"}, {"保存を押した。", "保存を押しました。"}, {"結果は3件だ。", "結果は3件です。"}, {"これは設計である。", "これは設計です。"}, {"確認しない。", "確認しません。"},
	{"実行して下さい。", "実行してください。"}, {"問題は無かった。", "問題はありませんでした。"}, {"これは重要だね。", "これは重要ですね。"}, {"体言止め", "体言止めです"}, {"動かない。", "動きません。"},
	{"設定を開き、保存を押す。", "設定を開き、保存を押します。"}, {"長い文でも、最後が常体なら常体と数える。", "長い文でも、最後が敬体なら敬体と数えます。"},
}

func diffRegisterHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "to-polite", old: doc("設定を開く。", "保存を押した。", "結果は3件だ。"), new: doc("設定を開きます。", "保存を押しました。", "結果は3件です。")},
		{name: "to-plain", old: doc("設定を開きます。", "保存を押しました。", "結果は3件です。"), new: doc("設定を開く。", "保存を押した。", "結果は3件だ。")},
		{name: "two-only", old: doc("設定を開く。", "保存を押した。"), new: doc("設定を開きます。", "保存を押しました。")},
		{name: "ratio-too-low", old: doc("設定を開く。", "保存を押した。", "結果は3件だ。", "確認しない。", "動かない。", "これは設計である。"), new: doc("設定を開きます。", "保存を押しました。", "結果は3件です。", "確認しない。", "動かない。", "これは設計である。")},
		{name: "ratio-exact-half", old: doc("設定を開く。", "保存を押した。", "結果は3件だ。", "確認しない。", "動かない。", "これは設計である。"), new: doc("設定を開きます。", "保存を押しました。", "結果は3件です。", "確認しない。", "動かない。", "これは設計である。")},
		{name: "unified", old: doc("設定を開きます。", "保存を押しました。", "結果は3件です。", "確認しない。"), new: doc("設定を開く。", "保存を押した。", "結果は3件だ。", "確認しない。")},
		{name: "switched", old: doc("設定を開く。", "保存を押した。", "結果は3件だ。", "動かない。"), new: doc("設定を開きます。", "保存を押しました。", "結果は3件です。", "動かない。")},
		{name: "kudasai", old: doc("実行して下さい。", "確認して下さい。", "保存して下さい。"), new: doc("実行してください。", "確認してください。", "保存してください。")},
		{name: "kudasai-plain", old: doc("実行して下さい。", "確認して下さい。", "保存して下さい。"), new: doc("実行する。", "確認する。", "保存する。")},
		{name: "window-edge", old: doc("A。設定を開く。設定を開きます。", "同じ行です。", "保存を押した。結果は3件だ。"), new: doc("A。い。う。え。お。か。設定を開きます。", "同じ行です。", "保存を押しました。結果は3件です。")},
		{name: "window-edge-five", old: doc("A。設定を開く。設定を開きます。", "同じ行です。", "保存を押した。結果は3件だ。"), new: doc("A。い。う。え。お。設定を開きます。", "同じ行です。", "保存を押しました。結果は3件です。")},
		{name: "window-edge-seven", old: doc("A。設定を開く。設定を開きます。", "同じ行です。", "保存を押した。結果は3件だ。"), new: doc("A。い。う。え。お。か。き。設定を開きます。", "同じ行です。", "保存を押しました。結果は3件です。")},
		{name: "merged-sentences", old: doc("設定を開く。保存を押す。結果を見る。"), new: doc("設定を開き、保存を押し、結果を見ます。")},
		{name: "merged-three-sets", old: doc("設定を開く。保存を押す。", "結果を見る。確認する。", "送る。戻る。"), new: doc("設定を開き、保存を押します。", "結果を見て、確認します。", "送り、戻ります。")},
		{name: "merged-short", old: doc("開く。押す。見る。"), new: doc("開き、押し、見ます。")},
		{name: "merged-low-contained", old: doc("設定を開く。保存を押す。結果を見る。"), new: doc("まったく別の話をします。")},
		{name: "dialogue-skipped", old: doc("田中「確認する」", "山田「見る」", "鈴木「送る」"), new: doc("田中「確認します」", "山田「見ます」", "鈴木「送ります」")},
		{name: "quote-row-skipped", old: doc("> 設定を開く。", "> 保存を押す。", "> 見る。"), new: doc("> 設定を開きます。", "> 保存を押します。", "> 見ます。")},
		{name: "heading-skipped", old: doc("# 設定を開く", "# 保存を押す", "# 見る"), new: doc("# 設定を開きます", "# 保存を押します", "# 見ます")},
		{name: "inline-code", old: doc("`コード` を開く。", "`コード` を押す。", "`コード` を見る。"), new: doc("`コード` を開きます。", "`コード` を押します。", "`コード` を見ます。")},
		{name: "nominal-end", old: doc("体言止め", "もう一つ", "もう一つ"), new: doc("体言止めです", "もう一つです", "もう一つです")},
		{name: "polite-ratio-skip-mail", old: doc("確認します。送ります。見ます。"), new: doc("確認する。送る。見る。")},
		{name: "long-unpaired", old: doc("あ。い。う。え。お。か。き。く。け。こ。さ。し。す。せ。そ。"), new: doc("設定を開きます。")},
		{name: "many-unpaired", old: doc("設定を開く。", "保存を押す。", "結果を見る。", "確認する。", "送る。", "戻る。", "消す。", "足す。"), new: doc("設定を開きます。保存を押します。結果を見ます。確認します。送ります。戻ります。消します。足します。")},
		{name: "unpaired-middle-window", old: doc("開く。", "押す。見る。確認する。", "送る。"), new: doc("開きます。", "押し、見て、確認します。", "送ります。")},
	}
}

// diffReviewFuzz は固定の種から、文の bank を old と new に並べて書き換える組 (入れ替える・消す・足す・同じ行につなぐ・前後に文を置く)。
func diffReviewFuzz(seed uint64, bank []string, n int, wrap []string, partner func(r *rand.Rand, s string) string) []diffFullCase {
	r := rand.New(rand.NewPCG(1076, seed))
	var cases []diffFullCase
	for i := range n {
		var old, new []string
		for range 1 + r.IntN(4) {
			w := randPick(r, wrap)
			o := randPick(r, bank)
			nw := randPick(r, bank)
			switch r.IntN(4) {
			case 0:
				nw = o
			case 1:
				nw = partner(r, o)
			}
			old, new = append(old, diffTemplate(w, "s", o)), append(new, diffTemplate(w, "s", nw))
			if r.IntN(4) == 0 {
				c := randPick(r, bank)
				if r.IntN(2) == 0 {
					old = append(old, c)
				} else {
					new = append(new, c)
				}
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

var diffReviewWrap = []string{"{s}", "{s}", "{s}", "- {s}", "## {s}", "> {s}", "| {s} | x |", "前の文です。{s}", "{s}後の文です。", "`{s}`"}

func diffReviewHedgeFuzz() []diffFullCase {
	return diffReviewFuzz(30, diffReviewHedgeBank, 1800, diffReviewWrap, func(r *rand.Rand, s string) string {
		return strings.TrimSuffix(s, "。") + randPick(r, []string{"。", "。確認する。", "が、確認する。", "。遅れると考えられる。", "か。"})
	})
}

func diffReviewValueFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 31))
	var cases []diffFullCase
	for i := range 2800 {
		var old, new []string
		for range 1 + r.IntN(4) {
			f := randPick(r, diffReviewValueBank)
			o, nw := randPick(r, diffReviewValues), randPick(r, diffReviewValues)
			switch r.IntN(4) {
			case 0:
				nw = o
			case 1:
				nw = strings.ReplaceAll(strings.ReplaceAll(o, "1", "2"), "5", "6")
			case 2:
				nw = "" // 値を消す
			}
			old = append(old, diffTemplate(f, "v", o))
			if nw == "" {
				new = append(new, "確認します。")
			} else {
				new = append(new, diffTemplate(f, "v", nw))
			}
			if r.IntN(4) == 0 {
				old = append(old, diffTemplate(randPick(r, diffReviewValueBank), "v", randPick(r, diffReviewValues)))
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

func diffReviewQuoteFuzz() []diffFullCase {
	return diffReviewFuzz(33, diffQuoteBank, 2400, diffReviewWrap, func(r *rand.Rand, s string) string {
		return strings.NewReplacer("はい", "いいえ", "行く", "行かない", "確認する", "確認しない", "必要", "不必要").Replace(s)
	})
}

// diffReviewPolitenessFuzz は固定の種 (PCG 1076/32) から、敬語と敬体・常体の文の割合を変える組 (1 文書に 2〜8 文)。
func diffReviewPolitenessFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 32))
	honorific := []string{"確認させていただきます。", "ご覧いただきます。", "お読みくださいます。", "ご連絡申し上げます。", "存じます。", "存じました。", "存じません。", "いただけます。", "くださいました。", "いただかない。", "させていただいた。"}
	polite := []string{"確認します。", "送りました。", "ありません。", "ませんでした。", "確認しますか。", "動きますね。", "動きますよね。", "確認です。", "確認でした。"}
	plain := []string{"確認する。", "送った。", "これは設計だ。", "これは設計である。", "これは設計であった。", "これは設計だった。", "これは設計だね。", "これは設計だよ。", "体言止め", "確認、"}
	pick := func(weights [3]int) string {
		switch n := r.IntN(weights[0] + weights[1] + weights[2]); {
		case n < weights[0]:
			return randPick(r, honorific)
		case n < weights[0]+weights[1]:
			return randPick(r, polite)
		default:
			return randPick(r, plain)
		}
	}
	var cases []diffFullCase
	for i := range 1800 {
		build := func() string {
			weights := [3]int{r.IntN(4), r.IntN(4), r.IntN(4)}
			if weights == [3]int{} {
				weights[2] = 1
			}
			var lines []string
			for range 2 + r.IntN(7) {
				lines = append(lines, pick(weights))
			}
			return diffContrastJoin(r, lines)
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: build(), new: build()})
	}
	return cases
}

// diffReviewRegisterFuzz は固定の種 (PCG 1076/34) から、文体を切り替える文を並べた文書を、一部だけ・全部・合わせた文で書き換える組。
func diffReviewRegisterFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 34))
	var cases []diffFullCase
	for i := range 2200 {
		n := 2 + r.IntN(8)
		toPolite := r.IntN(2) == 0
		unify := r.IntN(2) == 0 // 切り替えない文を、切り替え先の文体にする (もともと多数だった文体へそろえる形)
		var old, new []string
		for range n {
			stem := diffRegisterStems[r.IntN(len(diffRegisterStems))]
			o, nw := stem[0], stem[1]
			if !toPolite {
				o, nw = nw, o
			}
			if r.IntN(3) == 0 {
				nw = o // 切り替えない文
				if unify {
					if toPolite {
						o, nw = stem[1], stem[1]
					} else {
						o, nw = stem[0], stem[0]
					}
				}
			}
			old, new = append(old, o), append(new, nw)
		}
		oldDoc, newDoc := diffContrastJoin(r, old), diffContrastJoin(r, new)
		if r.IntN(4) == 0 {
			// 文を合わせた推敲: 新しい側を 1 つの文につなぐ
			joined := strings.Join(new, "")
			newDoc = diffDoc(strings.TrimSuffix(strings.Replace(joined, "。", "、", len(new)-1), "。") + "。")
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: oldDoc, new: newDoc})
	}
	return cases
}

// diffReviewStringCases は下の関数 (文字列の入力だけ) の呼び出しの case。
func diffReviewStringCases() []VisCase {
	keep := func(c Call) Call { c.Keep = true; return c }
	var keys []Call
	for _, v := range diffReviewValues {
		keys = append(keys, dcall("_citation_key", v))
	}
	for _, v := range []string{"（田中，2019）", "(田中、2019)", "[1–3]", "[1—3]", "[1−3]", "(Smith  &  Jones, 2020)", "[ 1 , 2 ]", "[1\t2]", "[1 2]", "[1　2]", ""} {
		keys = append(keys, dcall("_citation_key", v))
	}
	var registers []Call
	for _, s := range diffEndingStrings {
		registers = append(registers, dcall("_register_of", s))
	}
	for _, s := range []string{"実行して下さい。", "下さい", "実行してください。", "ご確認下さい", "下さい。下さい。"} {
		registers = append(registers, keep(dcall("_register_of", s)))
	}
	var contained []Call
	pairs := [][2]string{{"", "あ"}, {"あ", ""}, {"あいう", "あいう"}, {"あいう", "あxいxう"}, {"あいう", "うあい"}, {"設定を開く", "設定を開きます"}, {"abcd", "abxcd"}, {"あ", "いあう"}}
	for _, p := range pairs {
		contained = append(contained, keep(dcall("_contained", p[0], p[1])))
	}
	for _, s := range diffQuoteBank {
		for _, t := range []string{s, strings.ReplaceAll(s, "「", "『")} {
			contained = append(contained, dcall("_contained", t, s+"。"))
		}
	}
	var neighbors []Call
	order := [][]any{{}, {[]any{0, 1}}, {[]any{0, 2}, []any{3, 4}}, {[]any{1, 1}, []any{1, 2}, []any{4, 7}}, {[]any{2, 5}, []any{2, 6}}}
	for _, o := range order {
		firsts := make([]any, len(o))
		for k, p := range o {
			firsts[k] = p.([]any)[0]
		}
		for i := -1; i <= 6; i++ {
			neighbors = append(neighbors, keep(dcall("_neighbor_pairs", o, firsts, i, 9)))
		}
	}
	var quotes []Call
	for _, s := range append(append([]string{}, diffQuoteBank...), "「「「」」", "」「", "「『」』", "『「」』", "「」「」", "「あ」『い』「う」", "", "「」") {
		quotes = append(quotes, dcall("_line_quotes", s), dcall("_quote_normalize", s))
	}
	for _, s := range []string{"はい？", "はい?", "ａｂｃ", "ＡＢＣ…", "a・b", "a...b", "a ー b", "ビール", "〜～－-", "１２３", "ｱｲｳ"} {
		quotes = append(quotes, keep(dcall("_quote_normalize", s)))
	}
	return []VisCase{
		{Name: "review/constants", Calls: diffConstantCalls(diffReviewConstants)},
		{Name: "review/citation-key", Calls: keys},
		{Name: "review/register-of", Calls: registers},
		{Name: "review/contained", Calls: contained},
		{Name: "review/neighbor-pairs", Calls: neighbors},
		{Name: "review/quotes", Calls: quotes},
		{Name: "review/covered", Calls: diffReviewCoveredCalls()},
	}
}

// diffReviewCoveredCalls は _citation_number_covered の手書きの呼び出し。(key, body) の組は [key, _Body] の list で渡す (本家は添字でしか読まない)。
func diffReviewCoveredCalls() []Call {
	var calls []Call
	body := func(name, text string, line int) {
		c := dcall("_Body", text, text, line, 0)
		c.Save, c.Quiet = name, true
		calls = append(calls, c)
	}
	tok := func(key, name string) any { return []any{key, diffRef(name)} }
	body("b1", "[1]", 1)
	body("b2", "[2]", 1)
	body("b3", "[1, 2]", 2)
	body("b4", "[2, 1]", 2)
	body("b5", "(Smith, 2019)", 3)
	body("b6", "(Smith, 2020)", 3)
	body("b7", "[3]", 4)
	number := func(sub string, oldLine, newLine any, detail map[string]any) map[string]any {
		return map[string]any{"kind": "number", "subkind": sub, "old_line": oldLine, "new_line": newLine, "old": nil, "new": nil, "detail": detail, "basis": nil}
	}
	order := number("order", 2, 2, map[string]any{"old_order": []any{"1", "2"}, "new_order": []any{"2", "1"}})
	orderOther := number("order", 2, 2, map[string]any{"old_order": []any{"2", "1"}, "new_order": []any{"1", "2"}})
	orderNewOther := number("order", 2, 2, map[string]any{"old_order": []any{"1", "2"}, "new_order": []any{"1", "2"}})
	orderLine := number("order", 9, 2, map[string]any{"old_order": []any{"1", "2"}, "new_order": []any{"2", "1"}})
	removed := func(line int, items ...any) map[string]any {
		return number("lost", line, nil, map[string]any{"removed": items, "added": []any{}})
	}
	added := func(line int, items ...any) map[string]any {
		return number("added", nil, line, map[string]any{"removed": []any{}, "added": items})
	}
	replaced := number("replaced", 3, 3, map[string]any{"removed": []any{"2019"}, "added": []any{"2020"}})
	other := map[string]any{"kind": "negation", "subkind": nil, "old_line": 1, "new_line": 1, "old": nil, "new": nil, "detail": map[string]any{}, "basis": nil}
	for _, c := range []struct {
		old, new any
		risks    []any
	}{
		{tok("[1]", "b1"), tok("[2]", "b2"), nil},
		{tok("[1]", "b1"), tok("[2]", "b2"), []any{replaced}},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), nil},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), []any{order}},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), []any{orderOther}},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), []any{orderLine}},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), []any{orderNewOther}},
		{tok("[1, 2]", "b3"), tok("[2, 1]", "b4"), []any{other, order}},
		{tok("(Smith, 2019)", "b5"), tok("(Smith, 2020)", "b6"), nil},
		{tok("(Smith, 2019)", "b5"), tok("(Smith, 2020)", "b6"), []any{replaced}},
		{tok("(Smith, 2019)", "b5"), tok("(Smith, 2020)", "b6"), []any{number("replaced", 3, 3, map[string]any{"removed": []any{"2019"}, "added": []any{}})}},
		{tok("(Smith, 2019)", "b5"), tok("(Smith, 2020)", "b6"), []any{number("replaced", 3, 3, map[string]any{"removed": []any{}, "added": []any{"2020"}})}},
		{tok("[1]", "b1"), tok("(Smith, 2020)", "b6"), []any{replaced}},
		{tok("[3]", "b7"), nil, nil},
		{tok("[3]", "b7"), nil, []any{removed(4, "3")}},
		{tok("[3]", "b7"), nil, []any{removed(5, "3")}},
		{tok("[3]", "b7"), nil, []any{removed(4, "4")}},
		{tok("[3]", "b7"), nil, []any{other}},
		{nil, tok("[3]", "b7"), nil},
		{nil, tok("[3]", "b7"), []any{added(4, "3")}},
		{nil, tok("[3]", "b7"), []any{removed(4, "3")}},
		{nil, tok("[1, 2]", "b3"), []any{added(2, "1")}},
		{nil, tok("[1, 2]", "b3"), []any{added(2, "1", "2")}},
		{tok("[1, 2]", "b3"), nil, []any{removed(2, "1, 2")}},
		{tok("[1, 2]", "b3"), nil, []any{removed(2, "1"), removed(2, "2")}},
		{tok("[1, 2]", "b3"), tok("[1]", "b1"), []any{removed(2, "2")}},
		{tok("[1]", "b1"), tok("[1]", "b1"), nil},
		{tok("[1]", "b1"), tok("[1]", "b1"), []any{order}},
	} {
		calls = append(calls, Call{Module: DiffModule, Function: "_citation_number_covered", Args: []any{c.old, c.new, orEmpty(c.risks)}, Digest: true, Keep: true})
	}
	return calls
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// diffReviewCases は群 X・W の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffReviewCases(pairs [][3]string) []VisCase {
	cases := diffReviewStringCases()
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "review/" + b.name, Calls: diffReviewStageCalls(b.old, b.new, reviewAll, false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		parts  diffReviewParts
		keep   bool
	}{
		{"review/hand/", diffReviewHandPairs(), reviewAll, true},
		{"review/quote-hand/", diffQuoteHandPairs(), reviewQuote | reviewRegister, true},
		{"review/register-hand/", diffRegisterHandPairs(), reviewPoliteness | reviewRegister, true},
		{"review/hedge-fuzz/", diffReviewHedgeFuzz(), reviewHedges, false},
		{"review/value-fuzz/", diffReviewValueFuzz(), reviewImmutable, false},
		{"review/politeness-fuzz/", diffReviewPolitenessFuzz(), reviewPoliteness | reviewRegister, false},
		{"review/quote-fuzz/", diffReviewQuoteFuzz(), reviewQuote, false},
		{"review/register-fuzz/", diffReviewRegisterFuzz(), reviewRegister | reviewPoliteness, false},
	} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffReviewStageCalls(f.old, f.new, g.parts, g.keep)})
		}
	}
	return cases
}

// withKeep は呼び出しの結果そのものを記録に残すか (keep) を決める。
func withKeep(keep bool, c Call) Call {
	c.Keep = keep
	return c
}
