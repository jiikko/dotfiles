package diff

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 K: 見出しの構造。本家の _heading_structure とその部品。全文差分の hunk と Markdown の解析から、見出しの数・段・見出しと本文の行の入れ替わり・
// 見出しの形の変化を「構造」の見直し候補の文にする。位置は 0 から数える行の番号 (本家と同じ)。

const (
	headingShown        = 3   // 1 つの文に挙げる行の組の数 (ほかは「ほか N か所」)
	headingLineWidth    = 30  // 行を挙げるときの字数の上限 (超えたら「…」で切る)
	headingPairLimit    = 300 // 組にし直す見出しの数の上限 (hunk の片側。超えたら組にし直さない)
	headingBodyChars    = 300 // 見出しの組み直しで比べる、見出しのあとの節の本文の字数
	headingPairMin      = 0.1 // 組み直す見出しの似かたの下限
	headingSameLevel    = 0.2 // 段が同じ見出しどうしに足す似かた
	headingShapeChars   = 15  // 見出しの形の検査で見る元の見出しの字数の上限
	headingRemovedShown = 3   // 消えた見出しを挙げる数
	headingRatioMin     = 0.5 // 順に組にする見出しの文字の似かたの下限
)

const headingKeyTail = "：:"

var (
	atxMarkers   = pyre.NewLazy(`^[ \t]{0,3}#{1,6}(?:[ \t]+|$)|[ \t]+#+[ \t]*$`, 0)
	boldOnlyLine = pyre.NewLazy(`^[ \t]{0,3}(?:\*\*|__)(?![ \t])(.+?)(?<![ \t])(?:\*\*|__)[ \t]*$`, 0)
)

// headingEntry は見出しのブロック 1 つの (段, 印と空白を除いた文字)。
type headingEntry struct {
	level int
	key   string
}

// headingRows は本家の _heading_rows: 見出しのブロックごとの {最初の行の位置: (段, 文字)} と、見出しのブロックの行の位置からその見出しの最初の行の位置への対応 (setext の下線の行を含む)。
func (s *sides) headingRows(side int) (map[int]headingEntry, map[int]int) {
	a := s.analyses[side]
	heads, inside := map[int]headingEntry{}, map[int]int{}
	for _, block := range a.Blocks {
		if block.Kind != "heading" {
			continue
		}
		first := block.Lines[0]
		var indexes []int
		for _, row := range block.Lines {
			indexes = append(indexes, row.Line-1)
		}
		var atx *atxParts
		if len(block.Lines) == 1 {
			atx = atxMatch(first.Content)
		}
		var level int
		if atx != nil {
			level = atx.level
		} else {
			after := indexes[len(indexes)-1] + 1
			if after >= len(a.Lines) || a.Lines[after].Kind != "heading_underline" {
				continue
			}
			// 箇条書きの項目のあとの、字下げのない「-」「---」は次の項目か区切り線で、下線ではない (数えない)
			if a.Lines[after].ContentStart-a.Lines[after].Start < first.ContentStart-first.Start {
				continue
			}
			indexes = append(indexes, after)
			level = 2
			if strings.HasPrefix(pystr.Strip(a.Lines[after].Content), "=") {
				level = 1
			}
		}
		for _, i := range indexes {
			inside[i] = indexes[0]
		}
		var text strings.Builder
		for _, row := range block.Lines {
			text.WriteString(row.Content)
		}
		heads[indexes[0]] = headingEntry{level, headingKey(text.String())}
	}
	return heads, inside
}

