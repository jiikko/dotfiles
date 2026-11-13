package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 群 J: 全文差分。本家の full_diff・_build_full_diff・_align・_fill_gap・_pair_similar・_char_segments・_plan_segments・_make_hunk・_mark_moved・_encoding_notes。
// 生の文を行で対応させ、変わった塊の中を文字で対応させる (normalize は通さない)。
// 位置と長さは全部コードポイント (本家の str の添字)。どの手順も入力だけで決まり、時間で打ち切らない。

const (
	charAlignLimit  = 2000   // 文の断片をそのまま文字で対応させる、片側の字数の上限
	blockAlignLimit = 500    // 塊をそのまま文字で対応させる、片側の字数の上限。超える塊は文の断片に分けてから対応させる
	charBudget      = 200000 // 文書全体で文字の対応にかける字数 (対応させる文字列の長さの、両側の合計)
	pairWindow      = 4      // 文や断片を組にするとき、直前の組の次から見る数
	pairRatio       = 0.45   // 組にする似かたの下限 (difflib の ratio)
	smallGap        = 400    // 残った間を difflib で対応させる大きさの上限 (両側の要素の数の積)
	gapWindow       = 8      // 大きな間で、同じ中身の要素を探す先の長さ
	alignDepth      = 64     // 1 回ずつだけ出る要素の組を手がかりにたどる深さの上限
)

// 全文差分の hunk に付ける limits_applied の名前 (文書全体で 1 回ずつ、この順)。
var limitNames = []string{"long_fragment", "too_large", "budget"}

var (
	fragmentPattern  = pyre.NewLazy(`(?:[^。！？!?．.\n]|\.(?![ \t\n]|$))*(?:[。！？!?．]+|\.+(?=[ \t\n]|$))?[」』）)]*[ \t]*\n?`, 0)
	spacesPattern    = pyre.NewLazy("[\\s\ufeff]+", 0)
	atxHeadingStruct = pyre.NewLazy(`^( {0,3})(#{1,6})((?:[ \t].*)?)$`, 0)
)

// pair は対応した行か断片の組 (i と j はどちらも増える)。same は中身が同じか。
type pair struct {
	i, j int
	same bool
}

func isBlank(line string) bool { return pystr.Strip(line) == "" }

// splitLines は行に分ける。文字列が改行で終わるときは、最後の空の要素を除く (末尾の改行の有無は final_newline に記録する)。
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// sides は全文差分で使う、元と後のそれぞれの行と Markdown の解析。
type sides struct {
	lines    [2][]string
	analyses [2]*visibility.Analysis
}

// newSides は本家の _Sides。解析は行の分け方を "\n" だけにそろえてかける ("\r" は同じ長さの空白に置き換える。行番号と位置が全文差分の行と 1 対 1 に対応する)。
func newSides(orig, rw string) (*sides, error) {
	s := &sides{lines: [2][]string{splitLines(orig), splitLines(rw)}}
	for side, text := range [2]string{orig, rw} {
		if strings.Contains(text, "\r") {
			text = strings.ReplaceAll(text, "\r", " ")
		}
		a, err := visibility.AnalyzeMarkdown(text, true)
		if err != nil {
			return nil, err
		}
		s.analyses[side] = a
	}
	return s, nil
}

// align は 2 つの並び (行か文の断片) を対応させる。difflib を並び全体にかけると、空行やコードの囲みのように同じ要素が何度も出る文書で時間が 2 乗から 3 乗で伸びるので、
// 前後の同じ要素を外し、両方に 1 回ずつだけ出る要素 (patience diff) を手がかりに間を分けてから、小さい間だけ difflib にかける。
func align(old, new []string, similar bool) []pair {
	var pairs []pair
	alignRange(old, new, 0, len(old), 0, len(new), similar, &pairs, 0)
	return pairs
}

