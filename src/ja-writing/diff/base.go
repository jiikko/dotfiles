package diff

import (
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
)

// 群 B: v1.1.2 から引き継いだ点検 (9 つのキー) の下ごしらえ。本家の normalize・has_list・sentences・paragraphs・logic_points。
// 本家の count (re.findall の長さを返すだけ) と _bold_short は、本家のどこからも呼ばれない / internal/bold が持つので写さない。

var (
	normalizeBold    = pyre.NewLazy(`\*\*(.+?)\*\*`, 0)
	normalizeList    = pyre.NewLazy(`(?m)^\s*(?:[*\-・]|\d+[.)])\s+`, 0)
	normalizeHeading = pyre.NewLazy(`(?m)^#+\s*`, 0)
	normalizeSpaces  = pyre.NewLazy(`[ \t]+`, 0)
	hasListPattern   = pyre.NewLazy(`(?m)^\s*(?:[*\-・]|\d+[.)])\s+\S`, 0)
	sentencesSplit   = pyre.NewLazy(`(?<=[。！？!?])|\n+`, 0)
	listItemHead     = pyre.NewLazy(`\s*(?:[*\-・]|\d+[.)])\s+`, 0)
)

// Normalize は本家の normalize: 太字・箇条書きの印・見出しの印を外し、空白を 1 つにして前後を落とす。
func Normalize(t string) string {
	t = normalizeBold.MustSub(`\1`, t)
	t = normalizeList.MustSub("", t)
	t = normalizeHeading.MustSub("", t)
	t = normalizeSpaces.MustSub(" ", t)
	return pystr.Strip(t)
}

// HasList は本家の has_list: 箇条書きの行があるか。
func HasList(t string) bool { return hasListPattern.MustSearch(t) }

// Sentences は本家の sentences: 文末の記号の後ろと改行で割った、空白だけでない断片。
func Sentences(t string) []string {
	var out []string
	for _, s := range sentencesSplit.MustSplit(t) {
		if pystr.Strip(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// Paragraphs は本家の paragraphs: 段落の数を数える (続いた箇条書きは 1 つの段落とみなす)。段落の先頭の行を返す。
func Paragraphs(t string) []string {
	var blocks []string
	prevList := false
	for _, line := range strings.Split(t, "\n") {
		if pystr.Strip(line) == "" {
			prevList = false
			continue
		}
		isList := listItemHead.MustMatchAt0(line)
		if isList && prevList {
			continue
		}
		blocks = append(blocks, line)
		prevList = isList
	}
	return blocks
}

// logicRule は logic_points の 1 つの見立て (文頭のつなぎ・主題の「も」・予告だけの文・文頭の指示語)。順は本家の LOGIC の順。
type logicRule struct {
	name    string
	pattern *pyre.Lazy
}

var logicRules = []logicRule{
	{"文頭のつなぎ", pyre.NewLazy(`^(?:ただし|しかし|一方|また|さらに|つまり|そのため|したがって|だから|それでも|なお|そこで|ところが)`, 0)},
	{"主題の「も」", pyre.NewLazy(`^(?!それで)[^、。]{0,17}[^、。てでり]も、`, 0)},
	{"予告だけの文", pyre.NewLazy(`^.{0,28}(?:が|も)あります。$|次の(?:点|こと|とおり|通り)です|以下の(?:点|こと|とおり|通り)`, 0)},
	{"文頭の指示語", pyre.NewLazy(`^(?:これ|それ(?!でも|から)|こう(?:した|して|する|いう)|そう(?:した|して|する|いう)|この|その)(?!して)`, 0)},
}

// LogicPoint は logic_points の 1 件 ({"kind", "sentence"})。
type LogicPoint struct {
	Kind     string
	Sentence string
}

// LogicPoints は本家の logic_points: 正規化した文を文ごとに、4 つの見立てのどれに当たるかを並べる (同じ文が複数に当たれば複数)。
func LogicPoints(t string) []LogicPoint {
	var pts []LogicPoint
	for _, s := range Sentences(Normalize(t)) {
		s2 := pystr.Strip(s)
		for _, r := range logicRules {
			if r.pattern.MustSearch(s2) {
				pts = append(pts, LogicPoint{r.name, s2})
			}
		}
	}
	return pts
}
