package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 AB: 原文と資料にない人や担当を、主語か知らせる・頼む相手として足した文 (本家の _added_actors。JSON の added_actors)。
// 意味が変わる恐れの種類ではないので、恐れの件数・状態・--fail-on には入れない。

var (
	actorHeadRe   = pyre.NewLazyFrom(func() string { return alternation(sortedByLenDesc(slices.Concat(actorSpecific, actorGeneric))) }, 0)
	actorPersonRe = pyre.NewLazyFrom(func() string {
		return "(?P<head>" + alternation(sortedByLenDesc(actorPerson)) + ")(?P<hon>たち|達|ども)?"
	}, 0)
	actorPrefixChar = pyre.NewLazy("["+actorChar+"]", 0)
	actorNameChar   = pyre.NewLazy(actorNameCharPattern, 0)
	actorHonorific  = pyre.NewLazy(actorHonorificPattern, 0)
	actorTitle      = pyre.NewLazy(actorTitlePattern, 0)
	actorOrg        = pyre.NewLazy(actorOrgPattern, 0)
	actorOrgEnd     = pyre.NewLazy(actorOrgEndPattern, 0)
	actorOrgNum     = pyre.NewLazy(actorOrgNumberedPattern, 0)
	actorOrgPart    = pyre.NewLazy(actorOrgPartPattern, 0)
	actorNoHito     = pyre.NewLazy(actorNoHitoPattern, 0)
	actorSelfRef    = pyre.NewLazy(actorSelfRefPattern, 0)
	actorSubject    = pyre.NewLazy(actorSubjectPattern, 0)
	actorRecipient  = pyre.NewLazy(actorRecipientPattern, 0)
	actorNotify     = pyre.NewLazy(actorNotifyPattern, 0)
	actorFrom       = pyre.NewLazyFrom(func() string {
		return `より(?=[、,]|[おご]?(?:` + actorNotifyPattern + `|説明|発表|回答|返信|返答|申し|指示|提供|発信|配信|話し|話が))`
	}, 0)
	actorCoreRe        = pyre.NewLazy(actorCorePattern, 0)
	actorVerbSupport   = pyre.NewLazy(actorVerbSupportPattern, 0)
	actorKanjiKana     = pyre.NewLazy(actorKanjiKanaPattern, 0)
	actorKanjiEnd      = pyre.NewLazy(actorKanjiEndPattern, 0)
	actorOfHeadsSet    = wordSet(actorOfHeads)
	actorSwap          = sortedByLenDesc(actorSwapSource)
	actorSpecificSet   = wordSet(actorSpecific)
	actorTitleBadSet   = wordSet(actorTitleBad)
	actorNotTitleSet   = wordSet(actorNotTitle)
	actorOrgStemBadSet = wordSet(actorOrgStemBad)
	actorReaderSet     = wordSet(actorReader)
	actorSpaces        = pyre.NewLazy(`\s+`, 0)
	actorMatchSpan     = pyre.NewLazy(`(.+)の(?:人|方|者)$`, 0)
)

// alternation は "|".join(re.escape(x) for x in words)。
func alternation(words []string) string {
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = pyre.Escape(w)
	}
	return strings.Join(parts, "|")
}

// sortedByLenDesc は sorted(words, key=len, reverse=True) (長さはコードポイント。同じ長さは元の順のまま)。
func sortedByLenDesc(words []string) []string {
	out := slices.Clone(words)
	sort.SliceStable(out, func(i, j int) bool { return pyunicode.RuneCount(out[i]) > pyunicode.RuneCount(out[j]) })
	return out
}

