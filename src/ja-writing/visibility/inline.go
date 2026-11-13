package visibility

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

var (
	tagName       = mustRegexp(`</?[A-Za-z][A-Za-z0-9-]*`)
	attributeName = mustRegexp(`[A-Za-z_:][A-Za-z0-9_.:-]*`)
	autolink      = mustRegexp("<(?:[A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\\x00-\\x20]*|" +
		"[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9]" +
		`(?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?` +
		`(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*>)`)
	bareURL        = mustRegexp(`(?:https?://|www\.)[^\s<>]+`)
	blankTitleLine = mustRegexp(`(?:\r\n|\r|\n)[ \t]*(?:\r\n|\r|\n)`)
	inlineEvents   = mustRegexp("[\\\\`<\\[\\]hw]")
	backtickRun    = mustRegexp("`+")
	urlDomainSplit = mustRegexp(`[/?:#]`)
	domainSegment  = mustRegexp(`[A-Za-z0-9_-]+`)
)

// terminatorTokens は _terminator_positions が位置を持つ終わりの印 (本家と同じ 4 つ)。
var terminatorTokens = []string{"-->", "?>", "]]>", ">"}

var terminatorPatterns = func() map[string]*pyre.Pattern {
	m := map[string]*pyre.Pattern{}
	for _, tok := range terminatorTokens {
		m[tok] = mustRegexp(pyre.Escape(tok))
	}
	return m
}()

// Span は保護された範囲 [Start, End) (コードポイントの添字) と種類。
type Span struct {
	Start, End int
	Kind       string
}

func compareSpans(a, b Span) int {
	switch {
	case a.Start != b.Start:
		return a.Start - b.Start
	case a.End != b.End:
		return a.End - b.End
	default:
		return strings.Compare(a.Kind, b.Kind)
	}
}

// source は 1 つの文字列を []rune と照合用の pyre.Text の両方で持つ (Text を作り直すと 1 回ごとに全体を変換する)。
type source struct {
	r []rune
	t pyre.Text
}

func newSource(r []rune) *source { return &source{r: r, t: pyre.NewText(str(r))} }

func isAnyOf(c rune, set string) bool { return strings.ContainsRune(set, c) }

// UnicodeWhitespace は unicode_whitespace。空文字列は真、2 字以上は全部の字が空白のとき真。空白は \t \n \r \f と一般カテゴリ Zs。
func UnicodeWhitespace(ch string) bool {
	rs := runesOf(ch)
	for _, c := range rs {
		if !unicodeWhitespaceRune(c) {
			return false
		}
	}
	return true
}

func unicodeWhitespaceRune(c rune) bool {
	return c == '\t' || c == '\n' || c == '\r' || c == '\f' || pyunicode.Category(c) == "Zs"
}

// Escaped は escaped を公開したもの (internal/bold の _direct_escape が同じ判定を使う)。
func Escaped(text []rune, position int) bool { return escaped(text, position) }

// escaped は _escaped (position の字が奇数個の \ で escape されているか)。
func escaped(text []rune, position int) bool {
	start := position
	for start > 0 && text[start-1] == '\\' {
		start--
	}
	return (position-start)%2 == 1
}

// tagSpaceEnd は _tag_space_end (空白・タブと、高々 1 つの Markdown の改行)。Python の isspace ではない。
func tagSpaceEnd(text []rune, start int) int {
	pos := start
	for pos < len(text) && isSpaceTab(text[pos]) {
		pos++
	}
	if hasPrefixAt(text, "\r\n", pos) {
		pos += 2
	} else if pos < len(text) && (text[pos] == '\r' || text[pos] == '\n') {
		pos++
	}
	for pos < len(text) && isSpaceTab(text[pos]) {
		pos++
	}
	return pos
}

// terminatorPositions は _terminator_positions (終わりの印ごとの出現位置)。
func terminatorPositions(s *source) map[string][]int {
	out := map[string][]int{}
	for _, tok := range terminatorTokens {
		positions := []int{}
		for _, m := range must(terminatorPatterns[tok].Finditer(s.t, 0, pyre.NoEnd)) {
			positions = append(positions, m.Start(0))
		}
		out[tok] = positions
	}
	return out
}

