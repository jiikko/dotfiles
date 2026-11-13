package lint

import (
	"strconv"
	"strings"
	"sync"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G1 指標 (excess_bold・excess_list。本家 yomiyasu_lint.py の 1959-2070 と 5546-5570)。
//
// 文書の指標 (metrics) は出力の metrics の欄と点数 (地の文の字数) に使うので、候補を出す前から骨組みが計算する (metricsAndListGate)。
// この群が足すのは、指標から候補を作る g01Document だけ。

var (
	listMarkerLine = newLazy(`^\s*([-*+]|\d+\.)\s+`, 0)
	listLinkLine   = newLazy(`[-*+]\s+\[.*?\]\(https?://`, 0)
	boldRun        = newLazy(`\*\*[^*]+\*\*`, 0)
	detailsHint    = newLazy(`<details`, pyre.IGNORECASE)
	detailsToken   = newLazy(`<!--|<(/?)(details|script|style|textarea)(?=[\s/>]|$)`, pyre.IGNORECASE)
)

// pyNumber は Python の int か float (json.dumps の出力が違う: 0 と 0.0)。本家は字が 0 のとき比率を int の 0 にする。
type pyNumber struct {
	f     float64
	isInt bool
}

func (n pyNumber) value() pyjson.Value {
	if n.isInt {
		return int64(n.f)
	}
	return n.f
}

// metrics は analyze_markdown_metrics の戻り値 (JSON の metrics の欄)。
type metrics struct {
	charCount, totalLines, listLines, boldCount int
	listRatio, boldPer1000                      pyNumber
	// detailsListLines は組になった <details> がある文書にだけ出す (hasDetails)。
	detailsListLines int
	hasDetails       bool
}

func (m metrics) value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("char_count", int64(m.charCount))
	o.Set("total_lines", int64(m.totalLines))
	o.Set("list_lines", int64(m.listLines))
	o.Set("list_ratio", m.listRatio.value())
	o.Set("bold_count", int64(m.boldCount))
	o.Set("bold_per_1000", m.boldPer1000.value())
	if m.hasDetails {
		o.Set("details_list_lines", int64(m.detailsListLines))
	}
	return o
}

