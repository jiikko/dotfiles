package report

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"dupscan/internal/tree"
)

// EvidenceEntry は evidence の 1 行: 語を含むソースの行と、その行が属する単位。
type EvidenceEntry struct {
	File string
	Line int
	// Unit は doc の行ならその doc が付いた単位、それ以外なら行を囲む単位 (無ければ空)。
	Unit   string
	Symbol string // doc の単位の、署名を含まない宣言名
	Text   string
	Calls  []string // その行で呼んでいる名前 (重複を除いて出た順)
	Hits   int      // doc の節だけ: その単位の doc で語を含む行の数 (Line と Text は最初の行)
}

// CalledName は語を含む行 (テスト・ソース) で呼ばれている名前と、その行の数。
type CalledName struct {
	Name  string
	Lines int
}

// EvidenceSummary は evidence の出力。行は 3 つに分け、同じ行を 2 つに数えない。
type EvidenceSummary struct {
	Term      string
	Truncated bool            // 行か単位の表示が短縮されている
	Doc       []EvidenceEntry // doc comment に語がある単位 (単位ごとに 1 件)
	DocLines  int             // Doc の単位の doc で語を含む行の合計
	Test      []EvidenceEntry // test のファイルか test の単位の中の行 (doc を除く)
	Source    []EvidenceEntry // それ以外の行 (コメント・文字列を含む)
	Called    []CalledName    // Test と Source の行で呼ばれている、repo の中に定義がある名前 (行の数の多い順)
}

const (
	evidenceTextRunes = 160
	evidenceUnitRunes = 80 // 囲む単位の署名は同じ単位の行のたびに繰り返すので、本文より短く切る
)

// BuildEvidence は sources の各行から term (大文字小文字を区別しない) を含む行を探して分ける。
// 語の一致であって、定義・参照・呼び出しの照合ではない (行の中のどこに語があるかは見ない)。
// defined は repo の中に定義がある名前 (集計をそれに絞る。XCTAssertEqual や print のような外の名前で上位が埋まらないように)。
func BuildEvidence(term string, sources map[string][]byte, units []*tree.Unit, calls []tree.Call, defined map[string]bool, isTest func(string) bool) EvidenceSummary {
	s := EvidenceSummary{Term: term}
	needle := strings.ToLower(term)
	byFile := map[string][]*tree.Unit{}
	for _, u := range units {
		byFile[u.File] = append(byFile[u.File], u)
	}
	type at struct {
		file string
		line int
	}
	callsAt := map[at][]string{}
	for _, c := range calls {
		k := at{c.File, c.Line}
		if !slices.Contains(callsAt[k], c.Name) {
			callsAt[k] = append(callsAt[k], c.Name)
		}
	}
	files := make([]string, 0, len(sources))
	for f := range sources {
		files = append(files, f)
	}
	sort.Strings(files)
	docAt := map[*tree.Unit]int{} // 単位 → s.Doc の添字
	called := map[string]int{}
	for _, file := range files {
		for i, line := range strings.Split(string(sources[file]), "\n") {
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			n := i + 1
			e := EvidenceEntry{File: file, Line: n, Text: evidenceText(line), Calls: callsAt[at{file, n}]}
			textTruncated := e.Text != strings.ToValidUTF8(strings.TrimSpace(line), "\uFFFD")
			if u := docOwner(byFile[file], n); u != nil {
				s.DocLines++
				if j, ok := docAt[u]; ok {
					s.Doc[j].Hits++
					continue
				}
				e.Unit, e.Symbol, e.Calls, e.Hits = label(ref(u)), u.Name, nil, 1 // doc の中の行に呼び出しは無い
				docAt[u] = len(s.Doc)
				s.Doc = append(s.Doc, e)
				s.Truncated = s.Truncated || textTruncated || utf8.RuneCountInString(e.Unit) > evidenceUnitRunes
				continue
			}
			for _, name := range e.Calls {
				if defined[tree.MatchName(name)] {
					called[name]++
				}
			}
			u := enclosing(byFile[file], n)
			if u != nil {
				e.Unit = label(ref(u))
			}
			s.Truncated = s.Truncated || textTruncated || utf8.RuneCountInString(e.Unit) > evidenceUnitRunes
			if isTest(file) || u != nil && u.Test {
				s.Test = append(s.Test, e)
			} else {
				s.Source = append(s.Source, e)
			}
		}
	}
	for name, lines := range called {
		s.Called = append(s.Called, CalledName{Name: name, Lines: lines})
	}
	sort.Slice(s.Called, func(i, j int) bool {
		a, b := s.Called[i], s.Called[j]
		if a.Lines != b.Lines {
			return a.Lines > b.Lines
		}
		return a.Name < b.Name
	})
	return s
}

// docOwner は line を doc comment の中に持つ単位 (クロージャは doc を持たない)。
func docOwner(units []*tree.Unit, line int) *tree.Unit {
	for _, u := range units {
		if u.DocStart > 0 && u.DocStart <= line && line <= u.DocEnd {
			return u
		}
	}
	return nil
}

// evidenceText は行を 1 行の表示にする (前後の空白を除き、壊れた UTF-8 を置き換え、長い行を切る)。
func evidenceText(line string) string {
	return shorten(strings.ToValidUTF8(strings.TrimSpace(line), "\uFFFD"), evidenceTextRunes)
}

func shorten(t string, runes int) string {
	if utf8.RuneCountInString(t) <= runes {
		return t
	}
	return string([]rune(t)[:runes]) + "…"
}
