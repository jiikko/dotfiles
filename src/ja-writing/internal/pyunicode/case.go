package pyunicode

import "sort"

// 大文字小文字の変換。表は Python の str.casefold・lower・swapcase を全コードポイントについて読んだもの (case_gen.go。oracle/casemap.go が作る)。
// Go の strings.ToLower・unicode.SimpleFold は Unicode 15.0.0 の 1 字 1 字の対応で、ß→ss のような複数字への展開と、
// Unicode 16 / 17 で増えた字を持たない。

const capitalSigma = 0x3A3

func caseLookup(table []decomposition, r rune) (string, bool) {
	i := sort.Search(len(table), func(i int) bool { return table[i].cp >= r })
	if i < len(table) && table[i].cp == r {
		return table[i].to, true
	}
	return "", false
}

// sigma は rs[i] (Σ) を小文字にした字。CPython の handle_capital_sigma と同じく、
// \p{cased}\p{case-ignorable}*Σ(?!\p{case-ignorable}*\p{cased}) のときだけ語末の ς、そうでなければ σ。
func sigma(rs []rune, i int) string {
	j := i - 1
	for ; j >= 0 && inRanges(caseIgnorableRanges[:], rs[j]); j-- {
	}
	final := j >= 0 && inRanges(casedRanges[:], rs[j])
	if final {
		for j = i + 1; j < len(rs) && inRanges(caseIgnorableRanges[:], rs[j]); j++ {
		}
		final = j == len(rs) || !inRanges(casedRanges[:], rs[j])
	}
	if final {
		return "ς"
	}
	return "σ"
}

// mapRunes は s の 1 字ずつを table で置き換える。Σ だけは前後の字を見る (lower と swapcase が使う規則)。
func mapRunes(s string, table []decomposition, sigmaRule bool) string {
	rs := Runes(s)
	b := make([]byte, 0, len(s))
	for i, r := range rs {
		if sigmaRule && r == capitalSigma {
			b = append(b, sigma(rs, i)...)
		} else if to, ok := caseLookup(table, r); ok {
			b = append(b, to...)
		} else {
			b = AppendRune(b, r)
		}
	}
	return string(b)
}

// Casefold は Python の str.casefold()。
func Casefold(s string) string { return mapRunes(s, casefoldTable[:], false) }

// Lower は Python の str.lower() (語末の Σ は ς)。
func Lower(s string) string { return mapRunes(s, lowerTable[:], true) }

// Swapcase は Python の str.swapcase()。
func Swapcase(s string) string { return mapRunes(s, swapcaseTable[:], true) }
