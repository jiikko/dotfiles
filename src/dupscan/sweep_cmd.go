package main

import (
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/langs"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/tree"
)

// sweep は、重複を掘る 6 つの見方 (既定の report / 下限を下げて L1m も見る / 部分木の類似 / 名前の束 / 手で展開した helper / 型の写し) を 1 回の走査でまとめて出す。
// 見方ごとに順位の付け方が違うので、節を混ぜずに節の中で並べる。

const (
	sweepDeepMinNodes   = 12
	sweepMaxSimilarUnit = 8000
	sweepClusterMin     = 4
	// 20 個以上が同じ形の群は、構築子 (`init`) や経路登録の closure のような決まりきった書き方であることが多い (obaket の実測:
	// `init` ×118 が先頭を埋め、実在した 3 組の写しが 400 行目以降に沈んだ)。重みでは先頭に来るので、後ろへ回して先頭の数件だけ出す。
	sweepBigGroupMin   = 20
	sweepBigGroupShown = 5
)

type sweepSimilar struct {
	a, b   *group.Entry
	score  float64
	shared int // 共有している部分木の重み (寄せたときに減る量の目安)
}

type sweepCluster struct {
	verb, dir string
	names     []string
	// seq は連番の名前の束 (verb は数字を除いた名前、names は実際の名前)。
	seq bool
	// method は同名のメソッドの束 (verb は名前そのもの、names は受け口の型)。
	method bool
}

