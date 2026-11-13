package oracle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const lintConstantsCase = "constants"

func lcall(fn string, args ...any) Call {
	return Call{Module: LintModule, Function: fn, Args: args, Digest: true}
}

// lanalyze は analysis を作って覚える呼び出し (結果の記録は要らないので quiet)。analysis の中身は visibility.json が本家と突き合わせている。
func lanalyze(text, save string) Call {
	return Call{Module: VisModule, Function: "analyze_markdown", Args: []any{text}, Digest: true, Quiet: true, Save: save}
}

// recorder は呼び出しを作る (keep は結果そのものを記録に残すか。残すと記録が大きくなるので、短い入力の呼び出しだけにする)。
type recorder struct{ keep bool }

func (r recorder) call(fn string, args ...any) Call {
	c := lcall(fn, args...)
	c.Keep = r.keep
	return c
}

// callSaving は戻り値を覚える呼び出し (後の呼び出しが Ref で受ける)。
func (r recorder) callSaving(save, fn string, args ...any) Call {
	c := r.call(fn, args...)
	c.Save = save
	return c
}

// lintEndClosers は本家の _END_CLOSERS と同じ字 (記録の定数と Go の実装が一致することは別に見ている)。_strip_end_decoration の closers の引数に渡す。
const lintEndClosers = "）)】〕〉》］←↑→↓〜～"

// lintBracketClosers は _END_BRACKETS の閉じかっこ。_bracket_opens_sentence の closer に渡す。
const lintBracketClosers = "）)】〕〉》］"

// lintDocCalls は 1 つの文書 (Markdown の文字列) に、土台の関数を全部通す呼び出し。行ごとの呼び出しの引数は物理行 (改行を除いたもの)。
func lintDocCalls(text string, keep bool) []Call {
	rec := recorder{keep}
	lines, starts := physicalLinesOf(text)
	calls := []Call{lanalyze(text, "a")}
	for i, line := range lines {
		n := i + 1
		pname := fmt.Sprintf("p%d", n)
		calls = append(calls,
			rec.call("_emoji_matches", line),
			rec.call("_strip_end_decoration", line),
			rec.call("_strip_end_decoration", line, lintEndClosers),
			rec.call("_decoration_breaks", line),
			rec.call("_with_undecorated", []any{line}),
			rec.call("_mask_quoted_segments", line),
			rec.call("_mask_quoted_segments_sparse", line),
			rec.call("_question_content_end", line),
			rec.call("_last_context_sentence", line),
			rec.call("_ends_with_question", line),
			rec.callSaving(pname, "_prose_visible_text", Ref("a"), n),
			rec.call("_image_only_row", Ref("a"), map[string]any{"start": starts[i], "raw": line}, Ref(pname)),
			rec.call("_v220_scan_text", Ref("a"), n),
			rec.call("_end_closers_for", Ref("a"), n),
		)
		for pos, r := range []rune(line) {
			if strings.ContainsRune(lintBracketClosers, r) {
				calls = append(calls, rec.call("_bracket_opens_sentence", line, pos, string(r)))
			}
		}
	}
	all := make([]any, len(lines))
	for i, l := range lines {
		all[i] = l
	}
	return append(calls,
		rec.call("_with_undecorated", all),
		rec.call("_with_undecorated", all, ""),
		rec.call("_plain_sentence_records", Ref("a")),
		rec.call("_rule9_context", Ref("a")),
		rec.call("_end_quote_closers", Ref("a")),
	)
}

func lintCorpusCase(path string, raw []byte) VisCase {
	return VisCase{Name: "corpus/" + path, Calls: lintDocCalls(string(raw), false)}
}

func lintSnippetCase(name, text string) VisCase {
	return VisCase{Name: "snippet/" + name, Calls: lintDocCalls(text, true)}
}

