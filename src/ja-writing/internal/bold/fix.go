package bold

import (
	"slices"
	"strings"

	"ja-writing/visibility"
)

// 直し方の案の形 (本家の tries の 3 つめの値)。
const (
	shapeBracket = "bracket"
	shapePunct   = "punct"
	shapeSpace   = "space"
)

// 直し方 (本家の how の文字列。候補の文面にそのまま入る)。
const (
	howManual  = "手で直す"
	howBracket = "かっこの内側だけを太字にする"
	howPunct   = "句読点を太字の外に出す"
	howTrim    = "太字の内側の空白を取る"
	howSpace   = "文字に接する側に半角スペースを入れる"
)

const outsidePunct = "。、．，！？!?"

type try struct {
	middle []rune
	how    string
	shape  string
}

// tries は _bold_fix_with_context の案を、試す順に作る (かっこの内側 → 句読点を外へ → 空白)。
func tries(text []rune, i, j int) []try {
	inner := text[i+2 : j]
	var out []try
	if len(inner) >= 3 {
		if _, ok := brackets[inner[0]]; ok && closeOf(inner) == len(inner)-1 {
			out = append(out, try{concat([]rune{inner[0]}, []rune("**"), inner[1:len(inner)-1], []rune("**"), []rune{inner[len(inner)-1]}), howBracket, shapeBracket})
		}
	}
	if len(inner) >= 2 && strings.ContainsRune(outsidePunct, inner[len(inner)-1]) {
		out = append(out, try{concat([]rune("**"), inner[:len(inner)-1], []rune("**"), []rune{inner[len(inner)-1]}), howPunct, shapePunct})
	}
	bodyStart, bodyEnd := 0, len(inner)
	for bodyStart < bodyEnd && isWS(inner[bodyStart]) {
		bodyStart++
	}
	for bodyEnd > bodyStart && isWS(inner[bodyEnd-1]) {
		bodyEnd--
	}
	body := inner[bodyStart:bodyEnd]
	var left, right []rune
	if !canOpenBoth(charAt(text, i-1), firstChar(body)) {
		left = []rune(" ")
	}
	if !canCloseBoth(lastChar(body), charAt(text, j+2)) {
		right = []rune(" ")
	}
	how := howSpace
	if len(body) != len(inner) && left == nil && right == nil {
		how = howTrim
	}
	out = append(out, try{concat(left, []rune("**"), body, []rune("**"), right), how, shapeSpace})
	return out
}

func concat(parts ...[]rune) []rune {
	var out []rune
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// fixWithContext は _bold_fix_with_context (確かめられた案だけを出す。決まらなければ「手で直す」)。返す middle が nil なら案なし。
func fixWithContext(text []rune, i, j int, c *scanContext) ([]rune, string, error) {
	if !eligible(text, i, j, c) {
		return nil, howManual, nil
	}
	several := len(c.runs) > 2
	for _, t := range tries(text, i, j) {
		// 3 つ以上の印があるブロックで、解析し直した案が 2 つを超えたら、元の解析から決められる案を先に決める (本家 2714-2717 行)。
		if several && c.slowTries >= 2 && (c.haveRepair || c.remaining >= 3) {
			repair, err := repairIndexOf(text, c)
			if err != nil {
				return nil, "", err
			}
			if repair != nil {
				switch repair.decide(i, j, t.shape, t.middle) {
				case decideOK:
					return t.middle, t.how, nil
				case decideNG:
					continue
				case decideUnknown:
					// 決められないので、下で解析し直して確かめる
				}
			}
		}
		if several {
			c.slowTries++
		}
		ok, err := verifyByReparse(text, i, j, t.middle, c)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return t.middle, t.how, nil
		}
	}
	return nil, howManual, nil
}

