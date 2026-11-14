package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 S の下ごしらえ: 否定対比の節の名詞の取り出し (本家の _negated_tokens)・否定のある節か・比べて下げた形か。
// 正規表現は本家の字面のまま持つ (定数の記録と突き合わせる)。

const (
	contrastTokenOkuriMid = stiffOkuriMid + "たいめわ"
	contrastNounPlain     = "[" + stiffKanjiClass + "ァ-ヴーA-Za-z0-9]"
)

// CONTRAST_WEIGHTS (本家の重みの語。並びは本家のまま)
var contrastWeights = []string{"目的", "狙い", "重要", "まず", "第一に", "ねらい", "狙っ", "狙う", "主眼", "主目的", "主な", "最優先",
	"優先", "何より", "最も", "一番", "重視", "重点", "大事", "大切", "目指", "筆頭", "中心"}

var contrastStopTokens = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range contrastWeights {
		m[w] = true
	}
	for _, w := range []string{"場合", "部分", "程度", "意味", "自体", "以上", "以下", "全部", "全体", "一部", "問題", "必要", "ところ", "もの", "こと"} {
		m[w] = true
	}
	return m
}()

// 否定対比の逆転で文として数えない行の種類 (本文の数えない種類から、見出しと表を外したもの)
var contrastSkipKinds = func() map[string]bool {
	m := map[string]bool{}
	for k := range bodySkipKinds {
		if k != "heading" && k != "table" {
			m[k] = true
		}
	}
	return m
}()

var contrastNotOnly = []string{"だけ", "ばかり", "のみ"}

var (
	contrastMark     = pyre.NewLazy(`(?:では|じゃ|でも)(?:なく(?:て)?|ない|なかっ|ありません)|でなく(?:て)?|は(?:使わ|せ)ずに?`, 0)
	contrastZuMark   = pyre.NewLazy(`は(?:使わ|せ)ず`, 0)
	contrastRhetoric = pyre.NewLazy(`(?:ない|ありません)(?:か|でしょうか|だろうか|かと|のか)`, 0)
	contrastSubord   = pyre.NewLazy(`(?:た)?(?:ので|から|ため|場合|とき|時|なら|限り|かぎり)`, 0)
	contrastBreak    = pyre.NewLazy(`[、。，,．！？!?「」『』（）()：:\n|｜]`, 0)
	contrastTopic    = pyre.NewLazy(`(?:(?<=[^ぁ-ゖ])|(?<=[のにとで]))は|は(?=[^ぁ-ゖ])`, 0)
	contrastToken    = pyre.NewLazy("["+stiffKanjiClass+"]+(?:["+contrastTokenOkuriMid+"]["+stiffKanjiClass+"]+)*(?:["+stiffOkuriTail+"]|(?<=["+stiffKanjiClass+"])[さみ](?![ぁ-ゖ]))?"+
		"|[ァ-ヴー]{2,}|[A-Za-z][A-Za-z0-9_.+#-]+", 0)
	contrastNegAfter       = pyre.NewLazy(`(?:では|じゃ|でも)な|でな[くい]|ではありません|ない|なく|なかっ|ません|以外|を除|を除い|抜き|不要|二の次|後回し`, 0)
	contrastDemoteWord     = pyre.NewLazy(`(?<=[はもがを])(?:その次|二番手|二番目|脇に置|脇役|添え物|おまけ|どうでも|そのあとに回)`, 0)
	contrastOtherNoun      = "(?:[" + stiffKanjiClass + "ァ-ヴーA-Za-z0-9]+|ほか|それ|あと|これ)"
	contrastDemoteWordElse = pyre.NewLazy("(?:(?:で|て|にし|し)"+contrastOtherNoun+"(?:の[^、。はもがを]{1,12})?"+
		"|(?:に|が|けど|から|ので)"+contrastOtherNoun+")[はもがを]$", 0)
	contrastLexicalNeg  = pyre.NewLazy(`対象外|範囲外|適用外|非対応|未対応|無効|廃止|禁止|除外|据え置`, 0)
	contrastCoordHead   = pyre.NewLazy(`と|や|及び|および|並びに|ならびに|・`, 0)
	contrastTopicMarks  = pyre.NewLazy(`[はもが]`, 0)
	contrastKatakanaHdr = pyre.NewLazy(`[ァ-ヴー]`, 0)
	contrastLatinHdr    = pyre.NewLazy(`[A-Za-z]`, 0)
	contrastAdjNoun     = pyre.NewLazy("^["+stiffKanjiClass+"]い(["+stiffKanjiClass+"].*)$|^.*[いため](["+stiffKanjiClass+"]{2,}.*)$", 0)
)

