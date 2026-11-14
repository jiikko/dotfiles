package oracle

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 P (問い・変えない箇所・印の候補) の記録: 段 _question_risks・_protected_risks・_marks_risks・_modality_risks・_logic_risks と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/20〜22) の組を足したもの。

var diffQuestionConstants = []string{"_QUESTION", "_NOT_QUESTION_KA", "_FINAL_PERIOD_IN_QUOTE", "_SENTENCE_END", "_PROTECTED_SPANS", "_PROTECTED_LINES"}

// DiffQuestionFunctions は群 P の関数。
var DiffQuestionFunctions = []string{"_question_risks", "_is_question", "_only_final_period_removed", "_protected_risks", "_protected_spans", "_find_line", "_marks_risks", "_modality_risks", "_logic_risks"}

// DiffQuestionConstantNames は群 P の定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffQuestionConstantNames() []string { return diffQuestionConstants }

// diffQuestionBank は問いの文末の言い方ごとの文 (問いになるもの・ならないもの)。
var diffQuestionBank = []string{
	"動きますか。", "動きますか？", "動きますか?", "動きますか", "動きますか。」", "「動きますか。」", "「動きますか？」", "『動きますか。』", "動きますか。』）", "動きますか。)", "動きますか。）", "動きますか?）", "動きますか？」』",
	"動きますか。」。", "動きますか。 ", "動きますか？\x1c", "動きますか。\t", "動きますか。\n", "動きますか！", "動きます。", "動きますよ?", "動きますよ？？", "動きます？。",
	"原因は明らか。", "原因は明らか。」", "原因は明らか。）", "それは確か。", "それは確かか。", "ほか。", "そのほか。", "ほか。」", "ほかか。", "わずか。", "僅か。", "静か。", "豊か。", "速やか。", "穏やか。", "鮮やか。", "細やか。", "爽やか。", "健やか。",
	"和やか。", "緩やか。", "賑やか。", "にぎやか。", "華やか。", "密か。", "愚か。", "疎か。", "おろか。", "遥か。", "はるか。", "ほのか。", "柔らか。", "滑らか。", "なめらか。", "清らか。", "朗らか。", "安らか。", "大まか。",
	"原因は何か。", "どれが明らかか。", "明らかか。", "静かか。", "穏やかでしょうか。", "誰が確かか?", "確か？", "確か?", "ほか？", "ほか?", "明らか？", "あるか。", "あるか。」", "あるか", "あるか。。", "あるか？。", "あるかも。",
	"本当か。", "本当かな。", "本当かどうか。", "これでいいか、確認する。", "動くか動かないか。", "動くかどうか確認します。", "なるほど、そうか。", "そうか。」", "そうか。）", "か。", "か？", "？", "?", "。", "",
}

// diffQuestionStems は問いと言い切りを作る文の頭。
var diffQuestionStems = []string{"設定を保存しました", "この変更は安全", "管理者が承認する", "原因は特定できている", "そのファイルを読んだ", "彼は見たの", "再起動が必要", "ログを確認する", "誰が決めた", "明らか"}

var diffQuestionEndings = []string{"。", "か。", "か？", "か?", "ですか。", "ですか？", "でしょうか。", "ですよね。", "のか。", "のか", "のか。」", "のか」", "のか？」", "ます。", "だ。", "か。」", "か。）", "か。)", "ですか", "？", "?"}

var diffQuestionQuoteForms = [][2]string{{"「", "」"}, {"『", "』"}, {"", ""}, {"（", "）"}, {"「「", "」」"}}

func diffQuestionHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("動きますか。"), new: doc("動きますか。")},
		{name: "to-question", old: doc("動きます。"), new: doc("動きますか。")},
		{name: "to-statement", old: doc("動きますか？"), new: doc("動きます。")},
		{name: "final-period-removed", old: doc("「見たのか。」"), new: doc("「見たのか」")},
		{name: "final-period-added", old: doc("「見たのか」"), new: doc("「見たのか。」")},
		{name: "final-period-removed-double", old: doc("『見たのか。』"), new: doc("『見たのか』")},
		{name: "final-period-removed-sentence", old: doc("彼は言った。「見たのか。」"), new: doc("彼は言った。「見たのか」")},
		{name: "final-period-middle", old: doc("「見たのか。そうか」"), new: doc("「見たのか　そうか」")},
		{name: "final-period-not-quote", old: doc("見たのか。"), new: doc("見たのか")},
		{name: "final-period-other-change", old: doc("「見たのか。」"), new: doc("「見たのかな」")},
		{name: "final-period-trailing-space", old: doc("「見たのか。」 "), new: doc("「見たのか」")},
		{name: "ka-adjective", old: doc("原因は明らか。"), new: doc("原因は明らかか。")},
		{name: "ka-adjective-both", old: doc("原因は明らか。"), new: doc("原因は静か。")},
		{name: "ka-hoka", old: doc("そのほか。"), new: doc("そのほかか。")},
		{name: "ka-close-bracket", old: doc("動きますか。）"), new: doc("動きます。）")},
		{name: "question-mark-ascii", old: doc("動きます?"), new: doc("動きます.")},
		{name: "multi-sentence", old: doc("設定を開きます。動きますか。"), new: doc("設定を開きますか。動きます。")},
		{name: "two-sentences-swap-kind", old: doc("動きますか。保存します。"), new: doc("動きます。保存しますか。")},
		{name: "list", old: doc("- 動きますか。", "- 保存します。"), new: doc("- 動きます。", "- 保存しますか。")},
		{name: "heading-question", old: doc("# 動きますか", "本文です。"), new: doc("# 動きます", "本文です。")},
		{name: "quote", old: doc("> 動きますか。"), new: doc("> 動きます。")},
		{name: "table", old: doc("| 項目 | 値 |", "|---|---|", "| 動きますか。 | 保存 |"), new: doc("| 項目 | 値 |", "|---|---|", "| 動きます。 | 保存 |")},
		{name: "multi-hunk", old: doc("動きますか。", "同じ行です。", "保存します。"), new: doc("動きます。", "同じ行です。", "保存しますか。")},
		{name: "run-shifted", old: doc("動きますか。", "", "保存します。", "", "確認しますか。"), new: doc("動きます。保存しますか。", "", "確認します。")},
		{name: "crlf", old: "動きますか。\r\n", new: "動きます。\r\n"},
		{name: "empty-old", old: "", new: doc("動きますか。")},
	}
}

// diffQuestionFuzz は固定の種 (PCG 1076/20) から作った、文末を問いと言い切りに入れ替える組 (台詞の最後の「。」を外す組を含む)。
func diffQuestionFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 20))
	var cases []diffFullCase
	for n := range 900 {
		stem := randPick(r, diffQuestionStems)
		q := diffQuestionQuoteForms[r.IntN(len(diffQuestionQuoteForms))]
		a, b := randPick(r, diffQuestionEndings), randPick(r, diffQuestionEndings)
		old, new := q[0]+stem+a+q[1], q[0]+stem+b+q[1]
		lines := []string{old}
		var rest []string
		if r.IntN(3) == 0 {
			rest = append(rest, "同じ行です。")
		}
		if r.IntN(4) == 0 {
			lines = append([]string{"彼は言った。"}, lines...)
			new = "彼は言った。" + new
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffDoc(append(lines, rest...)...), new: diffDoc(append([]string{new}, rest...)...)})
	}
	return cases
}

func diffQuestionStringCalls() []Call {
	var calls []Call
	add := keepAdder(&calls)
	for _, s := range diffQuestionBank {
		add("_is_question", s)
	}
	for _, p := range [][2]string{
		{"「見たのか。」", "「見たのか」"}, {"「見たのか」", "「見たのか。」"}, {"『見たのか。』", "『見たのか』"}, {"見たのか。", "見たのか"}, {"「見たのか。」", "「見たのか。」"}, {"「見たのか。」 ", "「見たのか」\n"},
		{"「見たのか。」」", "「見たのか」」"}, {"「見た。のか。」", "「見たのか」"}, {"「見た。のか。」", "「見た。のか」"}, {"「見たのか。」）", "「見たのか」）"}, {"", ""}, {"。」", "」"}, {"「あ。」「い。」", "「あ。」「い」"},
		{"「あ。」「い。」", "「あ」「い」"}, {"「あ」\x1c", "「あ。」"}, {"」", "。」"},
	} {
		add("_only_final_period_removed", p[0], p[1])
	}
	return calls
}

