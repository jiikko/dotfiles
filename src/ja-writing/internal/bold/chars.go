// Package bold は yomiyasu v3.1.0 の yomiyasu_lint.py の「太字が表示されるか」(2076-2836 行) を Go で作り直したもの。
// 本家の yomiyasu_diff.py も同じ関数を自分で持つ (1923-2685 行。型注釈・変数名・正規表現を module で compile するかだけが違う)。lint と diff はこの package を共有する。
//
// 位置は全部 Python の str の添字と同じコードポイントの添字 (rune の添字)。本家は文の端の外の字を空の文字列 "" で表す。ここでは noChar (-1) で表す。
// unicode_whitespace("") は真、_bold_punct_*("") は偽で、この 2 つの答えを noChar でも同じにする。
package bold

import (
	"strings"

	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// noChar は本家の空の文字列 (文の端の外・空の部分文字列の先頭の字)。
const noChar rune = -1

// profile は 2 つの表示の規則。gfm は GitHub の GFM (ASCII の記号と Unicode の P を句読点とする)、common は CommonMark (Unicode の P と S)。
// 本家は関数 _bold_punct_gfm と _bold_punct_new で区別し、回す順は (gfm, common)。
type profile int

const (
	gfm profile = iota
	common
)

var profiles = [2]profile{gfm, common}

const asciiPunct = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

// brackets は BOLD_BRACKETS (開きかっこ → 閉じかっこ)。
var brackets = map[rune]rune{
	'「': '」', '『': '』', '（': '）', '(': ')', '【': '】', '〔': '〕', '［': '］', '[': ']',
	'〈': '〉', '《': '》', '“': '”', '‘': '’', '＜': '＞',
}

// isWS は _bold_ws (unicode_whitespace)。字が無いときは真。
func isWS(c rune) bool {
	if c == noChar {
		return true
	}
	return c == '\t' || c == '\n' || c == '\r' || c == '\f' || pyunicode.Category(c) == "Zs"
}

// isPunct は _bold_punct_gfm / _bold_punct_new。字が無いときは偽。
func isPunct(p profile, c rune) bool {
	if c == noChar {
		return false
	}
	cat := pyunicode.Category(c)
	if p == gfm {
		return (c < 0x80 && strings.ContainsRune(asciiPunct, c)) || cat[0] == 'P'
	}
	return cat[0] == 'P' || cat[0] == 'S'
}

// charAt は text[pos] (範囲の外は noChar)。
func charAt(text []rune, pos int) rune {
	if pos < 0 || pos >= len(text) {
		return noChar
	}
	return text[pos]
}

// firstChar・lastChar は s[:1] と s[-1:] (空なら noChar)。
func firstChar(s []rune) rune {
	if len(s) == 0 {
		return noChar
	}
	return s[0]
}

func lastChar(s []rune) rune {
	if len(s) == 0 {
		return noChar
	}
	return s[len(s)-1]
}

// rolesAround は _roles の判定を、印の左の字 prev と右の字 next から出す (開けるか, 閉じられるか)。
func rolesAround(prev, next rune, p profile) (canOpen, canClose bool) {
	canOpen = !isWS(next) && (!isPunct(p, next) || isWS(prev) || isPunct(p, prev))
	canClose = !isWS(prev) && (!isPunct(p, prev) || isWS(next) || isPunct(p, next))
	return canOpen, canClose
}

// roles は _roles (text[start:end] の印)。
func roles(text []rune, start, end int, p profile) (canOpen, canClose bool) {
	prev := noChar
	if start > 0 {
		prev = text[start-1]
	}
	return rolesAround(prev, charAt(text, end), p)
}

// canOpenBoth・canCloseBoth は _bold_can_open / _bold_can_close (2 つの規則の両方で開ける / 閉じられるか)。
func canOpenBoth(prev, next rune) bool {
	for _, p := range profiles {
		if o, _ := rolesAround(prev, next, p); !o {
			return false
		}
	}
	return true
}

func canCloseBoth(prev, next rune) bool {
	for _, p := range profiles {
		if _, c := rolesAround(prev, next, p); !c {
			return false
		}
	}
	return true
}

// starExtent は _star_extent (position を含む * の連なり。左へは escape されていない * だけを伸ばす)。
func starExtent(text []rune, position int) (left, right int) {
	left = position
	for left > 0 && text[left-1] == '*' && !visibility.Escaped(text, left-1) {
		left--
	}
	right = position
	for right < len(text) && text[right] == '*' {
		right++
	}
	return left, right
}

// runsFromMasks は _runs_from_masks (re.finditer(r"\*+") の連なりから、伏せた範囲 masked を除いたもの)。masked は並んでいて重ならない。
func runsFromMasks(text []rune, masked [][2]int) [][2]int {
	var runs [][2]int
	maskIndex := 0
	for k := 0; k < len(text); {
		if text[k] != '*' {
			k++
			continue
		}
		finish := k
		for finish < len(text) && text[finish] == '*' {
			finish++
		}
		position := k
		for position < finish {
			for maskIndex < len(masked) && masked[maskIndex][1] <= position {
				maskIndex++
			}
			if maskIndex < len(masked) && masked[maskIndex][0] <= position {
				position = min(finish, masked[maskIndex][1])
				continue
			}
			end := finish
			if maskIndex < len(masked) {
				end = min(end, masked[maskIndex][0])
			}
			if end > position {
				runs = append(runs, [2]int{position, end})
			}
			position = end
		}
		k = finish
	}
	return runs
}

// localPairOK は _local_pair_ok (i と j の印を、両隣の字だけで組めるか。2 つの規則の両方で)。
func localPairOK(text []rune, i, j int) bool {
	leftStart, leftEnd := starExtent(text, i)
	rightStart, rightEnd := starExtent(text, j)
	for _, p := range profiles {
		leftOpen, leftClose := roles(text, leftStart, leftEnd, p)
		rightOpen, rightClose := roles(text, rightStart, rightEnd, p)
		if !leftOpen || !rightClose {
			return false
		}
		leftN, rightN := leftEnd-leftStart, rightEnd-rightStart
		if (rightOpen || leftClose) && (leftN+rightN)%3 == 0 && (leftN%3 != 0 || rightN%3 != 0) {
			return false
		}
	}
	return true
}

// closeOf は _bold_close_of (s の先頭のかっこに対応する閉じかっこの位置。無ければ -1)。s[0] は brackets のかっこ。
func closeOf(s []rune) int {
	open, close, depth := s[0], brackets[s[0]], 0
	for k, x := range s {
		switch x {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

// short は _bold_short (30 字を超える太字は両端の 12 字だけを見せる)。
func short(s []rune) []rune {
	if len(s) <= 30 {
		return s
	}
	out := append([]rune{}, s[:12]...)
	out = append(out, '…')
	return append(out, s[len(s)-12:]...)
}

// find・rfind は str.find / str.rfind (token は ASCII)。
func find(s []rune, token string) int {
	for k := 0; k+len(token) <= len(s); k++ {
		if visibility.HasPrefixAt(s, token, k) {
			return k
		}
	}
	return -1
}

func rfind(s []rune, token string) int {
	for k := len(s) - len(token); k >= 0; k-- {
		if visibility.HasPrefixAt(s, token, k) {
			return k
		}
	}
	return -1
}

func contains(s []rune, token string) bool { return find(s, token) >= 0 }
