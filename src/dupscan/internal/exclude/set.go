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
	}
	return s
}

// LimitToBase は gate のために、組・単位の除外を base にあった釘か新しく足された要素だけに効かせる。
func (s *Set) LimitToBase(base []*group.Entry, baseCfg *Config) {
	s.basePins = map[string]bool{}
	for _, e := range base {
		s.basePins[Pin(e)] = true
	}
	s.newElems = map[string]bool{}
	for _, el := range NewElements(baseCfg, &Config{Exclude: s.rules}) {
		s.newElems[el] = true
	}
}

func (r Rule) pins() []string {
	if r.Unit != "" {
		return []string{r.Unit}
	}
	return r.Group
}

// Filter は、全部の段で外すパスの除外に当たる単位を除く (base と head の両方に掛け、出どころにも使わない)。
func (s *Set) Filter(entries []*group.Entry) (kept, dropped []*group.Entry) {
	for _, e := range entries {
		drop := false
		for i, r := range s.rules {
			if len(r.Paths) > 0 && r.matchesPath(e.Unit.File) {
				s.used[i] = true
				if len(r.levels) == len(norm.Levels) {
					drop = true
				}
			}
		}
		if drop {
			dropped = append(dropped, e)
		} else {
			kept = append(kept, e)
		}
	}
	return kept, dropped
}

func (r Rule) matchesPath(file string) bool {
	for _, g := range r.Paths {
		if Match(g, file) {
			return true
		}
	}
	return false
}

// Skip は、その単位をその段で比べないか (段を指定したパスの除外)。
func (s *Set) Skip(e *group.Entry, lv norm.Level) bool {
	for _, r := range s.rules {
		if len(r.Paths) > 0 && r.levels[lv] && r.matchesPath(e.Unit.File) {
			return true
		}
	}
	return false
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
		if !s.GroupExcluded(g.Level, g.Members) {
			out = append(out, g)
		}
	}
	return out
}

// Stale は、何も消していない除外 (起点の外を指すものを除く)。寄せ終わった・改名した・本体を直した除外がここに出る。
func (s *Set) Stale() []Rule {
	var out []Rule
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

// NewElements は head の設定にあって base の設定に無い要素 (glob と段・組の段と釘・単位の釘の 1 つずつ)。
// 除外の単位でなく要素で比べるので、並べ替え・reason の直しは出ず、既存の除外に足した釘だけが出る。
func NewElements(base, head *Config) []string {
	have := map[string]bool{}
	if base != nil {
		for _, r := range base.Exclude {
			for _, el := range r.elements() {
				have[el] = true
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	if head != nil {
		for _, r := range head.Exclude {
			for _, el := range r.elements() {
				if !have[el] && !seen[el] {
					seen[el] = true
					out = append(out, el)
				}
			}
		}
	}
	sort.Strings(out)
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
