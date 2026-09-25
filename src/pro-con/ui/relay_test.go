package ui

import (
	"testing"
)

// View は描くたびに、描いたままの文字 (端末へ出すものと同じ) と画面の状態を中継の受け口へ渡す。
func TestViewFeedsFrameSink(t *testing.T) {
	m, _ := cursorModel(t)
	var got string
	var st map[string]string
	var w, h, calls int
	m.SetFrameSink(func(a string, width, height int, state map[string]string) {
		got, w, h, st = a, width, height, state
		calls++
	})
	v := m.View()
	if calls != 1 || got != v.Content || w != 120 || h != 30 {
		t.Fatalf("受け口に渡したものが画面と違う: calls=%d w=%d h=%d 同じ=%v", calls, w, h, got == v.Content)
	}
	if st["mode"] != "board" || st["selected"] != "R1" {
		t.Fatalf("画面の状態: %+v", st)
	}
	m.SetFrameSink(nil)
	m.View() // 受け口を外したら呼ばない (落ちない)
	if calls != 1 {
		t.Fatalf("外した受け口を呼んだ: %d", calls)
	}
}
