package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 I: 選ぶ範囲 (どれか一つ ↔ 全部) と、関連から因果への言い方の変化。「AまたはBのどちらか」と「AとBの両方」は入れ替わると文の真偽が逆になり、
// 「関連がある」を「因果関係がある」に変えると、関わりを述べた文が原因と結果を述べる文に強まる。許可・能力 (群 H) と同じく、
// 文の種類の印とは別の表に置き、群 F と同じ範囲 (typeRanges) で比べる。
// 因果は、範囲のすぐあとに足した文 (原文のどの文にも対応しない文。同じ行の 2 文まで) も後の範囲に含める (原因を 2 文目に書いた書き換え)。
// 選ぶ範囲は含めない (足した文の「どちらも無料です」のような全部の言い方と分けられないため)。

// logicKinds は本家の LOGIC_MARKERS の順。
var logicKinds = []typeKind{
	{name: "どれか一つ", literals: []string{"または", "もしくは", "あるいは", "どちらか", "いずれか", "どれか", "片方", "択一", "か"},
		pattern: pyre.NewLazy(`または|もしくは|あるいは|どちらか(?!ら|と(?:いえば|言えば|いうと|言うと))|いずれか(?!ら)|どれか(?!ら)|片方(?:だけ|のみ)|択一|か(?<=[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ]か)(?<![何誰数幾]か)(?<![何数幾][一-龥]か)(?<![0-9０-９一二三四五六七八九十百千]か)(?!所|箇所|ヵ所|月|国|条|年|日|否|確|判|検|調|評|決|点|把|見極|吟味|試|迷|問|尋|分か|明|心配|不安|気)(?=[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[をがでにのはもと、。]|か|$))`, pyre.MULTILINE)},
	{name: "どれでも", literals: []string{"でも", "も"},
		pattern: pyre.NewLazy(`(?:どちら|いずれ|どれ)(?:から|で|に|を)?でも|(?:(?<=[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９])|(?<=[一-龥][しりきみちえけせてめれ]))(?:で|から)も、?[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?(?:の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?){0,2}(?:で|から)も`, pyre.MULTILINE)},
	{name: "どれか一つ（連体）", literals: []string{"かの", "一つの", "ひとつの", "1つの", "１つの", "一方の"},
		pattern: pyre.NewLazy(`(?:いずれ|どれ|どちら)か(?:一つ|ひとつ|1つ|１つ|一方)?の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,10}`, pyre.MULTILINE)},
	{name: "全部", literals: []string{"両方", "双方", "どちらも", "いずれも", "どれも", "両者とも", "つとも", "どちらとも", "すべて", "全て", "全部", "ともに", "共に", "も"},
		pattern: pyre.NewLazy(`両方(?!向)|双方(?!向)|どちらも|いずれも|どれも|両者とも|[二三2-3２-３]つとも|どちらとも(?!言|い|取|と|つ|決|判|限|不|評)|(?:どちら|いずれ|どれ)(?:に|へ)も|(?<=の)(?:すべて|全て|全部)(?!の)|(?:すべて|全て|全部)(?=[をがも]|に(?!おい|わた|渡))|ともに(?<=[はがも]ともに)|共に(?<=[はがも]共に)|(?:どちら|いずれ|どれ)の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,10}も(?![のう])|(?:(?<=[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９])|(?<=[一-龥][しりきみちえけせてめれ]))(?:に|と|へ)?も、?[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?(?:の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?){0,2}(?:に|と|へ)?も(?![のう])`, pyre.MULTILINE)},
	{name: "全部（連体）", literals: []string{"すべての", "全ての", "全部の", "全"},
		pattern: pyre.NewLazy(`(?:すべて|全て|全部)の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,10}|(?<![一-龥々])全(?!部)[一-龥々ァ-ヴー]{1,8}`, pyre.MULTILINE)},
	{name: "関連", literals: []string{"相関", "関連", "関係", "結び", "関わり", "かかわり"},
		pattern: pyre.NewLazy(`相関(?=(?:関係|性)?(?:が|は|も)?(?:ある(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|あり(?!ませ|えな)|あった(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|(?:見られ|みられ|認められ|指摘され|示され|確認され)(?!な|ず|ませ)|(?:強い|深い|高い)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは))|して(?:(?:いる|いた)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|います|おり|いました))|関連(?:性)?(?:が|は|も)?(?:ある(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|あり(?!ませ|えな)|あった(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|(?:見られ|みられ|認められ|指摘され|示され|確認され)(?!な|ず|ませ)|(?:強い|深い|高い)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは))|関連して(?:(?:いる|いた)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|います|おり|いました)|関係(?:性)?(?:が|は|も)?(?:ある(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|あり(?!ませ|えな)|あった(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|(?:見られ|みられ|認められ|指摘され|示され|確認され)(?!な|ず|ませ)|(?:強い|深い|高い)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは))|関係して(?:(?:いる|いた)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|います|おり|いました)|(?<=[とは])(?<!には)関(?:連|係)(?:する|します)(?=[。！!]|$|と(?:され|考えられ|いわれ|言われ|みられ|見られ))|結び(?<!に結び)(?:つ|付)いて(?:(?:いる|いた)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|います|おり|いました)|(?:関わり|かかわり)(?:が|は|も)?(?:ある(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|あり(?!ませ)|あった(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは)|(?:深い|強い)(?![一-龥々ァ-ヴー]|か(?!も)|とは(?:言|い)え|とは限ら|わけでは))`, pyre.MULTILINE)},
	{name: "因果", literals: []string{"因果", "原因", "起因", "要因", "起こ", "もたら", "誘発", "生じ", "起き", "発生", "増", "減", "伸び", "向上", "改善", "低下", "上昇", "悪化", "せい", "おかげ", "影響を", "つなが", "招", "結果"},
		pattern: pyre.NewLazy(`因果|原因|起因|要因|引き起こ[すしさせそ]|ひき起こ[すしさせそ]|もたら[すしさ]|誘発|(?:によって|により|ことで)[^。、]{0,12}?(?:生じ|起こ|起き|発生|増え(?:る|た|て)|増加(?:し|す)|減(?:る|った|って|り)|減少(?:し|す)|伸び(?:る|た|て)|向上(?:し|す)|改善(?:し|す)|低下(?:し|す)|上昇(?:し|す)|悪化(?:し|す))|せい(?<=のせい|たせい|るせい|いせい)(?:で|だ|です|である)|おかげ(?:で|だ|です|である)|影響を(?:与え|及ぼ)|(?<=[一-龥々ァ-ヴー])(?<!サーバー)(?<!ネット)(?<!ネットワーク)(?<!回線)(?<!電源)(?<!環境)(?<!端末)(?<!ホスト)(?<!ポート)(?<!ノード)(?<!機器)(?<!拠点)(?<!外部)(?<!社内)(?<!電話)(?<!窓口)(?<!担当)につなが(?:る|り|った|ります|りました)|(?<![者師客様人員長氏家生方])を招(?:く|き|いた)|(?<![査析験討認議論測定計証審票断索選較トそこあ])の結果、`, pyre.MULTILINE)},
	{name: "因果（広い）", literals: []string{"によ", "影響", "で", "から", "ため", "理由", "せい", "おかげ", "結果", "つなが", "招", "由来", "きっかけ", "引き金", "ので", "だから", "ゆえ", "故に"},
		pattern: pyre.NewLazy(`によ(?:る|り|って)|影響|で(?:起き|起こ|生じ|発生)|から(?:生じ|起き|起こ|来)|ため|理由|せい|おかげ|結果|につなが|招|由来|きっかけ|引き金|(?<!も)ので(?![はす])|だから|ゆえ|故に|で[^。、]{0,8}?(?:増え|減っ|減り|増加|減少|伸び|落ち|下が|上が)`, pyre.MULTILINE)},
}