// lintExtraDocs は markdownSnippets にない、土台が見る構造を狙う手書きの文書。
var lintExtraDocs = []struct{ name, text string }{
	{"image-only", "![a](b.png)\n\n![a](b.png) text\n\n[![badge](https://x/y.svg)](https://x)\n[![badge](https://x/y.svg)](https://x) after\n\n[![a](b)](c) [![d](e)](f)\n"},
	{"image-code-atom", "text one。\n`code\nspan` more。\n![i](j)\ntext two。\n"},
	{"image-sections", "一つ目の文です。\n![img](a.png)\n二つ目の文です。\n[![b](x)](y)\n三つ目の文です。\n"},
	{"quote-closers", "「会話です」\n続きの文です。\n\n「会話」とのことです。」\n\n\"quoted\" text。\n\"unpaired。\"\n\n“対です”と言う。”\n\n'a' and 'b'\n"},
	{"quote-closers-list", "- 「項目です」\n- 項目です」\n  続きです」\n- “a” と ”b”\n"},
	{"rule9", "「引用の中の文。」の外の文。\n次の文です。\n\n- 項目の文です。\n  **強調**__下線__の文です。\n> 引用の行です。\n\n『二重』と[括弧]の文。\n"},
	{"rule9-nested", "「外側「内側」の文。」ここは外。\n『外『内』。』外。\n[a [b] c] d\n"},
	{"scan-text", "`code` と path/to/file.go と camelCase と snake_case.py と \"引用\" と “引用” と ‘引用’ と 「括弧」 と [リンク](http://x/y) です。\n"},
	{"scan-identifier", "a.b.c と a/b と _x と /usr/bin/env と abcDef と ABC と a_b と x.y/ です。\n"},
	{"decoration-line", "見ていきましょう🚀！\n見ていきましょう？🚀\n（見ていきましょう）\n実践編（〜）\nこれはAです🚀これはBです🚀これはCです🚀\n設定は⚙ボタンから\nいかがでしたか😊 次回も。\n"},
	{"paragraphs", "一文目です。二文目です。三文目です。\n\n四文目です。\n五文目です。\n\n- 箇条です。\n- 箇条です。\n\n短い。ab。です。\n"},
	{"no-final-newline", "文の終わりが無い"},
	{"crlf", "一文目です。\r\n二文目です。\r\n\r\n三文目です。\r\n"},
}

// lintEmojiStrings は _emoji_matches に通す手書きの入力 (1 つが 1 つの枝を狙う)。FE0F・ZWJ などの見えにくい字は escape で書く。
var lintEmojiStrings = []string{
	"", "a", "あ", "🚀", "🚀🚀", "🚀a🚀", "🇯🇵", "🇯🇯", "🇯", "🇯a🇵", "🇯🇵🇺🇸", "🇯🇵🇺", "🇦🇨🇦🇩", "🇰🇵🇰🇷", "🇽🇰", "🇶🇦🇶", "🇦🇦",
	"1\ufe0f\u20e3", "1\u20e3", "#\ufe0f\u20e3", "*\ufe0f\u20e3", "a1\ufe0f\u20e3b", "1\ufe0f", "1", "0\ufe0f\u20e3\ufe0f", "10\ufe0f\u20e3", "\ufe0f\u20e3", "1\ufe0f\ufe0f\u20e3",
	"©", "©\ufe0e", "©\ufe0f", "®\ufe0f\ufe0e", "™", "™\ufe0e", "™\ufe0f", "‼", "‼\ufe0f", "‼\ufe0e", "↔\ufe0f", "↔\ufe0e", "↔",
	"☺", "☺\ufe0f", "☺\ufe0e", "❤", "❤\ufe0f", "❤\ufe0e", "❤\ufe0f\u200d\U0001f525", "✌\ufe0f", "✅", "⭐\ufe0e", "⭐",
	"\U0001f44d\U0001f3fd", "\U0001f468\u200d\U0001f469\u200d\U0001f467", "\U0001f3f4\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f", "\U0001f600\ufe0f", "\U0001f600\ufe0e",
	"\U0001f9ff", "\U0001faff", "\U0001fb00", "\U0001f000", "\U0001f004", "\U0001f0cf", "\U0001f1e6", "\U0001f3fb", "\u200d", "\ufe0f", "\ufe0e", "\u20e3", "\U000e0020",
	"☀", "☀\ufe0f", "⬅", "⬆\ufe0f", "〰", "〰\ufe0f", "〽", "〽\ufe0e", "㊗", "㊙\ufe0f", "⿿", "\u3000", "¨", "ª",
	"文字\U0001f600の途中✨と❤\ufe0f終わり", "A©B©\ufe0eC", "❤\ufe0f\ufe0f", "❤\ufe0e\ufe0f", "\U0001f1ef\U0001f1f5\ufe0f", "\U0001f1ef\U0001f1f5\u20e3",
	"#\ufe0f", "#\u20e3", "#\ufe0f\u20e3\ufe0f", "9\ufe0f\u20e3\U0001f600", "*", "#", "0", "9",
}

