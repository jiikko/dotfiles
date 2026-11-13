package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G8 語彙 (slop_vocabulary・metaphor_verb・translated_term。本家 yomiyasu_lint.py の 921-996・1188-1348・5340・5586-5651・5754-5771)。表は g08_tables.go。
//
// 本家は文書の語で表の行を絞る (_narrowed_document_rows) が、行ごとの必要な語の判定 (_may_match_literals) が同じ行を落とすので、文書で絞った結果は変わらない。
// Go は文書での絞り込みを持たず、行ごとの判定だけを写す (必要な語は patternRow.required)。

// kikuWindowLines は比喩の「効く」で、前の行の文字どおりの語 (薬・エアコンなど) を見る窓の行数 (_KIKU_WINDOW_LINES)。
const kikuWindowLines = 3

// g08State は G8 が文書の中で持つ状態。行ごとに決まるので、持つものは無い (比喩の「効く」の窓は骨組みの kikuLines)。
type g08State struct{}

// g08Prepare は行のループの前に 1 回。G8 は文書全体の下ごしらえを持たない。
func (s *lintState) g08Prepare() {}

// g08Window は行のループの 1 行ごとに呼ばれ、比喩の「効く」の窓 (この行より前の、直近 kikuWindowLines 行) を返して、この行を覚える。
// 本家の `kiku_before = kiku_lines[-3:]; kiku_lines = kiku_before + [plain_text]`。
func (s *lintState) g08Window(plain string) []string {
	before := slices.Clone(s.kikuLines[max(0, len(s.kikuLines)-kikuWindowLines):])
	s.kikuLines = append(slices.Clone(before), plain)
	return before
}

// mayMatch は本家の _may_match_literals (必要な語が無いなら全部の行に当てる)。
func mayMatch(required []string, text string) bool {
	if required == nil {
		return true
	}
	return slices.ContainsFunc(required, func(literal string) bool { return strings.Contains(text, literal) })
}

var labelWord = newLazy(`「([^」]+)」`, 0)

// hitWords は本家の _hit_words。候補の内部の欄 Words の値 (message に出る表の語・そこから「〜」と英語の注記を除いた語・当たった文字列を、重ねずにこの順で)。
func hitWords(label string, match *pyre.Match) []string {
	shown := label
	if m, err := labelWord.get().Search(pyre.NewText(label), 0, pyre.NoEnd); err != nil {
		panic(err)
	} else if m != nil {
		shown, _ = m.Group(1)
	}
	name, err := hitWordNote.get().Sub("", pyre.NewText(shown), 0)
	if err != nil {
		panic(err)
	}
	name = pystr.StripChars(name, "〜~ \u3000")
	matched := ""
	if match != nil {
		matched, _ = match.Group(0)
		matched = pystr.Strip(matched)
	}
	var words []string
	for _, word := range []string{shown, name, matched} {
		if word != "" && !slices.Contains(words, word) {
			words = append(words, word)
		}
	}
	return words
}

var hitWordNote = newLazy(`[ \u3000]*[（(][^）)]*[）)]$`, 0)

