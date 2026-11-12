package visibility

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

var (
	containerMarker = mustRegexp(`(?:[-+*]|\d{1,9}[.)])(?:[ \t]+|$)`)
	setext          = mustRegexp(`^ {0,3}(?:=+|-+)[ \t]*$`)
	thematic        = mustRegexp(`^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	fenceOpen       = mustRegexp("^ {0,3}(`{3,}|~{3,})(.*)$")
	atxHeading      = mustRegexp(`^ {0,3}#{1,6}(?:[ \t]+|$)`)
	physicalBreak   = mustRegexp(`\r\n|\r|\n`)
	tableDelimiter  = mustRegexp(`[ \t]*:?-+:?[ \t]*`)
	htmlDeclaration = mustRegexp(`<![A-Z]`)
	htmlRawStart    = mustRegexp(`<(?:script|pre|style|textarea)(?:[ \t>]|$)`, pyre.IGNORECASE)
	htmlRawName     = mustRegexp(`<([A-Za-z]+)`)
	htmlTagStart    = mustRegexp(`</?([A-Za-z][A-Za-z0-9-]*)(?:[ \t/>]|$)`)
	htmlTagName     = mustRegexp(`</?([A-Za-z][A-Za-z0-9-]*)`)
)

var htmlBlockTags = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range strings.Fields("address article aside base basefont blockquote body caption center col colgroup " +
		"dd details dialog dir div dl dt fieldset figcaption figure footer form frame " +
		"frameset h1 h2 h3 h4 h5 h6 head header hr html iframe legend li link main " +
		"menu menuitem nav noframes ol optgroup option p param section source summary " +
		"table tbody td tfoot th thead title tr track ul") {
		m[t] = true
	}
	return m
}()

// 本家は閉じ方の正規表現を行ごとに compile する (re のキャッシュが効く)。同じパターンを何度も pyre.Compile しない。
var (
	compiledMu    sync.Mutex
	compiledCache = map[string]*pyre.Pattern{}
)

func cachedRegexp(pattern string, flags int) *pyre.Pattern {
	key := strconv.Itoa(flags) + ":" + pattern
	compiledMu.Lock()
	defer compiledMu.Unlock()
	p, ok := compiledCache[key]
	if !ok {
		p = mustRegexp(pattern, flags)
		compiledCache[key] = p
	}
	return p
}

// Line は MarkdownLine (1 つの物理行の解析結果)。位置はコードポイントの添字。
type Line struct {
	Line         int
	Raw          string
	Start        int
	Ending       string
	ContentStart int
	Content      string
	QuoteDepth   int
	ListItem     bool
	Kind         string
}

// Block は MarkdownBlock。Lines は Analysis.Lines と同じ *Line (kind を書き換えると両方に見える)。
type Block struct {
	Kind  string
	Lines []*Line
}

// Analysis は analyze_markdown の戻り値。
// 🚨 LineVisibleText と RangeOverlapsProtected は結果の覚え (本家の dict の _line_visible_cache と _kind_mask_cache) を書くので、複数の goroutine から同時に呼ばない。
type Analysis struct {
	Text           string
	Lines          []*Line
	Blocks         []*Block
	ProtectedSpans []Span
	MaskSpans      [][2]int
	MaskEnds       []int

	runes     []rune
	lineCache map[lineKey]string
	kindMasks map[string]masks
}

type masks struct {
	spans [][2]int
	ends  []int
}

// firstEndingAfter は終わりが pos より後ろの、最初の範囲の添字 (bisect_right(ends, pos))。
func (m masks) firstEndingAfter(pos int) int {
	return sort.Search(len(m.ends), func(i int) bool { return m.ends[i] > pos })
}

type lineKey struct {
	line int
	kind string // 種類の集合を並べ替えて NUL でつないだもの。nil (全部) は all を立てて区別する
	all  bool
}

