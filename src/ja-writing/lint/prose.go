package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 文字の下ごしらえ (本家 yomiyasu_lint.py の T3)。引用を伏せる・文を切り出す・疑問の文を見分ける・規則が見る「見える文字」を作る。
//
// 本家の名前と Go の名前:
//
//	_mask_quoted_segments / _mask_quoted_segments_sparse   maskQuotedSegments (本家の 2 つは同じ出力で、速さだけが違う。Go は 1 つ)
//	_question_content_end     questionContentEnd
//	_last_context_sentence    lastContextSentence
//	_ends_with_question       endsWithQuestion
//	_prose_visible_text       (*document).proseVisibleText
//	_image_only_row           (*document).imageOnlyRow
//	_plain_sentence_records   (*document).plainSentenceRecords (戻りの項目は plainSentence)
//	_rule9_context            (*document).rule9Context (戻りの項目は contextSentence)
//	_v220_scan_text           (*document).v220ScanText
//
// 本家は analysis の dict に _prose_line_cache・_end_quote_closers・_image_link_start_indexes などの覚えを書き足す。
// Go は visibility.Analysis に欄を足さず、lint の文脈 document に持つ (document は 1 つの文書の 1 回の実行だけで使い、複数の goroutine から同時に呼ばない)。

// metricOpaqueKinds は指標と文の切り出しが「見えない」とみなす保護された範囲の種類 (本家の _METRIC_OPAQUE_KINDS)。
var metricOpaqueKinds = []string{"code", "frontmatter", "html_block", "reference_definition", "inline_code", "html_token", "autolink", "link_destination", "image", "bare_url"}

// proseScanKinds は地の文を見るときに伏せる種類 (本家の _PROSE_SCAN_KINDS = 上の種類 + container_prefix)。
var proseScanKinds = append(slices.Clone(metricOpaqueKinds), "container_prefix")

const questionClosers = "」』）)\"'*_"

var questionSuffixes = []string{"ですか", "ますか", "ませんか", "でしょうか", "だろうか", "のか", "ないか", "うか", "くか", "ぐか", "すか", "つか", "ぬか", "ぶか", "むか", "るか", "たか", "いいか", "よいか"}

var (
	whQuestion = newLazy(`なぜ|どうして(?!も)|どのよう(?:に|な)|いつ(?!でも|も|か(?!ら))|どこ(?!でも|も|か(?!ら))|`+
		`誰(?!でも|も|か(?!ら))|何(?!でも|も|か(?!ら))|どれ(?!でも|も|か(?!ら))|どの(?![^。！？!?、]{1,12}でも)`, 0)
	v220Quotes     = newLazy(`“[^”\n]*”|\"[^\"\n]*\"|‘[^’\n]*’`, 0)
	v220Identifier = newLazy(`(?<![A-Za-z0-9])[A-Za-z0-9]+(?:[_./\\][A-Za-z0-9]+)+[_./]?|(?<![a-z])[a-z]+[A-Z][A-Za-z0-9]*|[_./\\][A-Za-z0-9_]+`, 0)
)

// document は lint が 1 つの文書について持つもの。解析 (visibility.Analysis) と、本家が analysis の dict に書き足す覚え。
type document struct {
	an    *visibility.Analysis
	prose map[int]string
	// endQuote は endClosersFor が初めて作る (nil は未計算)。
	endQuote map[int]string
	// images と destinations は _image_link_start_indexes (画像とリンクの宛先の、始まり → 終わり)。nil は未計算。
	images, destinations map[int]int
}

func newDocument(an *visibility.Analysis) *document {
	return &document{an: an, prose: map[int]string{}}
}