func runSweep(args []string, stdout, stderr io.Writer) int {
	fs, c := newFlags("dupscan sweep", stderr)
	threshold := fs.Float64("similar-threshold", 0.6, "部分木の類似の weighted Dice の下限 (0 より大きく 1 以下)")
	maxUnits := fs.Int("similar-max-units", sweepMaxSimilarUnit, "部分木の類似で総当たりする単位の上限 (超えたら節ごと飛ばして、そう書く)")
	dirs, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan sweep")
	if *c.format != "text" {
		return fail(fmt.Errorf("sweep は text だけ (知らない形式 %q)", *c.format))
	}
	if !(*threshold > 0 && *threshold <= 1) {
		return fail(errors.New("similar-threshold は (0,1]"))
	}
	opt, err := c.options()
	if err != nil {
		return fail(err)
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	src := repo.Worktree{Scope: sc, Exts: langs.Extensions()}
	cfg, err := loadConfig(src, *c.noExclude)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	got, err := scanSource(src, true, analysisPlan{Normalize: true})
	if err != nil {
		return fail(err)
	}
	entries, err := got.cloneEntries()
	if err != nil {
		return fail(err)
	}
	cfg, err = withComments(cfg, entries, *c.noExclude)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	ex := exclude.New(cfg, entries, scopeFilter(sc))

	baseAll := group.Build(entries, opt)
	base := dropAllowedGroups(ex, entries, ex.ApplyGroups(baseAll))
	deepOpt := group.Options{MinNodes: sweepDeepMinNodes, MinClosureNodes: sweepDeepMinNodes, Levels: []norm.Level{norm.L1, norm.L2, norm.LM}}
	deepAll := group.Build(entries, deepOpt)
	deep := newGroups(dropAllowedGroups(ex, entries, ex.ApplyGroups(deepAll)), base)
	// 節 3 から除く「組に入る対」は、除外で消す前の組で数える (組の除外・allow-group で消した組の対を、似ている対として出し直さない)
	similar, skippedSimilar := sweepSimilarPairs(entries, baseAll, deepAll, *threshold, *maxUnits, ex.Allowed)
	clusters := sweepClusters(entries, opt)
	inlined := sweepInlined(entries, ex.Allowed)
	typeCopies := sweepTypeCopies(base, deep)

	w := &lineWriter{w: stdout}
	w.printf("dupscan sweep: 単位 %d・ファイル %d・読めないファイル %d。節ごとに順位は別 (上から先に見る価値が高い順)\n", len(entries), got.files, len(got.errFiles))
	w.printf("意図して分けている組・誤検知は、組の全員の単位の直前に `// dupscan:allow <false-positive|intentional> <理由>` を書くと静かになる (1 人だけに書いて組ごと静かにするなら、メンバーの 1 つに `// dupscan:allow-group <人数> <種別> <理由>`)\n")
	// 節ごとに、印が無ければ出た組の数との差で数える。節 2 は、印が無くても節 1 の組に覆われない組だけで比べる:
	// 節 1 から消した組は節 2 の走査にも同じ組として入る (deep 全体の差だと 2 回数える)。また、節 1 で除外した組の部分集合が
	// 節 2 に残ることがある (パスの除外は levels を省くと L1m に効かない)。それは除外が消した組ではないので引かない (引くと負になる)
	silenced := len(baseAll) - len(base) + len(newGroups(deepAll, baseAll)) - len(newGroups(deep, baseAll))
	sweepExclusionText(w, silenced, ex.Stale(), sc.Whole())
	w.section("1. 同じ形の組 (既定の下限 %d 節・段 %s)", opt.MinNodes, *c.levels)
	sweepGroups(w, base, *c.limit)
	w.section("2. 下限を %d 節に下げ、メンバー名だけ違う写し (L1m) も見て新しく出た組 (小さい単位が多く、雑音も混ざる。allow 印で静かにできる)", sweepDeepMinNodes)
	sweepGroups(w, deep, *c.limit)
	if skippedSimilar != "" {
		w.section("3. 部分木が似ている対 (飛ばした)")
		w.printf("%s\n", skippedSimilar)
	} else {
		w.section("3. 部分木が似ている対 (Dice %.2f 以上・%d 節以上。同一動作・再利用は未証明。上の組に入る対は除く。行頭は Dice と w = 共有している部分木の重みの合計 (寄せたときに減る量の目安) で、w の大きい順)", *threshold, sweepDeepMinNodes)
		sweepSimilarText(w, similar, *c.limit)
	}
	w.section("4. 名前の束 (同じ動詞で始まる小さい関数が同じ dir に %d 個以上・同名のメソッド・連番の名前。寄せられる共通の道具の手がかり)", sweepClusterMin)
	sweepClusterText(w, clusters, *c.limit)
	w.section("5. 既存の小さい関数 (文 %d〜%d 個) の本体の文の並びが、同じ dir の別の単位の中に手書きされている所 (その関数を呼べる候補。単位どうしの比較の 1〜3 には出ない。テスト関数の中は見ない)", inlineMinStmts, inlineMaxStmts)
	sweepInlinedText(w, inlined, *c.limit)
	w.section("6. 型の写し (節 1・2 の組 (L1m・クロージャ・型のメンバーが %d 人以上の組を除く。その中の小さい組は束ねる) を型で束ね、相手と同じ組に入るメンバーが両方に %d つ以上ある型どうしを写しとして、つながる型を 1 つにまとめる。寄せるなら型ごと。protocol の準拠や setUp / tearDown のような決まった形も混ざる。節は寄せたときに減る節の数の目安で、型 1 つ増えるごとに減る節 (節 ÷ (型の数 − 1)) の大きい順)", sweepBigGroupMin, sweepTypeCopyMin)
	sweepTypeCopyText(w, typeCopies, *c.limit)
	if w.err != nil {
		return fail(w.err)
	}
	return 0
}

// sweepExclusionText は、節 1・2 から除外 (印・設定ファイル) で消した組の数と、人数が合わずに効いていない allow-group を出す。
// 印を足した後の「なし」だけでは、印が効いて消えたのか、組が元から無かったのかを出力から区別できない。
// 人数の食い違いは gate では -v の一覧にしか出ず、sweep は組を黙って出し直すだけだった。
// 起点が repo の一部なら、L1 の同じ本体の単位が人数より多い印 (exclude.Rule.GroupMissOver) だけを出し、ほかは数だけ言う:
// 同じ本体の単位は起点の中でしか数えないので、module をまたぐ組の印は、写しが減ったのか起点の外にいるのかを区別できない
// (src/treefiler で 4 本がこの形だった)。
func sweepExclusionText(w *lineWriter, silenced int, stale []exclude.Rule, whole bool) {
	w.printf("印・除外で静かにした組 (節 1・2): %d\n", silenced)
	outside := 0
	for _, r := range stale {
		switch {
		case r.GroupMiss() == "" || r.Comment == "":
		case whole || r.GroupMissOver():
			w.printf("🚨 効いていない allow-group (組は出し続ける。寄せるか、確かめてから人数を直す): %s (%s)\n", r.Comment, r.GroupMiss())
		default:
			outside++
		}
	}
	if outside > 0 {
		w.printf("人数の合わない allow-group %d 個は、起点の中の L1 の同じ本体の単位が人数より少ないので出していない (起点の外のメンバーと区別できない。repo の root から打つか、gate -v で見る)\n", outside)
	}
}

// dropAllowedGroups は、メンバーが全部 `dupscan:allow` の許可済みの組を除く (gate が許可済みを新しい重複に数えないのと同じ扱い)。
// report は許可済みの単位に印を付けるだけで組は出すが、sweep は「見終えた組を静かにする」ための出力なので、全員が許可済みなら出さない。
// クロージャは、囲む単位が許可済みなら許可済みとして数える (関数に付けた印が、その中の同じ形のクロージャの組にも効く)。
func dropAllowedGroups(ex interface{ Allowed(*group.Entry) bool }, entries []*group.Entry, groups []group.Group) []group.Group {
	byKey := map[string]*group.Entry{}
	for _, e := range entries {
		byKey[e.Unit.Key] = e
	}
	allowed := func(m *group.Entry) bool {
		if ex.Allowed(m) {
			return true
		}
		owner := byKey[m.Unit.Owner]
		return m.Unit.Owner != "" && owner != nil && ex.Allowed(owner)
	}
	out := groups[:0:0]
	for _, g := range groups {
		all := true
		for _, m := range g.Members {
			all = all && allowed(m)
		}
		if !all {
			out = append(out, g)
		}
	}
	return out
}

// newGroups は groups のうち、メンバーが全部 covered の 1 つの組に含まれるものを除く (下限を下げて同じ組が再び出ただけのもの)。
func newGroups(groups, covered []group.Group) []group.Group {
	sets := make([]map[*group.Entry]bool, len(covered))
	for i, o := range covered {
		sets[i] = group.MemberSet(o.Members)
	}
	var out []group.Group
	for _, g := range groups {
		set := group.MemberSet(g.Members)
		if !slices.ContainsFunc(sets, func(o map[*group.Entry]bool) bool { return group.Subset(set, o) }) {
			out = append(out, g)
		}
	}
	return out
}

func sweepGroups(w *lineWriter, groups []group.Group, limit int) {
	if len(groups) == 0 {
		w.printf("なし\n")
		return
	}
	// 大きい写しほど寄せた効果が大きいので、節の数 × 余分な写しの数で並べる。ただし sweepBigGroupMin 個以上の群は後ろに回す
	sort.SliceStable(groups, func(i, j int) bool {
		if bi, bj := len(groups[i].Members) >= sweepBigGroupMin, len(groups[j].Members) >= sweepBigGroupMin; bi != bj {
			return bj
		}
		return sweepWeight(groups[i]) > sweepWeight(groups[j])
	})
	for i, g := range groups {
		if limit > 0 && i == limit {
			w.printf("… 他 %d 組 (-limit 0 で全部)\n", len(groups)-limit)
			break
		}
		w.printf("[%s] %s ×%d 節 %d\n", g.Level, partitionLabel(g), len(g.Members), g.Members[0].Result.Nodes)
		for j, m := range g.Members {
			if len(g.Members) >= sweepBigGroupMin && j == sweepBigGroupShown {
				w.printf("  … 他 %d 件 (-limit 0 でも畳む。全員は report の -limit 0 で見る)\n", len(g.Members)-sweepBigGroupShown)
				break
			}
			w.printf("  %s:%d %s\n", m.Unit.File, m.Unit.Line, unitLabel(m.Unit))
		}
	}
}

// partitionLabel は組の種別。メンバーが全部 testdata の下 (go tool が無視する fixture) なら、prod/test でなく testdata と出す。
func partitionLabel(g group.Group) string {
	for _, m := range g.Members {
		if m.Unit.File != "testdata" && !strings.HasPrefix(m.Unit.File, "testdata/") && !strings.Contains(m.Unit.File, "/testdata/") {
			return g.Partition
		}
	}
	return "testdata"
}

func sweepWeight(g group.Group) int { return g.Members[0].Result.Nodes * (len(g.Members) - 1) }

func unitLabel(u *tree.Unit) string {
	if u.Kind == tree.KindClosure {
		return "{closure} in " + u.OwnerName
	}
	return u.Name
}

// sweepSimilarPairs は、読める名前つきの単位を総当たりして、Dice が threshold 以上の対を返す。
// 単位が maxUnits を超えたら総当たりは重いので比べず、飛ばした理由の文を返す。
func sweepSimilarPairs(entries []*group.Entry, base, deep []group.Group, threshold float64, maxUnits int, allowed func(*group.Entry) bool) ([]sweepSimilar, string) {
	var cand []*group.Entry
	for _, e := range entries {
		if !e.Unit.Unreadable && e.Unit.Body != nil && e.Unit.Kind != tree.KindClosure && e.Result.Nodes >= sweepDeepMinNodes {
			cand = append(cand, e)
		}
	}
	if len(cand) > maxUnits {
		return nil, fmt.Sprintf("比べる単位が %d で上限 %d を超えた。-similar-max-units を上げるか、dir を絞って回す", len(cand), maxUnits)
	}
	grouped := map[[2]*group.Entry]bool{}
	for _, gs := range [][]group.Group{base, deep} {
		for _, g := range gs {
			for _, a := range g.Members {
				for _, b := range g.Members {
					grouped[[2]*group.Entry{a, b}] = true
				}
			}
		}
	}
	bags := make([]norm.SubtreeBag, len(cand))
	weights := make([]int, len(cand))
	quiet := make([]bool, len(cand)) // 許可済みの単位 (対ごとに引くと、総当たりの内側で何百万回も呼ぶことになる)
	for i, e := range cand {
		quiet[i] = allowed(e)
		bags[i] = norm.Subtrees(e.Unit, 3)
		for _, wt := range bags[i] {
			weights[i] += wt
		}
	}
	var out []sweepSimilar
	for i := range cand {
		for j := i + 1; j < len(cand); j++ {
			a, b := cand[i], cand[j]
			if path.Ext(a.Unit.File) != path.Ext(b.Unit.File) || grouped[[2]*group.Entry{a, b}] || nested(a.Unit, b.Unit) || (quiet[i] && quiet[j]) {
				continue
			}
			lo, hi := min(weights[i], weights[j]), weights[i]+weights[j]
			if hi == 0 || 2*float64(lo)/float64(hi) < threshold {
				continue // Dice の上限 2·min/(和) が下限に届かない
			}
			// Dice の分母 (重みの合計の和) は bag ごとに数えてある。bag の中を毎回歩き直さない
			if shared := bags[i].Shared(bags[j]); shared > 0 {
				if score := 2 * float64(shared) / float64(weights[i]+weights[j]); score >= threshold {
					out = append(out, sweepSimilar{a: a, b: b, score: score, shared: shared})
				}
			}
		}
	}
	// 共有している重みの大きい順 (同じなら一致度が高い順)。一致度だけで並べると、`os.WriteFile` を包む 3 行の test helper のような
	// 小さい対が 1.0 近くで上位を占め、寄せたときに減る量の大きい対が沈む。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].shared != out[j].shared {
			return out[i].shared > out[j].shared
		}
		return out[i].score > out[j].score
	})
	return out, ""
}

