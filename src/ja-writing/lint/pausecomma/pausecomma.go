// Package pausecomma は ja-writing lint の群 G13 (規則 pause_comma: 短い主語・主題・目的語のすぐあとの読点) を持つ。本家 yomiyasu_lint.py の 2967〜3380 行の写し。
// 位置は全部、Python の str の添字と同じコードポイントの添字。正規表現は本家と同じパターンを internal/pyre で照合する。
// lint の骨組みが Hits・ObjectHits・LineHits を呼び、G16 (ai_phrase) が TENotVerb を、G17 (slide_comma) が TEAttrObject を使う。
package pausecomma

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// Hit は (助詞の位置, かたまり, 句)。本家のタプルと同じ順で並べる (hitLess)。
type Hit struct {
	Pos    int
	Chunk  string
	Phrase string
}

func hitLess(a, b Hit) int {
	if a.Pos != b.Pos {
		return a.Pos - b.Pos
	}
	if c := strings.Compare(a.Chunk, b.Chunk); c != 0 {
		return c
	}
	return strings.Compare(a.Phrase, b.Phrase)
}

// pauseLazy は本家の _lazy_compile (初めて使うときに組む)。pyre の照合は時間の上限を持たないので、失敗は内部の矛盾だけで panic にする。
func pauseLazy(pattern string) func() *pyre.Pattern {
	return sync.OnceValue(func() *pyre.Pattern { return pyre.MustCompile(pattern, 0) })
}

func pauseMust(m *pyre.Match, err error) *pyre.Match {
	if err != nil {
		panic(fmt.Errorf("lint: pause_comma の正規表現の照合に失敗しました: %w", err))
	}
	return m
}

func pauseSearch(p func() *pyre.Pattern, s string) bool {
	return pauseMust(p().Search(pyre.NewText(s), 0, pyre.NoEnd)) != nil
}

func pauseMatch(p func() *pyre.Pattern, s string) bool {
	return pauseMust(p().Match(pyre.NewText(s), 0, pyre.NoEnd)) != nil
}

func pauseMatchAt(p func() *pyre.Pattern, t pyre.Text, pos int) bool {
	return pauseMust(p().Match(t, pos, pyre.NoEnd)) != nil
}

func pauseStr(r []rune) string { return pyunicode.FromRunes(r) }

func pauseRuneIn(r rune, set string) bool { return strings.ContainsRune(set, r) }

const (
	pauseKanji         = "一-龥々〆ヵヶ"
	pauseOpen          = "「『（(“【［["
	pauseClose         = "」』）)”】］]"
	pauseBoundary      = "、，,。！？!?：:"
	pauseLeadSymbols   = " \t\u3000・※•●○◆◇■□▶▷→＊*-"
	pauseMaxLen        = 15
	pauseListItemMax   = 18
	pauseShortClause   = 10
	pauseObjectMax     = 6
	pauseObjectListMax = 8
)

// pauseVerbAttr は動詞の連体の形 (本家の _PAUSE_VERB_ATTR)。G17 (slide_comma) も使う。
const pauseVerbAttr = `(?:[るたうくすつぬむぶぐ]|ない|ている|ていた|された|される|できる|した|する|った|んだ|いた|えた|れた|せた|` +
	`きた|だった|ような)`

var (
	pauseWord     = pauseLazy(`[` + pauseKanji + `ぁ-んァ-ヴーA-Za-z0-9０-９Ａ-Ｚａ-ｚ]`)
	pauseCheckbox = pauseLazy(`^\[[ xX]\]\s*`)
)

// pauseCompound は助詞のかたまり (長いものから)。本家は set から作るので同じ長さの語の順は PYTHONHASHSEED で変わるが、
// 同じ位置で終わる同じ長さの 2 語は同時に当たらない (別の字の並び) ので、照合の答えは順に依らない。Go は (長さの降順, 字の順) で固定する。
var pauseCompound = func() []string {
	words := []string{
		"においては", "にとっては", "については", "に対しては", "によっては", "としては", "としても",
		"についても", "においても", "にとっても", "に対しても",
		"からは", "までは", "よりは", "へは", "とは", "では", "には", "にも", "でも", "とも", "からも",
		"までも", "へも", "ても",
	}
	slices.SortStableFunc(words, func(a, b string) int {
		if la, lb := pyunicode.RuneCount(a), pyunicode.RuneCount(b); la != lb {
			return lb - la
		}
		return strings.Compare(a, b)
	})
	return words
}()

// pauseSkipChunks は拾わない助詞のかたまり (接続・逆接・譲歩)。
var pauseSkipChunks = map[string]bool{"でも": true, "ても": true, "とも": true, "としても": true, "によっては": true}

// pauseConnective は文頭の接続・前置き・手紙の定型。句全体がこれに当たれば拾わない。
var pauseConnective = map[string]bool{}

