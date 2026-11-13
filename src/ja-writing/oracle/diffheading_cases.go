package oracle

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// 群 K (見出しの構造) の段 (_heading_structure) に通す組。本家の _build_full_diff が作る (_Sides, hunks) を渡して、
// 見出しの数・段・見出しと本文の行の入れ替わり・見出しの形の変化・組み直しの規則の分かれ目を通す。

// diffHeadingStageCalls は組 1 つを、本家の _build_full_diff (覚える) と _heading_structure の 2 通りの呼び方 (shapes あり・なし) に通す呼び出し。
func diffHeadingStageCalls(old, new string) []Call {
	ref := func(path ...any) map[string]any { return map[string]any{"$ref": "full", "path": path} }
	build := dcall("_build_full_diff", old, new, nil)
	build.Save, build.Quiet = "full", true
	withShapes := dcall("_heading_structure", ref(1), ref(0, "hunks"), []any{})
	withShapes.ArgsOutOnly = []int{2}
	withShapes.Keep = len(old)+len(new) < 3000
	plain := dcall("_heading_structure", ref(1), ref(0, "hunks"))
	pairs := dcall("_sentence_pairs", ref(1), ref(0))
	pairs.Keep = len(old)+len(new) < 600
	bodies := func(side int) Call {
		c := dcall("_all_bodies", ref(1), side)
		c.Keep = len(old)+len(new) < 600
		return c
	}
	return []Call{build, withShapes, plain, pairs, bodies(0), bodies(1)}
}

