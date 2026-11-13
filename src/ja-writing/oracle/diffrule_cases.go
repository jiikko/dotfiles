package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 G (9 つのキーの組み立て) の記録: 段 _rule_candidates(orig_raw, rw_raw, stance)。Markdown のままの元と書き直した文章を渡す。

// diffRuleConstants は群 G の Go の表と比べる本家の定数。
var diffRuleConstants = []string{"MARKERS", "_MARKER_REGEXES", "CONTENT"}

func diffRuleConstantCalls() []Call {
	calls := make([]Call, len(diffRuleConstants))
	for i, name := range diffRuleConstants {
		calls[i] = Call{Module: DiffModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	return calls
}

// diffRuleStances は _rule_candidates に渡す立場 (nil は指定なし)。
var diffRuleStances = []any{nil, "勧め", "決まり", "説明"}

// diffRuleCalls は 1 組に _rule_candidates を通す呼び出し。withStances が偽なら立場なしだけ。
func diffRuleCalls(old, new string, withStances bool) []Call {
	stances := diffRuleStances[:1]
	if withStances {
		stances = diffRuleStances
	}
	calls := make([]Call, len(stances))
	for i, st := range stances {
		calls[i] = dcall("_rule_candidates", old, new, st)
		calls[i].Keep = len(old)+len(new) < 500
	}
	return calls
}

// diffRuleCases は群 G の手書きの組と固定の種の組。言い回しの種類の数の増減 (数が同じなら出ない)・元にない語と消えた語 (漢字・片仮名・英単語。
// 元の文章のどこかに部分文字列としてあれば新しい語にしない)・構造 (箇条書き・段落・文の数)・足した部分 (印の近く 3 字・新しい語) を狙う。
func diffRuleCases() []diffFullCase {
	doc := diffDoc
	cases := []diffFullCase{
		{name: "same", old: doc("確認してください。"), new: doc("確認してください。")},
		{name: "empty", old: "", new: ""},
		{name: "marker-count", old: doc("確認してください。また次に、ましょう。"), new: doc("確認してください。さらに確認してください。")},
		{name: "marker-same-count", old: doc("まず確認します。"), new: doc("次に確認します。")},
		{name: "marker-all", old: doc("てください。ましょう。べき。大切。でき。でしょう。のです。にします。たら。ことです。まず。"), new: doc("")},
		{name: "marker-conditions", old: doc("例えば、たとえば、場合は、なら。"), new: doc("書けば、見れば、行けば、場合、場合。")},
		{name: "marker-lookbehind", old: doc("したられます。作れます。"), new: doc("しられます。使えます。")},
		{name: "new-words-kanji", old: doc("設定を開きます。"), new: doc("設定画面を開き、手順を確認します。")},
		{name: "new-words-substring", old: doc("設定画面を開きます。"), new: doc("設定を開き、画面を確認します。")},
		{name: "new-words-katakana", old: doc("ファイルを開きます。"), new: doc("ファイルとフォルダーを開きます。")},
		{name: "new-words-english", old: doc("Gitを使います。"), new: doc("Git と GitHub を使います。a.b と c#d も。")},
		{name: "new-words-single-char", old: doc("あ"), new: doc("A い 漢 カ")},
		{name: "new-words-kanji-marks", old: doc("々〆ヵヶ"), new: doc("設々〆ヵヶ")},
		{name: "lost-words", old: doc("設定画面と手順とファイルを確認します。"), new: doc("設定を確認します。")},
		{name: "lost-words-substring", old: doc("設定画面を確認します。"), new: doc("設定画面が確認できます。画面は残ります。")},
		{name: "structure-list-to-prose", old: doc("- 確認する", "- 試す"), new: doc("確認して試す。")},
		{name: "structure-numbered", old: doc("1. 確認する", "2) 試す"), new: doc("確認する。試す。")},
		{name: "structure-list-kept", old: doc("- 確認する"), new: doc("- 確認する", "- 試す")},
		{name: "structure-paragraphs", old: doc("確認する。", "", "試す。", "", "読む。"), new: doc("確認する。試す。読む。")},
		{name: "structure-paragraphs-list", old: doc("- 確認する", "- 試す", "", "読む。"), new: doc("確認する。", "試す。")},
		{name: "structure-sentences", old: doc("確認する。試す。"), new: doc("確認する。試す。読む。書く。")},
		{name: "structure-sentences-fewer", old: doc("確認する。試す。読む。"), new: doc("確認して試して読む。")},
		{name: "spans-kind-near", old: doc("確認します。"), new: doc("確認してくださいます。")},
		{name: "spans-kind-around-3", old: doc("あいう確認します。"), new: doc("あいうでき確認します。")},
		{name: "spans-replace", old: doc("設定を確認します。"), new: doc("設定を確認できます。")},
		{name: "spans-delete-only", old: doc("設定を確認してください。"), new: doc("設定を確認。")},
		{name: "spans-context-edge", old: doc("あ"), new: doc("あ" + strings.Repeat("い", 30) + "ください。")},
		{name: "spans-context-head", old: doc("あ"), new: doc("ください。あ")},
		{name: "spans-new-word", old: doc("設定"), new: doc("設定画面")},
		{name: "logic-points", old: doc("設定する。"), new: doc("ただし、設定する。これは重要です。この設定も、必要です。以下の点です。")},
		{name: "bold-broken", old: doc("**確認**します。"), new: doc("**「確認」**します。**注意。**を読みます。")},
		{name: "bold-fixed", old: doc("**「確認」**します。"), new: doc("**確認**します。")},
		{name: "frontmatter", old: doc("---", "title: x", "---", "確認する。"), new: doc("---", "title: y", "---", "確認します。")},
		{name: "bold-in-frontmatter", old: doc("---", "title: **x**", "---", "確認する。"), new: doc("---", "title: **「x」**です", "---", "確認する。**「確認」**。")},
		{name: "stance-advise", old: doc("手順を実行します。読んでください。"), new: doc("手順を実行します。読みましょう。重要です。")},
		{name: "stance-mixed-register", old: doc("これは設計です。確認する。"), new: doc("これは設計だ。確認します。試す。")},
		{name: "endings-changed", old: doc("設定を確認します。"), new: doc("設定を確認してください。")},
	}
	return append(cases, diffRuleFuzz()...)
}

// diffRuleFuzz は固定の種 (PCG 1076/9) から作った、語・印・箇条書き・段落を混ぜた組。
func diffRuleFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 9))
	words := []string{"設定", "画面", "手順", "ファイル", "フォルダー", "Git", "GitHub", "確認", "結果", "資料", "a.b", "c#d", "々", "あ"}
	marks := []string{"してください", "ましょう", "べきです", "大切です", "できます", "でしょう", "のです", "にします", "たら、", "ことです", "まず、", "また、", "場合は", "必ず"}
	prefixes := []string{"", "", "", "- ", "1. ", "* ", "## ", "**"}
	sentence := func() string {
		p := randPick(r, prefixes)
		s := p + randPick(r, words) + "と" + randPick(r, words) + "を" + randPick(r, marks)
		if p == "**" {
			s += "**"
		}
		return s + "。"
	}
	var cases []diffFullCase
	for n := range 300 {
		var paragraphs [][]string
		for range 1 + r.IntN(4) {
			var lines []string
			for range 1 + r.IntN(3) {
				lines = append(lines, sentence())
			}
			paragraphs = append(paragraphs, lines)
		}
		cur := make([][]string, len(paragraphs))
		for i, p := range paragraphs {
			cur[i] = append([]string{}, p...)
		}
		for ops := 1 + r.IntN(4); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			if len(cur[i]) == 0 {
				continue
			}
			j := r.IntN(len(cur[i]))
			switch r.IntN(7) {
			case 0:
				cur[i][j] = sentence()
			case 1:
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 2:
				cur[i] = append(cur[i][:j], append([]string{sentence()}, cur[i][j:]...)...)
			case 3: // 箇条書きを地の文にする
				cur[i][j] = strings.TrimLeft(cur[i][j], "-*1. #")
			case 4: // 段落をまとめる
				if i+1 < len(cur) {
					cur[i] = append(cur[i], cur[i+1]...)
					cur = append(cur[:i+1], cur[i+2:]...)
				}
			case 5: // 語を替える
				cur[i][j] = strings.Replace(cur[i][j], randPick(r, words), randPick(r, words), 1)
			default: // 印を足す
				cur[i][j] = strings.Replace(cur[i][j], "。", randPick(r, marks)+"。", 1)
			}
		}
		cases = append(cases, diffFullCase{name: "rule-fuzz-" + strconv.Itoa(n), old: diffJoinGroups(paragraphs, "\n", "\n\n"), new: diffJoinGroups(cur, "\n", "\n\n")})
	}
	return cases
}