func wordSet(words []string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

func actorNorm(text string) string {
	return actorSpaces.MustSub("", pyunicode.Normalize(pyunicode.NFKC, text))
}

func actorCore(word string) string { return actorCoreRe.MustSub("", word) }

// actorMention は文の中の人や担当の語 1 つ (本家の _ActorMention)。start と end は文の中の位置 (コードポイント)。
type actorMention struct {
	start, end               int
	word, head, prefix, kind string
	modifier                 *actorMention
}

func actorMatchChar(l *pyre.Lazy, t pyre.Text, pos int) bool { return mustMatch(l, t, pos) != nil }

func actorPrefixStart(t pyre.Text, start, floor, limit int) int {
	position := start
	for position > floor && start-position < limit && actorMatchChar(actorPrefixChar, t, position-1) {
		position--
	}
	return position
}

func isASCIIAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isASCIIWord(s string) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

func actorMentions(text string) []*actorMention {
	rs := pyunicode.Runes(text)
	t := pyre.NewText(text)
	sub := func(a, b int) string { return pyunicode.FromRunes(rs[a:b]) }
	var found []*actorMention
	last := 0
	for _, m := range mustFinditer(actorHeadRe, t, 0, pyre.NoEnd) {
		head, start, end := group0(m), m.Start(0), m.End(0)
		var begin int
		if isASCIIWord(head) {
			if (start > 0 && isASCIIAlnum(rs[start-1])) || (end < len(rs) && isASCIIAlnum(rs[end])) {
				continue
			}
			begin = start
		} else {
			begin = actorPrefixStart(t, start, last, actorPrefixMax)
		}
		stop := end
		if h := mustMatch(actorHonorific, t, end); h != nil {
			stop = h.End(0)
		}
		found = append(found, &actorMention{begin, stop, sub(begin, stop), head, sub(begin, start), "role", nil})
		last = end
	}
	for _, m := range mustFinditer(actorPersonRe, t, 0, pyre.NoEnd) {
		if m.Start(0) > 0 && actorMatchChar(actorNameChar, t, m.Start(0)-1) {
			continue
		}
		head, _ := m.Group(1)
		found = append(found, &actorMention{m.Start(0), m.End(0), group0(m), head, "", "person", nil})
	}
	for _, m := range mustFinditer(actorTitle, t, 0, pyre.NoEnd) {
		start := m.Start(0)
		begin := start
		for begin > 0 && start-begin < 6 && actorMatchChar(actorNameChar, t, begin-1) {
			begin--
		}
		name := sub(begin, start)
		polite := name != "" && begin > 0 && rs[begin-1] == 'お'
		if name == "" || actorTitleBadSet[name] || (polite && actorTitleBadSet["お"+name]) {
			continue
		}
		nameRunes := pyunicode.Runes(name)
		if actorNotTitleSet[string(nameRunes[len(nameRunes)-1])+group0(m)] ||
			(m.End(0) < len(rs) && actorKanjiKana.MustMatchAt0(string(rs[m.End(0)]))) {
			continue
		}
		if polite && mustSearchAt(actorHeadRe, pyre.NewText(name), 0, pyre.NoEnd) == nil {
			begin--
		}
		found = append(found, &actorMention{begin, m.End(0), sub(begin, m.End(0)), name, "", "title", nil})
	}
	for _, m := range mustFinditer(actorNoHito, t, 0, pyre.NoEnd) {
		found = append(found, &actorMention{m.Start(0), m.End(0), group0(m), group0(m), "", "nohito", nil})
	}
	for _, m := range mustFinditer(actorOrg, t, 0, pyre.NoEnd) {
		begin := actorPrefixStart(t, m.Start(0), 0, 10)
		stem := sub(begin, m.Start(0))
		stemRunes := pyunicode.Runes(stem)
		if len(stemRunes) < 2 || actorOrgStemBadSet[stem] || actorOrgStemBadSet[string(stemRunes[len(stemRunes)-1])] ||
			actorOrgNum.MustSearch(stem) || (group0(m) == "部" && actorOrgPart.MustSearch(stem)) ||
			(group0(m) == "グループ" && !actorKanjiEnd.MustSearch(stem)) {
			continue
		}
		word := sub(begin, m.End(0))
		found = append(found, &actorMention{begin, m.End(0), word, word, "", "org", nil})
	}
	// 同じ所なら人称の語 (当チーム) を先に取る
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if la, lb := a.end-a.start, b.end-b.start; la != lb {
			return la > lb
		}
		return a.kind == "person" && b.kind != "person"
	})
	var out []*actorMention
	for _, mention := range found {
		if len(out) > 0 && mention.start < out[len(out)-1].end {
			continue
		}
		if len(out) > 0 {
			previous := out[len(out)-1]
			if mention.start == previous.end+1 && rs[previous.end] == 'の' && actorOfHeadsSet[actorCore(mention.word)] {
				// 「当番の担当者」「経理のメンバー」: 前の語を受けた 1 つの語にする (前の語は「の」で受けるので、それだけでは出ない)
				mention = &actorMention{previous.start, mention.end, sub(previous.start, mention.end), mention.head, mention.prefix, mention.kind, previous}
			}
		}
		out = append(out, mention)
	}
	return out
}

// actorFrameOf は語のすぐ後の助詞から、主語 (subject) か知らせる・頼む相手 (recipient) か。どちらでもなければ空。
func actorFrameOf(t pyre.Text, end int) string {
	if mustMatch(actorSubject, t, end) != nil || mustMatch(actorFrom, t, end) != nil {
		return "subject"
	}
	if mustMatch(actorRecipient, t, end) != nil && mustSearchAt(actorNotify, t, end, end+actorNotifyWindow) != nil {
		return "recipient"
	}
	return ""
}

