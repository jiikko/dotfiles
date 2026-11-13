package lint

import (
	"slices"
	"strings"
	"sync"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/lint/pausecomma"
	"ja-writing/visibility"
)

// G17 スライドの読点 (slide_comma。本家 yomiyasu_lint.py の 4721-4884)。slides だけ。表は g17_tables.go。
// 同じ行に pause_comma があれば主題と主語の読点は重ねず、pause_comma が拾った短い目的語の「を、」は重ねない。

var (
	slideMarkerWords = "、。！？!?：:"
	slideSameTail    = map[string]*lazyPattern{}
	slideSameTailMu  sync.Mutex
	slideLinkText    = newLazy(`\[([^\]\n]*)\]\([^)\n]*\)`, 0)
)

// slideSingleLines は本家の _v240_slide_single_lines。1 行で終わる見出し・箇条・段落の行の番号 (続きの行がある箇条と、複数行の段落の行は入れない)。
func (s *lintState) slideSingleLines() []int {
	rows := s.an.Lines
	var out []int
	for index, row := range rows {
		if row.QuoteDepth != 0 || pystr.Strip(row.Raw) == "" {
			continue
		}
		if row.Kind == "heading" {
			out = append(out, row.Line)
			continue
		}
		if row.Kind != "list" && row.Kind != "paragraph" {
			continue
		}
		bullet := mustMatchAt0(slideBullet, row.Raw)
		isText := func(other *visibility.Line) bool {
			return other != nil && pystr.Strip(other.Raw) != "" && (other.Kind == "list" || other.Kind == "paragraph")
		}
		var previous, following *visibility.Line
		if index > 0 {
			previous = rows[index-1]
		}
		if index+1 < len(rows) {
			following = rows[index+1]
		}
		if !bullet && isText(previous) {
			continue
		}
		if isText(following) && following.QuoteDepth == row.QuoteDepth && !mustMatchAt0(slideBullet, following.Raw) {
			continue
		}
		out = append(out, row.Line)
	}
	return out
}

// slideTexts は本家の _v240_slide_texts。(見える文字, 伏せた所を「＿」にした文字, 見える文字の各字の元の行での位置)。太字の記号は外す。
func (s *lintState) slideTexts(lineNo int) (visible, masked string, positions []int) {
	raw := pyunicode.Runes(s.an.Lines[lineNo-1].Raw)
	visibleRaw := pyunicode.Runes(s.doc.proseVisibleText(lineNo))
	if len(visibleRaw) == len(raw) {
		zone := matchAt0(slideMarkerZone, s.an.Lines[lineNo-1].Raw).End(0)
		for index, char := range visibleRaw {
			if char == ' ' && index >= zone && !strings.ContainsRune(" \t\u3000", raw[index]) {
				visibleRaw[index] = '＿'
			}
		}
	}
	var chars []rune
	for i, char := range visibleRaw {
		if char != '*' {
			chars = append(chars, char)
			positions = append(positions, i)
		}
	}
	visible = pyunicode.FromRunes(chars)
	if strings.Contains(visible, "__") {
		drop := map[int]bool{}
		for i := 0; i+1 < len(chars); i++ {
			if chars[i] == '_' && chars[i+1] == '_' {
				drop[i], drop[i+1] = true, true
				i++
			}
		}
		var keptChars []rune
		var keptPositions []int
		for i, char := range chars {
			if !drop[i] {
				keptChars = append(keptChars, char)
				keptPositions = append(keptPositions, positions[i])
			}
		}
		chars, positions = keptChars, keptPositions
		visible = pyunicode.FromRunes(chars)
	}
	masked = visible
	if strings.ContainsAny(masked, "「『[") {
		masked = maskQuotedSegments(masked)
	}
	if strings.ContainsAny(masked, "“\"‘") {
		masked = blankMatches(v220Quotes, masked)
	}
	masked = blankMatches(v220Identifier, masked)
	maskedRunes := pyunicode.Runes(masked)
	if len(maskedRunes) != len(chars) {
		return visible, masked, positions
	}
	for i, m := range maskedRunes {
		if m == ' ' && chars[i] != ' ' {
			maskedRunes[i] = '＿'
		}
	}
	return visible, pyunicode.FromRunes(maskedRunes), positions
}

