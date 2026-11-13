package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 F の範囲づくり: 元の各文が、文字単位の対応 (opcodes) で書き直した文章のどの範囲に当たるかをたどる。
// 文の種類の印 (TypeChanges) と、許可・能力の言い方 (群 H) と、選ぶ範囲・因果 (群 I) が、同じ範囲で比べる。

// rangeItem は元の文の並び (texts・spans) と、書き直した文章の範囲 [a, b)。
// 本家は範囲を作ってから広げ・狭めるので、a と b が動くのは groups だけ (丸ごと移した文 moved は動かない)。
type rangeItem struct {
	texts []string
	spans [][2]int
	a, b  int
}

const (
	stopChars    = "。！？!?\n"
	trimmedChars = "。！？!?、 \n"
)

func isStop(c rune) bool { return strings.ContainsRune(stopChars, c) }

// notInStops は本家の「s not in '。！？!?'」(部分文字列の判定): 空の文字列は含まれると数えるので偽。
func notInStops(s string) bool { return s != "" && !strings.Contains("。！？!?", s) }

// pySlice は Python のスライス rs[a:b] (範囲の外を詰める。a と b は 0 以上)。
func pySlice(rs []rune, a, b int) []rune {
	a, b = min(a, len(rs)), min(b, len(rs))
	if a >= b {
		return nil
	}
	return rs[a:b]
}

func bodyChars(rs []rune) int {
	n := 0
	for _, c := range rs {
		if !notBody[c] {
			n++
		}
	}
	return n
}

// sentenceSpans は正規化した文章を文に分け、(開始, 終了) を返す。コードブロックと表の行は飛ばす。
func sentenceSpans(t string) [][2]int {
	var spans [][2]int
	offset, inCode := 0, false
	for _, line := range strings.Split(t, "\n") {
		head := pystr.Strip(line)
		if strings.HasPrefix(head, "```") {
			inCode = !inCode
		} else if !inCode && head != "" && !strings.HasPrefix(head, "|") {
			for _, m := range mustFinditer(sentencePiece, pyre.NewText(line), 0, pyre.NoEnd) {
				if whole, _ := m.Group(0); pystr.Strip(whole) != "" {
					spans = append(spans, [2]int{offset + m.Start(0), offset + m.End(0)})
				}
			}
		}
		offset += pystr.Len(line) + 1
	}
	return spans
}

