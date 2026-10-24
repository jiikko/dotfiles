// Package gate は、base から head までに新しく増えた重複を決める (make test・CI で回す dupscan gate の判定)。
//
// 変わった単位 (鍵が base に無いか、L1 のハッシュが base と違う単位) ごとに、細かい段から順に他の単位と一致した段の組を見る (違反になった最初の段だけを出す)。
// 組のメンバーごとに「出どころ」(base の側の相手が、その段で持っていたハッシュ) を引き、組の大きさが同じ出どころから来た
// メンバーの最大の数より大きいときだけ違反にする。
//   - 移動・改名・整形・既存の写しを寄せる作業・入れ替え: 出どころが同じなので違反にしない
//   - 既存の写し 2 つに同じ修正を入れる: 2 つとも base で同じ組だったので違反にしない
//   - 新しい写し (出どころの無い単位・別の組から来た単位) が組に入る: 違反
//
// base の側の相手の当て方は origins。
package gate

import (
	"sort"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
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
	origin := origins(base, head, opt.Options)

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
	judged := map[string]bool{} // 段 + ハッシュ → 違反か。組ごとに 1 回だけ判定する
	for _, e := range head {
		if unchanged(e, origin) {
			continue
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
			k := lv.String() + h
			if _, done := judged[k]; !done {
				v, ok := judgeGroup(lv, h, g, origin, opt)
				if ok {
					r.Violations = append(r.Violations, v)
				}
				judged[k] = ok
			}
			// 違反になった最初の段だけを出す。違反でなければ粗い段も見る (細かい段では前からの写しの組のままでも、
			// 粗い段で触っていない単位と新しく同じ形になることがある)。
			if judged[k] {
				break
			}
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
			// base でこの段の比べる対象でなかった相手から来た別の単位 (鍵が違う) は、比べる対象だった相手と別に数える
			// (origins は種類をまたいで当てないので、ここに来るのは base で L3 だけ比べなかった L3Trivial の相手)。
			// 同じに数えると、L3 で比べていなかった単位が組に入った分を見逃す。その場で育った単位 (鍵が同じ) は
			// 同じに数える: 別に数えたり捨てたりすると、写しが同じ修正で一緒に下限を超えたときに止まる。
			if !eligible(b, lv, opt.Options) && b.Unit.Key != m.Unit.Key {
				key = "small:" + b.Result.Hash[lv]
			}
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
	// 寄せる側 (新しい) は、一番多い出どころ以外から来た、この変更で触った単位。触っていない単位は寄せる側に出さない
	// (触っていない単位を出すと、その commit で触っていないファイルに warning が付く)。
	v := Violation{Level: lv, Fail: failAt(lv, opt.Fail)}
	for _, m := range g {
		// 一番多い出どころも base に無い (新しい単位どうしの組) なら、全部が新しい。
		if (from[m] != mostKey || strings.HasPrefix(mostKey, "new:")) && !unchanged(m, origin) {
			v.New = append(v.New, m)
		} else {
			v.Same = append(v.Same, m)
		}
	}
	if len(v.New) == 0 { // 触った単位が全部一番多い出どころの側にいる: 触った単位を寄せる側にする
		v.Same = v.Same[:0]
		for _, m := range g {
			if unchanged(m, origin) {
				v.Same = append(v.Same, m)
			} else {
				v.New = append(v.New, m)
			}
		}
	}
	return v, true
}

// origins は head の単位ごとに base の側の相手 (出どころ) を 1 対 1 で当てる。ずれに強い順に当てる:
//  0. 読めない構文の単位は鍵で (本体で当てられない)
//  1. 鍵も本体 (L1) も同じ単位 (変わっていない)
//  2. 同じファイルで本体が同じ単位。同じ名前の相手を先に当てる (改名・クロージャの番号のずれ。名前をファイルで
//     区切らないと、別のファイルに足した同じ名前の新しい写しが、改名した写しの相手を取る)
//  3. 本体が同じ単位 (別のファイルへの移動)
//  4. 鍵が同じ単位 (その場での編集)。クロージャを除く: クロージャの鍵は囲む単位の中の出現の順番で、前のクロージャを
//     消すと後ろのクロージャが消えた鍵に入るため、鍵で当てると消えたクロージャを相手にしてしまう
//  5. 同じ修正を入れた写し: 本体が同じ head の単位が既に相手を持っていれば、その相手の base での本体と同じ base の単位
//     (写しの組に同じ修正を入れて片方を移した・番号がずれた。緩い段より先に当てないと、先着で別の単位に相手を取られる)
//  6. まだ相手の無い base の単位のうち、L2、次に L3 の形が同じもの (改名・移動しながらの小さな編集)
//  7. クロージャの鍵
//  8. 鍵が同じ単位 (下限をまたいで育った・縮んだ単位)
//
// 2 から 7 は、比べる対象 (下限以上で読める) の単位どうし、そうでない単位どうしの中でだけ当てる。またいで当てると、
// 足した短いクロージャが改名した写しの相手を取って写しを新しく見せ、新しい写しが触っていない小さいクロージャの相手を
// 取って通る (小さいクロージャどうしは本体が同じことが多い)。段の鍵が空の単位は、その段では当てない。
func origins(base, head []*group.Entry, opt group.Options) map[*group.Entry]*group.Entry {
	out := map[*group.Entry]*group.Entry{}
	used := map[*group.Entry]bool{}
	curBase, curHead := base, head
	match := func(keyOf func(*group.Entry) string) {
		pool := map[string][]*group.Entry{}
		for _, b := range curBase {
			if k := keyOf(b); !used[b] && k != "" {
				pool[k] = append(pool[k], b)
			}
		}
		for _, e := range curHead {
			if out[e] != nil {
				continue
			}
			k := keyOf(e)
			if k == "" {
				continue
			}
			for len(pool[k]) > 0 && used[pool[k][0]] {
				pool[k] = pool[k][1:]
			}
			if len(pool[k]) > 0 {
				out[e], used[pool[k][0]] = pool[k][0], true
				pool[k] = pool[k][1:]
			}
		}
	}
	isBase := map[*group.Entry]bool{}
	for _, b := range base {
		isBase[b] = true
	}
	match(func(e *group.Entry) string {
		if isBase[e] || e.Unit.Unreadable {
			return e.Unit.Key
		}
		return ""
	})
	match(func(e *group.Entry) string { return join(e.Unit.Key, l1(e)) })
	split := func(es []*group.Entry) (big, small []*group.Entry) {
		for _, e := range es {
			if eligible(e, norm.L1, opt) {
				big = append(big, e)
			} else {
				small = append(small, e)
			}
		}
		return big, small
	}
	hb, hs := split(head)
	bb, bs := split(base)
	for _, p := range [][2][]*group.Entry{{bb, hb}, {bs, hs}} {
		curBase, curHead = p[0], p[1]
		matchLoose(match, head, out, isBase)
	}
	curBase, curHead = base, head
	match(func(e *group.Entry) string { return e.Unit.Key })
	return out
}

func l1(e *group.Entry) string { return e.Result.Hash[norm.L1] }

// join は段の鍵を組み立てる。空の部分があれば空 (その段では当てない)。
func join(parts ...string) string {
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	return strings.Join(parts, "\x00")
}

// matchLoose は origins の 2 から 7 の段を順に当てる。
func matchLoose(match func(func(*group.Entry) string), head []*group.Entry, out map[*group.Entry]*group.Entry, isBase map[*group.Entry]bool) {
	match(func(e *group.Entry) string { return join(l1(e), e.Unit.File, e.Unit.Name) })
	match(func(e *group.Entry) string { return join(l1(e), e.Unit.File) })
	match(l1)
	match(func(e *group.Entry) string {
		if e.Unit.Kind == tree.KindClosure {
			return ""
		}
		return e.Unit.Key
	})
	byL1 := map[string][]*group.Entry{}
	for _, e := range head {
		byL1[l1(e)] = append(byL1[l1(e)], e)
	}
	match(func(e *group.Entry) string {
		if isBase[e] {
			return l1(e)
		}
		k := "" // 相手を持つ写しが複数あれば、相手の本体の小さい方 (並びで揺れないように)
		for _, s := range byL1[l1(e)] {
			if b := out[s]; b != nil && (k == "" || l1(b) < k) {
				k = l1(b)
			}
		}
		return k
	})
	match(func(e *group.Entry) string { return e.Result.Hash[norm.L2] })
	match(func(e *group.Entry) string { return e.Result.Hash[norm.L3] })
	match(func(e *group.Entry) string {
		if e.Unit.Kind != tree.KindClosure {
			return ""
		}
		return e.Unit.Key
	})
}

// unchanged は、出どころが同じ鍵・同じ本体の単位 (この変更で触っていない単位) か。
func unchanged(e *group.Entry, origin map[*group.Entry]*group.Entry) bool {
	b := origin[e]
	return b != nil && b.Unit.Key == e.Unit.Key && b.Result.Hash[norm.L1] == e.Result.Hash[norm.L1]
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
