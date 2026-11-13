package diff

import (
	"math/big"
	"sort"
	"strconv"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 群 M: 数値の取り出し (本家の extract_numbers・_find_numbers・_number_mask・_with_notation と、漢数字・平仮名の数・単位の決まり)。
// 位置と長さは全部コードポイント (本家の str の添字)。巨大な整数 (4,300 桁を超える数字の並び) は本家が ValueError で止まる所だが、Go は止まらずに値を返す。

// 全角の英数字と記号 (U+FF01〜U+FF5E) と全角空白を半角にする。NFKC と同じ結果で、文字数を変えない
func halfWidth(r rune) rune {
	switch {
	case r >= 0xFF01 && r <= 0xFF5E:
		return r - 0xFEE0
	case r == 0x3000:
		return ' '
	}
	return r
}

// 数と単位の決まり。数えた単位を書き方の違いでそろえる (「ヶ月」「カ月」「ヵ月」は「か月」)
var (
	numberUnits = []string{"週間", "時間", "か月", "ヶ月", "カ月", "ヵ月", "TB", "GB", "MB", "KB", "ms", "px", "%",
		"年", "月", "日", "時", "分", "秒", "件", "個", "人", "名", "回", "円", "倍", "割", "度", "点",
		"本", "枚", "台", "社", "週", "歳", "字", "行",
		"か所", "カ所", "ヶ所", "箇所", "種類", "章", "節", "項", "つ",
		"条", "号", "階"}
	unitNotBefore = map[string]string{"階": "層", "条": "件"}
	sameUnit      = map[string]string{"ヶ月": "か月", "カ月": "か月", "ヵ月": "か月", "カ所": "か所", "ヶ所": "か所", "箇所": "か所"}
	kanjiCounters = []string{"週間", "時間", "か月", "ヶ月", "カ月", "ヵ月",
		"年", "日", "件", "個", "人", "名", "回", "円", "倍", "割", "本", "枚", "台", "社", "歳",
		"か所", "カ所", "ヶ所", "箇所", "種類", "章", "節", "項", "月", "つ",
		"条", "号", "階", "分",
		"時"}
)

// unitPatternSource は単位の並びの正規表現 (本家の _unit_pattern。長い単位を先に並べ、同じ長さの単位は元の順のまま)。
func unitPatternSource(units []string) string {
	sorted := append([]string(nil), units...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(pyunicode.Runes(sorted[i])) > len(pyunicode.Runes(sorted[j])) })
	parts := make([]string, len(sorted))
	for i, u := range sorted {
		parts[i] = pyre.Escape(u)
		if not, ok := unitNotBefore[u]; ok {
			parts[i] += "(?!" + not + ")"
		}
	}
	return strings.Join(parts, "|")
}