// containers は _containers (行の頭の引用・リストの印)。(印の長さ, 引用の深さ, リストの項目か)。
func containers(raw []rune, allowList bool) (prefix, quotes int, listItem bool) {
	if len(raw) > 0 && raw[0] != ' ' && raw[0] != '>' &&
		(!allowList || (raw[0] != '-' && raw[0] != '+' && raw[0] != '*' && !pyunicode.IsDigit(raw[0]))) {
		return 0, 0, false
	}
	pos := 0
	var t pyre.Text
	haveText := false
	for pos < len(raw) {
		candidate := pos
		for candidate < len(raw) && candidate-pos < 3 && raw[candidate] == ' ' {
			candidate++
		}
		if candidate < len(raw) && raw[candidate] == '>' {
			quotes++
			pos = candidate + 1
			if pos < len(raw) && isSpaceTab(raw[pos]) {
				pos++
			}
			continue
		}
		if allowList {
			if !haveText {
				t, haveText = pyre.NewText(str(raw)), true
			}
			if marker := matchAt(containerMarker, t, candidate); marker != nil {
				listItem = true
				pos = marker.End(0)
				continue
			}
		}
		break
	}
	return pos, quotes, listItem
}

// columns は _columns (行頭の空白・タブの桁数。タブは 4 桁の区切りまで進む)。
func columns(text []rune) int {
	cols := 0
	for _, ch := range text {
		switch ch {
		case ' ':
			cols++
		case '\t':
			cols += 4 - cols%4
		default:
			return cols
		}
	}
	return cols
}

type physicalRow struct {
	start  int
	raw    []rune
	ending []rune
}

// physicalLines は _physical_lines (\r\n・\r・\n で行に割る。最後に改行の後ろの行を必ず 1 つ足す)。
func physicalLines(text []rune, t pyre.Text) []physicalRow {
	var rows []physicalRow
	start := 0
	for _, m := range must(physicalBreak.Finditer(t, 0, pyre.NoEnd)) {
		s, e := m.Span(0)
		rows = append(rows, physicalRow{start, text[start:s], text[s:e]})
		start = e
	}
	return append(rows, physicalRow{start, text[start:], nil})
}

// tableCells は _table_cells (GFM の表のセルの境界。escape された | はコードの中でもデータ)。
func tableCells(content []rune) [][]rune {
	stripped := stripSpaceTab(content)
	starts := []int{0}
	var ends []int
	for index, ch := range stripped {
		if ch == '|' && !escaped(stripped, index) {
			ends = append(ends, index)
			starts = append(starts, index+1)
		}
	}
	ends = append(ends, len(stripped))
	cells := make([][]rune, 0, len(starts))
	for i := range min(len(starts), len(ends)) {
		cells = append(cells, stripped[starts[i]:ends[i]])
	}
	if len(stripped) > 0 && stripped[0] == '|' {
		cells = cells[1:]
	}
	if len(stripped) > 0 && stripped[len(stripped)-1] == '|' && !escaped(stripped, len(stripped)-1) {
		cells = cells[:len(cells)-1]
	}
	return cells
}

type fenceState struct {
	char  rune
	width int
	depth int
}

type htmlUntil struct {
	set     bool
	blank   bool
	pattern string
}

