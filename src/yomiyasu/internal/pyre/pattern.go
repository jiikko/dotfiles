package pyre

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/dlclark/regexp2/v2"

	"yomiyasu/internal/pyunicode"
)

// NoEnd は endpos を指定しないときの値 (sys.maxsize)。
const NoEnd = math.MaxInt

// form は同じパターンを regexp2 のどの形で動かすか。
type form int

const (
	formSearch    form = iota // そのまま。どこからでも探す
	formMatch                 // 探索の開始位置に張り付ける (match)
	formFullmatch             // 開始位置から末尾までを覆う (fullmatch)
	formAdvance               // 直前の一致が空だったときの次の探索。開始位置の空の一致を断る (Python 3.7 以降の規則)
	formCount
)

type compiled struct {
	once sync.Once
	re   *regexp2.Regexp
	err  error
}

// Pattern は re.Pattern。compile した後は複数の goroutine から使える。
type Pattern struct {
	pattern    string
	flags      int
	groups     int
	groupIndex map[string]int
	groupNames []string
	closeRank  []int
	core       string
	forms      [formCount]compiled
}

// Compile は re.compile(pattern, flags)。
func Compile(pattern string, flags int) (*Pattern, error) {
	src := pyunicode.Runes(pattern)
	res, perr := parsePattern(src, flags)
	if perr != nil {
		return nil, perr
	}
	st := res.state
	var b strings.Builder
	if err := (&emitter{st: st}).seq(&b, res.body, res.flags); err != nil {
		return nil, err
	}
	p := &Pattern{
		pattern: pattern, flags: res.flags, groups: res.groups, groupIndex: map[string]int{}, core: b.String(),
		groupNames: make([]string, res.groups+1), closeRank: closeRanks(res.body, res.groups),
	}
	for name, i := range st.groupDict {
		p.groupIndex[name] = i
		p.groupNames[i] = name
	}
	if _, err := p.regexp(formSearch); err != nil {
		return nil, err
	}
	return p, nil
}

// MustCompile は Compile が失敗したら panic する。
func MustCompile(pattern string, flags int) *Pattern {
	p, err := Compile(pattern, flags)
	if err != nil {
		panic(err)
	}
	return p
}

// closeRanks はグループの閉じ括弧が (パターンの中で) 何番目に来るか。lastindex を決める。
func closeRanks(body seq, groups int) []int {
	ranks := make([]int, groups+1)
	next := 0
	var walk func(s seq)
	walk = func(s seq) {
		for _, n := range s {
			for _, child := range []seq{n.sub, n.item, n.yes, n.no} {
				walk(child)
			}
			for _, alt := range n.alts {
				walk(alt)
			}
			if n.op == opSubpattern && n.group != 0 {
				ranks[n.group] = next
				next++
			}
		}
	}
	walk(body)
	return ranks
}

func (p *Pattern) regexp(f form) (*regexp2.Regexp, error) {
	c := &p.forms[f]
	c.once.Do(func() {
		src := p.core
		switch f {
		case formMatch:
			src = `\G(?:` + src + `)`
		case formFullmatch:
			src = `\G(?:` + src + `)\z`
		case formAdvance:
			src = `(?:` + src + `)(?!\G)`
		default:
		}
		c.re, c.err = regexp2.Compile(src, regexp2.OptionMaxBacktrackingStackSize(-1))
		if c.err != nil {
			c.err = fmt.Errorf("パターン %q を regexp2 に直した %q が compile できません: %w", p.pattern, src, c.err)
		}
	})
	return c.re, c.err
}

// Pattern は元のパターンの文字列。
func (p *Pattern) Pattern() string { return p.pattern }

// Flags は re.Pattern.flags (UNICODE / ASCII の補いと、パターンの中のフラグを含む)。
func (p *Pattern) Flags() int { return p.flags }

// Groups は捕獲するグループの数。
func (p *Pattern) Groups() int { return p.groups }

// GroupIndex は名前付きのグループの名前と番号 (写しを返す)。
func (p *Pattern) GroupIndex() map[string]int {
	out := make(map[string]int, len(p.groupIndex))
	for k, v := range p.groupIndex {
		out[k] = v
	}
	return out
}

// Text は検索する文字列。コードポイントの列に直したものを持つので、同じ文字列を多くのパターンに当てるときは 1 回だけ作って使い回す。
type Text struct{ runes []rune }

// NewText は文字列 (WTF-8) を Text にする。
func NewText(s string) Text { return Text{pyunicode.Runes(s)} }

// Len はコードポイントの数。
func (t Text) Len() int { return len(t.runes) }

// Slice は [start, end) の字の列を文字列にする。
func (t Text) Slice(start, end int) string { return pyunicode.FromRunes(t.runes[start:end]) }

// String は全体を文字列にする。
func (t Text) String() string { return pyunicode.FromRunes(t.runes) }

// Match は re.Match。
type Match struct {
	re     *Pattern
	text   Text
	pos    int
	endpos int
	spans  []int // 2 * (グループ数 + 1)。参加しなかったグループは -1, -1
}