// nonSpaceCount は `len("".join(text.split()))` (Python の空白を全部除いた字数)。
func nonSpaceCount(text string) int {
	n := 0
	for _, r := range pyunicode.Runes(text) {
		if !pyunicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// isOpaqueRow は指標と語の走査が見ない行の種類 (コード・frontmatter・HTML のブロック・参照の定義)。
func isOpaqueRow(kind string) bool {
	return kind == "code" || kind == "frontmatter" || kind == "html_block" || kind == "reference_definition"
}

// metricsAndListGate は本家の _metrics_and_list_gate。指標と、箇条書きの判定用の字数 (<details> の中の箇条書きを除く)、点数用の地の文の字数を返す。
func (d *document) metricsAndListGate() (m metrics, listGateChars, proseChars int, err error) {
	type plainRow struct {
		row     *visibility.Line
		visible string
	}
	var plainRows []plainRow
	for _, row := range d.an.Lines {
		if isOpaqueRow(row.Kind) || row.QuoteDepth != 0 || row.Kind == "table" {
			continue
		}
		visible, verr := d.an.LineVisibleText(row.Line, metricOpaqueKinds)
		if verr != nil {
			return m, 0, 0, verr
		}
		if d.imageBreakRow(row, visible) {
			continue
		}
		if pystr.Strip(row.Raw) != "" {
			plainRows = append(plainRows, plainRow{row, visible})
			if row.Kind != "heading" && row.Kind != "heading_underline" && row.Kind != "thematic_break" {
				proseChars += nonSpaceCount(visible)
			}
		}
	}
	m.totalLines = len(plainRows)
	var inside map[int]bool
	inside, m.hasDetails = d.detailsInnerLines()
	detailsListChars := 0
	var content []string
	for _, p := range plainRows {
		content = append(content, p.visible)
		if !mustMatchAt0(listMarkerLine, p.row.Raw) || mustSearch(listLinkLine, p.row.Raw) {
			continue
		}
		if m.hasDetails && inside[p.row.Line] {
			m.detailsListLines++
			detailsListChars += nonSpaceCount(p.visible)
		} else {
			m.listLines++
		}
	}
	plainContent := strings.Join(content, "\n")
	m.boldCount = len(mustFindall(boldRun, plainContent))
	m.charCount = nonSpaceCount(plainContent)
	m.boldPer1000 = pyNumber{isInt: true}
	if m.charCount > 0 {
		m.boldPer1000 = pyNumber{f: float64(m.boldCount) / float64(m.charCount) * 1000}
	}
	ratioBase := m.totalLines - m.detailsListLines
	m.listRatio = pyNumber{isInt: true}
	if ratioBase > 0 {
		m.listRatio = pyNumber{f: float64(m.listLines) / float64(ratioBase)}
	}
	if !m.listRatio.isInt {
		m.listRatio.f, err = pyjson.Round(m.listRatio.f, 3)
	}
	if err == nil && !m.boldPer1000.isInt {
		m.boldPer1000.f, err = pyjson.Round(m.boldPer1000.f, 2)
	}
	return m, m.charCount - detailsListChars, proseChars, err
}

// detailsInnerLines は本家の _details_inner_lines。組になった <details> と </details> の間にある行の番号。組が 1 つもなければ ok が偽。
// 開きと閉じは HTML のブロックの行 (引用の外) でだけ見る。段落の中・インラインのコード・コードの囲み・HTML のコメントと script などの中の <details> は見ない。
// 閉じは、まだ閉じていない一番新しい開きと組にする (入れ子)。組にならない開きと閉じは見ない。
func (d *document) detailsInnerLines() (map[int]bool, bool) {
	if !mustSearch(detailsHint, d.an.Text) {
		return nil, false
	}
	var opened []int
	type pair struct{ first, last int }
	var pairs []pair
	inComment, rawClose := false, (*pyre.Pattern)(nil)
	for _, row := range d.an.Lines {
		if row.Kind != "html_block" || row.QuoteDepth != 0 {
			inComment, rawClose = false, nil
			continue
		}
		text := pyre.NewText(row.Raw)
		position := 0
		for position < text.Len() {
			if inComment {
				end := strings.Index(text.Slice(position, text.Len()), "-->")
				if end < 0 {
					break
				}
				inComment, position = false, position+pyunicode.RuneCount(text.Slice(position, text.Len())[:end])+3
				continue
			}
			if rawClose != nil {
				found, err := rawClose.Search(text, position, pyre.NoEnd)
				if err != nil {
					panic(err)
				}
				if found == nil {
					break
				}
				rawClose, position = nil, found.End(0)
				continue
			}
			token, err := detailsToken.Get().Search(text, position, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			if token == nil {
				break
			}
			position = token.End(0)
			whole, _ := token.Group(0)
			closing, _ := token.Group(1)
			name, _ := token.Group(2)
			switch {
			case whole == "<!--":
				inComment = true
			case !isDetailsName(name):
				if closing == "" {
					rawClose = rawCloseTag(name)
				}
			case closing == "":
				opened = append(opened, row.Line)
			case len(opened) > 0:
				pairs = append(pairs, pair{opened[len(opened)-1], row.Line})
				opened = opened[:len(opened)-1]
			}
		}
	}
	if len(pairs) == 0 {
		return nil, false
	}
	inside := map[int]bool{}
	for _, p := range pairs {
		for line := p.first + 1; line < p.last; line++ {
			inside[line] = true
		}
	}
	return inside, true
}

// isDetailsName は `token.group(2).lower() == "details"`。re.I は ſ・ı・İ も当てるが、str.lower() はそれらを ASCII にしないので、ASCII の字だけの綴りだけが真。
func isDetailsName(name string) bool {
	for i := range len(name) {
		if name[i] >= 0x80 {
			return false
		}
	}
	return strings.EqualFold(name, "details")
}

var (
	rawCloseMu    sync.Mutex
	rawCloseCache = map[string]*pyre.Pattern{}
)

// rawCloseTag は script・style・textarea の閉じ (`</名前` の後ろが空白・> か行末)。名前は 3 つの語を re.I で当てた綴りの数通りだけなので、組んだものを覚える。
func rawCloseTag(name string) *pyre.Pattern {
	rawCloseMu.Lock()
	defer rawCloseMu.Unlock()
	p, ok := rawCloseCache[name]
	if !ok {
		p = pyre.MustCompile(`</`+name+`(?=[\s>]|$)`, pyre.IGNORECASE)
		rawCloseCache[name] = p
	}
	return p
}

// g01Document は指標から文書単位の候補 (excess_bold・excess_list。どちらも line 1) を作る (本家 5546-5570)。
// message の数は本家の f-string (charCount > 300 なら字数が 0 でないので bold_per_1000 は float の repr。round(list_ratio*100, 1) は pyjson.Round を通す)。
func (s *lintState) g01Document() {
	m := s.metrics
	if m.charCount > 300 && m.boldPer1000.f > 3.0 {
		s.add(newFinding("excess_bold", 1, "warn",
			"太字の頻度（1,000字あたり "+pyjson.FloatRepr(m.boldPer1000.f)+"個）が設定した目安を超えています（推奨: 2.0以下）。強調の役割を確かめ、不要なものだけ整理してください。",
			"太字数: "+strconv.Itoa(m.boldCount)+"回 / "+strconv.Itoa(m.charCount)+"文字"))
	}
	if s.listGateChars > 300 && m.listRatio.f > 0.25 {
		percent, err := pyjson.Round(m.listRatio.f*100, 1)
		if err != nil {
			panic(err)
		}
		snippet := "リスト行: " + strconv.Itoa(m.listLines) + " / 全非空行: " + strconv.Itoa(m.totalLines-m.detailsListLines)
		if m.detailsListLines > 0 {
			snippet += "（<details> の中の箇条書き" + strconv.Itoa(m.detailsListLines) + "行は数えていません）"
		}
		s.add(newFinding("excess_list", 1, "warn",
			"箇条書きの比率（"+pyjson.FloatRepr(percent)+"%）が設定した目安を超えています（推奨: 15%以下）。項目の役割を確かめ、不要なものだけ整理してください。",
			snippet))
	}
}
