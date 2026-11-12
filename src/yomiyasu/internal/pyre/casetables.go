package pyre

import "sort"

// 表の中身は casetables_gen.go (Python の re から生成する)。

type (
	lowerPair struct{ x, lower rune }
	runeRange struct{ lo, hi rune }
	extraCase struct {
		lower  rune
		others []rune
	}
)

// lowerUnicode は _sre.unicode_tolower。
func lowerUnicode(r rune) rune {
	i := sort.Search(len(lowerPairs), func(i int) bool { return lowerPairs[i].x >= r })
	if i < len(lowerPairs) && lowerPairs[i].x == r {
		return lowerPairs[i].lower
	}
	return r
}

// isCasedUnicode は _sre.unicode_iscased。
func isCasedUnicode(r rune) bool {
	i := sort.Search(len(casedRanges), func(i int) bool { return casedRanges[i].hi >= r })
	return i < len(casedRanges) && casedRanges[i].lo <= r
}

func isASCIILetter(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }

// lowerASCII は _sre.ascii_tolower。
func lowerASCII(r rune) rune {
	if 'A' <= r && r <= 'Z' {
		return r + 32
	}
	return r
}

// caseRules は IGNORECASE の規則 (Unicode か ASCII か)。
type caseRules struct{ ascii bool }

func (c caseRules) lower(r rune) rune {
	if c.ascii {
		return lowerASCII(r)
	}
	return lowerUnicode(r)
}

func (c caseRules) isCased(r rune) bool {
	if c.ascii {
		return isASCIILetter(r)
	}
	return isCasedUnicode(r)
}

// changed は小文字化で別の字になる字 (昇順)。
func (c caseRules) changed() []lowerPair {
	if c.ascii {
		return asciiUpperPairs
	}
	return lowerPairs[:]
}

var asciiUpperPairs = func() []lowerPair {
	out := make([]lowerPair, 0, 26)
	for r := 'A'; r <= 'Z'; r++ {
		out = append(out, lowerPair{r, r + 32})
	}
	return out
}()
