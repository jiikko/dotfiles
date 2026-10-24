// Package gate は、base から head までに新しく増えた重複を決める (make test・CI で回す dupscan gate の判定)。
//
// 変わった単位 (鍵が base に無いか、L1 のハッシュが base と違う単位) ごとに、細かい段から見て最初に他の単位と一致した段の組を取る。
// 組のメンバーごとに「出どころ」(base の側の相手が、その段で持っていたハッシュ) を引き、組の大きさが同じ出どころから来た
// メンバーの最大の数より大きいときだけ違反にする。
//   - 移動・改名・整形・既存の写しを寄せる作業・入れ替え: 出どころが同じなので違反にしない
//   - 既存の写し 2 つに同じ修正を入れる: 2 つとも base で同じ組だったので違反にしない
//   - 新しい写し (出どころの無い単位・別の組から来た単位) が組に入る: 違反
//
// base の側の相手は、同じ鍵の単位か、鍵が消えた base の単位のうち L1 のハッシュが同じもの (移動・改名) を 1 対 1 で当てる。
package gate

import (
	"sort"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// Options は判定の設定。
type Options struct {
	group.Options
	// Fail は止める段。粗い段を止めるなら、それより細かい段の一致も止める (L1 で一致すれば L2・L3 でも一致するため)。
	Fail map[norm.Level]bool
}

// Violation は新しく増えた重複 1 つ (1 つの組)。
type Violation struct {
	Level norm.Level
	// New は組に新しく入った単位 (寄せる側)。
	New []*group.Entry
	// Same は組に前からいた単位 (寄せる先の候補)。
	Same []*group.Entry
	Fail bool
}

// Result は判定の結果。
type Result struct {
	Changed    int // 変わった単位の数
	Violations []Violation
	Unreadable []*group.Entry // 変わった単位のうち読めない構文を含むもの (確かめられなかった)
}

// Failed は止める段の違反があるか。
func (r Result) Failed() bool {
	for _, v := range r.Violations {
		if v.Fail {
			return true
		}
	}
	return false
}

// Judge は base と head の単位から違反を決める。
func Judge(base, head []*group.Entry, opt Options) Result {
	levels := append([]norm.Level(nil), opt.Levels...)
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	origin := origins(base, head)

	groups := map[norm.Level]map[string][]*group.Entry{}
	for _, lv := range levels {
		groups[lv] = map[string][]*group.Entry{}
		for _, e := range head {
			if eligible(e, lv, opt.Options) {
				groups[lv][e.Result.Hash[lv]] = append(groups[lv][e.Result.Hash[lv]], e)
			}
		}
	}

	var r Result
	seen := map[string]bool{} // 段 + ハッシュ。組ごとに 1 回だけ判定する
	for _, e := range head {
		if b := origin[e]; b != nil && b.Unit.Key == e.Unit.Key && b.Result.Hash[norm.L1] == e.Result.Hash[norm.L1] {
			continue // 変わっていない
		}
		r.Changed++
		if e.Unit.Unreadable {
			r.Unreadable = append(r.Unreadable, e)
			continue
		}
		for _, lv := range levels {
			if !eligible(e, lv, opt.Options) {
				continue
			}
			h := e.Result.Hash[lv]
			g := groups[lv][h]
			if len(g) < 2 {
				continue // この段では他と一致しない。粗い段を見る
			}
			if k := lv.String() + h; !seen[k] {
				seen[k] = true
				if v, ok := judgeGroup(lv, h, g, origin, opt); ok {
					r.Violations = append(r.Violations, v)
				}
			}
			break // 最初に一致した段だけで判定する
		}
	}
	sort.SliceStable(r.Violations, func(i, j int) bool {
		a, b := r.Violations[i].New[0].Unit, r.Violations[j].New[0].Unit
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return r
}

// judgeGroup は組が base より増えたかを、メンバーの出どころで決める。
func judgeGroup(lv norm.Level, h string, g []*group.Entry, origin map[*group.Entry]*group.Entry, opt Options) (Violation, bool) {
	buckets := map[string]int{}
	from := map[*group.Entry]string{}
	for _, m := range g {
		key := "new:" + m.Unit.Key // 出どころの無い単位は、それぞれ別
		if b := origin[m]; b != nil {
			key = "base:" + b.Result.Hash[lv]
		}
		from[m] = key
		buckets[key]++
	}
	// 一番多い出どころを「前からいた」側にする。同じ数なら、この組の形から来た出どころ → base から来た出どころ → 名前の順
	// (map の順で結果が揺れないように)。
	rank := func(k string) int {
		switch {
		case k == "base:"+h:
			return 0
		case len(k) > 5 && k[:5] == "base:":
			return 1
		}
		return 2
	}
	most, mostKey := 0, ""
	for k, n := range buckets {
		if n > most || n == most && (rank(k) < rank(mostKey) || rank(k) == rank(mostKey) && k < mostKey) {
			most, mostKey = n, k
		}
	}
	if len(g) <= most {
		return Violation{}, false
	}
	v := Violation{Level: lv, Fail: failAt(lv, opt.Fail)}
	for _, m := range g {
		if from[m] == mostKey {
			v.Same = append(v.Same, m)
		} else {
			v.New = append(v.New, m)
		}
	}
	return v, true
}

// origins は head の単位ごとに base の側の相手を当てる: 同じ鍵の単位、無ければ鍵が消えた base の単位のうち
// L1 のハッシュが同じもの (移動・改名) を 1 対 1 で。
func origins(base, head []*group.Entry) map[*group.Entry]*group.Entry {
	byKey := map[string]*group.Entry{}
	for _, b := range base {
		byKey[b.Unit.Key] = b
	}
	headKeys := map[string]bool{}
	for _, e := range head {
		headKeys[e.Unit.Key] = true
	}
	gone := map[string][]*group.Entry{} // L1 のハッシュ → 鍵が消えた base の単位
	for _, b := range base {
		if !headKeys[b.Unit.Key] {
			gone[b.Result.Hash[norm.L1]] = append(gone[b.Result.Hash[norm.L1]], b)
		}
	}
	out := map[*group.Entry]*group.Entry{}
	for _, e := range head {
		if b, ok := byKey[e.Unit.Key]; ok {
			out[e] = b
			continue
		}
		h := e.Result.Hash[norm.L1]
		if bs := gone[h]; len(bs) > 0 {
			out[e] = bs[0]
			gone[h] = bs[1:]
		}
	}
	return out
}

func failAt(lv norm.Level, fail map[norm.Level]bool) bool {
	for l, on := range fail {
		if on && l >= lv {
			return true
		}
	}
	return false
}

func eligible(e *group.Entry, lv norm.Level, opt group.Options) bool {
	if e.Unit.Unreadable || e.Result.Nodes < group.MinNodes(e, opt) {
		return false
	}
	return lv != norm.L3 || !e.Result.L3Trivial
}