// lintEmojiSurrogates は単独のサロゲートを含む入力 (本家の str は持てる)。
var lintEmojiSurrogates = []Cps{
	{0xD83D, 0x20}, {0x1F680, 0xD83D}, {0xDE80}, {0xA9, 0xD800, 0x61}, {0x1F1EF, 0xD83D, 0x1F1F5}, {0x31, 0xFE0F, 0xDC00, 0x20E3},
}

func lintEmojiCases() []VisCase {
	rec := recorder{true}
	var calls []Call
	for _, s := range lintEmojiStrings {
		calls = append(calls, rec.call("_emoji_matches", s))
	}
	for _, s := range lintEmojiSurrogates {
		calls = append(calls, rec.call("_emoji_matches", s))
	}
	return []VisCase{{Name: "emoji/snippets", Calls: calls}, lintEmojiSweep(), lintEmojiFlags(), lintEmojiTables()}
}

// lintEmojiSweep は全コードポイントを、後ろの字との 5 通りの並びで _emoji_matches に通す (1 回に 256 字分)。
func lintEmojiSweep() VisCase {
	var calls []Call
	suffixes := []string{"", "\ufe0f", "\ufe0e", "\u20e3", "\ufe0f\u20e3"}
	for _, span := range [][2]rune{{0x20, 0x33ff}, {0xe000, 0xe0ff}, {0xfe00, 0xfe0f}, {0x1f000, 0x1ffff}, {0xe0000, 0xe00ff}} {
		for lo := span[0]; lo <= span[1]; lo += 256 {
			hi := min(lo+255, span[1])
			for _, suffix := range suffixes {
				var b strings.Builder
				for cp := lo; cp <= hi; cp++ {
					if cp >= 0xD800 && cp <= 0xDFFF {
						continue
					}
					b.WriteRune(cp)
					b.WriteString(suffix)
					b.WriteString("あ")
				}
				calls = append(calls, lcall("_emoji_matches", b.String()))
			}
		}
	}
	// 続けて並べた字 (前の一致の終わりより前の始まりを飛ばす規則) と、ASCII の字の前後。
	for _, tail := range []string{"1", "#", "*", "a1", "\ufe0f", "\ufe0f\u20e3"} {
		var b strings.Builder
		for cp := rune(0x1f600); cp <= 0x1f64f; cp++ {
			b.WriteRune(cp)
			b.WriteString(tail)
		}
		calls = append(calls, lcall("_emoji_matches", b.String()))
	}
	return VisCase{Name: "emoji/sweep", Calls: calls}
}

// lintEmojiFlags は地域指示子の全 676 通りの組を、1 つ目の字ごとに 1 回で通す。
func lintEmojiFlags() VisCase {
	var calls []Call
	for first := rune(0x1f1e6); first <= 0x1f1ff; first++ {
		var b strings.Builder
		for second := rune(0x1f1e6); second <= 0x1f1ff; second++ {
			b.WriteRune(first)
			b.WriteRune(second)
			b.WriteString("あ")
		}
		calls = append(calls, lcall("_emoji_matches", b.String()))
		var packed strings.Builder
		for second := rune(0x1f1e6); second <= 0x1f1ff; second++ {
			packed.WriteRune(first)
			packed.WriteRune(second)
		}
		calls = append(calls, lcall("_emoji_matches", packed.String()))
	}
	return VisCase{Name: "emoji/flags", Calls: calls}
}