// nested は、片方の単位がもう片方の中にあるか (同じファイルで行の範囲が含まれる)。
func nested(a, b *tree.Unit) bool {
	if a.File != b.File {
		return false
	}
	in := func(x, y *tree.Unit) bool { return y.Line <= x.Line && x.EndLine <= y.EndLine }
	return in(a, b) || in(b, a)
}

func sweepSimilarText(w *lineWriter, pairs []sweepSimilar, limit int) {
	if len(pairs) == 0 {
		w.printf("なし\n")
		return
	}
	for i, p := range pairs {
		if limit > 0 && i == limit {
			w.printf("… 他 %d 対 (-limit 0 で全部)\n", len(pairs)-limit)
			break
		}
		w.printf("%.3f w=%d %s:%d %s ~ %s:%d %s\n", p.score, p.shared, p.a.Unit.File, p.a.Unit.Line, unitLabel(p.a.Unit), p.b.Unit.File, p.b.Unit.Line, unitLabel(p.b.Unit))
	}
}

// sweepClusters は節 4 の束を返す: 下限に満たない小さい関数を (dir, 名前の先頭の語) で束ねて sweepClusterMin 個以上のもの、同名のメソッドの束 (sweepMethodClusters)、連番の名前の束 (sweepSeqClusters)。
// 写しではなく「同じ種類の仕事を各所で書いている」手がかり。先頭の語の束は Test・New・init・述語と、メソッドを除く (メソッドは受け口の型ごとに同名が並ぶのが普通なので、同名の束として別に見る)。
func sweepClusters(entries []*group.Entry, opt group.Options) []sweepCluster {
	type key struct{ verb, dir string }
	by := map[key][]string{}
	for _, e := range entries {
		u := e.Unit
		if u.Unreadable || u.Test || u.Kind != tree.KindFunc || u.Enclosing != "" || e.Result.Nodes >= group.MinNodes(e, opt) {
			continue
		}
		verb := leadingWord(u.Name)
		if verb == "" || predicateVerbs[verb] || verb == "test" || verb == "new" || verb == "init" || verb == "main" {
			continue
		}
		k := key{verb, path.Dir(u.File)}
		by[k] = append(by[k], u.Name)
	}
	out := append(sweepMethodClusters(entries, opt), sweepSeqClusters(entries)...)
	for k, names := range by {
		if len(names) >= sweepClusterMin {
			sort.Strings(names)
			out = append(out, sweepCluster{verb: k.verb, dir: k.dir, names: names})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].names) != len(out[j].names) {
			return len(out[i].names) > len(out[j].names)
		}
		return out[i].dir+out[i].verb < out[j].dir+out[j].verb
	})
	return out
}