// diffProtectedHandPairs は変えない箇所の手書きの組 (名前, 元, 後)。インラインのコード・コードの囲み・先頭の設定・HTML・リンク先・URL・リンクの定義・画像・引用を狙う。
func diffProtectedHandPairs() []diffFullCase {
	doc := diffDoc
	code := func(lang string, body ...string) string {
		return "```" + lang + "\n" + strings.Join(body, "\n") + "\n```\n"
	}
	return []diffFullCase{
		{name: "same", old: doc("`a` を使う。"), new: doc("`a` を使う。")},
		{name: "inline-code", old: doc("`a` を使う。"), new: doc("`b` を使う。")},
		{name: "inline-code-outside", old: doc("`a` を使う。"), new: doc("`a` を使います。")},
		{name: "inline-code-added", old: doc("使う。"), new: doc("`a` を使う。")},
		{name: "inline-code-removed", old: doc("`a` を使う。"), new: doc("使う。")},
		{name: "inline-code-two", old: doc("`a` と `b` を使う。"), new: doc("`a` と `c` を使う。")},
		{name: "inline-code-one-line-one-to-one", old: doc("前。`a` 後。"), new: doc("前です。`a` 後です。")},
		{name: "inline-code-spaces", old: doc("`a b` を使う。"), new: doc("`a  b` を使う。")},
		{name: "fence", old: doc("前です。", "```sh", "make test", "```", "後です。"), new: doc("前です。", "```sh", "make lint", "```", "後です。")},
		{name: "fence-added", old: doc("前です。", "後です。"), new: doc("前です。", "```", "x", "```", "後です。")},
		{name: "fence-removed", old: doc("前です。", "```", "x", "```", "後です。"), new: doc("前です。", "後です。")},
		{name: "fence-lang", old: doc("```sh", "x", "```"), new: doc("```bash", "x", "```")},
		{name: "fence-unclosed", old: doc("前です。", "```", "x"), new: doc("前です。", "```", "y")},
		{name: "fence-tilde", old: doc("~~~", "x", "~~~"), new: doc("~~~", "y", "~~~")},
		{name: "fence-indented", old: doc("- 項目", "", "    code = 1"), new: doc("- 項目", "", "    code = 2")},
		{name: "fence-mixed", old: code("go", "a := 1", "b := 2") + "文です。\n", new: code("go", "a := 1", "b := 3") + "文でした。\n"},
		{name: "frontmatter", old: doc("---", "title: a", "---", "本文です。"), new: doc("---", "title: b", "---", "本文です。")},
		{name: "frontmatter-added", old: doc("本文です。"), new: doc("---", "title: b", "---", "本文です。")},
		{name: "frontmatter-removed", old: doc("---", "title: a", "---", "本文です。"), new: doc("本文です。")},
		{name: "frontmatter-body-only", old: doc("---", "title: a", "---", "本文です。"), new: doc("---", "title: a", "---", "本文でした。")},
		{name: "html-block", old: doc("<div>", "a", "</div>", "本文です。"), new: doc("<div>", "b", "</div>", "本文です。")},
		{name: "html-token", old: doc("改行<br>します。"), new: doc("改行<br/>します。")},
		{name: "html-token-added", old: doc("改行します。"), new: doc("改行<br>します。")},
		{name: "html-comment", old: doc("<!-- a -->", "本文です。"), new: doc("<!-- b -->", "本文です。")},
		{name: "link-destination", old: doc("[手順](https://a.example/x) を読む。"), new: doc("[手順](https://b.example/x) を読む。")},
		{name: "link-text-only", old: doc("[手順](https://a.example/x) を読む。"), new: doc("[手引き](https://a.example/x) を読む。")},
		{name: "link-added", old: doc("手順を読む。"), new: doc("[手順](https://a.example/x) を読む。")},
		{name: "link-title", old: doc(`[手順](https://a.example/x "a") を読む。`), new: doc(`[手順](https://a.example/x "b") を読む。`)},
		{name: "autolink", old: doc("<https://a.example/> を読む。"), new: doc("<https://b.example/> を読む。")},
		{name: "bare-url", old: doc("https://a.example/x を読む。"), new: doc("https://b.example/x を読む。")},
		{name: "bare-url-japanese-follow", old: doc("https://a.example/guideにあり、保存期間は3日です。"), new: doc("https://a.example/guideにあり、保存期間は5日です。")},
		{name: "bare-url-moved", old: doc("読む。https://a.example/x"), new: doc("https://a.example/x 読む。")},
		{name: "reference-definition", old: doc("[a]: https://a.example/", "本文です。"), new: doc("[a]: https://b.example/", "本文です。")},
		{name: "reference-definition-last", old: doc("本文です。", "", "[a]: https://a.example/"), new: doc("本文です。", "", "[a]: https://b.example/")},
		{name: "reference-definition-no-newline", old: "本文です。\n\n[a]: https://a.example/", new: "本文です。\n\n[a]: https://b.example/"},
		{name: "reference-definition-title", old: doc("[a]: https://a.example/ \"t\""), new: doc("[a]: https://a.example/ \"u\"")},
		{name: "image-path", old: doc("![図](a.png) です。"), new: doc("![図](b.png) です。")},
		{name: "image-alt-only", old: doc("![図](a.png) です。"), new: doc("![図1](a.png) です。")},
		{name: "image-added", old: doc("です。"), new: doc("![図](a.png) です。")},
		{name: "image-no-path", old: doc("![図]() です。"), new: doc("![図2]() です。")},
		{name: "image-reference", old: doc("![図][a] です。", "", "[a]: a.png"), new: doc("![図][a] です。", "", "[a]: b.png")},
		{name: "quote", old: doc("> 引用です。"), new: doc("> 引用でした。")},
		{name: "quote-added", old: doc("本文です。"), new: doc("本文です。", "> 引用です。")},
		{name: "quote-removed", old: doc("本文です。", "> 引用です。"), new: doc("本文です。")},
		{name: "quote-lazy", old: doc("> 引用です。", "続きです。"), new: doc("> 引用です。", "続きでした。")},
		{name: "quote-nested", old: doc("> > 入れ子です。"), new: doc("> > 入れ子でした。")},
		{name: "quote-with-code", old: doc("> `a` を使う。"), new: doc("> `b` を使う。")},
		{name: "quote-two-lines-replace", old: doc("> 一行目。", "> 二行目。"), new: doc("> 一行目です。", "> 二行目です。")},
		{name: "quote-list", old: doc("> - 項目です", "> 続き"), new: doc("> - 項目でした", "> 続きでした")},
		{name: "table-code", old: doc("| a | b |", "|---|---|", "| `x` | y |"), new: doc("| a | b |", "|---|---|", "| `z` | y |")},
		{name: "multi-kind-one-hunk", old: doc("`a` と [b](https://a.example/) と https://c.example/ を使う。"), new: doc("`x` と [b](https://z.example/) と https://y.example/ を使う。")},
		{name: "multi-hunk", old: doc("`a` を使う。", "同じ行です。", "[b](https://a.example/) を読む。"), new: doc("`x` を使う。", "同じ行です。", "[b](https://z.example/) を読む。")},
		{name: "multi-line-replace", old: doc("`a` を使う。", "`b` を使う。"), new: doc("`x` を使う。", "`y` を使う。")},
		{name: "multi-line-replace-same-code", old: doc("`a` を使う。", "`b` を使う。"), new: doc("`a` を使います。", "`b` を使います。")},
		{name: "replace-code-count-differs", old: doc("`a` を使う。", "`b` を使う。"), new: doc("`a` を使う。")},
		{name: "replace-quote-and-code", old: doc("> 引用です。", "`a` を使う。"), new: doc("> 引用でした。", "`b` を使う。")},
		{name: "whitespace-only-code", old: doc("`a` を使う。"), new: doc("`a`  を使う。")},
		{name: "empty-old", old: "", new: doc("```", "x", "```")},
		{name: "crlf-fence", old: "```\r\nx\r\n```\r\n", new: "```\r\ny\r\n```\r\n"},
		{name: "inline-code-double-backtick", old: doc("``a`b`` を使う。"), new: doc("``a`c`` を使う。")},
		{name: "heading-code", old: doc("# `a` の説明"), new: doc("# `b` の説明")},
		{name: "list-code", old: doc("- `a` を使う", "- `b` を使う"), new: doc("- `a` を使う", "- `c` を使う")},
	}
}

