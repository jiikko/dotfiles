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
}

// Build は組を作る。細かい段の組とメンバーが同じ組は、粗い段では重ねて出さない
// (L1 で一致すれば L2・L3 でも必ず一致するので、同じ組が 3 回出るだけになる)。
func Build(entries []*Entry, opt Options) []Group {
	var groups []Group
	var seen []map[*Entry]bool
	for _, lv := range opt.Levels {
		byHash := map[string][]*Entry{}
		for _, e := range entries {
			if e.Unit.Unreadable || e.Result.Nodes < minNodes(e, opt) {
				continue
			}
			if lv == norm.L3 && e.Result.L3Trivial {
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

func minNodes(e *Entry, opt Options) int {
	if e.Unit.Kind == tree.KindClosure && opt.MinClosureNodes > 0 {
		return opt.MinClosureNodes
	}
	return opt.MinNodes
}

// dropNested は、メンバー全部の囲む単位が既に同じ組に入っているクロージャの組を外す
// (写しの関数の中のクロージャは、外側の組を寄せれば一緒に片付くので、重ねて出すと組の数が膨らむだけ)。
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
	out := groups[:0:0]
	for _, g := range groups {
		if !nested(g, in) {
			out = append(out, g)
		}
	}
	return out
}

func nested(g Group, in map[string]map[int]bool) bool {
	var common map[int]bool
	for _, m := range g.Members {
		if m.Unit.Owner == "" {
			return false
		}
		owners := in[m.Unit.Owner]
		if common == nil {
			common = map[int]bool{}
			for k := range owners {
				common[k] = true
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
