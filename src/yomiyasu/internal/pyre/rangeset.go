package pyre

import (
	"slices"
	"sync"

	"yomiyasu/internal/pyunicode"
)

const maxRune = 0x10FFFF

// rset は字の集合を、昇順で重ならず隣り合わない範囲の列で持つ (空の集合は nil)。
type rset []runeRange

func normalize(rs []runeRange) rset {
	if len(rs) == 0 {
		return nil
	}
	rs = slices.Clone(rs)
	slices.SortFunc(rs, func(a, b runeRange) int { return int(a.lo - b.lo) })
	out := rset{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			last.hi = max(last.hi, r.hi)
			continue
		}
		out = append(out, r)
	}
	return out
}

func union(sets ...rset) rset {
	var all []runeRange
	for _, s := range sets {
		all = append(all, s...)
	}
	return normalize(all)
}

func single(r rune) rset { return rset{{r, r}} }

func rangeSet(lo, hi rune) rset { return rset{{lo, hi}} }

// complement は 0 から 0x10FFFF の中での補集合。
func complement(s rset) rset {
	var out rset
	next := rune(0)
	for _, r := range s {
		if r.lo > next {
			out = append(out, runeRange{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= maxRune {
		out = append(out, runeRange{next, maxRune})
	}
	return out
}

func (s rset) contains(r rune) bool {
	i, ok := slices.BinarySearchFunc(s, r, func(e runeRange, r rune) int {
		switch {
		case e.hi < r:
			return -1
		case e.lo > r:
			return 1
		}
		return 0
	})
	return ok && i < len(s)
}

// minus は a から b を引く。
func minus(a, b rset) rset {
	return intersect(a, complement(b))
}

func intersect(a, b rset) rset {
	var out rset
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		lo, hi := max(a[i].lo, b[j].lo), min(a[i].hi, b[j].hi)
		if lo <= hi {
			out = append(out, runeRange{lo, hi})
		}
		if a[i].hi < b[j].hi {
			i++
		} else {
			j++
		}
	}
	return out
}

// fromRanges は {Lo, Hi} の範囲の列 (pyunicode.Range・oracle.CPRange) を rset にする。
func fromRanges[T ~struct{ Lo, Hi rune }](rs []T) rset {
	out := make(rset, len(rs))
	for i, r := range rs {
		pair := struct{ Lo, Hi rune }(r)
		out[i] = runeRange{pair.Lo, pair.Hi}
	}
	return out
}

// \d \s \w の字の集合。ASCII の方は re.A のとき。
var (
	unicodeSetsOnce sync.Once
	unicodeDigit    rset
	unicodeSpace    rset
	unicodeWord     rset
)

var (
	asciiDigit = rangeSet('0', '9')
	asciiSpace = normalize([]runeRange{{9, 13}, {' ', ' '}})
	asciiWord  = normalize([]runeRange{{'0', '9'}, {'A', 'Z'}, {'a', 'z'}, {'_', '_'}})
)

func categorySet(cat category, ascii bool) rset {
	unicodeSetsOnce.Do(func() {
		unicodeDigit = fromRanges(pyunicode.DecimalRanges())
		unicodeSpace = fromRanges(pyunicode.SpaceRanges())
		unicodeWord = fromRanges(pyunicode.WordRanges())
	})
	var s rset
	switch cat {
	case catDigit, catNotDigit:
		s = unicodeDigit
		if ascii {
			s = asciiDigit
		}
	case catSpace, catNotSpace:
		s = unicodeSpace
		if ascii {
			s = asciiSpace
		}
	case catWord, catNotWord:
		s = unicodeWord
		if ascii {
			s = asciiWord
		}
	}
	if cat == catNotDigit || cat == catNotSpace || cat == catNotWord {
		return complement(s)
	}
	return s
}

// foldPreimage は「小文字にした字が s に入る字」の集合を返す (re.I の IN_UNI_IGNORE が、照合する字を小文字にしてから集合に当てる規則)。
func (c caseRules) foldPreimage(s rset) rset {
	pairs := c.changed()
	points := make([]runeRange, 0, len(pairs))
	for _, p := range pairs {
		points = append(points, runeRange{p.x, p.x})
	}
	out := minus(s, normalize(points))
	var add []runeRange
	for _, p := range pairs {
		if s.contains(p.lower) {
			add = append(add, runeRange{p.x, p.x})
		}
	}
	return union(out, normalize(add))
}

// lowerImage は範囲 [lo, hi] の各字を小文字にした字の集合に、同じ大文字を持つ別の小文字を足したもの
// (_optimize_charset が文字の集合の範囲を charmap に写す規則)。
func (c caseRules) lowerImage(lo, hi rune) rset {
	r := rangeSet(lo, hi)
	var changed, lowered []runeRange
	for _, p := range c.changed() {
		if lo <= p.x && p.x <= hi {
			changed = append(changed, runeRange{p.x, p.x})
			lowered = append(lowered, runeRange{p.lower, p.lower})
		}
	}
	image := union(minus(r, normalize(changed)), normalize(lowered))
	return c.withExtras(image)
}

// withExtras は集合の字に対応する別の小文字を足す。
func (c caseRules) withExtras(s rset) rset {
	if c.ascii {
		return s
	}
	var add []runeRange
	for _, e := range extraCases {
		if s.contains(e.lower) {
			for _, o := range e.others {
				add = append(add, runeRange{o, o})
			}
		}
	}
	return union(s, normalize(add))
}

// literalSet は IGNORECASE のときの単独の字 (集合の外) が当たる字の集合。
func (c caseRules) literalSet(r rune) rset {
	if !c.isCased(r) {
		return single(r)
	}
	lo := c.lower(r)
	return c.foldPreimage(c.withExtras(single(lo)))
}

// classSet は文字の集合 [...] が当たる字の集合。ignore のときは re.I の規則 (小文字にしてから照合し、字に大小があるときだけ小文字化する) を写す。
func classSet(items []setItem, negate, ignore bool, rules caseRules) rset {
	ascii := rules.ascii
	if !ignore {
		var parts []rset
		for _, it := range items {
			parts = append(parts, itemSet(it, ascii))
		}
		s := union(parts...)
		if negate {
			return complement(s)
		}
		return s
	}
	var parts []rset
	hascased := false
	for _, it := range items {
		switch it.kind {
		case itemLiteral:
			lo := rules.lower(it.lo)
			parts = append(parts, rules.withExtras(single(lo)))
			hascased = hascased || rules.isCased(lo)
		case itemRange:
			if it.lo <= 0xFFFF {
				parts = append(parts, rules.lowerImage(it.lo, min(it.hi, 0xFFFF)))
			}
			if it.hi > 0xFFFF {
				// 0xFFFF を超える字の範囲は、字を小文字にした値か、それを大文字にした値が範囲に入るかで照合する (RANGE_UNI_IGNORE)。
				// 大文字にする表は無いので、範囲の中で小文字化の対象の字の小文字化先を、大文字化の逆写像とみなす (0xFFFF を超える字では成り立つ)
				parts = append(parts, rangeSet(it.lo, it.hi), upperPreimage(it.lo, it.hi, rules))
				hascased = true
			} else if rules.anyCased(it.lo, it.hi) {
				hascased = true
			}
		case itemCategory:
			parts = append(parts, categorySet(it.cat, ascii))
		}
	}
	s := union(parts...)
	if negate {
		s = complement(s)
	}
	if hascased {
		return rules.foldPreimage(s)
	}
	return s
}

func upperPreimage(lo, hi rune, rules caseRules) rset {
	var out []runeRange
	for _, p := range rules.changed() {
		if lo <= p.x && p.x <= hi {
			out = append(out, runeRange{p.lower, p.lower})
		}
	}
	return normalize(out)
}

func (c caseRules) anyCased(lo, hi rune) bool {
	if c.ascii {
		return lo <= 'Z' && hi >= 'A' || lo <= 'z' && hi >= 'a'
	}
	i, _ := slices.BinarySearchFunc(casedRanges[:], lo, func(e runeRange, r rune) int { return int(e.hi - r) })
	for ; i < len(casedRanges) && casedRanges[i].lo <= hi; i++ {
		if casedRanges[i].hi >= lo {
			return true
		}
	}
	return false
}

func itemSet(it setItem, ascii bool) rset {
	switch it.kind {
	case itemLiteral:
		return single(it.lo)
	case itemRange:
		return rangeSet(it.lo, it.hi)
	default:
		return categorySet(it.cat, ascii)
	}
}