// predicateVerbs は束にしない先頭の語。述語 (`isX` / `hasX`) は入力の型がばらばらで、名前が揃っていても寄せられる道具の手がかりにならない。
var predicateVerbs = map[string]bool{"is": true, "has": true, "should": true, "can": true, "does": true, "will": true, "are": true}

// 同名のメソッドの束にしない名前 (標準の interface を満たすための名前は、型ごとに並ぶのが普通)。
var commonMethodNames = map[string]bool{"string": true, "error": true, "close": true, "len": true, "less": true, "swap": true, "write": true, "read": true, "unwrap": true, "init": true}

// sweepMethodClusters は、同じ dir (package) に、同じ名前で受け口の型が 2 種類以上の、節が下限の 3 倍未満のメソッドが 3 個以上ある束。
// 節の数が 3 倍以内に収まるものだけ (名前が同じでも大きさが桁違いなら別の仕事)。名前の先頭の語で束ねる側では見えない `take` / `busy` のような同名を拾う。
func sweepMethodClusters(entries []*group.Entry, opt group.Options) []sweepCluster {
	type key struct{ name, dir string }
	type member struct {
		recv  string
		nodes int
	}
	by := map[key][]member{}
	for _, e := range entries {
		u := e.Unit
		if u.Unreadable || u.Test || u.Kind != tree.KindFunc || u.Enclosing == "" || e.Result.Nodes >= 3*group.MinNodes(e, opt) || commonMethodNames[strings.ToLower(u.Name)] || len([]rune(u.Name)) < 4 {
			continue
		}
		k := key{u.Name, path.Dir(u.File)}
		by[k] = append(by[k], member{u.Enclosing, e.Result.Nodes})
	}
	var out []sweepCluster
	for k, ms := range by {
		lo, hi, recvs := ms[0].nodes, ms[0].nodes, map[string]bool{}
		for _, m := range ms {
			lo, hi = min(lo, m.nodes), max(hi, m.nodes)
			recvs[m.recv] = true
		}
		if len(ms) < 3 || len(recvs) < 2 || hi > 3*lo {
			continue
		}
		names := make([]string, 0, len(recvs))
		for r := range recvs {
			names = append(names, r)
		}
		sort.Strings(names)
		out = append(out, sweepCluster{verb: k.name, dir: k.dir, names: names, method: true})
	}
	return out
}

