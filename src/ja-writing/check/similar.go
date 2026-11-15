package check

import (
	"ja-writing/internal/pyunicode"
)

// SimilarToSource は資料を原文とみなすしきい値。似かたは、NFKC にして空白を除いた文字列の 2 字組 (隣り合う 2 文字) の重なり。
// 重なった 2 字組の数を最終稿の 2 字組の数で割った値と、資料の 2 字組の数で割った値の小さいほう (片側だけで決めると、大きな資料に短い最終稿の
// 2 字組がほとんど含まれてしまう)。本家は tests/corpus の推敲の 48 組 (最小 0.577) と、正しい新規作成の組 (最大 0.495) で測って決めた。
const SimilarToSource = 0.53

// Compact は _compact。資料と最終稿の似かたを求めるときと、同じ本文かを比べるときの下ごしらえ。NFKC にし、空白 (改行を含む) と書式の文字
// (一般カテゴリ Cf。ゼロ幅の空白・ソフトハイフン・BOM など) を除く。文字の間に書式の文字を挟んでも似かたが 0 にならないようにするため。
func Compact(text string) string {
	var out []rune
	for _, r := range pyunicode.Runes(pyunicode.Normalize(pyunicode.NFKC, text)) {
		if !pyunicode.IsSpace(r) && pyunicode.Category(r) != "Cf" {
			out = append(out, r)
		}
	}
	return pyunicode.FromRunes(out)
}

type bigram [2]rune

// bigramCounts は _bigram_counts。_compact の文字列の 2 字組を数える (compacted なら text はもう Compact したもの)。
func bigramCounts(text string, compacted bool) (counts map[bigram]int, total int) {
	if !compacted {
		text = Compact(text)
	}
	rs := pyunicode.Runes(text)
	counts = map[bigram]int{}
	for i := 0; i+1 < len(rs); i++ {
		counts[bigram{rs[i], rs[i+1]}]++
	}
	return counts, max(len(rs)-1, 0)
}

// similarityOfCounts は _similarity_of_counts。
func similarityOfCounts(material map[bigram]int, totalMaterial int, final map[bigram]int, totalFinal int) float64 {
	if totalMaterial == 0 || totalFinal == 0 {
		return 0
	}
	common := 0
	for key, n := range material {
		common += min(n, final[key])
	}
	return min(float64(common)/float64(totalMaterial), float64(common)/float64(totalFinal))
}

// Similarity は _similarity。資料と最終稿の似かた (0 から 1)。
func Similarity(material, final string) float64 {
	m, tm := bigramCounts(material, false)
	f, tf := bigramCounts(final, false)
	return similarityOfCounts(m, tm, f, tf)
}

// SourceLike は _source_like。資料ごとの似かたの並びと、原文とみなす資料の番号 (1 から。しきい値以上でいちばん似ているもの。同じなら若い番号。なければ 0) を返す。
// compacted なら、渡す文字列はもう Compact したもの (run が、同じ本文かを比べるときと同じ文字列を使い回す)。
func SourceLike(materialTexts []string, finalText string, compacted bool) (best int, values []float64) {
	final, totalFinal := bigramCounts(finalText, compacted)
	values = make([]float64, len(materialTexts))
	for i, text := range materialTexts {
		m, tm := bigramCounts(text, compacted)
		values[i] = similarityOfCounts(m, tm, final, totalFinal)
		if values[i] >= SimilarToSource && (best == 0 || values[i] > values[best-1]) {
			best = i + 1
		}
	}
	return best, values
}
