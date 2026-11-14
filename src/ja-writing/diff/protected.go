package diff

import (
	"slices"
	"sort"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
)

// 群 P の変えない箇所 (本家の _protected_risks・_protected_spans・_protected_contents・_protected_hits・_is_quote_row)。
// 変わった文字の範囲が protected_spans (inline_code、autolink、link_destination、bare_url、code、frontmatter、html_block、html_token、リンクの定義、画像のパス) か引用のブロックに重なれば出す。

// protectedSubkinds は protected_spans の種類 → subkind。リンクの定義と画像のパスも、リンク先 (link_destination) と同じく link にする。
// 数値の取り出しでは伏せるので、ここで出さないとどの判定からも漏れる。画像は ( ) の中のパスだけを見る (代わりの文は本文)。
var protectedSubkinds = map[string]string{
	"inline_code": "inline_code", "code": "code", "autolink": "url", "bare_url": "url",
	"link_destination": "link", "reference_definition": "link", "image": "link",
	"frontmatter": "frontmatter", "html_block": "html", "html_token": "html",
}

// protectedLines は追加と削除の塊を、行の種類で決める対応。
var protectedLines = map[string]string{"code": "code", "frontmatter": "frontmatter", "html_block": "html"}

type protectedSpan struct {
	left, right int
	kind        string
}

// protectedSpanList は範囲 (位置の順) と、その始まりの位置の並び。
type protectedSpanList struct {
	spans []protectedSpan
	lefts []int
}

func (s *sides) isQuoteRow(side, lineNo int) bool {
	rows := s.analyses[side].Lines
	if lineNo-1 >= len(rows) {
		return false
	}
	row := rows[lineNo-1]
	return row.QuoteDepth != 0 || row.Kind == "quote"
}

// protectedSpansOf はその側の変えない箇所の範囲。画像はパスだけを範囲にする。
func (s *sides) protectedSpansOf(side int) protectedSpanList {
	var found []protectedSpan
	text := s.texts[side]
	for _, sp := range s.analyses[side].ProtectedSpans {
		left, right, kind := sp.Start, sp.End, sp.Kind
		if _, ok := protectedSubkinds[kind]; !ok {
			continue
		}
		switch {
		case kind == "image":
			left = imagePathStart(text, left, right)
			if left >= right {
				continue
			}
		case kind == "reference_definition" && right > left && right-1 < len(text) && text[right-1] == '\n':
			right--
		}
		found = append(found, protectedSpan{left, right, kind})
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.left != b.left {
			return a.left < b.left
		}
		if a.right != b.right {
			return a.right < b.right
		}
		return a.kind < b.kind
	})
	lefts := make([]int, len(found))
	for i, f := range found {
		lefts[i] = f.left
	}
	return protectedSpanList{found, lefts}
}

type protectedHit struct {
	subkind, block string
	line           int
}

// hits は文字の範囲 [left, right) に重なる、変えない箇所の (subkind, 種類, 行番号) (行番号は重なりが始まる行)。
func (s *sides) protectedHits(side, left, right int, spans protectedSpanList) []protectedHit {
	var hits []protectedHit
	for i := max(0, bisectRight(spans.lefts, left)-1); i < len(spans.spans) && spans.spans[i].left < right; i++ {
		sp := spans.spans[i]
		if sp.right > left {
			hits = append(hits, protectedHit{protectedSubkinds[sp.kind], sp.kind, s.lineOf(side, max(left, sp.left))})
		}
	}
	for lineNo, last := s.lineOf(side, left), s.lineOf(side, right-1); lineNo <= last; lineNo++ {
		if s.isQuoteRow(side, lineNo) {
			hits = append(hits, protectedHit{"quote", "quote", lineNo})
			break
		}
	}
	return hits
}

type protectedItem struct {
	block, text string
	line        int
}

// orderedItems は subkind の最初に出た順を持つ { subkind: [項目] }。
type orderedItems struct {
	keys  []string
	items map[string][]protectedItem
}

func (o *orderedItems) add(subkind string, item protectedItem) {
	if o.items == nil {
		o.items = map[string][]protectedItem{}
	}
	if _, ok := o.items[subkind]; !ok {
		o.keys = append(o.keys, subkind)
	}
	o.items[subkind] = append(o.items[subkind], item)
}

// protectedContents は hunk のその側の行の中の、変えない箇所の中身 (位置の順)。範囲が hunk の外にまたがるときは、hunk の中の部分だけを中身にする。引用は行ごと。
func (s *sides) protectedContents(side int, h *hunk, spans protectedSpanList) *orderedItems {
	first, last := h.oldStart, h.oldEnd
	if side == 1 {
		first, last = h.newStart, h.newEnd
	}
	found := &orderedItems{}
	if last < first {
		return found
	}
	low := s.starts[side][first-1]
	high := s.starts[side][last-1] + pystr.Len(s.lines[side][last-1])
	for i := max(0, bisectRight(spans.lefts, low)-1); i < len(spans.spans) && spans.spans[i].left < high; i++ {
		sp := spans.spans[i]
		if sp.right > low {
			start := max(sp.left, low)
			found.add(protectedSubkinds[sp.kind], protectedItem{sp.kind, runeSlice(s.texts[side], start, min(sp.right, high)), s.lineOf(side, start)})
		}
	}
	for lineNo := first; lineNo <= last; lineNo++ {
		if s.isQuoteRow(side, lineNo) {
			found.add("quote", protectedItem{"quote", s.lines[side][lineNo-1], lineNo})
		}
	}
	return found
}

