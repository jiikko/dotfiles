package bold

import (
	"slices"

	"ja-writing/visibility"
)

// opaqueItem は本家の context["opaque"] の要素 (kind, text[left:right])。書き換えの案が、コードやリンクなどの保護範囲を変えていないかを比べる。
type opaqueItem struct {
	kind string
	text string
}

// scanContext は _bold_scan_context が返し、_bold_fix_with_context が書き足す dict。段落 (ブロック) ごとに 1 つ。
type scanContext struct {
	runs   [][2]int
	gfm    []emph
	common []emph

	opaque      []opaqueItem // nil は本家の None (まだ作っていない)
	opaqueSpans []visibility.Span
	haveOpaque  bool

	runIndex map[[2]int]int // _bold_run_index (作るまで nil)

	repair     *repairIndex // _bold_repair_index の結果 (nil は本家の None)
	haveRepair bool         // "repair_index" in context (None を入れても真)

	slowTries        int
	remaining        int
	delimiterMatches map[int][]emph // nil はまだ作っていない
}

// scan は _bold_scan_context。2 つの規則で組んだ太字の和と、どちらでも組めない隣り合う組を、並べて返す。
// gfmPairs・commonPairs は、それぞれの規則で組めた太字。
func scan(block []rune, runs [][2]int) ([]pair, map[pair]bool, map[pair]bool, *scanContext) {
	gfmList, usedGFM, emphGFM := delimiterParse(block, gfm, runs)
	commonList, usedCommon, emphCommon := delimiterParse(block, common, runs)
	pairs := map[pair]bool{}
	gfmPairs, commonPairs := map[pair]bool{}, map[pair]bool{}
	for _, p := range gfmList {
		pairs[p], gfmPairs[p] = true, true
	}
	for _, p := range commonList {
		pairs[p], commonPairs[p] = true, true
	}
	used := func(pos int) bool { return usedGFM[pos] || usedCommon[pos] }
	usedIn := func(start, end int) bool {
		for pos := start; pos < end; pos++ {
			if used(pos) {
				return true
			}
		}
		return false
	}
	// 組めない印の組は、後ろの本当の組を 1 つずらすことがある。ずれの二次的な症状ではなく、直接まずい隣の組だけを候補にする (本家 2318-2319 行)。
	nextDouble := make([]int, len(runs))
	later := -1
	for index := len(runs) - 1; index >= 0; index-- {
		nextDouble[index] = later
		if runs[index][1]-runs[index][0] >= 2 {
			later = index
		}
	}
	for index, run := range runs {
		start, end := run[0], run[1]
		if end-start < 2 || usedIn(start, end) {
			continue
		}
		later := nextDouble[index]
		if later < 0 {
			continue
		}
		laterStart, laterEnd := runs[later][0], runs[later][1]
		if end < laterStart && isWS(block[end]) && usedIn(laterStart, laterEnd) {
			continue
		}
		if !localPairOK(block, start, laterStart) {
			pairs[pair{start, laterStart}] = true
		}
	}
	sorted := make([]pair, 0, len(pairs))
	for p := range pairs {
		sorted = append(sorted, p)
	}
	slices.SortFunc(sorted, func(a, b pair) int {
		if a.i != b.i {
			return a.i - b.i
		}
		return a.j - b.j
	})
	return sorted, gfmPairs, commonPairs, &scanContext{runs: runs, gfm: emphGFM, common: emphCommon}
}

// runIndexOf は _bold_run_index (印の連なりから番号を引く表。1 回だけ作る)。
func (c *scanContext) runIndexOf() map[[2]int]int {
	if c.runIndex == nil {
		c.runIndex = make(map[[2]int]int, len(c.runs))
		for number, run := range c.runs {
			c.runIndex[run] = number
		}
	}
	return c.runIndex
}

func opaqueOf(text []rune, spans []visibility.Span) []opaqueItem {
	out := make([]opaqueItem, len(spans))
	for k, s := range spans {
		out[k] = opaqueItem{s.Kind, str(text[s.Start:s.End])}
	}
	return out
}