// slideToSentenceEdges は、文字単位の対応で足した文や消した文が文の境目からずれたものを、句点や改行の直後から始まる位置へずらす。
// 句点 1 文字ぶん前にずれた「。足した文」や、次の文と同じ書き出しの分だけ後ろにずれた「消した文の後半。次の文の書き出し」の形。
// 前後の一致と同じ文字の分だけずらせるときは、ずらす量の少ない方へずらす。
func slideToSentenceEdges(opcodes []difflib.OpCode, o, r []rune) []difflib.OpCode {
	ops := slices.Clone(opcodes)
	for k := 1; k < len(ops)-1; k++ {
		tag, prev, nxt := ops[k].Tag, &ops[k-1], &ops[k+1]
		if (tag != "insert" && tag != "delete") || prev.Tag != "equal" || nxt.Tag != "equal" {
			continue
		}
		var text []rune
		var a, b, left, right int
		if tag == "insert" {
			text, a, b, left, right = r, ops[k].J1, ops[k].J2, prev.J2-prev.J1, nxt.J2-nxt.J1
		} else {
			text, a, b, left, right = o, ops[k].I1, ops[k].I2, prev.I2-prev.I1, nxt.I2-nxt.I1
		}
		if isStop(text[a-1]) {
			continue
		}
		var shifts []int
		step := 0
		for step < min(b-a, right-1) && text[a+step] == text[b+step] {
			step++
			if isStop(text[a+step-1]) {
				shifts = append(shifts, step)
				break
			}
		}
		step = 0
		for step < min(b-a, left-1) && text[a-1-step] == text[b-1-step] {
			step++
			if isStop(text[a-step-1]) {
				shifts = append(shifts, -step)
				break
			}
		}
		if len(shifts) == 0 {
			continue
		}
		shift := shifts[0]
		for _, s := range shifts[1:] {
			if abs(s) < abs(shift) {
				shift = s
			}
		}
		prev.I2 += shift
		prev.J2 += shift
		ops[k].I1 += shift
		ops[k].I2 += shift
		ops[k].J1 += shift
		ops[k].J2 += shift
		nxt.I1 += shift
		nxt.J1 += shift
	}
	return ops
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// bigrams は 2 文字の組の集合。
func bigrams(rs []rune) map[string]struct{} {
	out := map[string]struct{}{}
	for i := 0; i+1 < len(rs); i++ {
		out[string(rs[i:i+2])] = struct{}{}
	}
	return out
}

func dice(x, y map[string]struct{}) float64 {
	shared := 0
	for k := range x {
		if _, ok := y[k]; ok {
			shared++
		}
	}
	total := len(x) + len(y)
	if total == 0 {
		total = 1
	}
	return float64(2*shared) / float64(total)
}

// rangeSentences は書き直した文章の範囲 [a, b) を、前後の文の切れ目 (句点・改行) まで広げた文。
func rangeSentences(r []rune, a, b int) string {
	left := -1
	for _, c := range stopChars {
		for i := min(a, len(r)) - 1; i >= 0; i-- {
			if r[i] == c {
				left = max(left, i)
				break
			}
		}
	}
	left++
	right := len(r)
	if b != 0 && strings.ContainsRune("。！？!?", r[b-1]) {
		right = b
	} else {
		for _, c := range stopChars {
			for i := b; i < len(r); i++ {
				if r[i] == c {
					right = min(right, i)
					break
				}
			}
		}
	}
	if right < len(r) && strings.ContainsRune("。！？!?", r[right]) {
		right++
	}
	return pystr.Strip(pyunicode.FromRunes(pySlice(r, left, right)))
}

// typeRanges は、元の各文が書き直した文章のどの範囲に当たるか。範囲が重なる元の文は 1 つにまとめ、丸ごと消えた文は入れない。
func typeRanges(o, r []rune, opcodes []difflib.OpCode) []*rangeItem {
	opcodes = slideToSentenceEdges(opcodes, o, r)
	starts := make([]int, len(opcodes))
	for i, op := range opcodes {
		starts[i] = op.I1
	}
	// 句点も改行も含まない挿入。文頭や、句点のない文の末尾に足した語句として、その文に含める。
	bare := func(op difflib.OpCode) bool {
		if op.Tag != "insert" {
			return false
		}
		for _, c := range pySlice(r, op.J1, op.J2) {
			if isStop(c) {
				return false
			}
		}
		return true
	}
	mapStart := func(i int) int {
		k := max(0, sort.SearchInts(starts, i+1)-1)
		op := opcodes[k]
		if op.I1 == i && k > 0 && opcodes[k-1].I1 == i && bare(opcodes[k-1]) {
			return opcodes[k-1].J1
		}
		if op.Tag == "equal" {
			return op.J1 + min(i-op.I1, op.J2-op.J1)
		}
		if op.Tag == "insert" {
			return op.J2
		}
		return op.J1
	}
	mapEnd := func(e int) int {
		k := sort.SearchInts(starts, e)
		if k < len(opcodes) && opcodes[k].I1 == e && !isStop(o[e-1]) && bare(opcodes[k]) {
			return opcodes[k].J2
		}
		op := opcodes[max(0, k-1)]
		if op.Tag == "equal" {
			return op.J1 + min(e-op.I1, op.J2-op.J1)
		}
		return op.J2
	}
	// 元の [i, j) のうち、書き直した文章と対応する文字の数。句読点・かっこ・空白は数えない。
	// 置き換えの部分は、置き換え先が短いほど少なく数える (消した文を含む長い部分が、短い語句に置き換わった場合など)。
	aligned := func(i, j int) float64 {
		k, count := max(0, sort.SearchInts(starts, i+1)-1), 0.0
		for k < len(opcodes) && opcodes[k].I1 < j {
			op := opcodes[k]
			if op.Tag == "equal" || op.Tag == "replace" {
				chars := float64(bodyChars(pySlice(o, max(i, op.I1), min(j, op.I2))))
				if op.Tag == "equal" {
					count += chars
				} else {
					count += chars * min(1.0, float64(op.J2-op.J1)/float64(op.I2-op.I1))
				}
			}
			k++
		}
		return count
	}
	rString := pyunicode.FromRunes(r)
	rSpans := sentenceSpans(rString)
	rStarts := make([]int, len(rSpans))
	for i, s := range rSpans {
		rStarts[i] = s[0]
	}
	// 元の [i, j) が対応する範囲が、それが入っている書き直した文の中身の 6 割以上を占めるか。
	ownsSentence := func(i, j int) bool {
		a, b := mapStart(i), mapEnd(j)
		k := sort.SearchInts(rStarts, a+1) - 1
		if b <= a || k < 0 || rSpans[k][1] <= a {
			return false
		}
		ra, rb := rSpans[k][0], rSpans[k][1]
		whole := bodyChars(pySlice(r, ra, rb))
		part := bodyChars(pySlice(r, max(a, ra), min(b, rb)))
		return whole > 0 && part*5 >= whole*3
	}
	// 丸ごと移した文は、書き直した文章の中で 1 文として見つかるものに限る (見出しなどの短い文が、別の文の一部に当たるのを除く)。
	whole := map[string][2]int{}
	for _, s := range rSpans {
		key := pystr.Strip(pyunicode.FromRunes(pySlice(r, s[0], s[1])))
		if _, ok := whole[key]; !ok {
			whole[key] = s
		}
	}
	var groups, moved []*rangeItem
	var gone []goneSentence
	var found [][2]int
	for _, s := range sentenceSpans(pyunicode.FromRunes(o)) {
		text := pystr.Strip(pyunicode.FromRunes(pySlice(o, s[0], s[1])))
		body := bodyChars(pyunicode.Runes(text))
		// 対応する文字が少なすぎる文は、残った文とみなさない。消した文の文末の数文字が、隣の文に足した印の中の文字と一致しただけの場合などで、
		// その文を比べると、隣の文の印の変化をこの文の変化として出してしまう。
		// ただし、対応する範囲が書き直した文の大半を占めるときは、短く書き直した文として残った文とみなす。
		if aligned(s[0], s[1]) < float64(min(body, max(3, (body+3)/4))) && !ownsSentence(s[0], s[1]) {
			if w, ok := whole[text]; ok {
				delete(whole, text)
				found = append(found, w)
				moved = append(moved, &rangeItem{[]string{text}, [][2]int{s}, w[0], w[1]})
			} else {
				gone = append(gone, goneSentence{text, s[0], s[1]})
			}
			continue
		}
		a, b := mapStart(s[0]), mapEnd(s[1])
		if b <= a || pystr.StripChars(pyunicode.FromRunes(pySlice(r, a, b)), trimmedChars) == "" {
			continue
		}
		if len(groups) > 0 && a < groups[len(groups)-1].b {
			last := groups[len(groups)-1]
			last.texts = append(last.texts, text)
			last.spans = append(last.spans, s)
			last.b = max(last.b, b)
		} else {
			groups = append(groups, &rangeItem{[]string{text}, [][2]int{s}, a, b})
		}
	}
	// 丸ごと移した文の位置に重なる範囲は、文字の対応がその文に引きずられたもの。移した文の部分を除き、残りの長い方で比べる。
	for _, f := range found {
		for _, g := range groups {
			if g.a < f[1] && f[0] < g.b {
				left := pystr.StripChars(pyunicode.FromRunes(pySlice(r, g.a, max(g.a, f[0]))), trimmedChars)
				right := pystr.StripChars(pyunicode.FromRunes(pySlice(r, min(g.b, f[1]), g.b)), trimmedChars)
				if pystr.Len(left) >= pystr.Len(right) {
					g.b = max(g.a, f[0])
				} else {
					g.a = min(g.b, f[1])
				}
			}
		}
	}
	kept := groups[:0:0]
	for _, g := range groups {
		if pystr.StripChars(pyunicode.FromRunes(pySlice(r, g.a, g.b)), trimmedChars) != "" {
			kept = append(kept, g)
		}
	}
	groups = kept
	if len(gone) > 0 {
		groups, moved = pairGoneSentences(o, r, groups, moved, gone, rSpans)
	}
	widenToSentences(groups, moved, rSpans)
	return append(groups, moved...)
}

// goneSentence は比べずに飛ばした元の文 (書き直した文章にない文) と、元の文章の中の範囲。
type goneSentence struct {
	text string
	s, e int
}

// overlapping は各範囲 (a, b) について、items のうち x.a < b かつ a < x.b のもの (items の順のまま) の一覧。
// items を始まりの順に並べ、前からの終わりの最大を持っておき、b より前に始まるものを後ろから、そこまでの終わりの最大が a 以下になる所まで見る。
func overlapping(items []*rangeItem, spans [][2]int) [][]*rangeItem {
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return items[order[x]].a < items[order[y]].a })
	starts := make([]int, len(order))
	reach := make([]int, len(order))
	for i, k := range order {
		starts[i] = items[k].a
		reach[i] = items[k].b
		if i > 0 {
			reach[i] = max(reach[i], reach[i-1])
		}
	}
	out := make([][]*rangeItem, len(spans))
	for n, span := range spans {
		a, b := span[0], span[1]
		var hits []int
		for q := sort.SearchInts(starts, b) - 1; q >= 0 && reach[q] > a; q-- {
			if k := order[q]; items[k].b > a {
				hits = append(hits, k)
			}
		}
		sort.Ints(hits)
		for _, k := range hits {
			out[n] = append(out[n], items[k])
		}
	}
	return out
}

