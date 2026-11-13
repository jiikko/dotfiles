package lint

import (
	"slices"
	"strings"
	"sync"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 文末の飾り (本家 yomiyasu_lint.py の T2)。文末に固定した規則は、文の終わりの後ろに続く飾りを読み飛ばして判定する。
// 飾りは、空白と、emoji_prohibited が絵文字として拾う字 (T1 の表から作る) と、☆♪✓のような記号。句点・感嘆符・疑問符は飾りではないので、除かずに位置だけを詰める。
// 閉じかっこ・矢印・波線を飾りに数えるのは文の型を見る規則だけ (endClosers)。閉じかっこは、対の開きかっこが文頭か句点か述語の直後にあるときだけ除く。
// 閉じかぎと引用符は、同じ段落のその行までに対の開きがない行でだけ除く (endQuoteClosers)。
//
// 本家の名前と Go の名前:
//
//	_merged_ranges            mergedRanges
//	_bracket_opens_sentence   bracketOpensSentence
//	_strip_end_decoration     stripEndDecoration (戻りの 2 つ目は、本家の `core is not sentence` = 飾りを除いたか)
//	_decoration_breaks        decorationBreaks (None は nil)
//	_with_undecorated         withUndecorated
//	_end_quote_closers        (*document).endQuoteClosers
//	_end_closers_for          (*document).endClosersFor
//	_END_VISIBLE_RANGES など  endVisibleRanges・endDecorationChars・endDecorationRun (表から作る文字列は、使うときに作る)

const (
	endMarks            = "。．！？!?"
	endClosers          = "）)】〕〉》］←↑→↓〜～"
	endBreakHeadChars   = "はがをのにでとへやも、，,：:"
	endKeycapPattern    = `[0-9#*]\ufe0f?\u20e3`
	endBracketPredicate = `(?:です|ます|でした|ました|ません|ない|だ|た|る)$`
	// 並びのあとが助詞なら、文が続いている。
	endBreakContinues = `(?:と|って|の|を|が|は|に|へ|や|か|から|まで|より|など|だけ|で(?![はも])|も(?!ちろん|し|っと|う)|ー)`
)

// endBracketPairs は閉じかっこと対の開きかっこ (本家の _END_BRACKETS の並び)。
var endBracketPairs = [...][2]rune{{'）', '（'}, {')', '('}, {'】', '【'}, {'〕', '〔'}, {'〉', '〈'}, {'》', '《'}, {'］', '［'}}

// endQuotePairs は閉じ引用符と対の開き引用符 (本家の _END_QUOTES の並び。" と ' は開きと閉じが同じ字)。
var endQuotePairs = [...][2]rune{{'」', '「'}, {'』', '『'}, {'”', '“'}, {'’', '‘'}, {'"', '"'}, {'\'', '\''}}

// endBreakPairs は句点の代わりの絵文字で分けるとき、かっこの中では分けないための対 (開き, 閉じ)。
var endBreakPairs = [...][2]rune{{'「', '」'}, {'『', '』'}, {'（', '）'}, {'(', ')'}, {'【', '】'}, {'“', '”'}, {'［', '］'}, {'[', ']'}}

// 文末の後ろに置かれる記号 (絵文字の表にない ☆♪✓ などを含む) と、絵文字の並びをつなぐ字。
var (
	endSymbolRanges = []runeRange{{0x2600, 0x27BF}, {0x2B05, 0x2B07}, {0x2B1B, 0x2B1C}, {0x2B50, 0x2B50}, {0x2B55, 0x2B55}, {0x231A, 0x231B}, {0x23E9, 0x23FA},
		{0x2934, 0x2935}, {0x3297, 0x3297}, {0x3299, 0x3299}, {0x1F000, 0x1FAFF}}
	endJoinerRanges = []runeRange{{0xFE0E, 0xFE0F}, {0x200D, 0x200D}, {0xE0020, 0xE007F}}
	// endVisibleRanges は絵文字の判定の元の表と旗の地域指示子を足して併せた区間。手で写さず同じ表から作るので、範囲がずれない。
	endVisibleRanges = mergedRanges(slices.Concat(endSymbolRanges, emojiExplicitVariationBases[:], emojiRegisteredTextBases[:], emojiOtherBases[:],
		[]runeRange{{regionalIndicatorA, regionalIndicatorZ}}))
)

// mergedRanges は本家の _merged_ranges。区間を (lo, hi) の順に並べ、重なるか隣り合うものをつなぐ。
func mergedRanges(ranges []runeRange) []runeRange {
	sorted := slices.SortedFunc(slices.Values(ranges), func(a, b runeRange) int {
		if a.lo != b.lo {
			return int(a.lo - b.lo)
		}
		return int(a.hi - b.hi)
	})
	var merged []runeRange
	for _, rg := range sorted {
		if n := len(merged); n > 0 && rg.lo <= merged[n-1].hi+1 {
			merged[n-1].hi = max(merged[n-1].hi, rg.hi)
		} else {
			merged = append(merged, rg)
		}
	}
	return merged
}

// isEndDecorationChar は _END_DECORATION_CHAR (飾りの字 1 字の文字クラス) に 1 字が合うか。
func isEndDecorationChar(r rune) bool {
	return inRuneRanges(endVisibleRanges, r) || slices.ContainsFunc(endJoinerRanges, func(rg runeRange) bool { return rg.lo <= r && r <= rg.hi })
}

func hasEndDecorationChar(rs []rune) bool { return slices.ContainsFunc(rs, isEndDecorationChar) }

// 表から作る文字クラスの中身と、正規表現の中で文末の前に置ける飾りの並び。
var (
	endVisibleChars = sync.OnceValue(func() string {
		class := emojiCharacterClass(endVisibleRanges)
		return class[1 : len(class)-1]
	})
	endDecorationChars = sync.OnceValue(func() string {
		class := emojiCharacterClass(endJoinerRanges)
		return endVisibleChars() + class[1:len(class)-1]
	})
	// endDecorationRun は正規表現の中で、文末の前に置ける飾りの並び (空でもよい)。他の群の正規表現がパターンの文字列に埋め込む。
	endDecorationRun = sync.OnceValue(func() string { return `(?:` + endKeycapPattern + `|[\s` + endDecorationChars() + `])*` })
)

var (
	endBracketAfterPredicate = newLazy(endBracketPredicate, 0)
	// 文の途中で、句点の代わりに置いた絵文字の並び。並びは見える飾りの字で始まる。
	endBreakRun = newLazyFrom(func() string {
		return `(?:` + endKeycapPattern + `|[` + endVisibleChars() + `])(?:` + endKeycapPattern + `|[\s` + endDecorationChars() + `])*`
	}, 0)
	endBreakContinuesPattern = newLazy(endBreakContinues, 0)
)

func endBracketOpener(closer rune) (opener rune, ok bool) {
	for _, p := range endBracketPairs {
		if p[0] == closer {
			return p[1], true
		}
	}
	return 0, false
}

// bracketOpensSentence は本家の _bracket_opens_sentence。text[position] の閉じかっこと対の開きかっこが、文頭か句点か述語の直後にあるか。
// 見るのは閉じかっこの前の 256 字まで。そこで分からなければ除かない (長い行でも手間を抑える)。closer が閉じかっこの表に無いと本家は KeyError で、Go は panic する。
func bracketOpensSentence(text []rune, position int, closer rune) bool {
	opener, ok := endBracketOpener(closer)
	if !ok {
		panic("bracketOpensSentence: 閉じかっこでない字 " + string(closer))
	}
	floor := max(0, position-256)
	depth := 0
	for index := position; index > floor; {
		index--
		switch text[index] {
		case closer:
			depth++
		case opener:
			if depth > 0 {
				depth--
				continue
			}
			before := index
			for before > floor && (pyunicode.IsSpace(text[before-1]) || isEndDecorationChar(text[before-1])) {
				before--
			}
			if before == 0 {
				return true
			}
			return before > floor && (strings.ContainsRune(endMarks, text[before-1]) || endsWithPredicate(text, max(0, before-3), before))
		}
	}
	return false
}

// endsWithPredicate は _END_BRACKET_AFTER_PREDICATE.search(text, pos, endpos) が合うか。`$` は endpos の手前の最後の改行の前にも合うので、正規表現を通す。
func endsWithPredicate(text []rune, pos, endpos int) bool {
	m, err := endBracketAfterPredicate.get().Search(pyre.NewText(pyunicode.FromRunes(text)), pos, endpos)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// stripEndDecoration は本家の _strip_end_decoration。文の終わりの後ろの飾りを除いた文を返す (句点・感嘆符・疑問符は残し、飾りとの並びの順も保つ)。
// 2 つ目の戻りは、飾りを除いたか (本家の `core is not sentence`)。closers の字 (閉じかっこ・矢印・波線と、呼び出し側が確かめた閉じ引用符) も飾りとして除く。
func stripEndDecoration(text string, closers string) (string, bool) {
	rs := pyunicode.Runes(text)
	closerSet := pyunicode.Runes(closers)
	position := len(rs)
	var marks []rune
	removed := false
scan:
	for position > 0 {
		char := rs[position-1]
		switch {
		case strings.ContainsRune(endMarks, char):
			marks = append(marks, char)
			position--
		case char == combiningKeycap:
			removed = true
			position--
			if position > 0 && rs[position-1] == variationSelector16 {
				position--
			}
			if position > 0 && strings.ContainsRune("0123456789#*", rs[position-1]) {
				position--
			}
		case pyunicode.IsSpace(char) || isEndDecorationChar(char):
			removed = true
			position--
		case slices.Contains(closerSet, char) && removableCloser(rs, position-1, char):
			removed = true
			position--
		default:
			break scan
		}
	}
	if !removed {
		return text, false
	}
	slices.Reverse(marks)
	return pyunicode.FromRunes(append(rs[:position:position], marks...)), true
}

// removableCloser は閉じ字を飾りとして除いてよいか。閉じかっこは、対の開きかっこが文頭か句点か述語の直後にあるときだけ。かっこ以外 (矢印・波線・引用符) は常に除く。
func removableCloser(rs []rune, index int, char rune) bool {
	_, isBracket := endBracketOpener(char)
	return !isBracket || bracketOpensSentence(rs, index, char)
}

// decorationBreaks は本家の _decoration_breaks。句点の代わりに絵文字の並びを置いて次の文を続けた所で分けた文の並びを返す。分けられなければ nil。
// 分けた文は後ろに絵文字の並びを残す (呼び出し側が stripEndDecoration で除く)。かっこや引用の中と、助詞や記号が続く所では分けない。
func decorationBreaks(sentence string) []string {
	rs := pyunicode.Runes(sentence)
	if !hasEndDecorationChar(rs) {
		return nil
	}
	text := pyre.NewText(sentence)
	runs, err := endBreakRun.get().Finditer(text, 0, len(rs))
	if err != nil {
		panic(err)
	}
	var pieces []string
	start, scanned := 0, 0
	var balance [len(endBreakPairs)]int
	hasText := false
	for _, run := range runs {
		segment := rs[scanned:run.Start(0)]
		scanned = run.Start(0)
		segmentText := pyunicode.FromRunes(segment)
		hasText = hasText || !allSpace(segment)
		for i, pair := range endBreakPairs {
			balance[i] += strings.Count(segmentText, string(pair[0])) - strings.Count(segmentText, string(pair[1]))
		}
		end := run.End(0)
		word := end
		for word < len(rs) && (rs[word] == '*' || rs[word] == '_') {
			word++
		}
		if word >= len(rs) || !pyunicode.IsAlnum(rs[word]) || endContinuesAt(text, word) {
			continue
		}
		last := run.Start(0)
		for last > start && pyunicode.IsSpace(rs[last-1]) {
			last--
		}
		if !hasText || last == start || strings.ContainsRune(endBreakHeadChars, rs[last-1]) || slices.ContainsFunc(balance[:], func(v int) bool { return v > 0 }) {
			continue
		}
		pieces = append(pieces, pyunicode.FromRunes(rs[start:end]))
		start, scanned = end, end
		balance = [len(endBreakPairs)]int{}
		hasText = false
	}
	if len(pieces) == 0 {
		return nil
	}
	return append(pieces, pyunicode.FromRunes(rs[start:]))
}

func endContinuesAt(text pyre.Text, pos int) bool {
	m, err := endBreakContinuesPattern.get().Match(text, pos, text.Len())
	if err != nil {
		panic(err)
	}
	return m != nil
}

func allSpace(rs []rune) bool {
	return !slices.ContainsFunc(rs, func(r rune) bool { return !pyunicode.IsSpace(r) })
}

// withUndecorated は本家の _with_undecorated。文の並びに、文末の飾りを除いた文と、句点の代わりの絵文字で分けた文を足す (飾りのない文はそのまま)。
func withUndecorated(sentences []string, closers string) []string {
	result := slices.Clone(sentences)
	for _, sentence := range sentences {
		if core, changed := stripEndDecoration(sentence, closers); changed {
			result = append(result, core)
		}
		for _, piece := range decorationBreaks(sentence) {
			core, _ := stripEndDecoration(piece, closers)
			result = append(result, core)
		}
	}
	return result
}

// endQuoteClosers は本家の _end_quote_closers。行ごとに、文末の飾りとして除いてよい閉じ引用符を返す。
// 同じ段落 (箇条書きは項目ごと) のその行までに対の開き (「『“‘) がない閉じかぎと閉じ引用符だけを除く。" と ' は、その行までに 1 つしかなく、それがその行にあるときだけ除く。
func (d *document) endQuoteClosers() map[int]string {
	allowed := map[int]string{}
	for _, block := range d.an.Blocks {
		if block.Kind != "paragraph" && block.Kind != "list" {
			continue
		}
		var counts [len(endQuotePairs)]int
		for _, row := range block.Lines {
			visibleText := d.proseVisibleText(row.Line)
			visible := pyunicode.Runes(visibleText)
			for i, pair := range endQuotePairs {
				counts[i] += strings.Count(visibleText, string(pair[1]))
			}
			var chars []rune
			for i, pair := range endQuotePairs {
				closer, opener := pair[0], pair[1]
				want := 0
				if closer == opener {
					want = 1
				}
				if slices.Contains(visible, closer) && counts[i] == want {
					chars = append(chars, closer)
				}
			}
			if len(chars) > 0 {
				allowed[row.Line] = pyunicode.FromRunes(chars)
			}
		}
	}
	return allowed
}

// endClosersFor は本家の _end_closers_for。文の型を見る規則が、その行の文末で飾りとして除く字 (閉じかっこ・矢印・波線と、対のない閉じ引用符)。
func (d *document) endClosersFor(lineNo int) string {
	if d.endQuote == nil {
		d.endQuote = d.endQuoteClosers()
	}
	return endClosers + d.endQuote[lineNo]
}
