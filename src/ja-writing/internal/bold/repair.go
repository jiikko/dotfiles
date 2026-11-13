package bold

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 1 つの段落に崩れた太字が多いときに、直し方の案を、段落を解析し直さずに元の解析から確かめる近道 (本家 2395-2408 行の _BoldRepairIndex)。
// 案は ** を内側の 1 字の向こうへ動かすか、外側に半角スペースを 1 つ入れるだけなので、次の 3 段が言えるときは解析し直したときと同じ答えになる。
//  1. 行の種類とブロックの分かれ方が変わらない  2. インラインの保護範囲が、位置のずれを除いて変わらない
//  3. 2 つの規則の太字の組み合わせで、狙いの組ができ、ほかの組が全部残る
// 言えないときは decide が決められない (decideUnknown) を返し、呼び出し側が解析し直す。

const lineStartMarks = " \t>-+*.)#`~["

// lineStartStopper は _line_start_stopper (行頭の印を読む処理が必ず止まる字か)。数字は isdigit で広めにとる。
func lineStartStopper(c rune) bool {
	return c != noChar && !strings.ContainsRune(lineStartMarks, c) && !pyunicode.IsDigit(c)
}

// intervals は _merged_intervals の結果 (並べてつないだ範囲と、その終わりの列)。
type intervals struct {
	spans [][2]int
	ends  []int
}

func mergedIntervals(spans [][2]int) intervals {
	sorted := slices2(spans)
	var out intervals
	for _, s := range sorted {
		if n := len(out.spans); n > 0 && s[0] <= out.spans[n-1][1] {
			out.spans[n-1][1] = max(out.spans[n-1][1], s[1])
		} else {
			out.spans = append(out.spans, s)
		}
	}
	for _, s := range out.spans {
		out.ends = append(out.ends, s[1])
	}
	return out
}

func slices2(spans [][2]int) [][2]int {
	out := append([][2]int{}, spans...)
	sort.Slice(out, func(a, b int) bool {
		if out[a][0] != out[b][0] {
			return out[a][0] < out[b][0]
		}
		return out[a][1] < out[b][1]
	})
	return out
}

// bisectRight は bisect_right(xs, x)。
func bisectRight(xs []int, x int) int {
	return sort.Search(len(xs), func(k int) bool { return xs[k] > x })
}

func (iv intervals) overlaps(left, right int) bool {
	k := bisectRight(iv.ends, left)
	return k < len(iv.spans) && iv.spans[k][0] < right
}

func (iv intervals) covers(position int) bool {
	k := bisectRight(iv.ends, position)
	return k < len(iv.spans) && iv.spans[k][0] <= position
}

// splits は _interval_splits (position の直前と直後の字が、同じつながった範囲に入るか)。
func (iv intervals) splits(position int) bool {
	k := bisectRight(iv.ends, position)
	return k < len(iv.spans) && iv.spans[k][0] < position
}

// repairIndex は _BoldRepairIndex (1 つのブロックの元の解析の結果)。
type repairIndex struct {
	text          []rune
	runs          [][2]int
	runIndex      map[[2]int]int
	lineStarts    []int
	contentStarts []int
	lineEnds      []int
	firstStoppers map[int]int
	raw           intervals
	masks         intervals
	urlStarts     []int
	spaces        []int
	unsureLinks   []int
	parsers       [2]*stack
	emphasis      [2]map[emph]bool
}

// referenceStart は _REFERENCE_START.match (` {0,3}\[`)。
func referenceStart(s []rune) bool {
	k := 0
	for k < len(s) && k < 3 && s[k] == ' ' {
		k++
	}
	return k < len(s) && s[k] == '['
}