// pairGoneSentences は、比べずに飛ばした元の文 (同じ文が書き直した文章にないもの) を、字面が重なる書き直した文と組にする。
// 組にできるのは、次をどちらも満たすときだけ (消しただけの文を、残したほかの文と組にしないため)。
//   - 短い方の文の 2 文字の組の半分以上 (かつ 4 組以上。短い方の組が 6 つ以下なら 3 組以上) が共通している。
//   - 共通する組のうち同じ数以上が、その書き直した文に範囲が掛かっている元の文 (そのまま残った文、丸ごと移した文) では説明できない。
//
// 組は字面の近い順 (Dice 係数) に、どちらの文も 1 回だけ使って決める。
// 書き直した文が 1 つの元の文の範囲だけでできていて、その元の文の字面が遠い (Dice 係数 0.6 未満で、消えた文より遠い) ときは、その範囲は比べない。
// 書き直した文が 2 文以上にまたがる範囲の端にあるときは、その範囲から外す。
func pairGoneSentences(o, r []rune, groups, moved []*rangeItem, gone []goneSentence, rSpans [][2]int) ([]*rangeItem, []*rangeItem) {
	rGrams := make([]map[string]struct{}, len(rSpans))
	for i, s := range rSpans {
		rGrams[i] = bigrams(pyunicode.Runes(pystr.Strip(pyunicode.FromRunes(pySlice(r, s[0], s[1])))))
	}
	owners := overlapping(groups, rSpans)
	movedOver := overlapping(moved, rSpans)
	groupGrams := map[*rangeItem]map[string]struct{}{}
	gramsOf := func(g *rangeItem) map[string]struct{} {
		grams, ok := groupGrams[g]
		if !ok {
			grams = bigrams(pyunicode.Runes(strings.Join(g.texts, "")))
			groupGrams[g] = grams
		}
		return grams
	}
	ownerGrams := make([]map[string]struct{}, len(rSpans))
	for i := range rSpans {
		ownerGrams[i] = map[string]struct{}{}
		for _, g := range append(slices.Clone(owners[i]), movedOver[i]...) {
			for k := range gramsOf(g) {
				ownerGrams[i][k] = struct{}{}
			}
		}
	}
	spansWith := map[string][]int{}
	for idx, grams := range rGrams {
		for gram := range grams {
			spansWith[gram] = append(spansWith[gram], idx)
		}
	}
	type candidate struct {
		score  float64
		gi, ix int
	}
	type passedPair struct {
		score float64
		idx   int
	}
	var candidates []candidate
	// 組にできる書き直した文と字面の近さは、消えた文の 2 文字の組の集合だけで決まる。同じ段落を繰り返す文書では同じ文が何度も消えるので、
	// 集合ごとに 1 回だけ数えて使い回す (候補は下で (近さ, gi, idx) の順に並べ直すので、作る順が変わっても結果は同じ)。
	passedByGrams := map[string][]passedPair{}
	for gi, g := range gone {
		grams := bigrams(pyunicode.Runes(g.text))
		key := gramKey(grams)
		passed, ok := passedByGrams[key]
		if !ok {
			// sharedCounts[idx] は len(grams & rGrams[idx])。共通の組が 1 つもない文は、下の least (3 以上) に届かないので数えなくてよい。
			sharedCounts := map[int]int{}
			for gram := range grams {
				for _, idx := range spansWith[gram] {
					sharedCounts[idx]++
				}
			}
			idxs := make([]int, 0, len(sharedCounts))
			for idx := range sharedCounts {
				idxs = append(idxs, idx)
			}
			sort.Ints(idxs)
			for _, idx := range idxs {
				shared := sharedCounts[idx]
				smaller := min(len(grams), len(rGrams[idx]))
				least := 4
				if smaller <= 6 {
					least = 3
				}
				if shared < least || 2*shared < smaller {
					continue
				}
				outside := 0
				for k := range grams {
					if _, in := rGrams[idx][k]; in {
						if _, owned := ownerGrams[idx][k]; !owned {
							outside++
						}
					}
				}
				if outside < least {
					continue
				}
				passed = append(passed, passedPair{dice(grams, rGrams[idx]), idx})
			}
			passedByGrams[key] = passed
		}
		for _, p := range passed {
			candidates = append(candidates, candidate{p.score, gi, p.idx})
		}
	}
	sort.Slice(candidates, func(x, y int) bool {
		a, b := candidates[x], candidates[y]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.gi != b.gi {
			return a.gi < b.gi
		}
		return a.ix < b.ix
	})
	usedGone := map[int]bool{}
	usedR := map[int]*rangeItem{}
	drop := map[*rangeItem]bool{}
	for _, c := range candidates {
		if usedGone[c.gi] {
			continue
		}
		g := gone[c.gi]
		if entry, ok := usedR[c.ix]; ok {
			// 改行で折り返した文の前半と後半が同じ文の相手になるときは、まとめて比べる。前半は句点で終わっていない。
			first, last := entry.spans[0], entry.spans[len(entry.spans)-1]
			after := pystr.Strip(pyunicode.FromRunes(pySlice(o, last[1], g.s))) == "" &&
				notInStops(lastRune(pystr.RStrip(pyunicode.FromRunes(pySlice(o, last[0], last[1])))))
			before := pystr.Strip(pyunicode.FromRunes(pySlice(o, g.e, first[0]))) == "" && notInStops(lastRune(g.text))
			if !after && !before {
				continue
			}
			usedGone[c.gi] = true
			entry.spans = append(entry.spans, [2]int{g.s, g.e})
			sort.Slice(entry.spans, func(x, y int) bool {
				if entry.spans[x][0] != entry.spans[y][0] {
					return entry.spans[x][0] < entry.spans[y][0]
				}
				return entry.spans[x][1] < entry.spans[y][1]
			})
			entry.texts = entry.texts[:0]
			for _, s := range entry.spans {
				entry.texts = append(entry.texts, pystr.Strip(pyunicode.FromRunes(pySlice(o, s[0], s[1]))))
			}
			continue
		}
		usedGone[c.gi] = true
		a, b := rSpans[c.ix][0], rSpans[c.ix][1]
		item := &rangeItem{[]string{g.text}, [][2]int{{g.s, g.e}}, a, b}
		moved = append(moved, item)
		usedR[c.ix] = item
		for _, og := range owners[c.ix] {
			switch {
			case a <= og.a && og.b <= b+1:
				if dice(bigrams(pyunicode.Runes(strings.Join(og.texts, ""))), rGrams[c.ix]) < min(c.score, 0.6) {
					drop[og] = true
				}
			case og.a < a && a < og.b && og.b <= b+1:
				og.b = a
			case a <= og.a && og.a < b && b < og.b:
				og.a = b
			}
		}
	}
	var kept []*rangeItem
	for _, g := range groups {
		if !drop[g] {
			kept = append(kept, g)
		}
	}
	return kept, moved
}

