package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 AA (勢い) の記録: tone(orig, rw, doc_type)・_tone_level・_pseudo_heading・_heading_chars・_mark_spans・_tone_excerpt・_tone_lines と定数。
// 組は群 Z と同じ入れ物 (diffBags) に、群 AA 専用の組 (数の分かれ目・本家のコメントの例文・固定の種の組) を足したもの。

// diffToneConstants は群 AA の Go の表と比べる本家の定数。
var diffToneConstants = []string{
	"TONE_INFO_DOC_TYPES", "TONE_TYPED_INFO_SUBKINDS", "TONE_TYPED_REVIEW_DOC_TYPES", "TONE_HEADING_RATIO", "_TONE_EXCERPT_WIDTH", "_INDIRECT_QUESTION",
	"_TONE_SKIP_KINDS", "_TONE_KINDS", "_EXCLAMATION", "_QUESTION_MARK", "_INTENSIFIER_EXCLUDED", "_ATX_MARKERS", "_MAYBE_EMOJI", "_ORDERED_ITEM_LINE",
	"_BOLD_ONLY_LINE", "_SENTENCE_END_MARKS", "_SPACES",
}

// DiffRaisingFunctions は、記録の中に例外になる呼び出しと成功する呼び出しの両方を持つ関数 (本家が投げる例外は仕様の一部で、型と文面も突き合わせる)。
// parse_list は書き方の誤りの ValueError、tone と _tone_excerpt は、印が共通の前の部分をまたいで推敲後の位置が負になるときの IndexError。
// _hedge_polarity の例外は、語の無い文で記録の手順が参照 (覚えた語の列の [0]) を解くときの IndexError で、本家の関数の中の例外ではない。Go のテストが参照を解く所で同じ型と文面を返すことを突き合わせるだけで、製品の hedgePolarity は検査していない。
var DiffRaisingFunctions = []string{"parse_list", "tone", "_tone_excerpt", "_hedge_polarity"}

// DiffToneFunctions は群 AA の関数。
var DiffToneFunctions = []string{"tone", "_tone_level", "_pseudo_heading", "_heading_chars", "_mark_spans", "_tone_excerpt", "_tone_lines", "_intensifier_pattern"}

// DiffToneConstantNames は群 AA の定数の名前。
func DiffToneConstantNames() []string { return diffToneConstants }

// diffToneDocTypes は tone に渡す文書の種類 (nil は指定なし。本家の DOC_TYPES の全部)。
var diffToneDocTypes = []any{nil, "pr", "article", "incident", "proposal", "minutes", "manual", "slides", "essay", "formal", "promo", "mail", "paper", "script"}

// diffToneStageCalls は組 1 つに tone と、段 (_tone_lines を両側・番号つき項目を見出しに数えるか数えないかの 4 通り) を通す呼び出し。
func diffToneStageCalls(old, new string, types []any, keep, lines bool) []Call {
	var calls []Call
	for _, t := range types {
		c := dcall("tone", old, new, t)
		c.Keep = keep
		calls = append(calls, c)
	}
	if !lines {
		return calls
	}
	load := dcall("_load_list", "intensifiers")
	load.Save, load.Quiet = "words", true
	pattern := dcall("_intensifier_pattern", map[string]any{"$ref": "words", "path": []any{0}})
	pattern.Save, pattern.Quiet = "ip", true
	build := dcall("_build_full_diff", old, new, nil)
	build.Save, build.Quiet = "full", true
	calls = append(calls, load, pattern, build)
	sides := map[string]any{"$ref": "full", "path": []any{1}}
	for side := range 2 {
		for _, pseudo := range []bool{false, true} {
			c := dcall("_tone_lines", sides, side, diffRef("ip"), pseudo)
			c.Keep = keep
			calls = append(calls, c)
		}
	}
	return calls
}

