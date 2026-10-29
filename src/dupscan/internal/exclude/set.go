package exclude

import (
	"fmt"
	"maps"
	"slices"
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
	pinKeys   map[string]int // ハッシュを除いた鍵ごとの単位数 (理由と候補の一意性を確かめる)
	pinCount  map[string]int
	entries   []*group.Entry
	groups    []group.Group
	// basePins は gate のときの base の単位の釘。組・単位の除外は、base に同じ釘の単位があったときか、
	// その釘が新しく足された要素のときだけ効く (消えた写しの釘の場所に後から同じ写しを置いても覆わない)。nil なら制限しない (report)。
	basePins map[string]int // 釘ごとの base の単位の数 (同じ関数に並んだ同じ本体のクロージャは同じ釘で数が 2 以上になる)
	newElems map[string]bool
	// baseAllow は base にあった allow-group の出どころ (Rule.allowID)。効いていない理由を分けるのに使う
	// (覆うかどうかは NewElements が決める: 変わっていない許可の要素は新しく足された要素にしない)。
	baseAllow map[string]bool
	inScope   func(file string) bool
	// narrowed は既定より狭い走査 (段・下限を変えた) か。狭い走査では組が作られないだけの除外を「効いていない」と言えない。
	narrowed bool
	// maxLevel は走査で組を作った段のうち最も粗い段。それより粗い段の組の除外は、組が作られないだけなので「効いていない」に数えない
	// (L3 の組の除外は、L3 を見ない既定の走査では何も消さない)。0 なら制限しない。
	maxLevel norm.Level
}