// logicChangeKinds は変化の向き (subkind, kind, 元の種類, 後の種類)。
type logicChangeKind struct{ subkind, kind, source, target string }

var logicChangeKinds = []logicChangeKind{
	{"one_to_all", "choice", "どれか一つ", "全部"}, {"all_to_one", "choice", "全部", "どれか一つ"}, {"relation_to_cause", "causality", "関連", "因果"},
}

const choiceNoun = `[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]`

var (
	// 問いの「か」: 「設定が有効か管理者に確認する」「各端末が最新か担当者に聞く」。「か」の左が「が＋語」か状態を表す語で、
	// 右の語のあとに確かめる・聞く言い方が続くもの。「木曜か金曜に確認する」「上長か人事部に確認する」は選ぶ「か」として残す。
	questionKaLeft  = pyre.NewLazy(`(?:が`+choiceNoun+`{1,15}|有効|無効|正常|異常|最新`+"\u7248"+`?|必要|不要|可能|不可|適切|妥当|正確|完了|対象|該当|安全|十分|問題|適用|OK|NG)$`, 0)
	questionKaRight = pyre.NewLazy(`[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?(?:の[一-龥々ァ-ヴーA-Za-zＡ-Ｚａ-ｚ0-9０-９]{1,15}(?:[しりきみちえけせてめれ](?<=[一-龥][しりきみちえけせてめれ])(?=[もの]))?){0,2}(?:に|へ|で|が|から|まで)?(?:確認|問い合わせ|問合せ|聞|尋|相談|確か|照会|伺|質問|判断|判定|調べ|調査|検証|点検|チェック|見極|報告|連絡|回答)`, 0)
	// 「AもBも」の印を、人が読む行で「AもBも」と出すため、最初の項を左に広げる字 (名詞の字と、名詞をつなぐ「の」、送りがな)
	choiceNounChar = pyre.NewLazy(choiceNoun, 0)
	choiceHead     = pyre.NewLazy(`(?:に|と|へ|で|から)?も`, 0)
)