// diffProtectedBudget は文字の対応を省く大きさの置き換えの塊 (合計の上限を超える・2 つ目の塊が残りに入らない) の中に、変えない箇所がある組。
func diffProtectedBudget() []diffFullCase {
	// extra は 1 行の終わりに足す変えない箇所の書式 (%s は変えた側で letters[1]、変えない側で letters[0]。%d は行の番号)。every 行に 1 つだけ変える
	type extra struct {
		format  string
		letters [2]string
		every   int
	}
	build := func(first, n int, changed bool, e extra) string {
		var sb strings.Builder
		for i := range n {
			r := []rune(kanjiRun(first+i, 1900))
			if changed {
				r[10] = 'あ'
			}
			sb.WriteString(string(r) + "。")
			if e.format != "" {
				letter := e.letters[0]
				if changed && e.every > 0 && i%e.every == 0 {
					letter = e.letters[1]
				}
				fmt.Fprintf(&sb, e.format, letter, i)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}
	code := extra{"`%s%d`", [2]string{"x", "y"}, 2}
	link := extra{"[a](https://%s.example/%d)", [2]string{"a", "b"}, 3}
	none := extra{}
	pair := func(name string, first int, e extra) diffFullCase {
		return diffFullCase{name: name, old: build(first, 60, false, e), new: build(first, 60, true, e)}
	}
	// 最後の行だけに変えない箇所がある塊 (範囲の読む上限は最後の行の終わりまで)
	lastLine := func(changed bool, mark string) string {
		r := []rune(kanjiRun(660, 1900))
		if changed {
			r[10] = 'あ'
		}
		return string(r) + "。" + mark + "\n"
	}
	return []diffFullCase{
		{name: "too-large-last-line-code", old: build(600, 59, false, none) + lastLine(false, "`x`"), new: build(600, 59, true, none) + lastLine(true, "`y`")},
		{name: "too-large-last-line-link", old: build(600, 59, false, none) + lastLine(false, "[a](https://a.example/1)"), new: build(600, 59, true, none) + lastLine(true, "[a](https://b.example/1)")},
		pair("too-large-code", 300, code),
		pair("too-large-link", 400, link),
		{name: "too-large-quote-lines", old: "> " + build(500, 60, false, none), new: "> " + build(500, 60, true, none)},
		{name: "budget-second-code", old: build(100, 50, false, code) + "区切り\n" + build(200, 10, false, code), new: build(100, 50, true, code) + "区切り\n" + build(200, 10, true, code)},
		{name: "budget-second-one-sided", old: build(100, 50, false, none) + "区切り\n" + build(200, 10, false, code), new: build(100, 50, true, none) + "区切り\n" + build(200, 10, true, none)},
	}
}

// diffTemplate は書式の {name} を value に置き換える。
func diffTemplate(format, name, value string) string {
	return strings.ReplaceAll(format, "{"+name+"}", value)
}

// diffProtectedFuzz は固定の種 (PCG 1076/21) から作った、変えない箇所の部品を並べた文書の 1 つを書き換える組。
func diffProtectedFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 21))
	// 部品は {v} を値に置き換える書式
	pieces := []string{
		"`{v}` を使う。", "[手順](https://{v}.example/x) を読む。", "https://{v}.example/x を読む。", "<https://{v}.example/> を読む。", "![図]({v}.png) です。", "改行<br class=\"{v}\">します。",
		"> 引用は {v} です。", "[{v}]: https://x.example/{v}", "- 項目 {v} です", "```\n{v}\n```", "<div>\n{v}\n</div>", "本文 {v} です。", "| a | {v} |\n|---|---|\n| `{v}` | y |",
	}
	values := []string{"a", "b", "あ", "x1", "https", "  ", "a b"}
	var cases []diffFullCase
	for n := range 700 {
		count := 1 + r.IntN(4)
		kinds := make([]int, count)
		for i := range kinds {
			kinds[i] = r.IntN(len(pieces))
		}
		old, new := make([]string, count), make([]string, count)
		changeAt := r.IntN(count)
		for i, k := range kinds {
			v := values[r.IntN(len(values))]
			old[i], new[i] = diffTemplate(pieces[k], "v", v), diffTemplate(pieces[k], "v", v)
			if i == changeAt || r.IntN(5) == 0 {
				new[i] = diffTemplate(pieces[k], "v", values[r.IntN(len(values))])
			}
		}
		sep := []string{"\n", "\n\n"}[r.IntN(2)]
		oldDoc, newDoc := strings.Join(old, sep)+"\n", strings.Join(new, sep)+"\n"
		if r.IntN(5) == 0 {
			// 先頭の設定 (中身を変える組と変えない組)
			a, b := values[r.IntN(len(values))], values[r.IntN(len(values))]
			oldDoc, newDoc = "---\ntitle: "+a+"\n---\n"+oldDoc, "---\ntitle: "+b+"\n---\n"+newDoc
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: oldDoc, new: newDoc})
	}
	return cases
}