// proseVisibleText は本家の _prose_visible_text。文の切り出しと語の走査が使う、元の行と同じ長さの見える文字 (保護された範囲を空白にしたもの)。
// lineNo は解析の行の番号 (1 から)。範囲の外は本家の IndexError で、Go は panic する (呼び出しは解析の行を辿るので、範囲の外は書き間違い)。
func (d *document) proseVisibleText(lineNo int) string {
	if text, ok := d.prose[lineNo]; ok {
		return text
	}
	text, err := d.an.LineVisibleText(lineNo, proseScanKinds)
	if err != nil {
		panic(err)
	}
	d.prose[lineNo] = text
	return text
}

// maskQuotedSegments は本家の _mask_quoted_segments (と _mask_quoted_segments_sparse)。「」『』[] の対になった引用 (入れ子を含む) を空白にする。
// 開きの字ごとの山を持ち、閉じの字が出たら同じ種類の最後の開きと対にする。対にならない開き・閉じはそのまま。
func maskQuotedSegments(text string) string {
	rs := pyunicode.Runes(text)
	closing := map[rune]rune{'「': '」', '『': '』', '[': ']'}
	stacks := map[rune][]int{}
	changes := make([]int, len(rs)+1)
	hasPair := false
	for index, char := range rs {
		if closer, ok := closing[char]; ok {
			stacks[closer] = append(stacks[closer], index)
		} else if stack := stacks[char]; len(stack) > 0 {
			start := stack[len(stack)-1]
			stacks[char] = stack[:len(stack)-1]
			changes[start]++
			changes[index+1]--
			hasPair = true
		}
	}
	if !hasPair {
		return text
	}
	depth := 0
	out := make([]rune, len(rs))
	for index, char := range rs {
		depth += changes[index]
		if depth != 0 {
			char = ' '
		}
		out[index] = char
	}
	return pyunicode.FromRunes(out)
}

// questionContentEnd は本家の _question_content_end。文の終わりの、空白・閉じ字・飾りを除いた位置。
func questionContentEnd(sentence []rune) int {
	end := len(sentence)
	for end > 0 && (pyunicode.IsSpace(sentence[end-1]) || strings.ContainsRune(questionClosers, sentence[end-1]) || isEndDecorationChar(sentence[end-1])) {
		end--
	}
	return end
}

// lastContextSentence は本家の _last_context_sentence。text の最後の文 (句点・感嘆符・疑問符の後ろから、飾りを除いた終わりまで)。
func lastContextSentence(text string) string {
	rs := pyunicode.Runes(text)
	end := questionContentEnd(rs)
	if end == 0 {
		return ""
	}
	start := 0
	for i := end - 2; i >= 0; i-- {
		if strings.ContainsRune("。！？!?", rs[i]) {
			start = i + 1
			break
		}
	}
	return pyunicode.FromRunes(rs[start:end])
}

// endsWithQuestion は本家の _ends_with_question。隣の文が「はい・いいえ」で答える疑問か (か で終わる任意の語ではない)。
func endsWithQuestion(sentence string) bool {
	rs := pyunicode.Runes(sentence)
	end := questionContentEnd(rs)
	if end == 0 {
		return false
	}
	explicit := rs[end-1] == '？' || rs[end-1] == '?'
	if !explicit && rs[end-1] == '。' {
		end--
	}
	if !explicit {
		head := pyunicode.FromRunes(rs[:end])
		if strings.HasSuffix(head, "いくつか") || !slices.ContainsFunc(questionSuffixes, func(suffix string) bool { return strings.HasSuffix(head, suffix) }) {
			return false
		}
	}
	m, err := whQuestion.get().Search(pyre.NewText(sentence), 0, end)
	if err != nil {
		panic(err)
	}
	return m == nil
}

