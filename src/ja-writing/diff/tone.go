package diff

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/lint"
	"ja-writing/visibility"
)

// 群 AA: 勢い (本家の tone・_tone・_tone_lines・_tone_places・_tone_excerpt・_mark_spans・_pseudo_heading・_heading_chars・_indirect_question_drop・_tone_level)。
// 「！」「？」・絵文字・強める語の数が原文より減ったら、減った行の前後を並べて候補にする。見出しの字数の合計が原文の 7 割未満になったら候補にする。
// 絵文字は lint の数え方 (lint.EmojiMatches) を使う (本家の、lint を読めないときの簡単な数え方 _FALLBACK_EMOJI は写さない)。

const (
	toneHeadingRatio = 0.7 // 見出しの字数の合計が、原文のこの割合未満になったら候補にする
	toneExcerptWidth = 20  // 位置の抜き出しの、印の前後の字数
)

// 参考 (info) にする文書の種類 (本家の TONE_INFO_DOC_TYPES)。
var toneInfoDocTypes = []string{"pr", "article", "incident", "manual", "formal", "paper"}

// 種類を指定していて promo でないとき、参考にする候補 (本家の TONE_TYPED_INFO_SUBKINDS) と、候補ごとに参考にしない種類 (TONE_TYPED_REVIEW_DOC_TYPES)。
var (
	toneTypedInfoSubkinds   = []string{"emoji", "heading"}
	toneTypedReviewDocTypes = map[string][]string{"emoji": {"promo", "mail"}, "heading": {"promo", "minutes", "mail"}}
)

// 数えない行の種類 (本家の _TONE_SKIP_KINDS) と、数える印の種類 (_TONE_KINDS)。
var (
	toneSkipKinds = map[string]bool{"code": true, "frontmatter": true, "html_block": true, "table": true, "reference_definition": true,
		"thematic_break": true, "blank": true, "heading_underline": true}
	toneKinds = []string{"exclamation", "question", "emoji", "intensifier"}
)

const sentenceEndMarks = "。．！？!?"

var (
	// 間接の問い (「予算が足りるかとの質問」「足りるかという声」「進めてよいかどうか」「足りるか否か」「直せるかと聞かれた」)。
	// 「ほかと」「確かと」「何かと」と、推量の「〜かと思います」は問いではない。「かと」と問う語の間には、句読点を除く 6 字までを許す
	indirectQuestion = pyre.NewLazy(`(?<![ほ確何])か(?:との|という|といった|どうか|否か|と[^。、！？!?\n]{0,6}?(?:聞|尋|問|質問|相談|訊|たず|き[いか]))`, 0)

	exclamationMark = pyre.NewLazy(`[！!]`, 0)
	questionMark    = pyre.NewLazy(`[？?]`, 0)
	// 強める語の数え方から外す語 (「絶対値」「絶対パス」は強める語ではない)
	intensifierExcluded = pyre.NewLazy(`絶対(?:値|パス|参照|座標|位置|時間|温度|音感|零度|的)`, 0)
	// 絵文字かもしれない字 (U+2000 以上で、CJK の文字と記号、全角の英数字と記号を除く)。ない行では絵文字の数え方を呼ばない
	maybeEmoji = pyre.NewLazyFrom(func() string {
		return fmt.Sprintf("[%c-%c%c-%c%c-%c]", 0x2000, 0x2fff, 0xa000, 0xfeff, 0xfff0, 0x10ffff)
	}, 0)
	// 紹介記事・宣伝文で見出しとして書かれることの多い行 (番号つき項目の行。段落の行の全体を太字にした行は見出しの構造 (headings.go) の boldOnlyLine と同じ正規表現)
	orderedItemLine = pyre.NewLazy(`^[ \t]{0,3}\d{1,9}[.)][ \t]+(.+)$`, 0)
	// 空白 (str.isspace の字) と BOM
	spacePattern = pyre.NewLazyFrom(func() string { return fmt.Sprintf("[\\s%c]+", 0xfeff) }, 0)
)

// toneLevel は候補の level (本家の _tone_level)。参考の種類はすべて info。種類を指定していて promo でなければ、絵文字と見出しは info
// (minutes の見出しは review のまま)。
func toneLevel(docType *string, subkind string) string {
	if docType != nil && slices.Contains(toneInfoDocTypes, *docType) {
		return "info"
	}
	if docType != nil && slices.Contains(toneTypedInfoSubkinds, subkind) && !slices.Contains(toneTypedReviewDocTypes[subkind], *docType) {
		return "info"
	}
	return "review"
}