// diffDoc は行を改行でつないだ文書 (末尾に改行を付ける)。
func diffDoc(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// diffHeadingCases は見出しの段の手書きの組と固定の種の組 (名前は heading/ を付けて case にする)。
func diffHeadingCases() []diffFullCase {
	doc := diffDoc
	numbered := func(n int, format string) []string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = fmt.Sprintf(format, i)
		}
		return lines
	}
	cases := []diffFullCase{
		{name: "level-shown-3", old: doc("# 一", "本文", "## 二", "本文", "### 三", "本文"), new: doc("## 一", "本文", "### 二", "本文", "#### 三", "本文")},
		{name: "level-shown-4", old: doc("# 一", "## 二", "### 三", "#### 四"), new: doc("## 一", "### 二", "#### 三", "##### 四")},
		{name: "level-shown-5", old: doc("# 一", "## 二", "### 三", "#### 四", "##### 五"), new: doc("## 一", "### 二", "#### 三", "##### 四", "###### 五")},
		{name: "width-30", old: doc("# "+kanjiRun(1, 30), "本文"), new: doc("## "+kanjiRun(1, 30), "本文")},
		{name: "width-31", old: doc("# "+kanjiRun(2, 31), "本文"), new: doc("## "+kanjiRun(2, 31), "本文")},
		{name: "setext-to-atx", old: doc("題名", "====", "", "節", "----", "本文"), new: doc("# 題名", "", "## 節", "本文")},
		{name: "atx-to-setext", old: doc("# 題名", "", "## 節", "本文"), new: doc("題名", "====", "", "節", "----", "本文")},
		{name: "setext-level-change", old: doc("題名", "====", "本文"), new: doc("題名", "----", "本文")},
		{name: "setext-underline-removed", old: doc("題名", "====", "本文"), new: doc("題名", "本文")},
		{name: "setext-underline-after-list", old: doc("- 項目", "-", "本文"), new: doc("- 項目", "  -", "本文")},
		{name: "bold-to-heading", old: doc("**はじめに**", "本文"), new: doc("## はじめに", "本文")},
		{name: "body-to-heading", old: doc("はじめに", "本文"), new: doc("## はじめに", "本文")},
		{name: "heading-to-bold", old: doc("## はじめに", "本文"), new: doc("**はじめに**", "本文")},
		{name: "heading-to-body", old: doc("## はじめに", "本文"), new: doc("はじめに", "本文")},
		{name: "heading-to-body-colon", old: doc("## はじめに：", "本文"), new: doc("はじめに", "本文")},
		// 本家と同じ答えの最小の入力 (わざと壊すと記録の検査が緑のまま通った形)。
		{name: "shape-crlf-heading-text", old: "# 。", new: "# す\r"},
		{name: "shape-order-used-old", old: "# 境界値検証:edge_waru_32", new: "境界値検証:edge_waru_32\n# 入"},
		{name: "shape-setext-underline-space", old: "限\n=", new: "限\n ="},
		{name: "shape-bold-line-body-kept", old: "```\n```\n(1) 導入まとめログ期限。背景API保存！結果テスト。\n**まとめ**", new: "# **まとめ**\n```\n```\n(1)導入まとめログ期限。背景API保存！結果テスト。"},
		{name: "shape-body-line-strip", old: "# 使い方確認設定結果の背景\n  APIまとめ目的導入.# 境界値検証: edge_kowareru_39次のように責務を分けることを提案します。", new: "# 背景背\n 一覧ssue/ status /doctor) の配線と半ページの移動量 (`linav.Half` は結果概要まとめ背景\u200b画面！\n#"},
		{name: "bold-two-spans", old: doc("**a** と **b**", "本文"), new: doc("## a と b", "本文")},
		{name: "quote-heading", old: doc("> ## 引用の見出し", "> 本文"), new: doc("> ### 引用の見出し", "> 本文")},
		{name: "removed-3", old: doc("# 全体", "## 消す一", "## 消す二", "## 消す三", "本文"), new: doc("# 全体", "本文")},
		{name: "removed-4", old: doc("# 全体", "## 消す一", "## 消す二", "## 消す三", "## 消す四", "本文"), new: doc("# 全体", "本文")},
		{name: "added-count", old: doc("# 全体", "本文"), new: doc("# 全体", "## 足す", "本文")},
		{name: "shape-sentence", old: doc("# 設定を直しました。", "本文"), new: doc("# 設定の修正", "本文")},
		{name: "shape-comma", old: doc("# 設定、確認", "本文"), new: doc("# 設定の確認", "本文")},
		{name: "shape-16-chars", old: doc("# "+kanjiRun(3, 16), "本文"), new: doc("# "+kanjiRun(4, 16), "本文")},
		{name: "shape-15-chars", old: doc("# "+kanjiRun(3, 15), "本文"), new: doc("# "+kanjiRun(4, 15), "本文")},
		{name: "shape-fullwidth-period", old: doc("# 設定を直した．", "本文"), new: doc("# 設定修正", "本文")},
		{name: "shape-wide-comma", old: doc("# 設定，確認", "本文"), new: doc("# 設定確認", "本文")},
		{name: "order-pair-body", old: doc("# 全体", "## 旧い節の題", "旧い節の本文は設定の手順です。", "## 別の節", "別の本文"), new: doc("# 全体", "## 新しい題", "旧い節の本文は設定の手順です。", "## 追加の節", "追加の本文")},
		// 見出しの似かたの下限 (0.1): 2 文字の組の共通が 1 つで、和集合が 10 (ちょうど 0.1) と 11 (0.0909)
		{name: "pair-min-exact", old: doc("# あい、うえお", "あ本文あ", "", "## 別の見出し", "別の本文"), new: doc("## かきうえくけこ", "い他文い", "", "## 別の題", "別の中身")},
		{name: "pair-min-below", old: doc("# あい、うえお", "あ本文あ"), new: doc("## かきうえくけこさ", "い他文い")},
		{name: "order-same-level", old: doc("# あい、うえお", "あ本文あ"), new: doc("# かきうえくけこさ", "い他文い")},
		{name: "ordinal-same", old: doc("# 全体", "", "## 旧い節", "", "## 旧い次の節"), new: doc("# 全体", "", "## 全く別の題", "", "## 全然違う題")},
		{name: "ratio-half", old: doc("# 全体", "## ab", "本文一", "", "## cd", "本文二"), new: doc("# 全体", "### ac", "本文三", "", "### ce", "本文四")},
		{name: "ratio-below-half", old: doc("# 全体", "## ab", "本文一", "## 前の", "本文二"), new: doc("# 全体", "### xy", "本文三", "### 別の", "本文四")},
		{name: "ordinal-differs", old: doc("# 前置き", "", "# 旧A", "", "# 旧B", "", "# 末尾"), new: doc("# 新A", "", "# 新B", "", "# 末尾")},
		// 見出しだけの hunk で、文書の中の何番目の見出しかが両側で違う (前にある別の hunk が見出しを 1 つ消した)。文字が似ていないので組にならない
		{name: "ordinal-differs-levels", old: doc("# 消す題", "同じ本文", "", "# 旧甲", "", "# 旧乙"), new: doc("同じ本文", "", "## 新丙", "", "## 新丁")},
		{name: "ordinal-same-levels", old: doc("# 同じ題", "同じ本文", "", "# 旧甲", "", "# 旧乙"), new: doc("# 同じ題", "同じ本文", "", "## 新丙", "", "## 新丁")},
		// 見出しだけの hunk のあとの空行を除いた次の行が、別の hunk で変わっている (節の本文を変えているので順には組にしない)
		{name: "heading-only-after-changed", old: doc("# 旧甲", "", "本文X"), new: doc("## 新乙", "", "本文Y")},
		{name: "heading-only-after-same", old: doc("# 旧甲", "", "本文X"), new: doc("## 新乙", "", "本文X")},
		{name: "no-change", old: doc("# 題", "本文"), new: doc("# 題", "本文")},
		{name: "empty-old", old: "", new: doc("# 題", "本文")},
		{name: "empty-new", old: doc("# 題", "本文"), new: ""},
		{name: "crlf", old: "# 題\r\n本文\r\n", new: "## 題\r\n本文\r\n"},
		{name: "keys-one-char", old: doc("# あ", "x", "# い", "y"), new: doc("## う", "x", "## え", "y")},
		{name: "keys-empty-body-both", old: doc("# 題一", "# 題二"), new: doc("## 題三", "## 題四")},
	}
	// 組み直す見出しの数の上限 (300): 片側 300 と 301 (hunk の片側が上限を超えたら組み直さない)
	sized := func(oldN, newN int) diffFullCase {
		return diffFullCase{
			name: fmt.Sprintf("pair-limit-%d-%d", oldN, newN),
			old:  doc(numbered(oldN, "# 手順の見出し%d")...),
			new:  doc(numbered(newN, "# 手順の項目%d")...),
		}
	}
	cases = append(cases, sized(300, 300), sized(301, 300), sized(300, 301))
	// 両側の数が違えば順には組にしないので、組み直す手順が上限の手前と向こうで通る
	unequal := func(oldN, newN int) diffFullCase {
		c := sized(oldN, newN)
		c.name = fmt.Sprintf("pair-limit-unequal-%d-%d", oldN, newN)
		return c
	}
	cases = append(cases, unequal(300, 299), unequal(301, 299), unequal(299, 301), unequal(299, 300))
	// 似かたに足す「段が同じ」の 0.2: 元の見出し 1 つに、後の 2 つの見出し (段が違うものと同じもの) の似かたの差を変えて、どちらが組になるか
	cases = append(cases,
		diffFullCase{name: "same-level-bonus-wins", old: doc("# あい、うえお"), new: doc("## あい、うき", "# あい、き")},
		diffFullCase{name: "same-level-bonus-loses", old: doc("# あい、うえお"), new: doc("## あい、うえ", "# あい、うえかき")},
		diffFullCase{name: "same-level-bonus-wins-reversed", old: doc("## あい、うえお"), new: doc("# あい、うき", "## あい、き")},
	)
	// 節の本文の字数の上限 (300): 本文 (一続きの 300 字) の 300 字目までと 301 字目までで、2 文字の組の重なりがちょうど 0.1 と 0.0997 になる
	oldBody, newBody := kanjiRun(7, 320), kanjiRun(7, 31)+"あ"
	cases = append(cases, diffFullCase{name: "body-300-boundary", old: doc("# あい、うえお", oldBody), new: doc("## かきくけこ", newBody)})
	return append(cases, diffHeadingFuzz()...)
}