// repairIndexOf は _bold_repair_index (ブロックごとに 1 回だけ作る。速く確かめられないブロックでは nil)。
func repairIndexOf(text []rune, c *scanContext) (*repairIndex, error) {
	if c.haveRepair {
		return c.repair, nil
	}
	var index *repairIndex
	// 「<」があると、HTML の組み立てと自動リンクが遠くまで読むので、近くだけを見る確かめができない (本家 2463 行)。
	if !slices.Contains(text, '<') {
		a, err := visibility.AnalyzeMarkdown(str(text), false)
		if err != nil {
			return nil, err
		}
		if slices.Equal(runsFromMasks(text, a.MaskSpans), c.runs) && !hasReferenceDefinition(a) && !blockStartsWithReference(a) {
			index = newRepairIndex(text, c, a)
		}
	}
	c.repair, c.haveRepair = index, true
	return index, nil
}

func hasReferenceDefinition(a *visibility.Analysis) bool {
	for _, row := range a.Lines {
		if row.Kind == "reference_definition" {
			return true
		}
	}
	return false
}

func blockStartsWithReference(a *visibility.Analysis) bool {
	for _, b := range a.Blocks {
		switch b.Kind {
		case "paragraph", "list", "quote", "heading":
			if referenceStart(pyunicode.Runes(b.Lines[0].Content)) {
				return true
			}
		}
	}
	return false
}

func newRepairIndex(text []rune, c *scanContext, a *visibility.Analysis) *repairIndex {
	ix := &repairIndex{text: text, runs: c.runs, runIndex: c.runIndexOf(), firstStoppers: map[int]int{}}
	for _, row := range a.Lines {
		ix.lineStarts = append(ix.lineStarts, row.Start)
		ix.contentStarts = append(ix.contentStarts, row.ContentStart)
		ix.lineEnds = append(ix.lineEnds, row.Start+len(pyunicode.Runes(row.Raw)))
	}
	spans := c.opaqueSpans
	if !c.haveOpaque {
		spans = visibility.InlineProtectedSpans(str(text))
	}
	raw := make([][2]int, len(spans))
	for k, s := range spans {
		raw[k] = [2]int{s.Start, s.End}
	}
	ix.raw = mergedIntervals(raw)
	ix.masks = intervals{spans: a.MaskSpans, ends: a.MaskEnds}
	for k := range text {
		if visibility.HasPrefixAt(text, "https://", k) || visibility.HasPrefixAt(text, "http://", k) || visibility.HasPrefixAt(text, "www.", k) {
			ix.urlStarts = append(ix.urlStarts, k)
		}
		if pyunicode.IsSpace(text[k]) {
			ix.spaces = append(ix.spaces, k)
		}
		// リンクの行き先を読んで失敗した「](」は、どこまで読んだか分からない。手前にあれば確かめない (本家 2495 行)。
		if visibility.HasPrefixAt(text, "](", k) && (!ix.raw.covers(k+1) || !ix.masks.covers(k+1)) {
			ix.unsureLinks = append(ix.unsureLinks, k)
		}
	}
	for _, p := range profiles {
		set := map[emph]bool{}
		src := c.gfm
		if p == common {
			src = c.common
		}
		for _, e := range src {
			set[e] = true
		}
		ix.emphasis[p] = set
	}
	return ix
}

func (ix *repairIndex) firstStopper(line int) int {
	if position, ok := ix.firstStoppers[line]; ok {
		return position
	}
	position, end := ix.lineStarts[line], ix.lineEnds[line]
	for position < end && !lineStartStopper(ix.text[position]) {
		position++
	}
	ix.firstStoppers[line] = position
	return position
}

// lineStartKept は _line_start_kept (edit より前を変えない書き換えで、その行の行頭の読み方が変わらないか)。
func (ix *repairIndex) lineStartKept(edit int, inserted bool, after func(int) rune) bool {
	line := bisectRight(ix.lineStarts, edit) - 1
	if line < 0 || edit < ix.contentStarts[line] {
		return false
	}
	if ix.firstStopper(line) < edit {
		return true
	}
	// 中身の頭 (行頭の印のあと、半角スペースとタブのあと) が ** なら、行頭を読む処理は 2 つめの * で止まる。
	text, lead := ix.text, ix.contentStarts[line]
	for lead < edit && (text[lead] == ' ' || text[lead] == '\t') {
		lead++
	}
	if visibility.HasPrefixAt(text, "**", lead) && edit >= lead+2 {
		return true
	}
	if edit == lead && !inserted {
		before := visibility.HasPrefixAt(text, "**", edit) || lineStartStopper(charAt(text, edit))
		afterOK := (after(edit) == '*' && after(edit+1) == '*') || lineStartStopper(after(edit))
		return before && afterOK
	}
	return false
}

