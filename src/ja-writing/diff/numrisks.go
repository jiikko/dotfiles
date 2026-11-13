package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 M: 数値の恐れ (本家の _number_risks と、その下の _number_contexts・_order_changed・_cancel_notation・_cluster_partners・_moved_keys・_present など)。
// 組になった文で数値の増減・並びを比べ (replaced・added・lost・order)、そのあと文書全体で後にだけある値と元にだけある値のうち、組の文で出していないものを added と lost にする。

// counter は Python の Counter (値は 1 以上だけを残す演算の結果を使う)。
type counter map[numKey]int

func counterOf(numbers []number) counter {
	c := counter{}
	for _, n := range numbers {
		c[n.key()]++
	}
	return c
}

func (c counter) clone() counter {
	out := make(counter, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

// minus は Counter の引き算 (結果は 1 以上の数だけ)。
func (c counter) minus(o counter) counter {
	out := counter{}
	for k, v := range c {
		if v-o[k] > 0 {
			out[k] = v - o[k]
		}
	}
	return out
}

// positive は +Counter (0 以下を外した新しい Counter)。
func (c counter) positive() counter {
	out := counter{}
	for k, v := range c {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}

func (c counter) equal(o counter) bool { return countsEqual(c, o) }

// countsEqual は 2 つの多重集合 (値は数) が同じか。
func countsEqual[K comparable](a, b map[K]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// bisectLeft・bisectRight は Python の bisect (昇順の並びで、x の入る位置)。
func bisectLeft(a []int, x int) int {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := (lo + hi) / 2
		if a[mid] < x {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func bisectRight(a []int, x int) int {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := (lo + hi) / 2
		if x < a[mid] {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// sideNumbers は 1 つの側の数値 (位置の順) と、その始まりの位置の並び。
type sideNumbers struct {
	found  []number
	starts []int
}

// numbersOf は数値の取り出し方で取った数値 (本家の _Sides.numbers)。
func (s *sides) numbersOf(side int) *sideNumbers {
	if s.numbers[side] == nil {
		found := findNumbers(s.texts[side], s.analyses[side], numberMaskedKinds)
		starts := make([]int, len(found))
		for i, n := range found {
			starts[i] = n.start
		}
		s.numbers[side] = &sideNumbers{found, starts}
	}
	return s.numbers[side]
}

// allKeysOf は伏せる前の数値 (コード、URL、先頭の設定、HTML の中も含む) の集合 (本家の _Sides.all_keys)。「文書のどこにもない」の判定に使う。
func (s *sides) allKeysOf(side int) *keySet {
	if s.allKeys[side] == nil {
		s.allKeys[side] = newKeySet(findNumbers(s.texts[side], s.analyses[side], map[string]bool{}))
	}
	return s.allKeys[side]
}

func (s *sides) docText(side int) pyre.Text {
	if !s.docReady[side] {
		s.docTexts[side], s.docReady[side] = runesText(s.texts[side]), true
	}
	return s.docTexts[side]
}

func (s *sides) lineOf(side, position int) int { return max(1, bisectRight(s.starts[side], position)) }

// sentenceNumbers は文の中の数値 (位置の順)。
func (s *sides) sentenceNumbers(side int, sn sentence) []number {
	sn2 := s.numbersOf(side)
	end := sn.start + pystr.Len(sn.text)
	return sn2.found[bisectLeft(sn2.starts, sn.start):bisectLeft(sn2.starts, end)]
}

// keySet は文書の数値の (数, 単位) の集合、数ごとの単位の集合 (単位なしは "")、範囲・日付・時刻のまとまり (数ごとに、その数を含むまとまりの並びの集まり)。
type keySet struct {
	keys     map[numKey]bool
	units    map[string]map[string]bool
	clusters map[string][][]numKey
}

func newKeySet(numbers []number) *keySet {
	k := &keySet{map[numKey]bool{}, map[string]map[string]bool{}, map[string][][]numKey{}}
	seen := map[string]map[string]bool{}
	for _, n := range numbers {
		k.keys[n.key()] = true
		if k.units[n.value] == nil {
			k.units[n.value] = map[string]bool{}
		}
		k.units[n.value][n.unit] = true
		if n.cluster != nil {
			id := clusterID(n.cluster)
			if seen[n.value] == nil {
				seen[n.value] = map[string]bool{}
			}
			if !seen[n.value][id] {
				seen[n.value][id] = true
				k.clusters[n.value] = append(k.clusters[n.value], n.cluster)
			}
		}
	}
	return k
}

func clusterID(c []numKey) string {
	var sb strings.Builder
	for _, k := range c {
		sb.WriteString(k.value)
		sb.WriteByte(0)
		sb.WriteString(k.unit)
		sb.WriteByte(1)
	}
	return sb.String()
}

// notationIndex は単位なしの数のうち、書き方の手がかり (notation) のあるものを (数, "") ごとにまとめる。
func notationIndex(numbers []number) map[numKey][]number {
	out := map[numKey][]number{}
	for _, n := range numbers {
		if n.notation != nil {
			out[n.key()] = append(out[n.key()], n)
		}
	}
	return out
}

// notationMatch は単位なしの数 n と、相手の側の同じ数の単位つきの値を、表記の違いとみなせるか。unit を渡せば、その単位の値だけを見る。
// みなすのは 2 つの形だけ。(1) 数のあとの空白に、相手の側の単位が続く (3 日 → 3日、3 s → 3s)。(2) 数が範囲・日付・時刻のまとまりの部品で、単位がまとまりの形に合う。
// まとまりの中の単位なしの数がこの数だけ (3〜5日 の 3、10時30 の 30) なら、相手の側の単位が、まとまりのほかの数の単位か、直前の数の単位に続く単位 (10時30 なら分) のときだけ。
// 2 つ以上なら、まとまりの中の単位なしの数がすべて、相手の側の 1 つのまとまりに同じ順にそろって出てきて、この数と同じ位置の単位が相手の単位で、その単位の並びがつなぎの字に合うときだけ。
func notationMatch(n number, other *keySet, unit *string) bool {
	if n.unit != "" || n.notation == nil {
		return false
	}
	units := other.units[n.value]
	var candidates []string
	if unit != nil {
		if *unit != "" && units[*unit] {
			candidates = []string{*unit}
		}
	} else {
		for u := range units {
			if u != "" {
				candidates = append(candidates, u)
			}
		}
	}
	if len(candidates) == 0 {
		return false
	}
	nt := n.notation
	if nt.follow != "" {
		for _, c := range candidates {
			if strings.HasPrefix(nt.follow, c) {
				return true
			}
		}
	}
	if len(nt.group) == 0 {
		return false
	}
	frame := nt.frame
	if len(nt.group) == 1 {
		allowed := map[string]bool{}
		for _, own := range frame.units {
			if own != "" {
				allowed[own] = true
			}
		}
		if frame.at > 0 {
			if next, ok := nextUnit[frame.units[frame.at-1]]; ok {
				allowed[next] = true
			}
		}
		for _, c := range candidates {
			if allowed[c] {
				return true
			}
		}
		return false
	}
	mine := 0
	for position, own := range frame.units {
		if own == "" {
			if position == frame.at {
				break
			}
			mine++
		}
	}
	size := len(nt.group)
	for _, cluster := range other.clusters[n.value] {
		for start := 0; start+size <= len(cluster); start++ {
			picked := cluster[start : start+size]
			same := true
			for i, k := range picked {
				if k.value != nt.group[i] {
					same = false
				}
			}
			if !same || !slices.Contains(candidates, picked[mine].unit) {
				continue
			}
			units := make([]string, size)
			for i, k := range picked {
				units[i] = k.unit
			}
			if unitsFit(frame.joins, units) {
				return true
			}
		}
	}
	return false
}

// unitsFit は範囲・日付・時刻のまとまり (数と数の間のつなぎの字の種類の並び joins) の単位なしの数に、相手の側のまとまりの単位の並びが合うか。
// 範囲だけなら 1 種類の単位。日付と時刻は、単位の並びが前から後ろへ進む (年→月→日、時→分→秒)。範囲と日付・時刻が混ざるときは、範囲のつなぎの字の数だけ並びが頭に戻ってよい。
func unitsFit(joins, units []string) bool {
	kinds := map[string]bool{}
	ranges := 0
	for _, j := range joins {
		if j != "adjacent" {
			kinds[j] = true
		}
		if j == "range" {
			ranges++
		}
	}
	var named []string
	for _, u := range units {
		if u != "" {
			named = append(named, u)
		}
	}
	if len(kinds) == 1 && kinds["range"] {
		distinct := map[string]bool{}
		for _, u := range named {
			distinct[u] = true
		}
		return len(distinct) == 1
	}
	if len(named) == 0 || len(named) != len(units) {
		return false
	}
	delete(kinds, "range")
	var order []string
	switch {
	case len(kinds) > 0 && onlyKinds(kinds, "colon"):
		order = timeUnits
	case len(kinds) > 0 && onlyKinds(kinds, "slash", "dash"):
		order = dateUnits
	default:
		order = append(append([]string(nil), dateUnits...), timeUnits...)
	}
	positions := make([]int, len(units))
	for i, u := range units {
		at := slices.Index(order, u)
		if at < 0 {
			return false
		}
		positions[i] = at
	}
	resets := 0
	for i := 0; i+1 < len(positions); i++ {
		if positions[i] >= positions[i+1] {
			resets++
		}
	}
	return resets <= ranges
}

func onlyKinds(set map[string]bool, allowed ...string) bool {
	for k := range set {
		if !slices.Contains(allowed, k) {
			return false
		}
	}
	return true
}

// present は (数, 単位) が相手の文書の値にあるか。同じ数と単位があれば「ある」。単位なしの値は、この側の同じ値が範囲・日付・時刻の部品か、空白のあとに単位が続く形で、
// 相手の側に同じ数の単位つきの値があれば「ある」(2026-10-13 → 2026年10月13日)。単位つきの値は、同じ数と単位があるときだけ「ある」。
func present(key numKey, notation map[numKey][]number, other *keySet) bool {
	if other.keys[key] {
		return true
	}
	if key.unit != "" {
		return false
	}
	for _, n := range notation[key] {
		if notationMatch(n, other, nil) {
			return true
		}
	}
	return false
}

// cancelNotation は組の文の数値を比べるとき、同じ数の「単位なし」と「単位あり」を表記の違いとして打ち消す。打ち消すのは、単位なしの側が範囲・日付・時刻のまとまりの部品で、
// まとまりの中の単位なしの数がすべて相手の側の 1 つのまとまりにそろって出てくるときと、単位なしの数のあとの空白に、相手の側の単位が続くときだけ。単位どうしが違う組 (3日 → 3週間) は残す。
// 何も打ち消せないときは、渡された Counter をそのまま返す (本家と同じ。呼び出し元の drop 側が書き換えるので、別のオブジェクトにすると raw_added - added の値が変わる)。
func cancelNotation(removed, added counter, oldNumbers, newNumbers []number) (counter, counter) {
	if len(removed) == 0 || len(added) == 0 {
		return removed, added
	}
	removed, added = removed.clone(), added.clone()
	type pass struct {
		mine, other counter
		numbers     []number
		others      []number
	}
	for _, p := range []pass{{removed, added, oldNumbers, newNumbers}, {added, removed, newNumbers, oldNumbers}} {
		var otherSet *keySet
		for _, n := range p.numbers {
			if n.unit != "" || n.notation == nil || p.mine[n.key()] <= 0 {
				continue
			}
			if otherSet == nil {
				otherSet = newKeySet(p.others)
			}
			var units []string
			for k, count := range p.other {
				if k.value == n.value && k.unit != "" && count > 0 {
					units = append(units, k.unit)
				}
			}
			sort.Strings(units)
			for _, u := range units {
				if notationMatch(n, otherSet, &u) {
					p.mine[n.key()]--
					p.other[numKey{n.value, u}]--
					break
				}
			}
		}
	}
	return removed.positive(), added.positive()
}

// followItem は組の文のすぐあとに続く、組にならなかった文と、その数値の並び。
type followItem struct {
	sentence sentence
	numbers  []number
}

// followingUnpaired は組の文 sentences[index] のすぐあとに続く、組にならなかった文と、その数値の並び (次の組の文の手前まで)。
func (s *sides) followingUnpaired(side int, sentences []sentence, index int, paired map[int]bool) []followItem {
	var found []followItem
	for position := index + 1; position < len(sentences) && !paired[position]; position++ {
		found = append(found, followItem{sentences[position], s.sentenceNumbers(side, sentences[position])})
	}
	return found
}

// unpairedAround は組の文 sentences[index] のすぐ前とすぐあとに続く、組にならなかった文の数値 (前と次の組の文の手前まで)。
func (s *sides) unpairedAround(side int, sentences []sentence, index int, paired map[int]bool) []number {
	var found []number
	for position := index - 1; position >= 0 && !paired[position]; position-- {
		found = append(found, s.sentenceNumbers(side, sentences[position])...)
	}
	for position := index + 1; position < len(sentences) && !paired[position]; position++ {
		found = append(found, s.sentenceNumbers(side, sentences[position])...)
	}
	return found
}

// withUnpairedText は組の文 sentences[index] と、そのすぐ前とすぐあとに続く組にならなかった文 (前と次の組の文の手前まで) をつないだ文字列。
func withUnpairedText(sentences []sentence, index int, paired map[int]bool) string {
	first := index
	for first > 0 && !paired[first-1] {
		first--
	}
	last := index
	for last+1 < len(sentences) && !paired[last+1] {
		last++
	}
	var sb strings.Builder
	for _, sn := range sentences[first : last+1] {
		sb.WriteString(sn.text)
	}
	return sb.String()
}

// quantityText は「一つの」「一つずつ」の相手の文の語を見るときの、組の文 (own) の側の文字列。2 つの文をまとめた・分けた推敲では、組の文のすぐ前とすぐあとの組にならなかった文もつなぐ。
// ただし、つなぐのは次のときだけ。(a) 自分の側に「一つの」「一つずつ」がある。(b) 相手の側に「一つずつ」がある。(c) 相手の側の「一つの」の名詞が、つなぐ隣の文に出る。
func quantityText(sentences []sentence, index int, paired map[int]bool, own sentence, ownNumbers, otherNumbers []number, otherDoc pyre.Text) string {
	joined := withUnpairedText(sentences, index, paired)
	if joined == own.text {
		return joined
	}
	if hasQuantityWeak(ownNumbers) {
		return joined
	}
	for _, n := range otherNumbers {
		if n.weak == weakEach {
			return joined
		}
	}
	rest := strings.Replace(joined, own.text, "", 1)
	for _, n := range otherNumbers {
		if n.weak != weakSingle {
			continue
		}
		if head := weakHead(otherDoc, n); head != "" && strings.Contains(rest, head) {
			return joined
		}
	}
	return own.text
}

func hasQuantityWeak(numbers []number) bool {
	for _, n := range numbers {
		if n.weak == weakEach || n.weak == weakSingle {
			return true
		}
	}
	return false
}

// movedKeys は続く文 (extra) の数値のうち、組の文から移った値とみなすものの Counter。source は値が消えた (増えた) 側の組の文、partner は続く文と同じ側の組の文、opposite は partner の側の増減。
// 続く文が partner より source に似ていれば (文を動かして組が 1 つずれた)、その文の値はすべて移した値。そうでなければ、opposite に同じ単位の値がない値だけを移した値とする。
func movedKeys(extra []followItem, opposite counter, source, partner string) counter {
	units := map[string]bool{}
	for k, count := range opposite {
		if count > 0 {
			units[k.unit] = true
		}
	}
	moved := counter{}
	pairRatio, haveRatio := 0.0, false
	srcRunes := pyunicode.Runes(source)
	for _, e := range extra {
		if len(e.numbers) == 0 {
			continue
		}
		if len(units) > 0 && slices.ContainsFunc(e.numbers, func(n number) bool { return units[n.unit] }) {
			if !haveRatio {
				pairRatio, haveRatio = textRatio(srcRunes, pyunicode.Runes(partner)), true
			}
			// ratio の上限 (quick_ratio) が組の似かた以下なら、ratio も超えないので求めない (結果は同じ)
			text := pyunicode.Runes(e.sentence.text)
			if ratioBound(srcRunes, text) > pairRatio && textRatio(srcRunes, text) > pairRatio {
				for _, n := range e.numbers {
					moved[n.key()]++
				}
				continue
			}
		}
		for _, n := range e.numbers {
			if !units[n.unit] {
				moved[n.key()]++
			}
		}
	}
	return moved
}

// clusterPartners は組の文の数値 (numbers) のうち、範囲・日付・時刻のまとまりの部品として、文書全体ではなく組の相手の文のまわりだけで「ある」を見る値の集合。
// 単位なしの数を持つまとまりと、表記の違いとして打ち消した値 (cancelled) を含むまとまりのうち、値の一部が組の相手の文 (others) に残るか打ち消したものの、すべての値。
func clusterPartners(numbers []number, cancelled counter, others []number) map[numKey]bool {
	partners := map[numKey]bool{}
	var kept map[numKey]bool
	for _, n := range numbers {
		if n.cluster == nil || (cancelled[n.key()] <= 0 && (n.unit != "" || n.notation == nil)) {
			continue
		}
		if kept == nil {
			kept = map[numKey]bool{}
			for _, o := range others {
				kept[o.key()] = true
			}
		}
		for _, k := range n.cluster {
			if kept[k] || cancelled[k] > 0 {
				for _, k2 := range n.cluster {
					partners[k2] = true
				}
				break
			}
		}
	}
	return partners
}

// dropWeak は数量を主張しない「一つ」(一つの、一つずつ、もう一つ) を、相手の側に同じ単位の値がなければ比べから外す。「二つの → 一つの」「一つの → 三つの」は数が変わったので残す。
// 「一つの」「一つずつ」は、相手の文で数の主張が変わっていれば (quantityChanged。「一つずつ → まとめて」「一つの → 複数の」) 外さない。
// 相手の文で新しく出た数を言う語は、語ごとの数の差で見る。渡された Counter を書き換える (本家と同じ)。
func dropWeak(removed, added counter, oldNumbers, newNumbers []number, oldText, newText string, oldDoc, newDoc pyre.Text) (counter, counter) {
	oldWords, newWords := quantityWordCounts(oldText), quantityWordCounts(newText)
	type pass struct {
		mine, other  counter
		numbers      []number
		partnerWords map[string]bool
		doc          pyre.Text
		own, partner string
	}
	for _, p := range []pass{
		{removed, added, oldNumbers, newWords2(newWords, oldWords), oldDoc, oldText, newText},
		{added, removed, newNumbers, newWords2(oldWords, newWords), newDoc, newText, oldText},
	} {
		weak := counter{}
		for _, n := range p.numbers {
			if n.weak != weakNone && !quantityChanged(n, p.doc, p.own, p.partner, p.partnerWords) {
				weak[n.key()]++
			}
		}
		for key, count := range weak {
			if p.mine[key] <= 0 {
				continue
			}
			hasUnit := false
			for k, n := range p.other {
				if k != key && n > 0 && k.unit == key.unit {
					hasUnit = true
				}
			}
			if !hasUnit {
				p.mine[key] -= min(count, p.mine[key])
			}
		}
	}
	return removed.positive(), added.positive()
}

func newWords2(a, b map[string]int) map[string]bool { return newWords(a, b) }

var (
	clauseEnd   = pyre.NewLazy(`[、，,。．！？!?]`, 0)
	contextWord = pyre.NewLazy(`[一-龥々〆ヵヶ]+|[ァ-ヴー]+|[A-Za-z]+`, 0)
)

func isSpaceish(r rune) bool { return r == ' ' || r == '\t' || r == 0x3000 }

func stripSpaceish(rs []rune) []rune {
	end := len(rs)
	for end > 0 && isSpaceish(rs[end-1]) {
		end--
	}
	start := 0
	for start < end && isSpaceish(rs[start]) {
		start++
	}
	return rs[start:end]
}

// ctxKey は数値ごとの (結び付きの手がかり, 値)。手がかりは、表の行なら何列目か、そうでなければ数の直前の 2 字と、数のあとの最初の 2 語。
type ctxKey struct {
	table bool
	col   int
	head  string
	words string
	key   numKey
}

func (s *sides) isTable(side int, sn sentence) bool {
	rows := s.analyses[side].Lines
	return sn.line-1 < len(rows) && rows[sn.line-1].Kind == "table"
}

// numberContexts は数値ごとの (結び付きの手がかり, 値) の多重集合。節を入れ替えただけで、語と数値の結び付きが同じなら、元と後で同じになる。
// 手がかりは、数値の直前の 2 字 (文頭と読点の直後は同じ「節の頭」とみなす)と、数値のあとから読点・句点 (か次の数値) までの最初の 2 語 (漢字、片仮名、英字の並び)。
// 表の行では、何列目か (前にある「|」の数) を手がかりにする。
func (s *sides) numberContexts(side int, sn sentence, numbers []number) map[ctxKey]int {
	found := map[ctxKey]int{}
	text := pyunicode.Runes(sn.text)
	table := s.isTable(side, sn)
	for index, n := range numbers {
		at := n.start - sn.start
		var ck ctxKey
		if table {
			count := 0
			for _, r := range text[:min(at, len(text))] {
				if r == '|' {
					count++
				}
			}
			ck = ctxKey{table: true, col: count}
		} else {
			head := stripSpaceish(text[:min(at, len(text))])
			headText := ""
			if len(head) > 0 && !strings.ContainsRune("、，,", head[len(head)-1]) {
				headText = pyunicode.FromRunes(head[max(0, len(head)-2):])
			}
			end := len(text)
			if index+1 < len(numbers) {
				end = numbers[index+1].start - sn.start
			}
			tail := runeSlice(text, n.end-sn.start, end)
			if cut := mustSearchAt(clauseEnd, pyre.NewText(tail), 0, pyre.NoEnd); cut != nil {
				tail = pyre.NewText(tail).Slice(0, cut.Start(0))
			}
			var words []string
			for _, m := range mustFinditer(contextWord, pyre.NewText(tail), 0, pyre.NoEnd) {
				w, _ := m.Group(0)
				words = append(words, w)
			}
			if len(words) > 2 {
				words = words[:2]
			}
			ck = ctxKey{head: headText, words: string(rune('0'+len(words))) + strings.Join(words, "\x01")}
		}
		ck.key = n.key()
		found[ck]++
	}
	return found
}

// numberTexts は numbers のうち、keys に入る値の書かれた形を、位置の順に、数えた回数だけ返す。
func (s *sides) numberTexts(side int, numbers []number, keys counter) []string {
	keys = keys.clone()
	var texts []string
	for _, n := range numbers {
		if keys[n.key()] > 0 {
			keys[n.key()]--
			texts = append(texts, runeSlice(s.texts[side], n.start, n.end))
		}
	}
	return texts
}

var leadModifiers = []string{"合計", "最大", "最小", "最短", "最長", "およそ", "約", "計"}

// lead は数 n の直前の 2 字 (文 sn の中だけ)。文頭か読点の直後なら "^"。数のすぐ前の共通の修飾語 (約、計、最大 など) は飛ばしてから取る。
func lead(text []rune, sn sentence, n number) string {
	end := n.start
	for _, word := range leadModifiers {
		w := len(pyunicode.Runes(word))
		if strings.HasSuffix(runeSlice(text, max(sn.start, end-w), end), word) {
			end -= w
			break
		}
	}
	before := runeSlice(text, max(sn.start, end-2), end)
	if before == "" || strings.HasSuffix(before, "、") || strings.HasSuffix(before, "，") || strings.HasSuffix(before, ",") {
		return "^"
	}
	return before
}

type keyLead struct {
	key  numKey
	lead string
}

// strongSwapped は数量を言う値 (weak でない値) と、その数の直前の 2 字 (lead) の組を元と後で比べ、値の並びが変わり、値の多重集合と直前の 2 字の多重集合はどちらも同じなのに、
// 組の多重集合が違えば真 (値と直前の語の結び付きが入れ替わった)。直前が文頭か読点の数は組に入れない。
func strongSwapped(oldText []rune, old sentence, oldNumbers []number, newText []rune, new sentence, newNumbers []number) bool {
	pairs := func(text []rune, sn sentence, numbers []number) []keyLead {
		var out []keyLead
		for _, n := range numbers {
			if n.weak != weakNone {
				continue
			}
			if l := lead(text, sn, n); l != "^" {
				out = append(out, keyLead{n.key(), l})
			}
		}
		return out
	}
	olds, news := pairs(oldText, old, oldNumbers), pairs(newText, new, newNumbers)
	keysOf := func(l []keyLead) []numKey {
		out := make([]numKey, len(l))
		for i, p := range l {
			out[i] = p.key
		}
		return out
	}
	if !countsEqual(multiset(olds, keyOf), multiset(news, keyOf)) || !countsEqual(multiset(olds, leadOf), multiset(news, leadOf)) {
		return false
	}
	return !slices.Equal(keysOf(olds), keysOf(news)) && !countsEqual(multiset(olds, pairOf), multiset(news, pairOf))
}

func keyOf(p keyLead) numKey   { return p.key }
func leadOf(p keyLead) string  { return p.lead }
func pairOf(p keyLead) keyLead { return p }

// multiset は並びの多重集合 (値は f で取る)。
func multiset[K comparable](l []keyLead, f func(keyLead) K) map[K]int {
	out := map[K]int{}
	for _, p := range l {
		out[f(p)]++
	}
	return out
}

// unitOrders は数値の並びを、単位ごとの値の並びにしたもの。
func unitOrders(numbers []number) map[string][]numKey {
	out := map[string][]numKey{}
	for _, n := range numbers {
		out[n.unit] = append(out[n.unit], n.key())
	}
	return out
}

func unitOrdersEqual(a, b map[string][]numKey) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// numberPhrases は数値の前に付く語句ごとの値 (Counter) の辞書を 2 つ返す。1 つ目の語句は節の初め (文頭か読点の直後) からその数の前までの字、
// 2 つ目は前の数のあと (同じ節に前の数がなければ節の初め) からその数の前までの字。節の初めにある数 (語句が空) と表の行は見ない。
func (s *sides) numberPhrases(side int, sn sentence, numbers []number) (clause, near map[string]counter) {
	if s.isTable(side, sn) {
		return map[string]counter{}, map[string]counter{}
	}
	text := pyunicode.Runes(sn.text)
	clause, near = map[string]counter{}, map[string]counter{}
	previous := 0
	for _, n := range numbers {
		at := n.start - sn.start
		start := 0
		for i := min(at, len(text)) - 1; i >= 0; i-- {
			if strings.ContainsRune("、，,", text[i]) {
				start = i + 1
				break
			}
		}
		for _, f := range []struct {
			found map[string]counter
			begin int
		}{{clause, start}, {near, max(start, previous)}} {
			if phrase := string(stripSpaceish(pySlice(text, f.begin, at))); phrase != "" {
				if f.found[phrase] == nil {
					f.found[phrase] = counter{}
				}
				f.found[phrase][n.key()]++
			}
		}
		previous = n.end - sn.start
	}
	return clause, near
}

// orderChanged は組の文の数値の並びが変わり、数と語の結び付きも変わったか (数値の並びの候補)。
// 値の並びが違い、結び付きの手がかり (numberContexts) も違う組のうち、次のどれかに当たるものだけを候補にする。
// 1. 同じ単位の数どうしの並びが変わった (「部屋にふたり、廊下に3人 → 部屋に3人、廊下にふたり」)。
// 3. 数量を言う値と、その数の直前の 2 字の組が入れ替わった (strongSwapped)。
// 2. 同じ語句に付く値が、元と後で 2 つ以上の語句について変わった (numberPhrases。変わった語句が 1 つだけの言い換えは数えない)。
func (s *sides) orderChanged(old sentence, oldNumbers []number, new sentence, newNumbers []number) bool {
	if slices.Equal(numberKeys(oldNumbers), numberKeys(newNumbers)) {
		return false
	}
	if countsEqual(s.numberContexts(0, old, oldNumbers), s.numberContexts(1, new, newNumbers)) {
		return false
	}
	if !unitOrdersEqual(unitOrders(oldNumbers), unitOrders(newNumbers)) {
		return true
	}
	if strongSwapped(s.texts[0], old, oldNumbers, s.texts[1], new, newNumbers) {
		return true
	}
	oc, on := s.numberPhrases(0, old, oldNumbers)
	nc, nn := s.numberPhrases(1, new, newNumbers)
	for _, pair := range [][2]map[string]counter{{oc, nc}, {on, nn}} {
		changed := 0
		for phrase, c := range pair[0] {
			if other, ok := pair[1][phrase]; ok && !c.equal(other) {
				changed++
			}
		}
		if changed >= 2 {
			return true
		}
	}
	return false
}

func numberKeys(numbers []number) []numKey {
	out := make([]numKey, len(numbers))
	for i, n := range numbers {
		out[i] = n.key()
	}
	return out
}

// groupIndex は risk を並べる hunk の番号。まとめた並びの組では、文のある hunk の番号 (見つからなければ並びの最初)。
func groupIndex(g *sentenceGroup, sn *sentence, side int) int {
	if len(g.run) == 0 || sn == nil {
		return g.hunk.index
	}
	for _, h := range g.run {
		start, end := h.newStart, h.newEnd
		if side != 1 {
			start, end = h.oldStart, h.oldEnd
		}
		if start <= sn.line && sn.line <= end {
			return h.index
		}
	}
	return g.hunk.index
}

// riskItem は意味が変わる恐れ 1 件と、並べる位置 (hunk の番号, 塊の中の順)。
type riskItem struct {
	index, order int
	risk         *pyjson.Object
}

func (r riskItem) value() pyjson.Value {
	return []pyjson.Value{int64(r.index), int64(r.order), r.risk}
}

func riskItemsValue(items []riskItem) pyjson.Value { return pyjson.MapList(items, riskItem.value) }

func optionalLine(n int) pyjson.Value {
	if n == 0 {
		return nil
	}
	return int64(n)
}

func optionalText(s string, ok bool) pyjson.Value {
	if !ok {
		return nil
	}
	return s
}

// newRisk は本家の _risk。行番号は 0 なら null、文は ok が偽なら null。
func newRisk(kind string, subkind pyjson.Value, oldLine, newLine int, old, new string, hasOld, hasNew bool, detail pyjson.Value) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("kind", kind)
	o.Set("subkind", subkind)
	o.Set("old_line", optionalLine(oldLine))
	o.Set("new_line", optionalLine(newLine))
	o.Set("old", optionalText(old, hasOld))
	o.Set("new", optionalText(new, hasNew))
	o.Set("detail", detail)
	o.Set("basis", nil)
	return o
}

func numberDetail(removed, added []string) pyjson.Value {
	d := pyjson.NewObject()
	d.Set("removed", pyjson.Strings(removed))
	d.Set("added", pyjson.Strings(added))
	return d
}

func intSet(pairs [][2]int, second bool) map[int]bool {
	out := map[int]bool{}
	for _, p := range pairs {
		if second {
			out[p[1]] = true
		} else {
			out[p[0]] = true
		}
	}
	return out
}

// sentenceFinder は位置を含む文 (行番号, 文書の中の位置, 文) を返す関数。hunk の文になければ、その行を丸ごと使う。文の始まりの位置の並びを 1 回だけ作り、二分探索で引く。
func (s *sides) sentenceFinder(side int, groups []*sentenceGroup) func(int) (int, int, string) {
	var sentences []sentence
	for _, g := range groups {
		if side == 0 {
			sentences = append(sentences, g.old...)
		} else {
			sentences = append(sentences, g.new...)
		}
	}
	starts := make([]int, len(sentences))
	for i, sn := range sentences {
		starts[i] = sn.start
	}
	return func(position int) (int, int, string) {
		index := bisectRight(starts, position) - 1
		if index >= 0 && position < sentences[index].start+pystr.Len(sentences[index].text) {
			sn := sentences[index]
			return sn.line, sn.start, sn.text
		}
		line := s.lineOf(side, position)
		return line, s.starts[side][line-1], pystr.Strip(s.lines[side][line-1])
	}
}

// hunkFinder はその側の行番号を含む hunk の番号を返す関数 (含む hunk がなければ ok が偽)。hunk の始まりの行で二分探索する。
func hunkFinder(full *fullDiff, side int) func(int) (int, bool) {
	starts := make([]int, len(full.hunks))
	for i, h := range full.hunks {
		starts[i] = h.newStart
		if side == 0 {
			starts[i] = h.oldStart
		}
	}
	return func(line int) (int, bool) {
		index := bisectRight(starts, line) - 1
		if index < 0 {
			return 0, false
		}
		h := full.hunks[index]
		first, last := h.newStart, h.newEnd
		if side == 0 {
			first, last = h.oldStart, h.oldEnd
		}
		if first <= line && line <= last {
			return h.index, true
		}
		return 0, false
	}
}

// numberRisks は数値 (kind number)。(hunk の番号, 塊の中の順, risk) の並びを返す。
// 組になった文で比べる (replaced、added、lost、order)。そのあと、文書全体で後にだけある値と元にだけある値のうち、組の文で出していないもの (足した文、消した文の中の値) を、値の集合で added と lost にする。
// replaced に入れた値は added と lost に重ねない。「元の文書のどこにもない」「後の文書のどこにもない」は、伏せる前の数値 (コードや URL の中も含む) で見る。
// 文書全体の比べで出すのは、hunk の行の中にある値だけ。数量を主張しない「一つ」は、単独では元にない数値・消えた数値にしない。
func (s *sides) numberRisks(full *fullDiff, groups []*sentenceGroup) []riskItem {
	var out []riskItem
	oldAll, newAll := s.allKeysOf(0), s.allKeysOf(1)
	shownRemoved, shownAdded := map[numKey]bool{}, map[numKey]bool{}
	order := 0
	for _, group := range groups {
		pairedOld, pairedNew := intSet(group.pairs, false), intSet(group.pairs, true)
		for _, p := range group.pairs {
			i, j := p[0], p[1]
			old, new := group.old[i], group.new[j]
			if old.text == new.text {
				continue
			}
			index := groupIndex(group, &new, 1)
			oldNumbers, newNumbers := s.sentenceNumbers(0, old), s.sentenceNumbers(1, new)
			oldCount, newCount := counterOf(oldNumbers), counterOf(newNumbers)
			rawRemoved, rawAdded := oldCount.minus(newCount), newCount.minus(oldCount)
			// 文を 2 つに分けた・2 つを合わせた推敲 (組の文のすぐあとに、組にならなかった文が続く)。組の文で消えた値が続く文にあれば (数と単位が同じ)、移しただけとみなして比べから外す。
			extraOld := s.followingUnpaired(0, group.old, i, pairedOld)
			extraNew := s.followingUnpaired(1, group.new, j, pairedNew)
			if len(extraNew) > 0 {
				if moved := movedKeys(extraNew, rawAdded, old.text, new.text); len(moved) > 0 {
					rawRemoved = rawRemoved.minus(moved)
				}
			}
			if len(extraOld) > 0 {
				if moved := movedKeys(extraOld, rawRemoved, new.text, old.text); len(moved) > 0 {
					rawAdded = rawAdded.minus(moved)
				}
			}
			removed, added := cancelNotation(rawRemoved, rawAdded, oldNumbers, newNumbers)
			// 表記の違いとして打ち消した値は、文書全体で比べるときにもう一度出さない
			for k := range rawRemoved.minus(removed) {
				shownRemoved[k] = true
			}
			for k := range rawAdded.minus(added) {
				shownAdded[k] = true
			}
			oldText, newText := old.text, new.text
			if hasQuantityWeak(oldNumbers) || hasQuantityWeak(newNumbers) {
				oldText = quantityText(group.old, i, pairedOld, old, oldNumbers, newNumbers, s.docText(1))
				newText = quantityText(group.new, j, pairedNew, new, newNumbers, oldNumbers, s.docText(0))
			}
			removed, added = dropWeak(removed, added, oldNumbers, newNumbers, oldText, newText, s.docText(0), s.docText(1))
			var risk *pyjson.Object
			switch {
			case len(removed) > 0 && len(added) > 0:
				risk = newRisk("number", "replaced", old.line, new.line, old.text, new.text, true, true,
					numberDetail(s.numberTexts(0, oldNumbers, removed), s.numberTexts(1, newNumbers, added)))
				for k := range removed {
					shownRemoved[k] = true
				}
				for k := range added {
					shownAdded[k] = true
				}
			case len(added) > 0:
				partners := clusterPartners(newNumbers, rawAdded.minus(added), oldNumbers)
				var pairSet *keySet
				if len(partners) > 0 {
					pairSet = newKeySet(append(append([]number(nil), oldNumbers...), s.unpairedAround(0, group.old, i, pairedOld)...))
				}
				index2 := notationIndex(newNumbers)
				fresh := counter{}
				for key, count := range added {
					other := oldAll
					if partners[key] {
						other = pairSet
					}
					if !present(key, index2, other) {
						fresh[key] = count
					}
				}
				if len(fresh) > 0 {
					risk = newRisk("number", "added", old.line, new.line, old.text, new.text, true, true,
						numberDetail(nil, s.numberTexts(1, newNumbers, fresh)))
					for k := range fresh {
						shownAdded[k] = true
					}
				}
			case len(removed) > 0:
				partners := clusterPartners(oldNumbers, rawRemoved.minus(removed), newNumbers)
				var pairSet *keySet
				if len(partners) > 0 {
					pairSet = newKeySet(append(append([]number(nil), newNumbers...), s.unpairedAround(1, group.new, j, pairedNew)...))
				}
				index2 := notationIndex(oldNumbers)
				gone := counter{}
				for key, count := range removed {
					other := newAll
					if partners[key] {
						other = pairSet
					}
					if !present(key, index2, other) {
						gone[key] = count
					}
				}
				if len(gone) > 0 {
					risk = newRisk("number", "lost", old.line, new.line, old.text, new.text, true, true,
						numberDetail(s.numberTexts(0, oldNumbers, gone), nil))
					for k := range gone {
						shownRemoved[k] = true
					}
				}
			case oldCount.equal(newCount) && !anyNumbers(extraOld) && !anyNumbers(extraNew) && s.orderChanged(old, oldNumbers, new, newNumbers):
				// 並びは、値と単位の多重集合がまったく同じときだけ見る (表記の違いだけを打ち消した組は並びに数えない)
				d := pyjson.NewObject()
				d.Set("old_order", pyjson.Strings(s.numberSpans(0, oldNumbers)))
				d.Set("new_order", pyjson.Strings(s.numberSpans(1, newNumbers)))
				risk = newRisk("number", "order", old.line, new.line, old.text, new.text, true, true, d)
			}
			if risk != nil {
				out = append(out, riskItem{index, order, risk})
				order++
			}
		}
	}
	// 文書全体で比べる (足した文、消した文、組にならなかった文の中の値)
	for side := range 2 {
		shown, other, subkind := shownRemoved, newAll, "lost"
		if side == 1 {
			shown, other, subkind = shownAdded, oldAll, "added"
		}
		found := s.numbersOf(side).found
		index := notationIndex(found)
		keys := map[numKey]bool{}
		for _, n := range found {
			if k := n.key(); !shown[k] && !present(k, index, other) {
				keys[k] = true
			}
		}
		if len(keys) == 0 {
			continue
		}
		sentenceAt, hunkAt := s.sentenceFinder(side, groups), hunkFinder(full, side)
		type firstHit struct {
			n    number
			hunk int
		}
		var firsts []firstHit
		seen := map[numKey]bool{}
		for _, n := range found {
			// 数量を主張しない「一つ」は、単独では出さない
			if keys[n.key()] && !seen[n.key()] && n.weak == weakNone {
				if hunk, ok := hunkAt(s.lineOf(side, n.start)); ok {
					seen[n.key()] = true
					firsts = append(firsts, firstHit{n, hunk})
				}
			}
		}
		type sentenceKey struct {
			hunk, line, start int
			text              string
		}
		var order []sentenceKey
		bySentence := map[sentenceKey][]string{}
		for _, f := range firsts {
			line, start, text := sentenceAt(f.n.start)
			sk := sentenceKey{f.hunk, line, start, text}
			if _, ok := bySentence[sk]; !ok {
				order = append(order, sk)
			}
			bySentence[sk] = append(bySentence[sk], runeSlice(s.texts[side], f.n.start, f.n.end))
		}
		for _, sk := range order {
			texts := bySentence[sk]
			var risk *pyjson.Object
			if side == 0 {
				risk = newRisk("number", subkind, sk.line, 0, sk.text, "", true, false, numberDetail(texts, nil))
			} else {
				risk = newRisk("number", subkind, 0, sk.line, "", sk.text, false, true, numberDetail(nil, texts))
			}
			out = append(out, riskItem{sk.hunk, 1000000 + sk.start, risk})
		}
	}
	return out
}

func anyNumbers(items []followItem) bool {
	for _, e := range items {
		if len(e.numbers) > 0 {
			return true
		}
	}
	return false
}

// numberSpans は数値の書かれた形を、位置の順に返す。
func (s *sides) numberSpans(side int, numbers []number) []string {
	out := make([]string, len(numbers))
	for i, n := range numbers {
		out[i] = runeSlice(s.texts[side], n.start, n.end)
	}
	return out
}

// NumberRisks は元の文と書き直した文の、数値の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func NumberRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.numberRisks(full, s.sentencePairs(full.hunks))), nil
}