// diffToneHeadingGrid は見出しの字数の分かれ目 (新しい字数が元の 7 割ちょうどの手前と向こう) を通す組。
func diffToneHeadingGrid() []diffFullCase {
	var cases []diffFullCase
	for _, old := range []int{3, 5, 10, 14, 20, 30, 50, 100} {
		edge := old * 7 / 10
		for _, n := range []int{edge - 1, edge, edge + 1, edge + 2} {
			if n < 0 {
				continue
			}
			cases = append(cases, diffFullCase{name: "tone-heading-" + strconv.Itoa(old) + "-" + strconv.Itoa(n),
				old: "# " + strings.Repeat("あ", old) + "\n\n本文です。\n", new: "# " + strings.Repeat("あ", n) + "\n\n本文です。\n"})
		}
	}
	return cases
}

// diffToneCountGrid は印 (「！」「？」・強める語) の数の増減 (同じ・1 つ減・1 つ増・全部消す) を通す組。
func diffToneCountGrid() []diffFullCase {
	var cases []diffFullCase
	for _, mark := range []string{"！", "!", "？", "?", "とても", "本当に", "最強", "絶対値", "絶対にやる", "😀", "✨"} {
		for _, o := range []int{0, 1, 2, 3} {
			for _, n := range []int{0, 1, 2, 3} {
				if o == n && o != 1 {
					continue
				}
				line := func(k int) string { return "今日は" + strings.Repeat(mark, k) + "晴れです。\n" }
				cases = append(cases, diffFullCase{name: "tone-count-" + strconv.Itoa(len(cases)), old: line(o), new: line(n)})
			}
		}
	}
	return cases
}

