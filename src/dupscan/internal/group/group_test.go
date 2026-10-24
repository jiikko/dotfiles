package group

import (
	"testing"

	"dupscan/internal/norm"
	"dupscan/internal/tree"
)

func entry(key string, test bool, h1, h2, h3 string, nodes int, trivial bool) *Entry {
	return &Entry{
		Unit:   &tree.Unit{Key: key, File: key, Test: test},
		Result: norm.Result{Hash: map[norm.Level]string{norm.L1: h1, norm.L2: h2, norm.L3: h3}, Nodes: nodes, L3Trivial: trivial},
	}
}

var all = Options{MinNodes: 5, Levels: norm.Levels}

func TestSameMembersAreNotRepeatedAtCoarserLevels(t *testing.T) {
	a := entry("a", false, "x", "y", "z", 10, false)
	b := entry("b", false, "x", "y", "z", 10, false)
	gs := Build([]*Entry{a, b}, all)
	if len(gs) != 1 || gs[0].Level != norm.L1 {
		t.Fatalf("L1 の 1 組だけを期待: %+v", gs)
	}
}

func TestCoarserGroupWithNewMembersIsReported(t *testing.T) {
	a := entry("a", false, "x", "y", "z", 10, false)
	b := entry("b", false, "x", "y", "z", 10, false)
	c := entry("c", false, "q", "y", "z", 10, false) // L2 から a・b と同じ
	gs := Build([]*Entry{a, b, c}, all)
	if len(gs) != 2 || gs[0].Level != norm.L1 || gs[1].Level != norm.L2 || len(gs[1].Members) != 3 {
		t.Fatalf("L1 の {a,b} と L2 の {a,b,c} を期待: %+v", gs)
	}
}

func TestSmallUnreadableAndTrivialAreExcluded(t *testing.T) {
	small := entry("s1", false, "x", "x", "x", 2, false)
	small2 := entry("s2", false, "x", "x", "x", 2, false)
	if gs := Build([]*Entry{small, small2}, all); len(gs) != 0 {
		t.Errorf("下限より小さい単位が組になった: %+v", gs)
	}
	tr1 := entry("t1", false, "a", "b", "z", 10, true)
	tr2 := entry("t2", false, "c", "d", "z", 10, true)
	if gs := Build([]*Entry{tr1, tr2}, all); len(gs) != 0 {
		t.Errorf("L3Trivial の単位が L3 の組になった: %+v", gs)
	}
	u1 := entry("u1", false, "x", "x", "x", 10, false)
	u2 := entry("u2", false, "x", "x", "x", 10, false)
	u2.Unit.Unreadable = true
	if gs := Build([]*Entry{u1, u2}, all); len(gs) != 0 {
		t.Errorf("読めない単位が組になった: %+v", gs)
	}
}

func TestPartition(t *testing.T) {
	gs := Build([]*Entry{entry("a", true, "x", "x", "x", 10, false), entry("b", false, "x", "x", "x", 10, false)}, all)
	if len(gs) != 1 || gs[0].Partition != "mixed" {
		t.Fatalf("mixed を期待: %+v", gs)
	}
}

func TestDisabledLevelIsSkipped(t *testing.T) {
	a := entry("a", false, "x", "y", "z", 10, false)
	b := entry("b", false, "q", "w", "z", 10, false)
	if gs := Build([]*Entry{a, b}, Options{MinNodes: 5, Levels: []norm.Level{norm.L1, norm.L2}}); len(gs) != 0 {
		t.Errorf("回さない段 (L3) の組が出た: %+v", gs)
	}
}

func TestClosureGroupInsideGroupedOwnersIsDropped(t *testing.T) {
	f1 := entry("f1", false, "f", "f", "f", 30, false)
	f2 := entry("f2", false, "f", "f", "f", 30, false)
	c1 := entry("c1", false, "c", "c", "c", 30, false)
	c2 := entry("c2", false, "c", "c", "c", 30, false)
	c1.Unit.Kind, c1.Unit.Owner = tree.KindClosure, "f1"
	c2.Unit.Kind, c2.Unit.Owner = tree.KindClosure, "f2"
	gs := Build([]*Entry{f1, f2, c1, c2}, all)
	if len(gs) != 1 || gs[0].Members[0].Unit.Key != "f1" {
		t.Fatalf("囲む関数の組だけを期待: %+v", gs)
	}
	// 囲む単位が組になっていなければ、クロージャの組は出す。
	f2.Result.Hash = map[norm.Level]string{norm.L1: "x", norm.L2: "x", norm.L3: "x"}
	if gs := Build([]*Entry{f1, f2, c1, c2}, all); len(gs) != 1 || gs[0].Members[0].Unit.Key != "c1" {
		t.Fatalf("クロージャの組だけを期待: %+v", gs)
	}
}

func TestClosureMinNodes(t *testing.T) {
	c1 := entry("c1", false, "c", "c", "c", 15, false)
	c2 := entry("c2", false, "c", "c", "c", 15, false)
	c1.Unit.Kind, c2.Unit.Kind = tree.KindClosure, tree.KindClosure
	if gs := Build([]*Entry{c1, c2}, Options{MinNodes: 10, MinClosureNodes: 20, Levels: norm.Levels}); len(gs) != 0 {
		t.Errorf("クロージャの下限より小さいクロージャが組になった: %+v", gs)
	}
}

func TestLevelsAreOrderedFinestFirst(t *testing.T) {
	a := entry("a", false, "x", "y", "z", 10, false)
	b := entry("b", false, "x", "y", "z", 10, false)
	gs := Build([]*Entry{a, b}, Options{MinNodes: 5, Levels: []norm.Level{norm.L3, norm.L1}})
	if len(gs) != 1 || gs[0].Level != norm.L1 {
		t.Fatalf("渡した順に関係なく L1 の組を期待: %+v", gs)
	}
}

func TestClosureGroupIsKeptWhenOwnersMatchOnlyAtCoarserLevel(t *testing.T) {
	f1 := entry("f1", false, "f1", "f1", "f", 30, false) // 囲む関数は L3 でだけ一致
	f2 := entry("f2", false, "f2", "f2", "f", 30, false)
	c1 := entry("c1", false, "c", "c", "c", 30, false) // クロージャは L1 で一致
	c2 := entry("c2", false, "c", "c", "c", 30, false)
	c1.Unit.Kind, c1.Unit.Owner = tree.KindClosure, "f1"
	c2.Unit.Kind, c2.Unit.Owner = tree.KindClosure, "f2"
	gs := Build([]*Entry{f1, f2, c1, c2}, all)
	var closureGroup bool
	for _, g := range gs {
		if g.Level == norm.L1 && g.Members[0].Unit.Key == "c1" {
			closureGroup = true
		}
	}
	if !closureGroup {
		t.Fatalf("囲む関数が L3 でしか一致しないのに、L1 のクロージャの組が消えた: %+v", gs)
	}
}
