package diff

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 群 Z: 文体の硬化 (本家の STIFFNESS_THRESHOLDS・_measure・_nominal_hits・_no_chains・_metrics_of・stiffness_metrics・_changed_excerpt・_stiffness・stiffness)。
// 原文と推敲後で、漢字の割合・平均の文の長さ・名詞化の言い回し・「の」の連続を比べ、推敲で硬くなった所を見直し候補にする。
// 数の割り算と丸め (round) は本家の float と同じ順で書く (100.0 * 漢字 / 字数、round(x, 2))。

// 硬化の閾値 (本家の STIFFNESS_THRESHOLDS。no_nouns は本家の中でも読まれず、「の」の連続の正規表現の {2,} が名詞 3 つ以上を表す)。
const (
	stiffDocKanjiPt       = 3.0 // 文書全体: 漢字の割合の増え (ポイント)
	stiffDocKanjiUp       = 2   // 文書全体: 漢字の字数の増えの下限
	stiffSentKanjiPt      = 8.0 // 1 文: 漢字の割合の増え
	stiffSentKanjiUp      = 2   // 1 文: 漢字の字数の増えの下限
	stiffSentHiraDown     = 2   // 1 文: 平仮名の字数の減りの下限
	stiffSentNominalKanji = 2   // 1 文の名詞化: 漢字の字数の増えの下限
	stiffNominalDelta     = 2   // 名詞化: 文書全体で原文より増えた数の下限
	stiffNoKango          = 3   // 「の」の連続: つながる名詞のうち漢語 (漢字 2 字以上) の数の下限
)

// stiffInfoDocTypes は硬化の検査を参考 (level info) としてだけ出す文書の種類 (本家の STIFFNESS_INFO_DOC_TYPES)。
var stiffInfoDocTypes = []string{"incident", "formal", "paper"}

// 漢字 (本家の _K。々 と 〆 を含む) と平仮名の範囲、長さに数える句読点とかっこ。
const (
	stiffKanjiClass    = "㐀-䶿一-鿿豈-﫿々〆"
	stiffLengthPunct   = "、。，．！？!?「」『』（）()…・"
	stiffOkuriMid      = "りきけしみえちびれげせじつ"
	stiffOkuriTail     = "りきけしみえちびれげせじ"
	stiffNounChars     = "[" + stiffKanjiClass + "ァ-ヴーA-Za-z0-9０-９Ａ-Ｚａ-ｚ]"
	stiffNounPattern   = stiffNounChars + "+(?:[" + stiffOkuriMid + "](?=[" + stiffKanjiClass + "])" + stiffNounChars + "+)*(?:[" + stiffOkuriTail + "](?=の))?"
	stiffNoChainSource = "(?<!" + stiffNounChars + ")" + stiffNounPattern + "(?:の" + stiffNounPattern + "){2,}"
	stiffKangoSource   = "^[" + stiffKanjiClass + "]+(?:[" + stiffOkuriMid + "][" + stiffKanjiClass + "]+)*[" + stiffOkuriTail + "]?$"
)

var (
	stiffKanji       = pyre.NewLazy("["+stiffKanjiClass+"]", 0)
	stiffHiragana    = pyre.NewLazy("[ぁ-ゖ]", 0)
	stiffNoChain     = pyre.NewLazy(stiffNoChainSource, 0)
	stiffKango       = pyre.NewLazy(stiffKangoSource, 0)
	stiffNumeralHead = pyre.NewLazy("[〇一二三四五六七八九十百千万]", 0)
	// 文字と数字 (Unicode の分類が L か N の字。Python の \w から「_」を除いたもの) と、長さに数える句読点とかっこ
	letterOrDigit      = pyre.NewLazy(`[^\W_]`, 0)
	lengthPunctPattern = pyre.NewLazyFrom(func() string {
		chars := pyunicode.Runes(stiffLengthPunct)
		slices.Sort(chars)
		return "[" + pyre.Escape(pyunicode.FromRunes(chars)) + "]"
	}, 0)
)

// stiffMeasure は本家の _measure: (文の長さ, 漢字の数, 記号と空白を除いた字数)。文の長さは文字・数字と句読点・かっこの字数。
func stiffMeasure(text string) (length, kanji, counted int) {
	t := pyre.NewText(text)
	counted = countFound(letterOrDigit, t)
	return counted + countFound(lengthPunctPattern, t), countFound(stiffKanji, t), counted
}

