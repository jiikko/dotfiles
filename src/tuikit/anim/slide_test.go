package anim

import "testing"

// 開く途中で閉じても逆再生しない: 閉じ始めは開いた姿 (見えている割合 1) から。
func TestSlideCloseDuringOpenStartsFromOpen(t *testing.T) {
	var s Slide
	s.Open(t0, d)
	s.Close(at(0.3), d)
	if p := s.Progress(at(0.3)); p != 1 {
		t.Fatalf("開く途中で閉じた直後の見えている割合 = %v, want 1 (逆再生していない)", p)
	}
	if !s.CloseLanded(at(1.3)) {
		t.Fatal("閉じる演出の所要を丸ごと過ぎても着地していない")
	}
}

// 開くときは今の状態によらず閉じた姿から始める。
func TestSlideOpenAlwaysStartsFromClosed(t *testing.T) {
	var s Slide
	s.Show()
	s.Open(t0, d)
	if p := s.Progress(t0); p != 0 {
		t.Fatalf("開いた状態から Open した直後の見えている割合 = %v, want 0", p)
	}
}

// 閉じる途中は時間が過ぎても片付けるまで tick を回す。片付けは閉じる途中にだけ効く。
func TestSlideCloseHoldsUntilEndClose(t *testing.T) {
	var s Slide
	if s.EndClose() {
		t.Fatal("閉じる途中でないのに EndClose が真 (毎打鍵で片付けが走る)")
	}
	s.Show()
	s.Close(t0, d)
	if s.CloseLanded(at(0.5)) {
		t.Fatal("閉じる途中で着地した")
	}
	if !s.Unsettled(at(3)) {
		t.Fatal("閉じる演出の時間が過ぎただけで Unsettled が偽")
	}
	s.LandOpen()
	if !s.Closing() {
		t.Fatal("LandOpen が閉じる途中を終わらせた (片付けを通らずに閉じる)")
	}
	s.Open(at(3), d)
	if !s.Closing() {
		t.Fatal("片付けの前の Open が閉じる途中を消した (閉じ切ったときの片付けが走らない)")
	}
	if !s.EndClose() || s.Closing() || s.Unsettled(at(3)) {
		t.Fatal("EndClose の後も閉じる途中のまま")
	}
}

// 見えている割合: 開く途中は 0→1、閉じる途中は 1→0、演出していなければ 1。
func TestSlideProgressDirections(t *testing.T) {
	var s Slide
	if p := s.Progress(t0); p != 1 {
		t.Fatalf("zero value の見えている割合 = %v, want 1", p)
	}
	s.Open(t0, d)
	if p := s.Progress(at(0.25)); p != 0.25 {
		t.Fatalf("開く途中 = %v, want 0.25", p)
	}
	s.LandOpen()
	if p := s.Progress(at(0.25)); p != 1 {
		t.Fatalf("LandOpen の後 = %v, want 1", p)
	}
	s.Close(t0, d)
	if p := s.Progress(at(0.25)); p != 0.75 {
		t.Fatalf("閉じる途中 = %v, want 0.75", p)
	}
}
