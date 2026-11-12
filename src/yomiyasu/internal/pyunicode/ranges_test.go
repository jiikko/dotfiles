package pyunicode

import "testing"

func inRangeList(rs []Range, r rune) bool {
	for _, x := range rs {
		if x.Lo <= r && r <= x.Hi {
			return true
		}
	}
	return false
}

// 範囲の一覧が、1 字ごとの判定 (記録と照合済み) と全コードポイントで同じこと。
func TestRangesAgreeWithPredicates(t *testing.T) {
	space, decimal, word := SpaceRanges(), DecimalRanges(), WordRanges()
	for r := rune(0); r <= MaxRune; r++ {
		if inRangeList(space, r) != IsSpace(r) || inRangeList(decimal, r) != IsDecimal(r) || inRangeList(word, r) != (IsAlnum(r) || r == '_') {
			t.Fatalf("U+%04X: 範囲の一覧と判定が違う", r)
		}
	}
	for i := 1; i < len(word); i++ {
		if word[i].Lo <= word[i-1].Hi+1 {
			t.Fatalf("WordRanges が昇順で隣り合わない形になっていない: %v %v", word[i-1], word[i])
		}
	}
}