func stiffRatio(kanji, counted int) float64 {
	if counted == 0 {
		return 0.0
	}
	return 100.0 * float64(kanji) / float64(counted)
}

func hiraganaCount(text string) int { return countFound(stiffHiragana, pyre.NewText(text)) }

// pyRound は round(x, ndigits) (ndigits は 0 以上。x は有限なので誤りは返らない)。
func pyRound(x float64, ndigits int) float64 {
	r, err := pyjson.Round(x, ndigits)
	if err != nil {
		panic(err)
	}
	return r
}

// nominalPatterns は名詞化の言い回しの正規表現 (名前つき) と、どれかが当たるかを 1 回で調べる正規表現 (本家の _any_of。作れなければ nil)。
type nominalPatterns struct {
	names    []string
	patterns []*pyre.Lazy
	combined *pyre.Lazy
}

var (
	nominalOnce  sync.Once
	nominalCache *nominalPatterns
)

func nominalization() *nominalPatterns {
	nominalOnce.Do(func() {
		p := &nominalPatterns{}
		for _, item := range LoadList("nominalization").Items {
			p.names = append(p.names, item.Name)
			p.patterns = append(p.patterns, pyre.NewLazy(item.Regex, 0))
		}
		p.combined = anyOf(p.patterns)
		nominalCache = p
	})
	return nominalCache
}

// anyOf は正規表現の並びのどれかが当たるかを 1 回で調べる正規表現 (本家の _any_of)。捕まえる組や文書全体にかかる印 (?i) などがあって、
// つなぐと意味が変わりうるときは nil (呼ぶ側は 1 つずつ調べる)。「|」は結び付きがいちばん弱いので、つないだ正規表現はどれかが当たる位置で当たる。
func anyOf(patterns []*pyre.Lazy) *pyre.Lazy {
	if len(patterns) == 0 {
		return nil
	}
	sources := make([]string, len(patterns))
	for i, l := range patterns {
		if p := l.Get(); p.Groups() != 0 || p.Flags()&^pyre.UNICODE != 0 {
			return nil
		}
		sources[i] = l.Pattern()
	}
	joined := strings.Join(sources, "|")
	if _, err := pyre.Compile(joined, 0); err != nil {
		return nil
	}
	return pyre.NewLazy(joined, 0)
}

// nominalHit は名詞化の言い回しの当たり (始め, 終わり, 名前)。
type nominalHit struct {
	start, end int
	name       string
}

// nominalHits は本家の _nominal_hits: 重なった当たりは 1 つにする (先に始まり、長いほうを残す)。
func nominalHits(text string) []nominalHit {
	p := nominalization()
	t := pyre.NewText(text)
	if p.combined != nil && mustSearchAt(p.combined, t, 0, pyre.NoEnd) == nil {
		return nil
	}
	var hits []nominalHit
	for i, re := range p.patterns {
		for _, m := range mustFinditer(re, t, 0, pyre.NoEnd) {
			hits = append(hits, nominalHit{m.Start(0), m.End(0), p.names[i]})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].start != hits[j].start {
			return hits[i].start < hits[j].start
		}
		return hits[i].end-hits[i].start > hits[j].end-hits[j].start
	})
	var kept []nominalHit
	lastEnd := -1
	for _, h := range hits {
		if h.start >= lastEnd {
			kept = append(kept, h)
			lastEnd = h.end
		}
	}
	return kept
}

// noChains は本家の _no_chains: 「の」の連続 (名詞 3 つ以上で、3 つとも漢語)。漢数字で始まる語は漢語に数えない。
func noChains(text string) []string {
	var out []string
	for _, m := range mustFinditer(stiffNoChain, pyre.NewText(text), 0, pyre.NoEnd) {
		chain, _ := m.Group(0)
		kango := 0
		for _, part := range strings.Split(chain, "の") {
			t := pyre.NewText(part)
			if matchAt(stiffKango, t, 0) && countFound(stiffKanji, t) >= 2 && !matchAt(stiffNumeralHead, t, 0) {
				kango++
			}
		}
		if kango >= stiffNoKango {
			out = append(out, chain)
		}
	}
	return out
}