// diffToneHandCases は本家のコメントの例文と、候補の枝・level・コードや表を数えない・伏せる範囲・間接の問い・組になる行の選び方・長い行の抜き出し・番号つき項目と太字だけの行を通す手書きの組。
func diffToneHandCases() []diffFullCase {
	doc := diffDoc
	long := strings.Repeat("あ", 30)
	return []diffFullCase{
		{name: "tone-same", old: doc("すごい！"), new: doc("すごい！")},
		{name: "tone-empty", old: "", new: ""},
		{name: "tone-empty-new", old: doc("すごい！？"), new: ""},
		{name: "tone-exclamation", old: doc("すごいです！ぜひ見てください！"), new: doc("すごいです。ぜひ見てください。")},
		{name: "tone-question", old: doc("予算は足りますか？"), new: doc("予算は足ります。")},
		{name: "tone-question-indirect", old: doc("予算は足りるのか？"), new: doc("予算が足りるかとの質問があった。")},
		{name: "tone-question-indirect-2", old: doc("進めてよいか？"), new: doc("進めてよいかどうか確認した。")},
		{name: "tone-question-indirect-not", old: doc("何かあるか？"), new: doc("何かと言われた。")},
		{name: "tone-question-indirect-thought", old: doc("足りるか？"), new: doc("足りるかと思います。")},
		{name: "tone-question-indirect-ask", old: doc("直せるか？"), new: doc("直せるかとお問い合わせがあった。")},
		{name: "tone-question-indirect-far", old: doc("直せるか？"), new: doc("直せるかと、とても長い前置きで質問された。")},
		{name: "tone-question-code", old: doc("```", "なぜ？", "```", "本文です。"), new: doc("```", "なぜ。", "```", "本文です。")},
		{name: "tone-question-table", old: doc("| a |", "|---|", "| なぜ？ |"), new: doc("| a |", "|---|", "| なぜ。 |")},
		{name: "tone-question-inline", old: doc("`なぜ？` と書く。"), new: doc("`なぜ。` と書く。")},
		{name: "tone-question-url", old: doc("https://example.com/?q=1 を見る。"), new: doc("https://example.com/ を見る。")},
		{name: "tone-emoji", old: doc("すごい😀です✨"), new: doc("すごいです")},
		{name: "tone-emoji-multi", old: doc("❤\ufe0f👍🏽🇯🇵1\ufe0f\u20e3👨\u200d👩\u200d👧☺✌"), new: doc("")},
		{name: "tone-emoji-heading", old: doc("# 🎉お知らせ", "本文。"), new: doc("# お知らせ", "本文。")},
		{name: "tone-intensifier", old: doc("本当にとても最強です。ぜひ絶対に使って。"), new: doc("使えます。")},
		{name: "tone-intensifier-dup", old: doc("とてもとてもすごくすごく良い。"), new: doc("とてもすごく良い。")},
		{name: "tone-intensifier-excluded", old: doc("絶対値と絶対パスと絶対的な話。"), new: doc("値とパスと話。")},
		{name: "tone-intensifier-order", old: doc("パンチずらりイチオシ大活躍抜群最強。"), new: doc("")},
		{name: "tone-intensifier-tie", old: doc("ぜひ。絶対。", "ぜひ。絶対。"), new: doc("ぜひ。")},
		{name: "tone-heading", old: doc("# 昭和から愛される老舗の味", "本文。"), new: doc("# 老舗の味", "本文。")},
		{name: "tone-heading-setext", old: doc("昭和から愛される老舗の味", "=====", "本文。"), new: doc("老舗", "====", "本文。")},
		{name: "tone-heading-closing", old: doc("## 昭和から愛される老舗の味 ##", "本文。"), new: doc("## 老舗の味 ##", "本文。")},
		{name: "tone-heading-levels", old: doc("###### 昭和から愛される老舗の味"), new: doc("####### 昭和から愛される老舗の味")},
		{name: "tone-heading-indent", old: doc("   # 昭和から愛される老舗の味"), new: doc("    # 昭和から愛される老舗の味")},
		{name: "tone-heading-removed", old: doc("# 昭和から愛される老舗の味", "本文。"), new: doc("本文。")},
		{name: "tone-heading-pseudo-list", old: doc("1. 昭和から愛される老舗の味", "2) 創業百年の技", "本文。"), new: doc("1. 老舗", "2) 技", "本文。")},
		{name: "tone-heading-pseudo-bold", old: doc("**昭和から愛される老舗の味**", "", "__創業百年の技__", "本文。"), new: doc("**老舗**", "", "__技__", "本文。")},
		{name: "tone-heading-pseudo-both", old: doc("1. **昭和から愛される老舗の味**", "本文。"), new: doc("1. **老舗**", "本文。")},
		{name: "tone-heading-pseudo-not", old: doc("**昭和から愛される老舗の味** です。", "- **創業百年の技**", "本文。"), new: doc("**老舗** です。", "- **技**", "本文。")},
		{name: "tone-heading-pseudo-space", old: doc("** 昭和から愛される老舗の味 **", "1. **昭和 から**", "本文。"), new: doc("** 老舗 **", "1. **昭**", "本文。")},
		{name: "tone-heading-space-count", old: doc("# あ い\u3000う\ufeffえ"), new: doc("# あ")},
		{name: "tone-places-many", old: doc("すごい！", "", "すごい！", "", "すごい！", "", "すごい！", "", "すごい！", "", "すごい！", "", "すごい！"), new: doc("すごい。", "", "すごい。", "", "すごい。", "", "すごい。", "", "すごい。", "", "すごい。", "", "すごい。")},
		{name: "tone-places-partner", old: doc("あ！", "い！", "う！"), new: doc("あ。")},
		{name: "tone-places-partner-more", old: doc("あ！", "い！"), new: doc("あ！！", "い。", "う。")},
		{name: "tone-places-partner-lost", old: doc("あ。", "い！", "う！"), new: doc("あ。", "い。")},
		{name: "tone-places-fewer-news", old: doc("あ！", "い。", "う！！"), new: doc("あ！", "い。")},
		{name: "tone-places-no-partner", old: doc("あ！", "い！"), new: doc("")},
		{name: "tone-places-skip-same", old: doc("あ！！", "い！！"), new: doc("あ！！", "い。")},
		{name: "tone-excerpt-long", old: doc(long + "すごい！" + long), new: doc(long + "すごい。" + long)},
		{name: "tone-excerpt-sentences", old: doc("前の文です。" + long + "すごい！次の文です。" + long), new: doc("前の文です。" + long + "すごい。次の文です。" + long)},
		{name: "tone-excerpt-deleted-tail", old: doc(long + "すごい！" + long + "です。"), new: doc(long)},
		{name: "tone-excerpt-appended", old: doc(long + "。" + long), new: doc(long + "！")},
		{name: "tone-excerpt-emoji", old: doc(long + "😀" + long + "😀" + long), new: doc(long + "😀" + long + long)},
		{name: "tone-excerpt-intensifier", old: doc(long + "とても" + long + "とても" + long), new: doc(long + "とても" + long + long)},
		{name: "tone-excerpt-index-error", old: doc("とても"), new: doc("とてつもなく" + strings.Repeat("あ", 60))},
		{name: "tone-excerpt-index-error-2", old: doc("とても", "別の行です。"), new: doc("とてつもなく"+strings.Repeat("あ", 60), "別の行です。")},
		{name: "tone-excerpt-negative-wrap", old: doc("とても" + strings.Repeat("あ", 45)), new: doc("とてつもなく" + strings.Repeat("い", 90))},
		{name: "tone-excerpt-negative-wrap-2", old: doc("とても" + strings.Repeat("あ", 5)), new: doc("とてつもなく" + strings.Repeat("い", 50))},
		{name: "tone-excerpt-negative-flag", old: doc("🇯🇵"), new: doc("🇯" + strings.Repeat("あ", 60))},
		{name: "tone-excerpt-long-new-only", old: doc("あ！"), new: doc(long + long + "。")},
		{name: "tone-crlf", old: "すごい！\r\n", new: "すごい。\r\n"},
		{name: "tone-frontmatter", old: doc("---", "title: すごい！", "---", "本文。"), new: doc("---", "title: すごい。", "---", "本文。")},
		{name: "tone-html", old: doc("<p>すごい！</p>"), new: doc("<p>すごい。</p>")},
		{name: "tone-reference", old: doc("[a]: https://x/! \"すごい！\"", "本文。"), new: doc("[a]: https://x/ \"すごい。\"", "本文。")},
		{name: "tone-thematic", old: doc("---", "本文！"), new: doc("---", "本文。")},
		{name: "tone-list-bullet", old: doc("- すごい！", "- 最強！"), new: doc("- すごい", "- 最強")},
		{name: "tone-moved", old: doc("すごい！", "", "ふつうです。"), new: doc("ふつうです。", "", "すごい。")},
		// 壊した形で緑のまま通った所 (4 本目の次の run): 抜き出しにかける長さの境 (消えた行だけ・組の行の長い方)・印が変わった所の直後から始まるときの選び方・
		// 変わった所が印の直前の挿入だけのときの推敲後の位置
		{name: "tone-found-len-old-39", old: doc(toneLine40(39), "本文です。"), new: doc("本文です。")},
		{name: "tone-found-len-old-40", old: doc(toneLine40(40), "本文です。"), new: doc("本文です。")},
		{name: "tone-found-len-old-41", old: doc(toneLine40(41), "本文です。"), new: doc("本文です。")},
		{name: "tone-found-len-new-39", old: doc("すごい！"), new: doc("すごい。" + strings.Repeat("あ", 35))},
		{name: "tone-found-len-new-40", old: doc("すごい！"), new: doc("すごい。" + strings.Repeat("あ", 36))},
		{name: "tone-found-len-new-41", old: doc("すごい！"), new: doc("すごい。" + strings.Repeat("あ", 37))},
		{name: "tone-found-overlap-boundary", old: doc("前！"+strings.Repeat("あ", 30)+"x！"+strings.Repeat("い", 30), "別の行！"), new: doc("前！" + strings.Repeat("あ", 30) + "y！" + strings.Repeat("い", 30))},
		{name: "tone-found-branch2-insert", old: doc(strings.Repeat("あ", 30)+"！"+strings.Repeat("い", 30), "別の行！"), new: doc(strings.Repeat("あ", 30) + "追加！" + strings.Repeat("い", 30))},
	}
}

