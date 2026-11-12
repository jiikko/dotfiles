package difflib

import (
	"slices"
	"sort"
)

// tuning は本家の _ExactSequenceMatcher の定数 (yomiyasu_diff.py の _MATCH_SMALL_AREA から _MATCH_AUTOMATON_MAX まで)。どの求め方も同じ値を求めるので、
// 定数は時間だけを変え、答えは変えない (本家の主張)。NewExact は本家と同じ値 (defaultTuning) を使う。試験は小さい値にして、短い入力でも全部の求め方を通す。
type tuning struct {
	smallArea     int // 範囲の両側の字数の積がこれ以下なら DP
	probeMax      int // 上限の長さちょうどの一致を、その長さの部分文字列の表で探す長さの上限
	seedShort     int // 種の長さ (sampleMin 以下)
	seedLong      int // 探す長さが種の 2 倍以上の段で使う長い種
	sampleMin     int // 種で探す段の、一致の長さの下限
	sampleBudget  int // 種で探す 1 段で調べてよい候補の数 = 範囲の両側の字数の和 / これ
	samFactor     int // 接尾辞オートマトンの 1 字の手間が DP の内側の 1 回の何倍か
	wholeIndexMin int // b の範囲がこれより長いときは b 全体の表を作って使い回す
	probeCache    int // 長さちょうどの一致を探す b 全体の表を、いくつの長さの分まで残すか
	localDiv      int // b 全体の表がまだない長さで、範囲の両側の字数の和が len(b) / これ以下なら、その範囲だけで探す
	automatonMax  int // 接尾辞オートマトンを作る b の範囲の長さの上限
	// hashMask は部分文字列の hash に掛ける mask (本家に無い。試験が小さくして hash の衝突を起こし、字で比べ直す経路を通す)。
	hashMask uint64
}

var defaultTuning = tuning{
	smallArea: 2048, probeMax: 64, seedShort: 12, seedLong: 40, sampleMin: 24, sampleBudget: 4,
	samFactor: 24, wholeIndexMin: 4096, probeCache: 2, localDiv: 8, automatonMax: 1 << 16, hashMask: ^uint64(0),
}

// path は速い照合の求め方 (試験が全部の求め方を通ったことを数えるための番号)。
type path int

const (
	pathSmallDP       path = iota // 範囲が小さいので DP
	pathFindA                     // 上限 = a の範囲の長さで、a の範囲が b に含まれた
	pathFindB                     // 上限 = b の範囲の長さで、b の範囲が a に含まれた
	pathFindMiss                  // 上の 2 つで含まれなかった (上限を 1 減らして続ける)
	pathProbeSmall                // 長さちょうどの一致を、b の範囲だけの表で探した (b の範囲が短い)
	pathProbeWhole                // 長さちょうどの一致を、b 全体の表で探した
	pathProbeLocalB               // 表の無い長さで、範囲だけの表 (b の側) で探した
	pathProbeLocalA               // 表の無い長さで、範囲だけの表 (a の側) で探した
	pathProbeEvict                // b 全体の表を最近使った順に追い出した
	pathProbeHit                  // 長さちょうどの一致が見つかった
	pathSeededFound               // 種で探して見つかった
	pathSeededEarly               // 種で探す段の早い打ち切り (length が上限と同じ段で、最初に見つかった格子の位置で止めた)
	pathSeededNone                // 種で探す段で、長さ length 以上の一致が無かった
	pathSeededBudget              // 種で探す段で、候補が多すぎて次の手に回した
	pathLateDP                    // 残りを DP で解いた
	pathAutomaton                 // 残りを接尾辞オートマトンで解いた
	pathAutomatonCap              // オートマトンが上限の長さに届いて読むのをやめた
	pathBisect                    // 残りを長さの二分探索で解いた
	pathHashCollision             // hash が同じで字の違う部分文字列を引いた (字で比べ直した)
	numPaths
)

// probeEntry は長さ size の b 全体の部分文字列の表 (最近使った順に tune.probeCache 個まで残す)。
type probeEntry struct {
	size  int
	index *gramIndex
}