// sweepSeqClusters は、末尾の数字だけが違う名前 (`contains` / `contains2`) の組。数字を足して名前を避けた写しの手がかり。
func sweepSeqClusters(entries []*group.Entry) []sweepCluster {
	type key struct{ base, dir string }
	type span struct{ lo, hi int }
	by := map[key]map[string]bool{}
	size := map[key]*span{}
	for _, e := range entries {
		u := e.Unit
		if u.Unreadable || u.Test || u.Kind != tree.KindFunc {
			continue
		}
		base := strings.TrimRight(u.Name, "0123456789")
		if len([]rune(base)) < 4 {
			continue
		}
		k := key{base, path.Dir(u.File)}
		if by[k] == nil {
			by[k] = map[string]bool{}
			size[k] = &span{e.Result.Nodes, e.Result.Nodes}
		}
		by[k][u.Name] = true
		size[k].lo, size[k].hi = min(size[k].lo, e.Result.Nodes), max(size[k].hi, e.Result.Nodes)
	}
	var out []sweepCluster
	for k, set := range by {
		// 数字が桁数・版を表す名前 (`Round` / `Round3`) は、大きさが違う別の仕事なので、節の数が 1.5 倍以内のものだけ
		if len(set) < 2 || 2*size[k].hi > 3*size[k].lo {
			continue
		}
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out = append(out, sweepCluster{verb: k.base, dir: k.dir, names: names, seq: true})
	}
	return out
}