// toneLine40 は「！」を 1 つ持つ n 字の行 (先頭の 15 字・「。」・「すごい！」・い)。
func toneLine40(n int) string {
	return strings.Repeat("あ", 15) + "。" + "すごい！" + strings.Repeat("い", n-20)
}

// diffToneFuzz は固定の種 (PCG 1076/13) から作った、印を足す・消す・文を入れ替える組。
func diffToneFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 13))
	pool := []string{"すごい！", "ぜひ使ってください！", "本当にとても便利です。", "なぜですか？", "どうして?", "予算は足りるのか？", "予算が足りるかとの質問があった。", "絶対値を見る。", "最強です✨", "使ってみて😀",
		"今日は晴れです。", "続きは次の通りです。", "ずらりと並んでいます。", "パンチがある。", "大活躍でした!", "よろしくお願いします。", "# 見出しです", "## 昭和から愛される老舗", "1. 創業百年の技", "**注目の一品**",
		"```", "| a | b |", "- 項目です！", "> 引用です？", "`コード！` です。", long30() + "とても" + long30()}
	var cases []diffFullCase
	for n := range 900 {
		var lines []string
		for range 1 + r.IntN(7) {
			lines = append(lines, randPick(r, pool))
		}
		cur := append([]string{}, lines...)
		for ops := 1 + r.IntN(3); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			switch r.IntN(5) {
			case 0:
				cur[i] = strings.NewReplacer("！", "。", "!", ".", "？", "。", "?", ".", "とても", "", "本当に", "").Replace(cur[i])
			case 1:
				cur = append(cur[:i], cur[i+1:]...)
			case 2:
				cur = append(cur[:i], append([]string{randPick(r, pool)}, cur[i:]...)...)
			case 3:
				cur[i] = randPick(r, pool)
			default:
				k := r.IntN(len(cur))
				cur[i], cur[k] = cur[k], cur[i]
			}
		}
		join := func(list []string) string {
			var b strings.Builder
			for _, s := range list {
				b.WriteString(s)
				if r.IntN(3) == 0 {
					b.WriteString("\n")
				}
				b.WriteString("\n")
			}
			return b.String()
		}
		cases = append(cases, diffFullCase{name: "tone-fuzz-" + strconv.Itoa(n), old: join(lines), new: join(cur)})
	}
	return cases
}