// boldOnly は本家の _bold_only: 段落の行の全体が 1 つの太字なら、その中の文字 (「**a** と **b**」は 1 つの太字でない)。
func boldOnly(content string) (string, bool) {
	m, err := boldOnlyLine.Get().Match(pyre.NewText(content), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	if m == nil {
		return "", false
	}
	inner, _ := m.Group(1)
	if strings.Contains(inner, "**") || strings.Contains(inner, "__") {
		return "", false
	}
	return inner, true
}

// headingKey は本家の _heading_key: 見出しと本文の行を比べる文字 (見出しの #、全体の太字、空白、末尾のコロンを除く)。
func headingKey(text string) string {
	text = atxMarkers.MustSub("", pystr.Strip(text))
	if inner, ok := boldOnly(text); ok {
		text = inner
	}
	return pystr.RStripChars(spacesPattern.MustSub("", text), headingKeyTail)
}

// headingShown は本家の _heading_shown。
func headingShownText(line string) string {
	line = pystr.Strip(line)
	r := pyunicode.Runes(line)
	if len(r) <= headingLineWidth {
		return line
	}
	return pyunicode.FromRunes(r[:headingLineWidth]) + "…"
}

// headingText は本家の _heading_text: 見出しの行から ATX の # を除いた文字 (空白は残す。cut なら長いときに切る)。
func headingText(line string, cut bool) string {
	text := atxMarkers.MustSub("", pystr.Strip(line))
	if cut {
		return headingShownText(text)
	}
	return text
}

// headingCounterpart は本家の _heading_counterpart: hunk の外の行 (side の側の位置 index) が、相手の側で何行目に当たるか。
func headingCounterpart(hunks []*hunk, side, index int) int {
	found := index
	for _, h := range hunks {
		start, end, otherEnd := h.oldStart, h.oldEnd, h.newEnd
		if side == 1 {
			start, end, otherEnd = h.newStart, h.newEnd, h.oldEnd
		}
		if start-1 > index {
			break
		}
		found = otherEnd + (index - end)
	}
	return found
}

type gramSet map[string]struct{}

// headingBigrams は本家の _heading_bigrams: 2 文字の組の集合 (1 文字以下の見出しは、その文字そのもの 1 つ)。
func headingBigrams(key string) gramSet {
	r := pyunicode.Runes(key)
	set := gramSet{}
	for k := 0; k+1 < len(r); k++ {
		set[pyunicode.FromRunes(r[k:k+2])] = struct{}{}
	}
	if len(set) == 0 {
		set[key] = struct{}{}
	}
	return set
}

// jaccard は本家の _jaccard (どちらかが空なら 0)。
func jaccard(left, right gramSet) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	inter := 0
	for k := range left {
		if _, ok := right[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(left)+len(right)-inter)
}

// headingBody は本家の _heading_body: 見出し (index 行目) のあとの節の本文 (次の見出しの前まで、headingBodyChars 字まで)。
func headingBody(lines []string, heads map[int]headingEntry, index int) string {
	var pieces strings.Builder
	size := 0
	for at := index + 1; at < len(lines); at++ {
		if _, ok := heads[at]; ok {
			break
		}
		if piece := pystr.Strip(lines[at]); piece != "" {
			pieces.WriteString(piece)
			size += pystr.Len(piece)
			if size >= headingBodyChars {
				break
			}
		}
	}
	r := pyunicode.Runes(pieces.String())
	if len(r) > headingBodyChars {
		r = r[:headingBodyChars]
	}
	return pyunicode.FromRunes(r)
}

// headingScore は DP の 1 つの値 (本家の (組の数, 似かたの和) の組。大小は組の数、同じなら和)。
type headingScore struct {
	count int
	total float64
}

func (a headingScore) less(b headingScore) bool {
	return a.count < b.count || (a.count == b.count && a.total < b.total)
}

// headingOrderPairs は本家の _heading_order_pairs: 組にならなかった見出しを、同じ置き換えの hunk の中で順を保って組にする。
// 似かたは見出しの文字と見出しのあとの節の本文の 2 文字の組の重なり (段が同じなら足す)。下限に満たない見出しは組にしない。
// その中で組の数をいちばん多くし、似かたの和がいちばん大きい組み方を選ぶ。(元の位置, 後の位置) の並びを返す。
func (s *sides) headingOrderPairs(hunks []*hunk, oldHeads, newHeads map[int]headingEntry, usedOld, usedNew map[int]bool) [][2]int {
	var found [][2]int
	oldAt, newAt := sortedKeys(oldHeads), sortedKeys(newHeads)
	for _, h := range hunks {
		if h.tag != "replace" {
			continue
		}
		var old, new []int
		for _, i := range oldAt[sort.SearchInts(oldAt, h.oldStart-1):sort.SearchInts(oldAt, h.oldEnd)] {
			if !usedOld[i] && oldHeads[i].key != "" {
				old = append(old, i)
			}
		}
		for _, j := range newAt[sort.SearchInts(newAt, h.newStart-1):sort.SearchInts(newAt, h.newEnd)] {
			if !usedNew[j] && newHeads[j].key != "" {
				new = append(new, j)
			}
		}
		if len(old) == 0 || len(new) == 0 || len(old) > headingPairLimit || len(new) > headingPairLimit {
			continue
		}
		rows, cols := len(old), len(new)
		oldGrams, newGrams := make([]gramSet, rows), make([]gramSet, cols)
		oldBodies, newBodies := make([]gramSet, rows), make([]gramSet, cols)
		for a, i := range old {
			oldGrams[a] = headingBigrams(oldHeads[i].key)
			oldBodies[a] = headingBigrams(headingBody(s.lines[0], oldHeads, i))
		}
		for b, j := range new {
			newGrams[b] = headingBigrams(newHeads[j].key)
			newBodies[b] = headingBigrams(headingBody(s.lines[1], newHeads, j))
		}
		best := make([][]headingScore, rows+1)
		for a := range best {
			best[a] = make([]headingScore, cols+1)
		}
		for a := 1; a <= rows; a++ {
			for b := 1; b <= cols; b++ {
				choice := best[a-1][b]
				if choice.less(best[a][b-1]) {
					choice = best[a][b-1]
				}
				score := jaccard(oldGrams[a-1], newGrams[b-1]) + jaccard(oldBodies[a-1], newBodies[b-1])
				if score >= headingPairMin {
					if oldHeads[old[a-1]].level == newHeads[new[b-1]].level {
						score += headingSameLevel
					}
					prev := best[a-1][b-1]
					cand := headingScore{prev.count + 1, prev.total + score}
					if choice.less(cand) {
						choice = cand
					}
				}
				best[a][b] = choice
			}
		}
		a, b := rows, cols
		var picked [][2]int
		for a > 0 && b > 0 {
			switch best[a][b] {
			case best[a-1][b]:
				a--
			case best[a][b-1]:
				b--
			default:
				picked = append(picked, [2]int{old[a-1], new[b-1]})
				a, b = a-1, b-1
			}
		}
		slices.Reverse(picked)
		found = append(found, picked...)
	}
	return found
}

// headingShapeLike は本家の _heading_shape_like: 見出しの形の検査で見る元の見出しか (句点で終わる、読点を含む、15 字を超える)。
func headingShapeLike(key string) bool {
	return strings.HasSuffix(key, "。") || strings.HasSuffix(key, "．") || strings.Contains(key, "、") || strings.Contains(key, "，") || pystr.Len(key) > headingShapeChars
}

// headingRole は行の (種類, 段, 比べる文字)。種類は heading (見出しの最初の行)・bold (太字だけの本文の行)・body (ほかの本文の行)。段は heading だけ。
type headingRole struct {
	kind  string
	level int
	key   string
}

type headingRow struct {
	index int
	role  headingRole
}

type headingPair struct {
	i        int
	old      headingRole
	j        int
	new      headingRole
	oldLevel int
	newLevel int
}

// HeadingShape は JSON の heading_shapes の 1 件。
type HeadingShape struct {
	OldLine, NewLine int
	Old, New         string
}

// Value は本家の OrderedDict (old_line・new_line・old・new の順)。
func (h HeadingShape) Value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("old_line", int64(h.OldLine))
	o.Set("new_line", int64(h.NewLine))
	o.Set("old", h.Old)
	o.Set("new", h.New)
	return o
}