func isChoiceNounChar(c rune) bool {
	m, err := choiceNounChar.Get().Match(pyre.NewText(string(c)), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// choiceItemStart は「AもBも」の印の開始位置 start (最初の「も」か、その前の助詞) から、最初の項 A の頭まで戻った位置。
func choiceItemStart(text []rune, start int) int {
	i, floor := start, max(0, start-30)
	for i > floor {
		c := text[i-1]
		switch {
		case isChoiceNounChar(c):
			i--
		case c == 'の' && i < start && i-2 >= 0 && isChoiceNounChar(text[i-2]):
			i--
		case strings.ContainsRune("しりきみちえけせてめれ", c) && i == start && i-2 >= 0 && '一' <= text[i-2] && text[i-2] <= '龥':
			i--
		default:
			return i
		}
	}
	return i
}

// logicPositions は文章全体に選ぶ範囲と因果の印の正規表現を 1 回ずつ当て、位置の順に返す。
// 問いの「か」は除き、「AもBも」「AでもBでも」の印は最初の項から書く (開始位置は変えない)。
func logicPositions(text string) []typeMark {
	t := pyre.NewText(text)
	runes := pyunicode.Runes(text)
	var found []typeMark
	for _, k := range logicKinds {
		if !containsAny(text, k.literals) {
			continue
		}
		for _, m := range mustFinditer(k.pattern, t, 0, pyre.NoEnd) {
			start := m.Start(0)
			hit, _ := m.Group(0)
			if k.name == "どれか一つ" && hit == "か" {
				if mustSearchAt(questionKaLeft, t, max(0, start-16), start) != nil && matchAt(questionKaRight, t, start+1) {
					continue
				}
			} else if (k.name == "全部" || k.name == "どれでも") && matchAt(choiceHead, pyre.NewText(hit), 0) && start > 0 && !strings.HasPrefix(hit, "とも") {
				if head := choiceItemStart(runes, start); head < start {
					hit = pyunicode.FromRunes(runes[head:start]) + hit
				}
			}
			found = append(found, typeMark{start, k.name, hit})
		}
	}
	return sortMarks(found)
}

func matchAt(l *pyre.Lazy, t pyre.Text, pos int) bool {
	m, err := l.Get().Match(t, pos, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m != nil
}

// logicHits は範囲の中から始まる印を種類ごとにまとめる (同じ印も数の分だけ並べる)。
func logicHits(positions []typeMark, spans [][2]int, keys []int) map[string][]string {
	marks := map[string][]string{}
	for _, span := range spans {
		lo, hi := sort.SearchInts(keys, span[0]), sort.SearchInts(keys, span[1])
		for i := lo; i < hi; i++ {
			marks[positions[i].kind] = append(marks[positions[i].kind], positions[i].hit)
		}
	}
	return marks
}

// logicTails は各範囲のすぐあとに足した文 (書き直した文章のどの範囲にも重ならない文。同じ行の 2 文まで) の (開始, 終了) の並び。
func logicTails(r []rune, ranges []*rangeItem) [][][2]int {
	spans := sentenceSpans(pyunicode.FromRunes(r))
	starts := make([]int, len(spans))
	for i, s := range spans {
		starts[i] = s[0]
	}
	owned := make([]bool, len(spans))
	for _, item := range ranges {
		for k := max(0, sort.SearchInts(starts, item.a+1)-1); k < len(spans) && spans[k][0] < item.b; k++ {
			if spans[k][1] > item.a {
				owned[k] = true
			}
		}
	}
	tails := make([][][2]int, len(ranges))
	for n, item := range ranges {
		k, end := sort.SearchInts(starts, item.b), item.b
		for k < len(spans) && len(tails[n]) < 2 && !owned[k] && !slices.Contains(pySlice(r, end, spans[k][0]), '\n') {
			tails[n] = append(tails[n], spans[k])
			end = spans[k][1]
			k++
		}
	}
	return tails
}

// adnominalNoun は「いずれかの条件」「すべての条件」「全項目」の名詞 (条件、項目)。
func adnominalNoun(hit string) string {
	if i := strings.Index(hit, "の"); i >= 0 {
		return hit[i+len("の"):]
	}
	return string([]rune(hit)[1:])
}

func unique(hits []string) []string {
	var out []string
	for _, h := range hits {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// LogicChange は選ぶ範囲・因果の言い方が変わった範囲 1 つ。Kind は choice か causality、Subkind は one_to_all・all_to_one・relation_to_cause。
// OldHits は元の範囲の From の印、NewHits は後の範囲で増えた To の印。
type LogicChange struct {
	Orig, Rewrite, Kind, Subkind, From, To string
	OldHits, NewHits                       []string
}

func stringSet(hits []string, noun bool) map[string]bool {
	out := map[string]bool{}
	for _, h := range hits {
		if noun {
			h = adnominalNoun(h)
		}
		out[h] = true
	}
	return out
}

// LogicChanges は本家の _logic_changes: 選ぶ範囲 (どれか一つ ↔ 全部) と、関連から因果への言い方が変わった範囲。
// o と r は正規化した元と後の文章。範囲は文の種類の印と同じで、cache に作った範囲があれば使い回す。因果の後の範囲には、そのすぐあとに足した文も含める。
func LogicChanges(o, r string, cache *typeCache) []LogicChange {
	if o == r {
		return nil
	}
	oPos, rPos := logicPositions(o), logicPositions(r)
	oKinds, rKinds := map[string]bool{}, map[string]bool{}
	for _, m := range oPos {
		oKinds[m.kind] = true
	}
	for _, m := range rPos {
		rKinds[m.kind] = true
	}
	if oKinds["全部（連体）"] {
		oKinds["全部"] = true
	}
	if rKinds["全部（連体）"] {
		rKinds["全部"] = true
	}
	anyChange := false
	for _, c := range logicChangeKinds {
		anyChange = anyChange || (oKinds[c.source] && rKinds[c.target])
	}
	if !anyChange {
		return nil
	}
	ranges, _, _ := cache.shared(o, r)
	oKeys, rKeys := markStarts(oPos), markStarts(rPos)
	rRunes := pyunicode.Runes(r)
	// 因果を比べるときに使う、範囲のすぐあとに足した文 (関連と因果の印がそろう文章でだけ作る)
	tails := make([][][2]int, len(ranges))
	if oKinds["関連"] && rKinds["因果"] {
		tails = logicTails(rRunes, ranges)
	}
	var out []LogicChange
	for n, item := range ranges {
		extra := tails[n]
		before := logicHits(oPos, item.spans, oKeys)
		after := logicHits(rPos, [][2]int{{item.a, item.b}}, rKeys)
		// 名詞を修飾する「すべての条件」は、もう一方の側に同じ名詞の「いずれかの条件」があり、同じ側に同じ名詞の全部の言い方
		// (すべての条件、全条件) がないときだけ、全部の言い方として数える (印は「すべて」「全条件」と書く)
		nouns := map[string]map[string]bool{"o": stringSet(before["どれか一つ（連体）"], true), "r": stringSet(after["どれか一つ（連体）"], true)}
		allNouns := map[string]map[string]bool{"o": stringSet(before["全部（連体）"], true), "r": stringSet(after["全部（連体）"], true)}
		promote := func(hits []string, other string) []string {
			var out []string
			for _, hit := range hits {
				if nouns[other][adnominalNoun(hit)] && !allNouns[other][adnominalNoun(hit)] {
					if i := strings.Index(hit, "の"); i >= 0 {
						out = append(out, hit[:i])
					} else {
						out = append(out, hit)
					}
				}
			}
			return out
		}
		alls := map[string][]string{
			"o": append(append([]string{}, before["全部"]...), promote(before["全部（連体）"], "r")...),
			"r": append(append([]string{}, after["全部"]...), promote(after["全部（連体）"], "o")...),
		}
		for _, c := range logicChangeKinds {
			end := item.b
			var old, newHits []string
			switch c.subkind {
			case "one_to_all":
				// どれか一つの言い方が後の範囲から消え (どれでもよい言い方も残らず)、全部の言い方が元より増えたとき。
				// 変わらずに残る「すべてを保存し」のような全部の言い方は、数で打ち消し合う
				old = before[c.source]
				if len(old) == 0 || len(alls["r"]) <= len(alls["o"]) || len(after[c.source]) > 0 || len(after["どれでも"]) > 0 {
					continue
				}
				for _, hit := range unique(alls["r"]) {
					if !slices.Contains(alls["o"], hit) {
						newHits = append(newHits, hit)
					}
				}
				if len(newHits) == 0 {
					newHits = unique(alls["r"])
				}
			case "all_to_one":
				// 全部の言い方が後の範囲から消え、元の範囲になかったどれか一つの言い方が現れたとき (元の範囲にどれか一つの言い方が
				// あれば、「CかD → CかDのいずれか」のような言い換えと分けられないので出さない)
				old = alls["o"]
				if len(old) == 0 || len(after[c.target]) == 0 || len(before[c.target]) > 0 || len(alls["r"]) > 0 {
					continue
				}
				newHits = unique(after[c.target])
			default:
				// 因果は、範囲のすぐあとに足した文も後の範囲に含める
				var tail map[string][]string
				if len(extra) > 0 {
					tail = logicHits(rPos, extra, rKeys)
				}
				found := append(append([]string{}, after[c.target]...), tail[c.target]...)
				old = before[c.source]
				if len(old) == 0 || len(found) == 0 || len(before[c.target]) > 0 || len(before["因果（広い）"]) > 0 {
					continue
				}
				newHits = unique(found)
				if len(after[c.target]) == 0 {
					end = extra[len(extra)-1][1]
				}
			}
			out = append(out, LogicChange{strings.Join(item.texts, ""), rangeSentences(rRunes, item.a, end), c.kind, c.subkind, c.source, c.target, unique(old), newHits})
		}
	}
	return out
}
