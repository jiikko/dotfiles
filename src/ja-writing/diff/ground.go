package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 資料との照合 (本家の ground と、使う _Doc・_Source・_ground_*)。check がプロセスの中で呼ぶ。照らすのは数値・名前・語・確度の語だけで、
// 主張・条件・担当が資料どおりかは判定しない。位置と長さは全部コードポイント (本家の str の添字)。

var (
	// 名前 (製品名や識別子)。NFKC のあとの英数字 2 字以上
	groundName = pyre.NewLazy(`[A-Za-z][A-Za-z0-9_.+#/-]*[A-Za-z0-9]`, 0)
	// URL の頭 (https:// など) は名前にしない。頭の長さに上限を置く (「a.b.c.d…」のような長い並びで 2 乗の時間にしない)
	groundURLScheme = pyre.NewLazy(`\b[A-Za-z][A-Za-z0-9+.-]{0,31}://`, 0)
	// 語 (漢字 2 字以上と片仮名 2 字以上)
	groundWord = pyre.NewLazy(`[一-龥々〆ヵヶ]{2,}|[ァ-ヴー]{2,}`, 0)
	// 確度の語。「はず」は「はずれ」「はずす」の頭を除き、「未定」は「未定義」を、「確定」は「確定する」「確定申告」を除く
	certaintyWords = []string{"未確認", "未定", "予定", "検討中", "調査中", "見込み", "可能性", "かもしれ", "はず", "確認済み", "確定", "暫定"}
	certaintyNot   = map[string]string{"はず": "(?![れさしすせ])", "未定": "(?!義)", "確定": "(?!する|申告)"}
	certainty      = pyre.NewLazyFrom(certaintySource, 0)
	// 照合だけに足す確度の語 (diff の確度の判定と候補の数は変えない)
	groundCertainty = pyre.NewLazyFrom(func() string {
		return certaintySource() + `|たぶん|多分|おそらく|恐らく|きっと|と思います|と思う|気がする|記憶では|方向で|検討して|まだ決まって|確かめていない` +
			"|" + typeKindPattern("推量").Pattern()
	}, pyre.MULTILINE)
	// 「名前 = 30」「timeout: 30」の形の設定の行 (資料にない数値の手がかり)
	bareSetting = pyre.NewLazy(`^[ \t]*[+-]?[ \t]*[^\W\d]\w*[ \t]*[=:][ \t]*`+
		`([0-9]+(?:\.[0-9]+)?)(?=[ \t]*(?:$|[#;]))`, 0)
	// 「ある」の判定の形から除く空白と強調の記号「*」
	squashed = pyre.NewLazy(`[\s*]+`, 0)
	// 照合だけで使う単位。msec を ms より先に読み、英単語の途中の s / min などは単位にしない
	groundUnit = pyre.NewLazyFrom(func() string {
		return `(?:secs|sec|msec|mins|min|ms|s)(?![A-Za-z])|ミリ秒|` + unitPatternSource(numberUnits)
	}, 0)
	groundSameUnit = map[string]string{"s": "秒", "sec": "秒", "secs": "秒", "ms": "ミリ秒", "msec": "ミリ秒", "min": "分", "mins": "分"}
	// 名前と語を取る前に伏せる protected_spans の種類 (Markdown や HTML の書き方の語を拾わない)
	groundMasked    = []string{"frontmatter", "html_block", "html_token"}
	groundNotJudged = []string{"claims", "conditions", "assignees", "meaning_equivalence"}
	groundKindOrder = map[string]int{"number": 0, "name": 1, "word": 2, "certainty": 3}
)

// certaintySource は本家の _CERTAINTY のパターン (長い語を先に並べ、同じ長さは元の順のまま)。
func certaintySource() string {
	words := slices.Clone(certaintyWords)
	sort.SliceStable(words, func(i, j int) bool { return pystr.Len(words[i]) > pystr.Len(words[j]) })
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = pyre.Escape(w) + certaintyNot[w]
	}
	return strings.Join(parts, "|")
}

