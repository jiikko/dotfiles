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

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// Options は判定の設定。
type Options struct {
	group.Options
	// Fail は止める段。粗い段を止めるなら、それより細かい段の一致も止める (L1 で一致すれば L2・L3 でも一致するため)。
	Fail map[norm.Level]bool
	// Exclude は除外の設定 (nil なら無し)。組の除外に当たる組は違反にせず、許可の単位は寄せる側に出さない。
	Exclude *exclude.Set
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
	// PathPassed は、パスの除外の下にあるので寄せる側に名指ししなかった、変わった単位 (除外で通した写し。人が見る数)。
	PathPassed []*group.Entry
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
			if group.Eligible(e, lv, opt.Options) {
				groups[lv][e.Result.Hash[lv]] = append(groups[lv][e.Result.Hash[lv]], e)
			}
		}
	}

	var r Result
	judged := map[string]bool{} // 段 + ハッシュ → 違反か。組ごとに 1 回だけ判定する
	passed := map[*group.Entry]bool{}
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
			if !group.Eligible(e, lv, opt.Options) {
				continue
			}
			h := e.Result.Hash[lv]
			g := groups[lv][h]
			if len(g) < 2 {
				continue // この段では他と一致しない。粗い段を見る
			}
			k := lv.String() + h
			if _, done := judged[k]; !done && opt.Exclude != nil && opt.Exclude.PathCovers(lv, g) {
				// 全員がパスの除外の下にある組。除外が無ければ違反だったときだけ、寄せる側を除外で通した数に入れる
				// (作り直しで写しの組に同じ修正が入る・改名するのは数えない)。
				judged[k] = false
				bare := opt
				bare.Exclude = nil
				if v, ok, _ := judgeGroup(lv, h, g, origin, bare); ok {
					for _, m := range v.New {
						passed[m] = true
					}
				}
			}
			if _, done := judged[k]; !done && opt.Exclude != nil && opt.Exclude.GroupExcluded(lv, g) {
				judged[k] = false // 除外した組。粗い段で新しいメンバーが増えていればそちらで出る
			}
			if _, done := judged[k]; !done {
				v, ok, byPath := judgeGroup(lv, h, g, origin, opt)
				if ok {
					r.Violations = append(r.Violations, v)
				}
				for _, m := range byPath {
					passed[m] = true
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
	for _, e := range head {
		if passed[e] {
			r.PathPassed = append(r.PathPassed, e)
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
// byPath は、パスの除外の下にあるので寄せる側から外した、変わった単位。
func judgeGroup(lv norm.Level, h string, g []*group.Entry, origin map[*group.Entry]*group.Entry, opt Options) (v Violation, ok bool, byPath []*group.Entry) {
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
			if !group.Eligible(b, lv, opt.Options) && b.Unit.Key != m.Unit.Key {
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
		return Violation{}, false, nil
	}
	// 寄せる側 (新しい) は、一番多い出どころ以外から来た、この変更で触った単位 (新しい単位どうしの組なら全部)。
	// 触っていない単位は寄せる側に出さない (触っていないファイルに warning が付く)。
	isCand := func(m *group.Entry) bool {
		return (from[m] != mostKey || strings.HasPrefix(mostKey, "new:")) && !unchanged(m, origin)
	}
	touched := func(m *group.Entry) bool { return !unchanged(m, origin) }
	// 名指ししない単位 (許可・パスの除外の下) は、寄せる側を選ぶ前に外す。選んでから外すと、外の写しが名指しされずに違反ごと消える。
	allowed := func(m *group.Entry) bool { return opt.Exclude != nil && opt.Exclude.Allowed(m) }
	quiet := func(m *group.Entry) bool {
		return allowed(m) || opt.Exclude != nil && opt.Exclude.PathExcluded(m, lv)
	}
	v = Violation{Level: lv, Fail: failAt(lv, opt.Fail)}
	pick := func(ok func(*group.Entry) bool) {
		v.New, v.Same = nil, nil
		for _, m := range g {
			if ok(m) && !quiet(m) {
				v.New = append(v.New, m)
			} else {
				v.Same = append(v.Same, m)
			}
		}
	}
	pick(isCand)
	// 寄せる側の候補が無い (触った単位が全部一番多い出どころの側) か、候補が全部パスの除外の下: 触った単位を寄せる側にする
	// (除外の下の単位と組になった外の写しは、外の単位として止める)。候補に許可の単位がいれば付け替えない (意図した写しなので、
	// 同じ修正を入れただけの写しを代わりに止めない)。
	if len(v.New) == 0 && !hasAny(g, func(m *group.Entry) bool { return isCand(m) && allowed(m) }) {
		pick(touched)
	}
	if len(v.New) == 0 {
		// 名指しできる単位が無い。除外が無ければ寄せる側だったパスの除外の下の単位を、除外で通した数に入れる。
		named := isCand
		if !hasAny(g, isCand) {
			named = touched
		}
		for _, m := range g {
			if named(m) && opt.Exclude != nil && opt.Exclude.PathExcluded(m, lv) {
				byPath = append(byPath, m)
			}
		}
		return Violation{}, false, byPath
	}
	return v, true, nil
}

func hasAny(g []*group.Entry, ok func(*group.Entry) bool) bool {
	for _, m := range g {
		if ok(m) {
			return true
		}
	}
	return false
}

// origins は head の単位ごとに base の側の相手 (出どころ) を 1 対 1 で当てる。ずれに強い順に当てる:
//  0. 読めない構文の単位は鍵で (本体で当てられない)
//  1. 鍵も本体 (L1) も同じ単位 (変わっていない)
//  2. 同じファイルで本体が同じ単位。同じ名前の相手を先に当てる (改名・クロージャの番号のずれ。名前をファイルで
//     区切らないと、別のファイルに足した同じ名前の新しい写しが、改名した写しの相手を取る)
//  3. 本体が同じ単位 (別のファイルへの移動)。同じ名前の相手を先に当てる (名前を変えずに移した写しが、別のファイルに
//     足した新しい写しと相手を取り合うと、head の並びで名指しが決まる)
//  4. 鍵が同じ単位 (その場での編集)。クロージャを除く: クロージャの鍵は囲む単位の中の出現の順番で、前のクロージャを
//     消すと後ろのクロージャが消えた鍵に入るため、鍵で当てると消えたクロージャを相手にしてしまう
//  5. 同じ修正を入れた写し: 本体が同じ head の単位が既に相手を持っていれば、その相手の base での本体と同じ base の単位
//     (写しの組に同じ修正を入れて片方を移した・番号がずれた。緩い段より先に当てないと、先着で別の単位に相手を取られる)
//  6. まだ相手の無い base の単位のうち、L2、次に L3 の形が同じもの (改名・移動しながらの小さな編集)
//  7. クロージャの鍵
//  8. 鍵が同じ単位 (下限をまたいで育った・縮んだ単位)
//     鍵だけなので、小さいクロージャの鍵の位置に大きい新しい写しが入ると (前に足して後ろがずれ、元の小さいクロージャも
//     書き換えた)、育った写しとして通る。逆に、下限より小さかった写しが一緒に育ちながら改名・番号のずれを伴うと、
//     ここでは当たらず止まる。緩い段で種類をまたぐと、小さいクロージャを同じ本体の関数に置き換える新しい写しが通る
//     (TestIneligibleBaseUnitIsNotOrigin) ので、両方は同時に直せない。実データ (DualNoteApp の first-parent 500 commit) では
//     どちらも 1 件も出ていない。再び直すなら、その形が実データで出た commit を添える
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
			if group.Eligible(e, norm.L1, opt) {
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
	match(func(e *group.Entry) string { return join(l1(e), e.Unit.Name) })
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
