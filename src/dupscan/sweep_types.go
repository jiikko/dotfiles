package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// sweepTypeCopyMin は、2 つの型を写しとみなすのに要る、相手と同じ組に入っている自分のメンバーの数 (両方の型で)。
// 1 つずつなら、メソッドを 1 つ写しただけで節 1・2 の組と同じことしか言えず、型で束ねる意味が無い。
const sweepTypeCopyMin = 2

// sweepTypeCopy は節 6 の候補 1 つ: メンバーを写し合っている型のつながり。
type sweepTypeCopy struct {
	types  []sweepTypeMembers
	weight int // 寄せたときに減る節の数の目安 (束ねた組ごとに 節 × (候補の中の型の数 − 1) の和)
}

type sweepTypeMembers struct {
	name    string
	file    string // 同じ形のメンバーのうち、最初のものの場所
	line    int
	members []string // 同じ形のメンバーの名前 (overload は `tableView ×3`)
}

// sweepTypeID は型の同一性。file でなく dir で持つ: Go の 1 つの型のメソッドは package (dir) の別のファイルにも書け、
// Swift の extension も `Foo+X.swift` のように同じ dir に分けるので、file で持つと 1 つの型が割れて取りこぼす。
// 別の dir の同名の型 (別の target の `MockPlayer`) は別の型のまま比べる。
type sweepTypeID struct{ dir, name string }

func (t sweepTypeID) less(o sweepTypeID) bool {
	if t.dir != o.dir {
		return t.dir < o.dir
	}
	return t.name < o.name
}

// sweepTypeCopies は、節 1・2 の組を型で束ね、メンバーを写し合っている型のつながりを返す。
//
// 束ねないもの (どれも、束ねると候補が写しでないもので埋まるか、同じ組を 2 度数える):
//   - L1m の組: 同じ組の中の名前違いのメンバー (`touchTag` ~ `touchDetail`) が全部対になり、候補を写しでないもので埋めた
//   - クロージャ: 大きいクロージャの組 1 つが、型の数の掛け算で対を増やした
//   - 型のメンバーが sweepBigGroupMin 人以上の組: `init` の羅列のような決まった形で、組の中の対の総当たりが人数の 2 乗で膨らむ (その中の小さい組は束ねる)
//   - 他の組の部分集合の組: 同じメンバーが L1 と L2 の両方の組に入ると、重みを 2 度数える
//
// protocol の準拠 (`tableView`) や `setUp` / `tearDown` のような決まった形は除かない (名前で除くと、同じ名前で中身の写しも消える)。
func sweepTypeCopies(groupSets ...[]group.Group) []sweepTypeCopy {
	typeOf := func(e *group.Entry) sweepTypeID { return sweepTypeID{path.Dir(e.Unit.File), e.Unit.Enclosing} }
	groups := sweepTypedGroups(groupSets)

	// 型の対ごとに、相手と同じ組に入っている自分のメンバー。下限は組の数でなくメンバーの数で見る:
	// 組の数は段の粒度で変わり (定数だけ違う 2 メソッドは L2 の組 1 つになる)、型を丸ごと写した対を落とす。
	type pair struct{ a, b sweepTypeID }
	sides := map[pair][2]map[*group.Entry]bool{}
	for _, ms := range groups {
		for i, x := range ms {
			for _, y := range ms[i+1:] {
				tx, ty := typeOf(x), typeOf(y)
				if tx == ty {
					continue
				}
				if ty.less(tx) {
					x, y, tx, ty = y, x, ty, tx
				}
				p := pair{tx, ty}
				if _, ok := sides[p]; !ok {
					sides[p] = [2]map[*group.Entry]bool{{}, {}}
				}
				sides[p][0][x], sides[p][1][y] = true, true
			}
		}
	}

	// 写しの対でつながる型を 1 つの候補にする。1 つの型は 1 つの候補にしか入らないので、組の重みを 2 つの候補に重ねない
	// (全員が互いに写しの集まりで分けると、2 つの集まりが型の対を共有したときに同じ組を両方で数えた)。
	parent := map[sweepTypeID]sweepTypeID{}
	var find func(sweepTypeID) sweepTypeID
	find = func(t sweepTypeID) sweepTypeID {
		p, ok := parent[t]
		if !ok || p == t {
			parent[t] = t
			return t
		}
		r := find(p)
		parent[t] = r
		return r
	}
	for p, sd := range sides {
		if len(sd[0]) >= sweepTypeCopyMin && len(sd[1]) >= sweepTypeCopyMin {
			ra, rb := find(p.a), find(p.b)
			if rb.less(ra) {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
	}

	byRoot := map[sweepTypeID]*sweepTypeCopy{}
	mine := map[sweepTypeID][]*group.Entry{}
	for _, ms := range groups {
		// 組のメンバーを候補ごとに分け、候補の 2 つ以上の型にまたがる分だけを、その型どうしの写しとして数える
		in := map[sweepTypeID][]*group.Entry{}
		types := map[sweepTypeID]map[sweepTypeID]bool{}
		for _, m := range ms {
			t := typeOf(m)
			if _, ok := parent[t]; !ok {
				continue
			}
			r := find(t)
			in[r] = append(in[r], m)
			if types[r] == nil {
				types[r] = map[sweepTypeID]bool{}
			}
			types[r][t] = true
		}
		for r, es := range in {
			if len(types[r]) < 2 {
				continue
			}
			if byRoot[r] == nil {
				byRoot[r] = &sweepTypeCopy{}
			}
			// メンバーの数でなく型の数で数える: 型を寄せても、1 つの型の中の写し (節 1・2 の所見) は減らない
			byRoot[r].weight += ms[0].Result.Nodes * (len(types[r]) - 1)
			for _, m := range es {
				mine[typeOf(m)] = append(mine[typeOf(m)], m)
			}
		}
	}
	for t, es := range mine {
		c := byRoot[find(t)]
		c.types = append(c.types, sweepTypeMemberLine(t.name, es))
	}

	out := make([]sweepTypeCopy, 0, len(byRoot))
	for _, c := range byRoot {
		sort.Slice(c.types, func(i, j int) bool {
			if c.types[i].file != c.types[j].file {
				return c.types[i].file < c.types[j].file
			}
			if c.types[i].line != c.types[j].line {
				return c.types[i].line < c.types[j].line
			}
			return c.types[i].name < c.types[j].name
		})
		out = append(out, *c)
	}
	label := func(c sweepTypeCopy) string {
		var b strings.Builder
		for _, t := range c.types {
			b.WriteString(t.file + "\x00" + t.name + "\x00")
		}
		return b.String()
	}
	// 型 1 つ増えるごとに減る節 (重み ÷ (型の数 − 1)) の大きい順。重みの和で並べると、`tableView` の準拠でつながった 7 つの型の
	// 決まった形が、2 つの型の丸写しより先に来た (flickorder-client)。
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].weight*(len(out[j].types)-1), out[j].weight*(len(out[i].types)-1)
		if a != b {
			return a > b
		}
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		return label(out[i]) < label(out[j])
	})
	return out
}