var (
	// 算用数字。英字や数字、「_」の直後と、「数字と . か ,」の直後からは始めない (v2、IPv6、x_3、1.2.3 の途中を数にしない)。
	// 「，」「．」(半角にしたもの) の直後の数は取る
	arabicNumber = pyre.NewLazy(`(?<![A-Za-z0-9_])(?<![0-9][.,])[0-9]+(?:[.,][0-9]+)*`, 0)
	emphasisMark = pyre.NewLazy(`\*+`, 0)
	thousandsRun = pyre.NewLazy(`[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?`, 0)
	unitPattern  = pyre.NewLazyFrom(func() string { return unitPatternSource(numberUnits) }, 0)
	// 英字の単位と % は、前に半角か全角の空白が 1 つあってもよい (「16 GB」「95 %」)
	spacedUnit = pyre.NewLazy(`[ \u3000](TB|GB|MB|KB|ms|px|%)(?![A-Za-z])`, 0)
	// 一覧にない英字の並び (5 字まで) は単位として取る (「30s」「100Mbps」「8GiB」)
	letterUnit = pyre.NewLazy(`[A-Za-z]{1,5}(?![A-Za-z])`, 0)
	// 「2千件」の「千」(千倍した 1 つの値にする)
	thousandMark = pyre.NewLazy(`千`, 0)
	// 漢数字は、意味が 1 つに決まる助数詞が続くときだけ取る
	kanjiNumber = pyre.NewLazyFrom(func() string {
		return "(?<![〇一二三四五六七八九十百千])([〇一二三四五六七八九十百千]+)(" + unitPatternSource(kanjiCounters) + ")"
	}, 0)
	// 漢数字の直前が漢字なら取らない (「統一」「一般」)。ただし直前がこの字なら取る
	kanjiChain = pyre.NewLazy(`[〇一二三四五六七八九十百千]+(?:時間|週間|時|分|人|名|回|件|個|枚)$`, 0)
	// 「十分」の直前の、漢数字に続いた「時」(十時十分)と、「時間」「人」「名」「回」「件」「個」「枚」「日」(一時間十分、一人十分、一日十分)と「毎日」(毎日十分)
	kanjiHourBefore = pyre.NewLazy(`[〇一二三四五六七八九十百千]+(?:時|(時間|人|名|回|件|個|枚|日))$|(毎日)$`, 0)
	// 漢数字を含むが数ではない語
	kanjiExcluded = pyre.NewLazy(`一時(?!間)|一度|一部|一方|一般|一緒|一切|一応|一番|一定|一致|一意|一括|一本化|一日中|一年中|一日も|`+
		`一人ひとり|一人一人|一回り|一枚岩|十分|十人十色|万一|四六時中`, 0)
	// 範囲や並べの前の漢数字 (三〜五日、三から五日、三、四日、三・四日 の「三」)
	kanjiRangeHead = pyre.NewLazy(`(?<![〇一二三四五六七八九十百千])([〇一二三四五六七八九十百千]+)`+
		`(?:[ \t]*(?:[~\u301c]|から)[ \t]*|[、・])$`, 0)
	// 範囲・日付・時刻のまとまりをつなぐ字
	notationJoin = pyre.NewLazy(`[ \t]*(?:[~\u301c]|から)[ \t]*|[-/:\u2010-\u2013\u2212]`, 0)
	joinRange    = pyre.NewLazy(`[~\u301c]|から`, 0)
	// 単位なしの数のあとの空白
	notationSpace = pyre.NewLazy(`[ \t]+`, 0)
	// 番号つき箇条書きの番号 (行頭の「1.」「2)」「(1)」「（１）」。引用の中も)
	listNumber = pyre.NewLazy(`^[ \t]*(?:>[ \t]*)*(?:[0-9]{1,9}[.)](?=[ \t]|$)|\([0-9]{1,9}\))`, pyre.MULTILINE)
	// 数量を主張しない「一つ」(一つの、一つずつ、もう一つ)
	weakOneMo = pyre.NewLazy(`も(?!ら|しく|って)(?=[^。！？!?\n]*?(?:な[いくかしけ]|ません|ず|ぬ|無))`, 0)
	// 平仮名の数
	kanaNumber           = pyre.NewLazy(`ここのつ|ひとつ|ふたつ|みっつ|よっつ|いつつ|むっつ|ななつ|やっつ|ひとり|ふたり`, 0)
	kanaNumberNotNext    = pyre.NewLazy(`まみ|かみ|ぶ(?!ん)|ながり|づき|き(?!り)|でに|ごと|よがり|け|り`, 0)
	kanaNumberNextStrict = []string{"の", "を", "が", "は", "も", "に", "で", "と", "だ", "め", "ずつ", "しか", "ほど", "まで", "くらい", "ぐらい", "さえ", "こそ", "じゃ", "あ"}
)

