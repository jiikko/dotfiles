package diff

import (
	"maps"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 Q: 数に付いた、概数と範囲を表す語 (本家の _qualifier_risks・_qualifier_states・_qualifier_marks・_qualifier_change ほか)。
// 同じ数 (値と単位が同じ) の前後の印が、組の文で消えたり増えたりしたら出す。「約120人 → 120人」は概数を数えた数に、「100人以上 → 100人」は範囲を 1 つの数に変える。
// 印は家族に分ける。概数 (約、およそ、ほど、程度、前後、くらい、ごろ など)、下限 (以上、以降、超)、上限 (以下、以内、以前、未満)、動詞の範囲の言い方 (を超える、を上回る、を下回る。
// 下限と上限のどちらとも同じとみなす)。同じ家族の中の言い換え (約120人 → 120人ほど、3日以内 → 3日以下) は出さない。数と印はすぐ隣り合うものだけを見る。

// qualifierShownSource は万・億・兆と、そのあとの単位の正規表現 (10万円、3億円)。
func qualifierShownSource() string { return `[万億兆](?:` + unitPatternSource(numberUnits) + `)?` }

var (
	// 範囲・日付・時刻のまとまりをつなぐ字
	qualifierJoin = pyre.NewLazy(`[ \t]*(?:[~〜]|から)[ \t]*|[-/:‐-–−]`, 0)
	// 数のあとに読み飛ばすもの (強調の記号、万・億・兆と単位、「3日間」の「間」)
	qualifierSkip = pyre.NewLazyFrom(func() string { return `\*{0,2}(?:` + qualifierShownSource() + `)?間?` }, 0)
	// 人が読む数の書き方に含める、数のあとの万・億・兆と単位 (10万円、3億円)
	qualifierShown        = pyre.NewLazyFrom(qualifierShownSource, 0)
	qualifierKatakanaUnit = pyre.NewLazy(`[ァ-ヴー]{1,6}`, 0)
	// 単位のある数のあとの、時期と半端を言う語 (10月中旬ごろ、15時半ごろ)
	qualifierPart = pyre.NewLazy(`上旬|中旬|下旬|初旬|半ば|半|末`, 0)
	// 数のすぐあとの印。名前は 家族_含むか (approx は概数、lower は下限、upper は上限。in は含む、ex は含まない)
	qualifierAfter = pyre.NewLazy(`[ 　]?(?:(?P<approx>ほど|程度|程(?![度遠])|前後(?!関係|左右|不覚|逆|差|し|の関係|の文脈)|ぐらい|くらい|位(?![置相階牌取])|ごろ|頃|余り(?!ま[すしせ]|そう)|余(?![裕計地分白剰談韻波興震熱力命暇生罪念儀所程技病勢光香情弊禄話聞部事日人りるっすせられさし])|あまり(?![にな良よ多]|ま[すしせ]|そう)|近く|足らず|内外|かそこら|やそこら|弱(?![点化体者小気音酸腰冷火毒視肉虫電震いさみめまるりっ]|く(?!らい))|強(?![化制調度力固烈硬気行豪盗敵者風弱靭要引奪迫権大国健壮打欲情いさみめまるりっ]|く(?!らい)))|(?P<upper_in>以下|以内|以前)|(?P<lower_in>以上|以降|以後)|(?P<lower_ex>超え|超(?![過一-龥々〆]))|(?P<upper_ex>未満)|(?P<bound_verb>を超[えすさし]|を上回|を下回))`, 0)
	// バージョンの番号 (v2.0前後) のあとの「前後」は概数を表す語として取らない
	qualifierVersionBefore = pyre.NewLazy(`(?:[vV]|[Vv]er\.?|[Vv]ersion|バージョン)[ 　]?$`, 0)
	// 数のすぐ前の印
	qualifierBefore = pyre.NewLazy(`(?:(?P<approx>約|およそ|おおよそ|凡そ|概ね|おおむね|ほぼ|推定|大体|だいたい))(?:で|、)?[ 　]*\*{0,2}$`, 0)
	// 相手の側の、同じ数のすぐ隣の、印に近い語
	qualifierNearAfter  = pyre.NewLazy(`[ 　]?(?:(?P<both>より古|より新)|(?P<upper>まで|より(?:少|前|下|短|低|早|小|手前|若)|に満たな|に届かな|を切[りっるらろれ]|を割[りっるらろれ]|のうちに|を上限|が上限|を限度|を最大)|(?P<lower>から|より|を下限|が下限|を最低|でも|過ぎ|すぎ|はかか|は必要|は要)|(?P<approx>規模|見当|ちょっと|ちょい|そこそこ|ばかり|に迫|に近))`, 0)
	qualifierNearBefore = pyre.NewLazy(`(?:(?P<upper>最大|最長|最高|最多|多くて|多くとも|多い時で|上限|最大限)|(?P<lower>最低|最小|最短|少なくとも|少なくて|下限)|(?P<approx>ざっと|およそ))(?:で|は|、)?[ 　]*\*{0,2}$`, 0)
	// 数が文の範囲を越えたときに、範囲を延ばす先の文の終わり
	qualifierSentenceStop = pyre.NewLazy(`[。！？!?\n]`, 0)
	// 全角の小数点 (３．５) で切れた文の終わり
	qualifierSplitDecimal = pyre.NewLazy(`[0-9０-９]．$`, 0)
	qualifierSentence     = map[string]*pyre.Lazy{
		"approx": pyre.NewLazy(`推定|推計|概算|見込|見積|目安|想定|概数|大まか|おおまか|ざっと|と見られ|とみられ`, 0),
		"lower":  pyre.NewLazy(`以上|以降|超え|超す|超し|超過|上回|少なくとも|最低|下限`, 0),
		"upper":  pyre.NewLazy(`以下|以内|未満|下回|上限|最大|最長|超えな|超えず|超えませ|まで|満たな`, 0),
	}
)

// 「約」で終わる語 (契約3件、予約2件、要約3行)。この語の「約」は印にしない
var qualifierYakuWords = wordsOf([]string{"契約", "予約", "集約", "要約", "節約", "制約", "公約", "条約", "特約", "規約", "確約",
	"誓約", "倹約", "婚約", "盟約", "協約", "密約", "違約", "解約", "成約", "新約", "旧約",
	"黙約", "簡約", "縮約", "締約"})

// 上の語の頭の字で終わる名詞 (所要約30分、主要約10社、作成約3時間、体制約50人、募集約50名)。この「約」は印にする
var qualifierYakuNouns = wordsOf([]string{"所要", "必要", "主要", "需要", "重要", "作成", "構成", "完成", "達成", "形成", "編成",
	"生成", "結成", "更新", "最新", "刷新", "体制", "募集", "収集", "編集", "結婚", "猶予"})

const qualifierBeforeReach = 8 // 数のすぐ前の印を見る字数

var (
	// 単位のある数にだけ付ける印 (ベスト4強、16強、震度5弱、3位 の 強・弱・位 は印ではない)
	qualifierUnitOnly = wordsOf([]string{"弱", "強", "位"})
	// 文書の中の場所を言う単位のあとの「以下」は「それより後」(第3章以下、第5条以下)。下限として読む
	qualifierPlaceUnits = wordsOf([]string{"章", "節", "項", "条", "号"})
	qualifierFamilies   = []string{"approx", "lower", "upper", "bound"}
	qualifierBounds     = wordsOf([]string{"lower", "upper", "bound"})
	qualifierStrict     = wordsOf([]string{"lower_in", "lower_ex", "upper_in", "upper_ex"})
)

// qualifierYakuOK は text の at の「約」(文は start から) を概数の印として読むか。直前の字と合わせて「約」で終わる語 (契約、予約) なら読まない。
// ただし、その頭の字で終わる名詞 (所要、主要、作成) のあとなら読む。
func qualifierYakuOK(text []rune, start, at int) bool {
	if at <= start || !qualifierYakuWords[runeSlice(text, at-1, at+1)] {
		return true
	}
	return at-2 >= start && qualifierYakuNouns[runeSlice(text, at-2, at)]
}

func mustMatchEnd(l *pyre.Lazy, t pyre.Text, pos, end int) *pyre.Match {
	m, err := l.Get().Match(t, pos, end)
	if err != nil {
		panic(err)
	}
	return m
}

// qualifierTail は数 n のあとの、読み飛ばす続き (強調の記号、万・億・兆と単位、「間」、片仮名の単位) の終わりと、単位があるかを返す。
func qualifierTail(text []rune, t pyre.Text, n number, end int) (int, bool) {
	at := mustMatchEnd(qualifierSkip, t, n.end, end).End(0)
	unit := n.unit != "" || strings.ContainsAny(runeSlice(text, n.end, at), "万億兆")
	if n.unit == "" && at == n.end {
		if k := mustMatchEnd(qualifierKatakanaUnit, t, at, end); k != nil {
			at, unit = k.End(0), true
		}
	}
	return at, unit
}

// qualifierShownEnd は人が読む数の書き方の終わり (10万円、3億円、5パーセント、10月中旬、15時半。強調の記号と「間」は含めない)。
func qualifierShownEnd(t pyre.Text, n number, end int) int {
	at, unit := n.end, n.unit != ""
	if shown := mustMatchEnd(qualifierShown, t, at, end); shown != nil {
		at, unit = shown.End(0), true
	} else if n.unit == "" {
		if k := mustMatchEnd(qualifierKatakanaUnit, t, at, end); k != nil {
			at, unit = k.End(0), true
		}
	}
	if unit {
		if part := mustMatchEnd(qualifierPart, t, at, end); part != nil {
			return part.End(0)
		}
	}
	return at
}

// qualifierAfterMarks は数のあとの印を 2 つまで読み、names と words に足す。終わりの位置を返す。数と印の間の「前」「後」は読み飛ばす (3年前ぐらい、1時間前ほど は 3年ぐらい前、1時間ほど前 と同じ概数)。
func qualifierAfterMarks(text []rune, t pyre.Text, at, end int, n number, unit bool, names map[string]bool, words *[]string) int {
	if unit {
		if part := mustMatchEnd(qualifierPart, t, at, end); part != nil {
			at = part.End(0)
		}
	}
	for range 2 {
		found := mustMatchEnd(qualifierAfter, t, at, end)
		if found == nil && (hasPrefixAt(text, at, "前") || hasPrefixAt(text, at, "後")) && at+1 < end {
			found = mustMatchEnd(qualifierAfter, t, at+1, end)
		}
		if found == nil {
			break
		}
		at = found.End(0)
		name, _ := found.LastGroup()
		word, _ := found.Group(0)
		if g, ok := groupByName(found, name); ok {
			word = g
		}
		switch {
		case qualifierUnitOnly[word] && !unit:
			continue // ベスト4強、16強、震度5弱、震度は6強、3位 (順位や段階の名前なので、範囲を表す語として取らない)
		case word == "前後" && !unit && mustSearchAt(qualifierVersionBefore, t, 0, n.start) != nil:
			continue // v2.0前後、バージョン2.0前後 (そのバージョンの前と後)
		case word == "近く" && n.unit == "号":
			continue // 国道1号近く (場所)
		}
		if word == "以下" && qualifierPlaceUnits[n.unit] {
			name = "lower_in" // 第3章以下 (第3章から後)
		}
		names[name] = true
		*words = append(*words, word)
	}
	return at
}

func groupByName(m *pyre.Match, name string) (string, bool) {
	idx, ok := m.Pattern().GroupIndex()[name]
	if !ok {
		return "", false
	}
	return m.Group(idx)
}

// qualifierMarks は文 text ([start, end) が文の範囲) の数 n に付いた印。(印の名前の集合, 印の語の並び, 隣の語の家族の集合) を返す。
func qualifierMarks(text []rune, t pyre.Text, start, end int, n number) (map[string]bool, []string, map[string]bool) {
	names, near := map[string]bool{}, map[string]bool{}
	var words []string
	headStart := max(start, n.start-qualifierBeforeReach)
	head := pyre.NewText(runeSlice(text, headStart, n.start))
	if before := mustSearchAt(qualifierBefore, head, 0, pyre.NoEnd); before != nil {
		at := headStart + before.Start(0)
		word, _ := groupByName(before, "approx")
		if word != "約" || qualifierYakuOK(text, start, at) {
			names["approx"] = true
			words = append(words, word)
		}
	}
	if nb := mustSearchAt(qualifierNearBefore, head, 0, pyre.NoEnd); nb != nil {
		name, _ := nb.LastGroup()
		near[name] = true
	}
	at, unit := qualifierTail(text, t, n, end)
	at = qualifierAfterMarks(text, t, at, end, n, unit, names, &words)
	if na := mustMatchEnd(qualifierNearAfter, t, at, end); na != nil {
		name, _ := na.LastGroup()
		if name == "both" {
			near["lower"], near["upper"] = true, true
		} else {
			near[name] = true
		}
	}
	return names, words, near
}

// qualifierJoined は前の数 previous と数 n が 1 つのまとまり (範囲の 3〜5日、3日から5日、3万円程度から5万円程度、万・億・兆を挟んだ 1万5000人、2億3000万円、3万〜5万円) か。
// 前の数のあとの万・億・兆と単位と印を読み飛ばし、残りがなにもないか、つなぎ (〜、から、-、/、:) だけなら真。
func qualifierJoined(text []rune, t pyre.Text, previous, n number) bool {
	if previous.end == n.start {
		return true
	}
	at, unit := qualifierTail(text, t, previous, n.start)
	compound := strings.ContainsAny(runeSlice(text, previous.end, at), "万億兆")
	at = qualifierAfterMarks(text, t, at, n.start, previous, unit, map[string]bool{}, new([]string))
	between := []rune(runeSlice(text, at, n.start))
	for i, r := range between {
		between[i] = halfWidth(r)
	}
	if len(between) == 0 {
		return compound || at == previous.end
	}
	return !strings.Contains(string(between), "\n") && qualifierJoin.MustFullMatch(string(between))
}

// qualifierReach は数の印を読む範囲の文字列の終わり (ふだんは文そのもの)。全角の小数点 (３．５) は文の区切りにも読まれるので、数が文の範囲を越えることがある。
// そのときは、その数を含む文の終わり (次の句点か改行) までを範囲にして、数のあとの印も読む。
func (s *sides) qualifierReach(side int, sn sentence, numbers []number) (int, int) {
	text := s.texts[side]
	start := sn.start
	end := sn.start + pystrLen(sn.text)
	reach := end
	for _, n := range numbers {
		reach = max(reach, n.end)
	}
	if reach > end {
		if stop := mustSearchAt(qualifierSentenceStop, s.docText(side), reach, pyre.NoEnd); stop != nil {
			end = stop.End(0)
		} else {
			end = len(text)
		}
	}
	return start, end
}

func pystrLen(s string) int { return len(pyunicode.Runes(s)) }

// qKey は数の key。まとまりは数の並びと最後の単位 (3〜5日 と 3日から5日 を同じにする)。
type qKey struct {
	multi  bool
	values string
	unit   string
}

type qState struct {
	key   qKey
	names []string // 昇順
	words []string
	near  []string // 昇順
	shown string
}

// qualifierStates は組の文の数ごとの (key, 印の名前の集合, 印の語の並び, 隣の語の家族の集合, 書かれた形) の並び。範囲・日付・時刻のまとまりと、万・億・兆を挟んだ数は、まとまりの数の印を合わせて 1 つにする。
// 数量を主張しない値 (weak) は入れない。
func (s *sides) qualifierStates(side int, sn sentence, numbers []number) []qState {
	text := s.texts[side]
	t := s.docText(side)
	start, end := s.qualifierReach(side, sn, numbers)
	type marks struct {
		names []string
		words []string
		near  []string
	}
	all := make([]marks, len(numbers))
	for i, n := range numbers {
		names, words, near := qualifierMarks(text, t, start, end, n)
		all[i] = marks{sortedWordKeys(names), words, sortedWordKeys(near)}
	}
	groups := make([]int, len(numbers))
	for i := range groups {
		groups[i] = i
	}
	for i := 1; i < len(numbers); i++ {
		if qualifierJoined(text, t, numbers[i-1], numbers[i]) {
			groups[i] = groups[i-1]
		}
	}
	type merged struct {
		members []number
		names   map[string]bool
		words   []string
		near    map[string]bool
	}
	var order []int
	byGroup := map[int]*merged{}
	for i, mk := range all {
		if numbers[i].weak != weakNone {
			continue
		}
		g := byGroup[groups[i]]
		if g == nil {
			g = &merged{names: map[string]bool{}, near: map[string]bool{}}
			byGroup[groups[i]] = g
			order = append(order, groups[i])
		}
		g.members = append(g.members, numbers[i])
		for _, n := range mk.names {
			g.names[n] = true
		}
		g.words = append(g.words, mk.words...)
		for _, n := range mk.near {
			g.near[n] = true
		}
	}
	var out []qState
	for _, gi := range order {
		g := byGroup[gi]
		last := g.members[len(g.members)-1]
		key := qKey{values: g.members[0].value, unit: g.members[0].unit}
		if len(g.members) > 1 {
			vals := make([]string, len(g.members))
			for i, m := range g.members {
				vals[i] = m.value
			}
			key = qKey{multi: true, values: strings.Join(vals, "\x00"), unit: last.unit}
		}
		shown := strings.ReplaceAll(runeSlice(text, g.members[0].start, qualifierShownEnd(t, last, end)), "**", "")
		out = append(out, qState{key, sortedWordKeys(g.names), g.words, sortedWordKeys(g.near), shown})
	}
	return out
}

func familiesOf(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		family, _, _ := strings.Cut(n, "_")
		out[family] = true
	}
	return out
}

