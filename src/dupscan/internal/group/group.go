// Package group は、正規化したハッシュが同じ単位を段ごとに組 (Type-2) にまとめる。
package group

import (
	"sort"

	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// Entry は組分けの入力 (単位と正規化の結果)。
type Entry struct {
	Unit   *tree.Unit
	Result norm.Result
}

// Group は同じ段でハッシュが同じ単位の組。
type Group struct {
	Level     norm.Level
	Hash      string
	Partition string // "test" / "prod" / "mixed"
	Members   []*Entry
}

// Options は組分けの設定。
type Options struct {
	MinNodes int
	// MinClosureNodes はクロージャの下限。短いクロージャ (`{ Tag(name: $0) }`) はどこにでもあり、関数と同じ下限では組が雑音で埋まる。
	MinClosureNodes int
	Levels          []norm.Level
	// Skip は、その単位をその段で比べないか (除外の設定の、段を指定したパスの除外)。nil なら外さない。
	Skip func(*Entry, norm.Level) bool
}

// Eligible は、その単位をその段で比べるか (読めない・下限より小さい・L3 で自明・除外した段は比べない)。
func Eligible(e *Entry, lv norm.Level, opt Options) bool {
	if e.Unit.Unreadable || e.Result.Nodes < MinNodes(e, opt) {
		return false
	}
	if lv == norm.L3 && e.Result.L3Trivial {
		return false
	}
	return opt.Skip == nil || !opt.Skip(e, lv)
}

// Build は組を作る。細かい段の組とメンバーが同じ組は、粗い段では重ねて出さない
// (L1 で一致すれば L2・L3 でも必ず一致するので、同じ組が 3 回出るだけになる)。
func Build(entries []*Entry, opt Options) []Group {
	var groups []Group
	var seen []map[*Entry]bool
	// 細かい段から見る (渡された順のままだと、L3 を先に渡すと写しそのものが L3 の組として出る)。
	levels := append([]norm.Level(nil), opt.Levels...)
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	for _, lv := range levels {
		byHash := map[string][]*Entry{}
		for _, e := range entries {
			if !Eligible(e, lv, opt) {
				continue
			}
			h := e.Result.Hash[lv]
			byHash[h] = append(byHash[h], e)
		}
		for h, ms := range byHash {
			if len(ms) < 2 {
				continue
			}
			set := map[*Entry]bool{}
			for _, m := range ms {
				set[m] = true
			}
			if covered(set, seen) {
				continue
			}
			seen = append(seen, set)
			sortEntries(ms)
			groups = append(groups, Group{Level: lv, Hash: h, Partition: partition(ms), Members: ms})
		}
	}
	groups = dropNested(groups)
	sort.SliceStable(groups, func(i, j int) bool {
		gi, gj := groups[i], groups[j]
		if gi.Level != gj.Level {
			return gi.Level < gj.Level
		}
		return key(gi.Members[0]) < key(gj.Members[0])
	})
	return groups
}

func covered(set map[*Entry]bool, seen []map[*Entry]bool) bool {
	for _, s := range seen {
		if len(s) != len(set) {
			continue
		}
		eq := true
		for e := range set {
			if !s[e] {
				eq = false
				break
			}
		}
		if eq {
			return true
		}
	}
	return false
}

func partition(ms []*Entry) string {
	t, p := 0, 0
	for _, m := range ms {
		if m.Unit.Test {
			t++
		} else {
			p++
		}
	}
	switch {
	case p == 0:
		return "test"
	case t == 0:
		return "prod"
	}
	return "mixed"
}

func key(e *Entry) string { return e.Unit.Key }

func sortEntries(ms []*Entry) {
	sort.Slice(ms, func(i, j int) bool {
		a, b := ms[i].Unit, ms[j].Unit
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}

// MinNodes は単位の種類に応じた下限 (gate も同じ下限で数える)。
func MinNodes(e *Entry, opt Options) int {
	if e.Unit.Kind == tree.KindClosure && opt.MinClosureNodes > 0 {
		return opt.MinClosureNodes
	}
	return opt.MinNodes
}

// dropNested は、メンバー全部の囲む単位が、同じかより細かい段で同じ組に入っているクロージャの組を外す
// (写しの関数の中のクロージャは、外側の組を寄せれば一緒に片付くので、重ねて出すと組の数が膨らむだけ)。
// 囲む単位が粗い段でしか一致しないなら外さない (外側は寄せられるとは限らず、細かい段の写しの方が強い証拠)。
func dropNested(groups []Group) []Group {
	in := map[string]map[int]bool{} // 単位の鍵 → 入っている組の番号
	for i, g := range groups {
		for _, m := range g.Members {
			if in[m.Unit.Key] == nil {
				in[m.Unit.Key] = map[int]bool{}
			}
			in[m.Unit.Key][i] = true
		}
	}
	finer := func(i int, lv norm.Level) bool { return groups[i].Level <= lv }
	out := groups[:0:0]
	for _, g := range groups {
		if !nested(g, in, finer) {
			out = append(out, g)
		}
	}
	return out
}

func nested(g Group, in map[string]map[int]bool, finer func(int, norm.Level) bool) bool {
	var common map[int]bool
	seenOwner := map[string]bool{}
	for _, m := range g.Members {
		if m.Unit.Owner == "" || seenOwner[m.Unit.Owner] {
			return false // 1 つの単位の中に同じクロージャが 2 つある: 外側を寄せても残るので外さない
		}
		seenOwner[m.Unit.Owner] = true
		owners := in[m.Unit.Owner]
		if common == nil {
			common = map[int]bool{}
			for k := range owners {
				if finer(k, g.Level) {
					common[k] = true
				}
			}
			continue
		}
		for k := range common {
			if !owners[k] {
				delete(common, k)
			}
		}
	}
	return len(common) > 0
}