func long30() string { return strings.Repeat("あ", 30) }

// diffToneStrings は _heading_chars・_mark_spans・_pseudo_heading に通す手書きの文。
func diffToneStrings() []string {
	return []string{"", " ", "あ", "！？!?", "すごい！すごい!", "なぜ？なぜ?", "とてもとても本当に", "絶対値と絶対的と絶対にやる", "😀", "😀✨😀", "あ😀い😀う", "❤\ufe0f👍🏽🇯🇵1\ufe0f\u20e3", "👨\u200d👩\u200d👧", "☺✌©™",
		"あ い\u3000う\ufeffえ\t\n\r\x0b\x0c\x1c\x85\u2028", "\u2000⿿ꀀ\ufeff\ufff0\U0010ffff", "\u1fff\u3000⿿", "# 見出し #", "1. **強調**", "**強調**", "__強調__", "** 強調 **", "**強調** です", "1. 項目", "12. 項目", "1234567890. 項目", "1.項目", "- 項目",
		"   1. 項目", "    1. 項目", "\t1. 項目", "   **強調**", "    **強調**", "1. **a**b**", "1. __a__ ", "**a**\n", "1. 項目\n", "**", "****", "** a**", "**a **", "1) **a**", "9. __a__"}
}

// diffToneCases は群 AA の case。
func diffToneCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "tone/constants", Calls: diffConstantCalls(diffToneConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "tone/" + b.name, Calls: diffToneStageCalls(b.old, b.new, diffToneDocTypes[:1], false, false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
	}{{"tone/hand/", diffToneHandCases()}, {"tone/heading/", diffToneHeadingGrid()}, {"tone/count/", diffToneCountGrid()}, {"tone/fuzz/", diffToneFuzz()}} {
		for _, f := range g.cases {
			types, keep := diffToneDocTypes[:1], false
			if g.prefix == "tone/hand/" {
				types, keep = diffToneDocTypes, true
			}
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffToneStageCalls(f.old, f.new, types, keep, g.prefix != "tone/fuzz/")})
		}
	}
	load := dcall("_load_list", "intensifiers")
	load.Save, load.Quiet = "words", true
	pattern := dcall("_intensifier_pattern", map[string]any{"$ref": "words", "path": []any{0}})
	pattern.Save, pattern.Quiet = "ip", true
	calls := []Call{load, pattern}
	stringCalls := func(s string) {
		h := dcall("_heading_chars", s)
		h.Keep = true
		calls = append(calls, h)
		for _, kind := range []string{"list", "paragraph", "heading", "table", ""} {
			c := dcall("_pseudo_heading", s, kind)
			c.Keep = true
			calls = append(calls, c)
		}
		for _, kind := range []string{"exclamation", "question", "emoji", "intensifier", "heading_chars", "other"} {
			c := dcall("_mark_spans", s, kind, diffRef("ip"))
			c.Keep = true
			calls = append(calls, c)
		}
	}
	for _, s := range diffToneStrings() {
		stringCalls(s)
	}
	for _, s := range []string{"x", "！", "?"} {
		calls = append(calls, dcall("_mark_spans", s, "intensifier", nil))
	}
	for _, doc := range diffToneDocTypes {
		for _, sub := range []any{nil, "exclamation", "question", "emoji", "intensifier", "heading", "other"} {
			c := dcall("_tone_level", doc, sub)
			c.Keep = true
			calls = append(calls, c)
		}
	}
	// 強める語を外す語 (絶対値・絶対パス・絶対的) の後ろに強める語が続く文。置き換えた長さがずれると、後ろの語の位置が変わる。既存の呼び出しの後ろに足す
	for _, s := range []string{"絶対値とても", "絶対パスとても", "絶対的にとても本当に"} {
		stringCalls(s)
	}
	cases = append(cases, VisCase{Name: "tone/strings", Calls: calls})
	var excerpts []Call
	for _, p := range diffToneExcerptCases() {
		c := dcall("_tone_excerpt", p.old, p.new, p.spans)
		c.Keep = true
		excerpts = append(excerpts, c)
		if p.width > 0 {
			w := dcall("_tone_excerpt", p.old, p.new, p.spans, p.width)
			w.Keep = true
			excerpts = append(excerpts, w)
		}
	}
	cases = append(cases, VisCase{Name: "tone/excerpt", Calls: excerpts})
	// 壊した形で緑のまま通った所を、直接の呼び出しで通す (_tone_excerpt の印の選び方・推敲後の位置・文の終わりのあとの空白だけの部分と、絵文字を重ねた見出しの字数。
	// 絵文字の字数は、異体字セレクタの付く字と付かない字が並ぶ「❤❤️」の形で、最初の 1 つだけ外すかすべて外すかが分かれる)
	var found []Call
	for _, p := range diffToneFoundExcerpts() {
		c := dcall("_tone_excerpt", p.old, p.new, p.spans)
		c.Keep = true
		found = append(found, c)
	}
	for _, s := range []string{"👍👍🏽", "👍🏽👍", "👍👍🏽👍", "😀😀", "👍👍🏽 見出し", "❤❤\ufe0f", "☺☺\ufe0f", "👍👍\ufe0f", "✌✌\ufe0f 見出し"} {
		c := dcall("_heading_chars", s)
		c.Keep = true
		found = append(found, c)
	}
	cases = append(cases, VisCase{Name: "tone/found", Calls: found})
	for _, f := range diffToneBrokenCases() {
		cases = append(cases, VisCase{Name: "tone/broken/" + f.name, Calls: diffToneStageCalls(f.old, f.new, []any{f.docType}, true, true)})
	}
	return cases
}

