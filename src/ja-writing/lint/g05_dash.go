package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G5 ダッシュ (dash_decoration・dash_insertion・dash_list_ending。本家 yomiyasu_lint.py の 1005-1182 と 5694-5698)。
// 1 文書の件数の上限 (4 件目以降) は post.go の applyDashLimit が持つので、ここでは全部足す。

// g05State は G5 が文書の中で持つ状態。1 行ごとに決まるので、持つものは無い。
type g05State struct{}

// dashChars は本家の DASH_CHARS (em dash・horizontal bar・罫線)。en dash・長音・波ダッシュは含めない。
const dashChars = "—―─━"

const dashListItem = `[^、。！？!?\s` + dashChars + `]{1,12}`

var (
	// 木構造や ASCII 図に使う罫線。これを含む行は図とみなす。
	diagramChars = newLazy(`[│┃├┣└┗┌┏┐┓┘┛┬┳┴┻┼╋╭╮╰╯]`, 0)
	// 短い語を読点で 3 つ以上並べ、ダッシュで閉じて文を終える形。文ごとに見る。
	dashListEnding = newLazyFrom(func() string {
		return `(?:^|(?<=[。！？!?]))\s*` + dashListItem + `(?:、[ \t\u3000]*` + dashListItem + `){2,}[ \t\u3000]*[` + dashChars + `]+` + endDecorationRun() + `(?:[。．.]|$)`
	}, 0)
	// 出典ラベル、著者と書名、英語の著者名と書誌情報。
	attributionLabel   = newLazy(`^(?:出典|引用元|著者|Source|Author)\s*[:：]`, pyre.IGNORECASE)
	attributionBook    = newLazy(`^[一-龥々ァ-ヴーA-Za-z .・'’-]+『[^』]+』(?:[ \t\u3000]*(?:（[^）]*）|\([^)]*\)))*[。.]?$`, 0)
	attributionEnglish = newLazy(`^[A-Z][A-Za-z.'’−-]*(?:[ \t]+[A-Z][A-Za-z.'’−-]*)+,[ \t]+\S`, 0)
	// ダッシュで補足や言い換えを挟む形。
	dashInsertion = newLazy(`[`+dashChars+`]{1,2}[^`+dashChars+`。\n]{1,40}[`+dashChars+`]{1,2}`, 0)
	// リンクの直後に著者などを添える区切り (表示上のテキストでは URL が空白になる)。
	linkAttribution = newLazy(`\][ \t]*[`+dashChars+`]+`, 0)
)

// nonRouteEndings は「人間」「作業時間」「目線」などの普通名詞 (区間を表す接尾辞と取り違えない)。
var nonRouteEndings = []string{"人間", "時間", "期間", "空間", "世間", "仲間", "手間", "目線", "視線", "動線", "光線", "伏線", "生命線", "方便", "利便", "不便"}

const (
	dashListEndingMessage = "名詞を読点で並べて末尾を「──」などのダッシュで閉じる書き方が検出されました。余韻を装う装飾に見えやすいため、" +
		"何についての列挙かを述語で言い切ってください。文脈から述語が分からない場合は、推測で補わず書き手に確かめてください。"
	dashInsertionMessage = "補足や言い換えをダッシュで挟む書き方が検出されました。読点や助詞、括弧でつなぐか、文を分けてください。" +
		"ただし、小説や随筆で書き手が文体として使っている場合や、引用は残してかまいません。"
	dashDecorationMessage = "ダッシュ記号（—、―、──など）が検出されました。補足や余韻のための装飾であれば、読点や助詞、括弧でつなぐか、" +
		"文を分けてください。ただし、小説や随筆で書き手が文体として使っている場合や、引用は残してかまいません。"
)

// g05Dash は表と引用でない行で、文書にダッシュの字があるとき (s.dashPossible) だけ呼ばれる。
func (s *lintState) g05Dash(r *rowCtx) {
	if f, ok := dashFinding(r.plainText, r.lineNo, r.stripped); ok {
		s.add(f)
	}
}

