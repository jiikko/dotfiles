package lint

import (
	"fmt"
	"strings"
	"sync"

	"ja-writing/internal/pyunicode"
)

// 絵文字の判定 (本家 yomiyasu_lint.py の T1。表は Unicode 18.0 の emoji-data などから作ったもので、emoji_tables_gen.go が生成する)。
//
// 本家の名前と Go の名前:
//
//	_emoji_matches          EmojiMatches (公開。diff が同じ数え方を借りる)
//	_emoji_character_class  emojiCharacterClass
//	_emoji_flag_pattern     emojiFlagPattern (emoji_pattern_test.go。記録との比べにだけ使う)
//	EMOJI_PATTERN           emojiPatternText (同。パターンの文字列だけ。照合は正規表現でなく下の表と枝で行う)
//	_EMOJI_START_PATTERN    emojiStartsAt
//
// 照合を正規表現にしない理由: EMOJI_PATTERN は 8,514 字で、旗の 259 通りの組と区間 424 個の選択肢を持つ。regexp2 は後戻りする照合器で、
// 文書の全部の位置から試すと遅くなりうる (本家は _EMOJI_START_PATTERN で始まりの字を絞ってから match している)。
// 表と 5 つの枝 (旗・キーキャップ・U+FE0F つきの基底・テキスト表現の基底・そのほか) を手で書き、本家の答えと同じことを lint-base.json (oracle) で確かめる。

// runeRange は閉区間 [lo, hi]。
type runeRange struct{ lo, hi rune }

const (
	variationSelector16 = '\ufe0f'
	variationSelector15 = '\ufe0e'
	combiningKeycap     = '\u20e3'
	regionalIndicatorA  = 0x1F1E6
	regionalIndicatorZ  = 0x1F1FF
)

// inRuneRanges は昇順で重ならない区間の並びの中に r があるか。
func inRuneRanges(ranges []runeRange, r rune) bool {
	low, high := 0, len(ranges)
	for low < high {
		mid := int(uint(low+high) >> 1)
		switch rg := ranges[mid]; {
		case r < rg.lo:
			high = mid
		case r > rg.hi:
			low = mid + 1
		default:
			return true
		}
	}
	return false
}

// emojiFlagPairs は旗として認める (1 つ目, 2 つ目) の組 (地域指示子の字)。
var emojiFlagPairs = sync.OnceValue(func() map[[2]rune]struct{} {
	pairs := make(map[[2]rune]struct{}, len(emojiFlagRegionCodes))
	for _, code := range emojiFlagRegionCodes {
		pairs[[2]rune{regionalIndicatorA + rune(code[0]-'A'), regionalIndicatorA + rune(code[1]-'A')}] = struct{}{}
	}
	return pairs
})

func isKeycapBase(r rune) bool { return (r >= '0' && r <= '9') || r == '#' || r == '*' }

// emojiStartsAt は _EMOJI_START_PATTERN が rs[pos] を始まりの候補にするか。
// 始まりの候補は、`[0-9#*]` の後ろに U+FE0F U+20E3 が続く字か、U+00A9〜U+2FFF・U+3030・U+303D・U+3297・U+3299・U+1F000〜U+1FAFF の字。
func emojiStartsAt(rs []rune, pos int) bool {
	r := rs[pos]
	if isKeycapBase(r) && pos+2 < len(rs) && rs[pos+1] == variationSelector16 && rs[pos+2] == combiningKeycap {
		return true
	}
	return (r >= 0xA9 && r <= 0x2FFF) || r == 0x3030 || r == 0x303D || r == 0x3297 || r == 0x3299 || (r >= 0x1F000 && r <= 0x1FAFF)
}

// emojiMatchAt は EMOJI_PATTERN.match(text, pos) の長さ (一致しなければ 0)。選択肢は本家の並びの順 (最初に合ったものが勝つ)。
func emojiMatchAt(rs []rune, pos int) int {
	r := rs[pos]
	next := func(i int) rune {
		if pos+i < len(rs) {
			return rs[pos+i]
		}
		return -1
	}
	if _, ok := emojiFlagPairs()[[2]rune{r, next(1)}]; ok {
		return 2
	}
	if isKeycapBase(r) && next(1) == variationSelector16 && next(2) == combiningKeycap {
		return 3
	}
	if inRuneRanges(emojiExplicitVariationBases[:], r) && next(1) == variationSelector16 {
		return 2
	}
	if inRuneRanges(emojiRegisteredTextBases[:], r) && next(1) != variationSelector15 {
		return 1
	}
	if inRuneRanges(emojiOtherBases[:], r) {
		return 1
	}
	return 0
}

// EmojiMatches は本家の _emoji_matches。text の絵文字を、出た順に、重ならない形で返す (EMOJI_PATTERN.findall と同じ結果)。無ければ nil。
// emoji_prohibited (lint) と diff の絵文字の数え方がこれを使う。
func EmojiMatches(text string) []string {
	rs := pyunicode.Runes(text)
	var matches []string
	previousEnd := 0
	for pos := range rs {
		if pos < previousEnd || !emojiStartsAt(rs, pos) {
			continue
		}
		if n := emojiMatchAt(rs, pos); n > 0 {
			matches = append(matches, pyunicode.FromRunes(rs[pos:pos+n]))
			previousEnd = pos + n
		}
	}
	return matches
}

// emojiCharacterClass は本家の _emoji_character_class。区間から正規表現の文字クラスの文字列を作る。
func emojiCharacterClass(ranges []runeRange) string {
	escaped := func(r rune) string {
		if r <= 0xFFFF {
			return fmt.Sprintf(`\u%04x`, r)
		}
		return fmt.Sprintf(`\U%08x`, r)
	}
	var b strings.Builder
	b.WriteByte('[')
	for _, rg := range ranges {
		b.WriteString(escaped(rg.lo))
		if rg.lo != rg.hi {
			b.WriteByte('-')
			b.WriteString(escaped(rg.hi))
		}
	}
	b.WriteByte(']')
	return b.String()
}