type diffToneExcerpt struct {
	old, new string
	spans    [][]int
	width    int
}

// diffToneExcerptCases は _tone_excerpt に通す組 (印の位置・共通の前と後ろ・文の終わりの印・長さの端)。
func diffToneExcerptCases() []diffToneExcerpt {
	a := strings.Repeat("あ", 25)
	b := strings.Repeat("い", 25)
	out := []diffToneExcerpt{
		{"", "", nil, 0}, {"あ", "", nil, 0}, {"", "あ", nil, 0}, {"あ！", "あ。", [][]int{{1, 2}}, 0}, {"あ！い", "あい", [][]int{{1, 2}}, 0}, {"！", "", [][]int{{0, 1}}, 0},
		{a + "！" + b, a + "。" + b, [][]int{{25, 26}}, 0}, {a + "！" + b, a + b, [][]int{{25, 26}}, 0}, {a + "！" + b, a + "！！" + b, [][]int{{25, 26}}, 0},
		{a + "！" + b + "！" + a, a + "。" + b + "！" + a, [][]int{{25, 26}, {51, 52}}, 0}, {a + "！" + b + "！" + a, a + "！" + b + "。" + a, [][]int{{25, 26}, {51, 52}}, 0},
		{a + "。" + b + "！" + a, a + "。" + b + "。" + a, [][]int{{51, 52}}, 0}, {a + "。" + b + "！" + a + "。", a + "。" + b + "。" + a + "。", [][]int{{51, 52}}, 0},
		{a + "！", a + "。", [][]int{{25, 26}}, 0}, {"！" + a, "。" + a, [][]int{{0, 1}}, 0}, {a + "とても" + b, a + b, [][]int{{25, 28}}, 0}, {a + "とても" + b, a + "ふつう" + b, [][]int{{25, 28}}, 0},
		{a + "とても" + b + "とても" + a, a + "とても" + b + a, [][]int{{25, 28}, {53, 56}}, 0}, {a + "とても" + b + "とても" + a, a + b + "とても" + a, [][]int{{25, 28}, {53, 56}}, 0},
		{"あ。" + a + "！" + b, "あ。" + a + "。" + b, [][]int{{27, 28}}, 0}, {"。" + a + "！" + b, "。" + a + "。" + b, [][]int{{26, 27}}, 0}, {"\u3000。" + a + "！" + b, "\u3000。" + a + "。" + b, [][]int{{27, 28}}, 0},
		{a + "！", "", [][]int{{25, 26}}, 0}, {a + b + "！", "", [][]int{{50, 51}}, 0}, {"あ" + a + "。", "あ", [][]int{{0, 1}}, 0}, {"。。。", "。", [][]int{{1, 2}}, 0}, {"   ", "あ", nil, 0}, {"！  ", "  ", [][]int{{0, 1}}, 0},
		{a + "！" + b, a + "。" + b, [][]int{{25, 26}}, 5}, {a + "！" + b, a + "。" + b, [][]int{{25, 26}}, 1}, {a + "！" + b, a + "。" + b, [][]int{{25, 26}}, 100}, {a + "！" + b, a + "。" + b, nil, 0}, {a + "！" + b, a + "。" + b, [][]int{{0, 1}}, 0},
		{a + "！" + b, a + b + a, [][]int{{25, 26}}, 0}, {a + "！" + b, "あ", [][]int{{25, 26}}, 0}, {"あいう！えお", "あいう。えお", [][]int{{3, 4}}, 0}, {"あいう！えお", "あ", [][]int{{3, 4}}, 0}, {"あいう！えお", "え", [][]int{{3, 4}}, 0},
		{"abc！def", "abXdef", [][]int{{3, 4}}, 0}, {"abc！def", "abc", [][]int{{3, 4}}, 3}, {"abcdef", "abxdef", [][]int{{0, 1}}, 0}, {"abcdef", "abxdef", [][]int{{5, 6}}, 0}, {"abcdef", "abxdef", [][]int{{1, 3}}, 0},
		{"あ" + a + "！", "あ" + a, [][]int{{26, 27}}, 0}, {"あ！い！う", "あ！い。う", [][]int{{1, 2}, {3, 4}}, 0}, {"あ！い！う", "あ。い！う", [][]int{{1, 2}, {3, 4}}, 0}, {"あ！い！う", "あ。い。う", [][]int{{3, 4}, {1, 2}}, 0},
		{"とても", "とてつもなく" + strings.Repeat("あ", 60), [][]int{{0, 3}}, 0}, {"とても", "とてつもなく" + strings.Repeat("あ", 20), [][]int{{0, 3}}, 0}, {"とても", "とてつもなく" + strings.Repeat("あ", 60), [][]int{{0, 3}}, 5},
		{"とても" + a + a, "とてつもなく" + a + a + a + a, [][]int{{0, 3}}, 0}, {"とても" + a, "とてつもなく" + b + b + b, [][]int{{0, 3}}, 0}, {"とても" + a, "とてつもなく" + b + b + b, [][]int{{0, 3}}, 1},
		{"とても" + a, "とてつもなく" + b + b + b, [][]int{{0, 3}, {10, 13}}, 0}, {"あ。とても" + a, "あ。とてつもなく" + b, [][]int{{2, 5}}, 0}, {"とても", "とてつもなく。" + strings.Repeat("あ", 60), [][]int{{0, 3}}, 0},
		{"とても" + a, "とてつもなく" + a, [][]int{{0, 3}}, 0}, {"とても" + a, "とて", [][]int{{0, 3}}, 0}, {"とても", "とて", [][]int{{0, 3}}, 0}, {"🇯🇵", "🇯" + strings.Repeat("あ", 60), [][]int{{0, 2}}, 0}, {"1\ufe0f\u20e3", "1" + strings.Repeat("あ", 60), [][]int{{0, 3}}, 0},
	}
	return out
}