func actorTier(m *actorMention, frame string) string {
	if m.kind == "person" {
		return "W"
	}
	if frame == "recipient" {
		return "A"
	}
	if m.kind == "title" || m.kind == "org" || m.kind == "nohito" || actorSpecificSet[m.head] || m.modifier != nil {
		return "B"
	}
	if m.prefix != "" {
		return "B"
	}
	return "C"
}

// actorSource は原文と資料の、人や担当の語を探す文字列 (NFKC、空白なし)。語ごとの答えは覚えておく。
// 本家は長い文章で 2 字ごとの位置の表を使うが、答えは部分文字列の検索と同じなので、表は持たない。
type actorSource struct {
	text    string
	selfRef bool
	present map[[3]string]bool
	has     map[string]bool
	hasVerb map[string]bool
	holds   map[string]bool
}

func newActorSource(texts []string) *actorSource {
	text := actorNorm(strings.Join(texts, "\n"))
	return &actorSource{text: text, selfRef: actorSelfRef.MustSearch(text), present: map[[3]string]bool{},
		has: map[string]bool{}, hasVerb: map[string]bool{}, holds: map[string]bool{}}
}

func (a *actorSource) contains(piece string) bool {
	found, ok := a.holds[piece]
	if !ok {
		found = strings.Contains(a.text, piece)
		a.holds[piece] = found
	}
	return found
}

func (a *actorSource) hasWord(word string) bool {
	if word == "サポート" {
		found, ok := a.hasVerb[word]
		if !ok {
			found = actorVerbSupport.MustSearch(a.text)
			a.hasVerb[word] = found
		}
		return found
	}
	return word != "" && a.contains(word)
}

func (a *actorSource) isPresent(m *actorMention) bool {
	modifierWord := ""
	if m.modifier != nil {
		modifierWord = m.modifier.word
	}
	key := [3]string{m.word, m.kind, modifierWord}
	if m.modifier == nil {
		key[2] = "\x00none"
	}
	if found, ok := a.present[key]; ok {
		return found
	}
	found := a.find(actorNorm(m.word), actorNorm(actorCore(m.word)), m.kind)
	if !found && m.modifier != nil {
		rest := pyunicode.Runes(m.word)
		rest = rest[min(len(pyunicode.Runes(m.modifier.word))+1, len(rest)):]
		if actorOfHeadsSet[actorCore(pyunicode.FromRunes(rest))] {
			found = a.isPresent(m.modifier)
		}
	}
	a.present[key] = found
	return found
}

// find は語そのもの、芯、語尾の「者」を外した複合語、「の」を詰めた複合語、部署の語の芯、部署や担当の語の入れ替え、
// 同じ人を指す別の書き方のどれかが原文か資料にあるか。
func (a *actorSource) find(word, core, kind string) bool {
	if a.contains(word) || (core != "" && a.contains(core)) {
		return true
	}
	coreRunes := pyunicode.Runes(core)
	n := len(coreRunes)
	if strings.HasSuffix(core, "者") && n >= 4 && a.contains(pyunicode.FromRunes(coreRunes[:n-1])) {
		return true
	}
	for i := 1; i < n; i++ {
		if a.contains(pyunicode.FromRunes(coreRunes[:i]) + "の" + pyunicode.FromRunes(coreRunes[i:])) {
			return true
		}
	}
	if kind == "org" {
		stem := pyunicode.Runes(actorOrgEnd.MustSub("", core))
		if len(stem) >= 2 && a.contains(pyunicode.FromRunes(stem)) {
			return true
		}
	}
	if m := mustMatch(actorMatchSpan, pyre.NewText(core), 0); m != nil {
		stem, _ := m.Group(1)
		if a.contains(stem+"者") || a.contains(stem+"担当") {
			return true
		}
	}
	for _, suffix := range actorSwap {
		if !strings.HasSuffix(core, suffix) {
			continue
		}
		stem := pyunicode.FromRunes(coreRunes[:n-pyunicode.RuneCount(suffix)])
		if pyunicode.RuneCount(stem) >= 2 {
			for _, other := range actorSwap {
				if a.contains(stem + other) {
					return true
				}
			}
			break
		}
	}
	for _, group := range actorSame {
		for _, name := range group {
			if !strings.HasSuffix(core, name) {
				continue
			}
			rest := core[:len(core)-len(name)]
			for _, other := range group {
				if other == name {
					continue
				}
				if rest != "" {
					if a.contains(rest+other) || a.contains(rest+"の"+other) {
						return true
					}
				} else if a.hasWord(other) {
					return true
				}
			}
		}
	}
	return false
}

// actorCandidate は原文と資料にない人や担当の語 1 つ。narrowed は原文の語を狭めたときの元の語 (なければ空)。
type actorCandidate struct{ word, frame, tier, narrowed string }