// New は head の単位 (path の除外で外す前の全部) に対する除外を作る。c が nil なら除外は無い。
// inScope は起点の中のファイルか (nil なら repo 全体)。起点の外を指す除外は「効いていない」に数えない。
func New(c *Config, entries []*group.Entry, inScope func(string) bool) *Set {
	s := &Set{pinCount: map[string]int{}, pinKeys: map[string]int{}, inScope: inScope, entries: entries}
	if c != nil {
		s.rules = c.Exclude
	}
	s.used = make([]bool, len(s.rules))
	s.ambiguous = make([]bool, len(s.rules))
	for _, e := range entries {
		s.pinCount[Pin(e)]++
		s.pinKeys[pinKey(Pin(e))]++
	}
	for i, r := range s.rules {
		// 釘は除外に書いた回数まで当たってよい。同じ単位の中で同じ本体のクロージャが並ぶと、順番を入れない釘はどれも同じになるので、
		// 組の除外にはその釘が人数分書かれる (報告の除外の 1 行のとおり)。当たる単位が書いた回数より多いときだけ名指しできない
		// (後から同じ本体のクロージャが増えた・別の単位が同じ釘になった) として効かせない。
		if r.groupMiss != "" {
			continue // 何も消さないので、名指しできるかは問わない (効いていない除外に人数の食い違いとして出す)
		}
		want := map[string]int{}
		for _, p := range r.pins() {
			want[p]++
		}
		for p, n := range want {
			if s.pinCount[p] > n {
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

// Scanned は、組を作った段を伝える。それより粗い段の組の除外を「効いていない」に数えなくなる。
func (s *Set) Scanned(levels []norm.Level) {
	for _, lv := range levels {
		s.maxLevel = max(s.maxLevel, lv)
	}
}

// LimitToBase は gate のために、組・単位の除外を base にあった釘か新しく足された要素だけに効かせる。
func (s *Set) LimitToBase(base []*group.Entry, baseCfg *Config) {
	s.basePins = map[string]int{}
	for _, e := range base {
		s.basePins[Pin(e)]++
	}
	s.newElems = map[string]bool{}
	for _, el := range NewElements(baseCfg, &Config{Exclude: s.rules}) {
		s.newElems[el.Key] = true
	}
	s.baseAllow = allowIDs(baseCfg)
}

// allowIDs は設定の allow-group の出どころ (Rule.allowID) の集合。
func allowIDs(c *Config) map[string]bool {
	out := map[string]bool{}
	if c != nil {
		for _, r := range c.Exclude {
			if r.allowID != "" {
				out[r.allowID] = true
			}
		}
	}
	return out
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

// PresetCovers は、段 lv の組をどれかのプリセットが外すか。パスの除外と同じく、書いた後にできた単位にも効く (gate の base の釘の制限を受けない)。
func (s *Set) PresetCovers(lv norm.Level, members []*group.Entry) bool {
	for i, r := range s.rules {
		if r.Preset != "" && presetCovers(r.Preset, lv, members) {
			s.used[i] = true
			return true
		}
	}
	return false
}

// GroupExcluded は、段 lv の組のメンバーが全部 1 つの組の除外 (段が lv 以下) の釘に当たるか。1 つでも外れたメンバーがいれば出す。
func (s *Set) GroupExcluded(lv norm.Level, members []*group.Entry) bool {
	for i, r := range s.rules {
		if len(r.Group) == 0 || s.ambiguous[i] || r.level > lv || r.groupMiss != "" {
			continue
		}
		pins := map[string]bool{}
		for _, p := range r.Group {
			pins[p] = true
		}
		// 同じ釘のメンバーの k 人目は、base にその釘が k 個以上あったか、k 人目の要素が新しく足されたときだけ覆う
		// (1 つ消した後に同じ写しを足し直すと、base の個数を超えた分が新しい写しとして出る)。
		all := true
		seen := map[string]int{}
		for _, m := range members {
			p := Pin(m)
			seen[p]++
			if !pins[p] || !s.known(p, groupElement(r.level, p, seen[p]), seen[p]) {
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
		if r.Unit == p && !s.ambiguous[i] && s.known(p, "unit:"+p, 1) {
			s.used[i] = true
			return true
		}
	}
	return false
}

func (s *Set) known(pin, elem string, k int) bool {
	return s.basePins == nil || s.basePins[pin] >= k || s.newElems[elem]
}

// ApplyGroups は組から、組の除外に当たる組を除く (dropNested の後で呼ぶ。先に除くと、除いた関数の中のクロージャの組が出てくる)。
func (s *Set) ApplyGroups(groups []group.Group) []group.Group {
	s.groups = groups
	out := groups[:0:0]
	for _, g := range groups {
		if !s.PathCovers(g.Level, g.Members) && !s.PresetCovers(g.Level, g.Members) && !s.GroupExcluded(g.Level, g.Members) {
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
		if len(r.Group) > 0 && s.maxLevel != 0 && r.level > s.maxLevel {
			continue
		}
		if r.Preset != "" && s.maxLevel != 0 && s.maxLevel < norm.L2 {
			continue // L2 の組を作らない走査では、プリセットは何も消せない
		}
		if !s.used[i] && !s.ambiguous[i] && s.ruleInScope(r) {
			out = append(out, r)
		}
	}
	return out
}

// pinKey は釘から本体のハッシュ (末尾の `@…`) を除いた鍵 (ファイル・型・署名)。
// 最後の `@` で切る: ハッシュは末尾に付くので、署名の `@escaping` や、パスの `B@v1.swift` の `@` で鍵が途中で切れない。
func pinKey(pin string) string {
	if i := strings.LastIndex(pin, "@"); i >= 0 {
		return pin[:i]
	}
	return pin
}

// StaleWhy は、効いていない除外が効かない理由を分ける。
//   - "group-count": `dupscan:allow-group` の人数と、同じ本体の単位の数が合わない (Rule.GroupMiss に段ごとの数)
//   - "allow-unchanged": gate で、base から変わっていない allow-group が base に無かった単位を指している (base にあった釘しか覆わない)
//   - "missing": 釘が指す単位のどれかが無い (削除・改名・起点の外)。1 つでも無ければこちら (除外を直すか消す)
//   - "duplicate": 同じ釘の除外が別にあって、そちらが効いている (重複を消す)
//   - "ungrouped": 単位の除外で、釘の単位が釘の本体のまま在る。または組の除外で、釘の単位が全部釘の本体のまま在り、その段で同じ本体の単位が
//     釘の数だけ (増えていない)。本体も顔ぶれも変わっていないので、効かないのは組の側の理由
//     (組に入らない: 節が下限未満・本体が他と一致しない / 別の除外が先に組を消した / gate の base の制約)。どれかは区別しない。
//     sweep の節 2・3 は組にならない単位の許可も読むので、「外してよい」とは言わない
//   - "hash": それ以外。同じ鍵 (ファイル・型・署名) の単位は全部ある。本体が変わった・組の顔ぶれが変わった等で、原因は特定しない。
//     鍵だけでは、削除したクロージャと、残った外側の関数も区別できない (Pin はクロージャの序数を削る)
//   - "": 単位を指さない除外 (glob・プリセット) か、釘が無い
func (s *Set) StaleWhy(r Rule) string {
	pins := r.pins()
	if len(pins) == 0 {
		return ""
	}
	if r.groupMiss != "" {
		return "group-count"
	}
	if s.baseAllow[r.allowID] {
		for _, p := range r.Group {
			if s.basePins[p] == 0 {
				return "allow-unchanged"
			}
		}
	}
	for _, p := range pins {
		if s.pinKeys[pinKey(p)] == 0 {
			return "missing"
		}
	}
	for i, o := range s.rules {
		if s.used[i] && o.Unit == r.Unit && o.Level == r.Level && samePins(o.Group, r.Group) {
			return "duplicate"
		}
	}
	if r.Unit != "" && s.pinCount[r.Unit] > 0 || len(r.Group) > 0 && s.sameMembers(r) {
		return "ungrouped"
	}
	return "hash"
}

// sameMembers は、組の除外の釘の単位が全部 (書いた回数ちょうど) 在り、組の段で本体が同じで、同じ本体の単位がほかに無いか。
func (s *Set) sameMembers(r Rule) bool {
	want := map[string]int{}
	for _, p := range r.Group {
		want[p]++
	}
	hash := ""
	for _, e := range s.entries {
		if want[Pin(e)] > 0 {
			hash = e.Result.Hash[r.level]
			break
		}
	}
	got := map[string]int{}
	for _, e := range s.entries {
		if e.Result.Hash[r.level] == hash {
			got[Pin(e)]++
		}
	}
	return hash != "" && maps.Equal(want, got)
}

// StaleGroupCandidate は同じ段・同じ鍵の組を再審査の候補にする。釘はクロージャの序数を
// 持たないので、鍵が複数の単位を指す場合は候補を出さない。除外を更新してよいとは判定しない。
func (s *Set) StaleGroupCandidate(r Rule) string {
	if len(r.Group) < 2 || s.StaleWhy(r) != "hash" {
		return ""
	}
	keys := make([]string, 0, len(r.Group))
	seen := map[string]bool{}
	for _, p := range r.Group {
		key := pinKey(p)
		if s.pinKeys[key] != 1 || seen[key] {
			return ""
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidate := ""
	for _, g := range s.groups {
		if g.Level != r.level || len(g.Members) != len(keys) {
			continue
		}
		current := make([]string, 0, len(g.Members))
		pins := make([]string, 0, len(g.Members))
		for _, m := range g.Members {
			pin := Pin(m)
			pins = append(pins, pin)
			current = append(current, pinKey(pin))
		}
		sort.Strings(current)
		if slices.Equal(keys, current) && !samePins(r.Group, pins) {
			if candidate != "" {
				return ""
			}
			candidate = GroupSnippet(g.Level, g.Members)
		}
	}
	return candidate
}

// samePins は組の釘を並びによらず比べる (組の除外は釘を集合として当てる)。
func samePins(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
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
	if len(r.Paths) > 0 || r.Preset != "" {
		return false // glob・形が起点のどこに当たるかは決められないので、起点を絞った走査では数えない
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
	// base から変わっていない allow-group (コメントの中身が base のどれかと同じ) の要素は、新しく足された要素にしない。
	// その許可は人数だけで顔ぶれを決め直すので、後から足した写しで人数が合うと新しい写しを指す。コメントは diff に出ないので、
	// 新しい要素として効かせると人の目に届かないまま写しを覆う (gate の Set.known は新しく足された要素なら効かせる)。
	unchanged := allowIDs(base)
	if head != nil {
		for _, r := range head.Exclude {
			if unchanged[r.allowID] {
				continue
			}
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
	case r.groupMiss != "":
		// 人数の合わない allow-group は何も消さないので、要素も持たない (「新しく足された除外」に数えない)
	case len(r.Group) > 0:
		n := map[string]int{}
		for _, p := range r.Group {
			n[p]++
			out = append(out, groupElement(r.level, p, n[p]))
		}
		// 同じ釘を 2 回以上書いた除外は、釘と回数の組み合わせそのものも要素にする。効いていなかった (人数の足りない) 除外を 1 行に合流すると
		// 釘ごとの要素の集合は変わらないまま組を消せるので、組み合わせが新しければ「新しく足された除外」に出す。
		if len(n) < len(r.Group) {
			var parts []string
			for p, c := range n {
				parts = append(parts, fmt.Sprintf("%s×%d", p, c))
			}
			sort.Strings(parts)
			// 釘の要素 (group:…) と別の名前空間にする: 同じ名前空間だと、組み合わせの文字列を釘として先に除外へ書いておけば合流を隠せる。
			// 釘を削って除外を狭めても組み合わせが変わるので新しい要素に出る (広げたか狭めたかは要素の集合では区別しない。止める側に倒す)。
			out = append(out, "group-pins:"+r.level.String()+":["+strings.Join(parts, ", ")+"]")
		}
	case r.Unit != "":
		out = append(out, "unit:"+r.Unit)
	case r.Preset != "":
		out = append(out, "preset:"+r.Preset)
	}
	return out
}

// groupElement は組の除外の要素。同じ釘を 2 回以上書いた (同じ関数に並んだ同じ本体のクロージャ) ときは、何人目かを要素に入れる。
// 入れないと、釘を書き足して許可の人数を増やしても「新しく足された除外」に出ない。
func groupElement(lv norm.Level, pin string, k int) string {
	e := "group:" + lv.String() + ":" + pin
	if k > 1 {
		e += fmt.Sprintf(" (%d つ目)", k)
	}
	return e
}
