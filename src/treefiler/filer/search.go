package filer

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
)

// search.go は開いている木の全体のあいまい検索 (`/`。spec §6.2)。入力は tuikit/lineedit (glogx-ui-guide §7 の編集キー)。

type searchState struct {
	active  bool
	line    lineedit.Line
	origin  *node // 検索を始めたときのカーソル (Esc / 一致なしで戻る先)
	at      int   // 今の一致の番号
	matches []*node
	hit     map[*node]bool // matches と同じ集合。ハイライトはこれに入る項目だけに付ける (光っているのに Tab で届かない項目を作らない)
	histAt  int            // 履歴を辿っている位置 (len(searchHist) = 辿っていない・打ちかけの語)
	draft   string         // 辿り始めたときの打ちかけの語 (この語で始まる履歴だけを辿り、↓ で最新を越えたら戻す。vim の / と同じ)
}

// searchHistKeep は検索の履歴を覚える件数 (ユーザー決定 2026-10-31)。
const searchHistKeep = 50

// searchHistReadLines は検索の履歴のファイルから読む行の数。開いている間は追記だけなので、同じ語を何度も打つとファイルに重なる。
// 読む窓が 50 件ぶんしかないと、重なった語で埋まって、50 件に満たないうちに別の語が落ちる。読んだら畳んだ形で書き直す (loadHistory)
const searchHistReadLines = 2000

// loadSearchHistory は検索の履歴を読み、同じ語は新しい方だけを残して最新 searchHistKeep 件にする。
func loadSearchHistory() []string {
	return loadHistory(searchHistoryPath(), searchHistReadLines, func(h []string) []string { return dedupeKeepNewest(h, searchHistKeep) })
}

// dedupeKeepNewest は hist (古い順) から、同じ語の古い方を落として末尾 keep 件を返す。
func dedupeKeepNewest(hist []string, keep int) []string {
	seen := map[string]bool{}
	var rev []string
	for i := len(hist) - 1; i >= 0 && len(rev) < keep; i-- {
		if !seen[hist[i]] {
			seen[hist[i]] = true
			rev = append(rev, hist[i])
		}
	}
	slices.Reverse(rev)
	return rev
}

// rememberSearch は確定した検索語を履歴の最新にする (前にあった同じ語は外す)。
func (m *Model) rememberSearch(q string) {
	if !keepInHistory(q) {
		return
	}
	m.searchHist = dedupeKeepNewest(append(slices.Clone(m.searchHist), q), searchHistKeep)
	appendHistory(searchHistoryPath(), q)
}

// stepHistory は打ちかけの語で始まる履歴を、d < 0 で古い方へ・d > 0 で新しい方へ 1 つ辿る。新しい方の端を越えたら打ちかけの語に戻す。
// 当たる語が無ければ何もしない。辿ったら一致を引き直す。
func (m *Model) stepHistory(d int) {
	s, h := &m.search, m.searchHist
	if s.histAt == len(h) {
		s.draft = s.line.String()
	}
	next := -1
	for i := s.histAt + d; i >= 0 && i < len(h); i += d {
		if strings.HasPrefix(h[i], s.draft) {
			next = i
			break
		}
	}
	var text string
	switch {
	case next >= 0:
		s.histAt, text = next, h[next]
	case d > 0 && s.histAt < len(h):
		s.histAt, text = len(h), s.draft
	default:
		return
	}
	s.line.Reset()
	s.line.Insert(text)
	m.refreshMatches()
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
	var stack [64]ch // 64 文字までの名前は確保しない (検索の打鍵で名前ごとに確保していた。issue 733)
	n := stack[:0]
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

// openTree は開いている木の全体を読み順 (root → 開いたフォルダの見せる子 → 次の兄弟) で並べる。開いていないフォルダの中は含めない
// (見えない項目はハイライトできず、飛ぶには途中を開いて木の形を変えることになる。spec §6.2)。画面の外の項目も含む
// (仮想化した配置は画面の近くしか持たない)。
func (m *Model) openTree() []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		out = append(out, n)
		if n.dir && n.expanded {
			for _, k := range m.kids(n) {
				walk(k)
			}
		}
	}
	walk(m.root)
	return out
}