// lintEmojiTables は _emoji_character_class・_emoji_flag_pattern・_merged_ranges の呼び出し。
func lintEmojiTables() VisCase {
	rec := recorder{true}
	pairs := func(ps ...[2]int) []any {
		out := make([]any, len(ps))
		for i, p := range ps {
			out[i] = []any{p[0], p[1]}
		}
		return out
	}
	calls := []Call{
		rec.call("_emoji_character_class", pairs()),
		rec.call("_emoji_character_class", pairs([2]int{0x41, 0x41})),
		rec.call("_emoji_character_class", pairs([2]int{0x41, 0x5a}, [2]int{0xffff, 0xffff}, [2]int{0x10000, 0x10001}, [2]int{0xa9, 0xaa})),
		rec.call("_emoji_character_class", pairs([2]int{0x1f600, 0x1f64f}, [2]int{0x20e3, 0x20e3})),
		lcall("_emoji_flag_pattern"),
		rec.call("_merged_ranges", pairs()),
		rec.call("_merged_ranges", pairs([2]int{5, 6}, [2]int{1, 2}, [2]int{3, 4})),
		rec.call("_merged_ranges", pairs([2]int{1, 5}, [2]int{2, 3}, [2]int{6, 7}, [2]int{9, 9}, [2]int{8, 8})),
		rec.call("_merged_ranges", pairs([2]int{3, 3}, [2]int{3, 3}, [2]int{1, 3}, [2]int{4, 10}, [2]int{12, 12})),
	}
	return VisCase{Name: "emoji/tables", Calls: calls}
}

// lintDecorationStrings は文末の飾りと句点の代わりの絵文字の関数に通す手書きの文。
var lintDecorationStrings = []string{
	"", " ", "。", "！？", "見ていきましょう", "見ていきましょう🚀！", "見ていきましょう？🚀", "見ていきましょう🚀 ", "見ていきましょう 🚀🚀。", "🚀", "🚀。🚀", "★見ていきましょう☆♪✓",
	"まとめました（後半で見ていきましょう）", "（見ていきましょう）", "実践編（〜）", "実践編（詳細）", "です。（補足）", "ます（補足）。", "あ（い）（う）", "あ。（い（う）え）", "（あ（い）う）", "（あ）い）",
	"矢印→", "波線〜〜", "終わり～", "上↑下↓", "閉じ）が先", "先（開き", "x" + strings.Repeat("あ", 300) + "）", "（" + strings.Repeat("あ", 300) + "）", "。" + strings.Repeat("あ", 250) + "（い）", "です" + strings.Repeat(" ", 260) + "（い）",
	"「会話です」", "「会話です」🚀", "引用”", "引用’", "引用\"", "引用'", "a\"b\"", "1\ufe0f\u20e3", "終わり1\ufe0f\u20e3", "終わり1\u20e3", "終わり\ufe0f\u20e3", "終わり🇯🇵", "終わり👍🏽", "終わり\u200d", "終わり\U000e0067",
	"これはAです🚀これはBです🚀これはCです🚀", "これはAです🚀 これはBです", "いかがでしたか🚀という結び", "🍎を買う", "設定は⚙ボタンから", "〜です♪（笑）", "本日は晴れ🚀、明日も", "あ🚀い🚀う🚀え", "あ🚀*い", "あ🚀_い", "あ🚀**い**う🚀え", "あ🚀1つ", "あ🚀ー", "あ🚀でも", "あ🚀では", "あ🚀もちろん", "あ🚀もう", "あ🚀しかし", "あ🚀だけ", "あ🚀まで",
	"「あ🚀い」う🚀え", "（あ🚀い）う🚀え", "「あ」🚀い", "【あ🚀い】う🚀え", "[あ🚀い]う🚀え", "“あ🚀い”う🚀え", "(a🚀b)c🚀d", "『あ🚀い』う🚀え",
	"🚀い🚀う", "  🚀い", "あ  🚀  い", "あ,🚀い", "あは🚀い", "あが🚀い", "あ、🚀い", "あ：🚀い", "あ🚀🚀い", "あ😊 い😊 う",
	"あ\u200d🚀い🚀う", "あ👍🏽い👍🏽う", "あ\U0001f1ef\U0001f1f5い\U0001f1ef\U0001f1f5う", "あ1\ufe0f\u20e3い1\ufe0f\u20e3う",
}

