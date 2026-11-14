package diff

import (
	"cmp"
	"slices"
	"sort"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 U の下ごしらえ: 確度の語の表 (本家の _CERTAINTY_TABLE)・語の取り出し・同じ強さの打ち消し。
// 正規表現は本家の字面のまま持つ (定数の記録と突き合わせる)。

// 事実や仕様を述べる文書の種類 (本家の COUNTED_TYPES)
var countedTypes = []string{"pr", "article", "incident", "proposal", "manual", "formal", "paper"}

const (
	certaintyVerb      = "[るうくすつぬぶむぐたい]"
	predicateEnd       = `(?=です|だ(?!け)|である|でした|だった|でござ|と(?:な|して|され)|に(?:な|し)|[。）)」』]|$)`
	predicateStem      = "(?<=[ァ-ヴー一-龥])"
	predicateBefore    = "(?<=[るうくすつぬぶむぐたいァ-ヴー一-龥])"
	certaintyYotei     = `予定(?=して(?:い|お)|しま|する(?=[。、]|$)|され)` + `|予定(?!外|より|通り|どおり)` + predicateEnd + `|` + predicateStem + `予定(?=の)|` + predicateBefore + `予定(?=で(?!は)|、)|(?<=の)予定(?=で、)`
	certaintyMikomi    = `見込(?:ま(?:れ|せ)|んで|む(?=[。、]|$)|め(?:る|ま)|み(?=ます))` + `|見込み` + predicateEnd + `|` + predicateStem + `見込み(?=の)|` + predicateBefore + `見込み(?=で(?!は)|、)|(?<=の)見込み(?=で、)` + `|見込み(?=(?:が|も|は)(?:高|大き|強|あ|立|薄|低|な))`
	certaintyAppearing = `(?<=[きしちにひみりぎじびぴえけせてねへめれげぜべぺさ])そう(?:です|だ|な|に|で(?!す)|でした|だった)` +
		`|(?<=[るうくすつぬぶむぐいた])よう(?:だ|です|でした|だった|である)(?=[。、！!よね]|$)` +
		`|(?<=[るうくすつぬぶむぐた])らし(?:い|かった)(?=[。、！!よね]|です|$)` +
		`|(?<=[るうくすつぬぶむぐいた])みたい(?:だ|です|でした|だった)(?=[。、！!よね]|$)`
)

// certaintyEntry は確度の語の表の 1 行。core でない語 (頻度や傾向の言い方、検討、意見) だけが動いた強めは --fail-on に数えない。
type certaintyEntry struct {
	level   int
	name    string
	pattern string
	core    bool
	regex   *pyre.Lazy
}

// 表の順に当て、前の語と重なった位置はあとの語に数えない (「可能性が高い」を「可能性」より先に置く)。
var certaintyTable = func() []certaintyEntry {
	rows := []certaintyEntry{
		{2, "可能性が高い", `可能性(?:が|は|も)?(?:高|大き|十分)`, true, nil},
		{1, "かもしれない", `かも(?:しれ|知れ)(?:ない|ません|ず|なかった|なけれ)`, true, nil},
		{1, "可能性", `可能性(?:が|も|は)?(?:あ|ござ|考え|残|存在|否定でき|捨てきれ|拭え)`, true, nil},
		{1, "おそれ", `(?:おそれ|恐れ|虞)(?=が|も|は|あ)`, true, nil},
		{1, "リスク", `リスク(?:が|も)(?:あ|高|伴|残)`, true, nil},
		{1, "懸念", `懸念(?:が|も)?(?:あ|され|残)`, true, nil},
		{1, "得る", `(?<=[しりきちびみいえけせれべ])(?:得る|得ます|うる)`, true, nil},
		{1, "かねない", `(?<=[しりきちびみいえけせれべ])かね(?:ない|ません|ず)`, true, nil},
		{1, "必ずしも", `必ずしも|とは限(?:らない|らず|りません|らなかった)`, true, nil},
		{1, "ことがある", `(?<![たっだ])こと(?:が|も)(?:ある|あり|あっ)|(?:ケース|とき)(?:が|も)(?:ある|あり|あっ|出)`, false, nil},
		{1, "場合がある", `場合(?:が|も)(?:ある|あり|あっ)`, false, nil},
		{1, "検討", `検討(?:中|して(?:い|お)|を(?:進め|続け|始め))`, false, nil},
		{2, "と思われる", `と(?:思|おも)われ`, true, nil},
		{2, "と考えられる", `と考えられ`, true, nil},
		{2, "と見られる", `と(?:見|み)られ`, true, nil},
		{2, "と見ている", `と(?:見|み)て(?:いる|います|おり)`, true, nil},
		{2, "見込み", certaintyMikomi, true, nil},
		{2, "予想", `予(?:想|測)(?:され|して(?:い|お)|します|する(?=[。、]|$)|です|だ(?=[。、]|$)|であ|では)`, true, nil},
		{2, "想定", `想定(?:され|して(?:い|お)|します|する(?=[。、]|$)|です|だ(?=[。、]|$)|であ|では)`, true, nil},
		{2, "見通し", `見通し(?:です|だ|で|が)`, true, nil},
		{2, "推測される", `推(?:測|定|察)(?:され|して(?:い|お)|します|する(?=[。、]|$)|です|だ(?=[。、]|$))`, true, nil},
		{2, "公算", `公算(?:が|は|も)?(?:大き|高|強)`, true, nil},
		{2, "でしょう", `でしょう(?!か)|だろう(?!か)|であろう`, true, nil},
		{2, "はず", `はず(?:だ|です|で|でした|だった|の)`, true, nil},
		{2, "おそらく", `おそらく|恐らく|たぶん|多分`, true, nil},
		{2, "示唆", `示唆(?:する|され|して|し)`, true, nil},
		{2, "そう・よう", certaintyAppearing, true, nil},
		{2, "傾向", `傾向(?:が|に)(?:ある|あり|見られ)`, false, nil},
		{2, "期待できる", `期待(?:でき|され|して(?:い|お)|します|する(?=[。、]|$))`, false, nil},
		{2, "と思う", `と(?:思|考え)(?:います|う|る|ます|ている|ています|ており|ております|ていました)|と(?:思|おも)って(?:い|お)` + `|思うのは`, false, nil},
		{3, "予定", certaintyYotei, true, nil},
		{3, "計画", `(?<=` + certaintyVerb + `)計画(?:です|だ|で|である|が(?=あ)|も(?=あ))|計画(?:して(?:い|お)|中)`, true, nil},
		{3, "方針", `(?<=` + certaintyVerb + `)方針(?:です|だ|で|である)`, true, nil},
		{3, "方向", `(?<=` + certaintyVerb + `)方向(?:です|だ|で|である)`, true, nil},
		{3, "つもり", `つもり(?:です|だ|で)`, true, nil},
		{5, "確実", `(?<!ほぼ)(?<!ほとんど)確実(?:に|だ|です|で|な)`, true, nil},
		{5, "必ず", `必ず(?!しも)`, true, nil},
		{5, "間違いなく", `(?<!ほぼ)間違いなく|間違いありません|間違いない`, true, nil},
		{5, "確定", `確定(?:です|だ|である|済|して(?:いる|います|おり)|しました(?!ら)|した(?!ら))`, true, nil},
		{5, "絶対に", `絶対に`, true, nil},
	}
	for i := range rows {
		rows[i].regex = pyre.NewLazy(rows[i].pattern, 0)
	}
	return rows
}()

// --fail-on に数えるのは、強さ 1 と 2 (可能性、見込み・推量) の core の語が、より強い語か断定に替わったときだけ。
// 予定 (強さ 3) から断定への言い切りは、書き手の予定を言い切る正しい書き換えでも起きるので、見直し候補にとどめる。
var certaintyCountedFrom = []int{1, 2}

var (
	certaintyNegAfter = pyre.NewLazy(`^[^、。]{0,4}?(?:は|が|も)?(?:な[いくかし]|ありません|ございません|低|小さ|少な|薄)`, 0)
	certaintyNegForms = pyre.NewLazy(`可能性(?:は|が|も)?(?:低|小さ|少な|な[いく]|ほぼな|ありません)|見込(?:み|め)(?:は|が)?(?:な[いく]|薄|低|ありません)`+
		`|予定(?:は|が)?(?:な[いく]|ありません)|はず(?:が|は)(?:な|ありません)|(?:おそれ|恐れ)(?:は|が)?(?:な[いく]|ありません)`+
		`|リスク(?:は|が)?(?:な[いく]|低|小さ|少な|ありません)|懸念(?:は|が)?(?:な[いく]|ありません)`+
		`|(?:あり|起こり|なり|し)(?:得|え)(?:ない|ません)`, 0)
	certaintyQuestion = pyre.NewLazy(`でしょうか[。？?]?[」』）)]*$`, 0)
)

// certWord は確度の語 1 つ (本家の (強さ, 名前, 位置, 否定つき, core) に終わりの位置を足したもの)。同じものかは値でなくポインタで見る場面がある (文を分けた先の語)。
type certWord struct {
	level    int
	name     string
	start    int
	negative bool
	core     bool
	end      int
}

// certaintyWordSpans は確度の語を、表の順に重なりを除いて、位置の順で返す (本家の _certainty_word_spans)。
func certaintyWordSpans(text string) []*certWord {
	t := pyre.NewText(text)
	var found []*certWord
	var taken [][2]int
	for _, entry := range certaintyTable {
	next:
		for _, m := range mustFinditer(entry.regex, t, 0, pyre.NoEnd) {
			for _, span := range taken {
				if span[0] < m.End(0) && m.Start(0) < span[1] {
					continue next
				}
			}
			taken = append(taken, [2]int{m.Start(0), m.End(0)})
			negative := false
			if entry.name != "必ずしも" {
				negative = mustSearchAt(certaintyNegAfter, pyre.NewText(runeSlice(pyunicode.Runes(text), m.End(0), m.End(0)+10)), 0, pyre.NoEnd) != nil
			}
			found = append(found, &certWord{level: entry.level, name: entry.name, start: m.Start(0), negative: negative, core: entry.core, end: m.End(0)})
		}
	}
	slices.SortStableFunc(found, func(a, b *certWord) int { return cmp.Compare(a.start, b.start) })
	return found
}

// certaintyNegations は確度を否定する形 (否定のついた表の語と、表にない否定の形の初めの 2 字) の数。
func certaintyNegations(text string, words []*certWord) map[string]int {
	found := map[string]int{}
	for _, w := range words {
		if w.negative {
			found[w.name]++
		}
	}
	for _, m := range mustFinditer(certaintyNegForms, pyre.NewText(text), 0, pyre.NoEnd) {
		whole, _ := m.Group(0)
		found[runeSlice(pyunicode.Runes(whole), 0, 2)]++
	}
	return found
}

func notNegative(words []*certWord) []*certWord {
	var out []*certWord
	for _, w := range words {
		if !w.negative {
			out = append(out, w)
		}
	}
	return out
}

// removeWord は list から最初の (値が等しい) 語を取り除く (Python の list.remove)。
func removeWord(list []*certWord, word *certWord) []*certWord {
	if i := indexWord(list, word); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	return list
}

// indexWord は list の最初の (値が等しい) 語の位置 (なければ -1)。
func indexWord(list []*certWord, word *certWord) int {
	for i, w := range list {
		if *w == *word {
			return i
		}
	}
	return -1
}

// cancelPairs は同じ強さの語を打ち消し、残った語と、打ち消した (元の語, 後の語) の組を返す (本家の _cancel_pairs)。
// 同じ名前の語から先に打ち消す (残る語の core を、打ち消した語から借りない)。
func cancelPairs(before, after []*certWord) (restOld, restNew []*certWord, pairs [][2]*certWord) {
	restOld, restNew = slices.Clone(before), slices.Clone(after)
	for _, sameName := range []bool{true, false} {
		for _, word := range slices.Clone(restOld) {
			for _, other := range restNew {
				if other.level == word.level && (!sameName || other.name == word.name) {
					restOld = removeWord(restOld, word)
					restNew = removeWord(restNew, other)
					pairs = append(pairs, [2]*certWord{word, other})
					break
				}
			}
		}
	}
	return restOld, restNew, pairs
}

func cancelSameLevel(before, after []*certWord) (restOld, restNew []*certWord) {
	restOld, restNew, _ = cancelPairs(before, after)
	return restOld, restNew
}

// cancelWithContext は隣の組にならなかった文の語で打ち消す。使った語は pools から取り除き、2 つの組で使い回さない。
func cancelWithContext(rest []*certWord, pools []*[]*certWord) []*certWord {
	var kept []*certWord
	for _, word := range rest {
		used := false
		for _, pool := range pools {
			if i := slices.IndexFunc(*pool, func(w *certWord) bool { return w.level == word.level }); i >= 0 {
				*pool = removeWord(*pool, (*pool)[i])
				used = true
				break
			}
		}
		if !used {
			kept = append(kept, word)
		}
	}
	return kept
}

func sortCertWords(words []*certWord) {
	sort.SliceStable(words, func(i, j int) bool {
		if words[i].level != words[j].level {
			return words[i].level < words[j].level
		}
		return words[i].start < words[j].start
	})
}

func certaintyQuestionText(text string) bool {
	return isQuestion(text) || mustSearchAt(certaintyQuestion, pyre.NewText(pystr.RStrip(text)), 0, pyre.NoEnd) != nil
}