func init() {
	for _, w := range []string{
		"それでは", "これでは", "では", "しかも", "けれども", "それでも", "それにしても", "とはいえ",
		"ただ", "もっとも", "それとも", "そもそも", "つきましては", "少なくとも", "そのためには",
		"今でも", "なかでも", "中でも", "それには", "まずは", "最後には", "さらには", "ひいては", "ついては",
		"このたびは", "この度は", "平素は", "日頃は", "日ごろは", "先日は", "昨日は", "本日は", "今回も",
	} {
		pauseConnective[w] = true
	}
}

var (
	pauseConnectiveEnd = pauseLazy(`的には$|的にも$`)
	// ほかの規則 (meta_intro・meta_filler) がすでに拾う文頭の句。1 つの読点に 2 つの候補を出さない。
	pauseCovered   = pauseLazy(`^(?:本記事では|ポイントは|(?:まず|ここで)?(?:重要|大切|大事)なのは|避けたいのは)$`)
	pauseGaConjEnd = pauseLazy(`(?:です|ます|でした|ました|ません|ませんでした|だ|だった|である|であった|た|る|い|ない|なかった|` +
		`う|く|ぐ|す|つ|ぬ|ぶ|む|よう|たい|らしい|ん|ある|いる|した|する|れる|せる|ている|ていた|ておく|けれど)$`)
	pauseGaNounHira = pauseLazy(`(?:さん|さま|様|くん|ちゃん|こと|もの|とき|ところ|ほう|ため|の|さ|たち|ら|など|だけ|まで|ばかり|` +
		`くらい|ぐらい|ほど|こそ|もと|うち|あと|まえ|わけ|はず|よう|みんな|それ|これ|あれ|どれ|ここ|そこ|どこ|誰|僕|私)$`)
	pauseNounishEnd = pauseLazy(`[` + pauseKanji + `ァ-ヴーｦ-ﾟA-Za-z0-9０-９Ａ-Ｚａ-ｚ」』）)\]>%％]$`)
	pauseClauseIn   = pauseLazy(`(?<=[` + pauseKanji + `ぁ-んァ-ヴー])` + pauseVerbAttr +
		`(?=[` + pauseKanji + `ァ-ヴーｦ-ﾟA-Za-z0-9０-９Ａ-Ｚａ-ｚ【「『（(［\[]|の|こと|もの|とき|ところ|ほう)`)
	pauseOkuriNoun = pauseLazy(`(?:問い合わせ|打ち合わせ|取り消し|申し込み|引き渡し|割り当て|振り込み|書き込み|読み込み|受け付け|` +
		`立ち上げ|切り替え|切り出し|入れ替え|洗い出し|聞き取り|見積もり|取り込み|呼び出し|払い戻し|` +
		`組み合わせ|貸し出し|締め|売り場|手当て|付け直し|使い方|考え方|進め方|書き方|作り)`)
	// 分裂文 (〜のは、〜のが、〜ことが、)。
	pauseCleft = pauseLazy(pauseVerbAttr + `(?:の|こと)$|(?:かった|しい|たい|ない|な|だった)の$`)
	// 条件・場面・時の枠。
	pauseFrame = pauseLazy(pauseVerbAttr + `(?:場合|とき|時|際|ころ|あと|後|うち|ため|上|うえ|前|間|限り|範囲|以上|` +
		`あいだ|のち|直前|直後|最中|途中|以降|以前|前後)(?:に|で)?$|` +
		`(?:場合|とき|際)(?:に|で)?$|` + pauseVerbAttr + `$`)
	pausePerson        = pauseLazy(`(?:僕|ぼく|私|わたし|わたくし|俺|おれ|あたし|自分|彼女|彼|我々|われわれ|あなた|君|きみ)(?:たち|ら|達)?$|(?:の人|人たち|人々)$`)
	pauseDemonstrative = pauseLazy(`^(?:それ|これ|あれ|そこ|ここ|あそこ|こちら|そちら|どれ|どこ)$`)
	pauseFixedSubj     = pauseLazy(`(?:必要|こと|可能性|恐れ|おそれ|余地)$`)
	pauseNominalEnd    = pauseLazy(`(?:こと|もの)$|` + pauseVerbAttr + `の$`)
	// 手紙の時候の挨拶「皆様には、ますます〜」(お客様は除く)。
	pauseLetterAddress  = pauseLazy(`(?<!客)(?:様|さま|各位)$`)
	pauseLetterGreeting = pauseLazy(`(?:ますます|益々|いよいよ|一層)`)
	// 並べの項目の前の句 (名詞で終わる)。今回・現在のような副詞の名詞は 1 つだけでは並べにしない。
	pauseAdverbialNoun = pauseLazy(`(?:結果|ため|上|うえ|際|場合|とき|ところ|一方|今回|現在|今後|以前|当日|通常|実際|最初|最後|当初|従来)$`)
	pauseItemStart     = pauseLazy(`^\s*(?:[-*+•]|\d+[.)．])\s`)
	pauseLabelLine     = pauseLazy(`^[^、，,]{0,12}$(?<![ぁ-ん。！？!?」』）)])`)
	pauseLaterMo       = pauseLazy(`(?<!最)[` + pauseKanji + `ァ-ヴーA-Za-z0-9）」』]も(?!の)`)
	pauseListAnd       = pauseLazy(`[` + pauseKanji + `ァ-ヴーA-Za-z0-9さみ](?:と|や)[` + pauseKanji + `ァ-ヴーA-Za-z0-9]`)
)