// diffFindLineDocs は _find_line に通す文書 (行ごとに正規化した文から探す)。
var diffFindLineDocs = []string{
	"# 見出しです\n\n- 項目です。確認します。\n- 次の項目です\n\n> 引用の文です。\n\n**太字**の文です。\n`code` を使います。\n\n| a | b |\n|---|---|\n| 表の文です。 | x |\n",
	"同じ文です。\n同じ文です。\n三行目の文です。保存します。\n\n\n五行目の文は二十字を超える長い文です。続きの文です。\n",
}

var diffFindLineTexts = []string{
	"", "見出しです", "項目です。確認します。", "確認します。", "次の項目です", "引用の文です。", "太字の文です。", "code を使います。", "表の文です。", "同じ文です。", "同じ文です。\n別の文です。", "三行目の文です。保存します。", "保存します。",
	"五行目の文は二十字を超える長い文です。続きの文です。", "五行目の文は二十字を超える長い", "五行目の文は二十字を超える長い文です。", "五行目の文は二十字を超える長い文ではない。", "ありません。", "。", "？", "!!", "　", " 見出しです ", "項目です！確認します。", "項目です」）。続き",
}

func diffFindLineCalls(text string) []Call {
	build := dcall("_build_full_diff", text, text, nil)
	build.Save, build.Quiet = "full", true
	calls := []Call{build}
	for _, side := range []int{0, 1} {
		for _, t := range diffFindLineTexts {
			c := dcall("_find_line", diffFullRef(1), side, t)
			c.Keep = true
			calls = append(calls, c)
		}
	}
	return calls
}

