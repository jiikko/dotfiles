package filer

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
)

// search.go はカーソルの列の中のあいまい検索 (`/`。spec §6.2)。入力は tuikit/lineedit (glogx-ui-guide §7 の編集キー)。

type searchState struct {
	active  bool
	line    lineedit.Line
	origin  *node // 検索を始めたときのカーソル (Esc / 一致なしで戻る先)
	at      int   // 今の一致の番号
	matches []*node
}

// rank の段 (小さいほど良い。spec §6.2 の fuzzy)。
const (
	rankPrefix = iota
	rankWordStart
	rankSubstring
	rankLoose
	rankNone
)

// fuzzy は name の中の query の一致の段と、一致した位置 (元の name のバイトの範囲) を返す。query に大文字があれば
// 大文字小文字を区別する。
// 🚨 比べるのは文字 (rune) 単位で、位置は元の name のバイトで返す。strings.ToLower した文字列の上で数えると、
// 小文字にすると長さが変わる文字 (Ⱥ は 2 → 3 バイト) で位置がずれ、元の name を切ると panic した (レビューで再現 2026-10-09)。
func fuzzy(name, query string) (int, [][2]int) {
	if query == "" {
		return rankNone, nil
	}
	fold := strings.ToLower(query) == query
	type ch struct {
		r          rune
		start, end int
	}
	n := make([]ch, 0, len(name))
	for i, r := range name {
		if fold {
			r = unicode.ToLower(r)
		}
		n = append(n, ch{r: r, start: i, end: i + utf8.RuneLen(r)})
	}
	for k := range n { // end は元の name の次の文字の頭 (小文字にした rune の長さではなく)
		if k+1 < len(n) {
			n[k].end = n[k+1].start
		} else {
			n[k].end = len(name)
		}
	}
	q := []rune(query)
	at := func(i int) bool { // n[i:] が q で始まるか
		if i+len(q) > len(n) {
			return false
		}
		for k, r := range q {
			if n[i+k].r != r {
				return false
			}
		}
		return true
	}
	span := func(i int) [][2]int { return [][2]int{{n[i].start, n[i+len(q)-1].end}} }
	if at(0) {
		return rankPrefix, span(0)
	}
	first := -1
	for i := 1; i < len(n); i++ {
		if !at(i) {
			continue
		}
		if first < 0 {
			first = i
		}
		if isWordStart(name, n[i].start) {
			return rankWordStart, span(i)
		}
	}
	if first >= 0 {
		return rankSubstring, span(first)
	}
	// 文字が順に含まれる (連続した一致は 1 つの範囲にまとめる)
	var spans [][2]int
	qi := 0
	for _, c := range n {
		if qi < len(q) && c.r == q[qi] {
			if len(spans) > 0 && spans[len(spans)-1][1] == c.start {
				spans[len(spans)-1][1] = c.end
			} else {
				spans = append(spans, [2]int{c.start, c.end})
			}
			qi++
		}
	}
	if qi == len(q) {
		return rankLoose, spans
	}
	return rankNone, nil
}

// isWordStart は name の i バイト目が語の先頭か (英数字でない文字の直後・小文字の直後の大文字)。
func isWordStart(name string, i int) bool {
	if i == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(name[:i])
	cur, _ := utf8.DecodeRuneInString(name[i:])
	if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}

// column はカーソルの列 (同じ深さの見えている項目を上から)。
func (m *Model) column(of *node) []*node {
	pos := m.layout()
	d := of.depth()
	var col []*node
	for n := range pos {
		if n.depth() == d {
			col = append(col, n)
		}
	}
	sortByY(col, pos)
	return col
}

func sortByY(ns []*node, pos map[*node]place) {
	for i := 1; i < len(ns); i++ {
		for j := i; j > 0 && pos[ns[j]].y < pos[ns[j-1]].y; j-- {
			ns[j], ns[j-1] = ns[j-1], ns[j]
		}
	}
}