// exactFinder は本家の _ExactSequenceMatcher の求め方。表 (種の表・長さちょうどの表・字の重なりの割合) は要るときに作り、並びを替えたら捨てる。
type exactFinder struct {
	m             *SequenceMatcher[rune]
	tune          tuning
	collisionRate float64
	haveCollision bool
	seedIndexes   map[int]*gramIndex
	probeIndexes  []probeEntry // 古いものが前
	stats         [numPaths]int
}

// NewExact は本家の _ExactSequenceMatcher(None, a, b, autojunk=False)。New と同じ答えを、長い文書で少ない計算で求める。
func NewExact(a, b []rune) *SequenceMatcher[rune] {
	m := &SequenceMatcher[rune]{}
	m.fast = &exactFinder{m: m, tune: defaultTuning}
	m.SetSeqs(a, b)
	return m
}

func (f *exactFinder) resetA() { f.haveCollision = false }

func (f *exactFinder) resetB() {
	f.haveCollision = false
	f.seedIndexes = nil
	f.probeIndexes = nil
}

// longest は find_longest_match(alo, ahi, blo, bhi) と同じ値 (本家の _longest)。bound は答えの長さの上限 (証明済み)。
func (f *exactFinder) longest(alo, ahi, blo, bhi, bound int) Match {
	a, b := f.m.a, f.m.b
	la, lb := ahi-alo, bhi-blo
	limit := min(bound, la, lb)
	if limit <= 0 {
		return Match{alo, blo, 0}
	}
	if la*lb <= f.tune.smallArea {
		f.stats[pathSmallDP]++
		return f.m.dp(alo, ahi, blo, bhi)
	}
	// 上限の長さちょうどの一致を探す。見つかれば、a で最も前 (同じ a なら b で最も前) のものが答え。見つからなければ答えは上限 -1 以下。
	switch {
	case limit == la:
		if j := indexRunes(b, blo, bhi, a[alo:ahi]); j != -1 {
			f.stats[pathFindA]++
			return Match{alo, j, limit}
		}
		f.stats[pathFindMiss]++
		limit--
	case limit == lb:
		if i := indexRunes(a, alo, ahi, b[blo:bhi]); i != -1 {
			f.stats[pathFindB]++
			return Match{i, blo, limit}
		}
		f.stats[pathFindMiss]++
		limit--
	case limit <= f.tune.probeMax:
		if found, ok := f.probe(alo, ahi, blo, bhi, limit); ok {
			f.stats[pathProbeHit]++
			return found
		}
		limit--
	}
	if limit <= 0 {
		return Match{alo, blo, 0}
	}
	// 長い一致を種で探す。長さ L 以上の一致がなければ答えは L より短いので、L を半分にして続ける。
	length := limit
	for length >= f.tune.sampleMin {
		found, state := f.seeded(alo, ahi, blo, bhi, length, limit, (la+lb)/f.tune.sampleBudget)
		if state == seedOverBudget {
			f.stats[pathSeededBudget]++
			break
		}
		if state == seedFound {
			f.stats[pathSeededFound]++
			return found
		}
		f.stats[pathSeededNone]++
		limit = length - 1
		length /= 2
	}
	// 残り。DP の手間の見込みが小さければ DP、大きければ接尾辞オートマトン。b の範囲がとても長いときは長さの二分探索 (使う記憶を抑える)。
	if float64(la*lb)*f.collision() <= float64(f.tune.samFactor*(la+lb)) {
		f.stats[pathLateDP]++
		return f.m.dp(alo, ahi, blo, bhi)
	}
	if lb <= f.tune.automatonMax {
		f.stats[pathAutomaton]++
		found, capped := automatonLongest(a, alo, ahi, b, blo, bhi, limit)
		if capped {
			f.stats[pathAutomatonCap]++
		}
		return found
	}
	f.stats[pathBisect]++
	return f.bisectLength(alo, ahi, blo, bhi, limit)
}