// diffProtectedStageCalls は組 1 つに _protected_risks と、両方の側の _protected_spans を通す呼び出し。
func diffProtectedStageCalls(old, new string, keep bool) []Call {
	build := dcall("_build_full_diff", old, new, nil)
	build.Save, build.Quiet = "full", true
	risks := dcall("_protected_risks", diffFullRef(1), diffFullRef(0))
	risks.Keep = keep
	calls := []Call{build, risks}
	for _, side := range []int{0, 1} {
		c := dcall("_protected_spans", diffFullRef(1), side)
		c.Keep = keep
		calls = append(calls, c)
	}
	return calls
}

// diffChangeStageCalls は組 1 つに、文の種類の印・許可・能力・選ぶ範囲と因果の変化を、本家の diff() と同じ順で作らせて (覚える)、_marks_risks・_modality_risks・_logic_risks に通す呼び出し。
func diffChangeStageCalls(old, new string, keep bool) []Call {
	save := func(c Call, name string) Call { c.Save, c.Quiet = name, true; return c }
	ops := dcall("_ExactSequenceMatcher", nil, diffRef("o"), diffRef("r"))
	ops.Kwargs = map[string]any{"autojunk": false}
	ops.Method = "get_opcodes"
	calls := []Call{
		save(dcall("_build_full_diff", old, new, nil), "full"),
		save(dcall("normalize", old), "o"), save(dcall("normalize", new), "r"),
		save(ops, "ops"),
		save(dcall("_type_changes", diffRef("o"), diffRef("r"), diffRef("ops")), "tc"),
		save(dcall("_modality_changes", diffRef("o"), diffRef("r")), "mc"),
		save(dcall("_logic_changes", diffRef("o"), diffRef("r")), "lc"),
	}
	for _, step := range [][2]string{{"_marks_risks", "tc"}, {"_modality_risks", "mc"}, {"_logic_risks", "lc"}} {
		c := dcall(step[0], diffFullRef(1), diffRef(step[1]))
		c.Keep = keep
		calls = append(calls, c)
	}
	return calls
}

