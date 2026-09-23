package anim

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

const d = 100 * time.Millisecond

func at(f float64) time.Time { return t0.Add(time.Duration(f * float64(d))) }

func TestTransitionOpensAndSettles(t *testing.T) {
	var tr Transition
	if tr.Phase() != Closed || tr.Openness(t0, EaseOutCubic) != 0 {
		t.Fatalf("zero value が閉じていない: phase=%v", tr.Phase())
	}
	tr.Open(t0, d)
	if !tr.Animating(at(0.5)) {
		t.Fatal("開く途中なのに Animating が偽")
	}
	if o := tr.Openness(at(0.5), EaseOutCubic); o <= 0 || o >= 1 {
		t.Fatalf("途中の開き具合 = %v, want 0 < x < 1", o)
	}
	if closed := tr.Settle(at(0.5)); closed || tr.Phase() != Opening {
		t.Fatalf("途中で Settle が状態を進めた: closed=%v phase=%v", closed, tr.Phase())
	}
	if closed := tr.Settle(at(1)); closed || tr.Phase() != Open {
		t.Fatalf("開き切りの Settle: closed=%v phase=%v", closed, tr.Phase())
	}
}

// 閉じ切ったときだけ closed=true (中身を捨ててよい合図)。
func TestTransitionCloseReportsClosedOnce(t *testing.T) {
	tr := NewOpen()
	tr.Close(t0, d)
	if closed := tr.Settle(at(0.5)); closed {
		t.Fatal("閉じる途中で closed=true")
	}
	if closed := tr.Settle(at(1)); !closed || tr.Phase() != Closed {
		t.Fatalf("閉じ切りの Settle: closed=%v phase=%v", closed, tr.Phase())
	}
	if closed := tr.Settle(at(2)); closed {
		t.Fatal("閉じた後の Settle がまた closed=true を返した (中身を二重に捨てる)")
	}
}

// 閉じる動きは開く動きの逆再生 (同じ進捗で同じ開き具合を通る)。
func TestTransitionCloseIsReverseOfOpen(t *testing.T) {
	var opening Transition
	opening.Open(t0, d)
	closing := NewOpen()
	closing.Close(t0, d)
	for _, f := range []float64{0, 0.25, 0.5, 0.75, 1} {
		o := opening.Openness(at(f), EaseOutCubic)
		c := closing.Openness(at(1-f), EaseOutCubic)
		if math.Abs(o-c) > 1e-9 {
			t.Errorf("進捗 %.2f: 開く %v と逆再生 %v が一致しない", f, o, c)
		}
	}
}

// 途中で向きを変えても見えている位置が跳ばない (両向きとも)。
func TestTransitionReverseIsContinuous(t *testing.T) {
	for _, f := range []float64{0.1, 0.3, 0.6, 0.9} {
		var tr Transition
		tr.Open(t0, d)
		before := tr.Openness(at(f), EaseOutCubic)
		tr.Close(at(f), d)
		if tr.Phase() != Closing {
			t.Fatalf("開く途中の Close で Closing にならない: %v", tr.Phase())
		}
		if after := tr.Openness(at(f), EaseOutCubic); math.Abs(after-before) > 1e-9 {
			t.Errorf("f=%.1f: 閉じ始めで跳んだ %v -> %v", f, before, after)
		}
		mid := at(f + 0.05)
		before = tr.Openness(mid, EaseOutCubic)
		tr.Open(mid, d)
		if tr.Phase() != Opening {
			t.Fatalf("閉じる途中の Open で Opening にならない: %v", tr.Phase())
		}
		if after := tr.Openness(mid, EaseOutCubic); math.Abs(after-before) > 1e-9 {
			t.Errorf("f=%.1f: 開き直しで跳んだ %v -> %v", f, before, after)
		}
	}
}

// 開くときと閉じるときで所要が違っても、向きを変えた瞬間に跳ばず、残りを新しい所要で進む。
func TestTransitionReverseWithDifferentDuration(t *testing.T) {
	const closeD = 3 * d
	var tr Transition
	tr.Open(t0, d)
	mid := at(0.4)
	before := tr.Openness(mid, EaseOutCubic)
	tr.Close(mid, closeD)
	if after := tr.Openness(mid, EaseOutCubic); math.Abs(after-before) > 1e-9 {
		t.Fatalf("所要の違う逆再生で跳んだ %v -> %v", before, after)
	}
	// 残り (開いた割合 0.4) を閉じる所要で進む: 0.4 × 3d 後に閉じ切る
	end := mid.Add(time.Duration(0.4 * float64(closeD)))
	if !tr.Animating(end.Add(-time.Millisecond)) {
		t.Fatal("閉じ切る前に演出が終わった (新しい所要で進んでいない)")
	}
	if !tr.Settle(end) {
		t.Fatal("残りを新しい所要で進んだ時刻に閉じ切らない")
	}
}

// 静止中に同じ向きを頼んでも何もしない (演出をやり直さない)。
func TestTransitionIdempotentRequests(t *testing.T) {
	tr := NewOpen()
	tr.Open(t0, d)
	if tr.Phase() != Open || tr.Animating(t0) {
		t.Fatalf("開いているのに Open で演出が始まった: %v", tr.Phase())
	}
	var closed Transition
	closed.Close(t0, d)
	if closed.Phase() != Closed {
		t.Fatalf("閉じているのに Close で演出が始まった: %v", closed.Phase())
	}
}

func TestTransitionFinishLandsImmediately(t *testing.T) {
	var tr Transition
	tr.Open(t0, d)
	if closed := tr.Finish(); closed || tr.Phase() != Open {
		t.Fatalf("開く演出の Finish: closed=%v phase=%v", closed, tr.Phase())
	}
	tr.Close(t0, d)
	if closed := tr.Finish(); !closed || tr.Phase() != Closed {
		t.Fatalf("閉じる演出の Finish: closed=%v phase=%v", closed, tr.Phase())
	}
}

// 所要 0 は即着地 (0 除算で NaN にしない)。
func TestTransitionZeroDuration(t *testing.T) {
	var tr Transition
	tr.Open(t0, 0)
	if tr.Animating(t0) || tr.Openness(t0, EaseOutCubic) != 1 {
		t.Fatalf("所要 0 が即着地しない: animating=%v openness=%v", tr.Animating(t0), tr.Openness(t0, EaseOutCubic))
	}
}