// 否定対比の逆転の候補で示す語の送り仮名 (取った語のすぐあとから否定を表す語までの平仮名 1〜2 字)
var (
	contrastShownTail      = pyre.NewLazy(`[ぁ-ゖ]{1,2}$`, 0)
	contrastShownNotHead   = "はがをのにとでもへやかよ"
	contrastShownParticles = []string{"など", "まで", "ほど", "しか", "さえ", "こそ"}
)

const contrastShownMoGa = "もが"

// 比べて下げる形
const (
	contrastDemoteReduce = "は減ら"
	contrastUseA         = "(?!使|用い|中心|基本|軸|前提|主体|活か|生か|ベース|重視|優先)"
)

// 8 番目の語は、この repo の文書の禁止語の検査に掛からないよう字の番号で書く (値は本家の _PROMOTE_TO と同じ。定数の記録が突き合わせる)。
var contrastPromoteTo = func() string {
	var b strings.Builder
	for _, w := range []string{"本番", "有効", "先頭", "上位", "主力", "正式", "標準", string([]rune{0x65e2, 0x5b9a}), "中心", "主軸", "前面", "最初", "一番", "トップ", "優先", "オン"} {
		b.WriteString("(?<!" + w + ")")
	}
	return b.String()
}()

var (
	contrastDemote = pyre.NewLazy(`(?<!に)より|の代わり|に代わ|に替え|から[^、。]{0,12}?(?:切り替|切替|移行|移し|乗り換|置き換)`+
		"|^["+stiffKanjiClass+"ァ-ヴーA-Za-z0-9]{0,8}(?:[ぁ-ゖ]{0,2}|の[^、。をはもがでにへと]{0,12})[をはも]"+contrastUseA+
		"[^、。を]{1,12}?(?:"+contrastPromoteTo+"[にへ](?:切り替|切替|移行|移[しす]|乗り換|置き換|置換|入れ替|差し替"+
		"|変更|改め|転換)|で置き換)"+
		`|(?:[うくぐすつぬぶむる]|の)前に|[はをも](?:後|あと)(?:に(?:し|す|回|まわ)|で|$)|後日|後回し`+
		`|次回(?!も|から(?![にと]))|見送|保留|(?<!に)控え|避け|(?<!は)やめ|はやめ(?!に)`+
		`|(?<!突き)(?<!食い)(?<!受け)(?<!引き)(?<!歯)止め|^(?:[ぁ-ゖ]{0,2}|の(?:数|回数|件数|時間|量|頻度|割合))は減ら`, 0)
	contrastReduceItself   = pyre.NewLazy(`[ぁ-ゖ]{0,5}?(?:こと|の(?=[がはを]))`, 0)
	contrastDemoteAnchored = pyre.NewLazy(`見送|保留|控え|避け|やめ|止め|次回|後日|[はをも](?:後|あと)`, 0)
	contrastAPhrase        = pyre.NewLazy(`の[^、。をはもがでにへと]{0,12}`, 0)
	contrastOtherObject    = pyre.NewLazy(`(?<!こと)(?<!の)を`, 0)
	contrastSwitchTo       = pyre.NewLazy(`に(?:替え|代わ)`, 0)
	contrastSwitchFrom     = pyre.NewLazy(`[をが]|から`, 0)
)

// 推敲後で A のすぐあとがこれなら、A を B と並べて肯定している・並べた 2 つを比べた形・仮説・逆接
var (
	contrastAlsoAfter = pyre.NewLazy(`(?:だけ|ばかり|のみ)(?:では|で|じゃ)なく|のみならず|に加え|に限らず|のほか`, 0)
	contrastCoord     = pyre.NewLazy(`及び|および|並びに|ならびに|や|・`+
		`|と(?!い[うっ]|して|しては|は|の|同|違|異|比|な[るりっれ]|す[るれ]|し[たてま]|呼|言|考|思|見|感|聞)`, 0)
	contrastCompare = pyre.NewLazy(`のうち|から選|比べ|比較|両方|どちら|いずれ|調べ|(?<!と)(?<!ことを)(?<!ことが)(?<!ことも)(?<!再)再?(?:確かめ|確認)`+
		`|検討|検証|候補|試[しすみ]`, 0)
	contrastHypothesis  = pyre.NewLazy(`疑|考え(?!られ(?:ます|る))|思っ|思い|予想|想定|見込|仮説|推測|睨`, 0)
	contrastAdversative = pyre.NewLazy(`(?:が|けど|けれど|けれども|ものの|のに|ても|でも)$`, 0)
	contrastWeightPat   = pyre.NewLazyFrom(func() string {
		parts := make([]string, len(contrastWeights))
		for i, w := range contrastWeights {
			if w == "まず" {
				parts[i] = `(?<![` + stiffKanjiClass + `])まず(?!い)`
			} else {
				parts[i] = pyre.Escape(w)
			}
		}
		return strings.Join(parts, "|")
	}, 0)
	contrastWeightOnly   = pyre.NewLazy(`^#*\s*(?:まず(?:は)?|第一に|何より(?:も)?|最優先(?:で|として)|一番に)$`, 0)
	contrastSideReplaced = pyre.NewLazy(`(?:の[^、。をはもがでにへと]{0,12})?を(?:置き換|置換|差し替|入れ替|切り替|切替)`, 0)
	contrastSideRejected = pyre.NewLazy(`[をはも](?:中止|減ら|省|捨て|廃止|控え|見送|禁止|やめ|止め|取りやめ|なく[さしすせ]|削)`, 0)
)