// lintQuoteStrings は引用の伏せと疑問の判定に通す手書きの文。
var lintQuoteStrings = []string{
	"", "「」", "「あ」", "「あ", "あ」", "「あ」い「う」", "「あ「い」う」", "「あ「い」", "『あ「い』う」", "[a]b[c]", "[a[b]c]", "]a[", "「あ[い」う]", "「あ\nい」う", "『あ』\n「い", "「「」」", "「」」「", "x[y]「z」『w』",
	"なぜですか？", "どうして？", "どうしても？", "いつですか", "いつでもですか", "いつもですか", "いつからですか", "どこですか。", "誰ですか", "何ですか", "何でもですか", "どれですか", "どのようにですか", "どのような方法ですか", "どの方法でもですか", "どの方法ですか",
	"これは重要ですか", "これは重要ですか。", "これは重要ですか？", "これは重要でしょうか", "これは重要だろうか。", "重要なのか", "重要ないか", "行うか", "行くか", "泳ぐか", "話すか", "待つか", "死ぬか", "遊ぶか", "読むか", "売るか", "来たか", "いいか", "よいか", "いくつか", "いくつか。", "いくつかですか",
	"これは重要ですか？」", "これは重要ですか）", "これは重要ですか**", "これは重要ですか🚀", "これは重要ですか？🚀", "これは重要ですか _", "これは重要ですか\u3000", "これは重要ですか'", "これは重要ですか\"", "これは重要ですか』", "これは重要ですか)",
	"あ。これは重要ですか", "あ！これは重要ですか？", "あ？い。う", "あ。い。う。", "あ。。", "。。", "。", "？", "あ？", "あ！", "あ!", "あ?", "あ!?", "あ.",
	"あ。い🚀", "あ。い？🚀", "あ。い）", "あ。い**", "あ。い。**", "あ。 い 。", "\u3000あ。", "あ。\u3000",
	"何かですか", "何かからですか", "どこかですか", "どこかからですか", "なぜなら", "どうしてもですか", "いつかですか", "誰かですか",
}

// lintBracketBoundaryStrings は、対の開きかっこを閉じかっこの 256 字前まで見る境目 (255・256・257 字前) の入力。
// 開きの前が句点・述語・無い (文頭) の組と、境目の外の字が句点になる組を持つ。
func lintBracketBoundaryStrings() []string {
	var out []string
	for _, prefix := range []string{"", "あ。", "あです", "あ" + strings.Repeat("い", 300) + "。", strings.Repeat("い", 250) + "。"} {
		for n := 250; n <= 260; n++ {
			out = append(out, prefix+"（"+strings.Repeat("あ", n)+"）")
		}
	}
	return out
}

// lintDecorationCalls は 1 つの文に、文末の飾りの関数を全部通す呼び出し。keep は結果そのものを記録に残すか。
func lintDecorationCalls(s string, keep bool) []Call {
	rec := recorder{keep}
	calls := []Call{
		rec.call("_strip_end_decoration", s), rec.call("_strip_end_decoration", s, lintEndClosers), rec.call("_strip_end_decoration", s, "」』”’\"'"), rec.call("_strip_end_decoration", s, lintEndClosers+"」』”’\"'"),
		rec.call("_decoration_breaks", s), rec.call("_with_undecorated", []any{s}), rec.call("_with_undecorated", []any{s}, ""), rec.call("_with_undecorated", []any{s, "あ。", s}, "）」"),
	}
	for pos, r := range []rune(s) {
		if strings.ContainsRune(lintBracketClosers, r) {
			calls = append(calls, rec.call("_bracket_opens_sentence", s, pos, string(r)))
		}
	}
	return calls
}