func searchText(p *lazyPattern, text string) *pyre.Match {
	m, err := p.get().Search(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m
}

// g08Vocabulary は見出しの行と、地の文の行で呼ばれる (本家の _vocabulary_findings の slop_vocabulary → metaphor_verb の順。5586-5651)。
// snippet は見出しなら r.stripped、地の文なら元の行の前後の空白を除いたもの。
func (s *lintState) g08Vocabulary(r *rowCtx, snippet string) {
	plain := r.plainText
	for _, word := range slopWords {
		if strings.Contains(plain, word) {
			f := newFinding("slop_vocabulary", r.lineNo, "warn", slopMessage(word), snippet)
			f.Words = []string{word}
			s.add(f)
		}
	}
	for _, p := range slopPatterns {
		if !mayMatch(p.required, plain) {
			continue
		}
		if m := searchText(p.re, plain); m != nil {
			f := newFinding("slop_vocabulary", r.lineNo, "warn", slopMessage(p.desc), snippet)
			f.Words = hitWords(p.desc, m)
			s.add(f)
		}
	}
	s.g08Metaphor(r, plain, snippet)
}

func slopMessage(word string) string {
	return "AI頻出語彙「" + word + "」が含まれています。文脈上必要のない比喩や大げさな装飾であれば、ふだん使う自然な表現に置き換えてください。ただし、文字どおりの意味や必要な文脈を担っている場合は残してかまいません。"
}

const (
	kowareruDesc   = "比喩動詞「壊れる」"
	silentFailDesc = "英語直訳「静かに壊れる (silently fail)」"
)

func metaphorMessage(p patternRow) string {
	tail, ok := metaphorVerbMessageTails[p.pattern]
	if !ok {
		tail = metaphorVerbMessageTail
	}
	return p.desc + "が検出されました。" + tail
}

// g08Metaphor は比喩動詞の表を表の順に当てる。「壊れる」と「静かに壊れる」が同じ動詞に二重に反応しないよう、前者の当たりの範囲に収まる後者の当たりは数えない。
func (s *lintState) g08Metaphor(r *rowCtx, plain, snippet string) {
	var kowareru *[2]int
	for _, p := range metaphorVerbPatterns {
		if !mayMatch(p.required, plain) {
			continue
		}
		if p.desc == silentFailDesc && kowareru != nil {
			matches, err := p.re.get().Finditer(pyre.NewText(plain), 0, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			for _, m := range matches {
				if kowareru[0] <= m.Start(0) && m.End(0) <= kowareru[1] {
					continue
				}
				f := newFinding("metaphor_verb", r.lineNo, "warn", metaphorMessage(p), snippet)
				f.Words = hitWords(p.desc, m)
				s.add(f)
				break
			}
			continue
		}
		m := searchText(p.re, plain)
		if m != nil && p.pattern == kikuPattern && !kikuMetaphorInLine(plain, r.kikuBefore) {
			m = nil
		}
		if m == nil {
			continue
		}
		if p.desc == kowareruDesc {
			kowareru = &[2]int{m.Start(0), m.End(0)}
		}
		f := newFinding("metaphor_verb", r.lineNo, "warn", metaphorMessage(p), snippet)
		f.Words = hitWords(p.desc, m)
		s.add(f)
	}
}

// kikuMetaphorInLine は本家の _kiku_metaphor_in_line。行の中に比喩の「効く」として拾う文があれば真。before は同じ文書の前の行 (新しい行が後ろ)。
// 語・慣用句・打ち消しなどの除き方は、本家のコメントに従って表 (kikuRE) と同じ順に当てる。
func kikuMetaphorInLine(text string, before []string) bool {
	re := func(name string) *lazyPattern { return kikuRE[name] }
	sentences := mustFindall0(re("sentence"), text)
	var earlier []string
	windowKnown, windowHit := false, false
	for _, sentence := range sentences {
		looseKnown, loose := false, false
		sentenceRunes := pyunicode.Runes(sentence)
		candidates, err := kikuCandidate.get().Finditer(pyre.NewText(sentence), 0, pyre.NoEnd)
		if err != nil {
			panic(err)
		}
		for _, c := range candidates {
			start := c.Start(0)
			prefix := runeSlice(sentenceRunes, max(0, start-48), start)
			suffix := runeSlice(sentenceRunes, start+1, start+13)
			if mustSearch(re("idiom"), prefix) || mustSearch(re("literal_subject"), prefix) {
				continue
			}
			if mustSearch(re("v210"), prefix) {
				return true
			}
			if mustMatchAt0(re("negation"), suffix) {
				continue
			}
			verb := mustMatchAt0(re("verb"), suffix)
			if verb && mustSearch(re("abstract_subject"), prefix) {
				return true
			}
			if mustMatchAt0(re("effect"), suffix) && mustSearch(re("effect_owner"), prefix) {
				return true
			}
			if !mustMatchAt0(re("gradual"), suffix) && (!verb || !mustSearch(re("adverb"), prefix)) {
				continue
			}
			if !looseKnown {
				loose, looseKnown = mustSearch(re("context_near"), sentence) || mustSearch(re("timing"), sentence), true
			}
			if loose {
				continue
			}
			if !windowKnown {
				nearby := slices.Concat(before[max(0, len(before)-kikuWindowLines):], earlier)
				windowHit, windowKnown = mustSearch(re("context_near"), strings.Join(nearby, "\n")), true
			}
			if windowHit {
				continue
			}
			return true
		}
		earlier = append(earlier, sentence)
	}
	return false
}

var kikuCandidate = newLazy(kikuPattern, 0)

// mustFindall0 は p.finditer(text) の全文の当たり (group(0))。
func mustFindall0(p *lazyPattern, text string) []string {
	matches, err := p.get().Finditer(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i], _ = m.Group(0)
	}
	return out
}

// g08Translated は地の文の行で、g08Vocabulary の後に呼ばれる (translated_term。同じ desc の型は 1 件にまとめ、当たった語は重ねずに持つ。5754-5771)。
func (s *lintState) g08Translated(r *rowCtx) {
	var order []string
	hits := map[string][]string{}
	for _, p := range translatedTermPatterns {
		if !mayMatch(p.required, r.plainText) {
			continue
		}
		if m := searchText(p.re, r.plainText); m != nil {
			if _, seen := hits[p.desc]; !seen {
				order = append(order, p.desc)
			}
			hits[p.desc] = append(hits[p.desc], hitWords(p.desc, m)...)
		}
	}
	for _, desc := range order {
		f := newFinding("translated_term", r.lineNo, "warn", "英語の直訳"+desc+"が検出されました。ソフトウェアの文脈であれば、開発現場で定着した言い方（「バージョン」「リグレッション」「デグレ」など）や具体的な説明を検討してください。ただし、出版物や製品の種類を表す「版」、回帰テストや回帰分析など定着した用語は残してかまいません。", pystr.Strip(r.line))
		f.Words = uniqueInOrder(hits[desc])
		s.add(f)
	}
}