// slideHitText は本家の _v240_slide_hit_text。当たった言い方の字 (元の行から切り出し、リンクは文字だけ、太字の記号は外す。20 字より長ければ頭に「…」)。
func slideHitText(raw string, positions []int, begin, end int) string {
	if len(positions) == 0 || begin >= end {
		return ""
	}
	rs := pyunicode.Runes(raw)
	text := runeSlice(rs, positions[begin], positions[end-1]+1)
	text = subText(slideLinkText, `\1`, text)
	text = pystr.Strip(strings.ReplaceAll(strings.ReplaceAll(text, "**", ""), "__", ""))
	if tr := pyunicode.Runes(text); len(tr) > 20 {
		text = "…" + pyunicode.FromRunes(tr[len(tr)-20:])
	}
	return text
}

func slideSameTailPattern(tail string) *lazyPattern {
	slideSameTailMu.Lock()
	defer slideSameTailMu.Unlock()
	p, ok := slideSameTail[tail]
	if !ok {
		p = newLazy(pyre.Escape(tail)+`[ \u3000]*[^、。！？!?]{1,12}$`, 0)
		slideSameTail[tail] = p
	}
	return p
}

// matchAtPos は p.match(text, pos) (pos から始まる照合)。
func matchAtPos(p *pyre.Pattern, text pyre.Text, pos int) bool {
	m, err := p.Match(text, pos, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// g17SlideCommaFindings は後から足す検出の 4 番目 (本家の _v240_slide_comma_findings。slides だけ)。s.findings の pause_comma の候補の Heads・Objects を読む。
func (s *lintState) g17SlideCommaFindings() {
	if s.docTypeName() != "slides" {
		return
	}
	pauseLines := map[int]bool{}
	objectWords := map[int][]string{}
	for _, f := range s.findings {
		if f.Rule == "pause_comma" {
			if f.Heads {
				pauseLines[f.Line] = true
			}
			objectWords[f.Line] = f.Objects
		}
	}
	for _, lineNo := range s.slideSingleLines() {
		row := s.an.Lines[lineNo-1]
		if !strings.Contains(row.Raw, "、") {
			continue
		}
		_, scan, positions := s.slideTexts(lineNo)
		scanRunes := pyunicode.Runes(scan)
		lead := matchAt0(slideLead, scan).End(0)
		body := pystr.RStrip(runeSlice(scanRunes, lead, len(scanRunes)))
		bodyText := pyre.NewText(body)
		bodyRunes := pyunicode.Runes(body)
		if len(bodyRunes) == 0 || len(bodyRunes) > slideLineMax {
			continue
		}
		var hits []string
		for _, comma := range slideComma {
			if (comma.name == "主題" || comma.name == "主語") && pauseLines[lineNo] {
				continue
			}
			matches, err := comma.re.Get().Finditer(bodyText, 0, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			for _, m := range matches {
				end := m.End(0)
				if comma.name == "主題" && (matchAtPos(slideActorAfter.Get(), bodyText, end) || matchAtPos(slideListAfter.Get(), bodyText, end) ||
					matchAtPos(pausecomma.TEAttrObject(), bodyText, end)) {
					continue
				}
				tail := runeSlice(bodyRunes, end-2, end)
				if next, ok := slideSameNext[tail]; ok && (comma.name == "主語" || comma.name == "目的語") &&
					(matchAtPos(next.Get(), bodyText, end) || mustSearch(slideSameTailPattern(tail), runeSlice(bodyRunes, 0, end-2))) {
					continue
				}
				begin := 0
				for i := min(end-1, len(bodyRunes)) - 1; i >= 0; i-- {
					if strings.ContainsRune(slideMarkerWords, bodyRunes[i]) {
						begin = i + 1
						break
					}
				}
				for begin < end && (bodyRunes[begin] == ' ' || bodyRunes[begin] == '\u3000') {
					begin++
				}
				if comma.name == "目的語" && slices.Contains(objectWords[lineNo], pystr.RStripChars(runeSlice(bodyRunes, begin, end), "、，,")) {
					continue
				}
				hit := runeSlice(bodyRunes, begin, end)
				if len(positions) == len(scanRunes) {
					hit = slideHitText(row.Raw, positions, lead+begin, lead+end)
				}
				hits = append(hits, "「"+hit+"」")
			}
		}
		if len(hits) > 0 {
			s.add(newFinding("slide_comma", lineNo, "info", slideMessageHead+strings.Join(uniqueInOrder(hits), "")+slideMessageTail, pystr.Strip(row.Raw)))
		}
	}
}