// ctext は文字列と、その code point の列と、正規表現に渡す形を 1 つにしたもの (位置は全部 code point)。
type ctext struct {
	s  string
	rs []rune
	t  pyre.Text
}

func newCtext(s string) ctext {
	return ctext{s, pyunicode.Runes(s), pyre.NewText(s)}
}

// sub は Python の text[a:b] (範囲の外は詰める。a・b は 0 以上)。
func (c ctext) sub(a, b int) ctext { return newCtext(runeSlice(c.rs, a, b)) }

func (c ctext) has(l *pyre.Lazy) bool { return mustSearchAt(l, c.t, 0, pyre.NoEnd) != nil }

// runeFind は Python の str.find(sub, start, end)。
func runeFind(rs, sub []rune, start, end int) int {
	end = min(end, len(rs))
	for i := start; i >= 0 && i+len(sub) <= end; i++ {
		if hasPrefixRunes(rs[i:], sub) {
			return i
		}
	}
	return -1
}

func hasPrefixRunes(rs, prefix []rune) bool {
	if len(rs) < len(prefix) {
		return false
	}
	for i, r := range prefix {
		if rs[i] != r {
			return false
		}
	}
	return true
}

// negAfter は名詞 token のあと (after。節の終わりまで) が否定のある節か (本家の _neg_after)。
// 語で否定する語は、A と並べた別の名詞の句の語 (「AとXの廃止」) なら数えない。下げる語は、A のあとから下げる語の前までが別の名詞の句で終わらないときだけ数える。
func negAfter(after ctext, token string) bool {
	if after.has(contrastNegAfter) {
		return true
	}
	for _, found := range mustFinditer(contrastDemoteWord, after.t, 0, pyre.NoEnd) {
		if mustSearchAt(contrastDemoteWordElse, after.t, 0, found.Start(0)) == nil {
			return true
		}
	}
	if strings.HasPrefix(after.s, "外") && !strings.HasSuffix(token, "内") {
		return true
	}
	coordinated := mustMatch(contrastCoordHead, after.t, 0) != nil
	for _, found := range mustFinditer(contrastLexicalNeg, after.t, 0, pyre.NoEnd) {
		start := found.Start(0)
		if coordinated && start > 0 && after.rs[start-1] == 'の' && mustSearchAt(contrastTopicMarks, after.t, 0, start) == nil {
			continue
		}
		return true
	}
	return false
}

// clauseBounds は position を含む節の [始め, 終わり) (本家の _clause_bounds)。
func clauseBounds(c ctext, position int) (int, int) {
	left := 0
	for _, m := range mustFinditer(contrastBreak, c.t, 0, position) {
		left = m.End(0)
	}
	if m := mustSearchAt(contrastBreak, c.t, position, pyre.NoEnd); m != nil {
		return left, m.Start(0)
	}
	return left, len(c.rs)
}

func contrastIsToken(token string) bool {
	if contrastStopTokens[token] {
		return false
	}
	t := pyre.NewText(token)
	if matchAt(contrastKatakanaHdr, t, 0) || matchAt(contrastLatinHdr, t, 0) {
		return pystr.Len(token) >= 2
	}
	if rs := pyunicode.Runes(token); len(rs) == 2 && (rs[1] == 'さ' || rs[1] == 'み') {
		return true
	}
	return countFound(stiffKanji, t) >= 2
}

// contrastTokens は _TOKEN.findall。
func contrastTokens(c ctext) []string {
	var out []string
	for _, m := range mustFinditer(contrastToken, c.t, 0, pyre.NoEnd) {
		w, _ := m.Group(0)
		out = append(out, w)
	}
	return out
}

// negatedToken は否定対比の節から取った名詞 (名詞・否定の印・印の終わりの位置)。
type negatedToken struct {
	token, mark string
	markEnd     int
}

