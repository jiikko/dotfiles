package anim

import "testing"

func TestCursorEaseOutBackOvershootsThenLands(t *testing.T) {
	if got := EaseOutBack(0, cursorOvershoot); got != 0 {
		t.Fatalf("t=0 で 0 でない: %v", got)
	}
	if got := EaseOutBack(1, cursorOvershoot); got != 1 {
		t.Fatalf("t=1 で 1 でない: %v", got)
	}
	// 着地の手前で 1 を超える (行き過ぎて戻る = ease-out-back の主張そのもの)。
	over := false
	for i := 1; i < 100; i++ {
		if EaseOutBack(float64(i)/100, cursorOvershoot) > 1 {
			over = true
			break
		}
	}
	if !over {
		t.Fatal("行き過ぎが 1 度も起きない (ease-out ではあるが back ではない)")
	}
}