// collision は a の 1 字と b の 1 字が同じである割合 (DP の手間の見積もりにだけ使う)。
func (f *exactFinder) collision() float64 {
	if !f.haveCollision {
		countA := make(map[rune]int)
		for _, r := range f.m.a {
			countA[r]++
		}
		same := 0
		for r, n := range countA {
			same += n * len(f.m.b2j[r])
		}
		f.collisionRate = float64(same) / float64(max(1, len(f.m.a)*len(f.m.b)))
		f.haveCollision = true
	}
	return f.collisionRate
}

// bisectLength は答えの長さを二分探索で決める (本家の _bisect_length)。長さ m の共通の部分があれば長さ m-1 もあるので、「長さ m ちょうどの一致があるか」は m について単調。
func (f *exactFinder) bisectLength(alo, ahi, blo, bhi, limit int) Match {
	low, high := 0, limit
	var found Match
	have := false
	for low < high {
		middle := (low + high + 1) / 2
		if match, ok := f.firstOfLength(alo, ahi, blo, bhi, middle); ok {
			low, found, have = middle, match, true
		} else {
			high = middle - 1
		}
	}
	if !have {
		return Match{alo, blo, 0}
	}
	return found
}

// firstOfLength は長さ size ちょうどの共通の部分のうち、a で最も前、同じ a なら b で最も前のもの (本家の _first_of_length と _probe の短い範囲)。
// b[blo:bhi] の部分ごとの hash で最も前の位置を残し、a の位置を前から見る。同じ並びは同じ hash なので、残った位置の字が a の部分と同じならそれが最も前。
// 違えば (hash だけが同じ) b の範囲を前から探し直す。本家は短い size で Python の hash と str.find を使うが、答えは同じ。
func (f *exactFinder) firstOfLength(alo, ahi, blo, bhi, size int) (Match, bool) {
	a, b := f.m.a, f.m.b
	first := make(map[uint64]int)
	for offset, h := range f.rollingHashes(b[blo:bhi], size) {
		if _, ok := first[h]; !ok {
			first[h] = blo + offset
		}
	}
	for offset, h := range f.rollingHashes(a[alo:ahi], size) {
		j, ok := first[h]
		if !ok {
			continue
		}
		i := alo + offset
		gram := a[i : i+size]
		if !slices.Equal(b[j:j+size], gram) {
			f.stats[pathHashCollision]++
			if j = indexRunes(b, blo, bhi, gram); j == -1 {
				continue
			}
		}
		return Match{i, j, size}, true
	}
	return Match{}, false
}

// probe は長さ size ちょうどの共通の部分のうち、a で最も前、同じ a なら b で最も前のもの (本家の _probe)。
func (f *exactFinder) probe(alo, ahi, blo, bhi, size int) (Match, bool) {
	a, b := f.m.a, f.m.b
	if bhi-blo <= f.tune.wholeIndexMin {
		f.stats[pathProbeSmall]++
		return f.firstOfLength(alo, ahi, blo, bhi, size)
	}
	// b の範囲が長いときは、b 全体の表を長さごとに作って使い回す (右の範囲が続くと、同じ長さで何度も引くため)。表は最近使った順に tune.probeCache 個まで残す。
	// 表のない長さで、範囲が b 全体より十分小さいときは、表を作らずにその範囲だけで探す。
	var index *gramIndex
	for k, e := range f.probeIndexes {
		if e.size == size {
			index = e.index
			f.probeIndexes = slices.Delete(f.probeIndexes, k, k+1)
			break
		}
	}
	if index == nil {
		if (ahi-alo)+(bhi-blo) <= len(b)/f.tune.localDiv {
			return f.probeLocal(alo, ahi, blo, bhi, size)
		}
		for len(f.probeIndexes) >= f.tune.probeCache {
			f.stats[pathProbeEvict]++
			f.probeIndexes = f.probeIndexes[1:]
		}
		index = f.newGramIndex(b, size)
	}
	f.probeIndexes = append(f.probeIndexes, probeEntry{size, index})
	f.stats[pathProbeWhole]++
	for offset, h := range f.rollingHashes(a[alo:ahi], size) {
		i := alo + offset
		gram := a[i : i+size]
		if j, ok := index.first(h, blo, bhi-size, func(j int) bool { return slices.Equal(b[j:j+size], gram) }, &f.stats[pathHashCollision]); ok {
			return Match{i, j, size}, true
		}
	}
	return Match{}, false
}

