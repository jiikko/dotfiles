package exclude

import (
	"sort"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// Set は 1 回の走査に効かせる除外。どの除外が実際に何かを消したかを数え、効いていない除外を出す。
type Set struct {
	rules     []Rule
	used      []bool
	ambiguous []bool
	pinCount  map[string]int
	// basePins は gate のときの base の単位の釘。組・単位の除外は、base に同じ釘の単位があったときか、
	// その釘が新しく足された要素のときだけ効く (消えた写しの釘の場所に後から同じ写しを置いても覆わない)。nil なら制限しない (report)。
	basePins map[string]bool
	newElems map[string]bool
	inScope  func(file string) bool
	// narrowed は既定より狭い走査 (段・下限を変えた) か。狭い走査では組が作られないだけの除外を「効いていない」と言えない。
	narrowed bool
}

// New は head の単位 (path の除外で外す前の全部) に対する除外を作る。c が nil なら除外は無い。
// inScope は起点の中のファイルか (nil なら repo 全体)。起点の外を指す除外は「効いていない」に数えない。
func New(c *Config, entries []*group.Entry, inScope func(string) bool) *Set {
	s := &Set{pinCount: map[string]int{}, inScope: inScope}
	if c != nil {
		s.rules = c.Exclude
	}
	s.used = make([]bool, len(s.rules))
	s.ambiguous = make([]bool, len(s.rules))
	for _, e := range entries {
		s.pinCount[Pin(e)]++
	}
	for i, r := range s.rules {
		for _, p := range r.pins() {
			if s.pinCount[p] > 1 {
				s.ambiguous[i] = true
			}
		}
		for _, e := range entries {
			if len(r.Paths) > 0 && r.matchesPath(e.Unit.File) {
				s.used[i] = true // パスの除外は、単位に当たれば効いている
				break
			}
		}
	}
	return s
}

// Narrowed は、既定より狭い走査 (段・下限を変えた) であることを伝える。効いていない除外を数えなくなる。
func (s *Set) Narrowed() { s.narrowed = true }

// LimitToBase は gate のために、組・単位の除外を base にあった釘か新しく足された要素だけに効かせる。
func (s *Set) LimitToBase(base []*group.Entry, baseCfg *Config) {
	s.basePins = map[string]bool{}
	for _, e := range base {
		s.basePins[Pin(e)] = true
	}
	s.newElems = map[string]bool{}
	for _, el := range NewElements(baseCfg, &Config{Exclude: s.rules}) {
		s.newElems[el.Key] = true
	}
}

func (r Rule) pins() []string {
	if r.Unit != "" {
		return []string{r.Unit}
	}
	return r.Group
}

func (r Rule) matchesPath(file string) bool {
	for _, g := range r.Paths {
		if Match(g, file) {
			return true
		}
	}
	return false
}

// PathExcluded は、その単位がその段でパスの除外の下にあるか。除外の下の単位は組の相手・出どころには残し、
// 寄せる側に名指ししない (比べる相手から外すと、除外の下から外へ写した新しい写しが、相手がいないので通る)。
func (s *Set) PathExcluded(e *group.Entry, lv norm.Level) bool {
	for _, r := range s.rules {
		if len(r.Paths) > 0 && r.levels[lv] && r.matchesPath(e.Unit.File) {
			return true
		}
	}
	return false
}

// PathCovers は、段 lv の組のメンバーが全部パスの除外の下にあるか (その組は出さない)。
func (s *Set) PathCovers(lv norm.Level, members []*group.Entry) bool {
	for _, m := range members {
		if !s.PathExcluded(m, lv) {
			return false
		}
	}
	return len(members) > 0
}

// GroupExcluded は、段 lv の組のメンバーが全部 1 つの組の除外 (段が lv 以下) の釘に当たるか。1 つでも外れたメンバーがいれば出す。
func (s *Set) GroupExcluded(lv norm.Level, members []*group.Entry) bool {
	for i, r := range s.rules {
		if len(r.Group) == 0 || s.ambiguous[i] || r.level > lv {
			continue
		}
		pins := map[string]bool{}
		for _, p := range r.Group {
			pins[p] = true
		}
		all := true
		for _, m := range members {
			p := Pin(m)
			if !pins[p] || !s.known(p, "group:"+r.level.String()+":"+p) {
				all = false
				break
			}
		}
		if all {
			s.used[i] = true
			return true
		}
	}
	return false
}

// Allowed は、単位の除外 (許可) がその単位に効くか。
func (s *Set) Allowed(e *group.Entry) bool {
	p := Pin(e)
	for i, r := range s.rules {
		if r.Unit == p && !s.ambiguous[i] && s.known(p, "unit:"+p) {
			s.used[i] = true
			return true
		}
	}
	return false
}

func (s *Set) known(pin, elem string) bool {
	return s.basePins == nil || s.basePins[pin] || s.newElems[elem]
}

// ApplyGroups は組から、組の除外に当たる組を除く (dropNested の後で呼ぶ。先に除くと、除いた関数の中のクロージャの組が出てくる)。
func (s *Set) ApplyGroups(groups []group.Group) []group.Group {
	out := groups[:0:0]
	for _, g := range groups {
		if !s.PathCovers(g.Level, g.Members) && !s.GroupExcluded(g.Level, g.Members) {
			out = append(out, g)
		}
	}
	return out
}

// Stale は、何も消していない除外 (起点の外を指すものを除く)。寄せ終わった・改名した・本体を直した除外がここに出る。
// 単位の除外は、その単位がどれかの組にいれば効いている (report では印を付け、gate では寄せる側から外しうる)。
func (s *Set) Stale() []Rule {
	var out []Rule
	if s.narrowed {
		return out
	}
	for i, r := range s.rules {
		if !s.used[i] && !s.ambiguous[i] && s.ruleInScope(r) {
			out = append(out, r)
		}
	}
	return out
}

// Ambiguous は、釘が 2 つ以上の単位に当たるので効かせなかった除外。
func (s *Set) Ambiguous() []Rule {
	var out []Rule
	for i, r := range s.rules {
		if s.ambiguous[i] {
			out = append(out, r)
		}
	}
	return out
}

func (s *Set) ruleInScope(r Rule) bool {
	if s.inScope == nil {
		return true
	}
	if len(r.Paths) > 0 {
		return false // glob が起点のどこに当たるかは決められないので、起点を絞った走査では数えない
	}
	for _, p := range r.pins() {
		if file, _, ok := strings.Cut(p, "|"); !ok || !s.inScope(file) {
			return false
		}
	}
	return true
}

// Element は除外の要素 1 つ。Comment は、単位の直前のコメントから作った要素ならその場所 (`file:line`)。
type Element struct {
	Key     string `json:"element"`
	Comment string `json:"comment,omitempty"`
}

// NewElements は head の設定にあって base の設定に無い要素 (glob と段・組の段と釘・単位の釘の 1 つずつ)。
// 除外の単位でなく要素で比べるので、並べ替え・reason の直しは出ず、既存の除外に足した釘だけが出る。
// コメントの許可は本体を直すと釘が変わって新しい要素になるので、どのコメントかを場所で添える (付いて回った許可と新しく付けた許可を読み分けるため)。
func NewElements(base, head *Config) []Element {
	have := map[string]bool{}
	if base != nil {
		for _, r := range base.Exclude {
			for _, el := range r.elements() {
				have[el] = true
			}
		}
	}
	var out []Element
	seen := map[string]int{}
	if head != nil {
		for _, r := range head.Exclude {
			for _, el := range r.elements() {
				if have[el] {
					continue
				}
				if i, ok := seen[el]; ok {
					if out[i].Comment == "" {
						out[i].Comment = r.Comment
					}
					continue
				}
				seen[el] = len(out)
				out = append(out, Element{Key: el, Comment: r.Comment})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (r Rule) elements() []string {
	var out []string
	switch {
	case len(r.Paths) > 0:
		var lvs []string
		for _, lv := range norm.Levels {
			if r.levels[lv] {
				lvs = append(lvs, lv.String())
			}
		}
		for _, g := range r.Paths {
			out = append(out, "paths:"+g+" ("+strings.Join(lvs, ",")+")")
		}
	case len(r.Group) > 0:
		for _, p := range r.Group {
			out = append(out, "group:"+r.level.String()+":"+p)
		}
	case r.Unit != "":
		out = append(out, "unit:"+r.Unit)
	}
	return out
}