func typeKindPattern(name string) *pyre.Lazy {
	for _, k := range typeKinds {
		if k.name == name {
			return k.pattern
		}
	}
	panic("文の種類 " + name + " は無い")
}

// groundDoc は 1 つの文書の行・解析・文 (本家の _Doc。_Sides の片側だけを使う)。
type groundDoc struct {
	*sides
	found  []sentence
	starts []int
	ready  bool
}

func newGroundDoc(text string) *groundDoc {
	s, err := newSides(text, "")
	if err != nil {
		panic(pyException{err})
	}
	return &groundDoc{sides: s}
}

// sentenceAt は位置を含む文 (意味が変わる恐れと同じ分け方)。文に分けない行 (コードなど) なら、その行を丸ごと返す。
func (d *groundDoc) sentenceAt(position int) string {
	if !d.ready {
		d.found = d.lineSentences(0, 0, len(d.lines[0]))
		d.starts = make([]int, len(d.found))
		for i, sn := range d.found {
			d.starts[i] = sn.start
		}
		d.ready = true
	}
	index := bisectRight(d.starts, position) - 1
	if index >= 0 && position < d.found[index].start+pystr.Len(d.found[index].text) {
		return d.found[index].text
	}
	return pystr.Strip(d.lines[0][d.lineOf(0, position)-1])
}

func (d *groundDoc) text() []rune { return d.texts[0] }

// squash は「ある」の判定に使う形 (NFKC にして空白と強調の記号「*」を除く。資料で太字で分かれた語も同じとみなす)。
func squash(text string) string {
	return squashed.MustSub("", pyunicode.Normalize(pyunicode.NFKC, text))
}

// groundMask は名前と語を取り出す文字列。全角を半角にし、先頭の設定と HTML を同じ長さの空白にする (位置は変えない)。
func groundMask(text []rune, a *visibility.Analysis) []rune {
	chars := make([]rune, len(text))
	for i, r := range text {
		chars[i] = halfWidth(r)
	}
	for _, span := range a.ProtectedSpans {
		if slices.Contains(groundMasked, span.Kind) {
			for index := span.Start; index < min(span.End, len(chars)); index++ {
				if chars[index] != '\n' {
					chars[index] = ' '
				}
			}
		}
	}
	return pyunicode.Runes(mustSubFunc(groundURLScheme, runesText(chars), blankMatch))
}

// groundNumberKey は照合用の比べる値。小数部が 0 だけのときだけ整数にし、非 0 の末尾やバージョンの番号は削らない。
func groundNumberKey(key numKey) numKey {
	value := key.value
	if whole, fraction, dot := strings.Cut(value, "."); dot && fraction != "" && strings.Trim(fraction, "0") == "" {
		value = whole
	}
	return numKey{value, groundSame(key.unit)}
}

func groundSame(unit string) string {
	if s, ok := groundSameUnit[unit]; ok {
		return s
	}
	return unit
}

// groundComparisonNumbers は表示用の値を変えずに、範囲・日付・時刻のまとまりと空白の手がかりも含めた比べる用の写しを作る。
func groundComparisonNumbers(numbers []number) []number {
	out := make([]number, len(numbers))
	for i, n := range numbers {
		key := groundNumberKey(n.key())
		c := n
		c.value, c.unit = key.value, key.unit
		if n.cluster != nil {
			c.cluster = make([]numKey, len(n.cluster))
			for j, k := range n.cluster {
				c.cluster[j] = groundNumberKey(k)
			}
		}
		if n.notation != nil {
			nt := &notation{follow: n.notation.follow}
			for _, v := range n.notation.group {
				nt.group = append(nt.group, groundNumberKey(numKey{v, ""}).value)
			}
			follow := pyunicode.Runes(nt.follow)
			if m := mustMatch(groundUnit, runesText(follow), 0); m != nil {
				unit, _ := m.Group(0)
				nt.follow = groundSame(unit) + pyunicode.FromRunes(follow[m.End(0):])
			}
			if f := n.notation.frame; f != nil {
				units := make([]string, len(f.units))
				for j, u := range f.units {
					units[j] = groundSame(u)
				}
				nt.frame = &notationFrame{joins: f.joins, units: units, at: f.at}
			}
			c.notation = nt
		}
		out[i] = c
	}
	return out
}