// TENotVerb は「て」「で」で終わっても動詞のて形でない語 (本家の _TE_NOT_VERB。並びも本家と同じ)。
// pause_comma の掛かり先の形 (pauseTE) と、G16 (ai_phrase) のて形の体言止めが使う。
var TENotVerb = []string{"について", "によって", "として", "に関して", "に対して", "を通して", "にとって", "につれて", "において",
	"にあたって", "に当たって", "に際して", "をめぐって", "を巡って", "に沿って", "にわたって", "に先立って",
	"そして", "さて", "すべて", "全て", "かつて", "やがて", "改めて", "あらためて", "初めて", "はじめて", "決して",
	"けっして", "大して", "たいして", "極めて", "きわめて", "あえて", "敢えて", "かえって", "却って", "至って",
	"総じて", "概して", "がいして", "おしなべて", "強いて", "果たして", "はたして", "従って", "したがって",
	"よって", "次いで", "ついで", "併せて", "あわせて", "合わせて", "続いて", "揃って", "そろって", "あて", "宛て",
	"めがけて", "目がけて", "を通じて", "に応じて", "に基づいて", "に基いて", "に加えて", "に向けて", "を受けて",
	"をもって", "に従って", "を含めて", "に限って", "にかけて", "に比べて", "に伴って", "を除いて", "を経て",
	"にて", "せいで", "くらいで", "ぐらいで"}

// pauseTE は動詞の「〜て」(本家の _PAUSE_TE)。TENotVerb の語ごとの否定の後読みを連ねる (re.escape は pyre.Escape で同じ字にする)。
var pauseTE = func() string {
	var b strings.Builder
	b.WriteString(`(?:(?<=[っいしきぎちにびみりえけげせぜてねべめれ])て|(?<=[見出寝着似煮経])て|(?<=[んい])で)`)
	for _, w := range TENotVerb {
		b.WriteString("(?<!" + pyre.Escape(w) + ")")
	}
	b.WriteString(`(?!い[るたなま]|る|き[たてま]|お[りくいき]|あ[るりげ]|は|も)`)
	return b.String()
}()

var (
	// teAttrObject は読点のあとの「〜て」+連体の形の述語+名詞+「を」(店長は、慌てて逃げた客を追いかけた)。G17 (slide_comma) も使う。
	teAttrObject = pauseLazy(`[^、，,。！？!?]{1,8}?` + pauseTE + `[^、，,。！？!?]{1,10}?` + pauseVerbAttr +
		`[` + pauseKanji + `ァ-ヴー]{1,8}を`)
	// pauseObjectAttach は目的語の「を、」のあとの連体の形の節+名詞+助詞 (鍵を、なくした人に届けた)。
	pauseObjectAttach = pauseLazy(`(?:[^、，,。！？!?]{1,8}?` + pauseTE + `)?[^、，,。！？!?]{0,10}?` + pauseVerbAttr +
		`[` + pauseKanji + `ァ-ヴー]{1,8}(?:に|へ|と|から|より|を)`)
	pauseObjectNoun = pauseLazy(`(?:この|その|あの|どの)?[` + pauseKanji + `ァ-ヴーA-Za-z0-9０-９Ａ-Ｚａ-ｚ・]+` +
		`(?:の[` + pauseKanji + `ァ-ヴーA-Za-z0-9０-９Ａ-Ｚａ-ｚ]+)?`)
)

var pauseFrameChunks = map[string]bool{"は": true, "も": true, "には": true, "では": true, "からは": true, "までは": true}

var pauseObjectQuestion = map[string]bool{"何": true, "誰": true, "何か": true, "誰か": true, "何事": true, "何もの": true}