// sweepTypedGroups は、束ねる組のうち、型に属する名前つきのメンバー (2 人以上・sweepBigGroupMin 人未満) を返す。
// 他の組に含まれる組は除く (同じ集合なら先に出た方を残す)。
func sweepTypedGroups(groupSets [][]group.Group) [][]*group.Entry {
	type typed struct {
		members []*group.Entry
		keys    map[string]bool
	}
	var all []typed
	for _, gs := range groupSets {
		for _, g := range gs {
			if g.Level == norm.LM {
				continue
			}
			tg := typed{keys: map[string]bool{}}
			for _, m := range g.Members {
				if m.Unit.Kind != tree.KindClosure && m.Unit.Enclosing != "" {
					tg.members = append(tg.members, m)
					tg.keys[m.Unit.Key] = true
				}
			}
			if len(tg.members) >= 2 && len(tg.members) < sweepBigGroupMin {
				all = append(all, tg)
			}
		}
	}
	var out [][]*group.Entry
	for i, g := range all {
		covered := false
		for j, o := range all {
			if i == j || len(o.keys) < len(g.keys) || (len(o.keys) == len(g.keys) && j > i) {
				continue
			}
			in := true
			for k := range g.keys {
				in = in && o.keys[k]
			}
			if covered = in; covered {
				break
			}
		}
		// 大きい群の中の小さい組 (L2 の群の中の L1 の組) は束ねる。大きい群に含む側を任せると、片方のメソッドが
		// 決まった形の群に入るだけで、もう片方と合わせた本物の丸写しが下限を割って消えた。
		if !covered {
			out = append(out, g.members)
		}
	}
	return out
}

// sweepTypeMemberLine は、候補の中の型 1 つの行 (最初のメンバーの場所と、同じ形のメンバーの名前)。
func sweepTypeMemberLine(name string, es []*group.Entry) sweepTypeMembers {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Unit.File != es[j].Unit.File {
			return es[i].Unit.File < es[j].Unit.File
		}
		return es[i].Unit.Line < es[j].Unit.Line
	})
	tm := sweepTypeMembers{name: name, file: es[0].Unit.File, line: es[0].Unit.Line}
	seen, count := map[*group.Entry]bool{}, map[string]int{}
	for _, e := range es {
		if !seen[e] {
			seen[e] = true
			count[e.Unit.Name]++
		}
	}
	for n, k := range count {
		if k > 1 {
			n = fmt.Sprintf("%s ×%d", n, k)
		}
		tm.members = append(tm.members, n)
	}
	sort.Strings(tm.members)
	return tm
}

func sweepTypeCopyText(w *lineWriter, cs []sweepTypeCopy, limit int) {
	if len(cs) == 0 {
		w.printf("なし\n")
		return
	}
	for i, c := range cs {
		if limit > 0 && i == limit {
			w.printf("… 他 %d 件 (-limit 0 で全部)\n", len(cs)-limit)
			break
		}
		w.printf("型 ×%d 節 %d\n", len(c.types), c.weight)
		for _, t := range c.types {
			w.printf("  %s:%d %s (%s)\n", t.file, t.line, t.name, strings.Join(t.members, ", "))
		}
	}
}