// groundKey は照合の項目の (種類, 比べる形)。数値は number、ほかは text。
type groundKey struct {
	kind   string
	number numKey
	text   string
}

// groundEntry は最終稿の項目 1 つ (本家の _ground_items の dict)。
type groundEntry struct {
	key         groundKey
	text        string
	value, unit *string
	lines       []int
	start       int
}

// groundItems は最終稿の項目を出てきた順に取り出す。
func groundItems(doc *groundDoc, numbers []number) []*groundEntry {
	text := doc.text()
	index := map[groundKey]*groundEntry{}
	var found []*groundEntry
	add := func(key groundKey, start, end int, value, unit *string) {
		line := doc.lineOf(0, start)
		if e, ok := index[key]; !ok {
			e = &groundEntry{key: key, text: runeSlice(text, start, end), value: value, unit: unit, lines: []int{line}, start: start}
			index[key] = e
			found = append(found, e)
		} else if e.lines[len(e.lines)-1] != line {
			e.lines = append(e.lines, line)
		}
	}
	for _, n := range numbers {
		value, unit := n.value, n.unit
		add(groundKey{kind: "number", number: n.key()}, n.start, n.end, &value, &unit)
	}
	masked := groundMask(text, doc.analyses[0])
	for _, m := range groundNames(masked) {
		word, _ := m.Group(0)
		add(groundKey{kind: "name", text: squash(word)}, m.Start(0), m.End(0), nil, nil)
	}
	for _, m := range mustFinditer(certainty, doc.docText(0), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		add(groundKey{kind: "certainty", text: word}, m.Start(0), m.End(0), nil, nil)
	}
	starts := make([]int, len(numbers))
	for i, n := range numbers {
		starts[i] = n.start
	}
	for _, m := range mustFinditer(groundWord, runesText(masked), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		if slices.Contains(certaintyWords, word) {
			continue
		}
		// 数値として取った範囲 (三日、十二月) に重なる語は、語として照らさない (数値で照らす)
		if i := bisectRight(starts, m.End(0)-1) - 1; i >= 0 && numbers[i].end > m.Start(0) {
			continue
		}
		add(groundKey{kind: "word", text: word}, m.Start(0), m.End(0), nil, nil)
	}
	return found
}

