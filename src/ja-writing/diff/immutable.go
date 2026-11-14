package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
)

// 群 X: 動かしてはいけない値 (本家の _immutable_values)。引用・宛名・裸のメールアドレス・科学の単位・統計の値・符号の増減。
// 数字・単位・URL は既存の数値と変えない箇所の恐れに任せる。

const (
	// 英字の著者は、年の前に「,」か「et al.」があるものだけ (「(Windows 2000)」「(RFC 2119)」のような製品名・規格名と年の括弧を引用と取らないため)。
	citationLatin = `[A-Z][a-z][A-Za-z'’.-]*(?:\s*(?:&|and)\s*[A-Z][A-Za-z'’.-]*)*` + `(?:\s+et\s+al\.[,，]?\s*|[,，]\s*)`
	citationJA    = `[一-鿿々〆ァ-ヴー]+(?:ほか|ら)?[,，]?\s+`
)

var (
	// 引用は数値の角括弧か、著者名＋西暦。注記、TODO、リンクのラベルは含めない。
	citationRe = pyre.NewLazy(`\[[0-9]+(?:\s*[,、–—-]\s*[0-9]+)*\](?!\s*[([])`+`|[(（](?:`+citationJA+"|"+citationLatin+`)[12][0-9]{3}[a-z]?[)）]`, 0)
	// 既存の数値が空白つきの mg 等を単位として取らない穴だけを補う。未知の英単語は単位とみなさない。
	scientificRe = pyre.NewLazy(`(?<![A-Za-z0-9_.])[0-9]+(?:\.[0-9]+)?[ \t　]*`+
		`(?:[numcdkMGTμµ]?g|[numcdkμµ]?m(?:ol)?|[numcdkMμµ]?[lL]|[numkMGTμµ]?s|[kMGT]?Hz|[kMGT]?Pa|[kMGT]?W|℃|°C)(?![A-Za-z])`, 0)
	// 符号は、直前が漢字・片仮名・数字・英字・%・閉じ括弧でないときだけ取る (「10月-12月」「9時-17時」の範囲のハイフンを負の数と取らないため)。
	signedRe    = pyre.NewLazy(`(?<![A-Za-z0-9_.])(?:(?<![一-鿿々〆ァ-ヴー%％)）\]])[+−-])?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)`, 0)
	statisticRe = pyre.NewLazy(`(?<![A-Za-z])(?:p|P|α|β|r|t|F|SD|SE)[ \t]*(?:<=|>=|[<>=≤≥])[ \t]*`+
		`[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`, 0)
	emailRe = pyre.NewLazy("(?<![A-Za-z0-9.!#$%&'*+/=?^_`{|}~@-])[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)+", 0)
	// 宛名と分かる行頭か「宛先:」の後だけ。名前のあとの助詞 (田中さんは) は本文中の言及として対象外。
	addresseeRe = pyre.NewLazy(`(?m)(?:^[ \t]*(?:宛先[：:][ \t]*)?|(?<=様、)[ \t]*|(?<=さん、)[ \t]*|(?<=様,)[ \t]*|(?<=さん,)[ \t]*)`+
		`(?P<address>(?P<name>[一-鿿々〆〇○ぁ-ゖァ-ヴーA-Za-zＡ-Ｚａ-ｚ][一-鿿々〆〇○ぁ-ゖァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９・＆& .　\t]{0,79}?)(?:様|さん))`+
		`(?=[ \t　]*(?:[、，,：:！!\n]|$))`, 0)
	spaceRuns = pyre.NewLazy(`\s+`, 0)

	// 「さん」「様」が付いても宛名でない語 (家族の呼び方など)。名前がこれで終わるものは除く。
	addresseeFamily = []string{"婆", "爺", "母", "父", "娘", "息子", "奥", "嫁", "兄", "姉", "坊"}
	// 「仕様」「模様」等の語末の様は敬称でない。
	addresseeNotHonorific = []string{"仕様", "模様", "同様", "多様", "異様", "有様", "無様", "一様"}
	addresseeNotName      = []string{"皆", "みな", "たく", "お客", "お子", "お父", "お母", "お兄", "お姉", "おじい", "おばあ", "お疲れ", "ご苦労", "おかげ", "お陰", "お互い", "逆", "同", "有", "無"}
	valueSkipKinds        = map[string]bool{"code": true, "frontmatter": true, "html_block": true, "reference_definition": true}
	// 引用の表記は、書き方だけの違い (括弧の全角と半角、空白、「、」と「,」、ダッシュの種類) をそろえてから比べる。
	citationKeyMap = map[rune]rune{'（': '(', '）': ')', '，': ',', '、': ',', '–': '-', '—': '-', '−': '-'}
)