// 関数の中で書く正規表現 (本家の re.search などの第 1 引数)。
var (
	pauseHiraRun2End   = pauseLazy(`[ぁ-ん]{2,}$`)
	pauseParticleEnd   = pauseLazy(`[をにでへとがはも]$`)
	pauseGaHa          = pauseLazy(`(が|は)`)
	pauseFixedFollow   = pauseLazy(`(?:あ|な|でき|大き|高)`)
	pauseHaParticleEnd = pauseLazy(`(?:で|に|と|て|から|まで|へ|より|だけ|ので)$`)
	pauseTeDeEnd       = pauseLazy(`(?:て|で)$`)
	pauseMoConjEnd     = pauseLazy(`(?:けれど|しか|それで|それに|ど)$`)
	pauseQuoteThenTo   = pauseLazy(`[」』”）)]と`)
	pauseInnerStop     = pauseLazy(`[、。！？!?]`)
	pauseSpace         = pauseLazy(`\s`)
	pauseInnerSubject  = pauseLazy(`[^、]が(?!、)|[^でにとて]は(?!、)`)
	pauseInnerList     = pauseLazy(`[` + pauseKanji + `ァ-ヴーA-Za-z0-9さみ](と|や)[` + pauseKanji + `ァ-ヴーA-Za-z0-9]`)
	pauseKanaCommaEnd  = pauseLazy(`[ぁ-ん、，,]$`)
)

// pauseCommaChunk は位置 i で終わる助詞のかたまりと、その始まり (本家の _pause_comma_chunk)。
// 本家の `prev in "こひもあ"` は prev が空 (かたまりが文字列の頭から始まる) のときも真なので、そのかたまりは飛ばす。
func pauseCommaChunk(t []rune, i int) (string, int) {
	for _, comp := range pauseCompound {
		c := []rune(comp)
		st := i + 1 - len(c)
		if st < 0 || !slices.Equal(t[st:i+1], c) {
			continue
		}
		prevIn := func(set string) bool { return st == 0 || pauseRuneIn(t[st-1], set) }
		if c[0] == 'と' && prevIn("こひもあ") {
			note("b-to")
			continue
		}
		if (comp == "では" || comp == "でも") && prevIn("ん") {
			note("b-n")
			continue
		}
		if c[0] == 'に' && prevIn("な") {
			note("b-na")
			continue
		}
		return comp, st
	}
	return string(t[i]), i
}

// pauseSplitKeep は re.split(r"(、|。)", s) (区切りも要素に残す)。
func pauseSplitKeep(s []rune) []string {
	var parts []string
	start := 0
	for k, r := range s {
		if r == '、' || r == '。' {
			parts = append(parts, pauseStr(s[start:k]), string(r))
			start = k + 1
		}
	}
	return append(parts, pauseStr(s[start:]))
}

// pauseCommaNextSubject は読点のあとに別の主語や主題が続くか (本家の _pause_comma_next_subject)。
func pauseCommaNextSubject(after []rune) bool {
	parts := pauseSplitKeep(after)
	var text strings.Builder
	for k := 0; k < len(parts); k += 2 {
		seg := parts[k]
		text.WriteString(seg)
		// 並べの 1 項目は目的語の「を」を含まない (「〜方法に変え、」は項目にしない)。
		isListItem := pyunicode.RuneCount(seg) <= pauseListItemMax && !pauseSearch(pauseHiraRun2End, seg) && !pauseSearch(pauseParticleEnd, seg) &&
			!strings.Contains(seg, "を") && k+1 < len(parts) && parts[k+1] == "、"
		if !isListItem {
			break
		}
		text.WriteString("、")
	}
	tr := pyunicode.Runes(text.String())
	head := pyre.NewText(pauseStr(tr[:min(60, len(tr))]))
	ms, err := pauseGaHa().Finditer(head, 0, pyre.NoEnd)
	if err != nil {
		panic(fmt.Errorf("lint: pause_comma の正規表現の照合に失敗しました: %w", err))
	}
	for _, m := range ms {
		before := pauseStr(tr[:m.Start(0)])
		if before == "" || strings.HasSuffix(before, "、") {
			continue
		}
		particle, _ := m.Group(1)
		if particle == "が" {
			if pauseSearch(pauseGaConjEnd, before) && !pauseSearch(pauseGaNounHira, before) {
				continue
			}
			if pauseSearch(pauseFixedSubj, before) && pauseMatch(pauseFixedFollow, pauseStr(tr[m.End(0):])) {
				continue
			}
		}
		// 「〜ことが」「〜するのが」は名詞化した主語なので、助詞の「と」「の」と分ける。
		nominal := pauseSearch(pauseNominalEnd, before)
		if particle == "は" && !nominal && pauseSearch(pauseHaParticleEnd, before) {
			continue
		}
		br := pyunicode.Runes(before)
		if pauseRuneIn(br[len(br)-1], "をにへとのやか") && !nominal {
			continue
		}
		return true
	}
	return false
}