// groundNames は名前 (英数字 2 字以上)。数字のすぐあとの英字 (200ms の ms、8GiB の GiB) は単位なので名前にしない。
func groundNames(masked []rune) []*pyre.Match {
	var out []*pyre.Match
	for _, m := range mustFinditer(groundName, runesText(masked), 0, pyre.NoEnd) {
		if m.Start(0) > 0 && pyunicode.IsDigit(masked[m.Start(0)-1]) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// groundFinal は最終稿の数値の比べる形 (本家の (_KeySet, _notation_index) の組)。
type groundFinal struct {
	set      *keySet
	notation map[numKey][]number
}

// groundSource は照らす文 (原文か資料) の比べる形 (本家の _Source)。
type groundSource struct {
	text        []rune
	foundNums   []number
	numbers     *keySet
	notation    map[numKey][]number
	squashed    string
	names       map[string]bool
	words       map[string]bool
	lines       []string
	numberLines map[numKey]map[int]bool
	numberRows  map[string][]numberRow
	nameLines   map[string]map[int]bool
	bareLines   map[string][]int
	certainties []string
}

type numberRow struct {
	number number
	line   int
}

func newGroundSource(text string) *groundSource {
	s, err := newSides(text, "")
	if err != nil {
		panic(pyException{err})
	}
	runes := s.texts[0]
	src := &groundSource{text: runes, lines: s.lines[0], names: map[string]bool{}, words: map[string]bool{},
		numberLines: map[numKey]map[int]bool{}, numberRows: map[string][]numberRow{}, nameLines: map[string]map[int]bool{}, bareLines: map[string][]int{}}
	src.foundNums = findNumbersWith(runes, s.analyses[0], numberMaskedKinds, groundUnit)
	comparison := groundComparisonNumbers(src.foundNums)
	src.numbers = newKeySet(comparison)
	src.notation = notationIndex(comparison)
	src.squashed = squash(pyunicode.FromRunes(runes))
	// 名前と語は集合で引き、ないときだけ部分文字列を探す (項目ごとに資料の全文を探すと「項目の数×資料の字数」の時間になる)
	masked := groundMask(runes, s.analyses[0])
	names := groundNames(masked)
	for _, m := range names {
		word, _ := m.Group(0)
		src.names[squash(word)] = true
	}
	for _, m := range mustFinditer(groundWord, runesText(masked), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		src.words[word] = true
	}
	for _, m := range mustFinditer(certainty, s.docText(0), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		src.words[word] = true
	}
	starts := s.starts[0]
	for _, n := range comparison {
		line := bisectRight(starts, n.start)
		addLine(src.numberLines, n.key(), line)
		src.numberRows[n.value] = append(src.numberRows[n.value], numberRow{n, line})
	}
	for _, m := range names {
		word, _ := m.Group(0)
		addLine(src.nameLines, squash(word), bisectRight(starts, m.Start(0)))
	}
	seen := map[string]bool{}
	for _, m := range mustFinditer(groundCertainty, s.docText(0), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		if !seen[word] {
			seen[word] = true
			src.certainties = append(src.certainties, word)
		}
	}
	for line, body := range src.lines {
		if m := mustMatch(bareSetting, pyre.NewText(pyunicode.Normalize(pyunicode.NFKC, body)), 0); m != nil {
			digits, _ := m.Group(1)
			value := groundNumberKey(numKey{digits, ""}).value
			src.bareLines[value] = append(src.bareLines[value], line+1)
		}
	}
	return src
}

func addLine[K comparable](m map[K]map[int]bool, key K, line int) {
	if m[key] == nil {
		m[key] = map[int]bool{}
	}
	m[key][line] = true
}

func sortedLines(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for line := range set {
		out = append(out, line)
	}
	sort.Ints(out)
	return out
}

// evidence は資料の行の手がかり [資料の番号, 行番号, 行の頭 60 字]。
func (s *groundSource) evidence(index, line int) pyjson.Value {
	body := pyunicode.Runes(strings.TrimRight(s.lines[line-1], "\r\n"))
	return []pyjson.Value{int64(index), int64(line), pyunicode.FromRunes(pySlice(body, 0, 60))}
}

func nameBoundary(key string) *pyre.Pattern {
	return pyre.MustCompile(`(?<![A-Za-z0-9])`+pyre.Escape(key)+`(?![A-Za-z0-9])`, 0)
}

func mustSearch(p *pyre.Pattern, text string) bool {
	m, err := p.Search(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

func (s *groundSource) matchingLines(key groundKey, final groundFinal) []int {
	if key.kind == "name" {
		if set, ok := s.nameLines[key.text]; ok {
			return sortedLines(set)
		}
		pattern := nameBoundary(key.text)
		var out []int
		for i, text := range s.lines {
			if mustSearch(pattern, squash(text)) {
				out = append(out, i+1)
			}
		}
		return out
	}
	k := groundNumberKey(key.number)
	lines := map[int]bool{}
	for line := range s.numberLines[k] {
		lines[line] = true
	}
	for _, row := range s.numberRows[k.value] {
		if k.unit != "" {
			unit := k.unit
			if row.number.unit == "" && notationMatch(row.number, final.set, &unit) {
				lines[row.line] = true
			}
		} else if row.number.unit != "" {
			unit := row.number.unit
			for _, other := range final.notation[k] {
				if notationMatch(other, s.numbers, &unit) {
					lines[row.line] = true
					break
				}
			}
		}
	}
	return sortedLines(lines)
}

func (s *groundSource) has(key groundKey, final groundFinal) bool {
	switch key.kind {
	case "number":
		return s.hasNumber(key.number, final)
	case "name":
		// 名前は英数字の境目で照らす (Java を JavaScript の中に、SQL を PostgreSQL の中に見つけない)
		return s.names[key.text] || mustSearch(nameBoundary(key.text), s.squashed)
	}
	return s.words[key.text] || strings.Contains(s.squashed, key.text)
}

// hasNumber は最終稿の値 key が、この文にあるか。同じ数の「単位なし」と「単位あり」は、diff の組の文と同じ条件でだけ表記の違いとみなす。
func (s *groundSource) hasNumber(key numKey, final groundFinal) bool {
	k := groundNumberKey(key)
	if s.numbers.keys[k] {
		return true
	}
	if k.unit != "" {
		unit := k.unit
		for _, n := range s.notation[numKey{k.value, ""}] {
			if notationMatch(n, final.set, &unit) {
				return true
			}
		}
		return false
	}
	for _, n := range final.notation[k] {
		if notationMatch(n, s.numbers, nil) {
			return true
		}
	}
	return false
}

func groundEvidence(sources []*groundSource, key groundKey, final groundFinal, indices []int) []pyjson.Value {
	rows := []pyjson.Value{}
	for _, index := range indices {
		source := sources[index-1]
		for _, line := range source.matchingLines(key, final) {
			rows = append(rows, source.evidence(index, line))
			if len(rows) == 3 {
				return rows
			}
		}
	}
	return rows
}

func groundItem(kind, text string, value, unit *string, lines []int, classification string) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("kind", kind)
	o.Set("text", text)
	o.Set("value", pyjson.OptionalString(value))
	o.Set("unit", pyjson.OptionalString(unit))
	o.Set("final_lines", intsValue(lines))
	o.Set("classification", classification)
	return o
}

// groundValueCertainty は資料の確度の語 (最終稿にないもの) と、最終稿で言い切った値の資料の行にある確度の語を、dropped_certainty の項目に足す。
func groundValueCertainty(items *[]*pyjson.Object, doc *groundDoc, found []*groundEntry, sources []*groundSource, final groundFinal) {
	if !slices.ContainsFunc(sources, func(s *groundSource) bool { return len(s.certainties) > 0 }) {
		return
	}
	dropped := map[string]*pyjson.Object{}
	for _, item := range *items {
		if item.Field("classification") == "dropped_certainty" {
			dropped[item.Field("text").(string)] = item
		}
	}
	add := func(word string, value *string) {
		if _, ok := dropped[word]; !ok {
			item := groundItem("certainty", word, nil, nil, nil, "dropped_certainty")
			dropped[word] = item
			*items = append(*items, item)
		}
		if value != nil {
			item := dropped[word]
			values, ok := item.Field("values").([]pyjson.Value)
			if !ok {
				values = []pyjson.Value{}
			}
			if !slices.Contains(values, pyjson.Value(*value)) {
				values = append(values, *value)
			}
			item.Set("values", values)
		}
	}
	finalWords := map[string]bool{}
	for _, m := range mustFinditer(groundCertainty, doc.docText(0), 0, pyre.NoEnd) {
		word, _ := m.Group(0)
		finalWords[word] = true
	}
	for _, source := range sources {
		for _, word := range source.certainties {
			if !finalWords[word] {
				add(word, nil)
			}
		}
	}
	// 値が繰り返される場合も、言い切った文を 1 つでも含めば見せる
	occurrences := map[numKey][]int{}
	for _, n := range findNumbersWith(doc.text(), doc.analyses[0], numberMaskedKinds, groundUnit) {
		occurrences[n.key()] = append(occurrences[n.key()], n.start)
	}
	for _, entry := range found {
		if entry.key.kind != "number" {
			continue
		}
		starts, ok := occurrences[entry.key.number]
		if !ok {
			starts = []int{entry.start}
		}
		if !slices.ContainsFunc(starts, func(start int) bool { return !groundCertainty.MustSearch(doc.sentenceAt(start)) }) {
			continue
		}
		for _, source := range sources {
			if !source.has(entry.key, final) {
				continue
			}
			for _, line := range source.matchingLines(entry.key, final) {
				for _, m := range mustFinditer(groundCertainty, pyre.NewText(source.lines[line-1]), 0, pyre.NoEnd) {
					word, _ := m.Group(0)
					text := entry.text
					add(word, &text)
				}
			}
		}
	}
}

// Ground は本家の ground (資料との照合)。final は最終稿、materials は資料の並び、original は原文 (推敲のとき。nil なら新規作成)、labels は資料の出典
// (nil なら全部 null。nil でなく数が materials と違えば、本家の ValueError と同じ文の error)。照合の JSON (attempt-<n>.materials.json の形) を返す。
// 本家が例外になる入力 (桁数の上限を超える数) は *visibility.Error を返す。
func Ground(final string, materials []string, original *string, labels []*string) (result *pyjson.Object, err error) {
	defer recoverPyException(&err)
	if labels == nil {
		labels = make([]*string, len(materials))
	}
	if len(labels) != len(materials) {
		return nil, &visibility.Error{Type: visibility.TypeValueError, Message: "資料と出典の数が違います"}
	}
	return ground(final, materials, original, labels), nil
}

func ground(final string, materials []string, original *string, labels []*string) *pyjson.Object {
	mode := "compose"
	if original != nil {
		mode = "rewrite"
	}
	doc := newGroundDoc(final)
	var originalSource *groundSource
	if original != nil {
		originalSource = newGroundSource(*original)
	}
	materialSources := make([]*groundSource, len(materials))
	for i, text := range materials {
		materialSources[i] = newGroundSource(text)
	}
	sources := []pyjson.Value{}
	if original != nil {
		o := pyjson.NewObject()
		o.Set("role", "source")
		o.Set("index", int64(1))
		o.Set("chars", int64(pystr.Len(*original)))
		sources = append(sources, o)
	}
	for i, text := range materials {
		o := pyjson.NewObject()
		o.Set("role", "material")
		o.Set("index", int64(i+1))
		o.Set("chars", int64(pystr.Len(text)))
		o.Set("label", pyjson.OptionalString(labels[i]))
		sources = append(sources, o)
	}

	var items []*pyjson.Object
	type pendingRisk struct {
		start int
		risk  *pyjson.Object
	}
	var risks []pendingRisk
	finalFound := findNumbersWith(doc.text(), doc.analyses[0], numberMaskedKinds, groundUnit)
	found := groundItems(doc, finalFound)
	comparison := groundComparisonNumbers(finalFound)
	finalNumbers := groundFinal{newKeySet(comparison), notationIndex(comparison)}
	// 数量を主張しない「一つ」(一つの、一つずつ、もう一つ) だけの値は、資料にない数値の候補にしない
	strong := map[numKey]bool{}
	for _, n := range finalFound {
		if n.weak == weakNone {
			strong[n.key()] = true
		}
	}
	ordered := slices.Clone(found)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := groundKindOrder[ordered[i].key.kind], groundKindOrder[ordered[j].key.kind]
		return a < b || a == b && ordered[i].start < ordered[j].start
	})
	for _, entry := range ordered {
		kind := entry.key.kind
		var classification string
		switch {
		case originalSource != nil && originalSource.has(entry.key, finalNumbers):
			classification = "in_original"
		case slices.ContainsFunc(materialSources, func(s *groundSource) bool { return s.has(entry.key, finalNumbers) }):
			classification = "from_materials"
		default:
			classification = "not_in_sources"
		}
		item := groundItem(kind, entry.text, entry.value, entry.unit, entry.lines, classification)
		items = append(items, item)
		switch {
		case classification == "from_materials":
			var in []int
			for i, s := range materialSources {
				if s.has(entry.key, finalNumbers) {
					in = append(in, i+1)
				}
			}
			item.Set("found_in", intsValue(in))
			if kind == "number" || kind == "name" {
				item.Set("evidence", groundEvidence(materialSources, entry.key, finalNumbers, in))
			}
		case classification == "not_in_sources" && kind == "number":
			var bare []pyjson.Value
			value := groundNumberKey(entry.key.number).value
			for i, s := range materialSources {
				for _, line := range s.bareLines[value] {
					bare = append(bare, s.evidence(i+1, line))
				}
				if len(bare) >= 3 {
					break
				}
			}
			if len(bare) > 0 {
				item.Set("bare_in", bare[:min(3, len(bare))])
			}
		}
		if classification != "not_in_sources" {
			continue
		}
		var detail *pyjson.Object
		switch {
		case kind == "number" && mode == "compose" && strong[entry.key.number]:
			detail = pyjson.NewObject()
			detail.Set("text", entry.text)
			detail.Set("value", pyjson.OptionalString(entry.value))
			detail.Set("unit", pyjson.OptionalString(entry.unit))
		case kind == "name":
			detail = pyjson.NewObject()
			detail.Set("text", entry.text)
		default:
			continue
		}
		risks = append(risks, pendingRisk{entry.start, newRisk("unsupported", kind, 0, entry.lines[0], "", doc.sentenceAt(entry.start), false, true, detail)})
	}
	finalCertainty := map[string]bool{}
	for _, entry := range found {
		if entry.key.kind == "certainty" {
			finalCertainty[entry.key.text] = true
		}
	}
	var dropped []string
	seenDropped := map[string]bool{}
	for _, source := range materialSources {
		for _, m := range mustFinditer(certainty, runesText(source.text), 0, pyre.NoEnd) {
			word, _ := m.Group(0)
			if !finalCertainty[word] && !seenDropped[word] {
				seenDropped[word] = true
				dropped = append(dropped, word)
			}
		}
	}
	for _, word := range dropped {
		items = append(items, groundItem("certainty", word, nil, nil, nil, "dropped_certainty"))
	}
	// 資料にあって最終稿にない数値 (参考。候補にしない)。資料で出てくる順に items の最後に入れる。「ある」の判定は照合と同じ条件。
	// 数量を主張しない「一つ」は入れない
	type restItem struct {
		key  numKey
		text string
	}
	var rest []restItem
	inRest := map[numKey]bool{}
	for _, source := range materialSources {
		for _, n := range source.foundNums {
			key := groundNumberKey(n.key())
			if inRest[n.key()] || n.weak != weakNone || finalNumbers.set.keys[key] {
				continue
			}
			var present bool
			if key.unit != "" {
				unit := key.unit
				present = slices.ContainsFunc(finalNumbers.notation[numKey{key.value, ""}], func(o number) bool { return notationMatch(o, source.numbers, &unit) })
			} else {
				present = slices.ContainsFunc(source.notation[key], func(o number) bool { return notationMatch(o, finalNumbers.set, nil) })
			}
			if !present {
				inRest[n.key()] = true
				rest = append(rest, restItem{n.key(), runeSlice(source.text, n.start, n.end)})
			}
		}
	}
	for _, r := range rest {
		value, unit := r.key.value, r.key.unit
		items = append(items, groundItem("number", r.text, &value, &unit, nil, "not_in_final"))
	}

	sort.SliceStable(risks, func(i, j int) bool { return risks[i].start < risks[j].start })
	lines := 0
	if mode == "compose" && slices.ContainsFunc(items, func(o *pyjson.Object) bool {
		return o.Field("kind") == "word" && o.Field("classification") == "not_in_sources"
	}) {
		lines++
	}
	if len(dropped) > 0 {
		lines++
	}
	if mode == "rewrite" && slices.ContainsFunc(items, func(o *pyjson.Object) bool { return o.Field("classification") == "from_materials" }) {
		lines++
	}
	groundValueCertainty(&items, doc, found, materialSources, finalNumbers)
	out := pyjson.NewObject()
	out.Set("format", "yomiyasu-materials")
	out.Set("format_version", int64(MaterialsFormatVersion))
	out.Set("tool_version", visibility.Version)
	out.Set("mode", mode)
	out.Set("sources", sources)
	out.Set("items", pyjson.MapList(items, func(o *pyjson.Object) pyjson.Value { return o }))
	out.Set("meaning_risks", pyjson.MapList(risks, func(r pendingRisk) pyjson.Value { return r.risk }))
	out.Set("candidate_count", int64(len(risks)+lines))
	out.Set("meaning_risk_count", int64(len(risks)))
	out.Set("not_judged", pyjson.Strings(groundNotJudged))
	return out
}
