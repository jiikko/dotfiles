// Package difflib は Python 3.15.0 の difflib.SequenceMatcher と、本家 yomiyasu の速い照合 (yomiyasu_diff.py の _ExactSequenceMatcher) を Go で再現する。
//
// 本家は照合を全部 SequenceMatcher(None, a, b, autojunk=False) か _ExactSequenceMatcher(None, a, b, autojunk=False) で作る (isjunk を渡さず、autojunk は常に False)。
// New と NewExact はその形だけを持つ。isjunk と autojunk の引数を写さないのは、本家が使わず、記録で確かめられない経路を増やすため。
// 本家が使い始めたら、記録の driver (oracle/py/record_difflib.py) が本家のソースの呼び出しの形を ast で見て止まる。
package difflib

import (
	"fmt"
	"slices"
	"sort"
)

// Match は a[A:A+Size] == b[B:B+Size] の一致 (Python の difflib.Match)。
type Match struct{ A, B, Size int }

// OpCode は get_opcodes の 1 つ (Tag は "replace" "delete" "insert" "equal")。a[I1:I2] を b[J1:J2] にする。
type OpCode struct {
	Tag            string
	I1, I2, J1, J2 int
}

// SequenceMatcher は difflib.SequenceMatcher(None, a, b, autojunk=False) と同じ値を返す。
// 文字列は 1 コードポイントを 1 要素にした []rune で渡す (Python の str の添字と同じ。単独のサロゲートも 1 要素)。
type SequenceMatcher[T comparable] struct {
	a, b           []T
	b2j            map[T][]int
	fullbcount     map[T]int
	matchingBlocks []Match
	opcodes        []OpCode
	// fast は NewExact が作ったときだけ持つ (本家の _ExactSequenceMatcher の求め方)。
	fast *exactFinder
	// j2len は DP の行 (find_longest_match の j2len と newj2len) の使い回しの置き場。
	j2len [2][]int
}

// New は difflib.SequenceMatcher(None, a, b, autojunk=False)。
func New[T comparable](a, b []T) *SequenceMatcher[T] {
	m := &SequenceMatcher[T]{}
	m.SetSeqs(a, b)
	return m
}

// SetSeqs は set_seqs(a, b)。
func (m *SequenceMatcher[T]) SetSeqs(a, b []T) {
	m.SetSeq1(a)
	m.SetSeq2(b)
}

// SetSeq1 は set_seq1(a)。比べる側 b から作る表は残す。
func (m *SequenceMatcher[T]) SetSeq1(a []T) {
	m.a = a
	m.matchingBlocks, m.opcodes = nil, nil
	if m.fast != nil {
		m.fast.resetA()
	}
}

// SetSeq2 は set_seq2(b)。
func (m *SequenceMatcher[T]) SetSeq2(b []T) {
	m.b = b
	m.matchingBlocks, m.opcodes = nil, nil
	m.fullbcount = nil
	m.b2j = make(map[T][]int)
	for i, elt := range b {
		m.b2j[elt] = append(m.b2j[elt], i)
	}
	if m.fast != nil {
		m.fast.resetB()
	}
}

// FindLongestMatch は find_longest_match(alo, ahi, blo, bhi)。範囲は並びの中 (0 <= alo <= ahi <= len(a)、b も同じ) でなければ panic する。
// 本家は範囲の外を渡さない (Python の difflib は範囲の外でも値を返すが、その振る舞いは写していない)。
func (m *SequenceMatcher[T]) FindLongestMatch(alo, ahi, blo, bhi int) Match {
	if alo < 0 || alo > ahi || ahi > len(m.a) || blo < 0 || blo > bhi || bhi > len(m.b) {
		panic(fmt.Sprintf("difflib: FindLongestMatch の範囲が並びの外です (a[%d:%d] / %d、b[%d:%d] / %d)", alo, ahi, len(m.a), blo, bhi, len(m.b)))
	}
	if m.fast != nil {
		return m.fast.longest(alo, ahi, blo, bhi, min(ahi-alo, bhi-blo))
	}
	return m.findLongest(alo, ahi, blo, bhi)
}