func alignRange(old, new []string, i1, i2, j1, j2 int, similar bool, out *[]pair, depth int) {
	for i1 < i2 && j1 < j2 && old[i1] == new[j1] {
		*out = append(*out, pair{i1, j1, true})
		i1, j1 = i1+1, j1+1
	}
	var tail []pair
	for i1 < i2 && j1 < j2 && old[i2-1] == new[j2-1] {
		i2, j2 = i2-1, j2-1
		tail = append(tail, pair{i2, j2, true})
	}
	if i1 < i2 && j1 < j2 {
		var anchors [][2]int
		if depth < alignDepth {
			anchors = uniqueAnchors(old, new, i1, i2, j1, j2)
		}
		if len(anchors) > 0 {
			previousI, previousJ := i1, j1
			for _, a := range anchors {
				alignRange(old, new, previousI, a[0], previousJ, a[1], similar, out, depth+1)
				*out = append(*out, pair{a[0], a[1], true})
				previousI, previousJ = a[0]+1, a[1]+1
			}
			alignRange(old, new, previousI, i2, previousJ, j2, similar, out, depth+1)
		} else {
			*out = append(*out, fillGap(old, new, i1, i2, j1, j2, similar)...)
		}
	}
	for k := len(tail) - 1; k >= 0; k-- {
		*out = append(*out, tail[k])
	}
}

// uniqueAnchors は old[i1:i2] と new[j1:j2] の両方に 1 回ずつだけ出る、空白だけでない要素の組のうち、順番を保つ最長の並び (同じ長さなら先に見つけたもの)。
func uniqueAnchors(old, new []string, i1, i2, j1, j2 int) [][2]int {
	countOld, countNew := map[string]int{}, map[string]int{}
	for _, s := range old[i1:i2] {
		countOld[s]++
	}
	for _, s := range new[j1:j2] {
		countNew[s]++
	}
	where := map[string]int{}
	for j := j1; j < j2; j++ {
		if item := new[j]; countNew[item] == 1 && countOld[item] == 1 && !isBlank(item) {
			where[item] = j
		}
	}
	var candidates [][2]int
	for i := i1; i < i2; i++ {
		if j, ok := where[old[i]]; ok {
			candidates = append(candidates, [2]int{i, j})
		}
	}
	var tails, tailValues []int
	previous := make([]int, len(candidates))
	for index, c := range candidates {
		previous[index] = -1
		position := sort.SearchInts(tailValues, c[1])
		if position > 0 {
			previous[index] = tails[position-1]
		}
		if position == len(tails) {
			tails = append(tails, index)
			tailValues = append(tailValues, c[1])
		} else {
			tails[position], tailValues[position] = index, c[1]
		}
	}
	var chain [][2]int
	index := -1
	if len(tails) > 0 {
		index = tails[len(tails)-1]
	}
	for index >= 0 {
		chain = append(chain, candidates[index])
		index = previous[index]
	}
	for l, r := 0, len(chain)-1; l < r; l, r = l+1, r-1 {
		chain[l], chain[r] = chain[r], chain[l]
	}
	return chain
}