// probeLocal は probe と同じ値を、範囲の中だけの表で求める (本家の _probe_local)。短いほうの側の表を作り、もう片方を前から見る。
// a の側の表 (部分ごとに最も前の a の位置) なら、b の位置を前から見て、引けた a の位置がそれまでより前のときだけ残す。
// 同じ a の位置は b で最も前の位置で最初に引けるので、残ったものが a で最も前、同じ a なら b で最も前になる。
func (f *exactFinder) probeLocal(alo, ahi, blo, bhi, size int) (Match, bool) {
	a, b := f.m.a, f.m.b
	if bhi-blo <= ahi-alo {
		f.stats[pathProbeLocalB]++
		return f.firstOfLength(alo, ahi, blo, bhi, size)
	}
	f.stats[pathProbeLocalA]++
	first := make(map[uint64]int)
	for offset, h := range f.rollingHashes(a[alo:ahi], size) {
		if _, ok := first[h]; !ok {
			first[h] = alo + offset
		}
	}
	besti, bestj := -1, -1
	for offset, h := range f.rollingHashes(b[blo:bhi], size) {
		i, ok := first[h]
		if !ok {
			continue
		}
		j := blo + offset
		gram := b[j : j+size]
		if !slices.Equal(a[i:i+size], gram) {
			f.stats[pathHashCollision]++
			if i = indexRunes(a, alo, ahi, gram); i == -1 {
				continue
			}
		}
		if besti == -1 || i < besti {
			besti, bestj = i, j
			if i == alo {
				break
			}
		}
	}
	if besti == -1 {
		return Match{}, false
	}
	return Match{besti, bestj, size}, true
}

type seedState int

const (
	seedFound      seedState = iota // 長さ length 以上の答えが見つかった
	seedNone                        // 長さ length 以上の一致は無い (答えは length-1 以下)
	seedOverBudget                  // 候補が多すぎて調べきれない (何も決めない)
)

type seedHit struct{ t, u int }

// seeded は長さ length 以上の共通の部分のうち、いちばん長く、a で最も前、同じ a なら b で最も前のもの (本家の _seeded。証明は本家の docstring)。
// 種の長さを q、格子の間隔を s = length-q+1 とし、a の格子の位置 t の種 a[t:t+q] が b[blo:bhi] に現れる所を全部引いて前後に伸ばす。
// length が上限と同じ段では、一致が見つかった最初の格子の位置で止める。
func (f *exactFinder) seeded(alo, ahi, blo, bhi, length, limit, budget int) (Match, seedState) {
	a, b := f.m.a, f.m.b
	q := f.tune.seedShort
	if length >= 2*f.tune.seedLong {
		q = f.tune.seedLong
	}
	if f.seedIndexes == nil {
		f.seedIndexes = make(map[int]*gramIndex)
	}
	index := f.seedIndexes[q]
	if index == nil {
		index = f.newGramIndex(b, q)
		f.seedIndexes[q] = index
	}
	step := length - q + 1
	early := length >= limit
	var seeds []seedHit
	total := 0
	var best seedCandidate
	have, stopped := false, false
	for t := alo + step - 1; t < ahi-q+1; t += step {
		gram := a[t : t+q]
		h := f.polyHash(gram)
		over := false
		index.each(h, blo, bhi-q, func(u int) bool {
			if !slices.Equal(b[u:u+q], gram) {
				f.stats[pathHashCollision]++
				return true
			}
			seeds = append(seeds, seedHit{t, u})
			total++
			if total > budget {
				over = true
				return false
			}
			return true
		})
		if over {
			return Match{}, seedOverBudget
		}
		if early && len(seeds) > 0 {
			best, have = f.extendSeeds(seeds, alo, ahi, blo, bhi, q, length, limit)
			if have {
				f.stats[pathSeededEarly]++
				stopped = true
				break
			}
			seeds = seeds[:0]
		}
	}
	if !stopped {
		best, have = f.extendSeeds(seeds, alo, ahi, blo, bhi, q, length, limit)
	}
	if !have {
		return Match{}, seedNone
	}
	return Match{best.i, best.j, best.size}, seedFound
}