// isDashAttribution は本家の _is_dash_attribution。ダッシュで始まる出典の行 (「── 出典: …」)。
func isDashAttribution(text string) bool {
	text = pystr.Strip(text)
	if text == "" || !strings.ContainsRune(dashChars, pyunicode.Runes(text)[0]) {
		return false
	}
	credit := pystr.LStripChars(text, dashChars+" \t\u3000")
	return mustMatchAt0(attributionLabel, credit) || mustMatchAt0(attributionBook, credit) || mustMatchAt0(attributionEnglish, credit)
}

// isRouteNounChar は _mask_route_dashes の noun_char (漢字・カタカナ・長音・々)。
func isRouteNounChar(r rune) bool {
	return (r >= '一' && r <= '龥') || (r >= 'ァ' && r <= 'ヴ') || r == 'ー' || r == '々'
}

// maskRouteDashes は本家の _mask_route_dashes。「東京―大阪間」のような区間を表すダッシュ (名詞の間の 1 字で、あとに「間・線・便」で終わる名詞が続く) を空白にする。
// 「人間」「時間」などの普通名詞は区間とみなさない。
func maskRouteDashes(text string) string {
	rs := pyunicode.Runes(text)
	out := append([]rune(nil), rs...)
	for left := 0; left < len(rs); {
		if !strings.ContainsRune(dashChars, rs[left]) {
			left++
			continue
		}
		right := left
		for right < len(rs) && strings.ContainsRune(dashChars, rs[right]) {
			right++
		}
		start := left
		left = right
		if right-start != 1 || start == 0 || right == len(rs) || !isRouteNounChar(rs[start-1]) || !isRouteNounChar(rs[right]) {
			continue
		}
		end := right
		for end < len(rs) && isRouteNounChar(rs[end]) {
			end++
		}
		destination := rs[right:end]
		for index, char := range destination {
			if char != '間' && char != '線' && char != '便' {
				continue
			}
			upTo := string(destination[:index+1])
			suffixLength := 1
			if strings.HasSuffix(upTo, "区間") {
				suffixLength = 2
			}
			if index+1 > suffixLength && !slices.ContainsFunc(nonRouteEndings, func(e string) bool { return strings.HasSuffix(upTo, e) }) {
				out[start] = ' '
				break
			}
		}
	}
	return pyunicode.FromRunes(out)
}

// dashFinding は本家の _dash_finding。ダッシュの装飾を 1 行に 1 件指摘する (図・区切り線・出典の行は散文ではないので出さない)。
func dashFinding(text string, lineNo int, snippet string) (finding, bool) {
	if !strings.ContainsAny(text, dashChars) {
		return finding{}, false
	}
	if isDashAttribution(text) {
		text = pystr.LStripChars(pystr.LStrip(text), dashChars)
	}
	linked, err := linkAttribution.get().Sub("]", pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	text = pystr.Strip(maskRouteDashes(maskQuotedSegments(linked)))
	rest := pystr.StripChars(text, dashChars+" \u3000")
	if !strings.ContainsAny(text, dashChars) || mustSearch(diagramChars, text) || rest == "" {
		return finding{}, false
	}
	// 「――◆――」のように記号だけを挟んだ区切り線や、出典表記の行は散文ではない。
	onlySymbols := true
	for _, ch := range pyunicode.Runes(rest) {
		if !strings.ContainsRune("PSZ", rune(pyunicode.Category(ch)[0])) && !strings.ContainsRune(dashChars, ch) {
			onlySymbols = false
			break
		}
	}
	if onlySymbols {
		return finding{}, false
	}
	rule, message := "dash_decoration", dashDecorationMessage
	switch {
	case mustSearch(dashListEnding, text):
		rule, message = "dash_list_ending", dashListEndingMessage
	case mustSearch(dashInsertion, text):
		rule, message = "dash_insertion", dashInsertionMessage
	}
	return newFinding(rule, lineNo, "warn", message, snippet), true
}
