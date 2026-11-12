package pyunicode

import (
	"slices"
	"sort"
)

// Form は正規化の形式。
type Form int

// 正規化の形式 (Python の unicodedata.normalize の "NFC" "NFD" "NFKC" "NFKD")。
const (
	NFC Form = iota
	NFD
	NFKC
	NFKD
)

// ハングルの音節の合成・分解の算法 (Unicode 標準の 3.12)。
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// Normalize は Python の unicodedata.normalize(form, s) と同じ結果を返す。s は WTF-8 の文字列 (サロゲートも 1 字として読む)。
func Normalize(form Form, s string) string {
	if isASCII(s) {
		return s
	}
	compat := form == NFKC || form == NFKD
	buf := decompose(Runes(s), compat)
	reorder(buf)
	if form == NFC || form == NFKC {
		buf = compose(buf)
	}
	return FromRunes(buf)
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func decompose(in []rune, compat bool) []rune {
	table := canonicalDecomp[:]
	if compat {
		table = compatDecomp[:]
	}
	out := make([]rune, 0, len(in)+len(in)/2)
	for _, r := range in {
		switch {
		case r >= hangulSBase && r < hangulSBase+hangulSCount:
			idx := r - hangulSBase
			out = append(out, hangulLBase+idx/hangulNCount, hangulVBase+idx%hangulNCount/hangulTCount)
			if t := idx % hangulTCount; t != 0 {
				out = append(out, hangulTBase+t)
			}
		case r < 0xA0:
			out = append(out, r)
		default:
			i := sort.Search(len(table), func(i int) bool { return table[i].cp >= r })
			if i < len(table) && table[i].cp == r {
				out = appendRunes(out, table[i].to)
			} else {
				out = append(out, r)
			}
		}
	}
	return out
}

// appendRunes は表の文字列 (サロゲートを含まない UTF-8) を字に開いて足す。
func appendRunes(out []rune, s string) []rune {
	for _, r := range s {
		out = append(out, r)
	}
	return out
}

// reorder は結合クラスが 0 でない字の連なりを、クラスの昇順に安定に並べ替える (正準順序)。
func reorder(buf []rune) {
	for i := 0; i < len(buf); {
		if CombiningClass(buf[i]) == 0 {
			i++
			continue
		}
		j := i
		for j < len(buf) && CombiningClass(buf[j]) != 0 {
			j++
		}
		run := buf[i:j]
		slices.SortStableFunc(run, func(a, b rune) int { return int(CombiningClass(a)) - int(CombiningClass(b)) })
		i = j
	}
}

// compose は分解して並べ替えた列に主合成を当てる (Unicode 標準 UAX #15 の合成の算法)。
func compose(in []rune) []rune {
	if len(in) == 0 {
		return in
	}
	out := make([]rune, 0, len(in))
	starter := -1
	var lastClass uint8
	for _, r := range in {
		class := CombiningClass(r)
		if starter >= 0 && (lastClass < class || lastClass == 0) {
			if c, ok := composeTwo(out[starter], r); ok {
				out[starter] = c
				continue
			}
		}
		if class == 0 {
			starter = len(out)
		}
		lastClass = class
		out = append(out, r)
		if starter < 0 {
			starter = 0
			if class != 0 {
				lastClass = 255
			}
		}
	}
	return out
}

func composeTwo(first, second rune) (rune, bool) {
	switch {
	case first >= hangulLBase && first < hangulLBase+hangulLCount && second >= hangulVBase && second < hangulVBase+hangulVCount:
		return hangulSBase + ((first-hangulLBase)*hangulVCount+(second-hangulVBase))*hangulTCount, true
	case first >= hangulSBase && first < hangulSBase+hangulSCount && (first-hangulSBase)%hangulTCount == 0 &&
		second > hangulTBase && second < hangulTBase+hangulTCount:
		return first + (second - hangulTBase), true
	}
	i := sort.Search(len(composePairs), func(i int) bool {
		p := composePairs[i]
		return p.first > first || (p.first == first && p.second >= second)
	})
	if i < len(composePairs) && composePairs[i].first == first && composePairs[i].second == second {
		return composePairs[i].composite, true
	}
	return 0, false
}
