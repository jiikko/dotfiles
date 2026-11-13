package lint

import (
	"slices"
	"strings"
)

// 本家の _emoji_flag_pattern と EMOJI_PATTERN を、Go の表から組み立てる。本番は正規表現で照合しないので (emoji.go の冒頭)、記録との比べと、
// 照合を正規表現の全部の一致と比べる検査 (TestEmojiMatchesAgreesWithPatternFindall) だけが使う。

// emojiFlagPattern は本家の _emoji_flag_pattern。登録された地域の旗の組だけを認める選択肢。
func emojiFlagPattern() string {
	seconds := map[byte][]rune{}
	var firsts []byte
	for _, code := range emojiFlagRegionCodes {
		if _, ok := seconds[code[0]]; !ok {
			firsts = append(firsts, code[0])
		}
		seconds[code[0]] = append(seconds[code[0]], regionalIndicatorA+rune(code[1]-'A'))
	}
	slices.Sort(firsts)
	groups := make([]string, 0, len(firsts))
	for _, first := range firsts {
		cp := regionalIndicatorA + rune(first-'A')
		second := uniqueInOrder(slices.Sorted(slices.Values(seconds[first])))
		classes := make([]runeRange, len(second))
		for i, r := range second {
			classes[i] = runeRange{r, r}
		}
		groups = append(groups, emojiCharacterClass([]runeRange{{cp, cp}})+emojiCharacterClass(classes))
	}
	return "(?:" + strings.Join(groups, "|") + ")"
}

// emojiPatternText は EMOJI_PATTERN のパターンの文字列。patterns.json (本家が使った全パターンの記録) に同じ文字列があることをテストが見る。
func emojiPatternText() string {
	return "(?:" + emojiFlagPattern() +
		`|[0-9#*]\ufe0f\u20e3` +
		"|" + emojiCharacterClass(emojiExplicitVariationBases[:]) + `\ufe0f` +
		"|" + emojiCharacterClass(emojiRegisteredTextBases[:]) + `(?!\ufe0e)` +
		"|" + emojiCharacterClass(emojiOtherBases[:]) + ")"
}
