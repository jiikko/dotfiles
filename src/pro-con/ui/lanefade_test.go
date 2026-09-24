package ui

import "testing"

// 256 色の混ぜ合わせ: 端は元の色のまま、途中は両端と違う色 (丸めても中間色がある)。
func TestMix256(t *testing.T) {
	if got := mix256(laneOff, laneOn, 0); got != laneOff {
		t.Fatalf("t=0 は元の色のはず: %d", got)
	}
	if got := mix256(laneOff, laneOn, 1); got != laneOn {
		t.Fatalf("t=1 は行き先の色のはず: %d", got)
	}
	if got := mix256(laneOff, laneOn, 0.5); got == laneOff || got == laneOn {
		t.Fatalf("途中の色が端と同じ (段階が無い): %d", got)
	}
	for _, n := range []int{16, 202, 240, 231, 255} { // 256 色の番号は RGB を経ても自分に戻る
		if got := nearest256(rgb256(n)); got != n {
			t.Fatalf("%d の RGB が %d に丸められた", n, got)
		}
	}
}

// レーンを移ると、離れるレーンは現在地の色から、入るレーンは平常の色から、所要をかけて入れ替わる。
func TestLaneColorFadesOnFocusChange(t *testing.T) {
	m, clk := cursorModel(t)
	from := m.col
	press(m, "l")
	to := m.col
	if to == from {
		t.Fatal("前提: l で隣のレーンへ移るはず")
	}
	if m.laneColor(from) != laneOn || m.laneColor(to) != laneOff {
		t.Fatalf("押した瞬間に色が入れ替わった: 離れる %d / 入る %d", m.laneColor(from), m.laneColor(to))
	}
	clk.t = clk.t.Add(cursorDuration / 3)
	if c := m.laneColor(to); c == laneOn || c == laneOff {
		t.Fatalf("途中の色が端のどちらかと同じ (パッと変わる): %d", c)
	}
	clk.t = clk.t.Add(cursorDuration)
	if m.laneColor(from) != laneOff || m.laneColor(to) != laneOn {
		t.Fatalf("所要の後に入れ替わっていない: 離れる %d / 入る %d", m.laneColor(from), m.laneColor(to))
	}
}

// タブの切り替えでは色を移さずに点け直す。
func TestLaneColorSnapsOnTabSwitch(t *testing.T) {
	m, clk := cursorModel(t)
	m.resetSlots()
	m.col = 0
	m.Update(frameMsg{})
	if m.laneFading(clk.t) || m.laneColor(0) != laneOn {
		t.Fatalf("置き直しで色が移っている: fading=%v 色=%d", m.laneFading(clk.t), m.laneColor(0))
	}
}