// AnalyzeMarkdown は analyze_markdown。skipFrontmatter が真なら、先頭の --- で囲まれた部分を frontmatter にする。
// 本家は <ſcript> のように、IGNORECASE では script・pre・style・textarea に当たるが ASCII の字だけの名前には当たらない行で AssertionError になる。同じく error を返す。
func AnalyzeMarkdown(text string, skipFrontmatter bool) (*Analysis, error) {
	runes := runesOf(text)
	t := pyre.NewText(text)
	rows := physicalLines(runes, t)
	a := &Analysis{Text: text, runes: runes}
	var protected []Span
	st := &analyzer{}
	activeListIndent := 0
	fmEnd := 0
	if skipFrontmatter && stringsEqualRunes(stripRunesPy(rows[0].raw), "---") {
		for index := 1; index < len(rows); index++ {
			if stringsEqualRunes(stripRunesPy(rows[index].raw), "---") {
				fmEnd = index + 1
				break
			}
		}
	}
	for index, row := range rows {
		start, raw := row.start, row.raw
		prefix, depth, listItem := containers(raw, true)
		quotePrefix, quoteDepth, _ := containers(raw, false)
		quoteContent := raw[quotePrefix:]
		qt := pyre.NewText(str(quoteContent))
		// setext と thematic の規則は、リストの印の認識より先に効く。
		if matchAt(thematic, qt, 0) != nil || (st.inTextBlock() && matchAt(setext, qt, 0) != nil) {
			prefix, depth, listItem = quotePrefix, quoteDepth, false
		}
		if listItem {
			activeListIndent = prefix
		} else if activeListIndent != 0 && depth != 0 && prefix >= activeListIndent {
			listItem = true
		} else if activeListIndent != 0 && hasNonSpace(raw) {
			indent := len(raw) - len(lstripSpaceTab(raw))
			if depth == 0 && indent >= activeListIndent {
				prefix = activeListIndent
				listItem = true
			} else if depth == 0 {
				activeListIndent = 0
			}
		}
		content := raw[prefix:]
		stripped := stripSpaceTab(content)
		ct := pyre.NewText(str(content))
		info := &Line{Line: index + 1, Raw: str(raw), Start: start, Ending: str(row.ending), ContentStart: start + prefix,
			Content: str(content), QuoteDepth: depth, ListItem: listItem, Kind: "paragraph"}
		if prefix != 0 {
			protected = append(protected, Span{start, start + prefix, "container_prefix"})
		}
		kind := ""
		switch {
		case index < fmEnd:
			kind = "frontmatter"
		case st.fence != nil && depth < st.fence.depth:
			st.fence = nil
		}
		if kind == "" && st.fence != nil {
			kind = "code"
			closing := cachedRegexp("^ {0,3}("+pyre.Escape(string(st.fence.char))+"{"+strconv.Itoa(st.fence.width)+",})[ \\t]*$", 0)
			if matchAt(closing, ct, 0) != nil {
				st.fence = nil
			}
		}
		if kind == "" && st.html.set {
			kind = "html_block"
			if st.html.blank {
				if len(stripped) == 0 {
					st.html = htmlUntil{}
				}
			} else if searchFrom(cachedRegexp(st.html.pattern, pyre.IGNORECASE), ct, 0) != nil {
				st.html = htmlUntil{}
			}
		}
		if kind == "" {
			var err error
			kind, err = st.classify(content, ct, stripped, depth, listItem)
			if err != nil {
				return nil, err
			}
		}
		info.Kind = kind
		a.Lines = append(a.Lines, info)
		if kind == "blank" || kind == "thematic_break" || kind == "heading_underline" {
			st.current = nil
			continue
		}
		if kind == "code" || kind == "frontmatter" || kind == "html_block" || kind == "reference_definition" {
			protected = append(protected, Span{start, start + len(raw) + len(row.ending), kind})
		}
		current := st.current
		continueBlock := current != nil && current.Kind == kind && kind != "heading" && kind != "table" && kind != "list"
		if continueBlock && kind == "quote" && current.Lines[0].QuoteDepth != depth {
			continueBlock = false
		}
		if kind == "list" && current != nil && current.Kind == "list" {
			if _, _, item := containers(raw, true); !item {
				continueBlock = true
			}
		}
		if kind == "paragraph" && current != nil && current.Kind == "quote" && depth == 0 {
			continueBlock = true
			info.Kind = "quote"
		}
		if !continueBlock {
			current = &Block{Kind: info.Kind}
			st.current = current
			st.blocks = append(st.blocks, current)
		}
		current.Lines = append(current.Lines, info)
	}
	a.Blocks = st.blocks
	// 定義は、完成した葉の段落の先頭だけで認識する。これで複数行のラベル・宛先・タイトルを受け入れ、定義の形をした後ろの行が既存の文章を割り込むことを防ぐ。
	var referenceBlocks []*Block
	for _, block := range a.Blocks {
		rows := block.Lines
		offset := 0
		if block.Kind == "paragraph" || block.Kind == "list" || block.Kind == "quote" || block.Kind == "heading" {
			contents := make([][]rune, len(rows))
			for i, r := range rows {
				contents[i] = runesOf(r.Content)
			}
			for offset < len(rows) {
				end, ok := referenceDefinitionEnd(contents, offset, false)
				if !ok {
					break
				}
				referenceRows := rows[offset : end+1]
				for _, r := range referenceRows {
					r.Kind = "reference_definition"
					protected = append(protected, Span{r.Start, r.Start + pyunicode.RuneCount(r.Raw) + pyunicode.RuneCount(r.Ending), "reference_definition"})
				}
				referenceBlocks = append(referenceBlocks, &Block{Kind: "reference_definition", Lines: referenceRows})
				offset = end + 1
			}
		}
		if offset < len(rows) {
			referenceBlocks = append(referenceBlocks, &Block{Kind: block.Kind, Lines: rows[offset:]})
		}
	}
	a.Blocks = referenceBlocks
	// インラインの解析は葉のブロックの境界を越えない。
	for _, block := range a.Blocks {
		switch block.Kind {
		case "code", "frontmatter", "html_block", "reference_definition":
			continue
		}
		first, last := block.Lines[0], block.Lines[len(block.Lines)-1]
		begin := first.Start
		finish := last.Start + pyunicode.RuneCount(last.Raw)
		segment := slices.Clone(runes[begin:finish])
		for _, r := range block.Lines {
			for position := r.Start; position < r.ContentStart; position++ {
				segment[position-begin] = ' '
			}
		}
		for _, sp := range inlineProtectedSpans(newSource(segment)) {
			protected = append(protected, Span{begin + sp.Start, begin + sp.End, sp.Kind})
		}
	}
	slices.SortFunc(protected, compareSpans)
	a.ProtectedSpans = protected
	a.MaskSpans, a.MaskEnds = mergeMasks(protected)
	return a, nil
}