// fillGap は 1 回ずつだけ出る要素で組にできなかった間 (old[i1:i2] と new[j1:j2]) で、組を作る。
func fillGap(old, new []string, i1, i2, j1, j2 int, similar bool) []pair {
	if i1 >= i2 || j1 >= j2 {
		return nil
	}
	var found []pair
	if (i2-i1)*(j2-j1) <= smallGap {
		m := difflib.New(old[i1:i2], new[j1:j2])
		for _, b := range m.GetMatchingBlocks() {
			for k := range b.Size {
				found = append(found, pair{i1 + b.A + k, j1 + b.B + k, true})
			}
		}
	} else {
		positions := map[string][]int{}
		for j := j1; j < j2; j++ {
			positions[new[j]] = append(positions[new[j]], j)
		}
		// 後の側の各位置までに、元の側にもある中身の要素がいくつあるか (飛ばしてよいかの判断に使う)
		inOld := map[string]bool{}
		for _, s := range old[i1:i2] {
			inOld[s] = true
		}
		shared := make([]int, 1, j2-j1+1)
		for j := j1; j < j2; j++ {
			n := shared[len(shared)-1]
			if inOld[new[j]] {
				n++
			}
			shared = append(shared, n)
		}
		k := j1
		for i := i1; i < i2; i++ {
			places := positions[old[i]]
			if len(places) == 0 {
				continue
			}
			index := sort.SearchInts(places, k)
			if index == len(places) {
				continue
			}
			place := places[index]
			// 近く (8 つ先まで) にあれば組にする。遠くても、そこまでに飛ばす要素のうち元の側にもある中身が 8 つ未満 (飛ばすのはほぼ足した塊) なら組にする。
			// 8 つ以上あるときは遠くの同じ中身と組にせず、ずれを防ぐ
			if place-k < gapWindow || shared[place-j1]-shared[k-j1] < gapWindow {
				found = append(found, pair{i, place, true})
				k = place + 1
			}
		}
	}
	if !similar {
		return found
	}
	// 同じ中身の組を区切りにして、その間の要素を似ているもので組にする
	var pairs []pair
	previousI, previousJ := i1, j1
	for n := 0; n <= len(found); n++ {
		i, j := i2, j2
		if n < len(found) {
			i, j = found[n].i, found[n].j
		}
		for _, p := range pairSimilar(old, new, previousI, i, previousJ, j) {
			pairs = append(pairs, pair{p[0], p[1], old[p[0]] == new[p[1]]})
		}
		if n < len(found) {
			pairs = append(pairs, found[n])
		}
		previousI, previousJ = i+1, j+1
	}
	return pairs
}

// commonAffix は 2 つの文字列の、前の同じ字の数と後ろの同じ字の数 (重ならない)。
func commonAffix(a, b []rune) (head, tail int) {
	limit := min(len(a), len(b))
	for head < limit && a[head] == b[head] {
		head++
	}
	for tail < limit-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	return head, tail
}

// affixRatio は前後の同じ字の数から求めた似かた (2×(前の同じ字＋後ろの同じ字)÷両側の字数)。difflib の ratio() より小さいか同じ。
func affixRatio(a, b []rune) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	head, tail := commonAffix(a, b)
	return 2.0 * float64(head+tail) / float64(len(a)+len(b))
}

// pairSimilar は文や断片の組み方。old[i1:i2] の各要素について、直前に組にした new の要素の次から 4 つの範囲で、quick_ratio が 0.45 以上のものを候補にし、
// ratio が最大で 0.45 以上なら組にする (順番を保つ)。どちらかが 2,000 字を超える組には difflib の ratio() をかけず、前後の同じ字の数から求めた似かたを使う
// (ratio() は長さのおよそ 2 乗の時間がかかり、「。」のない 6 万字の 1 行で十数秒になる)。
// 本家は seq2 に old の要素、seq1 に new の要素を渡す。ratio は向きで変わりうるので、向きも写す。
func pairSimilar(old, new []string, i1, i2, j1, j2 int) [][2]int {
	var pairs [][2]int
	start := j1
	m := difflib.New[rune](nil, nil)
	newRunes := make([][]rune, max(j2-j1, 0))
	runesOfNew := func(j int) []rune {
		if newRunes[j-j1] == nil {
			newRunes[j-j1] = pyunicode.Runes(new[j])
		}
		return newRunes[j-j1]
	}
	for i := i1; i < i2; i++ {
		if start >= j2 {
			break
		}
		oldRunes := pyunicode.Runes(old[i])
		m.SetSeq2(oldRunes)
		best, bestRatio := -1, 0.0
		for j := start; j < min(start+pairWindow, j2); j++ {
			newJ := runesOfNew(j)
			m.SetSeq1(newJ)
			// real_quick_ratio と quick_ratio は ratio (と affixRatio) の上限。下限に届かないか、それまでの最大を超えられない候補は ratio を求めずに飛ばす
			upper := m.RealQuickRatio()
			if upper < pairRatio || upper <= bestRatio {
				continue
			}
			upper = m.QuickRatio()
			if upper < pairRatio || upper <= bestRatio {
				continue
			}
			var ratio float64
			if len(oldRunes) > charAlignLimit || len(newJ) > charAlignLimit {
				ratio = affixRatio(oldRunes, newJ)
			} else {
				ratio = m.Ratio()
			}
			if ratio >= pairRatio && ratio > bestRatio {
				best, bestRatio = j, ratio
			}
		}
		if best >= 0 {
			pairs = append(pairs, [2]int{i, best})
			start = best + 1
		}
	}
	return pairs
}