// valueKinds は動かしてはいけない値の種類を、本家の検査の順に並べたもの。
var valueKinds = []struct {
	name string
	re   *pyre.Lazy
}{
	{"citation", citationRe}, {"addressee", addresseeRe}, {"email", emailRe},
	{"scientific_unit", scientificRe}, {"statistic", statisticRe}, {"number_sign", signedRe},
}

func citationKey(text string) string {
	return spaceRuns.MustSub("", strings.Map(func(r rune) rune {
		if to, ok := citationKeyMap[r]; ok {
			return to
		}
		return r
	}, text))
}

// removeBlanks は re.sub(r"[ \t　]+", "", s)。
func removeBlanks(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '　' {
			return -1
		}
		return r
	}, s)
}

// valueToken は動かしてはいけない値 1 つ。key は比べる形、body.text は書いてあるとおりの形。
type valueToken struct {
	key  string
	body body
}

// valueTokens は元の位置を保って、コード・URL 等を除いた値 (本家の _value_tokens)。表・見出しの引用も検査する。
func (s *sides) valueTokens(side int, re *pyre.Lazy) []valueToken {
	m := s.newMasker(side)
	rows := s.analyses[side].Lines
	var addr, name int
	if re == addresseeRe {
		index := re.Get().GroupIndex()
		addr, name = index["address"], index["name"]
	}
	var out []valueToken
	for _, match := range mustFinditer(re, s.docText(side), 0, pyre.NoEnd) {
		a, b := match.Span(0)
		line := s.lineOf(side, a)
		if line-1 < len(rows) && valueSkipKinds[rows[line-1].Kind] {
			continue
		}
		index := max(0, bisectRight(m.lefts, a)-1)
		if overlapsSpan(m.spans[min(index, len(m.spans)):min(index+2, len(m.spans))], a, b) {
			continue
		}
		var value string
		if re == addresseeRe {
			names, _ := match.Group(name)
			if slices.Contains(addresseeNotName, pystr.Strip(names)) {
				continue
			}
			address, _ := match.Group(addr)
			value = pystr.Strip(address)
			if pystr.EndsWithAny(value, addresseeNotHonorific) || pystr.EndsWithAny(pystr.Strip(names), addresseeFamily) {
				continue
			}
		} else {
			hit, _ := match.Group(0)
			value = pystr.Strip(hit)
		}
		if re == scientificRe || re == statisticRe {
			value = removeBlanks(value)
		}
		key := value
		if re == citationRe {
			key = citationKey(value)
		}
		out = append(out, valueToken{key, body{value, s.lines[side][line-1], line, a}})
	}
	return out
}

func overlapsSpan(spans [][2]int, a, b int) bool {
	return slices.ContainsFunc(spans, func(sp [2]int) bool { return sp[0] < b && sp[1] > a })
}

// placeRange は文の範囲 (始まり・終わり・文の番号)。placeEntry は全文差分の塊の行の、組の番号と文の範囲。
type placeRange struct{ start, end, sentence int }

type placeEntry struct {
	group  int
	ranges []placeRange
}