// diffChangeFuzz は固定の種 (PCG 1076/22) から、文の種類の印・許可・能力・選ぶ範囲と因果の文を、記号の付いた行に入れて 1 つを入れ替えた組。
func diffChangeFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 22))
	var pool []string
	pool = append(pool, diffTypeBank...)
	pool = append(pool, diffModalityBank...)
	pool = append(pool, diffChoiceOne...)
	pool = append(pool, diffChoiceAll...)
	// 行の包み方は {s} を文に置き換える書式
	wrap := []string{"{s}", "- {s}", "## {s}", "> {s}", "**{s}**", "1. {s}", "| {s} | x |", "前の文です。{s}"}
	var cases []diffFullCase
	for n := range 500 {
		count := 2 + r.IntN(4)
		old, new := make([]string, count), make([]string, count)
		changeAt := r.IntN(count)
		for i := range old {
			s := randPick(r, pool)
			w := randPick(r, wrap)
			old[i], new[i] = diffTemplate(w, "s", s), diffTemplate(w, "s", s)
			if i == changeAt || r.IntN(6) == 0 {
				new[i] = diffTemplate(randPick(r, wrap), "s", randPick(r, pool))
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffDoc(old...), new: diffDoc(new...)})
	}
	return cases
}

// diffQuestionCases は群 P の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffQuestionCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "question/constants", Calls: diffConstantCalls(diffQuestionConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases,
			VisCase{Name: "question/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_question_risks", false)},
			VisCase{Name: "protected/" + b.name, Calls: diffProtectedStageCalls(b.old, b.new, false)},
			VisCase{Name: "change/" + b.name, Calls: diffChangeStageCalls(b.old, b.new, false)})
	}
	questionStage := func(o, n string, k bool) []Call { return diffRiskStageCalls(o, n, "_question_risks", k) }
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
		make   func(old, new string, keep bool) []Call
	}{
		{"question/hand/", diffQuestionHandPairs(), true, questionStage},
		{"question/fuzz/", diffQuestionFuzz(), false, questionStage},
		{"protected/hand/", diffProtectedHandPairs(), true, diffProtectedStageCalls},
		{"protected/budget/", diffProtectedBudget(), false, diffProtectedStageCalls},
		{"protected/fuzz/", diffProtectedFuzz(), false, diffProtectedStageCalls},
		{"change/hand/", diffChangeHandPairs(), true, diffChangeStageCalls},
		{"change/fuzz/", diffChangeFuzz(), false, diffChangeStageCalls},
	} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: g.make(f.old, f.new, g.keep)})
		}
	}
	for i, d := range diffFindLineDocs {
		cases = append(cases, VisCase{Name: "find-line/" + strconv.Itoa(i), Calls: diffFindLineCalls(d)})
	}
	return append(cases, VisCase{Name: "question/strings", Calls: diffQuestionStringCalls()})
}

