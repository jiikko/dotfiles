package main

import (
	"path"
	"sort"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

// sweep の節 5: 既存の小さい関数の本体が、別の単位の中に手で展開されている所。単位どうしを比べる節 1〜3 は、
// 「関数の本体」と「別の関数の中の文の並び」を比べないので、この形を拾わない。

const (
	inlineMinStmts = 2
	inlineMaxStmts = 8
	// 並びが関数の本体の文の節の数のこの割合以上を覆えば、その関数の写しとみなす (`t.Helper()` のような小さい前置きを省いた展開も拾う)。
	inlineCoverPct = 80
)

type sweepInline struct {
	site, helper *group.Entry
	stmts, of    int // 並びの文の数 / helper の文の数
	nodes        int
}

type inlineWindow struct {
	helper     *group.Entry
	stmts, of  int
	nodes      int
	hash       string
	start, end int
}

// sweepInlined は、名前の付いた関数 (helper) の本体の文の並び (の 8 割以上) と同じ並びが、同じ dir の別の単位の中にある所を返す。
// helper は関数・メソッド (クロージャは呼べないので除く)。test のファイルの helper は test のファイルからしか呼べないので、置き場所も test に限る。
// 並びの局所の名前は、並びの中で初めて出た順に番号を振り直して比べる (helper の引数に当たる名前が、展開先では別の名前の局所でよい)。
// 置き場所の単位に `dupscan:allow` があれば出さない。
func sweepInlined(entries []*group.Entry, allowed func(*group.Entry) bool) []sweepInline {
	byShape := map[string][]inlineWindow{}
	for _, e := range entries {
		u := e.Unit
		if u.Unreadable || u.Kind != tree.KindFunc {
			continue
		}
		stmts := norm.Statements(u)
		if len(stmts) < inlineMinStmts || len(stmts) > inlineMaxStmts {
			continue
		}
		top := topSeq(norm.Seqs(u), stmts)
		if top == nil {
			continue
		}
		hash := norm.WindowHasher(u)
		total := 0
		for _, w := range top.Weight {
			total += w
		}
		for i := range stmts {
			nodes := 0
			for j := i + 1; j <= len(stmts); j++ {
				nodes += top.Weight[j-1]
				if j-i < inlineMinStmts || nodes < sweepDeepMinNodes || 100*nodes < inlineCoverPct*total {
					continue
				}
				k := norm.SeqKey(top.Shape[i:j])
				byShape[k] = append(byShape[k], inlineWindow{helper: e, stmts: j - i, of: len(stmts), nodes: nodes, hash: hash(stmts[i:j]), start: i, end: j})
			}
		}
	}
	if len(byShape) == 0 {
		return nil
	}
	best := map[[2]*group.Entry]sweepInline{}
	for _, e := range entries {
		u := e.Unit
		if u.Unreadable || allowed(e) {
			continue
		}
		hash := norm.WindowHasher(u)
		for _, s := range norm.Seqs(u) {
			for i := range s.Nodes {
				for j := i + inlineMinStmts; j <= len(s.Nodes) && j-i <= inlineMaxStmts; j++ {
					ws := byShape[norm.SeqKey(s.Shape[i:j])]
					if len(ws) == 0 {
						continue
					}
					h := hash(s.Nodes[i:j])
					for _, w := range ws {
						if w.hash != h || !inlineCallable(w.helper.Unit, u) || w.helper.Result.Hash[norm.L1] == e.Result.Hash[norm.L1] {
							continue
						}
						k := [2]*group.Entry{e, w.helper}
						if old, ok := best[k]; !ok || w.nodes > old.nodes {
							best[k] = sweepInline{site: e, helper: w.helper, stmts: w.stmts, of: w.of, nodes: w.nodes}
						}
					}
				}
			}
		}
	}
	out := make([]sweepInline, 0, len(best))
	for k, v := range best {
		// 互いに相手の本体の大半を含む 2 つの関数 (写しを少し変えた 2 つ) は、1 行にする: 相手の文の多くを覆う側 (helper の文の数が少ない側) を残す
		if r, ok := best[[2]*group.Entry{k[1], k[0]}]; ok && (r.of < v.of || r.of == v.of && r.helper.Unit.Key < v.helper.Unit.Key) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].nodes != out[j].nodes {
			return out[i].nodes > out[j].nodes
		}
		a, b := out[i].site.Unit, out[j].site.Unit
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return out
}

// topSeq は Seqs のうち、本体の一番上の文の並び stmts のもの。
func topSeq(seqs []norm.Seq, stmts []*tree.Node) *norm.Seq {
	for i := range seqs {
		if len(seqs[i].Nodes) == len(stmts) && seqs[i].Nodes[0] == stmts[0] && seqs[i].Nodes[len(stmts)-1] == stmts[len(stmts)-1] {
			return &seqs[i]
		}
	}
	return nil
}

// inlineCallable は、site の中から helper を呼べるとみなすか: 同じ dir で、helper が test のファイルなら site も test のファイル。
// helper がメソッドなら site も同じ型のメソッド (かその中のクロージャ) に限る (受け口の名前は局所の名前として比べるので、
// 別の型の同じ名前のフィールドを触る並びも一致するが、その型のメソッドは呼べない)。helper 自身とその中 (クロージャ) は除く。
func inlineCallable(helper, site *tree.Unit) bool {
	if path.Dir(helper.File) != path.Dir(site.File) || (helper.Test && !site.Test) || site.Key == helper.Key || helper.Enclosing != site.Enclosing && helper.Enclosing != "" {
		return false
	}
	return !nested(helper, site)
}

func sweepInlinedText(w *lineWriter, xs []sweepInline, limit int) {
	if len(xs) == 0 {
		w.printf("なし\n")
		return
	}
	for i, x := range xs {
		if limit > 0 && i == limit {
			w.printf("… 他 %d 件 (-limit 0 で全部)\n", len(xs)-limit)
			break
		}
		w.printf("%s:%d %s の中に %s (%s:%d) の本体の文 %d/%d 個 (節 %d)\n", x.site.Unit.File, x.site.Unit.Line, unitLabel(x.site.Unit), x.helper.Unit.Name, x.helper.Unit.File, x.helper.Unit.Line, x.stmts, x.of, x.nodes)
	}
}
