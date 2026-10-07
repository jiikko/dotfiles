package main

import "testing"

// groupExpansion の 3 つの集合の整合 (issue 666)。番号フィルタ中に畳んだ (collapsed) 状態は
// フィルタを解いたら残らず、その後の手動の展開が効く。解く経路は endFilter の 1 本に寄せてある。
func TestGroupExpansionEndFilterDropsCollapse(t *testing.T) {
	const a, b = "/repo/issues/epic/a", "/repo/issues/epic/b"
	var g groupExpansion
	g.setAuto(map[string]bool{a: true, b: true}) // 番号フィルタが a / b を一時的に開いた
	g.toggle(a)                                  // フィルタ中に a を明示的に畳む
	if g.expanded(a) || !g.collapsed[a] {
		t.Fatalf("フィルタ中の畳みが効いていない: expanded=%v collapsed=%v", g.expanded(a), g.collapsed)
	}
	g.endFilter(b) // 現在行は b の子: b だけ手動の展開へ引き継ぐ
	g.setAuto(nil)
	if len(g.collapsed) != 0 {
		t.Fatalf("フィルタを解いても畳んだ状態が残った: %v", g.collapsed)
	}
	if !g.expanded(b) || g.expanded(a) {
		t.Fatalf("解除後の展開が違う: a=%v b=%v (b だけ引き継ぐ)", g.expanded(a), g.expanded(b))
	}
	g.toggle(a) // 解除後に手動で開く
	if !g.expanded(a) {
		t.Fatal("解除後に手動で開いても a が開かない (畳んだ状態が残っている)")
	}
	if got := g.saved(); !got[a] || !got[b] || len(got) != 2 {
		t.Fatalf("保存する展開が違う: %v", got)
	}
}

// reveal は明示的な畳みを上書きして開き、prune は走査結果に無い group を手動の展開から落とす。
func TestGroupExpansionRevealAndPrune(t *testing.T) {
	const a, gone = "/repo/issues/epic/a", "/repo/issues/epic/gone"
	var g groupExpansion
	g.setAuto(map[string]bool{a: true})
	g.toggle(a) // 畳む
	g.reveal(a)
	if !g.expanded(a) || g.collapsed[a] {
		t.Fatalf("reveal が畳みを上書きしていない: expanded=%v collapsed=%v", g.expanded(a), g.collapsed)
	}
	g.reveal(gone)
	g.prune(map[string]bool{a: true})
	if g.manual[gone] || !g.manual[a] {
		t.Fatalf("prune の結果が違う: %v", g.manual)
	}
	g.reveal("") // 空の key は何もしない
	if g.manual[""] {
		t.Fatal("空の key を展開に入れた")
	}
}
