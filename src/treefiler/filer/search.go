package filer

import (
	"strings"
	"unicode"

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

// fuzzy は name の中の query の一致の段と、一致した位置 (バイトの範囲) を返す。query に大文字があれば大文字小文字を区別する。
func fuzzy(name, query string) (int, [][2]int) {
	if query == "" {
		return rankNone, nil
	}
	n, q := name, query
	if strings.ToLower(query) == query {
		n = strings.ToLower(name)
	}
	if strings.HasPrefix(n, q) {
		return rankPrefix, [][2]int{{0, len(q)}}
	}
	for i := strings.Index(n, q); i >= 0; {
		if isWordStart(name, i) {
			return rankWordStart, [][2]int{{i, i + len(q)}}
		}
		j := strings.Index(n[i+1:], q)
		if j < 0 {
			break
		}
		i += 1 + j
	}
	if i := strings.Index(n, q); i >= 0 {
		return rankSubstring, [][2]int{{i, i + len(q)}}
	}
	// 文字が順に含まれる (連続した一致は 1 つの範囲にまとめる)
	var spans [][2]int
	qi := 0
	qr := []rune(q)
	for bi, r := range n {
		if qi < len(qr) && r == qr[qi] {
			end := bi + len(string(r))
			if len(spans) > 0 && spans[len(spans)-1][1] == bi {
				spans[len(spans)-1][1] = end
			} else {
				spans = append(spans, [2]int{bi, end})
			}
			qi++
		}
	}
	if qi == len(qr) {
		return rankLoose, spans
	}
	return rankNone, nil
}

// isWordStart は name の i バイト目が語の先頭か (英数字でない文字の直後・小文字の直後の大文字)。
func isWordStart(name string, i int) bool {
	if i == 0 {
		return true
	}
	prev, _ := lastRune(name[:i])
	cur, _ := decodeRune(name[i:])
	if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if r, size := decodeRune(s[i:]); size == len(s)-i {
			return r, size
		}
	}
	return 0, 0
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
		m.setCur(s.origin)
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