func mergeMasks(ordered []Span) ([][2]int, []int) {
	merged := [][2]int{}
	for _, sp := range ordered {
		if n := len(merged); n > 0 && sp.Start <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], sp.End)
		} else {
			merged = append(merged, [2]int{sp.Start, sp.End})
		}
	}
	ends := make([]int, len(merged))
	for i, m := range merged {
		ends[i] = m[1]
	}
	return merged, ends
}

func stringsEqualRunes(r []rune, s string) bool {
	if len(r) != len(s) {
		return false
	}
	for i := range len(s) {
		if r[i] != rune(s[i]) {
			return false
		}
	}
	return true
}

// stripRunesPy は s.strip() (Python の空白)。
func stripRunesPy(r []rune) []rune {
	i, j := 0, len(r)
	for i < j && pyunicode.IsSpace(r[i]) {
		i++
	}
	for j > i && pyunicode.IsSpace(r[j-1]) {
		j--
	}
	return r[i:j]
}

// analyzer は analyze_markdown の行を読み進める間の状態 (柵・HTML ブロックの終わり・今のブロック・ここまでのブロック)。
type analyzer struct {
	blocks  []*Block
	current *Block
	fence   *fenceState
	html    htmlUntil
}

func (st *analyzer) inTextBlock() bool {
	c := st.current
	return c != nil && (c.Kind == "paragraph" || c.Kind == "quote" || c.Kind == "list")
}