// lastRune は本家の s[-1:] (空なら空の文字列)。
func lastRune(s string) string {
	rs := pyunicode.Runes(s)
	if len(rs) == 0 {
		return ""
	}
	return pyunicode.FromRunes(rs[len(rs)-1:])
}

// gramKey は 2 文字の組の集合を 1 つの文字列にする (並べてつなぐ。どれも 2 文字なので区切りは要らない)。
func gramKey(grams map[string]struct{}) string {
	keys := make([]string, 0, len(grams))
	for k := range grams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "")
}

// widenToSentences は範囲を、それが入っている書き直した文の端まで広げる。ほかの元の文の範囲に掛かるところには広げない。
// 消した文の文末の数文字に引きずられて、隣の文に足した印が範囲の外に出るのを防ぐ。
//
// ほかの範囲 (ca, cb) で止める所は、左が「a 以前に終わる範囲の終わり cb の最大」、右が「b 以後に始まる範囲の始まり ca の最小」。
// 範囲ごとにすべての範囲を見ると範囲の数の 2 乗 (数十万字の文書で十数秒) なので、終わりと始まりを並べておき、二分探索で引く。
// a < b で、どの範囲も ca <= cb なら、ca >= b の範囲は cb > a なので、2 つの条件は同時に成り立たない。(a, b) 自身は
// どちらの条件にも当たらないので、飛ばさなくても同じになる。それ以外の形 (a >= b の範囲、ca > cb の範囲があるとき) は、すべての範囲を順に見る。
func widenToSentences(groups, moved []*rangeItem, rSpans [][2]int) {
	if len(groups) == 0 {
		return
	}
	rStarts, rEnds := make([]int, len(rSpans)), make([]int, len(rSpans))
	for i, s := range rSpans {
		rStarts[i], rEnds[i] = s[0], s[1]
	}
	var claimed [][2]int
	for _, g := range groups {
		claimed = append(claimed, [2]int{g.a, g.b})
	}
	for _, m := range moved {
		claimed = append(claimed, [2]int{m.a, m.b})
	}
	sort.Slice(claimed, func(x, y int) bool {
		if claimed[x][0] != claimed[y][0] {
			return claimed[x][0] < claimed[y][0]
		}
		return claimed[x][1] < claimed[y][1]
	})
	ordered := true
	claimedStarts, claimedEnds := make([]int, len(claimed)), make([]int, len(claimed))
	for i, c := range claimed {
		ordered = ordered && c[0] <= c[1]
		claimedStarts[i], claimedEnds[i] = c[0], c[1]
	}
	sort.Ints(claimedEnds)
	for _, g := range groups {
		a, b := g.a, g.b
		i := sort.SearchInts(rStarts, a+1) - 1
		j := sort.SearchInts(rEnds, b)
		left, right := a, b
		if i >= 0 && i < len(rSpans) && rEnds[i] > a {
			left = rStarts[i]
		}
		if j < len(rSpans) && rStarts[j] < b {
			right = rEnds[j]
		}
		if ordered && a < b {
			if k := sort.SearchInts(claimedEnds, a+1); k > 0 {
				left = max(left, claimedEnds[k-1])
			}
			if k := sort.SearchInts(claimedStarts, b); k < len(claimedStarts) {
				right = min(right, claimedStarts[k])
			}
		} else {
			for _, c := range claimed {
				if c[0] == a && c[1] == b {
					continue
				}
				if c[1] <= a {
					left = max(left, c[1])
				} else if c[0] >= b {
					right = min(right, c[0])
				}
			}
		}
		g.a, g.b = min(a, left), max(b, right)
	}
}