// pauseCommaQuoteBlocks は句の中のかっこの中身 (本家の _pause_comma_quote_blocks)。
func pauseCommaQuoteBlocks(phrase []rune) [][]rune {
	var out [][]rune
	depth := 0
	var cur []rune
	for _, ch := range phrase {
		if pauseRuneIn(ch, pauseOpen) {
			depth++
			if depth == 1 {
				cur = nil
				continue
			}
		}
		if pauseRuneIn(ch, pauseClose) && depth > 0 {
			depth--
			if depth == 0 {
				out = append(out, cur)
				continue
			}
		}
		if depth > 0 {
			cur = append(cur, ch)
		}
	}
	return out
}

// pauseContext は深さ・句の頭・文の番号・文の範囲・文頭の判定の累積を行につき 1 回作る (本家の _pause_comma_context)。
type pauseContext struct {
	depth       []int
	starts      []int // 長さ len(t)+1
	sentenceIDs []int
	spans       [][2]int
	nonlead     []int // 長さ len(t)+1
}

func newPauseContext(t []rune) pauseContext {
	c := pauseContext{starts: []int{0}, nonlead: []int{0}}
	depth, boundary, sentenceStart := 0, 0, 0
	for i, ch := range t {
		if pauseRuneIn(ch, pauseOpen) {
			depth++
		}
		c.depth = append(c.depth, depth)
		c.sentenceIDs = append(c.sentenceIDs, len(c.spans))
		if depth == 0 {
			if pauseRuneIn(ch, pauseBoundary) {
				boundary = i + 1
			}
			if pauseRuneIn(ch, "。！？!?") {
				c.spans = append(c.spans, [2]int{sentenceStart, i})
				sentenceStart = i + 1
			}
		}
		c.starts = append(c.starts, boundary)
		lead := 0
		if !pauseRuneIn(ch, pauseLeadSymbols+pauseOpen) {
			lead = 1
		}
		c.nonlead = append(c.nonlead, c.nonlead[len(c.nonlead)-1]+lead)
		if pauseRuneIn(ch, pauseClose) && depth > 0 {
			depth--
		}
	}
	c.spans = append(c.spans, [2]int{sentenceStart, len(t)})
	return c
}

// unquoted はかっこの中の字を「〇」に替えた文字列 (あとの主語や並べに、かっこの中の助詞と読点を数えない)。
func (c pauseContext) unquoted(t []rune) []rune {
	out := make([]rune, len(t))
	for i, ch := range t {
		if c.depth[i] != 0 {
			ch = '〇'
		}
		out[i] = ch
	}
	return out
}

// pauseCommaPhraseEnd は位置 k から、深さ 0 の次の句の区切りの位置 (本家の _pause_comma_phrase_end)。
func pauseCommaPhraseEnd(t []rune, k int, depth []int) int {
	for k < len(t) && (depth[k] != 0 || !pauseRuneIn(t[k], pauseBoundary)) {
		k++
	}
	return k
}

// pauseCommaPrevSegment は句の頭 j の直前が読点のとき、その前の句 (本家の _pause_comma_prev_segment)。無ければ空。
// 本家は None と空の句を呼び出し側で同じに扱う (どちらも偽) ので、Go は空で返す。
func pauseCommaPrevSegment(t []rune, j int, starts []int) (string, int) {
	if j < 2 || !pauseRuneIn(t[j-1], "、，,") {
		return "", 0
	}
	k := starts[j-2]
	return pystr.LStripChars(pauseStr(t[k:j-1]), pauseLeadSymbols), k
}

// pauseCommaListBefore は句が並んだ名詞の最後の項目か (「A、B、Cは、」「AやB、Cは、」。本家の _pause_comma_list_before)。
func pauseCommaListBefore(t []rune, j int, starts []int) bool {
	prev, k := pauseCommaPrevSegment(t, j, starts)
	if prev == "" || !pauseSearch(pauseNounishEnd, prev) || pauseSearch(pauseAdverbialNoun, prev) {
		return false
	}
	if pauseSearch(pauseListAnd, prev) {
		return true
	}
	prev2, _ := pauseCommaPrevSegment(t, k, starts)
	return prev2 != "" && pauseSearch(pauseNounishEnd, prev2) && !pauseSearch(pauseAdverbialNoun, prev2)
}

// pauseCommaPhrase は句の頭 j から位置 end までを、箇条書きの記号とチェックボックスを除いて返す。
func pauseCommaPhrase(t []rune, j, end int) string {
	s := pystr.LStripChars(pauseStr(t[j:end]), pauseLeadSymbols)
	s, err := pauseCheckbox().Sub("", pyre.NewText(s), 0)
	if err != nil {
		panic(fmt.Errorf("lint: pause_comma の正規表現の照合に失敗しました: %w", err))
	}
	return pystr.LStripChars(s, pauseLeadSymbols)
}

// onCondition は試験が除外の条件ごとの通過を数える口 (本家の a〜t と目的語の条件の名前を受ける)。製品では nil で、何もしない。
var onCondition func(name string)

