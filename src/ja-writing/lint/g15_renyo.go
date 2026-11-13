package lint

import (
	"slices"
	"sort"
	"strings"
	"sync"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/lists"
)

// G15 連用形の名詞 (renyo_noun。本家 yomiyasu_lint.py の 4695-4720 と 4887-4920)。
//
// 本家は一覧 (references/lists/renyo-nouns.txt。ないとき・読めないときは内蔵の同じ中身) から正規表現を組む。Go は埋め込んだ一覧 (lists/data) を読む。
// 文書全体に「の[*_]*語」を元の文字 (記号を含む) で当てて、当たりの始まりの行だけを見える文字で調べる (速さのための絞り込みだが、元の文字に手掛かりが無い行は調べない形をそのまま写す)。

// g15State は G15 が文書の中で持つ状態。一覧から組んだ正規表現は文書をまたいで 1 回だけ作る (renyoTables)。
type g15State struct{}

const (
	renyoBefore = `(?<=[一-龥々〆ァ-ヴーぁ-んA-Za-z0-9０-９Ａ-Ｚａ-ｚ）」』\]) ])の`
	renyoAfter  = `(?=が(?!た[いかくけさそ])|を|は|に(?!く[いかくけさそ])|で|も(?!の)|と|や(?!す[いかくけさそ])|へ|の|だけ|まで` +
		`|など|から|より|こそ|さえ|しか|ばかり|くらい|ぐらい|ほど` +
		`|[、。，．）」』！？!?（(・／/:：|｜\s]|$)`
	renyoMessageHead = "他動詞を1語だけ名詞にした語を「の」で受ける形（当たった言い方: "
	v220MessageTail  = "の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。"
)

// renyoTable は一覧から組んだもの (本家の _renyo_pattern の戻り)。pattern は行の見える文字用、prefilter は文書全体の絞り込み用、skipAfter は語ごとに外す後ろの文字列。
type renyoTable struct {
	pattern, prefilter *pyre.Pattern
	skipAfter          map[string][]string
}

var (
	renyoOnce  sync.Once
	renyoCache renyoTable
)

// parseList は本家の _v220_parse_list (pattern=False)。空行と「#」で始まる行は読み飛ばし、各行の前後の空白を除く。
func parseList(text string) []string {
	text = strings.TrimPrefix(text, "\ufeff")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if pystr.Strip(line) == "" || strings.HasPrefix(pystr.LStrip(line), "#") {
			continue
		}
		out = append(out, pystr.Strip(line))
	}
	return out
}

func renyoPatterns() renyoTable {
	renyoOnce.Do(func() {
		data, err := lists.Read("renyo-nouns.txt")
		if err != nil {
			panic(err)
		}
		skip := map[string][]string{}
		var words []string
		for _, item := range parseList(string(data)) {
			word, after, _ := strings.Cut(item, "\t")
			word = pystr.Strip(word)
			if word == "" {
				continue
			}
			words = append(words, word)
			if after = pystr.Strip(after); after != "" {
				skip[word] = []string{}
				for _, part := range strings.Split(after, "|") {
					if part != "" {
						skip[word] = append(skip[word], part)
					}
				}
			}
		}
		if len(words) == 0 {
			return
		}
		words = uniqueInOrder(words)
		// 長い語を先に (本家は set の順で、同じ長さの語は同じ位置で同時に当たらないので答えは変わらない)。
		sort.SliceStable(words, func(i, j int) bool {
			if li, lj := len([]rune(words[i])), len([]rune(words[j])); li != lj {
				return li > lj
			}
			return words[i] < words[j]
		})
		escaped := make([]string, len(words))
		for i, w := range words {
			escaped[i] = pyre.Escape(w)
		}
		alternation := strings.Join(escaped, "|")
		renyoCache = renyoTable{
			pattern:   pyre.MustCompile(renyoBefore+"("+alternation+")"+renyoAfter, 0),
			prefilter: pyre.MustCompile("の[*_]*(?:"+alternation+")", 0),
			skipAfter: skip,
		}
	})
	return renyoCache
}

// candidateLines は text に p を当てた当たりの始まりがある行の番号 (昇順。本家の bisect_right(starts, match.start()))。
func (s *lintState) candidateLines(p *pyre.Pattern) []int {
	matches, err := p.Finditer(pyre.NewText(s.an.Text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	positions := make([]int, len(matches))
	for i, m := range matches {
		positions[i] = m.Start(0)
	}
	return s.linesAt(positions)
}

// linesAt は文書の文字の位置 (コードポイントの数) を含む行の番号 (重ねず昇順)。
func (s *lintState) linesAt(positions []int) []int {
	starts := make([]int, len(s.an.Lines))
	for i, row := range s.an.Lines {
		starts[i] = row.Start
	}
	set := map[int]bool{}
	for _, position := range positions {
		set[sort.Search(len(starts), func(i int) bool { return starts[i] > position })] = true
	}
	lines := make([]int, 0, len(set))
	for n := range set {
		lines = append(lines, n)
	}
	sort.Ints(lines)
	return lines
}

// g15RenyoFindings は後から足す検出の 2 番目 (本家の _v230_renyo_findings)。
func (s *lintState) g15RenyoFindings() {
	table := renyoPatterns()
	if table.pattern == nil || len(s.an.Lines) == 0 {
		return
	}
	for _, lineNo := range s.candidateLines(table.prefilter) {
		row := s.an.Lines[lineNo-1]
		if (row.Kind != "paragraph" && row.Kind != "list" && row.Kind != "heading" && row.Kind != "table") || row.QuoteDepth != 0 {
			continue
		}
		scan := s.doc.v220ScanText(lineNo)
		scanText := pyre.NewText(scan)
		matches, err := table.pattern.Finditer(scanText, 0, pyre.NoEnd)
		if err != nil {
			panic(err)
		}
		var words []string
		for _, m := range matches {
			word, _ := m.Group(1)
			rest := scanText.Slice(m.End(0), scanText.Len())
			if slices.ContainsFunc(table.skipAfter[word], func(part string) bool { return strings.HasPrefix(rest, part) }) {
				continue
			}
			words = append(words, word)
		}
		if len(words) == 0 {
			continue
		}
		words = uniqueInOrder(words)
		f := newFinding("renyo_noun", lineNo, "info", renyoMessage(words), pystr.Strip(row.Raw))
		f.Words = words
		s.add(f)
	}
}

// renyoMessage は renyo_noun の message (本家の _RENYO_MESSAGE.format("、".join("の" + word for word in rest)))。post.go の withWords が呼ぶ。
func renyoMessage(words []string) string {
	shown := make([]string, len(words))
	for i, w := range words {
		shown[i] = "の" + w
	}
	return renyoMessageHead + strings.Join(shown, "、") + "）" + v220MessageTail
}