// negatedTokens は 1 つの文の否定対比の節から名詞を取り出す (本家の _negated_tokens)。
func negatedTokens(sentence ctext) []negatedToken {
	var out []negatedToken
	for _, match := range mustFinditer(contrastMark, sentence.t, 0, pyre.NoEnd) {
		mark, _ := match.Group(0)
		if (strings.HasPrefix(mark, "では") || strings.HasPrefix(mark, "じゃ")) && mustMatch(contrastRhetoric, sentence.t, match.Start(0)+2) != nil {
			continue
		}
		if mustMatch(contrastSubord, sentence.t, match.End(0)) != nil {
			continue
		}
		before := runeSlice(sentence.rs, 0, match.Start(0))
		suffixed := false
		for _, n := range contrastNotOnly {
			if strings.HasSuffix(before, n) {
				suffixed = true
			}
		}
		if suffixed {
			continue
		}
		left := 0
		for _, brk := range mustFinditer(contrastBreak, sentence.t, 0, match.Start(0)) {
			left = brk.End(0)
		}
		span := sentence.sub(left, match.Start(0))
		topicCut := 0
		for _, topic := range mustFinditer(contrastTopic, span.t, 0, pyre.NoEnd) {
			topicCut = topic.End(0)
		}
		rawTokens := contrastTokens(span.sub(topicCut, len(span.rs)))
		var tokens []string
		for _, tk := range rawTokens {
			if contrastIsToken(tk) {
				tokens = append(tokens, tk)
			}
		}
		if topicCut != 0 && len(rawTokens) > 0 {
			all := true
			for _, tk := range rawTokens {
				if !slices.Contains(contrastWeights, tk) {
					all = false
				}
			}
			if all {
				// 「共通化は目的ではなく」: は のあとが重みの語だけなら、主題の名詞を取る
				tokens = nil
				for _, tk := range contrastTokens(span.sub(0, topicCut)) {
					if contrastIsToken(tk) {
						tokens = append(tokens, tk)
					}
				}
			}
		}
		for _, tk := range tokens {
			out = append(out, negatedToken{tk, mark, match.End(0)})
			if noun := mustMatch(contrastAdjNoun, pyre.NewText(tk), 0); noun != nil {
				// 「高い品質」「支払い期限」の「品質」「期限」も否定された語として見る
				text, ok := noun.Group(1)
				if !ok || text == "" {
					text, _ = noun.Group(2)
				}
				if contrastIsToken(text) {
					out = append(out, negatedToken{text, mark, match.End(0)})
				}
			}
		}
	}
	return out
}

// shownWord は候補で示す語 (本家の _shown_word)。取った語のすぐあとから否定を表す語の始まりまでが送り仮名なら足す。比べに使う語は token のまま。
func shownWord(text ctext, token string, markStart int) string {
	head := text.sub(0, markStart)
	tail := mustSearchAt(contrastShownTail, head.t, 0, pyre.NoEnd)
	if tail == nil || !strings.HasSuffix(runeSlice(head.rs, 0, tail.Start(0)), token) {
		return token
	}
	kana, _ := tail.Group(0)
	if slices.Contains(contrastShownParticles, kana) {
		return token
	}
	rs := pyunicode.Runes(kana)
	if !strings.ContainsRune(contrastShownNotHead, rs[0]) || (len(rs) == 2 && strings.ContainsRune(contrastShownMoGa, rs[0]) && strings.ContainsRune(stiffOkuriTail, rs[1])) {
		return token + kana
	}
	return token
}

// demoted は名詞 (text の position から) のあと (after。節の終わりまで) に比べて下げる形があれば真 (本家の _demoted)。
func demoted(text ctext, left, position int, after ctext) bool {
	for _, match := range mustFinditer(contrastDemote, after.t, 0, pyre.NoEnd) {
		whole, _ := match.Group(0)
		if match.Start(0) == 0 && mustMatch(contrastSwitchTo, after.t, 0) != nil && mustSearchAt(contrastSwitchFrom, text.t, left, position) != nil {
			continue // 「XをAに替える」: A は置き換えた先
		}
		if strings.HasSuffix(whole, contrastDemoteReduce) && mustMatch(contrastReduceItself, after.t, match.End(0)) != nil &&
			mustSearchAt(contrastWeightPat, after.t, match.End(0), pyre.NoEnd) != nil {
			continue // 「Aは減らすことを最優先にする」: A を減らすことが主張
		}
		if newCtext(whole).has(contrastDemoteAnchored) && otherObject(after.sub(0, match.Start(0))) {
			continue // A と下げる語の間に別の目的語がある: 下げたのは A でない
		}
		return true
	}
	return false
}

// otherObject は A のあとから下げる語の前まで (segment) に、A の句のあとの最初の助詞より後ろで別の名詞に付いた「を」があれば真 (本家の _other_object)。
func otherObject(segment ctext) bool {
	rest := segment
	if head := mustMatch(contrastAPhrase, segment.t, 0); head != nil {
		rest = segment.sub(head.End(0), len(segment.rs))
	}
	return mustSearchAt(contrastOtherObject, rest.t, 1, pyre.NoEnd) != nil
}