// findLongest は Python の find_longest_match の本体 (捨てる要素が無いときの DP と、一致を前後に広げる処理)。
func (m *SequenceMatcher[T]) findLongest(alo, ahi, blo, bhi int) Match {
	best := m.dp(alo, ahi, blo, bhi)
	besti, bestj, bestsize := best.A, best.B, best.Size
	// 捨てる要素が無いので、広げる処理は Python の 4 つの while のうち前の 2 つだけが動きうる (後の 2 つは isbjunk が常に偽)。
	// DP の答えはいちばん長いので、ここで広がることは無いが、Python と同じ手順を残す。
	for besti > alo && bestj > blo && m.a[besti-1] == m.b[bestj-1] {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	for besti+bestsize < ahi && bestj+bestsize < bhi && m.a[besti+bestsize] == m.b[bestj+bestsize] {
		bestsize++
	}
	return Match{besti, bestj, bestsize}
}

// dp は find_longest_match の DP。a[i] と同じ b の位置 (b2j) のうち [blo, bhi) の中だけをたどり、i の小さい順、同じ i では j の小さい順に見て、
// 今までの最大を超えたときだけ記録する (いちばん長く、a で最も前、同じ a なら b で最も前)。本家の _dp も同じ値を求める。
func (m *SequenceMatcher[T]) dp(alo, ahi, blo, bhi int) Match {
	besti, bestj, bestsize := alo, blo, 0
	width := bhi - blo + 1
	// 2 つの行は呼び出しの間ずっと全部 0 に保つ (書いた所だけを 0 に戻す)。範囲の幅ごとに 0 で埋め直すと、範囲を分けるたびに b の長さの手間がかかる。
	for k := range m.j2len {
		if len(m.j2len[k]) < width {
			m.j2len[k] = make([]int, width)
		}
	}
	// prev[x] は j2len[blo+x-1] (Python の j2len.get(j-1, 0) を prev[j-blo] で引く)。
	prev, cur := m.j2len[0], m.j2len[1]
	var prevTouched, curTouched []int
	for i := alo; i < ahi; i++ {
		positions := m.b2j[m.a[i]]
		for _, j := range positions[sort.SearchInts(positions, blo):] {
			if j >= bhi {
				break
			}
			k := prev[j-blo] + 1
			cur[j-blo+1] = k
			curTouched = append(curTouched, j-blo+1)
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		for _, x := range prevTouched {
			prev[x] = 0
		}
		prev, cur = cur, prev
		prevTouched, curTouched = curTouched, prevTouched[:0]
	}
	for _, x := range prevTouched {
		prev[x] = 0
	}
	return Match{besti, bestj, bestsize}
}

type span struct{ alo, ahi, blo, bhi, bound int }

// GetMatchingBlocks は get_matching_blocks。最後に (len(a), len(b), 0) を持つ。返した slice は matcher が持ち続けるので書き換えない。
func (m *SequenceMatcher[T]) GetMatchingBlocks() []Match {
	if m.matchingBlocks != nil {
		return m.matchingBlocks
	}
	la, lb := len(m.a), len(m.b)
	// Python と同じく、範囲を後入れ先出しで分ける。bound は本家の _ExactSequenceMatcher が分けた範囲に渡す答えの長さの上限
	// (左の範囲は k-1、右の範囲は k。証明は本家の docstring)。difflib の求め方では使わない。
	queue := []span{{0, la, 0, lb, min(la, lb)}}
	var blocks []Match
	for len(queue) > 0 {
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		var x Match
		if m.fast != nil {
			x = m.fast.longest(s.alo, s.ahi, s.blo, s.bhi, s.bound)
		} else {
			x = m.findLongest(s.alo, s.ahi, s.blo, s.bhi)
		}
		i, j, k := x.A, x.B, x.Size
		if k > 0 {
			blocks = append(blocks, x)
			if s.alo < i && s.blo < j {
				queue = append(queue, span{s.alo, i, s.blo, j, k - 1})
			}
			if i+k < s.ahi && j+k < s.bhi {
				queue = append(queue, span{i + k, s.ahi, j + k, s.bhi, k})
			}
		}
	}
	slices.SortFunc(blocks, func(x, y Match) int {
		if x.A != y.A {
			return x.A - y.A
		}
		if x.B != y.B {
			return x.B - y.B
		}
		return x.Size - y.Size
	})
	// 隣り合う一致をつなぐ処理。捨てる要素が無いと隣り合う一致は生まれない (隣り合うなら、親の範囲の答えをもう 1 字伸ばせて「いちばん長い」に反する)。
	// この処理を外す変異は試験で緑のまま通る (等価)。Python と同じ手順を残すのは、isjunk / autojunk を写すことになったときに要るため。
	i1, j1, k1 := 0, 0, 0
	nonAdjacent := []Match{}
	for _, blk := range blocks {
		if i1+k1 == blk.A && j1+k1 == blk.B {
			k1 += blk.Size
		} else {
			if k1 > 0 {
				nonAdjacent = append(nonAdjacent, Match{i1, j1, k1})
			}
			i1, j1, k1 = blk.A, blk.B, blk.Size
		}
	}
	if k1 > 0 {
		nonAdjacent = append(nonAdjacent, Match{i1, j1, k1})
	}
	m.matchingBlocks = append(nonAdjacent, Match{la, lb, 0})
	return m.matchingBlocks
}

// GetOpcodes は get_opcodes。返した slice は matcher が持ち続けるので書き換えない。
func (m *SequenceMatcher[T]) GetOpcodes() []OpCode {
	if m.opcodes != nil {
		return m.opcodes
	}
	i, j := 0, 0
	answer := []OpCode{}
	for _, blk := range m.GetMatchingBlocks() {
		ai, bj, size := blk.A, blk.B, blk.Size
		tag := ""
		switch {
		case i < ai && j < bj:
			tag = "replace"
		case i < ai:
			tag = "delete"
		case j < bj:
			tag = "insert"
		}
		if tag != "" {
			answer = append(answer, OpCode{tag, i, ai, j, bj})
		}
		i, j = ai+size, bj+size
		if size > 0 {
			answer = append(answer, OpCode{"equal", ai, i, bj, j})
		}
	}
	m.opcodes = answer
	return answer
}

// calculateRatio は difflib._calculate_ratio。
func calculateRatio(matches, length int) float64 {
	if length > 0 {
		return 2.0 * float64(matches) / float64(length)
	}
	return 1.0
}

// Ratio は ratio。
func (m *SequenceMatcher[T]) Ratio() float64 {
	matches := 0
	for _, blk := range m.GetMatchingBlocks() {
		matches += blk.Size
	}
	return calculateRatio(matches, len(m.a)+len(m.b))
}

// QuickRatio は quick_ratio (ratio の上限。要素の多重集合の重なり)。
func (m *SequenceMatcher[T]) QuickRatio() float64 {
	if m.fullbcount == nil {
		m.fullbcount = make(map[T]int, len(m.b2j))
		for _, elt := range m.b {
			m.fullbcount[elt]++
		}
	}
	avail := make(map[T]int)
	matches := 0
	for _, elt := range m.a {
		numb, ok := avail[elt]
		if !ok {
			numb = m.fullbcount[elt]
		}
		avail[elt] = numb - 1
		if numb > 0 {
			matches++
		}
	}
	return calculateRatio(matches, len(m.a)+len(m.b))
}

// RealQuickRatio は real_quick_ratio (quick_ratio の上限)。
func (m *SequenceMatcher[T]) RealQuickRatio() float64 {
	la, lb := len(m.a), len(m.b)
	return calculateRatio(min(la, lb), la+lb)
}