// imageOnlyRow は本家の _image_only_row。見える文字が無い行が画像だけの行か (画像の後ろの文は飛ばさない)。バッジ (`[![` で始まる、画像を 1 つだけ含むリンク) も画像だけの行。
func (d *document) imageOnlyRow(row *visibility.Line, visible string) bool {
	rowRunes := pyunicode.Runes(row.Raw)
	if pystr.Strip(visible) == "" {
		return d.an.RangeOverlapsProtected(row.Start, row.Start+len(rowRunes), []string{"image"})
	}
	if !strings.HasPrefix(pystr.Strip(row.Raw), "[![") {
		return false
	}
	if d.images == nil {
		d.images, d.destinations = map[int]int{}, map[int]int{}
		for _, span := range d.an.ProtectedSpans {
			switch span.Kind {
			case "image":
				d.images[span.Start] = span.End
			case "link_destination":
				d.destinations[span.Start] = span.End
			}
		}
	}
	start := row.Start + len(rowRunes) - len(pyunicode.Runes(pystr.LStrip(row.Raw)))
	end := row.Start + len(pyunicode.Runes(pystr.RStrip(row.Raw)))
	imageEnd, ok := d.images[start+1]
	if !ok {
		return false
	}
	text := pyunicode.Runes(d.an.Text)
	following := runeSlice(text, imageEnd, imageEnd+2)
	destinationEnd, hasDestination := d.destinations[imageEnd+1]
	return following == "](" && hasDestination && destinationEnd == end
}

// imageBreakRow は、行が画像だけ (見える文字が無い、またはバッジ) で、文の並びの視覚的な区切りになるか。
// 本家は `(not visible.strip() or raw.lstrip().startswith("[![")) and _image_only_row(...)` を 5 か所に書く。先に安い条件で絞るので、画像だけの行の判定は手掛かりのある行にしか走らない。
func (d *document) imageBreakRow(row *visibility.Line, visible string) bool {
	return (pystr.Strip(visible) == "" || strings.HasPrefix(pystr.LStrip(row.Raw), "[![")) && d.imageOnlyRow(row, visible)
}

// runeSlice は Python の text[start:end] (範囲の外は切り詰める)。
func runeSlice(text []rune, start, end int) string {
	start, end = min(max(start, 0), len(text)), min(max(end, 0), len(text))
	if start >= end {
		return ""
	}
	return pyunicode.FromRunes(text[start:end])
}

// plainSentence は plainSentenceRecords の 1 件。Group は (段落のブロックの番号, 画像で区切った節の番号)。
type plainSentence struct {
	Line    int
	Visible string
	Source  string
	Group   [2]int
}

// sentenceEnds は本家の _SENTENCE_END_POSITION.finditer の始まり (「。！？」の直後の位置。空の一致なので、文の最後の字が句点でも末尾に出る)。
func sentenceEnds(visible []rune) []int {
	var ends []int
	for i := 1; i <= len(visible); i++ {
		if strings.ContainsRune("。！？", visible[i-1]) {
			ends = append(ends, i)
		}
	}
	return ends
}

// plainSentenceRecords は本家の _plain_sentence_records。段落の見える文字と、元の行の同じ範囲の文字を、同じ切り目で文にして返す。
// 画像だけの行は視覚的な区切りとして節を替える。3 字以下の文は数えない。
func (d *document) plainSentenceRecords() []plainSentence {
	var records []plainSentence
	for blockID, block := range d.an.Blocks {
		if block.Kind != "paragraph" {
			continue
		}
		section := 0
		for _, row := range block.Lines {
			visible := d.proseVisibleText(row.Line)
			if d.imageBreakRow(row, visible) {
				section++
				continue
			}
			if pystr.Strip(visible) == "" {
				continue
			}
			visibleRunes, rawRunes := pyunicode.Runes(visible), pyunicode.Runes(row.Raw)
			ends := sentenceEnds(visibleRunes)
			if len(ends) == 0 || ends[len(ends)-1] != len(visibleRunes) {
				ends = append(ends, len(visibleRunes))
			}
			start := 0
			for _, end := range ends {
				clean := pystr.Strip(runeSlice(visibleRunes, start, end))
				if clean != "" && pystr.Len(clean) > 3 {
					records = append(records, plainSentence{row.Line, clean, pystr.Strip(runeSlice(rawRunes, start, end)), [2]int{blockID, section}})
				}
				start = end
			}
		}
	}
	return records
}