// valuePlaces は全文差分の塊の行ごとに、組の番号と文の範囲 (元と後。本家の _value_places)。塊の外の行は変わっていない行。
func valuePlaces(groups []*sentenceGroup) [2]map[int]*placeEntry {
	places := [2]map[int]*placeEntry{{}, {}}
	for g, group := range groups {
		hunks := group.run
		if hunks == nil {
			hunks = []*hunk{group.hunk}
		}
		for side := range 2 {
			sentences := group.old
			if side == 1 {
				sentences = group.new
			}
			for _, h := range hunks {
				first, last := h.oldStart, h.oldEnd
				if side == 1 {
					first, last = h.newStart, h.newEnd
				}
				for line := first; line <= last; line++ {
					if places[side][line] == nil {
						places[side][line] = &placeEntry{group: g}
					}
				}
			}
			for k, sn := range sentences {
				if entry := places[side][sn.line]; entry != nil {
					entry.ranges = append(entry.ranges, placeRange{sn.start, sn.start + pystr.Len(sn.text), k})
				}
			}
		}
	}
	return places
}

// sentenceAt は ranges のうち position を含む最初の範囲の文の番号 (本家の _sentence_at)。始まりの順に重ならずに並ぶときは二分探索で引く
// (1 行に文が数千ある文書で、値ごとに行の文をすべて見ると文の数の 2 乗になる)。
func sentenceAt(ranges []placeRange, position int, ordered map[*placeEntry]bool, entry *placeEntry) (int, bool) {
	isOrdered, known := ordered[entry]
	if !known {
		isOrdered = true
		for k := 0; k+1 < len(ranges); k++ {
			if ranges[k].end > ranges[k+1].start {
				isOrdered = false
				break
			}
		}
		ordered[entry] = isOrdered
	}
	if !isOrdered {
		for _, r := range ranges {
			if r.start <= position && position < r.end {
				return r.sentence, true
			}
		}
		return 0, false
	}
	k := sort.Search(len(ranges), func(i int) bool { return ranges[i].start > position }) - 1
	if k >= 0 && position < ranges[k].end {
		return ranges[k].sentence, true
	}
	return 0, false
}

type taggedValue struct {
	token       valueToken
	group       int
	sentence    int
	hasSentence bool
}

type cancelKey struct{ a, b int }