// Pattern はこの一致を作ったパターン。
func (m *Match) Pattern() *Pattern { return m.re }

// Pos と EndPos は探索の範囲 (Python の match.pos / match.endpos)。
func (m *Match) Pos() int { return m.pos }

// EndPos は探索の範囲の終わり。
func (m *Match) EndPos() int { return m.endpos }

// Span は group 番号のグループの [start, end)。参加しなかったグループは -1, -1。範囲外の番号も -1, -1。
func (m *Match) Span(group int) (start, end int) {
	if group < 0 || group > m.re.groups {
		return -1, -1
	}
	return m.spans[2*group], m.spans[2*group+1]
}

// Start はグループの始まり (参加しなかったら -1)。
func (m *Match) Start(group int) int { s, _ := m.Span(group); return s }

// End はグループの終わり (参加しなかったら -1)。
func (m *Match) End(group int) int { _, e := m.Span(group); return e }

// Group はグループの文字列。参加しなかったグループ・範囲外の番号は ok が偽。
func (m *Match) Group(group int) (s string, ok bool) {
	start, end := m.Span(group)
	if start < 0 {
		return "", false
	}
	return m.text.Slice(start, end), true
}

// Str は参加しなかったグループ (None) を区別できる文字列。
type Str struct {
	Value string
	Set   bool
}

// Groups は 1 番から最後のグループの文字列。
func (m *Match) Groups() []Str {
	out := make([]Str, m.re.groups)
	for g := 1; g <= m.re.groups; g++ {
		s, ok := m.Group(g)
		out[g-1] = Str{s, ok}
	}
	return out
}

// GroupDict は名前付きのグループの名前から文字列への写し。
func (m *Match) GroupDict() map[string]Str {
	out := make(map[string]Str, len(m.re.groupIndex))
	for name, g := range m.re.groupIndex {
		s, ok := m.Group(g)
		out[name] = Str{s, ok}
	}
	return out
}

// LastIndex は最後に閉じた捕獲グループの番号 (無ければ ok が偽)。
// 一致したグループのうち末尾が最も後ろのもの、同じ末尾なら閉じ括弧が後ろのものを選ぶ。
// 先読みの中のグループが後のグループより遠くまで進んだときは、Python の値とずれうる。
func (m *Match) LastIndex() (index int, ok bool) {
	bestEnd, bestRank := -1, -1
	for g := 1; g <= m.re.groups; g++ {
		start, end := m.Span(g)
		if start < 0 {
			continue
		}
		if end > bestEnd || end == bestEnd && m.re.closeRank[g] > bestRank {
			index, bestEnd, bestRank, ok = g, end, m.re.closeRank[g], true
		}
	}
	return index, ok
}

// LastGroup は LastIndex のグループの名前 (名前が無いときは ok が偽)。
func (m *Match) LastGroup() (name string, ok bool) {
	index, found := m.LastIndex()
	if !found || m.re.groupNames[index] == "" {
		return "", false
	}
	return m.re.groupNames[index], true
}

// Expand は Match.expand (置換の書式を一致で埋める)。
func (m *Match) Expand(template string) (string, error) {
	t, err := m.re.ParseTemplate(template)
	if err != nil {
		return "", err
	}
	return t.expand(m), nil
}

func clamp(v, n int) int { return min(max(v, 0), n) }

// run は rs[:endpos] の中で、pos から探して最初の一致を返す。
func (p *Pattern) run(f form, t Text, pos, endpos int) (*Match, error) {
	pos, endpos = clamp(pos, t.Len()), clamp(endpos, t.Len())
	if pos > endpos {
		return nil, nil
	}
	re, err := p.regexp(f)
	if err != nil {
		return nil, err
	}
	m, err := re.FindRunesMatchStartingAt(t.runes[:endpos], pos)
	if err != nil {
		return nil, fmt.Errorf("パターン %q の照合に失敗しました: %w", p.pattern, err)
	}
	if m == nil {
		return nil, nil
	}
	spans := make([]int, 2*(p.groups+1))
	spans[0], spans[1] = m.RuneIndex, m.RuneIndex+m.RuneLength
	for g := 1; g <= p.groups; g++ {
		spans[2*g], spans[2*g+1] = -1, -1
		if grp := m.GroupByNumber(g); grp != nil && len(grp.Captures) > 0 {
			spans[2*g], spans[2*g+1] = grp.RuneIndex, grp.RuneIndex+grp.RuneLength
		}
	}
	return &Match{re: p, text: t, pos: pos, endpos: endpos, spans: spans}, nil
}

// Search は pattern.search(string, pos, endpos)。一致しなければ nil。
func (p *Pattern) Search(t Text, pos, endpos int) (*Match, error) {
	return p.run(formSearch, t, pos, endpos)
}

// Match は pattern.match(string, pos, endpos)。
func (p *Pattern) Match(t Text, pos, endpos int) (*Match, error) {
	return p.run(formMatch, t, pos, endpos)
}