// eligible は _bold_fix_with_context の、案を作る前の断り (偽なら「手で直す」)。真のときは c.opaque を作ってある。
func eligible(text []rune, i, j int, c *scanContext) bool {
	openStart, openEnd := starExtent(text, i)
	closeStart, closeEnd := starExtent(text, j)
	if openEnd-openStart != 2 || closeEnd-closeStart != 2 {
		return false
	}
	several := len(c.runs) > 2
	index := c.runIndexOf()
	if _, ok := index[[2]int{i, i + 2}]; !ok {
		return false
	}
	if _, ok := index[[2]int{j, j + 2}]; !ok {
		return false
	}
	inner := text[i+2 : j]
	if slices.Contains(inner, '*') {
		return false
	}
	// 印の多い段落では、空白だけの症状からは狙いの組が決まらない (本家 2678-2679 行)。
	if several && (isWS(firstChar(inner)) || isWS(lastChar(inner))) {
		return false
	}
	// この印の字に触れる既存の組は、報告した組そのものでなければならない。そうでなければ、どの案でも他の組を残せない。
	if c.delimiterMatches == nil {
		c.delimiterMatches = map[int][]emph{}
		for _, list := range [2][]emph{c.gfm, c.common} {
			for _, e := range list {
				for k := range e.width {
					c.delimiterMatches[e.left+k] = append(c.delimiterMatches[e.left+k], e)
					c.delimiterMatches[e.right+k] = append(c.delimiterMatches[e.right+k], e)
				}
			}
		}
	}
	target := emph{i, j, 2}
	for _, position := range [4]int{i, i + 1, j, j + 1} {
		for _, e := range c.delimiterMatches[position] {
			if e != target {
				return false
			}
		}
	}
	if !c.haveOpaque {
		c.opaqueSpans = visibility.InlineProtectedSpans(str(text))
		c.opaque = opaqueOf(text, c.opaqueSpans)
		c.haveOpaque = true
	}
	return true
}

// verifyByReparse は、案で書き換えた文を解析し直して、狙いの組が 2 つの規則の両方でできて、ほかの組が全部残るかを確かめる。
func verifyByReparse(text []rune, i, j int, middle []rune, c *scanContext) (bool, error) {
	candidate := concat(text[:i], middle, text[j+2:])
	target := pair{i + find(middle, "**"), i + rfind(middle, "**")}
	a, err := visibility.AnalyzeMarkdown(str(candidate), false)
	if err != nil {
		return false, err
	}
	runs := runsFromMasks(candidate, a.MaskSpans)
	if !slices.Equal(opaqueOf(candidate, visibility.InlineProtectedSpans(str(candidate))), c.opaque) {
		return false, nil
	}
	// 2 字の印がちょうど 2 つなら、両隣の字だけで決まる (競う印が無く、2+2 は 3 の倍数の規則に当たらない)。
	if len(runs) == 2 && len(c.runs) == 2 {
		return runs[0] == [2]int{target.i, target.i + 2} && runs[1] == [2]int{target.j, target.j + 2} &&
			localPairOK(candidate, target.i, target.j), nil
	}
	gfmPairs, _, emphGFM := delimiterParse(candidate, gfm, runs)
	commonPairs, _, emphCommon := delimiterParse(candidate, common, runs)
	delta := len(candidate) - len(text)
	return slices.Contains(gfmPairs, target) && slices.Contains(commonPairs, target) &&
		preservesOtherEmphasis(c.gfm, emphGFM, i, j, delta) && preservesOtherEmphasis(c.common, emphCommon, i, j, delta), nil
}

// preservesOtherEmphasis は _preserves_other_emphasis (元の組が、書き換えのあとも同じ相手と組んでいるか。報告した組そのものは除く)。
func preservesOtherEmphasis(original, repaired []emph, i, j, delta int) bool {
	set := make(map[emph]bool, len(repaired))
	for _, e := range repaired {
		set[e] = true
	}
	for _, e := range original {
		if e == (emph{i, j, 2}) {
			continue // 報告した組は、片方の規則でしか組めないことがある
		}
		if (i <= e.left && e.left < j+2) || (i <= e.right && e.right < j+2) {
			return false // 交差する組や内側の組は、書き換えるには曖昧すぎる
		}
		left, right := e.left, e.right
		if left >= j+2 {
			left += delta
		}
		if right >= j+2 {
			right += delta
		}
		if !set[emph{left, right, e.width}] {
			return false
		}
	}
	return true
}
