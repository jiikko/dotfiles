package diff

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"ja-writing/internal/pystr"
	"ja-writing/lists"
)

// 群 R: 語の一覧 (本家の parse_list・_load_list・_list_info)。
// 一覧は lists パッケージが埋め込んで持つので、本家の「隣のファイルを読めなければ内蔵の一覧で動く」経路 (BUILTIN_LISTS・_LIST_PROBLEMS) は写さない。
// 本家が隣のファイルを読めたときの値 (list は "references/lists/<名前>.txt"、list_problem は null) だけを出す (issue 1076 の決めたこと 5)。

// ListError は一覧の書き方の誤り (本家の ValueError)。Message は本家の文面と同じ。
type ListError struct{ Message string }

func (e *ListError) Error() string { return e.Message }

// listProblemWidth は誤りの文に写す行の字数の上限 (本家の _LIST_PROBLEM_WIDTH)。
const listProblemWidth = 40

// ListItem は「名前<TAB>正規表現」の一覧の 1 行。Third は 3 つ目の欄 (気持ちの語の「原文の言い方」)。無いときは空。
type ListItem struct{ Name, Regex, Third string }

// 語だけの一覧 (強める語・慎重表現。本家の parse_list の pattern が偽) の読みは lists.Words。
// ParsePatternList は「名前<TAB>正規表現」の一覧の項目 (本家の parse_list の pattern が真)。
// 正規表現の欄が空の行は ListError (本家の ValueError)。正規表現として組めるかはここでは確かめない。
func ParsePatternList(text string) ([]ListItem, error) {
	var out []ListItem
	for _, line := range lists.Lines(text) {
		name, regex, _ := strings.Cut(line, "\t")
		regex, third, _ := strings.Cut(regex, "\t")
		if pystr.Strip(regex) == "" {
			shown := line
			if runes := []rune(line); len(runes) > listProblemWidth {
				shown = string(runes[:listProblemWidth]) + "…"
			}
			return nil, &ListError{"「名前<TAB>正規表現」の形でない行があります: " + shown}
		}
		out = append(out, ListItem{pystr.Strip(name), pystr.Strip(regex), pystr.Strip(third)})
	}
	return out, nil
}

// wordLists は本家の _LIST_PATTERNS (一覧の書き方。真は「名前<TAB>正規表現」) と _LIST_THIRD_COLUMN (3 つ目の欄を置ける一覧)。
var wordLists = map[string]struct{ pattern, third bool }{
	"nominalization": {pattern: true},
	"intensifiers":   {},
	"feelings":       {pattern: true, third: true},
	"hedges":         {},
}

// WordList は読んだ一覧。pattern の一覧は Items、語の一覧は Words に入る。
type WordList struct {
	Items []ListItem
	Words []string
}

var (
	wordListMu    sync.Mutex
	wordListCache = map[string]*WordList{}
)

// LoadList は埋め込んだ一覧を読む (本家の _load_list の items)。名前は nominalization・intensifiers・feelings・hedges のどれか。
// 埋め込みの一覧は固定で、読めないのも 3 つ目の欄の置き違いも組み立ての誤りなので panic する (記録との突き合わせとビルドの検査が見つける)。
func LoadList(name string) *WordList {
	wordListMu.Lock()
	defer wordListMu.Unlock()
	if l, ok := wordListCache[name]; ok {
		return l
	}
	spec, ok := wordLists[name]
	if !ok {
		panic(fmt.Sprintf("語の一覧 %q は無い", name))
	}
	data, err := lists.Read(name + ".txt")
	if err != nil {
		panic(err)
	}
	l := &WordList{}
	if spec.pattern {
		items, err := ParsePatternList(string(data))
		if err != nil {
			panic(name + ".txt: " + err.Error())
		}
		if !spec.third && slices.ContainsFunc(items, func(i ListItem) bool { return i.Third != "" }) {
			panic(name + ".txt: 3つ目の欄はこの一覧では使えません")
		}
		l.Items = items
	} else {
		l.Words = lists.Words(string(data))
	}
	wordListCache[name] = l
	return l
}

// ListSource は JSON の "list" に出す一覧の出どころ (本家の _list_info の list)。
// "list_problem" はいつも null (埋め込みなので、読めない経路が無い)。
func ListSource(name string) string {
	if _, ok := wordLists[name]; !ok {
		panic(fmt.Sprintf("語の一覧 %q は無い", name))
	}
	return "references/lists/" + name + ".txt"
}