// classify は、柵の中・HTML ブロックの中でない行の種類を決める。
// setext の下線は今のブロックと前の行の種類を書き換え、表の区切りの行は見出しの行を表のブロックに移す (blocks と current を書き換える)。
func (st *analyzer) classify(content []rune, ct pyre.Text, stripped []rune, depth int, listItem bool) (string, error) {
	current := st.current
	if marker := matchAt(fenceOpen, ct, 0); marker != nil && (content[marker.Start(1)] != '`' || !slices.Contains(content[marker.Start(2):marker.End(2)], '`')) {
		st.fence = &fenceState{content[marker.Start(1)], marker.End(1) - marker.Start(1), depth}
		return "code", nil
	}
	switch {
	case columns(content) >= 4 && (current == nil || current.Kind == "code"):
		return "code", nil
	case len(stripped) == 0:
		return "blank", nil
	case matchAt(atxHeading, ct, 0) != nil:
		return "heading", nil
	case matchAt(setext, ct, 0) != nil && st.inTextBlock():
		current.Kind = "heading"
		for _, earlier := range current.Lines {
			earlier.Kind = "heading"
		}
		return "heading_underline", nil
	case matchAt(thematic, ct, 0) != nil:
		return "thematic_break", nil
	case st.inTextBlock() && len(current.Lines) > 0 && slices.Contains(content, '|') && isTableDelimiterRow(content, runesOf(current.Lines[len(current.Lines)-1].Content)):
		header := current.Lines[len(current.Lines)-1]
		current.Lines = current.Lines[:len(current.Lines)-1]
		header.Kind = "table"
		if len(current.Lines) == 0 {
			// current は最後に足したブロック。
			st.blocks = st.blocks[:len(st.blocks)-1]
		}
		st.blocks = append(st.blocks, &Block{Kind: "table", Lines: []*Line{header}})
		st.current = nil
		return "table", nil
	case current != nil && current.Kind == "table" && slices.Contains(content, '|'):
		return "table", nil
	}
	htmlStart := lstripSpaceTab(content)
	hs := pyre.NewText(str(htmlStart))
	var until htmlUntil
	switch {
	case hasPrefixAt(htmlStart, "<!--", 0):
		until = htmlUntil{set: true, pattern: "-->"}
	case hasPrefixAt(htmlStart, "<?", 0):
		until = htmlUntil{set: true, pattern: `\?>`}
	case hasPrefixAt(htmlStart, "<![CDATA[", 0):
		until = htmlUntil{set: true, pattern: `\]\]>`}
	case matchAt(htmlDeclaration, hs, 0) != nil:
		until = htmlUntil{set: true, pattern: ">"}
	case matchAt(htmlRawStart, hs, 0) != nil:
		opening := matchAt(htmlRawName, hs, 0)
		if opening == nil {
			return "", newError(TypeAssertionError, "")
		}
		tag := str(htmlStart[opening.Start(1):opening.End(1)])
		until = htmlUntil{set: true, pattern: "</" + tag + `\s*>`}
	case matchAt(htmlTagStart, hs, 0) != nil:
		opening := matchAt(htmlTagName, hs, 0)
		if opening == nil {
			return "", newError(TypeAssertionError, "")
		}
		tag := strings.ToLower(str(htmlStart[opening.Start(1):opening.End(1)]))
		if htmlBlockTags[tag] {
			until = htmlUntil{set: true, blank: true}
		} else if !st.inTextBlock() {
			if end, ok := tagEnd(newSource(htmlStart), 0, nil); ok && len(stripSpaceTab(htmlStart[end:])) == 0 {
				until = htmlUntil{set: true, blank: true}
			}
		}
	}
	if until.set {
		st.html = until
		if !until.blank && searchFrom(cachedRegexp(until.pattern, pyre.IGNORECASE), ct, 0) != nil {
			st.html = htmlUntil{}
		}
		return "html_block", nil
	}
	switch {
	case listItem:
		return "list", nil
	case depth != 0:
		return "quote", nil
	}
	return "paragraph", nil
}