func (c actorCandidate) value() pyjson.Value {
	o := pyjson.NewObject()
	o.Set("word", c.word)
	o.Set("frame", c.frame)
	o.Set("tier", c.tier)
	if c.narrowed == "" {
		o.Set("narrowed_from", nil)
	} else {
		o.Set("narrowed_from", c.narrowed)
	}
	return o
}

func actorCandidates(text string, source *actorSource) []actorCandidate {
	text = pyunicode.Normalize(pyunicode.NFKC, text)
	t := pyre.NewText(text)
	var out []actorCandidate
	for _, m := range actorMentions(text) {
		frame := actorFrameOf(t, m.end)
		if frame == "" || source.isPresent(m) {
			continue
		}
		if m.kind == "person" && source.selfRef && !actorReaderSet[m.head] {
			continue
		}
		narrowed := ""
		if m.prefix != "" && m.modifier == nil && source.hasWord(actorNorm(m.head)) {
			narrowed = m.head
		}
		out = append(out, actorCandidate{m.word, frame, actorTier(m, frame), narrowed})
	}
	return out
}

// actorOldIndex は書き直した文 index に対応する元の文の番号 (なければ -1)。組になっていなければ、前後の組の間で位置から見込んだ元の文の近く (前後 3 文まで) から、
// いちばん似た文 (ratio 0.3 以上)。paired は組になった書き直した文の番号の昇順の並び。
func actorOldIndex(group *sentenceGroup, index int, pairs map[int]int, paired []int) int {
	if i, ok := pairs[index]; ok {
		return i
	}
	if len(group.old) == 0 {
		return -1
	}
	k := bisectLeft(paired, index)
	before, after := -1, len(group.new)
	if k > 0 {
		before = paired[k-1]
	}
	if k < len(paired) {
		after = paired[k]
	}
	low, high := 0, len(group.old)-1
	if before >= 0 {
		low = pairs[before]
	}
	if after < len(group.new) {
		high = pairs[after]
	}
	position := low
	if high > low {
		position = low + (index-before)*(high-low)/(after-before)
	}
	target := pyunicode.Runes(group.new[index].text)
	best := -1
	bestRatio := 0.3
	from, to := max(low, position-3), min(high, position+3)+1
	if from > len(group.old) {
		from = len(group.old)
	}
	to = min(to, len(group.old))
	for i := from; i < to; i++ {
		matcher := difflib.New(pyunicode.Runes(group.old[i].text), target)
		if matcher.RealQuickRatio() < bestRatio || matcher.QuickRatio() < bestRatio {
			continue
		}
		if ratio := matcher.Ratio(); ratio >= bestRatio {
			best, bestRatio = i, ratio
		}
	}
	return best
}

var actorMaskSkip = wordSet(actorSkipKinds)

// addedActors は書き直した文のうち、原文と資料にない人や担当を、主語か知らせる・頼む相手に足した文 (JSON の added_actors)。新しい文の順。
func (s *sides) addedActors(groups []*sentenceGroup, materials []string) pyjson.Value {
	source := newActorSource(append([]string{pyunicode.FromRunes(s.texts[0])}, materials...))
	masker := s.newMaskerWith(1, actorMaskSkip)
	seen := map[string][]actorCandidate{}
	out := []pyjson.Value{}
	for _, group := range groups {
		pairs := map[int]int{}
		for _, p := range group.pairs {
			pairs[p[1]] = p[0]
		}
		paired := make([]int, 0, len(pairs))
		for j := range pairs {
			paired = append(paired, j)
		}
		sort.Ints(paired)
		for index, sn := range group.new {
			b, ok := masker.body(sn)
			if !ok || pystr.Strip(b.text) == "" {
				continue
			}
			actors, done := seen[b.text]
			if !done {
				actors = actorCandidates(b.text, source)
				seen[b.text] = actors
			}
			if len(actors) == 0 {
				continue
			}
			var old *sentence
			if i := actorOldIndex(group, index, pairs, paired); i >= 0 {
				old = &group.old[i]
			}
			item := pyjson.NewObject()
			if old != nil {
				item.Set("old_line", int64(old.line))
			} else {
				item.Set("old_line", nil)
			}
			item.Set("new_line", int64(sn.line))
			if old != nil {
				item.Set("old", old.text)
			} else {
				item.Set("old", nil)
			}
			item.Set("new", sn.text)
			item.Set("actors", pyjson.MapList(actors, actorCandidate.value))
			out = append(out, item)
		}
	}
	return out
}

// AddedActors は元の文書と書き直した文書の、原文と資料にない人や担当を足した文 (JSON の added_actors の値)。materials は --material の資料。
func AddedActors(orig, rw string, materials []string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.addedActors(s.sentencePairs(full.hunks), materials), nil
}

func group0(m *pyre.Match) string {
	s, _ := m.Group(0)
	return s
}