// diffChangeHandPairs は印の候補の手書きの組。印が消える・増える文を、見出し・箇条書き・引用・太字・表の行に入れ、_find_line が行を探す枝 (20 字・文の終わり・複数行) を通す。
func diffChangeHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("雨が降るそうです。"), new: doc("雨が降るそうです。")},
		{name: "mark-lost", old: doc("雨が降るそうです。"), new: doc("雨が降ります。")},
		{name: "mark-added", old: doc("雨が降ります。"), new: doc("雨が降るらしい。")},
		{name: "mark-in-list", old: doc("- 設定を開きます。", "- 雨が降るそうです。"), new: doc("- 設定を開きます。", "- 雨が降ります。")},
		{name: "mark-in-heading", old: doc("# 雨が降るそうです", "本文です。"), new: doc("# 雨が降ります", "本文です。")},
		{name: "mark-in-quote", old: doc("> 雨が降るそうです。"), new: doc("> 雨が降ります。")},
		{name: "mark-in-bold", old: doc("**雨が降るそうです。**"), new: doc("**雨が降ります。**")},
		{name: "mark-in-table", old: doc("| a | b |", "|---|---|", "| 雨が降るそうです。 | x |"), new: doc("| a | b |", "|---|---|", "| 雨が降ります。 | x |")},
		{name: "mark-long-sentence", old: doc("とても長い前置きの文がここに入りますが、雨が降るそうです。"), new: doc("とても長い前置きの文がここに入りますが、雨が降ります。")},
		{name: "mark-two-lines-same-head", old: doc("同じ頭の文です。雨が降るそうです。", "同じ頭の文です。雨が降ります。"), new: doc("同じ頭の文です。雨が降ります。", "同じ頭の文です。雨が降ります。")},
		{name: "mark-sentence-moved", old: doc("雨が降るそうです。", "晴れます。"), new: doc("晴れます。", "雨が降ります。")},
		{name: "mark-sentences-merged", old: doc("雨が降るそうです。", "傘を持ちます。"), new: doc("雨が降ります。傘を持ちます。")},
		{name: "permission-lost", old: doc("設定を開きます。", "保存してもよい。", "終わります。"), new: doc("設定を開きます。", "保存します。", "終わります。")},
		{name: "permission-added-list", old: doc("- 保存します。"), new: doc("- 保存してもよい。")},
		{name: "ability-lost", old: doc("保存できる。"), new: doc("保存します。")},
		{name: "ability-lost-quote", old: doc("> 保存できる。"), new: doc("> 保存します。")},
		{name: "choice-one-to-all", old: doc("AまたはBを選ぶ。"), new: doc("AとBの両方を選ぶ。")},
		{name: "choice-all-to-one", old: doc("AとBの両方を選ぶ。"), new: doc("AまたはBを選ぶ。")},
		{name: "choice-heading", old: doc("## AまたはBを選ぶ"), new: doc("## AとBの両方を選ぶ")},
		{name: "causality", old: doc("設定が原因で止まる。"), new: doc("設定のため止まる。")},
		{name: "marks-and-modality", old: doc("雨が降るそうです。保存してもよい。"), new: doc("雨が降ります。保存します。")},
		{name: "marks-modality-choice", old: doc("雨が降るそうです。", "保存してもよい。", "AまたはBを選ぶ。"), new: doc("雨が降ります。", "保存します。", "AとBの両方を選ぶ。")},
		{name: "empty-old", old: "", new: doc("雨が降るそうです。")},
		{name: "empty-new", old: doc("雨が降るそうです。"), new: ""},
		{name: "crlf", old: "雨が降るそうです。\r\n", new: "雨が降ります。\r\n"},
		{name: "frontmatter-first", old: doc("---", "title: a", "---", "雨が降るそうです。"), new: doc("---", "title: a", "---", "雨が降ります。")},
		{name: "code-between", old: doc("雨が降るそうです。", "```", "x", "```", "終わり。"), new: doc("雨が降ります。", "```", "x", "```", "終わり。")},
	}
}