// FullMatch は pattern.fullmatch(string, pos, endpos)。
func (p *Pattern) FullMatch(t Text, pos, endpos int) (*Match, error) {
	return p.run(formFullmatch, t, pos, endpos)
}

// scan は finditer / findall / sub / split の共通の走査。match ごとに visit を呼び、false を返したら止める。
// 直前の一致が空だったら、次の探索は開始位置に空の一致を許さない (Python 3.7 以降)。
func (p *Pattern) scan(t Text, pos, endpos int, visit func(*Match) (bool, error)) error {
	start, advance := clamp(pos, t.Len()), false
	for {
		f := formSearch
		if advance {
			f = formAdvance
		}
		m, err := p.run(f, t, start, endpos)
		if err != nil || m == nil {
			return err
		}
		if more, err := visit(m); err != nil || !more {
			return err
		}
		advance = m.spans[0] == m.spans[1]
		start = m.spans[1]
	}
}

// Finditer は pattern.finditer(string, pos, endpos) の全部の一致。
func (p *Pattern) Finditer(t Text, pos, endpos int) ([]*Match, error) {
	var out []*Match
	err := p.scan(t, pos, endpos, func(m *Match) (bool, error) {
		out = append(out, m)
		return true, nil
	})
	return out, err
}

// Findall は pattern.findall。グループが無ければ一致した文字列、1 つなら そのグループ、2 つ以上ならグループの文字列の列を 1 要素にする
// (参加しなかったグループは空の文字列)。戻り値の各要素は、前の 2 つでは長さ 1 の列。
func (p *Pattern) Findall(t Text, pos, endpos int) ([][]string, error) {
	var out [][]string
	err := p.scan(t, pos, endpos, func(m *Match) (bool, error) {
		switch p.groups {
		case 0:
			s, _ := m.Group(0)
			out = append(out, []string{s})
		default:
			row := make([]string, p.groups)
			for g := 1; g <= p.groups; g++ {
				row[g-1], _ = m.Group(g)
			}
			out = append(out, row)
		}
		return true, nil
	})
	return out, err
}

// Subn は pattern.subn(repl, string, count) (repl は置換の書式)。count が 0 なら全部置き換える。
func (p *Pattern) Subn(repl string, t Text, count int) (string, int, error) {
	tmpl, err := p.ParseTemplate(repl)
	if err != nil {
		return "", 0, err
	}
	return p.subn(func(m *Match) (string, error) { return tmpl.expand(m), nil }, t, count)
}

// SubnFunc は repl が関数のときの pattern.subn。
func (p *Pattern) SubnFunc(repl func(*Match) string, t Text, count int) (string, int, error) {
	return p.subn(func(m *Match) (string, error) { return repl(m), nil }, t, count)
}

// Sub は pattern.sub (置換の書式)。
func (p *Pattern) Sub(repl string, t Text, count int) (string, error) {
	s, _, err := p.Subn(repl, t, count)
	return s, err
}

// SubFunc は repl が関数のときの pattern.sub。
func (p *Pattern) SubFunc(repl func(*Match) string, t Text, count int) (string, error) {
	s, _, err := p.SubnFunc(repl, t, count)
	return s, err
}

func (p *Pattern) subn(repl func(*Match) (string, error), t Text, count int) (string, int, error) {
	var sb strings.Builder
	last, n := 0, 0
	if count >= 0 {
		err := p.scan(t, 0, NoEnd, func(m *Match) (bool, error) {
			s, err := repl(m)
			if err != nil {
				return false, err
			}
			if last < m.spans[0] {
				sb.WriteString(t.Slice(last, m.spans[0]))
			}
			sb.WriteString(s)
			last = m.spans[1]
			n++
			return count == 0 || n < count, nil
		})
		if err != nil {
			return "", 0, err
		}
	}
	if last < t.Len() {
		sb.WriteString(t.Slice(last, t.Len()))
	}
	return sb.String(), n, nil
}

// Split は pattern.split(string, maxsplit)。グループがあればその文字列も間に入る (参加しなかったグループは Set が偽)。
func (p *Pattern) Split(t Text, maxsplit int) ([]Str, error) {
	var out []Str
	last, n := 0, 0
	if maxsplit >= 0 {
		err := p.scan(t, 0, NoEnd, func(m *Match) (bool, error) {
			out = append(out, Str{t.Slice(last, m.spans[0]), true})
			out = append(out, m.Groups()...)
			n++
			last = m.spans[1]
			return maxsplit == 0 || n < maxsplit, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return append(out, Str{t.Slice(last, t.Len()), true}), nil
}

// Escape は re.escape。
func Escape(s string) string {
	const special = "()[]{}?*+-|^$\\.&~# \t\n\r\v\f"
	var sb strings.Builder
	for _, r := range pyunicode.Runes(s) {
		if r < 0x80 && strings.ContainsRune(special, r) {
			sb.WriteByte('\\')
		}
		sb.WriteString(pyunicode.FromRunes([]rune{r}))
	}
	return sb.String()
}