func sameItemTexts(a, b []protectedItem) bool {
	return slices.EqualFunc(a, b, func(x, y protectedItem) bool { return x.text == y.text })
}

// protectedFound は 1 つの hunk で見つけた変えない箇所 (subkind ごとに 1 つ。最初に出た順)。行は 0 なら None。
type protectedFound struct {
	keys    []string
	block   map[string]string
	lineNos map[string]*[2]int
}

func (f *protectedFound) hit(subkind, block string, side, lineNo int) {
	if f.block == nil {
		f.block, f.lineNos = map[string]string{}, map[string]*[2]int{}
	}
	if _, ok := f.block[subkind]; !ok {
		f.keys = append(f.keys, subkind)
		f.block[subkind], f.lineNos[subkind] = block, &[2]int{}
	}
	if f.lineNos[subkind][side] == 0 {
		f.lineNos[subkind][side] = lineNo
	}
}

// protectedRisks は変えない箇所 (kind protected)。空白だけの変更でも出す。追加と削除の塊は行の種類で決める。hunk と subkind ごとに 1 つ。
// 文字の対応を省いた置き換えの塊 (予算) は、変えない箇所の中身を種類ごとに両側で並べて比べ、違う種類だけを出す (インラインのコードやリンク先は行の種類に出ないため)。
// 行番号は、元と後のそれぞれで最初に当たった行。片側だけで当たったときは、1 行が 1 行に変わった塊なら相手の行を、そうでなければ None を入れる
// (大きな書き換えで、関係のない行を相手として出さないため)。
func (s *sides) protectedRisks(full *fullDiff) []riskItem {
	spans := [2]protectedSpanList{s.protectedSpansOf(0), s.protectedSpansOf(1)}
	var out []riskItem
	for _, h := range full.hunks {
		found := &protectedFound{}
		switch {
		case h.hasSegments:
			bases := [2]int{s.starts[0][h.oldStart-1], s.starts[1][h.newStart-1]}
			var offsets [2]int
			for _, seg := range h.segments {
				n := pystr.Len(seg.text)
				if seg.tag == "=" {
					offsets[0] += n
					offsets[1] += n
					continue
				}
				side := 1
				if seg.tag == "-" {
					side = 0
				}
				left := bases[side] + offsets[side]
				for _, hit := range s.protectedHits(side, left, left+n, spans[side]) {
					found.hit(hit.subkind, hit.block, side, hit.line)
				}
				offsets[side] += n
			}
		case h.tag == "replace":
			contents := [2]*orderedItems{s.protectedContents(0, h, spans[0]), s.protectedContents(1, h, spans[1])}
			seen := map[string]bool{}
			for _, subkind := range append(append([]string{}, contents[0].keys...), contents[1].keys...) {
				if seen[subkind] {
					continue
				}
				seen[subkind] = true
				oldItems, newItems := contents[0].items[subkind], contents[1].items[subkind]
				if sameItemTexts(oldItems, newItems) {
					continue
				}
				for side, items := range [2][]protectedItem{oldItems, newItems} {
					other := newItems
					if side == 1 {
						other = oldItems
					}
					differs := false
					for position, it := range items {
						if position >= len(other) || other[position].text != it.text {
							found.hit(subkind, it.block, side, it.line)
							differs = true
							break
						}
					}
					if !differs && len(items) > 0 {
						found.hit(subkind, items[0].block, side, items[0].line)
					}
				}
			}
		default:
			for side, r := range [2][2]int{{h.oldStart, h.oldEnd}, {h.newStart, h.newEnd}} {
				rows := s.analyses[side].Lines
				for lineNo := r[0]; lineNo <= r[1]; lineNo++ {
					row := rows[lineNo-1]
					subkind := protectedLines[row.Kind]
					if subkind == "" && s.isQuoteRow(side, lineNo) {
						subkind = "quote"
					}
					if subkind != "" {
						block := row.Kind
						if subkind == "quote" {
							block = "quote"
						}
						found.hit(subkind, block, side, lineNo)
					}
				}
			}
		}
		oneToOne := h.tag == "replace" && h.oldStart == h.oldEnd && h.newStart == h.newEnd
		for order, subkind := range found.keys {
			oldLine, newLine := found.lineNos[subkind][0], found.lineNos[subkind][1]
			if oneToOne {
				oldLine, newLine = h.oldStart, h.newStart
			}
			var old, new string
			if oldLine != 0 {
				old = s.lines[0][oldLine-1]
			}
			if newLine != 0 {
				new = s.lines[1][newLine-1]
			}
			d := pyjson.NewObject()
			d.Set("block", found.block[subkind])
			out = append(out, riskItem{h.index, order, newRisk("protected", subkind, oldLine, newLine, old, new, oldLine != 0, newLine != 0, d)})
		}
	}
	return out
}

// ProtectedRisks は元の文と書き直した文の、変えない箇所の変化の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func ProtectedRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.protectedRisks(full)), nil
}
