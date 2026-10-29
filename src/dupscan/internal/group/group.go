// Package group は、正規化したハッシュが同じ単位を段ごとに組 (Type-2) にまとめる。
package group

import (
	"fmt"
	"slices"
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

// Eligible は、その単位をその段で比べるか (読めない・下限より小さい・L3 で自明は比べない)。
func Eligible(e *Entry, lv norm.Level, opt Options) bool { return Skip(e, lv, opt) == "" }

// Skip は、その単位をその段で比べない理由 (比べるなら空)。
func Skip(e *Entry, lv norm.Level, opt Options) string {
	switch {
	case e.Unit.Unreadable:
		return "読めない構文を含む"
	case e.Result.Nodes < MinNodes(e, opt):
		return fmt.Sprintf("節が %d で下限 %d より少ない", e.Result.Nodes, MinNodes(e, opt))
	case lv == norm.L3 && e.Result.L3Trivial:
		return "本体が局所の名前を参照しないので、L3 では値の位置がどれも同じ記号に畳まれる (同じ形の呼び出しならどれも一致する)"
	}
	return ""
}

// Match は、ある単位に近い単位 1 つと、一致した最も細かい段。
type Match struct {
	Entry *Entry
	Level norm.Level
	// Folded は、局所の名前を参照しない (L3Trivial の) 単位どうしの L3 の一致 (中身を見た一致ではない。Build はこれを組にしない)。
	Folded bool
}

// Near は target と同じ段のハッシュを持つ他の単位を、一致した最も細かい段で返す。比べるかは Build と同じ Eligible で決める。
// Build と違い、被った組・入れ子の組を外さない (指した単位がクロージャでも、その一致を出す)。
func Near(target *Entry, entries []*Entry, opt Options) []Match {
	levels := slices.Sorted(slices.Values(opt.Levels))
	var out []Match
	for _, e := range entries {
		if e == target {
			continue
		}
		found := false
		for _, lv := range levels {
			if Eligible(target, lv, opt) && Eligible(e, lv, opt) && e.Result.Hash[lv] == target.Result.Hash[lv] {
				out = append(out, Match{Entry: e, Level: lv})
				found = true
				break
			}
		}
		if !found && slices.Contains(levels, norm.L3) && folded(target, opt) && folded(e, opt) && e.Result.Hash[norm.L3] == target.Result.Hash[norm.L3] {
			out = append(out, Match{Entry: e, Level: norm.L3, Folded: true})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Folded != out[j].Folded {
			return !out[i].Folded
		}
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		return key(out[i].Entry) < key(out[j].Entry)
	})
	return out
}

// folded は、L3 で畳まれたことだけが理由で比べない単位か。
func folded(e *Entry, opt Options) bool {
	return e.Result.L3Trivial && !e.Unit.Unreadable && e.Result.Nodes >= MinNodes(e, opt)
}

// Build は組を作る。細かい段の組とメンバーが同じ組は、粗い段では重ねて出さない
// (L1 で一致すれば L2・L3 でも必ず一致するので、同じ組が 3 回出るだけになる)。
func Build(entries []*Entry, opt Options) []Group {
	var groups []Group
	var seen []map[*Entry]bool
	// 細かい段から見る (渡された順のままだと、L3 を先に渡すと写しそのものが L3 の組として出る)。
	levels := slices.Sorted(slices.Values(opt.Levels))
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
			set := MemberSet(ms)
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
		if len(s) == len(set) && Subset(set, s) {
			return true
		}
	}
	return false
}

// MemberSet は組のメンバーの集合。
func MemberSet(members []*Entry) map[*Entry]bool {
	set := make(map[*Entry]bool, len(members))
	for _, m := range members {
		set[m] = true
	}
	return set
}

// Subset は、a のメンバーが全員 b に入っているか。組が別の組に含まれるか・同じ集合かの判定はこれを通す
// (同じ entries から作った組どうしは、同じ単位を同じ *Entry で持つ)。
func Subset(a, b map[*Entry]bool) bool {
	if len(a) > len(b) {
		return false
	}
	for e := range a {
		if !b[e] {
			return false
		}
	}
	return true
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

// CountBelowMin は節が下限に満たず、どの段でも比べない単位の数 (読めない構文の単位は別に数えるので含めない)。
// 「重複なし」が、比べていない小さい単位を黙らせないために report が出す。
func CountBelowMin(entries []*Entry, opt Options) int {
	n := 0
	for _, e := range entries {
		if !e.Unit.Unreadable && e.Result.Nodes < MinNodes(e, opt) {
			n++
		}
	}
	return n
}

// CountBelowMinGroups は、下限に満たない単位だけでできる組 (下限を floor まで下げたときにだけ見える写し) の数。
// 「重複なし」の注記に添える手がかり (除外の設定は見ない数。見るのは -min-nodes を下げて回し直したとき)。
func CountBelowMinGroups(entries []*Entry, opt Options, floor int) int {
	low := Options{MinNodes: floor, MinClosureNodes: floor, Levels: opt.Levels}
	n := 0
	for _, g := range Build(entries, low) {
		only := true
		for _, m := range g.Members {
			only = only && m.Result.Nodes < MinNodes(m, opt)
		}
		if only {
			n++
		}
	}
	return n
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
