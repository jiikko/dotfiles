package golang

import (
	"testing"

	"dupscan/internal/tree"
)

// 子の並びは cap を切って返す。後で append しても、領域の中の隣の並びを上書きしない
// (withStmts は作った節の Children へ append する)。
func TestArenaChildrenDoNotOverwriteTheNeighbour(t *testing.T) {
	var a arena
	n1, n2, n3, n4 := &tree.Node{Kind: "1"}, &tree.Node{Kind: "2"}, &tree.Node{Kind: "3"}, &tree.Node{Kind: "4"}
	x := a.children([]*tree.Node{n1, n2})
	y := a.children([]*tree.Node{n3})
	x = append(x, n4)
	if y[0] != n3 {
		t.Fatalf("x への append が隣の並び y を上書きした: y[0]=%v", y[0].Kind)
	}
	if len(x) != 3 || x[2] != n4 {
		t.Errorf("x = %v", x)
	}
}

func TestArenaChildrenEmptyIsNil(t *testing.T) {
	var a arena
	a.children([]*tree.Node{{Kind: "x"}}) // 領域が空でないとき、空の並びは領域の途中の空 slice になりうる
	if got := a.children(nil); got != nil {
		t.Errorf("子が無い節の Children は nil のまま (空の slice にしない): %#v", got)
	}
}

// 領域が満ちて新しい領域に替わっても、先に切り出した節はそのまま読める。
func TestArenaNodesSurviveChunkChange(t *testing.T) {
	w := &walker{}
	first := w.mk(tree.Node{Kind: "first"})
	for range arenaChunk * 3 {
		w.mk(tree.Node{Kind: "other"})
	}
	if first.Kind != "first" {
		t.Errorf("先の節が壊れた: %q", first.Kind)
	}
	if len(w.arena.nodes) > arenaChunk {
		t.Errorf("1 つの領域が chunk を超えた: %d", len(w.arena.nodes))
	}
}

// 領域の大きさは arenaFirst から倍々で arenaChunk まで。固定の大きさだと、空関数 1 つのファイルが多い入力で確保量が膨らむ。
func TestNextChunkGrowsFromSmallToTheCap(t *testing.T) {
	want := []int{arenaFirst, arenaFirst * 2, arenaFirst * 4, arenaChunk, arenaChunk}
	got, size := []int{}, 0
	for range want {
		size = nextChunk(size)
		got = append(got, size)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("領域の大きさの並び = %v, want %v", got, want)
		}
	}
	if arenaFirst >= arenaChunk {
		t.Errorf("最初の大きさ %d が上限 %d 以上 (段階的に増えない)", arenaFirst, arenaChunk)
	}
}

// 小さいファイルでは、最初の小さい領域だけで済む (節 3 つのファイルで 64 節ぶんを取らない)。
func TestArenaTakesOnlyASmallFirstChunkForASmallFile(t *testing.T) {
	w := &walker{}
	for range 3 {
		w.mk(tree.Node{Kind: "n"})
	}
	if cap(w.arena.nodes) != arenaFirst {
		t.Errorf("3 節のファイルが %d 節ぶんの領域を取った (最初は %d)", cap(w.arena.nodes), arenaFirst)
	}
}