// tagEnd は _tag_end。タグ・注釈・処理命令・CDATA・宣言の終わりの添字 (閉じていなければ ok が偽)。
// terminators が nil のときは、終わりの印をその都度探す。
func tagEnd(s *source, start int, terminators map[string][]int) (int, bool) {
	text := s.r
	closed := func(token string, begin int) (int, bool) {
		var end int
		if terminators == nil {
			end = indexFrom(text, token, begin)
		} else {
			positions := terminators[token]
			index := sort.SearchInts(positions, begin)
			end = -1
			if index < len(positions) {
				end = positions[index]
			}
		}
		if end < 0 {
			return 0, false
		}
		return end + len(token), true
	}
	if hasPrefixAt(text, "<!--", start) {
		if hasPrefixAt(text, "<!-->", start) {
			return start + 5, true
		}
		if hasPrefixAt(text, "<!--->", start) {
			return start + 6, true
		}
		return closed("-->", start+4)
	}
	if hasPrefixAt(text, "<?", start) {
		return closed("?>", start+2)
	}
	if hasPrefixAt(text, "<![CDATA[", start) {
		return closed("]]>", start+9)
	}
	if hasPrefixAt(text, "<!", start) && start+2 < len(text) && isASCIILetter(text[start+2]) {
		return closed(">", start+3)
	}
	opening := matchAt(tagName, s.t, start)
	if opening == nil {
		return 0, false
	}
	pos := opening.End(0)
	closing := hasPrefixAt(text, "</", start)
	for pos < len(text) {
		spaceEnd := tagSpaceEnd(text, pos)
		if spaceEnd < len(text) && text[spaceEnd] == '>' {
			return spaceEnd + 1, true
		}
		if !closing && hasPrefixAt(text, "/>", spaceEnd) {
			return spaceEnd + 2, true
		}
		if closing || spaceEnd == pos {
			return 0, false
		}
		attribute := matchAt(attributeName, s.t, spaceEnd)
		if attribute == nil {
			return 0, false
		}
		pos = attribute.End(0)
		assignment := tagSpaceEnd(text, pos)
		if assignment >= len(text) || text[assignment] != '=' {
			continue // 同じ空白が次の属性の区切りになる。
		}
		pos = tagSpaceEnd(text, assignment+1)
		if pos >= len(text) {
			return 0, false
		}
		if text[pos] == '"' || text[pos] == '\'' {
			end := slices.Index(text[pos+1:], text[pos])
			if end < 0 {
				return 0, false
			}
			pos = pos + 1 + end + 1
		} else {
			begin := pos
			for pos < len(text) && !isAnyOf(text[pos], " \t\r\n\"'=<>`") {
				pos++
			}
			if pos == begin {
				return 0, false
			}
		}
	}
	return 0, false
}

func isASCIILetter(c rune) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// linkEnd は _link_end (インラインのリンクの宛先とタイトルの終わり。検証できなければ ok が偽)。opening は '(' の添字。
func linkEnd(s *source, opening int) (int, bool) {
	text := s.r
	n := len(text)
	pos := opening + 1
	for pos < n && isAnyOf(text[pos], " \t\n\r") {
		pos++
	}
	if pos >= n {
		return 0, false
	}
	if text[pos] == '<' {
		pos++
		broke := false
		for pos < n {
			if text[pos] == '\\' && pos+1 < n {
				pos += 2
				continue
			}
			if text[pos] == '>' {
				pos++
				broke = true
				break
			}
			if isAnyOf(text[pos], "<\n\r") {
				return 0, false
			}
			pos++
		}
		if !broke {
			return 0, false
		}
	} else {
		depth := 0
		for pos < n {
			ch := text[pos]
			if ch == '\\' && pos+1 < n {
				pos += 2
				continue
			}
			if ch == '(' {
				depth++
				if depth > 32 {
					return 0, false
				}
			} else if ch == ')' {
				if depth == 0 {
					return pos + 1, true
				}
				depth--
			} else if isAnyOf(ch, " \t\n\r") {
				if depth != 0 {
					return 0, false
				}
				break
			} else if ch < 32 || ch == '<' || ch == '>' {
				return 0, false
			}
			pos++
		}
		if depth != 0 {
			return 0, false
		}
	}
	spacesStart := pos
	for pos < n && isAnyOf(text[pos], " \t\n\r") {
		pos++
	}
	if pos < n && text[pos] == ')' {
		return pos + 1, true
	}
	if pos == spacesStart || pos >= n || !isAnyOf(text[pos], "\"'(") {
		return 0, false
	}
	closer := text[pos]
	if closer == '(' {
		closer = ')'
	}
	pos++
	broke := false
	for pos < n {
		if text[pos] == '\\' && pos+1 < n {
			pos += 2
			continue
		}
		if text[pos] == closer {
			pos++
			broke = true
			break
		}
		if matchAt(blankTitleLine, s.t, pos) != nil {
			return 0, false
		}
		if closer == ')' && text[pos] == '(' {
			return 0, false
		}
		pos++
	}
	if !broke {
		return 0, false
	}
	for pos < n && isAnyOf(text[pos], " \t\n\r") {
		pos++
	}
	if pos < n && text[pos] == ')' {
		return pos + 1, true
	}
	return 0, false
}

// InlineProtectedSpans は inline_protected_spans (コード・escape された記号・不透明なインラインの印。ラベルは残す)。(開始, 終わり, 種類) の昇順。
func InlineProtectedSpans(text string) []Span { return inlineProtectedSpans(newSource(runesOf(text))) }

