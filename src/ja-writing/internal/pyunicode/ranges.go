package pyunicode

import "slices"

// Range は両端を含むコードポイントの範囲。
type Range struct{ Lo, Hi rune }

func exportRanges(rs []runeRange) []Range {
	out := make([]Range, len(rs))
	for i, r := range rs {
		out[i] = Range{r.lo, r.hi}
	}
	return out
}

// SpaceRanges は IsSpace が真になる字の範囲 (昇順・重ならない)。
func SpaceRanges() []Range { return exportRanges(spaceRanges[:]) }

// DecimalRanges は IsDecimal が真になる字の範囲 (昇順・重ならない)。
func DecimalRanges() []Range { return exportRanges(decimalRanges[:]) }

// WordRanges は IsAlnum が真になる字と '_' の範囲 (昇順・重ならない)。re の Unicode の \w の字の集合。
func WordRanges() []Range {
	var all []Range
	for _, rs := range [][]runeRange{alphaRanges[:], decimalRanges[:], digitRanges[:], numericRanges[:], {{'_', '_'}}} {
		all = append(all, exportRanges(rs)...)
	}
	slices.SortFunc(all, func(a, b Range) int { return int(a.Lo - b.Lo) })
	merged := all[:1]
	for _, r := range all[1:] {
		last := &merged[len(merged)-1]
		if r.Lo <= last.Hi+1 {
			last.Hi = max(last.Hi, r.Hi)
			continue
		}
		merged = append(merged, r)
	}
	return merged
}