// isTableDelimiterRow は表の区切りの行 (|---|:--:|) かどうか。セルの数が直前の行と同じで、全部のセルが - を 1 つ以上持つ区切り。
func isTableDelimiterRow(content, previous []rune) bool {
	cells := tableCells(content)
	if len(cells) != len(tableCells(previous)) {
		return false
	}
	for _, cell := range cells {
		if !fullMatches(tableDelimiter, cell) {
			return false
		}
	}
	return true
}

// mergedMasks は _merged_masks。
func mergedMasks(spans []Span, kinds map[string]bool) masks {
	var chosen []Span
	for _, sp := range spans {
		if kinds[sp.Kind] {
			chosen = append(chosen, sp)
		}
	}
	slices.SortFunc(chosen, compareSpans)
	m, e := mergeMasks(chosen)
	return masks{m, e}
}

func kindSet(kinds []string) (map[string]bool, string) {
	set := map[string]bool{}
	for _, k := range kinds {
		set[k] = true
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	return set, strings.Join(names, "\x00")
}

func (a *Analysis) masksFor(kinds []string) masks {
	if kinds == nil {
		return masks{a.MaskSpans, a.MaskEnds}
	}
	set, key := kindSet(kinds)
	if m, ok := a.kindMasks[key]; ok {
		return m
	}
	if a.kindMasks == nil {
		a.kindMasks = map[string]masks{}
	}
	m := mergedMasks(a.ProtectedSpans, set)
	a.kindMasks[key] = m
	return m
}

// LineVisibleText は line_visible_text。選んだ種類 (kinds。nil は全部。空の集合は何も隠さない) の保護された範囲を空白にした、lineNo (1 から) 行目の字。
// lineNo は Python の添字と同じ規則で、0 以下は後ろから数え、範囲の外は IndexError。
func (a *Analysis) LineVisibleText(lineNo int, kinds []string) (string, error) {
	_, key := kindSet(kinds)
	ck := lineKey{lineNo, key, kinds == nil}
	if v, ok := a.lineCache[ck]; ok {
		return v, nil
	}
	idx := lineNo - 1
	if idx < 0 {
		idx += len(a.Lines)
	}
	if idx < 0 || idx >= len(a.Lines) {
		return "", newError(TypeIndexError, "list index out of range")
	}
	row := a.Lines[idx]
	result := a.visible(row, kinds)
	if a.lineCache == nil {
		a.lineCache = map[lineKey]string{}
	}
	a.lineCache[ck] = result
	return result, nil
}

func (a *Analysis) visible(row *Line, kinds []string) string {
	start := row.Start
	rawRunes := runesOf(row.Raw)
	end := start + len(rawRunes)
	if len(a.ProtectedSpans) == 0 {
		return row.Raw
	}
	m := a.masksFor(kinds)
	begin := m.firstEndingAfter(start)
	if begin >= len(m.spans) || m.spans[begin][0] >= end {
		return row.Raw
	}
	visible := slices.Clone(rawRunes)
	for index := begin; index < len(m.spans); index++ {
		left, right := m.spans[index][0], m.spans[index][1]
		if left >= end {
			break
		}
		for position := max(left, start); position < min(right, end); position++ {
			if visible[position-start] != '\r' && visible[position-start] != '\n' {
				visible[position-start] = ' '
			}
		}
	}
	return str(visible)
}

// RangeOverlapsProtected は range_overlaps_protected (空でない [start, end) が保護された範囲に掛かるか)。kinds の扱いは LineVisibleText と同じ。
func (a *Analysis) RangeOverlapsProtected(start, end int, kinds []string) bool {
	if end <= start {
		return false
	}
	m := a.masksFor(kinds)
	index := m.firstEndingAfter(start)
	return index < len(m.spans) && m.spans[index][0] < end
}
