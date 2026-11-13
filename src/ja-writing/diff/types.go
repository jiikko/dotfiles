package diff

import (
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
)

// 群 F: 文の種類の印。文の種類 (伝聞・推量・意見・例え・依頼・決まり) を表す語を、書き直す前後の対応する範囲 (types_ranges.go) で比べる。
// 正規表現は組んだ後の文字列のまま写した (手で写して書き間違えないため)。本家の値と同じかは TestBaseMatchesPythonRecording が定数の記録と比べる。

// typeEnd は、語が文の終わりか区切りの前にあること (本家の _END)。
const typeEnd = `(?=[。、！!？?よねがけのかし]|です|$)`

// typeKind は文の種類 1 つと、その印の正規表現。
// 印の正規表現は語の文字列を先頭に置き、直前の文字を確かめる後読みはその後ろに書く (当たる見込みのない位置を飛ばせるので速い)。
// literals は、その種類の正規表現が当たるために必ず含まれる文字列。どれも含まない文章には当てない。
// 後読みの長い選択肢を文章全体に当てるのは、同じ段落を繰り返す文書で遅いため。
type typeKind struct {
	name     string
	literals []string
	pattern  *pyre.Lazy
}

// typeKinds は本家の TYPE_MARKERS の順 (出力の順もこの順)。
var typeKinds = []typeKind{
	{name: "伝聞", literals: []string{"そう", "とされ", "という", "といいます", "とのこと", "によると", "によれば", "われ", "と聞"},
		pattern: pyre.NewLazy(`そう(?:(?<=[るうくすつぬぶむぐ]そう)|(?<=だそう)|(?<=ったそう)|(?<=んだそう)|(?<=ないそう))(?:(?:だ|です|でした|だった|で(?=、))(?=[。、！!よね]|$)|な(?=の(?:で|に|だ|です)))|とされ(?<!落とされ)(?<!必要とされ)(?:る|ている|ています|ており|た|ます|ました|てきた)|という(?=[。！!]|が、|$)|といいます(?<=[るうくすつぬぶむぐただ]といいます)(?=[。！!]|$)|とのこと(?<!あとのこと)(?<!後とのこと)|によ(?<!場合によ)(?:ると|れば)|と(?:言|い)われ(?:る|ている|ています|ており|た|ます|ました|てきた)|と聞(?<!かと聞)(?:いた|いて(?:いる|います|おり)|きました)`, pyre.MULTILINE)},
	{name: "推量", literals: []string{"らし", "よう", "みたい", "かも", "可能性", "だろう", "であろう", "でしょう", "はず", "られ", "われ"},
		pattern: pyre.NewLazy(`らし(?<=[るうくすつぬぶむぐいた]らし)(?:い|かった|く(?=、))`+typeEnd+`|らし(?<!素晴らし)(?<!すばらし)(?<!めずらし)(?<!わざとらし)(?<!可愛らし)(?<!かわいらし)(?<!憎らし)(?<!馬鹿らし)(?<!ばからし)(?<!男らし)(?<!女らし)(?<!子供らし)(?<!子どもらし)(?<!いやらし)(?<!汚らし)(?<!春らし)(?<!夏らし)(?<!秋らし)(?<!冬らし)(?<!彼らし)(?:い(?=[。！!]|です|でした|$|(?<!人間らしい)(?<!自分らしい)(?<!大人らしい)(?<!プロらしい)(?<!学生らしい)(?:が(?!ん)|けど|けれど))|かった(?=[。！!]|です|でした|$|が(?!ん)|けど|けれど))|よう(?<!次のよう)(?<!以下のよう)(?<!下記のよう)(?<!上記のよう)(?<!左記のよう)(?<!このよう)(?<!そのよう)(?<!あのよう)(?<!どのよう)(?:(?:だ|です|でした|だった|である|であった|で(?=、))`+typeEnd+`|な(?=の(?:で|に|だ|です)))|みたい(?:(?:だ|です|でした|だった)(?=[。！!よね]|$)|な(?=の(?:で|に|だ|です)))|かも(?:しれ|知れ)(?:ない|ません|ず)|可能性があ(?:る|ります|った|りました)|だろう(?<!でだろう)(?!か)|であろう|でしょう(?!か)|ではない(?:でしょう|だろう)か|はず(?:だ|です|で|でした|だった)|と(?:見|み)られ(?:る|ている|ています|ており|た|ます)|と(?:思|おも)われ(?:る|ます|た)|と考えられ(?:る|ます|た)`, pyre.MULTILINE)},
	{name: "意見", literals: []string{"と思", "とおも", "と考え", "と感じ", "は思", "はおも", "は考え", "は感じ", "ように思", "ように感じ"},
		pattern: pyre.NewLazy(`と(?:(?:私|僕|筆者|我々|われわれ|当社|弊社|私たち)は)?(?:思|おも)(?:う|い(?:ます|ました|(?=、))|った|って(?:いる|います|いた|いました|おり))|と(?:(?:私|僕|筆者|我々|われわれ|当社|弊社|私たち)は)?(?:考え|感じ)(?:る(?!と)|ます|ました|た|て(?:いる|います|いた|いました|おり)|(?<![0-9０-９][件人円個回倍割%％]と考え)(?<![0-9０-９][件人円個回倍割%％]と感じ)(?=、))|ように(?:思|感じ)(?:う|います|いました|られ)`, pyre.MULTILINE)},
	{name: "例え", literals: []string{"まるで", "あたかも", "さながら", "ごと", "如"},
		pattern: pyre.NewLazy(`まるで|あたかも|さながら|(?:ごと|如)(?<!前述のごと)(?<!上記のごと)(?<!周知のごと)(?<!以下のごと)(?<!次のごと)(?<!前述の如)(?<!上記の如)(?<!周知の如)(?<!以下の如)(?<!次の如)[くき]`, pyre.MULTILINE)},
	{name: "依頼・決まり", literals: []string{"ください", "下さい", "お願い", "いただけ", "もらえ", "ほしい", "もらいた", "いただきた", "べき", "なければ", "なくては", "ないといけ", "はいけ", "はなら", "ねばなら", "必要"},
		pattern: pyre.NewLazy(`(?:て|で)(?:ください|下さい)|を(?:ください|下さい)|(?:ご|お)[一-龥ぁ-んァ-ヴー]{1,8}(?:ください|下さい)|お願い(?<!よろしくお願い)(?<!宜しくお願い)(?:します|いたします|致します|申し上げます|できますか|できますでしょうか)|て(?:いただけ|もらえ)(?:ますか|ませんか)|てほしい|てもらいたい|ていただきたい|べき(?<!しかるべき)(?<!然るべき)(?<!驚くべき)(?<!恐るべき)(?<!来るべき)(?<!特筆すべき)(?<!注目すべき)(?!乗)|なければ(?:ならない|なりません|ならず|いけない|いけません)|なくては(?:ならない|なりません|ならず|いけない|いけません)|ないといけ(?:ない|ません)|(?:て|で)は(?:いけ|なら)(?:ない|ません)|ねばなら(?:ない|ず)|必要(?<!不必要)(?:があ(?:る|ります|り(?=、))|(?:です|だ)(?=[。！!]|$)|[にと]な(?:る(?!まで)|ります|り(?=、)|った(?!ら(?!し))|りました))`, pyre.MULTILINE)},
}