func note(name string) {
	if onCondition != nil {
		onCondition(name)
	}
}

// gaIsConjunction は s で終わる句のあとの「が」が述語のあとの逆接・前置き (「〜ですが、」) か。名詞や「〜のが」の主語は除く。
// pauseNounishEnd の否定は効かない (述語の終わりは全部ひらがなで終わり、名詞の終わりの字の集合はひらがなを含まないので、両方に当たる s は無い)。
// 外す書き換えは試験で緑のまま通るが、本家と同じ式のまま残す (本家が述語の終わりに漢字を足したときに要る)。
func gaIsConjunction(s string) bool {
	return pauseSearch(pauseGaConjEnd, s) && !pauseSearch(pauseGaNounHira, s) && !pauseSearch(pauseNounishEnd, s)
}

// Hits は見える 1 行 (か段落としてつないだ文字列) から (助詞の位置, かたまり, 句) を出現順に返す (本家の _pause_comma_hits)。
// 判定は本家のコメントの a〜t の順で、当てはまる除外の条件があれば残す。
func Hits(plain string) []Hit {
	if !strings.Contains(plain, "は、") && !strings.Contains(plain, "が、") && !strings.Contains(plain, "も、") {
		return nil
	}
	t := pyunicode.Runes(plain)
	text := pyre.NewText(plain)
	c := newPauseContext(t)
	var positions []int
	for i := 0; i+1 < len(t); i++ {
		if pauseRuneIn(t[i], "はがも") && t[i+1] == '、' {
			positions = append(positions, i)
		}
	}
	// r の同じかたまりの出現数。複合助詞の接尾辞も数える。
	counts := make([]map[string]int, len(c.spans))
	for k := range counts {
		counts[k] = map[string]int{}
	}
	chunks := append(slices.Clone(pauseCompound), "は", "が", "も")
	for _, i := range positions {
		// 引用の中と、述語のあとの「が、」(逆接) は並べに数えない。
		if c.depth[i] > 0 {
			continue
		}
		if t[i] == 'が' {
			pre := pauseStr(t[max(0, i-8):i])
			if gaIsConjunction(pre) {
				continue
			}
		}
		inSentence := counts[c.sentenceIDs[i]]
		for _, chunk := range chunks {
			cr := []rune(chunk)
			if st := i + 1 - len(cr); st >= 0 && slices.Equal(t[st:i+1], cr) {
				inSentence[chunk]++
			}
		}
	}
	var hits []Hit
	var unquoted []rune
	for _, i := range positions {
		// a: 引用の中。b: 長い助詞のかたまりから照合。c: 接続・譲歩。
		if c.depth[i] > 0 {
			note("a")
			continue
		}
		chunk, cstart := pauseCommaChunk(t, i)
		if pauseSkipChunks[chunk] {
			note("c")
			continue
		}
		// d: 句の頭の箇条書き記号とチェックボックスを除く。
		j := c.starts[cstart]
		phrase := pauseCommaPhrase(t, j, cstart)
		if phrase == "" || !pauseSearch(pauseWord, phrase) {
			note("d")
			continue
		}
		pr := pyunicode.Runes(phrase)
		// e: 引用から続く句。f: 逆接の「が」。g: て形。
		if pauseRuneIn(pr[0], pauseClose) || (pr[0] == 'と' && j >= 1 && pauseRuneIn(t[j-1], "、，,")) {
			note("e")
			continue
		}
		if chunk == "が" && gaIsConjunction(phrase) {
			note("f")
			continue
		}
		if (chunk == "は" || chunk == "も") && pauseSearch(pauseTeDeEnd, phrase) {
			note("g")
			continue
		}
		if chunk == "も" && pauseSearch(pauseMoConjEnd, phrase) {
			note("g-mo")
			continue
		}
		// h: 接続・定型。i: 既存規則が拾う文頭。
		span := c.spans[c.sentenceIDs[i]]
		atStart := c.nonlead[j] == c.nonlead[span[0]]
		whole := phrase + chunk
		if pauseConnective[whole] || pauseSearch(pauseConnectiveEnd, whole) {
			note("h")
			continue
		}
		if atStart && pauseMatch(pauseCovered, whole) {
			note("i")
			continue
		}
		if chunk == "には" && pauseSearch(pauseLetterAddress, phrase) && pauseMatchAt(pauseLetterGreeting, text, i+2) {
			note("i-letter")
			continue
		}
		// j: 引用を含む主語。k: 空白を除いて 15 字を超える句。
		if pauseSearch(pauseQuoteThenTo, phrase) {
			note("j")
			continue
		}
		if slices.ContainsFunc(pauseCommaQuoteBlocks(pr), func(inner []rune) bool {
			return pauseSearch(pauseInnerStop, pauseStr(inner)) || len(inner) > 12
		}) {
			note("j-block")
			continue
		}
		plen := pyunicode.RuneCount(pauseSub(pauseSpace, phrase, func(*pyre.Match) string { return "" }))
		if plen > pauseMaxLen {
			note("k")
			continue
		}
		// l: 分裂文。m: 条件・場面。n: 問いの節。
		if (chunk == "は" || chunk == "が") && pauseSearch(pauseCleft, phrase) {
			note("l")
			continue
		}
		if pauseFrameChunks[chunk] && pauseSearch(pauseFrame, phrase) {
			note("m")
			continue
		}
		if pr[len(pr)-1] == 'か' && plen > pauseShortClause {
			note("n")
			continue
		}
		// o: 句の中の主語。p: 送り仮名の名詞を伏せて節を調べる。
		if len(pr) > 1 && pauseSearch(pauseInnerSubject, pauseStr(pr[:len(pr)-1])) {
			note("o")
			continue
		}
		masked := pauseSub(pauseOkuriNoun, phrase, func(m *pyre.Match) string {
			g, _ := m.Group(0)
			return strings.Repeat("名", pyunicode.RuneCount(g))
		})
		if plen > pauseShortClause && pauseSearch(pauseClauseIn, masked) {
			note("p")
			continue
		}
		// q: あとの主語。先頭 60 字と、その直後の定型を判定できれば足りるので 84 字に限る。かっこの中の助詞と読点は数えない。
		if unquoted == nil {
			unquoted = c.unquoted(t)
		}
		after := unquoted[i+2 : max(i+2, min(span[1]+1, i+2+84, len(t)))]
		if pauseCommaNextSubject(after) || pauseMatch(teAttrObject, pauseStr(after)) {
			note("q")
			continue
		}
		// r: 同じ文で並んだ句、句の中で並んだ語、直前の「と、」「や、」。
		if counts[c.sentenceIDs[i]][chunk] > 1 {
			note("r")
			continue
		}
		if pauseSearch(pauseInnerList, phrase) {
			note("r-inner")
			continue
		}
		if j >= 2 && pauseRuneIn(t[j-2], "とや") && t[j-1] == '、' {
			note("r-before")
			continue
		}
		if pauseCommaListBefore(t, j, c.starts) {
			note("r-list")
			continue
		}
		if strings.HasSuffix(chunk, "も") && pauseSearch(pauseLaterMo, pauseStr(t[i+2:pauseCommaPhraseEnd(t, i+2, c.depth)])) {
			note("r-mo")
			continue
		}
		// s: 人と指示語。t: 外すと同じ字がつながる所。
		if pauseSearch(pausePerson, phrase) || pauseMatch(pauseDemonstrative, phrase) {
			note("s")
			continue
		}
		if i+2 < len(t) && t[i+2] == t[i] {
			note("t")
			continue
		}
		note("hit")
		hits = append(hits, Hit{i, chunk, phrase})
	}
	return hits
}