type seedCandidate struct{ size, i, j int }

// less は (-長さ, a の位置, b の位置) の順 (本家の candidate < best)。
func (c seedCandidate) less(d seedCandidate) bool {
	if c.size != d.size {
		return c.size > d.size
	}
	if c.i != d.i {
		return c.i < d.i
	}
	return c.j < d.j
}

// extendSeeds は種を範囲の中で前後に伸ばせるだけ伸ばした一致のうち、長さ length 以上で、いちばん長く、a で最も前、b で最も前のもの (本家の _extend_seeds)。
// どの一致も長さは上限以下なので、伸ばす長さも上限で止める。
func (f *exactFinder) extendSeeds(seeds []seedHit, alo, ahi, blo, bhi, q, length, limit int) (seedCandidate, bool) {
	a, b := f.m.a, f.m.b
	var best seedCandidate
	have := false
	for _, s := range seeds {
		t, u := s.t, s.u
		back := commonSuffixLen(a, t, b, u, min(t-alo, u-blo, limit-q))
		size := back + q + commonPrefixLen(a, t+q, b, u+q, min(ahi-t, bhi-u, limit-back)-q)
		if size >= length {
			c := seedCandidate{size, t - back, u - back}
			if !have || c.less(best) {
				best, have = c, true
			}
		}
	}
	return best, have
}

// commonPrefixLen は a[i:i+n] == b[j:j+n] となる最大の n (limit 以下)。本家は切り出した文字列どうしを倍々の幅で比べるが (C の速さで比べるため)、答えは同じ。
func commonPrefixLen(a []rune, i int, b []rune, j int, limit int) int {
	n := 0
	for n < limit && a[i+n] == b[j+n] {
		n++
	}
	return n
}

// commonSuffixLen は a[i-n:i] == b[j-n:j] となる最大の n (limit 以下)。
func commonSuffixLen(a []rune, i int, b []rune, j int, limit int) int {
	n := 0
	for n < limit && a[i-n-1] == b[j-n-1] {
		n++
	}
	return n
}

