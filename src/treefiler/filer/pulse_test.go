package filer

import (
	"testing"
	"time"
)

// pulseHeadX は経路の光の頭の桁 (画面の座標)。光を外して描いた画面と比べ、色が変わったセルのうちいちばん左を頭とする
// (尾はカーソルの側に伸びる。頭が途中の名前の上にあるフレームでは、名前は光らないので線の上の尾の左端になる)。
func pulseHeadX(t *testing.T, m *Model) (head, cursorX int) {
	t.Helper()
	keep := m.pulses
	m.pulses = nil
	base := m.draw()
	was := append([]cell(nil), base.cells...)
	m.pulses = keep
	c := m.draw()
	head, cursorX = -1, -1
	for i := range c.cells {
		x := i % c.w
		if was[i].bg == cCursorBg && (cursorX < 0 || x < cursorX) {
			cursorX = x
		}
		if c.cells[i] != was[i] && (head < 0 || x < head) {
			head = x
		}
	}
	return head, cursorX
}

// カーソルを動かすと、経路の線を光がカーソルの側から root の側へ走り、走り終えたら止まる (2026-10-29 ユーザー依頼)。
func TestPulseRunsFromCursorTowardRoot(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	settle(m)
	at := fixedNow
	adv := func(d time.Duration) {
		for end := at.Add(d); at.Before(end); {
			at = at.Add(16 * time.Millisecond)
			m.Advance(at)
		}
	}
	adv(16 * time.Millisecond)
	x0, cx := pulseHeadX(t, m)
	if x0 < 0 || cx < 0 || x0 < cx-4 {
		t.Fatalf("出た直後の光の頭がカーソルの近くに無い: 頭=%d カーソル=%d", x0, cx)
	}
	adv(150 * time.Millisecond)
	x1, _ := pulseHeadX(t, m)
	if x1 < 0 || x1 >= x0-4 {
		t.Fatalf("光が root の側へ走らない: %d → %d", x0, x1)
	}
	adv(time.Second)
	if len(m.pulses) != 0 || m.Animating() {
		t.Fatalf("走り終えた光が残る: %d 本 (動き=%v)", len(m.pulses), m.Animating())
	}
}

// 次のキーでは走っている光を打ち切らず、新しい光を足す (押しっぱなしで光が root まで届く)。
func TestPulseSurvivesNextKey(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "a/one.txt")
	settle(m)
	m.Advance(fixedNow.Add(16 * time.Millisecond))
	m.HandleKey("j")
	m.Advance(fixedNow.Add(32 * time.Millisecond))
	if len(m.pulses) != 2 {
		t.Fatalf("次のキーの後の光が %d 本 (前の光と新しい光の 2 本のはず)", len(m.pulses))
	}
}