func pauseSub(p func() *pyre.Pattern, s string, repl func(*pyre.Match) string) string {
	out, err := p().SubFunc(repl, pyre.NewText(s), 0)
	if err != nil {
		panic(fmt.Errorf("lint: pause_comma の正規表現の照合に失敗しました: %w", err))
	}
	return out
}

// ObjectHits は見える 1 行から、短い目的語の「を、」の (「を」の位置, "を", 目的語) を出現順に返す (本家の _pause_comma_object_hits、2.5.0)。
func ObjectHits(plain string) []Hit {
	if !strings.Contains(plain, "を、") && !strings.Contains(plain, "を，") && !strings.Contains(plain, "を,") {
		return nil
	}
	t := pyunicode.Runes(plain)
	c := newPauseContext(t)
	var hits []Hit
	var unquoted []rune
	var unquotedText pyre.Text
	for i := 0; i+1 < len(t); i++ {
		if t[i] != 'を' || !pauseRuneIn(t[i+1], "、，,") || c.depth[i] > 0 {
			continue
		}
		j := c.starts[i]
		phrase := pauseCommaPhrase(t, j, i)
		if pyunicode.RuneCount(phrase) > pauseObjectMax || pauseMust(pauseObjectNoun().FullMatch(pyre.NewText(phrase), 0, pyre.NoEnd)) == nil ||
			pauseObjectQuestion[phrase] {
			note("obj-noun")
			continue
		}
		prev, _ := pauseCommaPrevSegment(t, j, c.starts)
		if prev != "" && (strings.HasSuffix(prev, "を") || (pyunicode.RuneCount(prev) <= pauseObjectListMax && pauseSearch(pauseNounishEnd, prev))) {
			note("obj-prev")
			continue // Aを、Bを、の 2 つ目からと、A、Bを、
		}
		if slices.Contains(t[i+2:pauseCommaPhraseEnd(t, i+2, c.depth)], 'を') {
			note("obj-second")
			continue // Aを、Bを、の 1 つ目と、読点のあとの句にもう 1 つの目的語
		}
		if unquoted == nil {
			unquoted = c.unquoted(t)
			unquotedText = pyre.NewText(pauseStr(unquoted))
		}
		end := min(c.spans[c.sentenceIDs[i]][1]+1, i+2+84, len(t))
		if pauseCommaNextSubject(unquoted[i+2 : max(i+2, end)]) {
			note("obj-subject")
			continue // 読点のあとに別の主語や主題が続く
		}
		if pauseMatchAt(pauseObjectAttach, unquotedText, i+2) {
			note("obj-attach")
			continue // 掛かり先を決める読点 (鍵を、なくした人に)
		}
		note("obj-hit")
		hits = append(hits, Hit{i, "を", phrase})
	}
	return hits
}

