package lint

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/lists"
)

// G14 の一覧と手掛かりの語 (本家の _v220_load_list・_v220_parse_list・_v220_literal_re・_v220_patterns・_v220_thin_match)。
//
// 本家は references/lists/ の隣の一覧を読み、無い・読めないときは内蔵の同じ中身で動く。Go は埋め込んだ一覧 (lists/data) だけを読む
// (中身は固定で、内蔵と同じ中身かは _V220_BUILTIN_LISTS の記録と比べる)。一覧の誤りを見つけて内蔵へ戻す枝は、埋め込みの一覧では起きないので持たない。

// thinPair は thin-evaluation.txt の 1 行 (名詞, 述語の正規表現)。
type thinPair struct{ noun, predicate string }

// parseThinList は本家の _v220_parse_list (pattern=True)。「名詞<TAB>述語の正規表現」でない行は panic する (埋め込みの一覧は固定で、記録と比べる)。
func parseThinList(text string) []thinPair {
	var out []thinPair
	for _, line := range lists.Lines(text) {
		name, regex, _ := strings.Cut(line, "\t")
		name, regex = pystr.Strip(name), pystr.Strip(regex)
		if name == "" || regex == "" || strings.Contains(regex, "\t") {
			panic("thin-evaluation.txt に「名詞<TAB>述語の正規表現」の形でない行があります: " + line)
		}
		out = append(out, thinPair{name, regex})
	}
	return out
}

// literalAlternation は本家の _v220_literal_re の正規表現の文字列。先頭の字ごとにまとめ、長い語を先に試す。
// 同じ長さの語の並びは、本家は集合の順 (実行ごとに変わる) で、辞書順にしても答えは変わらない (同じ位置に同時に当たる同じ長さの別の語は無い)。
func literalAlternation(literals []string) string {
	uniq := slices.Clone(literals)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	slices.SortStableFunc(uniq, byLengthDesc)
	var heads []string
	tails := map[string][]string{}
	for _, literal := range uniq {
		if literal == "" {
			continue
		}
		head, tail := firstRuneSplit(literal)
		if _, ok := tails[head]; !ok {
			heads = append(heads, head)
		}
		tails[head] = append(tails[head], tail)
	}
	parts := make([]string, len(heads))
	for i, head := range heads {
		ts := tails[head]
		if slices.Contains(ts, "") {
			var nonEmpty []string
			for _, t := range ts {
				if t != "" {
					nonEmpty = append(nonEmpty, t)
				}
			}
			ts = append(nonEmpty, "")
		}
		if len(ts) == 1 {
			parts[i] = pyre.Escape(head) + pyre.Escape(ts[0])
			continue
		}
		escaped := make([]string, len(ts))
		for j, t := range ts {
			escaped[j] = pyre.Escape(t)
		}
		parts[i] = pyre.Escape(head) + "(?:" + strings.Join(escaped, "|") + ")"
	}
	return strings.Join(parts, "|")
}

// byLengthDesc は文字数の長い順 (本家の sorted(key=len, reverse=True)。同じ長さは元の並びのまま)。
func byLengthDesc(a, b string) int { return cmp.Compare(pystr.Len(b), pystr.Len(a)) }

func firstRuneSplit(s string) (string, string) {
	_, size := utf8.DecodeRuneInString(s)
	return s[:size], s[size:]
}

// literalPattern は literals の正規表現 (使うときに組む)。
func literalPattern(literals []string) *lazyPattern {
	return newLazyFrom(func() string { return literalAlternation(literals) }, 0)
}

// v220Tables は一覧から組んだ正規表現 (本家の _v220_patterns の戻り。一覧は固定なので、組むのは 1 回)。
type v220Tables struct {
	katakana *lazyPattern
	thin     *lazyPattern
	thinNoun *lazyPattern
	// nouns は thin-evaluation の名詞 (長い順)。body がどの名詞で始まるかを見る。
	nouns []string
	// trigger は文書全体に当てる手掛かりの語の正規表現 (漢字などで始まる語・カタカナで始まる語の 2 本)。
	trigger []*lazyPattern
}

var (
	v220Once  sync.Once
	v220Cache v220Tables
)

func v220Patterns() *v220Tables {
	v220Once.Do(func() {
		kdata, err := lists.Read("katakana-terms.txt")
		if err != nil {
			panic(err)
		}
		tdata, err := lists.Read("thin-evaluation.txt")
		if err != nil {
			panic(err)
		}
		katakana := lists.Words(string(kdata))
		thin := parseThinList(string(tdata))
		t := &v220Cache
		// 番号や記号を付けた項目の名前 (マイルストーン1、マイルストーン B) は、何を指すかが決まっているので外す。
		t.katakana = newLazy("(?:"+literalAlternationPlain(katakana)+")(?![ \u3000]?[0-9０-９A-Za-zＡ-Ｚａ-ｚ])", 0)
		nounSet := map[string]bool{}
		bodies := make([]string, len(thin))
		for i, p := range thin {
			nounSet[p.noun] = true
			bodies[i] = "(?:" + pyre.Escape(p.noun) + "(?:が|は|も)" + thinAdverb + "(?:" + p.predicate + "))"
		}
		t.nouns = slices.Sorted(maps.Keys(nounSet))
		slices.SortStableFunc(t.nouns, byLengthDesc)
		t.thin = newLazy(`^(?P<prefix>[^、，,]{0,`+strconv.Itoa(thinPrefixMax)+`}?)(?P<body>`+strings.Join(bodies, "|")+`)`+thinTail+`[。．！!]*$`, 0)
		t.thinNoun = literalPattern(t.nouns)
		// 手掛かりの語は先頭の平仮名を外し、漢字などで始まる語とカタカナで始まる語を別の正規表現にする (本家は速さのため)。
		literals := map[string]bool{}
		for _, group := range [][]string{v220TriggerLiterals, katakana, t.nouns} {
			for _, literal := range group {
				if stripped := strings.TrimLeftFunc(literal, isHiraganaRune); stripped != "" {
					literals[stripped] = true
				}
			}
		}
		var plain, kata []string
		for literal := range literals {
			if first, _ := utf8.DecodeRuneInString(literal); isKatakanaRune(first) {
				kata = append(kata, literal)
			} else {
				plain = append(plain, literal)
			}
		}
		for _, group := range [][]string{plain, kata} {
			if len(group) > 0 {
				t.trigger = append(t.trigger, literalPattern(group))
			}
		}
	})
	return &v220Cache
}

// literalAlternationPlain は本家の「長い語を先に " | " でつなぐ」(katakana_term の語。先頭の字ではまとめない)。
func literalAlternationPlain(words []string) string {
	uniq := slices.Clone(words)
	slices.Sort(uniq)
	uniq = slices.Compact(uniq)
	slices.SortStableFunc(uniq, byLengthDesc)
	escaped := make([]string, len(uniq))
	for i, w := range uniq {
		escaped[i] = pyre.Escape(w)
	}
	return strings.Join(escaped, "|")
}

// thinMatch は本家の _v220_thin_match (thin_evaluation の正規表現を文の先頭から当てる)。
func thinMatch(sentence string) *pyre.Match {
	m, err := v220Patterns().thin.Get().Match(pyre.NewText(sentence), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m
}