// leadingWord は camelCase / snake_case の名前の先頭の語 (小文字)。`readFile` → `read`。語が 1 つだけの名前は束にしない (空)。
func leadingWord(name string) string {
	rs := []rune(name)
	for i := 1; i < len(rs); i++ {
		if unicode.IsUpper(rs[i]) || rs[i] == '_' {
			return strings.ToLower(string(rs[:i]))
		}
	}
	return ""
}

func sweepClusterText(w *lineWriter, cs []sweepCluster, limit int) {
	if len(cs) == 0 {
		w.printf("なし\n")
		return
	}
	for i, c := range cs {
		if limit > 0 && i == limit {
			w.printf("… 他 %d 束 (-limit 0 で全部)\n", len(cs)-limit)
			break
		}
		if c.seq {
			w.printf("%s/ 連番の名前 %s: %s\n", c.dir, c.verb, strings.Join(c.names, ", "))
			continue
		}
		if c.method {
			w.printf("%s/ %s (同名のメソッド) 受け口 ×%d: %s\n", c.dir, c.verb, len(c.names), strings.Join(c.names, ", "))
			continue
		}
		w.printf("%s/ %s* ×%d: %s\n", c.dir, c.verb, len(c.names), strings.Join(c.names, ", "))
	}
}

// lineWriter は最初の書き込みの失敗を覚えて、以降を飛ばす。
type lineWriter struct {
	w   io.Writer
	err error
}

func (l *lineWriter) printf(format string, a ...any) {
	if l.err == nil {
		_, l.err = fmt.Fprintf(l.w, format, a...)
	}
}

func (l *lineWriter) section(format string, a ...any) {
	l.printf("\n## "+format+"\n", a...)
}
