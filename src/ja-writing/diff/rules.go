package diff

import (
	"sort"
	"strconv"
	"strings"

	"ja-writing/internal/bold"
	"ja-writing/internal/difflib"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 G: 9 つのキーの組み立て (本家の _rule_candidates)。v1.1.2 の点検 (言い回しの種類の数の増減・元にない語と消えた語・構造・足した部分・文末・太字)。

// markerKind は言い回しの種類 (文末や言い回しの種類の数が増えた・減ったものを候補にする)。順は本家の MARKERS の順。
type markerKind struct {
	name    string
	pattern *pyre.Lazy
}

var markerKinds = []markerKind{
	{"依頼", pyre.NewLazy(`(?:て|で)ください`, 0)},
	{"勧誘", pyre.NewLazy(`ましょう`, 0)},
	{"義務", pyre.NewLazy(`なければ(?:なりません|ならない)|なくては(?:なりません|ならない)|ねばならない|必要があ(?:ります|る)|べき`, 0)},
	{"評価", pyre.NewLazy(`大切|重要|大事|不可欠|欠かせ|肝心|肝要`, 0)},
	{"可能", pyre.NewLazy(`でき(?:ます|る|ません|ない)|(?<![しさ])(?:ら|れ)(?:ます|ません)(?=[。、がけし]|$)|(?<=[作使書読言防守残伝送続進])(?:れ|え|け|め|せ)(?:ます|る)(?=[。、がけし]|$)`, 0)},
	{"推量", pyre.NewLazy(`でしょう|だろう|かもしれ|はず|と思(?:います|う)|ようです|らしい|おそれ|たいところ`, 0)},
	{"念押し", pyre.NewLazy(`のです|んです|こそ|まさに|必ず|絶対|常に`, 0)},
	{"意志", pyre.NewLazy(`(?:に|ように|ことに)し(?:ます|ている|ています)`, 0)},
	{"条件", pyre.NewLazy(`(?<!例)(?<!たと)(?:れ|え|け|せ|て|ね|め|べ)ば(?![かり])|なら(?=[、。]|$|\s)|たら(?=[、。]|$|\s)|場合`, 0)},
	{"説明化", pyre.NewLazy(`ことが挙げられ|ということ|ことです|ことになります`, 0)},
	{"つなぎ", pyre.NewLazy(`まず|また(?!は)|そして|さらに|次に|最後に|ただし|しかし|つまり|そのため|ので(?!す)|によって|ことで|ことにより`, 0)},
}

// contentWord は語の単位 (漢字 2 字以上・片仮名 2 字以上・英単語)。
var contentWord = pyre.NewLazy(`[一-龥々〆ヵヶ]{2,}|[ァ-ヴー]{2,}|[A-Za-z][A-Za-z0-9_.+#/-]+`, 0)

// MarkerChange は言い回しの種類 1 つの数の増減。
type MarkerChange struct {
	Kind                  string
	Orig, Rewrite         int
	OrigHits, RewriteHits []string
}

// SpanChange は足した部分 (文字単位の差分) のうち、種類か新しい語に当たるもの。
type SpanChange struct {
	Added, Was string
	Kinds      []string
	NewWords   []string
	Where      string
}

// EndingFlag は立場に合わない文末の見直しの知らせ (note と、その文)。
type EndingFlag struct {
	Note      string
	Sentences []string
}

// EndingsSection は endings の節。Stance は指定なしのとき空。
type EndingsSection struct {
	Stance    string
	Changes   []EndingChange
	Flags     []EndingFlag
	OrigFlags []EndingFlag
}

// RuleCandidates は _rule_candidates の結果。
type RuleCandidates struct {
	Markers     []MarkerChange
	NewWords    []string
	LostWords   []string
	Structure   []string
	Spans       []SpanChange
	Logic       []LogicPoint
	Bold        []bold.Problem
	Endings     EndingsSection
	TypeChanges []TypeChange
}

func findall(l *pyre.Lazy, text string) []string {
	var out []string
	for _, m := range mustFinditer(l, pyre.NewText(text), 0, pyre.NoEnd) {
		hit, _ := m.Group(0)
		out = append(out, hit)
	}
	return out
}

func endingFlags(flags []StanceFlag) []EndingFlag {
	out := make([]EndingFlag, len(flags))
	for i, f := range flags {
		out[i] = EndingFlag{Note: f.Note}
		for _, r := range f.Rows {
			out[i].Sentences = append(out[i].Sentences, r.Sentence)
		}
	}
	return out
}

// pyContext は本家の context: t の [i, j) を「［ ］」で囲み、前後 width 字を添える。
func pyContext(t []rune, i, j, width int) string {
	a, b := max(0, i-width), min(len(t), j+width)
	return pyunicode.FromRunes(pySlice(t, a, i)) + "［" + pyunicode.FromRunes(pySlice(t, i, j)) + "］" + pyunicode.FromRunes(pySlice(t, j, b))
}

// RuleCandidatesOf は本家の _rule_candidates(orig_raw, rw_raw, stance): 元と書き直した文章 (Markdown のまま) の 9 つの点検。stance は空なら指定なし。
// 範囲づくりの結果は cache に置く (許可・能力 (群 H) と選ぶ範囲・因果 (群 I) が同じ範囲を使い回す)。
func RuleCandidatesOf(origRaw, rwRaw, stance string, cache *typeCache) (*RuleCandidates, error) {
	o, r := Normalize(origRaw), Normalize(rwRaw)
	out := &RuleCandidates{Logic: LogicPoints(rwRaw)}
	var err error
	if out.Bold, err = bold.Problems(rwRaw, true); err != nil {
		return nil, err
	}
	rwUnits, flags := StanceFlags(rwRaw, stance, false)
	origUnits, origFlags := StanceFlags(origRaw, stance, false)
	out.Endings = EndingsSection{Stance: stance, Changes: endingChangesFromUnits(origUnits, rwUnits), Flags: endingFlags(flags), OrigFlags: endingFlags(origFlags)}
	oRunes, rRunes := pyunicode.Runes(o), pyunicode.Runes(r)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(oRunes, rRunes).GetOpcodes()
	}
	out.TypeChanges = TypeChanges(o, r, opcodes, cache)

	// 1. 種類ごとの数の増減
	for _, k := range markerKinds {
		hitsO, hitsR := findall(k.pattern, o), findall(k.pattern, r)
		if len(hitsO) != len(hitsR) {
			out.Markers = append(out.Markers, MarkerChange{k.name, len(hitsO), len(hitsR), hitsO, hitsR})
		}
	}
	// 2. 元にない語・消えた語 (語の単位で、文のどこかに出てくるかを見る)
	oWords, rWords := map[string]bool{}, map[string]bool{}
	for _, w := range findall(contentWord, o) {
		oWords[w] = true
	}
	for _, w := range findall(contentWord, r) {
		rWords[w] = true
	}
	// 「w not in o」と同じ。o から取った語は o にあるので探さず、ほかの語も、同じ語は o 全体から探し直さない
	absent := map[string]bool{}
	notInO := func(w string) bool {
		found, ok := absent[w]
		if !ok {
			found = !oWords[w] && !strings.Contains(o, w)
			absent[w] = found
		}
		return found
	}
	for w := range rWords {
		if notInO(w) {
			out.NewWords = append(out.NewWords, w)
		}
	}
	for w := range oWords {
		if !rWords[w] && !strings.Contains(r, w) {
			out.LostWords = append(out.LostWords, w)
		}
	}
	sort.Strings(out.NewWords)
	sort.Strings(out.LostWords)
	// 3. 構造
	if HasList(origRaw) && !HasList(rwRaw) {
		out.Structure = append(out.Structure, "箇条書きを地の文にした。各項目の文末（指示・説明・評価）が元と同じか見る")
	}
	if po, pr := len(Paragraphs(origRaw)), len(Paragraphs(rwRaw)); pr < po {
		out.Structure = append(out.Structure, "段落をまとめた（"+strconv.Itoa(po)+" → "+strconv.Itoa(pr)+"）。まとめた段落の話題が1つか見る")
	}
	if so, sr := len(Sentences(o)), len(Sentences(r)); sr != so {
		out.Structure = append(out.Structure, "文の数が変わった（"+strconv.Itoa(so)+" → "+strconv.Itoa(sr)+"）")
	}
	// 4. 足した部分 (文字単位の差分)。種類か新しい語に当たるものだけ残す
	if o != r {
		for _, op := range opcodes {
			if op.Tag != "insert" && op.Tag != "replace" {
				continue
			}
			seg := pyunicode.FromRunes(pySlice(rRunes, op.J1, op.J2))
			around := pyunicode.FromRunes(pySlice(rRunes, max(0, op.J1-3), op.J2+3))
			var kinds, words []string
			for _, k := range markerKinds {
				if mustSearchAt(k.pattern, pyre.NewText(around), 0, pyre.NoEnd) != nil {
					kinds = append(kinds, k.name)
				}
			}
			for _, w := range findall(contentWord, seg) {
				if notInO(w) {
					words = append(words, w)
				}
			}
			if len(kinds) > 0 || len(words) > 0 {
				out.Spans = append(out.Spans, SpanChange{seg, pyunicode.FromRunes(pySlice(oRunes, op.I1, op.I2)), kinds, words, pyContext(rRunes, op.J1, op.J2, 18)})
			}
		}
	}
	return out, nil
}
