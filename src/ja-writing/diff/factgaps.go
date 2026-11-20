package diff

import (
	"slices"
	"sort"
	"sync"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 事実の穴の候補 (本家の fact_gaps。check の --fact-gaps の手がかりで、diff の出力には含めない)。最終稿の文のうち、比喩や程度の語だけで、
// 中身や値が書かれていないかもしれない所を拾う。語の並びは一覧 fact-gaps.txt (本家の BUILTIN_LISTS["fact-gaps"] と同じ中身)。

var (
	factGapNeed = map[string]string{"degree": "程度の値（何がどれだけ）", "change": "変わる前と後の値", "effect": "何がどう変わるか",
		"figure": "比喩が指す中身（何が起きた、何をした）"}
	factGapHedge = pyre.NewLazy(`かもしれ|かも知れ|可能性|予定|未定|未確認|検討中|調査中|見込み|はず|でしょう|だろう|と思|ようです|らしい`+
		`|おそれ|恐れ|と考えられ|と見られ|れば|なら(?=[、。]|$)|たら(?=[、。]|$)`, 0)
	// 当たった語のすぐあとの述語が打ち消し (大きく変わりません、一度に大きく変えず) なら出さない
	factGapNegatedTail = pyre.NewLazy(`^[^、。，！？!?]{0,6}?(?:ない|ません|ず(?![つっ])|なかっ|(?:する|される|した|している)こと(?:は|が)(?:ない|ありません))`, 0)
	// 意図してぼかした数・時期・相手。この語がある文は出さない (埋めるよう促さない)
	factGapVague = pyre.NewLazy(`一部の|複数|いくつか|数(?:社|時間|分|秒|日|週間|か月|年|倍|割|件|回|人|台|十|百|千|万)|近日|後日|別途|ある取引先|関係者`, 0)
	// 比べる元がある (「本体より長く」) 変化は、値がなくても穴にしない (change だけ)
	factGapCompared  = pyre.NewLazy(`より`, 0)
	factGapQuestion  = pyre.NewLazy(`(?:[？?]|か[。？?])[」』）)]*$`, 0)
	factGapKanjiNum  = pyre.NewLazy(`[〇一二三四五六七八九十百千万]+(?:倍|割|分|秒|時間|日|件|回|人|個|%|％)`, 0)
	factGapAssume    = pyre.NewLazy(`もし|仮に|場合|とすれば|としたら|ならば`, 0)
	factGapQuote     = pyre.NewLazy(`「[^」]*」|『[^』]*』`, 0)
	factGapOmission  = pyre.NewLazy(`省きます|省略|伏せ|社外秘|非公開|別途|控え|今後|これからも|引き続き|まいります|ていきます|ていきたい|続けます|に向けて`, 0)
	factGapSkipKinds = []string{"code", "frontmatter", "html_block", "table", "heading", "heading_underline", "blank", "thematic_break",
		"reference_definition"}
)

type factGapPattern struct {
	kind    string
	pattern *pyre.Lazy
}

var (
	factGapOnce     sync.Once
	factGapPatterns []factGapPattern
)

func factGapList() []factGapPattern {
	factGapOnce.Do(func() {
		for _, item := range LoadList("fact-gaps").Items {
			factGapPatterns = append(factGapPatterns, factGapPattern{item.Name, pyre.NewLazy(item.Regex, 0)})
		}
	})
	return factGapPatterns
}

// factGapHasNumber は文に数値 (数値の取り出し方で取れるもの) か、漢数字と単位の並びがあるか。
func factGapHasNumber(text string) bool {
	s, err := newSides(text, "")
	if err != nil {
		panic(pyException{err})
	}
	return len(findNumbers(s.texts[0], s.analyses[0], numberMaskedKinds)) > 0 || factGapKanjiNum.MustSearch(text)
}

type factGap struct {
	kind, sentence, match string
	line                  int
}

// FactGaps は本家の fact_gaps。全件数と種類ごとの件数を数え、items には 20 件・文 80 字まで残す。docType が essay か promo なら探さない。
// 本家が例外になる入力 (桁数の上限を超える数) は *visibility.Error を返す。
func FactGaps(text string, docType *string) (result *pyjson.Object, err error) {
	defer recoverPyException(&err)
	if docType != nil && (*docType == "essay" || *docType == "promo") {
		out := pyjson.NewObject()
		out.Set("count", int64(0))
		out.Set("skipped", *docType)
		out.Set("by_kind", pyjson.NewObject())
		out.Set("items", []pyjson.Value{})
		return out, nil
	}
	return factGaps(text), nil
}

func factGaps(text string) *pyjson.Object {
	doc := newGroundDoc(text)
	rows := doc.analyses[0].Lines
	var sentences []sentence
	for index := range doc.lines[0] {
		if index < len(rows) && (slices.Contains(factGapSkipKinds, rows[index].Kind) || rows[index].QuoteDepth != 0) {
			continue
		}
		sentences = append(sentences, doc.lineSentences(0, index, index+1)...)
	}
	masker := doc.newMasker(0)
	for _, m := range mustFinditer(factGapQuote, doc.docText(0), 0, pyre.NoEnd) {
		masker.spans = append(masker.spans, [2]int{m.Start(0), m.End(0)})
	}
	sort.Slice(masker.spans, func(i, j int) bool {
		a, b := masker.spans[i], masker.spans[j]
		return a[0] < b[0] || a[0] == b[0] && a[1] < b[1]
	})
	masker.lefts = masker.lefts[:0]
	for _, span := range masker.spans {
		masker.lefts = append(masker.lefts, span[0])
	}
	patterns := factGapList()
	var out []factGap
	for position, sn := range sentences {
		body := sn.text
		if masker.mask(body, sn.start) != body || factGapHasNumber(body) ||
			factGapHedge.MustSearch(body) || factGapQuestion.MustSearch(body) || factGapVague.MustSearch(body) ||
			factGapOmission.MustSearch(body) || factGapAssume.MustSearch(body) || groundCertainty.MustSearch(body) {
			continue
		}
		if position+1 < len(sentences) {
			next := sentences[position+1]
			if next.line-sn.line <= 1 && factGapHasNumber(next.text) {
				continue
			}
		}
		if gap, ok := factGapIn(patterns, body); ok {
			gap.line = sn.line
			out = append(out, gap)
		}
	}
	counts := pyjson.NewObject()
	for _, gap := range out {
		n, _ := counts.Field(gap.kind).(int64)
		counts.Set(gap.kind, n+1)
	}
	items := []pyjson.Value{}
	for _, gap := range out[:min(20, len(out))] {
		o := pyjson.NewObject()
		o.Set("kind", gap.kind)
		o.Set("line", int64(gap.line))
		o.Set("sentence", pyunicode.FromRunes(pySlice(pyunicode.Runes(gap.sentence), 0, 80)))
		o.Set("match", gap.match)
		o.Set("need", factGapNeed[gap.kind])
		items = append(items, o)
	}
	result := pyjson.NewObject()
	result.Set("count", int64(len(out)))
	result.Set("skipped", nil)
	result.Set("by_kind", counts)
	result.Set("items", items)
	return result
}

// factGapIn は一覧の行の順に正規表現を当て、最初に当たった (打ち消されていない) 語の種類と語を返す。
func factGapIn(patterns []factGapPattern, body string) (factGap, bool) {
	runes := pyunicode.Runes(body)
	t := pyre.NewText(body)
	for _, p := range patterns {
		if _, ok := factGapNeed[p.kind]; !ok {
			continue
		}
		for _, m := range mustFinditer(p.pattern, t, 0, pyre.NoEnd) {
			if factGapNegatedTail.MustMatchAt0(pyunicode.FromRunes(runes[m.End(0):])) {
				continue
			}
			if p.kind == "change" && factGapCompared.MustSearch(pyunicode.FromRunes(runes[:m.Start(0)])) {
				continue
			}
			word, _ := m.Group(0)
			return factGap{kind: p.kind, sentence: body, match: word}, true
		}
	}
	return factGap{}, false
}