func countFound(l *pyre.Lazy, t pyre.Text) int { return len(mustFinditer(l, t, 0, pyre.NoEnd)) }

// stiffValues は 1 つの文の値 (本家の memo に入れる (文の長さ, 漢字, 字数, 名詞化の名前, 「の」の連続の数))。
type stiffValues struct {
	length, kanji, counted int
	names                  []string
	chains                 int
}

func stiffValuesOf(text string) stiffValues {
	v := stiffValues{}
	v.length, v.kanji, v.counted = stiffMeasure(text)
	for _, h := range nominalHits(text) {
		v.names = append(v.names, h.name)
	}
	v.chains = len(noChains(text))
	return v
}

// stiffMetrics は数える文の並びの値 (本家の _metrics_of の戻り)。
type stiffMetrics struct {
	sentences, chars     int
	avgLength, kanjiRate float64
	kanji, nominal       int
	names                map[string]int
	chains               int
}

// metricsOf は本家の _metrics_of。memo (文 → 値) は、同じ文の値を 1 回だけ求める (元と推敲後で変わらない文が多いので、速くなる。結果は同じ)。
func metricsOf(bodies []body, memo map[string]stiffValues) stiffMetrics {
	length, kanji, counted, chains := 0, 0, 0, 0
	names := map[string]int{}
	for _, b := range bodies {
		v, ok := memo[b.text]
		if !ok {
			v = stiffValuesOf(b.text)
			memo[b.text] = v
		}
		length, kanji, counted, chains = length+v.length, kanji+v.kanji, counted+v.counted, chains+v.chains
		for _, name := range v.names {
			names[name]++
		}
	}
	m := stiffMetrics{sentences: len(bodies), chars: length, kanji: kanji, names: names, chains: chains}
	if m.sentences > 0 {
		m.avgLength = pyRound(float64(length)/float64(m.sentences), 2)
	}
	m.kanjiRate = pyRound(stiffRatio(kanji, counted), 2)
	for _, n := range names {
		m.nominal += n
	}
	return m
}

func (m stiffMetrics) value() pyjson.Value {
	names := pyjson.NewObject()
	for _, name := range slices.Sorted(maps.Keys(m.names)) {
		names.Set(name, int64(m.names[name]))
	}
	o := pyjson.NewObject()
	o.Set("sentences", int64(m.sentences))
	o.Set("chars", int64(m.chars))
	o.Set("avg_sentence_length", m.avgLength)
	o.Set("kanji_ratio", m.kanjiRate)
	o.Set("kanji", int64(m.kanji))
	o.Set("nominalizations", int64(m.nominal))
	o.Set("nominalization_names", names)
	o.Set("no_chains", int64(m.chains))
	return o
}

// StiffnessMetrics は本家の stiffness_metrics (1 つの文書の値。コード、URL、表、見出しは数えない)。
func StiffnessMetrics(text string) (pyjson.Value, error) {
	s, err := newSides(text, text)
	if err != nil {
		return nil, err
	}
	return metricsOf(s.allBodies(0), map[string]stiffValues{}).value(), nil
}

// 抜き出しの区切り (本家の _EXCERPT_BREAK)。
const stiffExcerptBreak = "、。，！？!?"

// ChangedExcerpt は本家の _changed_excerpt: 2 つの文の変わった所を、読点か句点で区切った節の単位で出す (最初に変わった節から最後に変わった節まで)。
func ChangedExcerpt(oldText, newText string, width int) (string, string) {
	a, b := pyunicode.Runes(oldText), pyunicode.Runes(newText)
	head, tail := commonAffix(a, b)
	clause := func(text []rune, left, right int) string {
		for left > 0 && !strings.ContainsRune(stiffExcerptBreak, text[left-1]) {
			left--
		}
		for right < len(text) && (right == 0 || !strings.ContainsRune(stiffExcerptBreak, text[right-1])) && !strings.ContainsRune(stiffExcerptBreak, text[right]) {
			right++
		}
		part := []rune(pystr.StripChars(pyunicode.FromRunes(text[left:right]), "、，"))
		if len(part) <= width {
			return string(part)
		}
		return string(part[:width]) + "…"
	}
	return clause(a, head, max(head, len(a)-tail)), clause(b, head, max(head, len(b)-tail))
}

