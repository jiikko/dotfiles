// Package gate は、base から head までに新しく増えた重複を決める (make test・CI で回す dupscan gate の判定)。
//
// 変わった単位 (鍵が base に無いか、L1 のハッシュが base と違う単位) ごとに、細かい段から見て最初に他の単位と一致した段で、
// その形の単位の数が base より増えていれば違反にする。数で見るので、移動・改名・整形・既存の写しを寄せる作業では違反にならない。
// 同じ組の古い写しを 1 つ消して新しい写しを 1 つ足す入れ替えは、数が増えないので違反にしない (重複の量は増えていない)。
package gate

import (
	"sort"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// Options は判定の設定。
type Options struct {
	group.Options
	// Fail は止める段。それ以外の段の違反は報告だけ。
	Fail map[norm.Level]bool
}

// Violation は新しく増えた重複 1 つ。
type Violation struct {
	Level norm.Level
	// Changed は変わった単位 (寄せる側)。
	Changed *group.Entry
	// Same は同じ形の他の単位 (寄せる先の候補)。
	Same []*group.Entry
	// Fail は止める段の違反か。
	Fail bool
}

// Result は判定の結果。
type Result struct {
	Changed    int // 変わった単位の数
	Violations []Violation
	Unreadable []*group.Entry // 変わったファイルにある読めない構文の単位 (確かめられなかった)
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

	baseByKey := map[string]*group.Entry{}
	for _, e := range base {
		baseByKey[e.Unit.Key] = e
	}
	count := func(es []*group.Entry, lv norm.Level) map[string][]*group.Entry {
		m := map[string][]*group.Entry{}
		for _, e := range es {
			if eligible(e, lv, opt.Options) {
				m[e.Result.Hash[lv]] = append(m[e.Result.Hash[lv]], e)
			}
		}
		return m
	}
	baseCount := map[norm.Level]map[string][]*group.Entry{}
	headCount := map[norm.Level]map[string][]*group.Entry{}
	for _, lv := range levels {
		baseCount[lv] = count(base, lv)
		headCount[lv] = count(head, lv)
	}

	var r Result
	reported := map[string]bool{} // 段 + 形のハッシュ。同じ組の新しい単位が 2 つあっても違反は 1 件にする
	for _, e := range head {
		b, ok := baseByKey[e.Unit.Key]
		if ok && b.Result.Hash[norm.L1] == e.Result.Hash[norm.L1] {
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
			same := headCount[lv][h]
			if len(same) < 2 {
				continue // この段では他と一致しない。粗い段を見る
			}
			// 最初に一致した段だけで判定する (L1 で一致すれば粗い段でも必ず一致するので、重ねて出さない)。
			if len(same) > len(baseCount[lv][h]) && !reported[lv.String()+h] {
				reported[lv.String()+h] = true
				v := Violation{Level: lv, Changed: e, Fail: opt.Fail[lv]}
				for _, s := range same {
					if s != e {
						v.Same = append(v.Same, s)
					}
				}
				r.Violations = append(r.Violations, v)
			}
			break
		}
	}
	sort.SliceStable(r.Violations, func(i, j int) bool {
		a, b := r.Violations[i].Changed.Unit, r.Violations[j].Changed.Unit
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return r
}

func eligible(e *group.Entry, lv norm.Level, opt group.Options) bool {
	if e.Unit.Unreadable || e.Result.Nodes < group.MinNodes(e, opt) {
		return false
	}
	return lv != norm.L3 || !e.Result.L3Trivial
}
