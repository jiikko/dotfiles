package card

import (
	"slices"
	"testing"
)

// 通常の流れは途切れずに依頼から完了へつながり、質問待ち以外の全レーンを通る (レーンを足したら流れにも書く)。
func TestMainFlowChainsToDone(t *testing.T) {
	var seen []State
	for i, st := range MainFlow {
		if i > 0 && st.From != MainFlow[i-1].To {
			t.Fatalf("%d 番目の移り変わり %s → %s が前の先 %s から始まらない", i, st.From.Label(), st.To.Label(), MainFlow[i-1].To.Label())
		}
		if st.To.Other == "" {
			seen = append(seen, st.To.Lane)
		}
	}
	if last := MainFlow[len(MainFlow)-1].To; last != at(Done) {
		t.Fatalf("通常の流れが完了で終わらない (%s)", last.Label())
	}
	for _, s := range Columns {
		if s != Waiting && !slices.Contains(seen, s) {
			t.Errorf("通常の流れが %s を通らない", s.Label())
		}
	}
}

// どの移り変わりにも誰が・何でがあり、寄り道は質問待ちから出て戻る道を持つ (入ったきりの寄り道を書かない)。
func TestFlowStepsSayWhoAndHow(t *testing.T) {
	outOfWaiting := false
	for _, st := range slices.Concat(MainFlow, FlowDetours) {
		if st.Who == "" || st.How == "" || st.From.Label() == "" || st.To.Label() == "" {
			t.Errorf("誰が・何で・端のどれかが空: %+v", st)
		}
		if st.From == at(Waiting) && st.To != at(Waiting) {
			outOfWaiting = true
		}
	}
	if !outOfWaiting {
		t.Error("質問待ちから戻る道が書かれていない")
	}
}