// stiffPair は変わった文の組。
type stiffPair struct{ o, n body }

// stiffSentence は組の 1 文の値 (本家の sentence_values: 元の長さ・後の長さ・元の漢字の割合・後の漢字の割合・元の漢字・後の漢字)。
type stiffSentence struct {
	ol, nl   int
	orr, nrr float64
	ok, nk   int
}

func stiffCandidate(subkind string, oldLine, newLine *int, oldText, newText *string, detail *pyjson.Object, level string) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("kind", "stiffness")
	o.Set("subkind", subkind)
	o.Set("old_line", optionalInt(oldLine))
	o.Set("new_line", optionalInt(newLine))
	o.Set("old", pyjson.OptionalString(oldText))
	o.Set("new", pyjson.OptionalString(newText))
	o.Set("detail", detail)
	o.Set("level", level)
	return o
}

func intPtr(n int) *int { return &n }

// stiffnessResult は _stiffness の戻り (JSON の stiffness と同じ形)。list_problem はいつも null (埋め込みの一覧は読めない経路が無い)。
func stiffnessResult(level string, candidates []pyjson.Value, oldM, newM stiffMetrics) pyjson.Value {
	metrics := pyjson.NewObject()
	metrics.Set("old", oldM.value())
	metrics.Set("new", newM.value())
	o := pyjson.NewObject()
	o.Set("level", level)
	o.Set("candidate_count", int64(len(candidates)))
	o.Set("candidates", candidates)
	o.Set("metrics", metrics)
	o.Set("list", ListSource("nominalization"))
	o.Set("list_problem", nil)
	return o
}

// positiveNames は a の数から b の数を引いて、正になる名前の昇順 (本家の Counter の subtract と sorted)。
func positiveNames(a, b map[string]int) []string {
	diff := map[string]int{}
	for k, v := range a {
		diff[k] += v
	}
	for k, v := range b {
		diff[k] -= v
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(diff)) {
		if diff[k] > 0 {
			out = append(out, k)
		}
	}
	return out
}

func countNames(hits []nominalHit) map[string]int {
	m := map[string]int{}
	for _, h := range hits {
		m[h.name]++
	}
	return m
}

// Stiffness は本家の stiffness (文体の硬化の検査。docType は文書の種類。incident・formal・paper なら参考)。
func Stiffness(orig, rw string, docType *string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	dt, err := visibility.NormalizeDocType(docType)
	if err != nil {
		return nil, err
	}
	return s.stiffness(full, groups, dt), nil
}