// qualifierChange は 1 つの数の、概数と範囲を表す語を比べる。(消えた家族, 増えた家族, 含むかが変わった家族) を返す。相手の側の隣の語 (near) と、相手の文のその家族の語 (oldWords と newWords) があれば、
// その家族は消えた・増えたにしない。動詞の言い方 (bound) は下限と上限のどちらとも同じとみなす。含むかは、元と後の両方がその家族の名詞の印 (以上、超、以下、未満 など) 1 つずつのときだけ比べる。
func qualifierChange(oldNames, newNames, oldNear, newNear []string, oldWords, newWords map[string]bool) (lost, added, strict []string) {
	present := func(family string, fams map[string]bool, near []string, words map[string]bool) bool {
		has := map[string]bool{}
		for f := range fams {
			has[f] = true
		}
		for _, n := range near {
			has[n] = true
		}
		for w := range words {
			has[w] = true
		}
		if family == "bound" {
			return slices.ContainsFunc(sortedWordKeys(has), func(k string) bool { return qualifierBounds[k] })
		}
		return has[family] || (qualifierBounds[family] && fams["bound"])
	}
	oldF, newF := familiesOf(oldNames), familiesOf(newNames)
	for _, f := range qualifierFamilies {
		if oldF[f] && !present(f, newF, newNear, newWords) {
			lost = append(lost, f)
		}
		if newF[f] && !present(f, oldF, oldNear, oldWords) {
			added = append(added, f)
		}
	}
	for _, family := range []string{"lower", "upper"} {
		var olds, news []string
		for _, n := range oldNames {
			if strings.HasPrefix(n, family) {
				olds = append(olds, n)
			}
		}
		for _, n := range newNames {
			if strings.HasPrefix(n, family) {
				news = append(news, n)
			}
		}
		if len(olds) == 1 && len(news) == 1 && olds[0] != news[0] && qualifierStrict[olds[0]] && qualifierStrict[news[0]] {
			strict = append(strict, family)
		}
	}
	return lost, added, strict
}