// block は equal 以外の塊 (本家の _opcodes の 1 つ)。
type block struct {
	tag            string
	i1, i2, j1, j2 int
}

// opcodes は組の並びから equal 以外の塊を作る。
func opcodes(pairs []pair, n, m int) []block {
	var blocks []block
	i, j := 0, 0
	for k := 0; k <= len(pairs); k++ {
		x, y := n, m
		if k < len(pairs) {
			x, y = pairs[k].i, pairs[k].j
		}
		if x > i || y > j {
			tag := "insert"
			switch {
			case x > i && y > j:
				tag = "replace"
			case x > i:
				tag = "delete"
			}
			blocks = append(blocks, block{tag, i, x, j, y})
		}
		i, j = x+1, y+1
	}
	return blocks
}

// segment は文字の対応の 1 つ (tag は "=" "-" "+")。
type segment struct{ tag, text string }

// mergeSegments は続く同じ印の文字列を 1 つにし、空の文字列を除く。
func mergeSegments(segments []segment) []segment {
	var merged []segment
	var parts []string
	flush := func() {
		if len(merged) > 0 {
			merged[len(merged)-1].text = strings.Join(parts, "")
		}
	}
	for _, s := range segments {
		if s.text == "" {
			continue
		}
		if len(merged) > 0 && merged[len(merged)-1].tag == s.tag {
			parts = append(parts, s.text)
			continue
		}
		flush()
		merged = append(merged, segment{tag: s.tag})
		parts = []string{s.text}
	}
	flush()
	return merged
}

// charSegments は 2 つの文字列を文字で対応させる。前と後ろの同じ文字を先に外してから、残りを本家の速い照合 (difflib.NewExact) で対応させる。
func charSegments(a, b string) []segment {
	ar, br := pyunicode.Runes(a), pyunicode.Runes(b)
	head, tail := commonAffix(ar, br)
	middleA, middleB := ar[head:len(ar)-tail], br[head:len(br)-tail]
	text := pyunicode.FromRunes
	segments := []segment{{"=", text(ar[:head])}}
	if len(middleA) > 0 && len(middleB) > 0 {
		for _, op := range difflib.NewExact(middleA, middleB).GetOpcodes() {
			if op.Tag == "equal" {
				segments = append(segments, segment{"=", text(middleA[op.I1:op.I2])})
			} else {
				segments = append(segments, segment{"-", text(middleA[op.I1:op.I2])}, segment{"+", text(middleB[op.J1:op.J2])})
			}
		}
	} else {
		segments = append(segments, segment{"-", text(middleA)}, segment{"+", text(middleB)})
	}
	segments = append(segments, segment{"=", text(ar[len(ar)-tail:])})
	return mergeSegments(segments)
}

// fragments は文の断片に分ける。断片は「句点までの文と、続く閉じかっこと改行」で、全部つなぐと元の文字列に戻る。
// 「．」と、空白か行末が続く「.」(英語の文) でも切る。3.14 のような数の中の「.」では切らない。
func fragments(text string) []string {
	t := pyre.NewText(text)
	var pieces []string
	for position, size := 0, t.Len(); position < size; {
		m, err := fragmentPattern.Get().Match(t, position, pyre.NoEnd)
		if err != nil {
			panic(err)
		}
		end := m.End(0)
		if end == position {
			end = position + 1
		}
		pieces = append(pieces, t.Slice(position, end))
		position = end
	}
	return pieces
}

// segmentPlan は塊の文字の対応の計画。cost は文字の対応にかける文字列の長さの、両側の合計。
type segmentPlan struct {
	cost         int
	build        func() []segment
	longFragment bool
}