func lintStringCases() []VisCase {
	rec := recorder{true}
	var decor, quote []Call
	for _, s := range lintDecorationStrings {
		decor = append(decor, lintDecorationCalls(s, true)...)
	}
	// 境目の入力は長いので、結果そのものは記録に残さない (digest だけ)。
	for _, s := range lintBracketBoundaryStrings() {
		decor = append(decor, lintDecorationCalls(s, false)...)
	}
	for _, s := range lintQuoteStrings {
		quote = append(quote,
			rec.call("_mask_quoted_segments", s), rec.call("_mask_quoted_segments_sparse", s), rec.call("_question_content_end", s), rec.call("_last_context_sentence", s), rec.call("_ends_with_question", s),
			rec.call("_strip_end_decoration", s, lintEndClosers))
	}
	// 単独のサロゲートを含む文 (str のまま渡る)。
	for _, s := range []Cps{{0xD800}, {0x3042, 0xD800, 0x3002}, {0xD83D, 0x1F680}, {0x300C, 0xD800, 0x300D}, {0x3042, 0xDC00, 0xFF1F}} {
		quote = append(quote, rec.call("_mask_quoted_segments", s), rec.call("_question_content_end", s), rec.call("_ends_with_question", s), rec.call("_last_context_sentence", s),
			rec.call("_strip_end_decoration", s), rec.call("_decoration_breaks", s), rec.call("_emoji_matches", s))
	}
	return []VisCase{{Name: "strings/decoration", Calls: decor}, {Name: "strings/quote-question", Calls: quote}}
}

func lintConstantCases() []VisCase {
	calls := make([]Call, len(LintBaseConstants))
	for i, name := range LintBaseConstants {
		calls[i] = Call{Module: LintModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	return []VisCase{{Name: lintConstantsCase, Calls: calls}}
}

// lintAnalysisFails は本家の analyze_markdown が AssertionError で落ちる入力 (visibility.json が例外として記録している)。
// analysis を作れないので、analysis を使う土台の関数の case に入れない。
var lintAnalysisFails = map[string]bool{"html-script-long-s": true, "html-style-long-s": true}

// LintBaseCases は記録する呼び出しの全部 (入力の集まり・手書きの文書と文・定数)。
func LintBaseCases(c Corpus) ([]VisCase, error) {
	cases := lintConstantCases()
	cases = append(cases, lintEmojiCases()...)
	cases = append(cases, lintStringCases()...)
	cases = append(cases, lintTableFunctionCases()...)
	cases = append(cases, lintShapeStringCases()...)
	for _, f := range c.Files {
		raw, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, fmt.Errorf("入力 %s を読めません: %w", f.Path, err)
		}
		cases = append(cases, lintCorpusCase(f.Path, raw))
	}
	for _, s := range markdownSnippets {
		if lintAnalysisFails[s.name] {
			continue
		}
		cases = append(cases, lintSnippetCase(s.name, s.text))
	}
	for _, s := range lintExtraDocs {
		cases = append(cases, lintSnippetCase("lint-"+s.name, s.text))
	}
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases, nil
}

// lintTableFunctionCases は一覧や表から組んだ値を返す関数の呼び出し (_v220_load_list は項目と出どころ。正規表現は同じ長さの語の並びが PYTHONHASHSEED で変わるので記録しない) (G15 以降の表。定数と同じく、Go の表が本家の値と同じかを比べる)。
func lintTableFunctionCases() []VisCase {
	rec := recorder{true}
	return []VisCase{{Name: "tables/functions", Calls: []Call{rec.call("_v220_load_list", "renyo-nouns")}}}
}