// typeKindNames は typeKinds の名前の並び (marksIn の kinds に渡す。指定しないときの並び)。
var typeKindNames = func() []string {
	names := make([]string, len(typeKinds))
	for i, k := range typeKinds {
		names[i] = k.name
	}
	return names
}()

var (
	// 「まるで〜のようだ」の「よう」「みたい」は、推量ではなく例えを表す語として数える。
	// 対にするのは、同じ文の中で「まるで」「あたかも」「さながら」のあとに最初に出てくる 1 つだけ。
	simileHeads = []string{"まるで", "あたかも", "さながら"}
	simileLike  = pyre.NewLazy(`(?:よう|みたい)(?:であった|である|だった|でした|です|だ|な|に|で)?`, 0)
	stopsChars  = "。！？!?\n"
	stopPattern = pyre.NewLazy("["+stopsChars+"]", 0)
	// 文の中身の文字数を数えるときに除く文字 (句読点・かっこ・空白)
	notBody       = map[rune]bool{}
	sentencePiece = pyre.NewLazy(`[^。！？!?\n]+[。！？!?]*[」』）)]*`, 0)
)

func init() {
	for _, c := range "。、，！？!?「」『』（）() \n\u3000" {
		notBody[c] = true
	}
}

// typeMark は文章の中の印 1 つ (開始位置は code point、種類、印の文字列)。
type typeMark struct {
	start int
	kind  string
	hit   string
}