func planSegments(a, b string) segmentPlan {
	if pystr.Len(a) <= blockAlignLimit && pystr.Len(b) <= blockAlignLimit {
		return segmentPlan{cost: pystr.Len(a) + pystr.Len(b), build: func() []segment { return charSegments(a, b) }}
	}
	old, new := fragments(a), fragments(b)
	pairs := align(old, new, true)
	plan := segmentPlan{}
	for _, p := range pairs {
		if p.same {
			continue
		}
		if pystr.Len(old[p.i]) > charAlignLimit || pystr.Len(new[p.j]) > charAlignLimit {
			plan.longFragment = true
		} else {
			plan.cost += pystr.Len(old[p.i]) + pystr.Len(new[p.j])
		}
	}
	plan.build = func() []segment {
		var segments []segment
		i, j := 0, 0
		for k := 0; k <= len(pairs); k++ {
			x, y := len(old), len(new)
			if k < len(pairs) {
				x, y = pairs[k].i, pairs[k].j
			}
			segments = append(segments, segment{"-", strings.Join(old[i:x], "")}, segment{"+", strings.Join(new[j:y], "")})
			if k == len(pairs) {
				break
			}
			switch {
			case pairs[k].same:
				segments = append(segments, segment{"=", old[x]})
			case pystr.Len(old[x]) > charAlignLimit || pystr.Len(new[y]) > charAlignLimit:
				// 長い断片の組は文字で対応させないが、前と後ろの同じ字は外す (時間は字数に比例するだけ)。
				// 変えていないインラインのコードやリンク先を「変わった」と出さず、「差:」も出せる
				o, n := pyunicode.Runes(old[x]), pyunicode.Runes(new[y])
				head, tail := commonAffix(o, n)
				segments = append(segments, segment{"=", pyunicode.FromRunes(o[:head])}, segment{"-", pyunicode.FromRunes(o[head : len(o)-tail])},
					segment{"+", pyunicode.FromRunes(n[head : len(n)-tail])}, segment{"=", pyunicode.FromRunes(o[len(o)-tail:])})
			default:
				segments = append(segments, charSegments(old[x], new[y])...)
			}
			i, j = x+1, y+1
		}
		return mergeSegments(segments)
	}
	return plan
}

// kinds は行 first から last の手前まで (0 から数える) の行の種類を、重ねずに出てきた順に集める。
func kinds(a *visibility.Analysis, first, last int) []string {
	var found []string
	for index := first; index < last; index++ {
		kind := "paragraph"
		if index < len(a.Lines) {
			kind = a.Lines[index].Kind
			// 引用の中の行は、箇条書きの続きとして読まれても引用として出す
			if (kind == "paragraph" || kind == "list") && a.Lines[index].QuoteDepth != 0 {
				kind = "quote"
			}
		}
		if !slices.Contains(found, kind) {
			found = append(found, kind)
		}
	}
	return found
}

// EncodingNote は全文差分の注記 (改行コード・BOM) の 1 つ。Old と New は eol なら文字列、bom なら bool。
type EncodingNote struct {
	Kind     string
	Old, New pyjson.Value
}

// Value は JSON の注記 ({"kind", "old", "new"})。
func (n EncodingNote) Value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("kind", n.Kind)
	o.Set("old", n.Old)
	o.Set("new", n.New)
	return o
}

// encodingNotes は decode_source の情報 2 つから、全文差分の注記を作る。どちらかに改行がないときは改行コードを比べない。
func encodingNotes(oldInfo, newInfo visibility.SourceInfo) []EncodingNote {
	var notes []EncodingNote
	if oldInfo.EOL != newInfo.EOL && oldInfo.EOL != "none" && newInfo.EOL != "none" {
		notes = append(notes, EncodingNote{"eol", oldInfo.EOL, newInfo.EOL})
	}
	if oldInfo.BOM != newInfo.BOM {
		notes = append(notes, EncodingNote{"bom", oldInfo.BOM, newInfo.BOM})
	}
	return notes
}

// hunkNote は hunk の注記。Kind ごとに持つ欄が違う (whitespace_only は Kind だけ、heading_level は Old と New、moved_to と moved_from は Hunk と Line)。
type hunkNote struct {
	kind       string
	old, new   int
	hunk, line int
}