// intensifierPattern は強める語の正規表現 (本家の _intensifier_pattern。語が無ければ nil)。長い語を先に並べる
// (同じ長さの語は同じ位置で同時に当たらないので、並びは答えを変えない)。
func intensifierPattern(words []string) *pyre.Lazy {
	var uniq []string
	for _, w := range words {
		if w != "" && !slices.Contains(uniq, w) {
			uniq = append(uniq, w)
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	slices.SortStableFunc(uniq, func(a, b string) int { return pystr.Len(b) - pystr.Len(a) })
	escaped := make([]string, len(uniq))
	for i, w := range uniq {
		escaped[i] = pyre.Escape(w)
	}
	return pyre.NewLazy(strings.Join(escaped, "|"), 0)
}

// maskExcludedIntensifiers は強める語として数えない語を、同じ長さの別の字に置き換えた文 (本家の _INTENSIFIER_EXCLUDED.sub)。
func maskExcludedIntensifiers(text string) string {
	out, err := intensifierExcluded.Get().SubFunc(func(m *pyre.Match) string {
		s, _ := m.Group(0)
		return strings.Repeat("＃", pystr.Len(s))
	}, pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	return out
}

// headingChars は見出しの字数 (空白と絵文字を除く。本家の _heading_chars)。絵文字を外しただけの見出しを「見出しが短くなった」に数えない。
func headingChars(text string) int {
	if maybeEmoji.MustSearch(text) {
		for _, found := range lint.EmojiMatches(text) {
			text = strings.Replace(text, found, "", 1)
		}
	}
	return pystr.Len(spacePattern.MustSub("", text))
}

// PseudoHeading は番号つき項目の行と太字だけの行の、見出しとして数える文字列 (番号、太字の印を除く。本家の _pseudo_heading)。当たらなければ ok が偽。
func PseudoHeading(line, kind string) (heading string, ok bool) {
	t := pyre.NewText(line)
	switch kind {
	case "list":
		if m, err := orderedItemLine.Get().Match(t, 0, pyre.NoEnd); err != nil {
			panic(err)
		} else if m != nil {
			item, _ := m.Group(1)
			if inner, err := boldOnlyLine.Get().Match(pyre.NewText(item), 0, pyre.NoEnd); err != nil {
				panic(err)
			} else if inner != nil {
				s, _ := inner.Group(1)
				return s, true
			}
			return item, true
		}
	case "paragraph":
		if m, err := boldOnlyLine.Get().Match(t, 0, pyre.NoEnd); err != nil {
			panic(err)
		} else if m != nil {
			s, _ := m.Group(1)
			return s, true
		}
	}
	return "", false
}

// toneCounts は 1 行の数 (本家の _tone_lines の値)。
type toneCounts struct {
	exclamation, question, emoji, intensifier, headingChars int
	words                                                   map[string]int
	text                                                    string
}

func (c *toneCounts) count(kind string) int {
	switch kind {
	case "exclamation":
		return c.exclamation
	case "question":
		return c.question
	case "emoji":
		return c.emoji
	case "intensifier":
		return c.intensifier
	}
	return c.headingChars
}

// toneLines は行番号 (1 から) ごとの数。数えない行 (コード、表など) は入れない。行番号の昇順に並べる。
type toneLines struct {
	order  []int
	counts map[int]*toneCounts
}

// toneLinesOf は本家の _tone_lines。行の中のインラインのコード、URL、リンク先、画像、HTML のタグは伏せる。pseudo が真なら (--doc-type promo)、
// 番号つき項目の行と太字だけの行も見出しの字数に数える。
func (s *sides) toneLinesOf(side int, intensifiers *pyre.Lazy, pseudo bool) *toneLines {
	m := s.newMaskerWith(side, toneSkipKinds)
	out := &toneLines{counts: map[int]*toneCounts{}}
	for index, line := range s.lines[side] {
		kind := "paragraph"
		if index < len(m.rows) {
			kind = m.rows[index].Kind
		}
		if toneSkipKinds[kind] {
			continue
		}
		text := m.mask(line, s.starts[side][index])
		if pystr.Strip(text) == "" {
			continue
		}
		t := pyre.NewText(text)
		c := &toneCounts{exclamation: countFound(exclamationMark, t), question: countFound(questionMark, t), words: map[string]int{}, text: line}
		if maybeEmoji.MustSearch(text) {
			c.emoji = len(lint.EmojiMatches(text))
		}
		if intensifiers != nil {
			for _, match := range mustFinditer(intensifiers, pyre.NewText(maskExcludedIntensifiers(text)), 0, pyre.NoEnd) {
				w, _ := match.Group(0)
				c.words[w]++
				c.intensifier++
			}
		}
		if kind == "heading" {
			c.headingChars = headingChars(atxMarkers.MustSub("", line))
		} else if pseudo {
			if heading, ok := PseudoHeading(line, kind); ok {
				c.headingChars = headingChars(heading)
			}
		}
		out.order = append(out.order, index+1)
		out.counts[index+1] = c
	}
	return out
}

func (l *toneLines) totals() map[string]int {
	found := map[string]int{}
	for _, no := range l.order {
		c := l.counts[no]
		for _, kind := range toneKinds {
			found[kind] += c.count(kind)
		}
		found["heading_chars"] += c.headingChars
	}
	return found
}

func totalsValue(t map[string]int) pyjson.Value {
	o := pyjson.NewObject()
	for _, kind := range toneKinds {
		o.Set(kind, int64(t[kind]))
	}
	o.Set("heading_chars", int64(t["heading_chars"]))
	return o
}

// MarkSpans は行の中の、数えた印 (「！」「？」、絵文字、強める語) の位置 [(始め, 終わり)] (本家の _mark_spans)。抜き出しの中心を決めるためだけに使う
// (行は伏せる前の形なので、インラインのコードの中の印も入る)。
func MarkSpans(text, kind string, intensifiers *pyre.Lazy) [][2]int {
	var spans [][2]int
	add := func(ms []*pyre.Match) {
		for _, m := range ms {
			spans = append(spans, [2]int{m.Start(0), m.End(0)})
		}
	}
	t := pyre.NewText(text)
	switch kind {
	case "exclamation":
		add(mustFinditer(exclamationMark, t, 0, pyre.NoEnd))
	case "question":
		add(mustFinditer(questionMark, t, 0, pyre.NoEnd))
	case "intensifier":
		if intensifiers != nil {
			add(mustFinditer(intensifiers, pyre.NewText(maskExcludedIntensifiers(text)), 0, pyre.NoEnd))
		}
	case "emoji":
		if maybeEmoji.MustSearch(text) {
			// found を探す位置 (cursor) と返す位置は code point。Go の文字列の探し方は byte なので、残りを切り出して数え直す
			cursor := 0
			for _, found := range lint.EmojiMatches(text) {
				rest := pyunicode.FromRunes(pyunicode.Runes(text)[cursor:])
				if at := strings.Index(rest, found); at >= 0 {
					pos := cursor + pystr.Len(rest[:at])
					spans = append(spans, [2]int{pos, pos + pystr.Len(found)})
					cursor = pos + pystr.Len(found)
				}
			}
		}
	}
	return spans
}

// floorDiv は Python の // (負の数は小さいほうへ丸める)。b は正。
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// ToneExcerpt は減った印のまわりの抜き出し (前後 width 字。切った側に「…」。本家の _tone_excerpt)。印は、変わった所に重なる最初のもの (なければ最初のもの)。
// 推敲後の側は、同じ位置 (変わった所の中なら、変わった所の長さの割合で対応させた位置) のまわり。前後 width 字の中で、印の前の文の終わりのあとから始め、
// 印のあとの最初の文の終わりで止める (印が「！」「？」なら印で止める)。どちらも空にしない。
//
// 印が共通の前の部分をまたぐと、推敲後の側の位置が負になる (割合で対応させた位置の割り算が負の数)。本家は Python の添字なので、負の位置を後ろから数え、
// 文字列の長さを超える負の位置は IndexError (本家の tone() ごと失敗する。印が「とても」で、元の文が「とても」だけ・推敲後が「ともかく」で始まる長い文、など)。
// 負の位置の添字・切り出しを同じ規則で写し、IndexError は error で返す。
func ToneExcerpt(oldText, newText string, spans [][2]int, width int) (string, string, error) {
	a, b := pyunicode.Runes(oldText), pyunicode.Runes(newText)
	head, tail := commonAffix(a, b)
	oldEnd, newEnd := len(a)-tail, len(b)-tail
	var start, end int
	if len(spans) > 0 {
		start, end = spans[0][0], spans[0][1]
		for _, sp := range spans {
			if sp[0] < max(oldEnd, head+1) && sp[1] > head {
				start, end = sp[0], sp[1]
				break
			}
		}
	} else {
		start, end = head, head
	}
	var newStart, newEndAt int
	switch {
	case end <= head:
		newStart, newEndAt = start, end
	case start >= oldEnd:
		newStart, newEndAt = start+len(b)-len(a), end+len(b)-len(a)
	default:
		newStart = head + floorDiv((start-head)*(newEnd-head), max(1, oldEnd-head))
		newEndAt = newStart
	}
	first, err := toneCut(a, start, end, width)
	if err != nil {
		return "", "", err
	}
	second, err := toneCut(b, newStart, newEndAt, width)
	if err != nil {
		return "", "", err
	}
	return first, second, nil
}

// pyIndexError は Python の str の添字が範囲の外のときの IndexError。
func pyIndexError() error {
	return &visibility.Error{Type: visibility.TypeIndexError, Message: "string index out of range"}
}

// pyAt は text[i] (負の添字は後ろから数える。範囲の外は IndexError)。
func pyAt(text []rune, i int) (rune, error) {
	if i < 0 {
		i += len(text)
	}
	if i < 0 || i >= len(text) {
		return 0, pyIndexError()
	}
	return text[i], nil
}

// pySliceAny は text[a:b] (負の添字は後ろから数え、範囲の外は詰める)。
func pySliceAny(text []rune, a, b int) []rune {
	norm := func(i int) int {
		if i < 0 {
			return max(0, i+len(text))
		}
		return min(i, len(text))
	}
	a, b = norm(a), norm(b)
	if a >= b {
		return nil
	}
	return text[a:b]
}

// toneCut は本家の _tone_excerpt の中の cut: 印 [left, right) の前後 width 字を、文の終わりで切った抜き出し。
func toneCut(text []rune, left, right, width int) (string, error) {
	first, last := max(0, left-width), min(len(text), right+width)
	atEnd := false
	if right > left {
		c, err := pyAt(text, right-1)
		if err != nil {
			return "", err
		}
		atEnd = strings.ContainsRune(sentenceEndMarks, c)
	}
	if atEnd {
		last = right
	} else {
		for index := right; index < last; index++ {
			c, err := pyAt(text, index)
			if err != nil {
				return "", err
			}
			if strings.ContainsRune(sentenceEndMarks, c) {
				last = index + 1
				break
			}
		}
	}
	for index := left - 1; index >= first; index-- {
		c, err := pyAt(text, index)
		if err != nil {
			return "", err
		}
		if strings.ContainsRune(sentenceEndMarks, c) {
			if pystr.Strip(pyunicode.FromRunes(pySliceAny(text, index+1, last))) != "" {
				first = index + 1
			}
			break
		}
	}
	part := pystr.Strip(pyunicode.FromRunes(pySliceAny(text, first, last)))
	if part == "" {
		whole := []rune(pystr.Strip(pyunicode.FromRunes(text)))
		return string(whole[:min(2*width, len(whole))]), nil
	}
	if first > 0 {
		part = "…" + part
	}
	if last < len(text) {
		part += "…"
	}
	return part, nil
}

// indirectQuestionDrop は組になった文で、推敲後の文が間接の問い (indirectQuestion) になったときに減った「？」の数の合計 (本家の _indirect_question_drop)。
// コード、表などの数えない行の文と、伏せる範囲の中の「？」は数えない (toneLinesOf と同じ)。
func (s *sides) indirectQuestionDrop(groups []*sentenceGroup) int {
	maskers := [2]*masker{s.newMaskerWith(0, toneSkipKinds), s.newMaskerWith(1, toneSkipKinds)}
	dropped := 0
	for _, g := range groups {
		for _, p := range g.pairs {
			o, ok1 := maskers[0].body(g.old[p[0]])
			n, ok2 := maskers[1].body(g.new[p[1]])
			if !ok1 || !ok2 {
				continue
			}
			before, after := countFound(questionMark, pyre.NewText(o.text)), countFound(questionMark, pyre.NewText(n.text))
			if before > after && indirectQuestion.MustSearch(n.text) {
				dropped += before - after
			}
		}
	}
	return dropped
}

// tonePlace は数が減った hunk の、減った行の前後。new_line と new は、組になる行が無いとき null。
type tonePlace struct {
	oldLine int
	newLine *int
	oldText string
	newText *string
}

func (p tonePlace) value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("old_line", int64(p.oldLine))
	o.Set("new_line", optionalInt(p.newLine))
	o.Set("old", p.oldText)
	o.Set("new", pyjson.OptionalString(p.newText))
	return o
}

type toneLine struct {
	no int
	c  *toneCounts
}

func (l *toneLines) between(first, last int) []toneLine {
	var out []toneLine
	for no := first; no <= last; no++ {
		if c, ok := l.counts[no]; ok {
			out = append(out, toneLine{no, c})
		}
	}
	return out
}

func sumCounts(lines []toneLine, kind string) int {
	n := 0
	for _, l := range lines {
		n += l.c.count(kind)
	}
	return n
}

// tonePlaces は数が減った hunk ごとの、減った行の前後 (本家の _tone_places)。kind が heading_chars なら見出しの行だけを比べる。
// 長い行 (40 字を超える) は、減った印のまわりだけを抜き出す。
func tonePlaces(full *fullDiff, oldLines, newLines *toneLines, kind string, intensifiers *pyre.Lazy) ([]tonePlace, error) {
	var places []tonePlace
	for _, h := range full.hunks {
		olds, news := oldLines.between(h.oldStart, h.oldEnd), newLines.between(h.newStart, h.newEnd)
		if kind == "heading_chars" {
			withHeading := func(lines []toneLine) []toneLine {
				var out []toneLine
				for _, l := range lines {
					if l.c.headingChars != 0 {
						out = append(out, l)
					}
				}
				return out
			}
			olds = withHeading(olds)
			if kept := withHeading(news); len(kept) > 0 {
				news = kept
			}
		}
		if sumCounts(olds, kind) <= sumCounts(news, kind) {
			continue
		}
		var chosen *toneLine
		var chosenPartner *toneLine
		for offset, l := range olds {
			if l.c.count(kind) == 0 {
				continue
			}
			var partner *toneLine
			switch {
			case offset < len(news):
				partner = &news[offset]
			case len(news) > 0:
				partner = &news[len(news)-1]
			}
			if partner == nil || partner.c.count(kind) < l.c.count(kind) {
				chosen, chosenPartner = &olds[offset], partner
				break
			}
			if chosen == nil {
				chosen, chosenPartner = &olds[offset], partner
			}
		}
		oldText := pystr.Strip(chosen.c.text)
		if chosenPartner == nil {
			if kind != "heading_chars" && pystr.Len(oldText) > 2*toneExcerptWidth {
				var err error
				if oldText, _, err = ToneExcerpt(oldText, "", MarkSpans(oldText, kind, intensifiers), toneExcerptWidth); err != nil {
					return nil, err
				}
			}
			places = append(places, tonePlace{oldLine: chosen.no, oldText: oldText})
			continue
		}
		newText := pystr.Strip(chosenPartner.c.text)
		if kind != "heading_chars" && (pystr.Len(oldText) > 2*toneExcerptWidth || pystr.Len(newText) > 2*toneExcerptWidth) {
			var err error
			if oldText, newText, err = ToneExcerpt(oldText, newText, MarkSpans(oldText, kind, intensifiers), toneExcerptWidth); err != nil {
				return nil, err
			}
		}
		places = append(places, tonePlace{oldLine: chosen.no, newLine: intPtr(chosenPartner.no), oldText: oldText, newText: &newText})
	}
	return places, nil
}

// toneCandidate は勢いの候補 (本家の dict と同じ順の鍵)。
func toneCandidate(subkind string, places []tonePlace, detail *pyjson.Object, level string) pyjson.Value {
	var first tonePlace
	hasFirst := len(places) > 0
	if hasFirst {
		first = places[0]
	}
	o := pyjson.NewObject()
	o.Set("kind", "tone")
	o.Set("subkind", subkind)
	if hasFirst {
		o.Set("old_line", int64(first.oldLine))
		o.Set("new_line", optionalInt(first.newLine))
		o.Set("old", first.oldText)
		o.Set("new", pyjson.OptionalString(first.newText))
	} else {
		o.Set("old_line", nil)
		o.Set("new_line", nil)
		o.Set("old", nil)
		o.Set("new", nil)
	}
	o.Set("detail", detail)
	o.Set("level", level)
	return o
}

func (s *sides) tone(full *fullDiff, docType *string, groups []*sentenceGroup) (pyjson.Value, error) {
	list := LoadList("intensifiers")
	intensifiers := intensifierPattern(list.Words)
	baseLevel := toneLevel(docType, "")
	pseudo := docType != nil && *docType == "promo"
	oldLines, newLines := s.toneLinesOf(0, intensifiers, pseudo), s.toneLinesOf(1, intensifiers, pseudo)
	oldTotal, newTotal := oldLines.totals(), newLines.totals()
	var out []pyjson.Value
	var levels []string
	if len(full.hunks) > 0 {
		for _, kind := range toneKinds {
			if newTotal[kind] >= oldTotal[kind] {
				continue
			}
			// 間接の問いにした推敲で減った「？」は数えない (議事録の「予算は足りるのか？」→「予算が足りるかとの質問があった」)。
			// 意味が変わる恐れの「問いの文が問いでなくなった」(sentence_type) は変えない
			if kind == "question" {
				if groups == nil {
					groups = s.sentencePairs(full.hunks)
				}
				if newTotal[kind] >= oldTotal[kind]-s.indirectQuestionDrop(groups) {
					continue
				}
			}
			places, err := tonePlaces(full, oldLines, newLines, kind, intensifiers)
			if err != nil {
				return nil, err
			}
			detail := pyjson.NewObject()
			detail.Set("old", int64(oldTotal[kind]))
			detail.Set("new", int64(newTotal[kind]))
			detail.Set("places", pyjson.MapList(places, tonePlace.value))
			if kind == "intensifier" {
				before, after := map[string]int{}, map[string]int{}
				for _, no := range oldLines.order {
					for w, n := range oldLines.counts[no].words {
						before[w] += n
					}
				}
				for _, no := range newLines.order {
					for w, n := range newLines.counts[no].words {
						after[w] += n
					}
				}
				words := slices.Sorted(maps.Keys(before))
				sort.SliceStable(words, func(i, j int) bool { return before[words[i]] > before[words[j]] })
				var lost []string
				for _, w := range words {
					if after[w] < before[w] {
						lost = append(lost, w)
					}
				}
				detail.Set("words", pyjson.Strings(lost))
			}
			level := toneLevel(docType, kind)
			levels = append(levels, level)
			out = append(out, toneCandidate(kind, places, detail, level))
		}
		oldHeading, newHeading := oldTotal["heading_chars"], newTotal["heading_chars"]
		if oldHeading != 0 && float64(newHeading) < float64(oldHeading)*toneHeadingRatio {
			places, err := tonePlaces(full, oldLines, newLines, "heading_chars", intensifiers)
			if err != nil {
				return nil, err
			}
			detail := pyjson.NewObject()
			detail.Set("old", int64(oldHeading))
			detail.Set("new", int64(newHeading))
			detail.Set("places", pyjson.MapList(places, tonePlace.value))
			level := toneLevel(docType, "heading")
			levels = append(levels, level)
			out = append(out, toneCandidate("heading", places, detail, level))
		}
	}
	// 節の level: 見直し候補が 1 つでもあれば review、参考だけなら info、候補がなければ種類で決まる値
	level := baseLevel
	switch {
	case slices.Contains(levels, "review"):
		level = "review"
	case len(out) > 0:
		level = "info"
	}
	counts := pyjson.NewObject()
	counts.Set("old", totalsValue(oldTotal))
	counts.Set("new", totalsValue(newTotal))
	o := pyjson.NewObject()
	o.Set("level", level)
	o.Set("candidate_count", int64(len(out)))
	o.Set("candidates", append([]pyjson.Value{}, out...))
	o.Set("counts", counts)
	o.Set("list", ListSource("intensifiers"))
	o.Set("list_problem", nil)
	return o, nil
}

// Tone は本家の tone (勢いの検査。JSON の tone と同じ形)。docType は文書の種類 (pr・article・incident・manual・formal・paper なら参考。
// mail と promo は絵文字と見出しも見直し候補、minutes は見出しを見直し候補にする)。
func Tone(orig, rw string, docType *string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	dt, err := visibility.NormalizeDocType(docType)
	if err != nil {
		return nil, err
	}
	return s.tone(full, dt, nil)
}