func countStates(states []qState) map[qKey]int {
	out := map[qKey]int{}
	for _, st := range states {
		out[st.key]++
	}
	return out
}

func statesWith(states []qState, key qKey) []qState {
	var out []qState
	for _, st := range states {
		if st.key == key {
			out = append(out, st)
		}
	}
	return out
}

func namesCount(states []qState) map[string]int {
	out := map[string]int{}
	for _, st := range states {
		out[strings.Join(st.names, "\x00")]++
	}
	return out
}

// dropMatching は印の名前が同じものを先に外した残り (rest の数だけ外す)。
func dropMatching(states []qState, rest map[string]int) []qState {
	var kept []qState
	for _, st := range states {
		id := strings.Join(st.names, "\x00")
		if rest[id] > 0 {
			rest[id]--
		} else {
			kept = append(kept, st)
		}
	}
	return kept
}

// qualifierRisks は数に付いた、概数と範囲を表す語の変化 (kind number_qualifier)。組の文で、同じ値と単位の数が元と後に同じ数だけあり、その語の家族が消えたか (lost)、増えたか (added)、
// 家族や含むかが入れ替わったか (changed) を見る。
func (s *sides) qualifierRisks(groups []*sentenceGroup) []pyjson.Value {
	var out []pyjson.Value
	for _, group := range groups {
		for _, p := range group.pairs {
			old, new := group.old[p[0]], group.new[p[1]]
			if old.text == new.text && !qualifierSplitDecimal.MustSearch(old.text) {
				continue
			}
			oldNumbers := s.sentenceNumbers(0, old)
			if len(oldNumbers) == 0 {
				continue
			}
			newNumbers := s.sentenceNumbers(1, new)
			if len(newNumbers) == 0 {
				continue
			}
			if old.text == new.text {
				_, oe := s.qualifierReach(0, old, oldNumbers)
				_, ne := s.qualifierReach(1, new, newNumbers)
				if runeSlice(s.texts[0], old.start, oe) == runeSlice(s.texts[1], new.start, ne) {
					continue // 全角の小数点で切れた文で、数を含む文の終わりまで見ても同じ
				}
			}
			olds, news := s.qualifierStates(0, old, oldNumbers), s.qualifierStates(1, new, newNumbers)
			oldCount, newCount := countStates(olds), countStates(news)
			var oldWords, newWords map[string]bool
			var keys []qKey
			seen := map[qKey]bool{}
			for _, st := range olds {
				if !seen[st.key] {
					seen[st.key] = true
					keys = append(keys, st.key)
				}
			}
			for _, key := range keys {
				if c, ok := newCount[key]; !ok || oldCount[key] != c {
					continue
				}
				oldStates, newStates := statesWith(olds, key), statesWith(news, key)
				if countsEqual(namesCount(oldStates), namesCount(newStates)) {
					continue
				}
				if len(oldStates) > 1 {
					// 同じ値が 2 つ以上あるときは、印の名前が同じものを先に外し、残りを位置の順に組にする
					keptOld := dropMatching(oldStates, namesCount(newStates))
					keptNew := dropMatching(newStates, namesCount(oldStates))
					oldStates, newStates = keptOld, keptNew
				}
				for i := range min(len(oldStates), len(newStates)) {
					before, after := oldStates[i], newStates[i]
					if oldWords == nil {
						oldWords, newWords = map[string]bool{}, map[string]bool{}
						for f, l := range qualifierSentence {
							if l.MustSearch(old.text) {
								oldWords[f] = true
							}
							if l.MustSearch(new.text) {
								newWords[f] = true
							}
						}
					}
					lost, added, strict := qualifierChange(before.names, after.names, before.near, after.near, oldWords, newWords)
					if len(lost) == 0 && len(added) == 0 && len(strict) == 0 {
						continue
					}
					subkind := "changed"
					if len(lost) > 0 && len(added) == 0 && len(strict) == 0 {
						subkind = "lost"
					} else if len(added) > 0 && len(lost) == 0 && len(strict) == 0 {
						subkind = "added"
					}
					d := pyjson.NewObject()
					d.Set("old_number", before.shown)
					d.Set("new_number", after.shown)
					d.Set("from", pyjson.Strings(before.names))
					d.Set("to", pyjson.Strings(after.names))
					d.Set("old_markers", pyjson.Strings(before.words))
					d.Set("new_markers", pyjson.Strings(after.words))
					out = append(out, newRisk("number_qualifier", subkind, old.line, new.line, old.text, new.text, true, true, d))
				}
			}
		}
	}
	return out
}

// QualifierRisks は元の文と書き直した文の、概数と範囲を表す語の変化の恐れ (risk の並び。JSON の値の形)。
func QualifierRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.qualifierRisks(s.sentencePairs(full.hunks)), nil
}

// sortedWordKeys は集合の語を昇順に並べたもの (本家は frozenset を sorted で JSON にする)。空でも nil にしない。
func sortedWordKeys(m map[string]bool) []string {
	return append([]string{}, slices.Sorted(maps.Keys(m))...)
}