// automatonLongest は a[alo:ahi] と b[blo:bhi] のいちばん長い共通の部分 (同じ長さなら a で前、それも同じなら b で前) を、b[blo:bhi] の接尾辞オートマトンで求める
// (本家の _automaton_longest)。各状態に、その状態の文字列が b で最初に終わる位置 (first) を持たせる。a を前から 1 字ずつ読み、
// 長さが今までの最大を超えたときだけ記録するので、最大の長さに最初に届いた a の位置が残る。limit (証明済みの上限) に届いたら読むのをやめる。
// capped は limit に届いて読むのをやめたか (試験が枝を数えるため)。
func automatonLongest(a []rune, alo, ahi int, b []rune, blo, bhi, limit int) (found Match, capped bool) {
	n := bhi - blo
	length := make([]int, 1, 2*n+1)
	link := make([]int, 1, 2*n+1)
	first := make([]int, 1, 2*n+1)
	trans := make([]map[rune]int, 1, 2*n+1)
	link[0], first[0], trans[0] = -1, -1, map[rune]int{}
	last := 0
	for pos := blo; pos < bhi; pos++ {
		c := b[pos]
		cur := len(length)
		length = append(length, length[last]+1)
		link = append(link, 0)
		trans = append(trans, map[rune]int{})
		first = append(first, pos)
		p := last
		for p != -1 {
			if _, ok := trans[p][c]; ok {
				break
			}
			trans[p][c] = cur
			p = link[p]
		}
		if p != -1 {
			q := trans[p][c]
			if length[p]+1 == length[q] {
				link[cur] = q
			} else {
				clone := len(length)
				length = append(length, length[p]+1)
				link = append(link, link[q])
				copied := make(map[rune]int, len(trans[q]))
				for k, v := range trans[q] {
					copied[k] = v
				}
				trans = append(trans, copied)
				first = append(first, first[q])
				for p != -1 {
					if t, ok := trans[p][c]; !ok || t != q {
						break
					}
					trans[p][c] = clone
					p = link[p]
				}
				link[q] = clone
				link[cur] = clone
			}
		}
		last = cur
	}
	state, size, best := 0, 0, 0
	bestEnd, bestState := -1, -1
	for pos := alo; pos < ahi; pos++ {
		c := a[pos]
		for state != 0 {
			if _, ok := trans[state][c]; ok {
				break
			}
			state = link[state]
			size = length[state]
		}
		target, ok := trans[state][c]
		if !ok {
			state, size = 0, 0
			continue
		}
		state = target
		size++
		if size > best {
			best, bestEnd, bestState = size, pos, state
			if best >= limit {
				capped = true
				break
			}
		}
	}
	if best == 0 {
		return Match{alo, blo, 0}, capped
	}
	return Match{bestEnd - best + 1, first[bestState] - best + 1, best}, capped
}

// gramIndex は b の長さ q の部分文字列を引く表 (本家の _gram_index)。各位置 j について、部分の hash の下位ビットと j を 1 つの整数にまとめ、小さい順に並べる。
// 同じ hash の位置は j の小さい順に続くので、範囲の中の位置を二分探索で引ける。hash は表を引く手がかりにだけ使い、引いた位置は必ず字で比べる。
type gramIndex struct {
	keys        []uint64
	shift, size int
	keep, low   uint64
}

func (f *exactFinder) newGramIndex(b []rune, q int) *gramIndex {
	shift := bitLength(len(b))
	g := &gramIndex{shift: shift, size: q, keep: 1<<(63-shift) - 1, low: 1<<shift - 1}
	hashes := f.rollingHashes(b, q)
	g.keys = make([]uint64, len(hashes))
	for j, h := range hashes {
		g.keys[j] = (h&g.keep)<<shift | uint64(j)
	}
	slices.Sort(g.keys)
	return g
}

func bitLength(n int) int {
	l := 0
	for n > 0 {
		l++
		n >>= 1
	}
	return l
}

// each は hash が h で、位置が [lo, hi] の中の j を小さい順に visit へ渡す (visit が false を返したら止める)。
func (g *gramIndex) each(h uint64, lo, hi int, visit func(j int) bool) {
	if hi < lo {
		return
	}
	base := (h & g.keep) << g.shift
	end := base | uint64(hi)
	for p := sort.Search(len(g.keys), func(k int) bool { return g.keys[k] >= base|uint64(lo) }); p < len(g.keys) && g.keys[p] <= end; p++ {
		if !visit(int(g.keys[p] & g.low)) {
			return
		}
	}
}

// first は hash が h で位置が [lo, hi] の中の j のうち、same が真になる最も前のもの。same が偽の位置 (hash だけが同じ) は collisions に数える。
func (g *gramIndex) first(h uint64, lo, hi int, same func(j int) bool, collisions *int) (int, bool) {
	found, ok := -1, false
	g.each(h, lo, hi, func(j int) bool {
		if same(j) {
			found, ok = j, true
			return false
		}
		*collisions++
		return true
	})
	return found, ok
}

func (f *exactFinder) rollingHashes(rs []rune, size int) []uint64 {
	hs := rollingHashes(rs, size)
	for k := range hs {
		hs[k] &= f.tune.hashMask
	}
	return hs
}

func (f *exactFinder) polyHash(rs []rune) uint64 { return polyHash(rs) & f.tune.hashMask }