func (s *sides) stiffness(full *fullDiff, groups []*sentenceGroup, docType *string) pyjson.Value {
	level := "review"
	if docType != nil && slices.Contains(stiffInfoDocTypes, *docType) {
		level = "info"
	}
	maskers := [2]*masker{s.newMasker(0), s.newMasker(1)}
	oldBodies := s.allBodiesWith(0, maskers[0])
	memo := map[string]stiffValues{}
	oldM := metricsOf(oldBodies, memo)
	if len(full.hunks) == 0 {
		return stiffnessResult(level, []pyjson.Value{}, oldM, oldM)
	}
	newBodies := s.allBodiesWith(1, maskers[1])
	newM := metricsOf(newBodies, memo)

	// 文の組 (変わった文だけ)。組にならない文は、置き換えの塊ごとにまとめて 1 つの組にする (文全体を書き換えた推敲)
	var pairs []stiffPair
	for _, g := range groups {
		for _, p := range g.pairs {
			o, ok1 := maskers[0].body(g.old[p[0]])
			n, ok2 := maskers[1].body(g.new[p[1]])
			if !ok1 || !ok2 || o.raw == n.raw {
				continue
			}
			pairs = append(pairs, stiffPair{o, n})
		}
		if g.hunk.tag != "replace" {
			continue
		}
		pairedOld, pairedNew := map[int]bool{}, map[int]bool{}
		for _, p := range g.pairs {
			pairedOld[p[0]], pairedNew[p[1]] = true, true
		}
		var restOld, restNew []body
		for k, sn := range g.old {
			if b, ok := maskers[0].body(sn); ok && !pairedOld[k] {
				restOld = append(restOld, b)
			}
		}
		for k, sn := range g.new {
			if b, ok := maskers[1].body(sn); ok && !pairedNew[k] {
				restNew = append(restNew, b)
			}
		}
		if len(restOld) > 0 && len(restNew) > 0 {
			pairs = append(pairs, stiffPair{joinBodies(restOld), joinBodies(restNew)})
		}
	}
	sortedPairs := slices.Clone(pairs)
	sort.SliceStable(sortedPairs, func(i, j int) bool { return sortedPairs[i].n.start < sortedPairs[j].n.start })

	values := map[[4]int]stiffSentence{}
	sentenceValues := func(p stiffPair) stiffSentence {
		key := [4]int{p.o.start, p.n.start, pystr.Len(p.o.text), pystr.Len(p.n.text)}
		if v, ok := values[key]; ok {
			return v
		}
		ol, ok, oc := stiffMeasure(p.o.text)
		nl, nk, nc := stiffMeasure(p.n.text)
		v := stiffSentence{ol, nl, stiffRatio(ok, oc), stiffRatio(nk, nc), ok, nk}
		values[key] = v
		return v
	}

	var out []pyjson.Value
	type lineKey struct {
		line int
		raw  string
	}
	reported := map[lineKey]bool{}
	// 1 文の条件: 漢字の割合 +8 ポイント以上、漢字 +2 字以上、平仮名 -2 字以上
	for _, p := range sortedPairs {
		v := sentenceValues(p)
		if v.nrr-v.orr < stiffSentKanjiPt || v.nk-v.ok < stiffSentKanjiUp {
			continue
		}
		if hiraganaCount(p.o.text)-hiraganaCount(p.n.text) < stiffSentHiraDown {
			continue
		}
		reported[lineKey{p.n.line, p.n.raw}] = true
		detail := pyjson.NewObject()
		detail.Set("old_kanji_ratio", pyRound(v.orr, 1))
		detail.Set("new_kanji_ratio", pyRound(v.nrr, 1))
		detail.Set("old_length", int64(v.ol))
		detail.Set("new_length", int64(v.nl))
		out = append(out, stiffCandidate("sentence", intPtr(p.o.line), intPtr(p.n.line), &p.o.raw, &p.n.raw, detail, level))
	}
	// 1 文の名詞化: 変わった文に、組の原文の文になかった名詞化の言い回しが出て、漢字が 2 字以上増えた
	for _, p := range sortedPairs {
		names := positiveNames(countNames(nominalHits(p.n.text)), countNames(nominalHits(p.o.text)))
		if len(names) == 0 {
			continue
		}
		v := sentenceValues(p)
		if v.nk-v.ok < stiffSentNominalKanji || reported[lineKey{p.n.line, p.n.raw}] {
			continue
		}
		reported[lineKey{p.n.line, p.n.raw}] = true
		detail := pyjson.NewObject()
		detail.Set("added_names", pyjson.Strings(names))
		detail.Set("old_kanji", int64(v.ok))
		detail.Set("new_kanji", int64(v.nk))
		out = append(out, stiffCandidate("sentence_nominal", intPtr(p.o.line), intPtr(p.n.line), &p.o.raw, &p.n.raw, detail, level))
	}
	// 文書全体: 漢字の割合 +3 ポイント以上、かつ漢字 +2 字以上。変わった文の組が 2 つ以上のときだけ出す。組が 1 つなら文書全体の値はその 1 文の値と同じ向きに
	// 動き、1 文の条件より緩い条件になる (1 文の書き換えは 1 文の条件と名詞化の条件で見る)
	if len(pairs) >= 2 && newM.kanjiRate-oldM.kanjiRate >= stiffDocKanjiPt && newM.kanji-oldM.kanji >= stiffDocKanjiUp {
		best := pairs[0]
		bestGain := sentenceValues(best).nrr - sentenceValues(best).orr
		for _, p := range pairs[1:] {
			if gain := sentenceValues(p).nrr - sentenceValues(p).orr; gain > bestGain {
				best, bestGain = p, gain
			}
		}
		a, b := ChangedExcerpt(best.o.text, best.n.text, 40)
		example := pyjson.NewObject()
		example.Set("old_line", int64(best.o.line))
		example.Set("new_line", int64(best.n.line))
		example.Set("old", a)
		example.Set("new", b)
		detail := pyjson.NewObject()
		detail.Set("old_kanji_ratio", oldM.kanjiRate)
		detail.Set("new_kanji_ratio", newM.kanjiRate)
		detail.Set("old_avg_length", oldM.avgLength)
		detail.Set("new_avg_length", newM.avgLength)
		detail.Set("example", example)
		out = append(out, stiffCandidate("document", nil, nil, nil, nil, detail, level))
	}
	// 名詞化の言い回し: 文書全体で原文より 2 つ以上増えた
	if newM.nominal-oldM.nominal >= stiffNominalDelta {
		names := positiveNames(newM.names, oldM.names)
		var where []int
		oldRaws := map[string]bool{}
		for _, b := range oldBodies {
			oldRaws[b.raw] = true
		}
		for _, b := range newBodies {
			if !oldRaws[b.raw] && !slices.Contains(where, b.line) && len(nominalHits(b.text)) > 0 {
				where = append(where, b.line)
			}
		}
		var first *int
		if len(where) > 0 {
			first = intPtr(where[0])
		}
		lines := make([]pyjson.Value, len(where))
		for i, w := range where {
			lines[i] = int64(w)
		}
		detail := pyjson.NewObject()
		detail.Set("old", int64(oldM.nominal))
		detail.Set("new", int64(newM.nominal))
		detail.Set("added_names", pyjson.Strings(names))
		detail.Set("new_lines", lines)
		out = append(out, stiffCandidate("nominalization", nil, first, nil, nil, detail, level))
	}
	// 「の」の連続が、原文になかった所に出た
	oldChains := map[string]bool{}
	oldRawSet := map[string]bool{}
	for _, b := range oldBodies {
		oldRawSet[b.raw] = true
		for _, c := range noChains(b.text) {
			oldChains[c] = true
		}
	}
	pairedOld := map[int]body{}
	for _, p := range pairs {
		if _, ok := pairedOld[p.n.start]; !ok {
			pairedOld[p.n.start] = p.o
		}
	}
	for _, g := range groups {
		for _, sn := range g.new {
			n, ok := maskers[1].body(sn)
			if !ok {
				continue
			}
			chains := noChains(n.text)
			if len(chains) == 0 {
				continue
			}
			var fresh []string
			o, paired := pairedOld[n.start]
			if paired {
				if o.raw == n.raw {
					continue
				}
				before := noChains(o.text)
				// 連続の名詞がすべて組の原文の文にあれば出さない (補足カッコを外しただけの推敲。名詞は「の」を含まない)
				for _, chain := range chains {
					if !slices.Contains(before, chain) && !allIn(strings.Split(chain, "の"), o.text) {
						fresh = append(fresh, chain)
					}
				}
				if len(chains) <= len(before) || len(fresh) == 0 {
					continue
				}
			} else {
				if oldRawSet[n.raw] {
					continue
				}
				for _, chain := range chains {
					if !oldChains[chain] {
						fresh = append(fresh, chain)
					}
				}
				if len(fresh) == 0 {
					continue
				}
			}
			detail := pyjson.NewObject()
			detail.Set("chains", pyjson.Strings(fresh))
			var oldLine *int
			var oldRaw *string
			if paired {
				oldLine, oldRaw = intPtr(o.line), &o.raw
			}
			out = append(out, stiffCandidate("no_chain", oldLine, intPtr(n.line), oldRaw, &n.raw, detail, level))
		}
	}
	return stiffnessResult(level, out, oldM, newM)
}

func allIn(parts []string, text string) bool {
	for _, p := range parts {
		if !strings.Contains(text, p) {
			return false
		}
	}
	return true
}

// joinBodies は組にならなかった文を 1 つにつなげた文 (置き換えの塊ごとの組。行と位置は先頭の文のもの)。
func joinBodies(list []body) body {
	var text, raw strings.Builder
	for _, b := range list {
		text.WriteString(b.text)
		raw.WriteString(b.raw)
	}
	return body{text: text.String(), raw: raw.String(), line: list[0].line, start: list[0].start}
}