// findMatches は order (読み順) の中で query に一致する項目を、良い段から並べる。同じ段の中は from から読み順で次に来るものを先に
// (from が一致すれば from から)、末尾を越えたら先頭から。どれかが部分一致以上なら、文字が順に含まれるだけの一致は外す (spec §6.2)。
func findMatches(order []*node, query string, from *node) []*node {
	start := 0
	for i, n := range order {
		if n == from {
			start = i
			break
		}
	}
	var byRank [rankNone][]*node
	for k := range order {
		n := order[(start+k)%len(order)]
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

// setMatches は一致とハイライトの集合を 1 か所で揃えて置く。
func (s *searchState) setMatches(ms []*node) {
	s.matches = ms
	s.hit = make(map[*node]bool, len(ms))
	for _, n := range ms {
		s.hit[n] = true
	}
}

func (m *Model) startSearch() {
	m.search = searchState{active: true, origin: m.cur, histAt: len(m.searchHist)}
}

// searchKey は検索中のキー。lineedit の編集キーを先に渡し、Enter / Esc / 一致の移動だけを自分で捌く。
func (m *Model) searchKey(key, text string) {
	s := &m.search
	switch {
	case key == "enter":
		if q := s.line.String(); q != "" {
			m.rememberSearch(q) // 一致が無くても残す (vim と同じ。打ち直す手間を省く)
			if len(s.matches) > 0 {
				m.lastSearch = q
			}
		}
		m.endSearch(false)
		return
	case key == "esc" || key == "ctrl+c":
		m.endSearch(true)
		return
	case key == "tab" || key == "ctrl+n":
		m.stepMatch(1)
		return
	case key == "shift+tab" || key == "ctrl+p":
		m.stepMatch(-1)
		return
	case key == "up": // 履歴を辿る (vim の / と同じく、打ちかけの語で始まるものだけ)
		m.stepHistory(-1)
		return
	case key == "down":
		m.stepHistory(1)
		return
	case (key == "backspace" || key == "ctrl+h") && s.line.Empty(): // 空欄の backspace で閉じる (ctrl+h は lineedit と同じく backspace の別名)
		m.endSearch(true)
		return
	default:
		before := s.line.String()
		if !s.line.Key(key, text) {
			return
		}
		if s.line.String() != before { // 打ち直したら、次の ↑ はその語で始まる履歴を新しい方から辿る (カーソルを動かしただけでは辿り続ける)
			s.histAt = len(m.searchHist)
		}
	}
	m.refreshMatches()
}

// endSearch は検索を閉じる (restore なら始めた位置へ戻る)。一致と起点は手放す: 閉じた後は誰も読まず、残すと次の / まで消えた部分木を掴む。
func (m *Model) endSearch(restore bool) {
	if restore {
		m.restoreOrigin()
	}
	s := &m.search
	s.active, s.matches, s.hit, s.origin = false, nil, nil, nil
}

// refreshMatches は検索語が変わった後に一致を引き直し、いちばん良い一致へ移る (無ければ始めた位置へ)。
func (m *Model) refreshMatches() {
	s := &m.search
	if s.line.Empty() { // 空欄は何にも一致しない (木を辿らない)
		s.setMatches(nil)
		s.at = 0
		m.restoreOrigin()
		return
	}
	s.setMatches(findMatches(m.openTree(), s.line.String(), s.origin))
	s.at = 0
	if len(s.matches) > 0 {
		m.setCur(s.matches[0])
	} else {
		m.restoreOrigin()
	}
}

// restoreOrigin は検索を始めた位置へ戻す。検索中のライブ更新で消えていたら、生きているいちばん近い祖先へ
// (Refresh / applyChange がカーソルに使う規則と同じ)。「今の位置に留まる」にしない: 一致なしで一度戻った後は、
// 今の位置そのものが消えた origin を指している。
func (m *Model) restoreOrigin() {
	m.setCur(m.liveAncestor(m.search.origin))
}

func (m *Model) stepMatch(d int) {
	s := &m.search
	// 検索中のライブ更新で消えた・畳まれた一致は外す (木の外へカーソルを出さない)。今の開いた木の集合で引く
	// (一致ごとに inTree で兄弟を線形に探すと、一致の数 × 項目の数になる)
	inTree := map[*node]bool{}
	for _, n := range m.openTree() {
		inTree[n] = true
	}
	live := s.matches[:0]
	for _, n := range s.matches {
		if inTree[n] {
			live = append(live, n)
		}
	}
	s.setMatches(compactTail(s.matches, live))
	s.at = min(s.at, max(len(live)-1, 0))
	if len(s.matches) == 0 {
		return
	}
	s.at = listnav.Cycle(s.at, d, len(s.matches))
	m.setCur(s.matches[s.at])
}

// repeatSearch は確定した前回の検索で、開いている木の読み順で次 / 前の一致へ (n / N)。
func (m *Model) repeatSearch(d int) {
	if m.lastSearch == "" {
		m.fail("前回の検索がありません (/ で検索する)")
		return
	}
	col := m.openTree() // 読み順の並び。1 回だけ作る
	ms := findMatches(col, m.lastSearch, m.root)
	if len(ms) == 0 {
		m.fail("一致がありません: " + m.lastSearch)
		return
	}
	// 今のカーソルの次 / 前の一致へ
	idx := map[*node]int{}
	for i, n := range col {
		idx[n] = i
	}
	cur := idx[m.cur]
	best := -1
	for j, n := range ms { // 🚨 添字で回す (中で indexOf を呼ぶと一致の数の 2 乗)
		i := idx[n]
		switch {
		case d > 0 && i > cur && (best < 0 || i < idx[ms[best]]):
			best = j
		case d < 0 && i < cur && (best < 0 || i > idx[ms[best]]):
			best = j
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