func inlineProtectedSpans(s *source) []Span {
	text := s.r
	n := len(text)
	byWidth := map[int][][2]int{}
	startsByWidth := map[int][]int{}
	for _, m := range must(backtickRun.Finditer(s.t, 0, pyre.NoEnd)) {
		start, end := m.Span(0)
		byWidth[end-start] = append(byWidth[end-start], [2]int{start, end})
		startsByWidth[end-start] = append(startsByWidth[end-start], start)
	}
	spans := []Span{}
	var brackets []int
	var terminators map[string][]int
	if slices.Contains(text, '<') {
		terminators = terminatorPositions(s)
	}
	pos := 0
	for pos < n {
		// イベントの字以外はどのトークンも始められない。イベントだけを探しても、位置と状態は全部の字を見たときと同じ。
		event := searchFrom(inlineEvents, s.t, pos)
		if event == nil {
			break
		}
		pos = event.Start(0)
		ch := text[pos]
		if ch == '\\' && pos+1 < n && isASCIIPunct(text[pos+1]) {
			spans = append(spans, Span{pos, pos + 2, "escape"})
			pos += 2
			continue
		}
		if ch == '`' {
			end := pos + 1
			for end < n && text[end] == '`' {
				end++
			}
			width := end - pos
			positions := startsByWidth[width]
			index := sort.SearchInts(positions, end)
			if index < len(positions) {
				closing := byWidth[width][index][1]
				spans = append(spans, Span{pos, closing, "inline_code"})
				pos = closing
				continue
			}
			pos = end
			continue
		}
		if ch == '<' {
			if auto := matchAt(autolink, s.t, pos); auto != nil {
				end := auto.End(0)
				spans = append(spans, Span{pos, end, "autolink"})
				pos = end
				continue
			}
			if end, ok := tagEnd(s, pos, terminators); ok {
				spans = append(spans, Span{pos, end, "html_token"})
				pos = end
				continue
			}
		}
		if ch == '[' {
			brackets = append(brackets, pos)
		} else if ch == ']' && len(brackets) > 0 {
			opening := brackets[len(brackets)-1]
			brackets = brackets[:len(brackets)-1]
			if pos+1 < n && text[pos+1] == '(' {
				if end, ok := linkEnd(s, pos+1); ok {
					if opening > 0 && text[opening-1] == '!' && !escaped(text, opening-1) {
						spans = append(spans, Span{opening - 1, end, "image"})
					} else {
						spans = append(spans, Span{pos + 1, end, "link_destination"})
					}
					pos = end
					continue
				}
			}
		}
		// GFM の裸の URL は、強調の指示ではなく、文章の検査にかける材料。
		if hasPrefixAt(text, "http://", pos) || hasPrefixAt(text, "https://", pos) || hasPrefixAt(text, "www.", pos) {
			www := hasPrefixAt(text, "www.", pos)
			// 始まりになれない www は、後ろ全部を読む前に断る。
			if www && pos > 0 && !unicodeWhitespaceRune(text[pos-1]) && !isAnyOf(text[pos-1], "*_~(") {
				pos++
				continue
			}
			if match := matchAt(bareURL, s.t, pos); match != nil {
				end := match.End(0)
				// 新しい www の認識は、公開された GFM の境界と ASCII のドメインに限る。日本語に隣り合う場合の方針は広げない。
				if !www || validWWW(text, pos, match.Start(0), end) {
					// 末尾の Markdown の区切りと句読点は URL のデータではない。
					for end > pos && isAnyOf(text[end-1], "?!.,:*_~") {
						end--
					}
					excess := countRune(text[pos:end], ')') - countRune(text[pos:end], '(')
					for excess > 0 && end > pos && text[end-1] == ')' {
						end--
						excess--
					}
					spans = append(spans, Span{pos, end, "bare_url"})
					pos = end
					continue
				}
			}
		}
		pos++
	}
	slices.SortFunc(spans, compareSpans)
	return spans
}

// validWWW は www. で始まる裸の URL が GFM の境界と ASCII のドメインの条件を満たすか (本家の valid_www)。matchStart..matchEnd が一致の範囲。
func validWWW(text []rune, pos, matchStart, matchEnd int) bool {
	matched := newSource(text[matchStart:matchEnd])
	domain := must(urlDomainSplit.Split(matched.t, 1))[0].Value
	segments := strings.Split(domain, ".")
	if len(segments) < 2 {
		return false
	}
	for _, part := range segments {
		if !fullMatches(domainSegment, runesOf(part)) {
			return false
		}
	}
	for _, part := range segments[max(len(segments)-2, 0):] {
		if strings.Contains(part, "_") {
			return false
		}
	}
	return pos == 0 || unicodeWhitespaceRune(text[pos-1]) || isAnyOf(text[pos-1], "*_~(")
}

func countRune(r []rune, c rune) int {
	n := 0
	for _, x := range r {
		if x == c {
			n++
		}
	}
	return n
}