// uncancelledTokens は同じ値を元と後で打ち消し、残った値 (消えた値、足した値) を返す (本家の _uncancelled_tokens)。
// 同じ値の移動は候補にせず、同じ値が 2 回から 1 回に減ったときは残す。全文差分で変わっていない行の値は除き (両側で同じ)、残りを組の文どうし、同じ塊の中、
// 文書全体の順に打ち消す。位置を見ずに前から打ち消すと、変えていない別の行の値が残って候補の行がずれる。
func uncancelledTokens(old, new []valueToken, places [2]map[int]*placeEntry, groups []*sentenceGroup) [2][]valueToken {
	var tagged [2][]taggedValue
	for side, tokens := range [2][]valueToken{old, new} {
		ordered := map[*placeEntry]bool{}
		for _, t := range tokens {
			entry := places[side][t.body.line]
			if entry == nil {
				continue
			}
			k, ok := sentenceAt(entry.ranges, t.body.start, ordered, entry)
			tagged[side] = append(tagged[side], taggedValue{t, entry.group, k, ok})
		}
	}
	alive := [2][]bool{make([]bool, len(tagged[0])), make([]bool, len(tagged[1]))}
	for side := range alive {
		for i := range alive[side] {
			alive[side][i] = true
		}
	}
	pairMaps := make([]map[int]int, len(groups))
	for g, group := range groups {
		pairMaps[g] = map[int]int{}
		for _, p := range group.pairs {
			pairMaps[g][p[0]] = p[1]
		}
	}
	cancel := func(oldKey, newKey func(taggedValue) (cancelKey, bool)) {
		type waitKey struct {
			key   cancelKey
			value string
		}
		waiting := map[waitKey][]int{}
		for j, row := range tagged[1] {
			if !alive[1][j] {
				continue
			}
			if key, ok := newKey(row); ok {
				k := waitKey{key, row.token.key}
				waiting[k] = append(waiting[k], j)
			}
		}
		for i, row := range tagged[0] {
			if !alive[0][i] {
				continue
			}
			key, ok := oldKey(row)
			if !ok {
				continue
			}
			k := waitKey{key, row.token.key}
			if queue := waiting[k]; len(queue) > 0 {
				j := queue[0]
				waiting[k] = queue[1:]
				alive[0][i], alive[1][j] = false, false
			}
		}
	}
	cancel(func(row taggedValue) (cancelKey, bool) {
		if !row.hasSentence {
			return cancelKey{}, false
		}
		partner, ok := pairMaps[row.group][row.sentence]
		return cancelKey{row.group, partner}, ok
	}, func(row taggedValue) (cancelKey, bool) { return cancelKey{row.group, row.sentence}, row.hasSentence })
	byGroup := func(row taggedValue) (cancelKey, bool) { return cancelKey{row.group, 0}, true }
	cancel(byGroup, byGroup)
	whole := func(taggedValue) (cancelKey, bool) { return cancelKey{}, true }
	cancel(whole, whole)
	var out [2][]valueToken
	for side := range out {
		for k, row := range tagged[side] {
			if alive[side][k] {
				out[side] = append(out[side], row.token)
			}
		}
	}
	return out
}

// digitRuns は re.findall(r"[0-9]+", s)。
func digitRuns(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			out, start = append(out, s[start:i]), -1
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// withoutDigits は re.sub(r"[0-9]+", "", s)。
func withoutDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return -1
		}
		return r
	}, s)
}

func countOf(items []string) map[string]int {
	c := map[string]int{}
	for _, it := range items {
		c[it]++
	}
	return c
}

// countersEqual は Counter どうしの比べ (0 の項は無いものと同じ)。
func countersEqual(a, b map[string]int) bool { return countsEqual(a, b) }

// positiveRemainder は (a - b) が空でないか。
func positiveRemainder(a, b map[string]int) bool {
	for k, n := range a {
		if n-b[k] > 0 {
			return true
		}
	}
	return false
}

func riskKind(r *pyjson.Object) string {
	kind, _ := r.Get("kind")
	s, _ := kind.(string)
	return s
}

// riskLineIs は risk[key] == line (欄が無いか null なら偽)。
func riskLineIs(r *pyjson.Object, key string, line int) bool {
	v, ok := r.Get(key)
	n, isInt := v.(int64)
	return ok && isInt && int(n) == line
}

// riskDetailStrings は risk["detail"].get(key) を文字列の並びにする (欄が無ければ空。文字列でない要素があれば ok は偽)。
func riskDetailStrings(r *pyjson.Object, key string) (out []string, ok bool) {
	detail, _ := r.Get("detail")
	d, _ := detail.(*pyjson.Object)
	if d == nil {
		return nil, true
	}
	v, present := d.Get(key)
	if !present {
		return nil, true
	}
	list, isList := v.([]pyjson.Value)
	if !isList {
		return nil, false
	}
	for _, e := range list {
		str, isStr := e.(string)
		if !isStr {
			return nil, false
		}
		out = append(out, str)
	}
	return out, true
}