func (ix *repairIndex) parser(p profile, index int) *stack {
	s := ix.parsers[p]
	if s == nil || s.index > index {
		s = newStack(ix.text, p, ix.runs)
		ix.parsers[p] = s
	}
	s.advance(index)
	return s
}

// verdict は decide の答え (本家の True / False / None)。
type verdict int

const (
	decideUnknown verdict = iota
	decideOK
	decideNG
)

// decide は _BoldRepairIndex.decide。案 middle (形 shape) で直した文が、ブロックを解析し直して確かめたときに通るか。
func (ix *repairIndex) decide(i, j int, shape string, middle []rune) verdict {
	text := ix.text
	before, ok := ix.runIndex[[2]int{i, i + 2}]
	if !ok {
		return decideUnknown
	}
	if b, ok := ix.runIndex[[2]int{j, j + 2}]; !ok || b != before+1 {
		return decideUnknown
	}
	type edit struct {
		position int
		inserted bool
	}
	var changed [][2]int
	var gaps []int
	var edits []edit
	var first, second, delta int
	switch shape {
	case shapeBracket:
		// ASCII のかっこを動かすと、隣り合う「](」と「![」が生まれることがある (リンクと画像の記法)
		if text[i+2] == '(' && charAt(text, i-1) == ']' {
			return decideUnknown
		}
		if text[i+2] == '[' && (charAt(text, i-1) == '!' || charAt(text, j+2) == '(') {
			return decideUnknown
		}
		changed = [][2]int{{i, i + 3}, {j - 1, j + 2}}
		edits = []edit{{i, false}, {j - 1, false}}
		first, second, delta = i+1, j-1, 0
	case shapePunct:
		if text[j-1] == '!' && charAt(text, j+2) == '[' {
			return decideUnknown // 「!」が「[」の前に来ると画像の記法になる
		}
		changed = [][2]int{{j - 1, j + 2}}
		edits = []edit{{j - 1, false}}
		first, second, delta = i, j-1, 0
	default:
		left := 0
		for left < len(middle) && middle[left] == ' ' {
			left++
		}
		right := 0
		for right < len(middle) && middle[len(middle)-1-right] == ' ' {
			right++
		}
		if left > 1 || right > 1 || len(middle) != left+right+j-i+2 || !slices.Equal(middle[left:len(middle)-right], text[i:j+2]) {
			return decideUnknown
		}
		for _, g := range [2][2]int{{i, left}, {j + 2, right}} {
			if g[1] > 0 {
				gaps = append(gaps, g[0])
				edits = append(edits, edit{g[0], true})
			}
		}
		first, second, delta = i+left, j+left, left+right
	}
	end := i + len(middle)
	after := func(position int) rune {
		switch {
		case position < 0:
			return noChar
		case position < i:
			return text[position]
		case position < end:
			return middle[position-i]
		}
		return charAt(text, position-delta)
	}

	if len(changed) > 0 || len(gaps) > 0 {
		if (shape == shapeBracket || (len(gaps) > 0 && gaps[0] == i)) && i > 0 && text[i-1] == '\\' {
			return decideUnknown
		}
		if (shape == shapeBracket || shape == shapePunct) && text[j-2] == '\\' {
			return decideUnknown
		}
		for _, c := range changed {
			if ix.raw.overlaps(c[0], c[1]) || ix.masks.overlaps(c[0], c[1]) {
				return decideUnknown
			}
		}
		for _, position := range gaps {
			if ix.raw.splits(position) || ix.masks.splits(position) {
				return decideUnknown
			}
		}
		if len(ix.unsureLinks) > 0 && ix.unsureLinks[0] <= j+2 {
			return decideUnknown
		}
		space := bisectRight(ix.spaces, i-1) - 1
		wordStart := -1
		if space >= 0 {
			wordStart = ix.spaces[space]
		}
		url := bisectRight(ix.urlStarts, wordStart)
		if url < len(ix.urlStarts) && ix.urlStarts[url] <= j+3 {
			return decideUnknown
		}
		checked := map[int]bool{}
		for _, e := range edits {
			line := bisectRight(ix.lineStarts, e.position) - 1
			if checked[line] {
				continue // 同じ行の左の書き換えで、行頭の読み方は決まっている
			}
			checked[line] = true
			if !ix.lineStartKept(e.position, e.inserted, after) {
				return decideUnknown
			}
		}
	}

	result := decideOK
	for _, p := range profiles {
		openA, closeA := roles(text, i, i+2, p)
		openB, closeB := roles(text, j, j+2, p)
		openFirst, closeFirst := rolesAround(after(first-1), after(first+2), p)
		_, closeSecond := rolesAround(after(second-1), after(second+2), p)
		if !openFirst || !closeSecond {
			return decideNG
		}
		state := ix.parser(p, before)
		if closeFirst && state.findsOpener(2, openFirst) {
			return decideNG
		}
		if closeA && state.findsOpener(2, openA) {
			return decideUnknown // 元の解析と食い違う (起きないはずの形)
		}
		// 閉じる相手が見つからなかった位置の記録 (bottoms) を、書き換えの前と後で比べる。
		gfmBounds := p == gfm
		original, repaired := map[bottomKey]int{}, map[bottomKey]int{}
		type failure struct {
			key    bottomKey
			opens  bool
			bottom int
		}
		var failed []failure
		get := func(m map[bottomKey]int, key bottomKey, fallback int) int {
			if v, ok := m[key]; ok {
				return v
			}
			return fallback
		}
		if closeA {
			key := bottomKeyOf(2, gfmBounds, openA)
			failed = append(failed, failure{key, openA, get(state.bottoms, key, -1)})
			original[key] = i
		}
		if closeB && !ix.emphasis[p][emph{i, j, 2}] {
			key := bottomKeyOf(2, gfmBounds, openB)
			failed = append(failed, failure{key, openB, get(original, key, get(state.bottoms, key, -1))})
			original[key] = j
		}
		if closeFirst {
			key := bottomKeyOf(2, gfmBounds, openFirst)
			failed = append(failed, failure{key, openFirst, get(state.bottoms, key, -1)})
			repaired[key] = first
		}
		keys := map[bottomKey]bool{}
		for k := range original {
			keys[k] = true
		}
		for k := range repaired {
			keys[k] = true
		}
		// 鍵を回す順は答えに出ない (中で途中で返さず、result を「決められない」にするかだけを決める)。
		for key := range keys {
			kept := get(state.bottoms, key, -1)
			o, r := get(original, key, kept), get(repaired, key, kept)
			if o == r || min(o, r) >= i {
				continue // 同じ値か、どちらも手前のどの印よりも後ろ
			}
			// 片方だけが kept のまま。手前の印のうち kept 以降のものが、後ろの閉じの印から選ばれうると食い違う。
			if len(state.openers) == 0 || state.openers[len(state.openers)-1].start < kept {
				continue
			}
			// この鍵で kept を下限に相手を探して見つからなかった閉じる側の記号があれば、そのとき選べた組の記号はどれも kept より前にある。
			// CommonMark では同じ鍵の閉じる側はいつも同じ組を選べるので食い違わない。GFM では、開く側にもなれる閉じる側が選べない (1, False) の組だけが残る。
			var proofs []bool
			for _, f := range failed {
				if f.key == key && f.bottom == kept {
					proofs = append(proofs, f.opens)
				}
			}
			if len(proofs) > 0 && (!gfmBounds || !allTrue(proofs)) {
				continue
			}
			bucket := state.buckets[1][0]
			if len(proofs) > 0 && (len(bucket) == 0 || bucket[len(bucket)-1].start < kept) {
				continue
			}
			result = decideUnknown
		}
	}
	return result
}

func allTrue(bs []bool) bool {
	for _, b := range bs {
		if !b {
			return false
		}
	}
	return true
}

func str(r []rune) string { return pyunicode.FromRunes(r) }