func (n hunkNote) value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("kind", n.kind)
	switch n.kind {
	case "heading_level":
		o.Set("old", int64(n.old))
		o.Set("new", int64(n.new))
	case "moved_to", "moved_from":
		o.Set("hunk", int64(n.hunk))
		o.Set("line", int64(n.line))
	}
	return o
}

// hunk は全文差分の変わった塊 1 つ。
type hunk struct {
	index                              int
	tag                                string
	oldStart, oldEnd, newStart, newEnd int
	oldBlocks, newBlocks               []string
	whitespaceOnly                     bool
	notes                              []hunkNote
	movedTo, movedFrom                 *int
	oldLines, newLines                 []string
	segments                           []segment
	hasSegments                        bool
	segmentsOmitted                    string
}

func optionalInt(p *int) pyjson.Value {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func (h *hunk) value() pyjson.Value {
	notes := make([]pyjson.Value, len(h.notes))
	for i, n := range h.notes {
		notes[i] = n.value()
	}
	var segments pyjson.Value
	if h.hasSegments {
		list := make([]pyjson.Value, len(h.segments))
		for i, s := range h.segments {
			list[i] = []pyjson.Value{s.tag, s.text}
		}
		segments = list
	}
	var omitted pyjson.Value
	if h.segmentsOmitted != "" {
		omitted = h.segmentsOmitted
	}
	o := pyjson.NewObject()
	o.Set("index", int64(h.index))
	o.Set("tag", h.tag)
	o.Set("old_start", int64(h.oldStart))
	o.Set("old_end", int64(h.oldEnd))
	o.Set("new_start", int64(h.newStart))
	o.Set("new_end", int64(h.newEnd))
	o.Set("old_blocks", pyjson.Strings(h.oldBlocks))
	o.Set("new_blocks", pyjson.Strings(h.newBlocks))
	o.Set("whitespace_only", h.whitespaceOnly)
	o.Set("notes", notes)
	o.Set("moved_to", optionalInt(h.movedTo))
	o.Set("moved_from", optionalInt(h.movedFrom))
	o.Set("old_lines", pyjson.Strings(h.oldLines))
	o.Set("new_lines", pyjson.Strings(h.newLines))
	o.Set("segments", segments)
	o.Set("segments_omitted", omitted)
	return o
}

// fullDiff は JSON の full_diff。
type fullDiff struct {
	encodingNotes []EncodingNote
	finalNewline  [2]bool
	limitsApplied []string
	hunks         []*hunk
}

func (f *fullDiff) value() pyjson.Value {
	notes := make([]pyjson.Value, len(f.encodingNotes))
	for i, n := range f.encodingNotes {
		notes[i] = n.Value()
	}
	hunks := make([]pyjson.Value, len(f.hunks))
	for i, h := range f.hunks {
		hunks[i] = h.value()
	}
	newline := pyjson.NewObject()
	newline.Set("old", f.finalNewline[0])
	newline.Set("new", f.finalNewline[1])
	o := pyjson.NewObject()
	o.Set("encoding_notes", notes)
	o.Set("final_newline", newline)
	o.Set("limits_applied", pyjson.Strings(f.limitsApplied))
	o.Set("hunks", hunks)
	return o
}

// FullDiff は元の文と書き直した文の全文差分 (JSON の full_diff)。渡された文字列をそのまま比べる (BOM を外したり、改行をそろえたりしない)。
func FullDiff(orig, rw string, notes []EncodingNote) (pyjson.Value, error) {
	full, _, err := buildFullDiff(orig, rw, notes)
	if err != nil {
		return nil, err
	}
	return full.value(), nil
}

func buildFullDiff(orig, rw string, notes []EncodingNote) (*fullDiff, *sides, error) {
	s, err := newSides(orig, rw)
	if err != nil {
		return nil, nil, err
	}
	oldLines, newLines := s.lines[0], s.lines[1]
	limits := map[string]bool{}
	var hunks []*hunk
	if !slices.Equal(oldLines, newLines) {
		for _, b := range opcodes(align(oldLines, newLines, false), len(oldLines), len(newLines)) {
			hunks = append(hunks, makeHunk(len(hunks)+1, b, s))
		}
	}
	markMoved(hunks)
	budget := charBudget
	for _, h := range hunks {
		if h.tag != "replace" {
			continue
		}
		plan := planSegments(strings.Join(h.oldLines, "\n"), strings.Join(h.newLines, "\n"))
		switch {
		case plan.cost > charBudget:
			h.segmentsOmitted = "too_large"
		case plan.cost > budget:
			h.segmentsOmitted = "budget"
		default:
			budget -= plan.cost
			h.segments, h.hasSegments = plan.build(), true
			if plan.longFragment {
				limits["long_fragment"] = true
			}
		}
		if h.segmentsOmitted != "" {
			limits[h.segmentsOmitted] = true
		}
	}
	full := &fullDiff{
		encodingNotes: notes,
		finalNewline:  [2]bool{strings.HasSuffix(orig, "\n"), strings.HasSuffix(rw, "\n")},
		hunks:         hunks,
	}
	for _, name := range limitNames {
		if limits[name] {
			full.limitsApplied = append(full.limitsApplied, name)
		}
	}
	return full, s, nil
}

func makeHunk(index int, b block, s *sides) *hunk {
	oldPart, newPart := s.lines[0][b.i1:b.i2], s.lines[1][b.j1:b.j2]
	h := &hunk{
		index: index, tag: b.tag, oldStart: b.i1 + 1, oldEnd: b.i2, newStart: b.j1 + 1, newEnd: b.j2,
		oldBlocks: kinds(s.analyses[0], b.i1, b.i2), newBlocks: kinds(s.analyses[1], b.j1, b.j2),
		oldLines: oldPart, newLines: newPart,
	}
	h.whitespaceOnly = spacesPattern.MustSub("", strings.Join(oldPart, "\n")) == spacesPattern.MustSub("", strings.Join(newPart, "\n"))
	if h.whitespaceOnly {
		h.notes = append(h.notes, hunkNote{kind: "whitespace_only"})
	}
	if len(oldPart) == 1 && len(newPart) == 1 && slices.Equal(h.oldBlocks, []string{"heading"}) && slices.Equal(h.newBlocks, []string{"heading"}) {
		om, nm := atxMatch(oldPart[0]), atxMatch(newPart[0])
		if om != nil && nm != nil && om.indent == nm.indent && om.rest == nm.rest && om.level != nm.level {
			h.notes = append(h.notes, hunkNote{kind: "heading_level", old: om.level, new: nm.level})
		}
	}
	return h
}

type atxParts struct {
	indent, rest string
	level        int
}

func atxMatch(line string) *atxParts {
	m, err := atxHeadingStruct.Get().Match(pyre.NewText(line), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	if m == nil {
		return nil
	}
	indent, _ := m.Group(1)
	hashes, _ := m.Group(2)
	rest, _ := m.Group(3)
	return &atxParts{indent: indent, rest: rest, level: pystr.Len(hashes)}
}

// markMoved は、消した空白だけでない行と中身が完全に同じ行が、ほかの hunk で足されていれば、2 つの hunk を対にする (動かした行)。1 つの hunk の相手は 1 つだけ (最初に見つかったもの)。
func markMoved(hunks []*hunk) {
	type added struct {
		h    *hunk
		line int
	}
	byLine := map[string][]added{}
	for _, h := range hunks {
		for offset, line := range h.newLines {
			if !isBlank(line) {
				byLine[line] = append(byLine[line], added{h, h.newStart + offset})
			}
		}
	}
	if len(byLine) == 0 {
		return
	}
	for _, h := range hunks {
		for offset, line := range h.oldLines {
			if h.movedTo != nil || isBlank(line) {
				continue
			}
			for _, a := range byLine[line] {
				if a.h == h || a.h.movedFrom != nil {
					continue
				}
				to, from := a.h.index, h.index
				h.movedTo = &to
				h.notes = append(h.notes, hunkNote{kind: "moved_to", hunk: a.h.index, line: a.line})
				a.h.movedFrom = &from
				a.h.notes = append(a.h.notes, hunkNote{kind: "moved_from", hunk: h.index, line: h.oldStart + offset})
				break
			}
		}
	}
}