// diffToneFoundExcerpts は _tone_excerpt の、印の選び方 (変わった所の直後から始まる印)・推敲後の位置 (印の直後が変わった所の始まり)・
// 文の終わりのあとが空白だけのときの始め方で、本家のコピーに壊した形を入れると答えが変わる組 (乱数で探した小さいもの)。
func diffToneFoundExcerpts() []diffToneExcerpt {
	return []diffToneExcerpt{
		{"。!てあ!？", "。!!？", [][]int{{1, 2}, {4, 5}, {5, 6}}, 0},
		{"い\u3000？とえあ", "い\u3000？ととあ", [][]int{{0, 2}, {5, 7}}, 0},
		{"？え。\u3000！", "？え！!うう！", [][]int{{0, 1}, {4, 5}}, 0},
		{"!？！\u3000", "!と も?う！\u3000", [][]int{{0, 1}, {2, 4}}, 0},
		{" !あ？!?", " !!あ？!?", [][]int{{1, 2}, {3, 4}, {4, 5}, {5, 6}}, 0},
		{"えおおうう も", "えおおううも", [][]int{{0, 3}, {6, 8}}, 0},
		{"?", "。?", [][]int{{0, 1}}, 0},
		{"?", "!も?", [][]int{{0, 1}}, 0},
		{"?", "!\u3000！?", [][]int{{0, 1}}, 0},
		{"て", "。！！あて", [][]int{{0, 1}}, 0},
		{"！?", "！！とと?", [][]int{{0, 1}, {1, 2}}, 0},
		{"!", " もう?と！!", [][]int{{0, 1}}, 0},
		{"\u3000", "!う？?\u3000て。！ !。？!もと\u3000おて?!？！\u3000\u3000", [][]int{{0, 1}}, 0},
		{"\u3000", " \u3000あ？あも?\u3000いあい!う?\u3000あい。うとい!も。\u3000", [][]int{{0, 1}}, 0},
		{"\u3000", "お!あと？?も あ。もえ！あう?え?  \u3000とうう？\u3000\u3000", [][]int{{0, 1}}, 0},
		{"!おう?\u3000", "!おう?？\u3000おえ!\u3000?!も?!い\u3000も!えててあお!\u3000", [][]int{{0, 2}, {1, 4}, {2, 3}, {3, 4}, {4, 5}}, 0},
		{"？!。 てえと?\u3000", "？!。 てえと?と?い！!あ? も？ ！あいも?\u3000?\u3000", [][]int{{0, 1}, {8, 9}}, 0},
		{"う。おと ? ", "う。おと ?？！\u3000いう うい。い\u3000ててい   も。?\u3000ああ\u3000！? ", [][]int{{0, 3}, {2, 5}, {4, 6}, {6, 7}}, 0},
	}
}