// citationNumberCovered は同じ引用の数字だけの変化と、数字を含む引用の表記の追加・削除が、同じ行の既存の数値の候補で出た場合に真 (本家の _citation_number_covered)。
// 番号や年の変化、[3] の削除は数値の候補として出る。著者の変化 (数字以外の違い) は省かない。変えない箇所の恐れはその存在だけで引用の候補を省かない。
func citationNumberCovered(old, new *valueToken, risks []*pyjson.Object) bool {
	var a, b string
	if old != nil {
		a = old.key
	}
	if new != nil {
		b = new.key
	}
	if old != nil && new != nil && withoutDigits(a) != withoutDigits(b) {
		return false
	}
	if old != nil && new != nil {
		for _, risk := range risks {
			sub, _ := risk.Get("subkind")
			if riskKind(risk) != "number" || sub != "order" || !riskLineIs(risk, "old_line", old.body.line) || !riskLineIs(risk, "new_line", new.body.line) {
				continue
			}
			oldOrder, okOld := riskDetailStrings(risk, "old_order")
			newOrder, okNew := riskDetailStrings(risk, "new_order")
			if okOld && okNew && slices.Equal(oldOrder, digitRuns(a)) && slices.Equal(newOrder, digitRuns(b)) {
				return true
			}
		}
	}
	for _, side := range []struct {
		name    string
		own     *valueToken
		other   *valueToken
		lineKey string
	}{{"removed", old, new, "old_line"}, {"added", new, old, "new_line"}} {
		if side.own == nil {
			continue
		}
		otherKey := ""
		if side.other != nil {
			otherKey = side.other.key
		}
		changed := map[string]int{}
		for k, n := range countOf(digitRuns(side.own.key)) {
			if left := n - countOf(digitRuns(otherKey))[k]; left > 0 {
				changed[k] = left
			}
		}
		if len(changed) == 0 {
			continue
		}
		reported := map[string]int{}
		for _, risk := range risks {
			if riskKind(risk) != "number" || !riskLineIs(risk, side.lineKey, side.own.body.line) {
				continue
			}
			items, _ := riskDetailStrings(risk, side.name)
			for _, item := range items {
				for _, n := range digitRuns(item) {
					reported[n]++
				}
			}
		}
		if positiveRemainder(changed, reported) {
			return false
		}
	}
	return !countersEqual(countOf(digitRuns(a)), countOf(digitRuns(b)))
}