// Fallback は 1 つの文字列の主題・主語の当たりと、objects が真なら目的語の「を、」の当たりを位置の順に混ぜて返す
// (本家の lint_text が行をつなぐ表に無い行で当て直す形 (5885〜5887 行) と、_pause_comma_line_hits の flush の形。objects は doc_type != "essay")。
func Fallback(plain string, objects bool) []Hit {
	hits := Hits(plain)
	if !objects {
		return hits
	}
	hits = append(slices.Clone(hits), ObjectHits(plain)...)
	slices.SortFunc(hits, hitLess)
	return hits
}

type pauseUnitRow struct {
	line  int
	plain string
}

// LineHits は段落と箇条書きの 1 項目ごとに見える行をつないで当てる (行をまたぐ文と引用を 1 つに読む。本家の _pause_comma_line_hits)。
// 行番号から、その行の見える字での (助詞の位置, かたまり, 句) の並びへの表を返す。表にある行は、当たりが無くても空の並びを持つ。
// visible は本家の _prose_visible_text (T3) で、行番号から見える字を返す。objects が真なら短い目的語の「を、」も位置の順に混ぜる。
func LineHits(a *visibility.Analysis, visible func(lineNo int) string, objects bool) map[int][]Hit {
	out := map[int][]Hit{}
	flush := func(unit []pauseUnitRow, kind string) []pauseUnitRow {
		if len(unit) == 0 {
			return unit
		}
		var joined strings.Builder
		offsets := make([]int, len(unit))
		pos := 0
		for k, row := range unit {
			offsets[k] = pos
			out[row.line] = []Hit{}
			pos += pyunicode.RuneCount(row.plain)
			joined.WriteString(row.plain)
		}
		js := joined.String()
		// 段落が句点なしにかなや読点で終わるときは、文の途中で切れていてあとが見えないので、その文では拾わない (名詞で終わる題名のような行は拾う)。
		tail := pos
		if kind == "paragraph" && pauseSearch(pauseKanaCommaEnd, pystr.RStrip(js)) {
			jr := pyunicode.Runes(js)
			tail = -1
			for k := len(jr) - 1; k >= 0; k-- {
				if pauseRuneIn(jr[k], "。！？!?") {
					tail = k
					break
				}
			}
		}
		hits := Fallback(js, objects)
		for _, h := range hits {
			if h.Pos > tail {
				continue
			}
			k := sort.SearchInts(offsets, h.Pos+1) - 1 // bisect_right(offsets, pos) - 1
			out[unit[k].line] = append(out[unit[k].line], Hit{h.Pos - offsets[k], h.Chunk, h.Phrase})
		}
		return unit[:0]
	}
	for _, block := range a.Blocks {
		kind := block.Kind
		if kind != "paragraph" && kind != "list" {
			continue
		}
		var unit []pauseUnitRow
		for _, row := range block.Lines {
			if row.QuoteDepth != 0 || (row.Kind != "paragraph" && row.Kind != "list") {
				unit = flush(unit, kind)
				continue
			}
			plain := strings.ReplaceAll(strings.ReplaceAll(pystr.Strip(visible(row.Line)), "__", ""), "*", "")
			if plain == "" {
				unit = flush(unit, kind)
				continue
			}
			// 新しい項目と、短く名詞で終わる見出しのような行 (「■影響」) のあとでは、つながない。
			if len(unit) > 0 && (pauseMatch(pauseItemStart, row.Raw) || pauseMatch(pauseLabelLine, unit[len(unit)-1].plain)) {
				unit = flush(unit, kind)
			}
			unit = append(unit, pauseUnitRow{row.Line, plain})
		}
		flush(unit, kind)
	}
	return out
}

// TEAttrObject は読点のあとの「〜て」+連体の形の述語+名詞+「を」のパターン (本家の _PAUSE_TE_ATTR_OBJECT)。G17 (slide_comma) が本家と同じく match で使う。
func TEAttrObject() *pyre.Pattern { return teAttrObject() }