// HeadingStructure は元の文と書き直した文の、見出しの変化の見直し候補の文の並び (本家の _heading_structure)。withShapes なら、見出しの形の変化 (JSON の heading_shapes) も返す。
func HeadingStructure(orig, rw string, withShapes bool) ([]string, []HeadingShape, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, nil, err
	}
	if !withShapes {
		return headingStructure(s, full.hunks, nil), nil, nil
	}
	shapes := []HeadingShape{}
	out := headingStructure(s, full.hunks, &shapes)
	return out, shapes, nil
}

// headingStructure は本家の _heading_structure: 見出しの変化を、「構造」の見直し候補の文の並びにする。
// shapes に並びの置き場を渡すと、見出しの形の変化 (JSON の heading_shapes) を足す (nil なら足さない)。
func headingStructure(s *sides, hunks []*hunk, shapes *[]HeadingShape) []string {
	oldHeads, oldInside := s.headingRows(0)
	newHeads, newInside := s.headingRows(1)
	var out []string
	if len(oldHeads) != len(newHeads) {
		out = append(out, fmt.Sprintf("見出しの数が変わった（%d → %d）。節の分け方が元と同じでよいか見る", len(oldHeads), len(newHeads)))
	}
	heads := [2]map[int]headingEntry{oldHeads, newHeads}
	insides := [2]map[int]int{oldInside, newInside}

	role := func(side, index int) (headingRole, bool) {
		if e, ok := heads[side][index]; ok {
			return headingRole{"heading", e.level, e.key}, e.key != ""
		}
		rows := s.analyses[side].Lines
		if _, ok := insides[side][index]; ok || index >= len(rows) {
			return headingRole{}, false
		}
		row := rows[index]
		if row.Kind != "paragraph" || row.QuoteDepth != 0 {
			return headingRole{}, false
		}
		key := headingKey(row.Content)
		if key == "" {
			return headingRole{}, false
		}
		kind := "body"
		if _, ok := boldOnly(row.Content); ok {
			kind = "bold"
		}
		return headingRole{kind: kind, key: key}, true
	}

	// 変わった行 (hunk の中の行) だけを見る
	var changed [2][]int
	for _, h := range hunks {
		if h.tag == "replace" || h.tag == "delete" {
			for i := h.oldStart - 1; i < h.oldEnd; i++ {
				changed[0] = append(changed[0], i)
			}
		}
		if h.tag == "replace" || h.tag == "insert" {
			for j := h.newStart - 1; j < h.newEnd; j++ {
				changed[1] = append(changed[1], j)
			}
		}
	}
	// setext の下線の行だけが変わったときは、見出しの最初の行 (変わっていない行) と、相手の側でそれに当たる行も見る
	hunkSet := [2]map[int]bool{{}, {}}
	for side := range 2 {
		for _, i := range changed[side] {
			hunkSet[side][i] = true
		}
	}
	for side := range 2 {
		for _, index := range slices.Clone(changed[side]) {
			first, ok := insides[side][index]
			if !ok || hunkSet[side][first] {
				continue
			}
			other := headingCounterpart(hunks, side, first)
			for _, t := range [2][2]int{{side, first}, {1 - side, other}} {
				if !hunkSet[t[0]][t[1]] {
					hunkSet[t[0]][t[1]] = true
					changed[t[0]] = append(changed[t[0]], t[1])
				}
			}
		}
	}
	var rows [2][]headingRow
	for side := range 2 {
		set := make([]int, 0, len(hunkSet[side]))
		for i := range hunkSet[side] {
			set = append(set, i)
		}
		sort.Ints(set)
		changed[side] = set
		for _, index := range set {
			if r, ok := role(side, index); ok {
				rows[side] = append(rows[side], headingRow{index, r})
			}
		}
	}
	var pairs []headingPair
	pairedOld, pairedNew := map[int]bool{}, map[int]bool{}

	pairRows := func(oldRows, newRows []headingRow, headingOnly bool) []headingPair {
		both := [2][]headingRow{oldRows, newRows}
		if headingOnly {
			for k := range both {
				var only []headingRow
				for _, r := range both[k] {
					if r.role.kind == "heading" {
						only = append(only, r)
					}
				}
				both[k] = only
			}
		}
		var headingKeys, allKeys [2]map[string]bool
		for k := range both {
			headingKeys[k], allKeys[k] = map[string]bool{}, map[string]bool{}
			for _, r := range both[k] {
				allKeys[k][r.role.key] = true
				if r.role.kind == "heading" {
					headingKeys[k][r.role.key] = true
				}
			}
		}
		for side := range 2 {
			other := 1 - side
			var kept []headingRow
			for _, r := range both[side] {
				if (r.role.kind == "heading" && allKeys[other][r.role.key]) || (r.role.kind != "heading" && headingKeys[other][r.role.key]) {
					kept = append(kept, r)
				}
			}
			both[side] = kept
		}
		keysOf := func(rs []headingRow) []string {
			out := make([]string, len(rs))
			for i, r := range rs {
				out[i] = r.role.key
			}
			return out
		}
		var found []headingPair
		for _, m := range difflib.New(keysOf(both[0]), keysOf(both[1])).GetMatchingBlocks() {
			for k := range m.Size {
				o, n := both[0][m.A+k], both[1][m.B+k]
				if o.role.kind == "heading" || n.role.kind == "heading" {
					found = append(found, headingPair{i: o.index, old: o.role, j: n.index, new: n.role})
				}
			}
		}
		return found
	}
	take := func(found []headingPair) {
		for _, p := range found {
			pairs = append(pairs, p)
			pairedOld[p.i] = true
			pairedNew[p.j] = true
		}
	}
	betweenPairs := func(headingOnly bool) {
		bounds := make([][2]int, 0, len(pairs)+2)
		for _, p := range pairs {
			bounds = append(bounds, [2]int{p.i, p.j})
		}
		sort.Slice(bounds, func(a, b int) bool {
			return bounds[a][0] < bounds[b][0] || (bounds[a][0] == bounds[b][0] && bounds[a][1] < bounds[b][1])
		})
		bounds = append([][2]int{{-1, -1}}, bounds...)
		bounds = append(bounds, [2]int{len(s.lines[0]), len(s.lines[1])})
		var rest [2][]headingRow
		for _, r := range rows[0] {
			if !pairedOld[r.index] {
				rest[0] = append(rest[0], r)
			}
		}
		for _, r := range rows[1] {
			if !pairedNew[r.index] {
				rest[1] = append(rest[1], r)
			}
		}
		atOld, atNew := 0, 0
		var found []headingPair
		for k := 1; k < len(bounds); k++ {
			hiI, hiJ := bounds[k][0], bounds[k][1]
			var segOld, segNew []headingRow
			for atOld < len(rest[0]) && rest[0][atOld].index < hiI {
				segOld = append(segOld, rest[0][atOld])
				atOld++
			}
			for atNew < len(rest[1]) && rest[1][atNew].index < hiJ {
				segNew = append(segNew, rest[1][atNew])
				atNew++
			}
			if len(segOld) > 0 && len(segNew) > 0 {
				found = append(found, pairRows(segOld, segNew, headingOnly)...)
			}
		}
		take(found)
	}

	// 見出しどうしの組を先に作る。同じ hunk の中の見出しどうし、文書の中の見出しどうし、残った行 (本文の行や太字の行と見出し) の順に組にする
	var found []headingPair
	for _, h := range hunks {
		if h.tag != "replace" {
			continue
		}
		var hunkOld, hunkNew []headingRow
		for _, r := range rows[0] {
			if r.index >= h.oldStart-1 && r.index < h.oldEnd {
				hunkOld = append(hunkOld, r)
			}
		}
		for _, r := range rows[1] {
			if r.index >= h.newStart-1 && r.index < h.newEnd {
				hunkNew = append(hunkNew, r)
			}
		}
		if len(hunkOld) > 0 && len(hunkNew) > 0 {
			found = append(found, pairRows(hunkOld, hunkNew, true)...)
		}
	}
	take(found)
	betweenPairs(true)
	betweenPairs(false)

	// 同じ置き換えの hunk の中で組にならなかった見出しは、両側の数が同じなら順に組にする
	ordinal := [2]map[int]int{{}, {}}
	for side := range 2 {
		keys := sortedKeys(heads[side])
		for n, at := range keys {
			ordinal[side][at] = n
		}
	}
	localHeadingHunk := func(h *hunk) bool {
		for side, span := range [2][2]int{{h.oldStart - 1, h.oldEnd}, {h.newStart - 1, h.newEnd}} {
			lines := s.lines[side]
			for at := span[0]; at < span[1]; at++ {
				if _, in := insides[side][at]; !isBlank(lines[at]) && !in {
					return false
				}
			}
			after := span[1]
			for after < len(lines) && isBlank(lines[after]) {
				after++
			}
			if after < len(lines) && hunkSet[side][after] {
				return false
			}
		}
		return true
	}
	for _, h := range hunks {
		if h.tag != "replace" {
			continue
		}
		headingsOnly := localHeadingHunk(h)
		var restOld, restNew []headingRow
		for i := h.oldStart - 1; i < h.oldEnd; i++ {
			if r, ok := role(0, i); ok && !pairedOld[i] && r.kind == "heading" {
				restOld = append(restOld, headingRow{i, r})
			}
		}
		for j := h.newStart - 1; j < h.newEnd; j++ {
			if r, ok := role(1, j); ok && !pairedNew[j] && r.kind == "heading" {
				restNew = append(restNew, headingRow{j, r})
			}
		}
		if len(restOld) > 0 && len(restOld) == len(restNew) {
			for k := range restOld {
				o, n := restOld[k], restNew[k]
				if (headingsOnly && ordinal[0][o.index] == ordinal[1][n.index]) ||
					difflib.New(pyunicode.Runes(o.role.key), pyunicode.Runes(n.role.key)).Ratio() >= headingRatioMin {
					pairs = append(pairs, headingPair{i: o.index, old: o.role, j: n.index, new: n.role})
				}
			}
		}
	}

	// 見出しどうしの組に、組にならなかった見出しを順に組にし直した組を足し、形の変化と消えた見出しを見る
	// (段の変化と、見出しと本文の行の入れ替わりの候補は、ここまでの組だけで作る)
	var headingPairs [][2]int
	usedOld, usedNew := map[int]bool{}, map[int]bool{}
	for _, p := range pairs {
		if p.old.kind == "heading" && p.new.kind == "heading" {
			headingPairs = append(headingPairs, [2]int{p.i, p.j})
		}
		usedOld[p.i], usedNew[p.j] = true, true
	}
	headingPairs = append(headingPairs, s.headingOrderPairs(hunks, oldHeads, newHeads, usedOld, usedNew)...)
	if shapes != nil {
		sorted := slices.Clone(headingPairs)
		sort.SliceStable(sorted, func(a, b int) bool {
			return sorted[a][0] < sorted[b][0] || (sorted[a][0] == sorted[b][0] && sorted[a][1] < sorted[b][1])
		})
		for _, p := range sorted {
			if oldHeads[p[0]].key != newHeads[p[1]].key && headingShapeLike(oldHeads[p[0]].key) {
				*shapes = append(*shapes, HeadingShape{p[0] + 1, p[1] + 1, headingText(s.lines[0][p[0]], false), headingText(s.lines[1][p[1]], false)})
			}
		}
	}
	if len(out) > 0 {
		kept := map[int]bool{}
		for _, p := range pairs {
			kept[p.i] = true
		}
		for _, p := range headingPairs {
			kept[p[0]] = true
		}
		newKeys := map[string]bool{}
		for _, e := range newHeads {
			newKeys[e.key] = true
		}
		var removed []string
		for _, i := range sortedKeys(oldHeads) {
			if !kept[i] && oldHeads[i].key != "" && !newKeys[oldHeads[i].key] {
				removed = append(removed, headingText(s.lines[0][i], true))
			}
		}
		if len(removed) > 0 {
			shown := strings.Join(removed[:min(len(removed), headingRemovedShown)], "、")
			if len(removed) > headingRemovedShown {
				shown += fmt.Sprintf("ほか%d件", len(removed)-headingRemovedShown)
			}
			out[0] += "（消えた見出し: " + shown + "）"
		}
	}

	groupNames := []string{"bold", "body", "to_body", "level"}
	groups := map[string][]headingPair{}
	sorted := slices.Clone(pairs)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].i < sorted[b].i })
	for _, p := range sorted {
		switch {
		case p.old.kind == "heading" && p.new.kind == "heading":
			if p.old.level != p.new.level {
				p.oldLevel, p.newLevel = p.old.level, p.new.level
				groups["level"] = append(groups["level"], p)
			}
		case p.new.kind == "heading":
			groups[p.old.kind] = append(groups[p.old.kind], p)
		default:
			groups["to_body"] = append(groups["to_body"], p)
		}
	}
	labels := map[string]string{"bold": "太字の行を見出しにした", "body": "本文の行を見出しにした", "to_body": "見出しを本文の行にした", "level": "見出しのレベルを変えた"}
	for _, name := range groupNames {
		items := groups[name]
		if len(items) == 0 {
			continue
		}
		var shown []string
		for _, p := range items[:min(len(items), headingShown)] {
			oldText, newText := headingShownText(s.lines[0][p.i]), headingShownText(s.lines[1][p.j])
			if name == "level" {
				shown = append(shown, fmt.Sprintf("元%d行目 レベル%d「%s」→ 後%d行目 レベル%d「%s」", p.i+1, p.oldLevel, oldText, p.j+1, p.newLevel, newText))
				continue
			}
			oldDesc, newDesc := fmt.Sprintf("元%d行目「%s」", p.i+1, oldText), fmt.Sprintf("後%d行目「%s」", p.j+1, newText)
			if oldText == newText {
				// 文字の行は同じで、setext の下線だけを足したか消した。下線の行も挙げる
				side, at := 1, p.j
				if name == "to_body" {
					side, at = 0, p.i
				}
				last := at
				for {
					if first, ok := insides[side][last+1]; !ok || first != at {
						break
					}
					last++
				}
				if last != at {
					underline := fmt.Sprintf("と%d行目「%s」", last+1, headingShownText(s.lines[side][last]))
					if side == 0 {
						oldDesc += underline
					} else {
						newDesc += underline
					}
				}
			}
			shown = append(shown, oldDesc+"→ "+newDesc)
		}
		if len(items) > headingShown {
			shown = append(shown, fmt.Sprintf("ほか%dか所", len(items)-headingShown))
		}
		out = append(out, labels[name]+"（"+strings.Join(shown, "、")+"）")
	}
	return out
}