// wordRuns は re.findall(r"[0-9.]+", s)。
func wordRuns(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == '.' || (r >= '0' && r <= '9') {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			out, start = append(out, s[start:i]), -1
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// comparisonSigns は re.findall(r"<=|>=|[<>=≤≥]", s)。
func comparisonSigns(s string) []string {
	rs := []rune(s)
	var out []string
	for i := 0; i < len(rs); i++ {
		switch {
		case (rs[i] == '<' || rs[i] == '>') && i+1 < len(rs) && rs[i+1] == '=':
			out = append(out, string(rs[i:i+2]))
			i++
		case strings.ContainsRune("<>=≤≥", rs[i]):
			out = append(out, string(rs[i]))
		}
	}
	return out
}

// immutableValues は引用・宛名・裸のメールアドレスの増減 (本家の _immutable_values)。数字・単位・URL は既存の数値と変えない箇所の恐れに任せる。
// 戻り値は (独立の節, 意味が変わる恐れに足す引用の候補)。risks は意味が変わる恐れ (数値の候補だけが読まれる)。
func (s *sides) immutableValues(risks []*pyjson.Object, groups []*sentenceGroup) (*pyjson.Object, []*pyjson.Object) {
	var out, citations []*pyjson.Object
	places := valuePlaces(groups)
	for _, vk := range valueKinds {
		kept := uncancelledTokens(s.valueTokens(0, vk.re), s.valueTokens(1, vk.re), places, groups)
		removed, added := kept[0], kept[1]
		if vk.name == "number_sign" {
			// 同じ絶対値の符号だけ変わった穴を補う。数そのものの増減は既存の数値に任せる。
			byValue := map[string][]valueToken{}
			for _, t := range added {
				k := pystr.LStripChars(t.key, "+−-")
				byValue[k] = append(byValue[k], t)
			}
			var signRemoved, signAdded []valueToken
			for _, own := range removed {
				k := pystr.LStripChars(own.key, "+−-")
				if matches := byValue[k]; len(matches) > 0 {
					other := matches[0]
					byValue[k] = matches[1:]
					if negativeSign(own.key) != negativeSign(other.key) {
						signRemoved, signAdded = append(signRemoved, own), append(signAdded, other)
					}
				}
			}
			removed, added = signRemoved, signAdded
		}
		for index := range max(len(removed), len(added)) {
			var old, new *valueToken
			if index < len(removed) {
				old = &removed[index]
			}
			if index < len(added) {
				new = &added[index]
			}
			switch vk.name {
			case "statistic":
				// 数字の変化は既存の数値が扱う。数が同じで比較の印だけ変わった穴に限定する。
				if old == nil || new == nil || !slices.Equal(wordRuns(old.key), wordRuns(new.key)) {
					continue
				}
				if slices.Equal(comparisonSigns(old.key), comparisonSigns(new.key)) {
					continue
				}
			case "scientific_unit":
				// 数字・単位の変化を既存の数値がすでに報告していれば追加しない。
				covered := true
				for _, side := range []struct {
					name    string
					own     *valueToken
					lineKey string
				}{{"removed", old, "old_line"}, {"added", new, "new_line"}} {
					if side.own == nil {
						continue
					}
					if !slices.ContainsFunc(risks, func(r *pyjson.Object) bool {
						if riskKind(r) != "number" || !riskLineIs(r, side.lineKey, side.own.body.line) {
							return false
						}
						items, _ := riskDetailStrings(r, side.name)
						return slices.ContainsFunc(items, func(v string) bool { return removeBlanks(v) == side.own.key })
					}) {
						covered = false
					}
				}
				if covered {
					continue
				}
			case "citation":
				if citationNumberCovered(old, new, risks) {
					continue
				}
			}
			detail := pyjson.NewObject()
			detail.Set("removed", pyjson.Strings(tokenTexts(old)))
			detail.Set("added", pyjson.Strings(tokenTexts(new)))
			var oldBody, newBody *body
			if old != nil {
				oldBody = &old.body
			}
			if new != nil {
				newBody = &new.body
			}
			item := reviewCandidate(vk.name, oldBody, newBody, detail)
			if vk.name == "addressee" || vk.name == "email" {
				out = append(out, item)
				continue
			}
			subkind := "added"
			switch {
			case old != nil && new != nil:
				subkind = "replaced"
			case old != nil:
				subkind = "lost"
			}
			kind := "number"
			if vk.name == "citation" {
				kind = "citation"
			}
			oldLine, newLine := 0, 0
			var oldRaw, newRaw string
			if old != nil {
				oldLine, oldRaw = old.body.line, old.body.raw
			}
			if new != nil {
				newLine, newRaw = new.body.line, new.body.raw
			}
			citations = append(citations, newRisk(kind, subkind, oldLine, newLine, oldRaw, newRaw, old != nil, new != nil, detail))
		}
	}
	section := reviewSection(out)
	section.Set("meaning_risk_count", int64(len(citations)))
	return section, citations
}

func negativeSign(v string) bool { return v != "" && strings.ContainsRune("-−", []rune(v)[0]) }

func tokenTexts(t *valueToken) []string {
	if t == nil {
		return nil
	}
	return []string{t.body.text}
}

// ImmutableValues は元の文と書き直した文の、動かしてはいけない値の節と、意味が変わる恐れに足す引用などの候補 (JSON の値の形。[節, 候補の並び])。
// 数値の恐れは、既存の数値の候補と重ねないために読む。
func ImmutableValues(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	var risks []*pyjson.Object
	for _, item := range s.numberRisks(full, groups) {
		risks = append(risks, item.risk)
	}
	section, citations := s.immutableValues(risks, groups)
	return []pyjson.Value{section, riskObjectsValue(citations)}, nil
}
