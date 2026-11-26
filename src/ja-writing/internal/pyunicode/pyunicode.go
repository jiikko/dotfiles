// Package pyunicode は Python 3.15.0 (unicodedata 17.0.0) の文字の性質を Go で再現する。
//
// Go 1.26 の unicode と golang.org/x/text の norm は Unicode 15.0.0 なので、Unicode 16 / 17 で増えた字の
// 一般カテゴリ・数字の扱い・結合クラス・合成が Python と違う。そのため表は Python の unicodedata を全コードポイントについて読んで生成し
// (oracle/py/gen_unicode.py と oracle/tablegen.go。make golden で作り直す)、標準ライブラリの表は使わない。
//
// 文字列は Python の str と同じく「コードポイントの列」として扱う。Go の string には単独のサロゲート (JSON の "\ud800" など) を
// 入れられないので、サロゲートは WTF-8 (3 バイトの UTF-8 の形のまま) で持つ。数え方・読み方は DecodeRune と AppendRune にそろえる。
package pyunicode

import "sort"

type (
	categoryRun struct {
		start rune
		index uint8
	}
	runeRange    struct{ lo, hi rune }
	combiningRun struct {
		lo, hi rune
		class  uint8
	}
	decomposition struct {
		cp rune
		to string
	}
	composePair struct{ first, second, composite rune }
)

// MaxRune は最大のコードポイント。
const MaxRune = 0x10FFFF

// Category は Python の unicodedata.category(chr(r)) と同じ 2 文字の一般カテゴリ ("Lu"・"Nd"・割り当てのない字は "Cn"・サロゲートは "Cs")。
// 範囲外の値は "Cn" を返す。
func Category(r rune) string {
	if r < 0 || r > MaxRune {
		return "Cn"
	}
	i := sort.Search(len(categoryRuns), func(i int) bool { return categoryRuns[i].start > r }) - 1
	return categoryNames[categoryRuns[i].index]
}

// lint は 1 字ごとに呼ぶので、sort.Search (比べるたびに closure を呼ぶ) を使わずに探す。表の両端の外は探さずに返す
// (空白・改行の表は仮名や漢字より前で終わる)。DecimalValue と添字を返す関数に分けると、この関数が IsSpace などの呼び出し元へ
// インライン展開されなくなり、lint-len-1024k で 1 回 473 ms → 490〜515 ms に遅くなった。分けるなら展開されることを go build -gcflags=-m で確かめる。
func inRanges(rs []runeRange, r rune) bool {
	if len(rs) == 0 || r < rs[0].lo || r > rs[len(rs)-1].hi {
		return false
	}
	lo, hi := 0, len(rs)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if rs[m].hi < r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return rs[lo].lo <= r
}

// IsSpace は Python の chr(r).isspace()。
func IsSpace(r rune) bool { return inRanges(spaceRanges[:], r) }

// IsAlpha は Python の chr(r).isalpha() (一般カテゴリが L で始まる字)。
func IsAlpha(r rune) bool { return inRanges(alphaRanges[:], r) }

// IsDecimal は Python の chr(r).isdecimal()。re の \d はこれと同じ字に当たる。
func IsDecimal(r rune) bool { return inRanges(decimalRanges[:], r) }

// IsDigit は Python の chr(r).isdigit()。
func IsDigit(r rune) bool { return inRanges(digitRanges[:], r) }

// IsNumeric は Python の chr(r).isnumeric()。
func IsNumeric(r rune) bool { return inRanges(numericRanges[:], r) }

// IsIdentStart は Python の chr(r).isidentifier() (識別子の先頭に置ける字。'_' を含む)。
func IsIdentStart(r rune) bool { return inRanges(identStartRanges[:], r) }

// IsIdentContinue は Python の ("a" + chr(r)).isidentifier() (識別子の 2 字目以降に置ける字)。
func IsIdentContinue(r rune) bool { return inRanges(identContinueRanges[:], r) }

// IsAlnum は Python の chr(r).isalnum() (isalpha・isdecimal・isdigit・isnumeric のどれか)。re の \w は、これに '_' を足した字に当たる。
func IsAlnum(r rune) bool { return IsAlpha(r) || IsDecimal(r) || IsDigit(r) || IsNumeric(r) }

// IsPrintable は Python の chr(r).isprintable() (repr が escape せずに出す字)。
// CPython は一般カテゴリが Cc Cf Cs Co Cn Zl Zp Zs の字を印字できないとし、ASCII の空白 ' ' だけを例外にする。
// 全コードポイントで記録 (unicode.json の printable) と同じになることは golden_test.go が見る。
func IsPrintable(r rune) bool {
	if r == ' ' {
		return true
	}
	switch Category(r) {
	case "Cc", "Cf", "Cs", "Co", "Cn", "Zl", "Zp", "Zs":
		return false
	}
	return true
}

// IsLineBreak は str.splitlines が行の終わりとみなす 1 字 (\n \r \v \f \x1c \x1d \x1e \x85 U+2028 U+2029)。
// \x1f は isspace だが区切りではない。\r\n の組は SplitLines (pystr) が 1 つの区切りにする。
func IsLineBreak(r rune) bool { return inRanges(linebreakRanges[:], r) }

// CombiningClass は Python の unicodedata.combining(chr(r)) (正準結合クラス。0 は結合しない字)。
func CombiningClass(r rune) uint8 {
	i := sort.Search(len(combiningRuns), func(i int) bool { return combiningRuns[i].hi >= r })
	if i < len(combiningRuns) && combiningRuns[i].lo <= r {
		return combiningRuns[i].class
	}
	return 0
}

// DecimalValue は Python の unicodedata.decimal(chr(r)) (IsDecimal の字の数字としての値 0〜9)。
// Unicode は十進数字を 0 から 9 まで連続した 10 字の並びで割り当てるので、範囲の先頭 (0 の字) からの距離を 10 で割った余りになる。
// 全コードポイントで記録 (casemap.json の decimal) と同じになることは case_test.go が見る。
func DecimalValue(r rune) (int, bool) {
	i := sort.Search(len(decimalRanges), func(i int) bool { return decimalRanges[i].hi >= r })
	if i < len(decimalRanges) && decimalRanges[i].lo <= r {
		return int(r-decimalRanges[i].lo) % 10, true
	}
	return 0, false
}

// lookupDecomp は cp の昇順の表から r の行を二分探索で引く (正規化の分解表と、大文字小文字の表が使う)。
func lookupDecomp(table []decomposition, r rune) (string, bool) {
	i := sort.Search(len(table), func(i int) bool { return table[i].cp >= r })
	if i < len(table) && table[i].cp == r {
		return table[i].to, true
	}
	return "", false
}
