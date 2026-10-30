package tickchain

import (
	"testing"
	"time"
)

const (
	fast = 16 * time.Millisecond
	slow = 80 * time.Millisecond
)

var t0 = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

// 遅い tick が張ってある間に速い tick が要ったら、待たずに張り直す (打鍵の直後に遅い tick まで止まらない)。
func TestFasterNeedRearmsOverPendingSlowTick(t *testing.T) {
	var c Chain
	slowSeq, ok := c.Want(t0, slow)
	if !ok {
		t.Fatal("何も張っていないのに張らない")
	}
	fastSeq, ok := c.Want(t0.Add(10*time.Millisecond), fast)
	if !ok {
		t.Fatal("遅い tick (80ms 後) が張ってあるのに、16ms 後に要る tick を張らない")
	}
	if c.Fired(slowSeq) {
		t.Fatal("追い越された遅い tick を最新として受けた")
	}
	if !c.Fired(fastSeq) {
		t.Fatal("張り直した tick を最新として受けない")
	}
}

// 張ってある tick が間に合うなら張らない (動きの間に毎 Update で tick が増殖しない)。
func TestPendingTickInTimeIsShared(t *testing.T) {
	var c Chain
	seq, _ := c.Want(t0, fast)
	if _, ok := c.Want(t0.Add(5*time.Millisecond), fast); ok {
		t.Fatal("11ms 後に届く tick があるのに、16ms 以内の tick をもう 1 本張った")
	}
	if _, ok := c.Want(t0.Add(5*time.Millisecond), slow); ok {
		t.Fatal("速い tick が張ってあるのに、遅い tick をもう 1 本張った")
	}
	if !c.Fired(seq) {
		t.Fatal("張った tick を最新として受けない")
	}
	if _, ok := c.Want(t0.Add(16*time.Millisecond), fast); !ok {
		t.Fatal("届いた後に次の tick を張らない (動きが続かない)")
	}
}

// 届いた tick をもう一度受けても束ねは空いたまま (同じ seq の重複で次の張りを止めない)。
func TestFiredTwiceDoesNotBlock(t *testing.T) {
	var c Chain
	seq, _ := c.Want(t0, fast)
	c.Fired(seq)
	if c.Fired(seq) {
		t.Fatal("届いた後の同じ seq をもう一度最新として受けた")
	}
	if _, ok := c.Want(t0.Add(fast), fast); !ok {
		t.Fatal("次の tick を張らない")
	}
}