// diffHeadingFuzz は固定の種から作った、見出しの多い文書の組 (見出しの書き換え・段の変更・太字や本文の行との入れ替え・setext・消す・足す)。
// 種を変えない限り同じ組になる。
func diffHeadingFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 5))
	words := []string{"設定", "手順", "確認", "概要", "結果", "注意", "方法", "目的", "背景", "まとめ", "例", "準備"}
	title := func() string {
		var sb strings.Builder
		for range 1 + r.IntN(3) {
			sb.WriteString(words[r.IntN(len(words))])
		}
		return sb.String()
	}
	type line struct {
		kind  string // atx・bold・body・setext・text・blank・quote
		level int
		text  string
	}
	render := func(ls []line) string {
		var out []string
		for _, l := range ls {
			switch l.kind {
			case "atx":
				out = append(out, strings.Repeat("#", l.level)+" "+l.text)
			case "bold":
				out = append(out, "**"+l.text+"**")
			case "setext":
				mark := "-"
				if l.level == 1 {
					mark = "="
				}
				out = append(out, l.text, strings.Repeat(mark, 3))
			case "quote":
				out = append(out, "> "+l.text)
			case "blank":
				out = append(out, "")
			default:
				out = append(out, l.text)
			}
		}
		return strings.Join(out, "\n") + "\n"
	}
	var cases []diffFullCase
	for n := range 120 {
		var old []line
		for range 2 + r.IntN(9) {
			switch r.IntN(7) {
			case 0:
				old = append(old, line{"bold", 0, title()})
			case 1:
				old = append(old, line{"setext", 1 + r.IntN(2), title()})
			case 2:
				old = append(old, line{"quote", 0, title()})
			default:
				old = append(old, line{"atx", 1 + r.IntN(4), title()})
			}
			for range r.IntN(3) {
				old = append(old, line{"text", 0, title() + "の本文です。"})
			}
			if r.IntN(3) == 0 {
				old = append(old, line{"blank", 0, ""})
			}
		}
		cur := append([]line{}, old...)
		for ops := 1 + r.IntN(5); ops > 0 && len(cur) > 1; ops-- {
			i := r.IntN(len(cur))
			switch r.IntN(7) {
			case 0:
				cur = append(cur[:i], cur[i+1:]...)
			case 1:
				cur = append(cur[:i], append([]line{{"atx", 1 + r.IntN(4), title()}}, cur[i:]...)...)
			case 2:
				cur[i].text = title()
			case 3:
				cur[i].level = 1 + r.IntN(4)
			case 4:
				kinds := []string{"atx", "bold", "text", "setext"}
				cur[i].kind = kinds[r.IntN(len(kinds))]
				if cur[i].level == 0 {
					cur[i].level = 1 + r.IntN(2)
				}
			case 5:
				cur[i].text += "の本文です。"
			default:
				j := r.IntN(len(cur))
				cur[i], cur[j] = cur[j], cur[i]
			}
		}
		cases = append(cases, diffFullCase{name: fmt.Sprintf("fuzz-%03d", n), old: render(old), new: render(cur)})
	}
	return cases
}