func mustFinditer(l *pyre.Lazy, t pyre.Text, pos, end int) []*pyre.Match {
	ms, err := l.Get().Finditer(t, pos, end)
	if err != nil {
		panic(err)
	}
	return ms
}

func mustSearchAt(l *pyre.Lazy, t pyre.Text, pos, end int) *pyre.Match {
	m, err := l.Get().Search(t, pos, end)
	if err != nil {
		panic(err)
	}
	return m
}

func containsAny(text string, literals []string) bool {
	for _, l := range literals {
		if strings.Contains(text, l) {
			return true
		}
	}
	return false
}

// markPositions は文章全体に印の正規表現を 1 回ずつ当て、位置の順 (同じ位置は種類の順) の印を返す。
func markPositions(text string) []typeMark {
	t := pyre.NewText(text)
	var found []typeMark
	for _, k := range typeKinds {
		if !containsAny(text, k.literals) {
			continue
		}
		for _, m := range mustFinditer(k.pattern, t, 0, pyre.NoEnd) {
			hit, _ := m.Group(0)
			found = append(found, typeMark{m.Start(0), k.name, hit})
		}
	}
	type head struct {
		start int
		hit   string
	}
	var heads []head
	for _, m := range found {
		if m.kind == "例え" && slices.Contains(simileHeads, m.hit) {
			heads = append(heads, head{m.start, m.hit})
		}
	}
	if len(heads) > 0 {
		guesses := map[int]int{}
		for i, m := range found {
			if m.kind == "推量" {
				guesses[m.start] = i
			}
		}
		paired := map[int]bool{}
		for _, h := range heads {
			end := t.Len()
			if stop := mustSearchAt(stopPattern, t, h.start, pyre.NoEnd); stop != nil {
				end = stop.Start(0)
			}
			like := mustSearchAt(simileLike, t, h.start+pystr.Len(h.hit), end)
			if like == nil || paired[like.Start(0)] {
				continue
			}
			paired[like.Start(0)] = true
			// 推量として拾った「ようだ」なら、拾った語のまま種類を例えに替える。意見の「ように思う」などはそのまま残し、例えを表す語を別に足す。
			if i, ok := guesses[like.Start(0)]; ok {
				found[i] = typeMark{like.Start(0), "例え", found[i].hit}
			} else {
				hit, _ := like.Group(0)
				found = append(found, typeMark{like.Start(0), "例え", hit})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })
	return found
}

// KindHits は種類と、その範囲にあった印 (同じ印は 1 回だけ)。
type KindHits struct {
	Kind string
	Hits []string
}

// kindMarks は範囲の中にあった印の種類ごとの一覧 (kinds の順)。
type kindMarks []KindHits

func (k kindMarks) has(kind string) bool {
	return slices.ContainsFunc(k, func(e KindHits) bool { return e.Kind == kind })
}

// markStarts は印の開始位置の並び (範囲ごとに作り直さないよう、呼ぶ側が 1 回だけ作って渡す)。
func markStarts(marks []typeMark) []int {
	out := make([]int, len(marks))
	for i, m := range marks {
		out[i] = m.start
	}
	return out
}

// marksIn は範囲 (開始, 終了) の中から始まる印を種類ごとにまとめる。並びは kinds の順。keys は positions の開始位置の並び (nil なら作る)。
func marksIn(positions []typeMark, spans [][2]int, kinds []string, keys []int) kindMarks {
	if keys == nil {
		keys = markStarts(positions)
	}
	byKind := map[string][]string{}
	for _, span := range spans {
		lo, hi := sort.SearchInts(keys, span[0]), sort.SearchInts(keys, span[1])
		for i := lo; i < hi; i++ {
			m := positions[i]
			if !slices.Contains(byKind[m.kind], m.hit) {
				byKind[m.kind] = append(byKind[m.kind], m.hit)
			}
		}
	}
	var out kindMarks
	for _, kind := range kinds {
		if hits, ok := byKind[kind]; ok {
			out = append(out, KindHits{kind, hits})
		}
	}
	return out
}

// TypeMarks は本家の type_marks: 文や範囲に含まれる文の種類の印 (同じ印は 1 回だけ)。
func TypeMarks(sentence string) []KindHits {
	return marksIn(markPositions(sentence), [][2]int{{0, pystr.Len(sentence)}}, typeKindNames, nil)
}