// TypeChange は文の種類の印が消えた・増えた範囲 1 つ。
type TypeChange struct {
	Orig, Rewrite string
	Lost, Added   []KindHits
}

// typeShared は TypeChanges が作った範囲と印の位置 (許可・能力の比べ (群 H) と、選ぶ範囲・因果の比べ (群 I) が、同じ文章なら範囲を作り直さずに使う。
// 範囲づくりは数十万字の文書で重いため)。haveRanges が偽のときは、印がどちらにも無くて範囲を作らなかった。
type typeShared struct {
	o, r           string
	opcodes        []difflib.OpCode
	ranges         []*rangeItem
	haveRanges     bool
	oMarks, rMarks []typeMark
}

// typeCache は直前の TypeChanges が作った範囲を 1 件だけ持つ。本家は module の変数 (_LAST_TYPE_RANGES) に置いたが、Go は呼び出しに渡す。
// 文章が違えば使わずに作り直す。1 回の比較が終わったら捨てる (比べ終えた文章の全文と範囲を持ち続けないため)。
type typeCache struct{ last *typeShared }

// TypeChanges は本家の _type_changes: 元の各文が、文字単位の対応で書き直した文章のどの範囲に当たるかをたどり、範囲ごとに印を比べる。
// 文を分けても、つないでも、範囲の中で比べるので、移っただけの印は変化として出ない。範囲が重なる元の文はまとめて比べる。
// 丸ごと消えた文は比べない。丸ごと移した文は同じ文を探し、移したうえで書き換えた文は、字面の近い文を書き直した文章から探して比べる。
// o と r は正規化した文章、opcodes はその文字単位の対応。作った範囲は cache に置く (nil なら置かない)。
func TypeChanges(o, r string, opcodes []difflib.OpCode, cache *typeCache) []TypeChange {
	if len(opcodes) == 0 {
		return nil
	}
	oMarks, rMarks := markPositions(o), markPositions(r)
	shared := &typeShared{o: o, r: r, opcodes: opcodes, oMarks: oMarks, rMarks: rMarks}
	if cache != nil {
		cache.last = shared
	}
	if len(oMarks) == 0 && len(rMarks) == 0 {
		return nil
	}
	shared.ranges, shared.haveRanges = typeRanges(pyunicode.Runes(o), pyunicode.Runes(r), opcodes), true
	oKeys, rKeys := markStarts(oMarks), markStarts(rMarks)
	rRunes := pyunicode.Runes(r)
	var out []TypeChange
	for _, item := range shared.ranges {
		before := marksIn(oMarks, item.spans, typeKindNames, oKeys)
		after := marksIn(rMarks, [][2]int{{item.a, item.b}}, typeKindNames, rKeys)
		var lost, added []KindHits
		for _, e := range before {
			if !after.has(e.Kind) {
				lost = append(lost, e)
			}
		}
		for _, e := range after {
			if !before.has(e.Kind) {
				added = append(added, e)
			}
		}
		if len(lost) > 0 || len(added) > 0 {
			out = append(out, TypeChange{strings.Join(item.texts, ""), rangeSentences(rRunes, item.a, item.b), lost, added})
		}
	}
	return out
}