var (
	// 数値を取り出す前に同じ長さの空白で伏せる protected_spans の種類
	numberMaskedKindList = []string{"code", "inline_code", "autolink", "bare_url", "link_destination", "frontmatter", "html_block",
		"html_token", "reference_definition", "image"}
	numberMaskedKinds = func() map[string]bool {
		m := map[string]bool{}
		for _, k := range numberMaskedKindList {
			m[k] = true
		}
		return m
	}()

	kanjiDigits = map[rune]int{'〇': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	kanjiUnits  = map[rune]int{'十': 10, '百': 100, '千': 1000}
	// 漢数字の直前が漢字なら取らない。ただし直前がこの字なら取る
	kanjiBefore      = "約後計全第毎各残大小長短年月週日"
	kanjiBeforeWords = map[string]bool{"午前": true, "令和": true, "平成": true, "昭和": true, "大正": true, "明治": true}
	// 時刻を言う語 (朝九時、夜十時)。このあとの漢数字は、助数詞が「時」のときだけ取る
	timeOfDayWords = map[string]bool{"午前": true, "午後": true, "深夜": true, "未明": true, "早朝": true, "夕方": true, "毎朝": true, "今朝": true, "明朝": true,
		"毎晩": true, "今晩": true, "今夜": true, "毎夜": true, "夜中": true}
	timeOfDayChars    = "朝夜晩昼"
	ichijiHourNext    = "〇一二三四五六七八九十百半頃過前"
	kanjiNumerals     = "〇一二三四五六七八九十百千"
	kanjiChainReach   = 12
	kanjiRangeReach   = 16
	jippunAfter       = []string{"の", "、", "。", "，", "．", ",", ".", ")", "）", "」", "』", "\n", "ほど", "くらい", "ぐらい", "程度", "前", "後", "以", "未満", "まで", "から", "かか", "を", "も", "が", "は", "強", "弱", "余", "ずつ", "ごと", "おき", "とす", "とし", "だけ"}
	jippunDeNot       = []string{"です", "でき", "でし"}
	ichijiJippunAfter = []string{"まで", "から", "前", "後", "過ぎ", "すぎ", "ごろ", "頃", "以", "。", ")", "）", "」"}
	// 期間の語のあとの、同じ期間の漢数字の「一日」「一年」(今日一日、去年一年)
	wholePeriodWords = map[string]bool{"今日": true, "本日": true, "昨日": true, "明日": true, "終日": true, "当日": true, "前日": true, "翌日": true, "初日": true, "元日": true, "曜日": true,
		"今年": true, "去年": true, "昨年": true, "来年": true, "今月": true, "先月": true, "来月": true, "今週": true, "先週": true, "来週": true}
	samePeriod = map[rune][]string{'日': {"日"}, '年': {"年"}, '月': {"月", "か月", "ヶ月", "カ月", "ヵ月"}, '週': {"週間"}}
	// 漢数字の「分」「条」で、数量を言わないことのある慣用句
	bunNotBefore     = []string{"の一", "の二", "の三", "の四", "の五", "の六", "の七", "の八", "の九", "の十", "の百", "の千", "率", "位", "五分", "九厘"}
	nibunNotBefore   = []string{"し", "す", "さ", "化"}
	junibunNotBefore = []string{"に", "な"}
	jouNotBefore     = []string{"の光", "の煙"}

	dateUnits = []string{"年", "月", "日"}
	timeUnits = []string{"時", "分", "秒"}
	nextUnit  = map[string]string{"年": "月", "月": "日", "時": "分", "分": "秒"}

	weakOneAfter    = []string{"ひとつ", "一つ", "1つ"}
	weakOneBefore   = "もう"
	weakOneRepeated = []string{"一つ", "1つ"}
	weakOnePaired   = []string{"の", "ずつ"}
	weakOneOnly     = "のみ"

	kanaNumbers = map[string][2]string{"ひとつ": {"1", "つ"}, "ふたつ": {"2", "つ"}, "みっつ": {"3", "つ"}, "よっつ": {"4", "つ"},
		"いつつ": {"5", "つ"}, "むっつ": {"6", "つ"}, "ななつ": {"7", "つ"}, "やっつ": {"8", "つ"},
		"ここのつ": {"9", "つ"}, "ひとり": {"1", "人"}, "ふたり": {"2", "人"}}
	kanaNumberStrict       = map[string]bool{"いつつ": true, "ここのつ": true}
	kanaNumberNotKanjiNext = map[string]rune{"ひとり": '言'}
	kanaNumberBefore       = "をがはもにでとのやへ"
	kanaOnly               = "のみ"
)

// 数量を主張しない「一つ」の印 (number.weak)。真偽だけのもの (weakTrue) と、「一つの」(weakSingle)・「一つずつ」(weakEach)。
const (
	weakNone   = ""
	weakTrue   = "1"
	weakSingle = "single"
	weakEach   = "each"
)

// numKey は比べるときの (数, 単位)。
type numKey struct{ value, unit string }

// notation は単位なしの数の書き方の手がかり (本家の number.notation)。group はまとまりの中の単位なしの数の値の並び、follow は数のあとの空白に続く字 (8 字まで)、
// frame はまとまりの (数と数の間のつなぎの字の種類の並び, 単位の並び, この数の位置)。
type notation struct {
	group  []string
	follow string
	frame  *notationFrame
}

type notationFrame struct {
	joins []string
	units []string
	at    int
}

// number は取り出した数値 1 つ (本家の _Number)。cluster は範囲・日付・時刻のまとまり (2 つ以上のときだけ) の数の並び。
type number struct {
	start, end  int
	value, unit string
	notation    *notation
	weak        string
	cluster     []numKey
}

func (n number) key() numKey { return numKey{n.value, n.unit} }

func isKanjiChar(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FA5 || r == '々' || r == '〆' || r == 'ヵ' || r == 'ヶ'
}
func isKatakanaChar(r rune) bool { return r >= 0x30A1 && r <= 0x30F4 || r == 'ー' }
func isHiraganaChar(r rune) bool { return r >= 0x3041 && r <= 0x3096 }

func mustMatch(l *pyre.Lazy, t pyre.Text, pos int) *pyre.Match {
	m, err := l.Get().Match(t, pos, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m
}

func mustSubFunc(l *pyre.Lazy, t pyre.Text, repl func(*pyre.Match) string) string {
	s, err := l.Get().SubFunc(repl, t, 0)
	if err != nil {
		panic(err)
	}
	return s
}

func runesText(rs []rune) pyre.Text { return pyre.NewText(pyunicode.FromRunes(rs)) }

// hasPrefixAt は rs の at から始まる部分が prefix か (本家の str.startswith(prefix, at))。at が範囲の外なら空の接頭辞だけが当たる。
func hasPrefixAt(rs []rune, at int, prefix string) bool {
	p := pyunicode.Runes(prefix)
	if at < 0 || at+len(p) > len(rs) {
		return false
	}
	for i, r := range p {
		if rs[at+i] != r {
			return false
		}
	}
	return true
}

func hasAnyPrefixAt(rs []rune, at int, prefixes []string) bool {
	for _, p := range prefixes {
		if hasPrefixAt(rs, at, p) {
			return true
		}
	}
	return false
}

// runeSlice は Python のスライス rs[a:b] (a と b は 0 以上。範囲の外を詰める)。
func runeSlice(rs []rune, a, b int) string { return pyunicode.FromRunes(pySlice(rs, a, b)) }

// imagePathStart は画像の protected_spans (![代わりの文](パス)) の、パスの始まりの位置。パスがなければ right。
func imagePathStart(text []rune, left, right int) int {
	for i := left; i+1 < right; i++ {
		if text[i] == ']' && text[i+1] == '(' {
			return i + 2
		}
	}
	return right
}

// numberMask は数値を取り出す文字列。全角を半角にし、伏せる範囲 (コード、URL など) と番号つき箇条書きの番号を同じ長さの空白にする。
func numberMask(text []rune, a *visibility.Analysis, maskedKinds map[string]bool) []rune {
	chars := make([]rune, len(text))
	for i, r := range text {
		chars[i] = halfWidth(r)
	}
	for _, span := range a.ProtectedSpans {
		left, right := span.Start, span.End
		if span.Kind == "image" && maskedKinds[span.Kind] {
			left = imagePathStart(text, left, right)
		} else if !maskedKinds[span.Kind] {
			continue
		}
		for i := left; i < min(right, len(chars)); i++ {
			if chars[i] != '\n' {
				chars[i] = ' '
			}
		}
	}
	masked := runesText(chars)
	out := mustSubFunc(listNumber, masked, func(m *pyre.Match) string { return strings.Repeat(" ", m.End(0)-m.Start(0)) })
	return pyunicode.Runes(out)
}

// plainNumber は算用数字の文字列の頭の 0 を外す (「03」→「3」、「0.5」はそのまま)。
func plainNumber(digits string) string {
	whole, rest, found := strings.Cut(digits, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if found {
		return whole + "." + rest
	}
	return whole
}

// kanjiValue は漢数字を整数 (の 10 進の文字列) にする。十・百・千を使う書き方と、位取りの書き方 (二〇二六) を読む。読めない並び (二三、十十) は ok が偽。
func kanjiValue(text []rune) (string, bool) {
	hasUnit := false
	for _, r := range text {
		if _, ok := kanjiUnits[r]; ok {
			hasUnit = true
		}
	}
	if !hasUnit {
		var sb strings.Builder
		for _, r := range text {
			sb.WriteByte(byte('0' + kanjiDigits[r]))
		}
		n, _ := new(big.Int).SetString(sb.String(), 10)
		return n.String(), true
	}
	total, last := 0, 10000
	current := -1
	for _, r := range text {
		if unit, ok := kanjiUnits[r]; ok {
			if unit >= last {
				return "", false
			}
			if current < 0 {
				total += unit
			} else {
				total += current * unit
			}
			current, last = -1, unit
		} else {
			if current >= 0 || r == '〇' {
				return "", false
			}
			current = kanjiDigits[r]
		}
	}
	if current > 0 {
		total += current
	}
	return strconv.Itoa(total), true
}

func hourWordBefore(text []rune, start int) bool {
	return timeOfDayWords[runeSlice(text, max(0, start-2), start)] || (start > 0 && strings.ContainsRune(timeOfDayChars, text[start-1]))
}

// kanjiExcludedRepl は kanjiExcluded に当たった語を同じ長さの「＃」にする。「十分」は、直前が漢数字 (三十分) か、あとが「間」(十分間) なら残す。
// ただし「十分間に合う」の「間」は十分間にしない。「一時」は時刻 (十一時、午前一時) のときだけ残す。
func kanjiExcludedRepl(text []rune) func(*pyre.Match) string {
	return func(m *pyre.Match) string {
		word := runeSlice(text, m.Start(0), m.End(0))
		start, end := m.Start(0), m.End(0)
		if word == "十分" {
			if (start > 0 && strings.ContainsRune(kanjiNumerals, text[start-1])) ||
				(hasPrefixAt(text, end, "間") && !hasPrefixAt(text, end, "間に合")) {
				return word
			}
			hour, err := kanjiHourBefore.Get().Search(runesText(text), max(0, start-kanjiChainReach), start)
			if err != nil {
				panic(err)
			}
			if hour != nil {
				_, g1 := hour.Group(1)
				_, g2 := hour.Group(2)
				if !g1 && !g2 {
					whole, _ := hour.Group(0)
					if whole != "一時" || hourWordBefore(text, hour.Start(0)) || hasAnyPrefixAt(text, end, ichijiJippunAfter) {
						return word
					}
				} else if end == len(text) || hasAnyPrefixAt(text, end, jippunAfter) || (hasPrefixAt(text, end, "で") && !hasAnyPrefixAt(text, end, jippunDeNot)) {
					return word
				}
			}
		}
		if word == "一時" && start > 0 && (strings.ContainsRune(kanjiNumerals, text[start-1]) ||
			(hourWordBefore(text, start) && (end == len(text) || !isKanjiChar(text[end]) || strings.ContainsRune(ichijiHourNext, text[end])))) {
			return word
		}
		return strings.Repeat("＃", len([]rune(word)))
	}
}

// kanjiBeforeOK は kanjiText の start から始まる漢数字を、直前の字で数として取るか。直前が漢字でないか、kanjiBefore の字か、直前の 2 字が元号などの語か、
// 漢数字に続いた助数詞 (kanjiChain) なら取る。
func kanjiBeforeOK(text []rune, start int) bool {
	if start == 0 || !isKanjiChar(text[start-1]) || strings.ContainsRune(kanjiBefore, text[start-1]) {
		return true
	}
	if kanjiBeforeWords[runeSlice(text, max(0, start-2), start)] {
		return true
	}
	m, err := kanjiChain.Get().Search(runesText(text), max(0, start-kanjiChainReach), start)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// wholePeriod は start から始まる漢数字 (値が 1) が、期間の語のあとの同じ期間の数 (今日一日、去年一年、今週一週間) か、前に漢字のない「日一日」「年一年」なら真。
func wholePeriod(text []rune, start int, counter string) bool {
	if start == 0 {
		return false
	}
	counters := samePeriod[text[start-1]]
	found := false
	for _, c := range counters {
		if c == counter {
			found = true
		}
	}
	if !found {
		return false
	}
	if wholePeriodWords[runeSlice(text, max(0, start-2), start)] {
		return true
	}
	return (counter == "日" || counter == "年") && (start < 2 || !isKanjiChar(text[start-2]))
}

// kanjiHeadOK は kanjiRangeHead に当たった、範囲や並べの前の漢数字を数として取るか。
func kanjiHeadOK(text []rune, head *pyre.Match, taken [][2]int, found []number) (string, bool) {
	start := head.Start(1)
	if !kanjiBeforeOK(text, start) {
		return "", false
	}
	for _, t := range taken {
		if t[0] <= start && start < t[1] {
			return "", false
		}
	}
	for _, n := range found {
		if n.start <= start && start < n.end {
			return "", false
		}
	}
	return kanjiValue(text[head.Start(1):head.End(1)])
}

// kanaNumberOK は kanaNumber に当たった平仮名の数を、前後の字で数として取るか。
func kanaNumberOK(text []rune, m *pyre.Match) bool {
	word, _ := m.Group(0)
	end := m.End(0)
	strict := kanaNumberStrict[word]
	if strict && m.Start(0) > 0 {
		before := text[m.Start(0)-1]
		if isKanjiChar(before) || isKatakanaChar(before) || (isHiraganaChar(before) && !strings.ContainsRune(kanaNumberBefore, before)) {
			return false
		}
	}
	if word == "いつつ" && m.Start(0) > 0 && strings.ContainsRune("にで", text[m.Start(0)-1]) &&
		(end == len(text) || text[end] == 'も' || !isHiraganaChar(text[end])) {
		return false
	}
	if next, ok := kanaNumberNotKanjiNext[word]; ok && end < len(text) && text[end] == next {
		return false
	}
	if end == len(text) || !isHiraganaChar(text[end]) {
		return true
	}
	if mustMatch(kanaNumberNotNext, runesText(text), end) != nil {
		return false
	}
	return !strict || hasAnyPrefixAt(text, end, kanaNumberNextStrict)
}

func joinKind(between string) string {
	if between == "" {
		return "adjacent"
	}
	if joinRange.MustSearch(between) {
		return "range"
	}
	if strings.Contains(between, ":") {
		return "colon"
	}
	if strings.Contains(between, "/") {
		return "slash"
	}
	return "dash"
}

// withNotation は数値の並びに、範囲・日付・時刻のまとまり (cluster)、単位なしの数の書き方の手がかり (notation)、数量を主張しない「一つ」の印 (weak) を付ける。
func withNotation(masked []rune, found []number) []number {
	groups := make([]int, len(found))
	for i := range groups {
		groups[i] = i
	}
	joins := map[int][]string{}
	for index := 1; index < len(found); index++ {
		between := runeSlice(masked, found[index-1].end, found[index].start)
		if !strings.Contains(between, "\n") && (between == "" || notationJoin.MustFullMatch(between)) {
			groups[index] = groups[index-1]
			joins[groups[index]] = append(joins[groups[index]], joinKind(between))
		}
	}
	members := map[int][]int{}
	for index := range found {
		members[groups[index]] = append(members[groups[index]], index)
	}
	t := runesText(masked)
	out := make([]number, len(found))
	for index, n := range found {
		cluster := members[groups[index]]
		var keys []numKey
		if len(cluster) > 1 {
			for _, member := range cluster {
				keys = append(keys, found[member].key())
			}
		}
		var nt *notation
		weak := n.weak
		if n.unit == "" {
			var group []string
			if keys != nil {
				for _, member := range cluster {
					if found[member].unit == "" {
						group = append(group, found[member].value)
					}
				}
			}
			follow := ""
			if space := mustMatch(notationSpace, t, n.end); space != nil {
				follow = runeSlice(masked, space.End(0), space.End(0)+8)
				follow, _, _ = strings.Cut(follow, "\n")
			}
			if len(group) > 0 || follow != "" {
				var frame *notationFrame
				if len(group) > 0 {
					frame = &notationFrame{joins: joins[groups[index]]}
					for at, member := range cluster {
						frame.units = append(frame.units, found[member].unit)
						if member == index {
							frame.at = at
						}
					}
				}
				nt = &notation{group, follow, frame}
			}
		} else if n.value == "1" && n.unit == "つ" {
			if hasAnyPrefixAt(masked, n.end, weakOneAfter) || mustMatch(weakOneMo, t, n.end) != nil ||
				runeSlice(masked, max(0, n.start-2), n.start) == weakOneRepeated[0] || runeSlice(masked, max(0, n.start-2), n.start) == weakOneRepeated[1] ||
				runeSlice(masked, max(0, n.start-len([]rune(weakOneBefore))), n.start) == weakOneBefore {
				weak = weakTrue
			} else if hasAnyPrefixAt(masked, n.end, weakOnePaired) && !hasPrefixAt(masked, n.end, weakOneOnly) {
				weak = weakSingle
				if hasPrefixAt(masked, n.end, "ずつ") {
					weak = weakEach
				}
			}
		}
		n.notation, n.weak, n.cluster = nt, weak, keys
		out[index] = n
	}
	return out
}

// findNumbers は数値の取り出し方で、文書の中の数値を位置の順に返す。英字が続く数 (3D、5G、30s、8GiB) は、一覧の単位が続かなければ単位なしの数として取る。
// 数と単位の間の強調の記号 (**) は読み飛ばす。maskedKinds は伏せる protected_spans の種類 (空なら伏せない)。
func findNumbers(text []rune, a *visibility.Analysis, maskedKinds map[string]bool) []number {
	masked := numberMask(text, a, maskedKinds)
	t := runesText(masked)
	var found []number
	var taken [][2]int
	arabics, err := arabicNumber.Get().Finditer(t, 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	for _, m := range arabics {
		digits, _ := m.Group(0)
		after := m.End(0)
		var thousand *pyre.Match
		if !strings.Contains(digits, ",") && !strings.Contains(digits, ".") {
			thousand = mustMatch(thousandMark, t, after)
		}
		if thousand != nil {
			after = thousand.End(0)
		}
		unit := mustMatch(unitPattern, t, after)
		if unit == nil {
			if emphasis := mustMatch(emphasisMark, t, after); emphasis != nil {
				unit = mustMatch(unitPattern, t, emphasis.End(0))
			}
		}
		if unit == nil && thousand == nil {
			unit = mustMatch(spacedUnit, t, after)
		}
		if unit == nil && thousand == nil {
			unit = mustMatch(letterUnit, t, after)
		}
		unitText, follow := "", after
		if unit != nil {
			if idx, ok := unit.LastIndex(); ok {
				unitText, _ = unit.Group(idx)
			} else {
				unitText, _ = unit.Group(0)
			}
			follow = unit.End(0)
		}
		canon := func(u string) string {
			if s, ok := sameUnit[u]; ok {
				return s
			}
			return u
		}
		if thousand != nil {
			taken = append(taken, [2]int{m.Start(0), follow})
			n, _ := new(big.Int).SetString(digits, 10)
			found = append(found, number{start: m.Start(0), end: follow, value: n.Mul(n, big.NewInt(1000)).String(), unit: canon(unitText)})
			continue
		}
		if strings.Contains(digits, ",") && !thousandsRun.MustFullMatch(digits) {
			pieces := strings.Split(digits, ",")
			offset := m.Start(0)
			for index, piece := range pieces {
				last := index == len(pieces)-1
				end := offset + len(piece)
				u := ""
				if last {
					end, u = follow, canon(unitText)
				}
				found = append(found, number{start: offset, end: end, value: plainNumber(piece), unit: u})
				offset += len(piece) + 1
			}
			continue
		}
		found = append(found, number{start: m.Start(0), end: follow, value: plainNumber(strings.ReplaceAll(digits, ",", "")), unit: canon(unitText)})
	}
	kanjiRunes := pyunicode.Runes(mustSubFunc(kanjiExcluded, t, kanjiExcludedRepl(masked)))
	kt := runesText(kanjiRunes)
	kanjis, err := kanjiNumber.Get().Finditer(kt, 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	for _, m := range kanjis {
		counter, _ := m.Group(2)
		if !kanjiBeforeOK(kanjiRunes, m.Start(0)) && (counter != "時" || !hourWordBefore(kanjiRunes, m.Start(0))) {
			continue
		}
		skip := false
		for _, tk := range taken {
			if tk[0] <= m.Start(0) && m.Start(0) < tk[1] {
				skip = true
			}
		}
		if skip {
			continue
		}
		value, ok := kanjiValue(kanjiRunes[m.Start(1):m.End(1)])
		if !ok {
			continue
		}
		idiom := value == "1" && wholePeriod(kanjiRunes, m.Start(0), counter)
		switch counter {
		case "分":
			word, _ := m.Group(0)
			idiom = hasAnyPrefixAt(kanjiRunes, m.End(0), bunNotBefore) ||
				(word == "二分" && hasAnyPrefixAt(kanjiRunes, m.End(0), nibunNotBefore)) ||
				(word == "十二分" && hasAnyPrefixAt(kanjiRunes, m.End(0), junibunNotBefore)) ||
				(word == "五分" && strings.HasSuffix(runeSlice(kanjiRunes, 0, m.Start(0)), "五分"))
		case "条":
			idiom = hasAnyPrefixAt(kanjiRunes, m.End(0), jouNotBefore)
		}
		head, err := kanjiRangeHead.Get().Search(kt, max(0, m.Start(0)-kanjiRangeReach), m.Start(0))
		if err != nil {
			panic(err)
		}
		if head != nil {
			if headValue, ok := kanjiHeadOK(kanjiRunes, head, taken, found); ok {
				found = append(found, number{start: head.Start(1), end: head.End(1), value: headValue})
			}
		}
		unit := counter
		if s, ok := sameUnit[counter]; ok {
			unit = s
		}
		n := number{start: m.Start(0), end: m.End(0), value: value, unit: unit}
		if idiom {
			n.weak = weakTrue
		}
		found = append(found, n)
	}
	kanas, err := kanaNumber.Get().Finditer(kt, 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	for _, m := range kanas {
		if kanaNumberOK(kanjiRunes, m) {
			word, _ := m.Group(0)
			vu := kanaNumbers[word]
			n := number{start: m.Start(0), end: m.End(0), value: vu[0], unit: vu[1]}
			if !hasPrefixAt(kanjiRunes, m.End(0), kanaOnly) {
				n.weak = weakTrue
			}
			found = append(found, n)
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		return found[i].start < found[j].start || (found[i].start == found[j].start && found[i].end < found[j].end)
	})
	return withNotation(masked, found)
}

// NumberItem は ExtractNumbers の 1 件 (本家の extract_numbers の dict)。
type NumberItem struct {
	Text, Value, Unit string
	Start, End        int
}

// ExtractNumbers は文の中の数値を、位置の順に取り出す。コード、インラインのコード、URL、リンク先、先頭の設定、HTML の中の数は取らない。引用と表の中の数は取る。
// 表記だけの違い (全角と半角、3 桁区切りの有無、漢数字と算用数字、か月とヶ月) は Value と Unit が同じになる。
func ExtractNumbers(text string) ([]NumberItem, error) {
	s, err := newSides(text, "")
	if err != nil {
		return nil, err
	}
	runes := pyunicode.Runes(text)
	var out []NumberItem
	for _, n := range findNumbers(runes, s.analyses[0], numberMaskedKinds) {
		out = append(out, NumberItem{runeSlice(runes, n.start, n.end), n.value, n.unit, n.start, n.end})
	}
	return out, nil
}

// JSON は extract_numbers の戻り値の dict。
func (n NumberItem) JSON() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("text", n.Text)
	o.Set("value", n.Value)
	o.Set("unit", n.Unit)
	o.Set("start", int64(n.Start))
	o.Set("end", int64(n.End))
	return o
}