// contextSentence は rule9Context の 1 件 (文と、その次の文)。
type contextSentence struct{ Sentence, Following string }

// rule9Context は本家の _rule9_context。新しい規則 (過言・指示の残りなど) が見る、引用を伏せた地の文と、その直後の文を行ごとに返す。
// 引用の伏せは段落をまたいで掛け (改行も伏せうるので、元の行の幅で切り直す)、行ごとの幅を保つ。返す map の鍵は段落と箇条書きの行。
func (d *document) rule9Context() map[int][]contextSentence {
	rows := d.an.Lines
	parts := make([]string, len(rows))
	for i, row := range rows {
		if (row.Kind == "paragraph" || row.Kind == "list") && row.QuoteDepth == 0 {
			parts[i] = stripEmphasisMarks(d.proseVisibleText(row.Line))
		}
	}
	masked := pyunicode.Runes(maskQuotedSegments(strings.Join(parts, "\n")))
	offset := 0
	visibleLines := make(map[int]string, len(rows))
	for i, row := range rows {
		width := pystr.Len(parts[i])
		visibleLines[row.Line] = pystr.Strip(runeSlice(masked, offset, offset+width))
		offset += width + 1
	}
	contexts := map[int][]contextSentence{}
	for _, block := range d.an.Blocks {
		if block.Kind != "paragraph" && block.Kind != "list" {
			continue
		}
		type record struct {
			line     int
			sentence string
		}
		var records []record
		for _, row := range block.Lines {
			contexts[row.Line] = []contextSentence{}
			for _, sentence := range splitAfterMarks(visibleLines[row.Line], "。！？!?") {
				if pystr.Strip(sentence) != "" {
					records = append(records, record{row.Line, pystr.Strip(sentence)})
				}
			}
		}
		for i, r := range records {
			following := ""
			if i+1 < len(records) {
				following = records[i+1].sentence
			}
			contexts[r.line] = append(contexts[r.line], contextSentence{r.sentence, following})
		}
	}
	return contexts
}

// stripEmphasisMarks は `.replace("__", "").replace("*", "")` (見える文字から太字の記号を除く。順は本家のまま: `___` は `_` が 1 つ残る)。
func stripEmphasisMarks(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "__", ""), "*", "")
}

// splitAfterMarks は本家の _LINE_SENTENCE_BREAK.split (マークの直後で切る。空の一致で切るので、末尾がマークなら最後に空の要素が出る)。
func splitAfterMarks(s, marks string) []string {
	rs := pyunicode.Runes(s)
	var pieces []string
	start := 0
	for i := 1; i <= len(rs); i++ {
		if strings.ContainsRune(marks, rs[i-1]) {
			pieces = append(pieces, pyunicode.FromRunes(rs[start:i]))
			start = i
		}
	}
	return append(pieces, pyunicode.FromRunes(rs[start:]))
}

// v220ScanText は本家の _v220_scan_text。新しい語の規則が見る行の文字。見える文字から、カギ括弧と引用符の中、英数字の識別子を空白にする。
func (d *document) v220ScanText(lineNo int) string {
	visible := stripEmphasisMarks(d.proseVisibleText(lineNo))
	if strings.ContainsAny(visible, "「『[") {
		visible = maskQuotedSegments(visible)
	}
	if strings.ContainsAny(visible, "“\"‘") {
		visible = blankMatches(v220Quotes, visible)
	}
	return blankMatches(v220Identifier, visible)
}

// blankMatches は re.sub(lambda m: " " * len(m.group()), text) (一致を同じ長さの空白にする)。
func blankMatches(p *lazyPattern, text string) string {
	out, err := p.get().SubFunc(func(m *pyre.Match) string {
		start, end := m.Span(0)
		return strings.Repeat(" ", end-start)
	}, pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	return out
}