// findMatches は列の中で query に一致する項目を、良い段から (同じ段は上から) 並べる。
// どれかが部分一致以上なら、文字が順に含まれるだけの一致は外す (spec §6.2)。
func findMatches(col []*node, query string) []*node {
	var byRank [rankNone][]*node
	for _, n := range col {
		if r, _ := fuzzy(n.name, query); r < rankNone {
			byRank[r] = append(byRank[r], n)
		}
	}
	var out []*node
	for r := range rankLoose {
		out = append(out, byRank[r]...)
	}
	if len(out) == 0 {
		out = byRank[rankLoose]
	}
	return out
}

func (m *Model) startSearch() {
	m.search = searchState{active: true, origin: m.cur}
}

// searchKey は検索中のキー。lineedit の編集キーを先に渡し、Enter / Esc / 一致の移動だけを自分で捌く。
func (m *Model) searchKey(key, text string) {
	s := &m.search
	switch {
	case key == "enter":
		s.active = false
		if q := s.line.String(); q != "" && len(s.matches) > 0 {
			m.lastSearch = q
		}
		return
	case key == "esc" || key == "ctrl+c":
		s.active = false
		if m.inTree(s.origin) { // 検索中のライブ更新で消えていたら、今の位置に留まる
			m.setCur(s.origin)
		}
		return
	case key == "tab" || key == "down" || key == "ctrl+n":
		m.stepMatch(1)
		return
	case key == "shift+tab" || key == "ctrl+p" || (key == "up" && !s.line.Empty()):
		m.stepMatch(-1)
		return
	case key == "up" && s.line.Empty():
		s.line.Insert(m.lastSearch) // 空欄の ↑ は前回の検索を呼び戻す
	case key == "backspace" && s.line.Empty():
		s.active = false // 空欄の backspace で閉じる
		m.setCur(s.origin)
		return
	default:
		if !s.line.Key(key, text) {
			return
		}
	}
	s.matches = findMatches(m.column(s.origin), s.line.String())
	s.at = 0
	if len(s.matches) > 0 {
		m.setCur(s.matches[0])
	} else {
		m.setCur(s.origin)
	}
}

func (m *Model) stepMatch(d int) {
	s := &m.search
	live := s.matches[:0]
	for _, n := range s.matches {
		if m.inTree(n) { // 検索中のライブ更新で消えた一致は外す (木の外へカーソルを出さない)
			live = append(live, n)
		}
	}
	s.matches = live
	s.at = min(s.at, max(len(live)-1, 0))
	if len(s.matches) == 0 {
		return
	}
	s.at = (s.at + d + len(s.matches)) % len(s.matches)
	m.setCur(s.matches[s.at])
}

// repeatSearch は確定した前回の検索で、カーソルの列の次 / 前の一致へ (n / N)。
func (m *Model) repeatSearch(d int) {
	if m.lastSearch == "" {
		m.fail("前回の検索がありません (/ で検索する)")
		return
	}
	ms := findMatches(m.column(m.cur), m.lastSearch)
	if len(ms) == 0 {
		m.fail("一致がありません: " + m.lastSearch)
		return
	}
	// 列の中の並び (上から) で、今のカーソルの次 / 前の一致へ
	col := m.column(m.cur)
	idx := map[*node]int{}
	for i, n := range col {
		idx[n] = i
	}
	cur := idx[m.cur]
	best := -1
	for _, n := range ms {
		i := idx[n]
		switch {
		case d > 0 && i > cur && (best < 0 || i < idx[ms[best]]):
			best = indexOf(ms, n)
		case d < 0 && i < cur && (best < 0 || i > idx[ms[best]]):
			best = indexOf(ms, n)
		}
	}
	if best < 0 { // 端を越えたら反対側から (巡回)
		best = 0
		for j, n := range ms {
			if (d > 0 && idx[n] < idx[ms[best]]) || (d < 0 && idx[n] > idx[ms[best]]) {
				best = j
			}
		}
	}
	m.setCur(ms[best])
}

func indexOf(ns []*node, n *node) int {
	for i, k := range ns {
		if k == n {
			return i
		}
	}
	return -1
}
