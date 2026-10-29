package filer

import (
	"testing"
	"time"
)

// pulseDiff は光を外して描いた画面と今の画面を比べる。changed は変わったセル (画面の座標)、cur はカーソルの背景の左端。
// カーソルの背景が光で塗り替わったら落とす (出たばかりの頭はそのセルに居る)。
func pulseDiff(t *testing.T, m *Model) (changed [][2]int, cur [2]int) {
	t.Helper()
	keep := m.pulses
	m.pulses = nil
	was := append([]cell(nil), m.draw().cells...)
	m.pulses = keep
	c := m.draw()
	cur = [2]int{-1, -1}
	for i := range c.cells {
		x, y := i%c.w, i/c.w
		if was[i].bg == cCursorBg {
			if cur[0] < 0 || x < cur[0] {
				cur = [2]int{x, y}
			}
			if c.cells[i].bg != cCursorBg {
				t.Fatalf("光がカーソルの背景を塗り替えた (%d,%d)", x, y)
			}
		}
		if c.cells[i] != was[i] {
			changed = append(changed, [2]int{x, y})
		}
	}
	return changed, cur
}

// headX は光ったセルのうちいちばん左 (尾はカーソルの側に伸びる。頭が途中の名前の上にあるフレームでは、名前は光らないので
// 線の上の尾の左端になる)。
func headX(changed [][2]int) int {
	h := -1
	for _, p := range changed {
		if h < 0 || p[0] < h {
			h = p[0]
		}
	}
	return h
}

func advBy(m *Model, at *time.Time, d time.Duration) {
	for end := at.Add(d); at.Before(end); {
		*at = at.Add(16 * time.Millisecond)
		m.Advance(*at)
	}
}

// カーソルを動かすと、経路の線を光がカーソルの側から root の側へ走り、走り終えたら止まる (2026-10-29 ユーザー依頼)。
// 光るのは経路の線だけ: カーソルの行の、カーソルより左のセルしか変わらない (root の他の子への枝・縦棒は光らない)。
func TestPulseRunsFromCursorTowardRoot(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	settle(m)
	at := fixedNow
	var heads []int
	for range 30 {
		changed, cur := pulseDiff(t, m)
		if cur[0] < 0 {
			t.Fatal("前提: カーソルが描かれていない")
		}
		for _, p := range changed {
			if p[1] != cur[1] || p[0] > cur[0] {
				t.Fatalf("経路の外が光った: (%d,%d) カーソル=%v", p[0], p[1], cur)
			}
		}
		if h := headX(changed); h >= 0 {
			heads = append(heads, h)
		}
		advBy(m, &at, 16*time.Millisecond)
	}
	if len(heads) < 5 {
		t.Fatalf("光ったフレームが少ない: %v", heads)
	}
	_, cur := pulseDiff(t, m)
	if heads[0] < cur[0]-4 || heads[len(heads)-1] >= heads[0]-4 {
		t.Fatalf("光がカーソルの側から root の側へ走らない: 頭の列=%v カーソル=%d", heads, cur[0])
	}
	if len(m.pulses) != 0 || m.Animating() {
		t.Fatalf("走り終えた光が残る: %d 本 (動き=%v)", len(m.pulses), m.Animating())
	}
}

// 次のキーでは走っている光を打ち切らず (進みもそのまま)、新しい光を足す (押しっぱなしで光が root まで届く)。
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
	if m.pulses[0].t.el < 0.03 {
		t.Fatalf("次のキーで前の光が出直した (進み %v 秒)", m.pulses[0].t.el)
	}
}

// 押しっぱなしでも同時に走る光は maxPulses 本まで。起動時のカーソルでは光らない。
func TestPulseCountIsCapped(t *testing.T) {
	m := newTest(t)
	if len(m.pulses) != 0 {
		t.Fatalf("起動しただけで光が %d 本", len(m.pulses))
	}
	at := fixedNow
	for range maxPulses + 2 {
		m.HandleKey("j")
		at = at.Add(time.Millisecond)
		m.Advance(at)
	}
	if len(m.pulses) != maxPulses {
		t.Fatalf("光が %d 本 (上限 %d)", len(m.pulses), maxPulses)
	}
	if el := m.pulses[len(m.pulses)-1].t.el; el > 0.0015 {
		t.Fatalf("上限で今の光を足さなかった (最後の光の進み %v 秒。古い光から捨てるはず)", el)
	}
}

// 光は出発点 (カーソルの名前の 1 桁左) より右を光らせない: 開いたフォルダの上では経路がカーソルの右へ続き、尾がそこまで届く。
func TestPulseDoesNotLightRightOfCursor(t *testing.T) {
	hs := pulseHeads{{x: 10, tail: 12, a: 1, from: 12}}
	c := &cell{bg: cBg}
	for x := 10; x <= 22; x++ {
		lit := hs.light(c, x, cAccRoute) != cAccRoute
		if lit != (x <= 12) {
			t.Fatalf("桁 %d: 光った=%v (出発点 12 より右は光らない)", x, lit)
		}
	}
}

// glogx は New の後に RestorePlace でカーソルを記録の場所へ移す。場所の復元では光らない。
func TestPulseNotOnRestorePlace(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	p := m.Place()
	m2 := newAt(t, m.root.path())
	m2.Resize(120, 40)
	settle(m2)
	m2.RestorePlace(p)
	m2.Advance(fixedNow.Add(16 * time.Millisecond))
	if m2.cur.raw != "x.txt" {
		t.Fatalf("前提: 記録の場所へ戻っていない (%s)", m2.cur.raw)
	}
	if len(m2.pulses) != 0 {
		t.Fatalf("場所の復元で光が %d 本出た", len(m2.pulses))
	}
}

// 動きの途中は経路に縦棒が出る (親と子の行が離れている)。縦棒は光らせない: 桁だけで光るので、頭がその桁に来ると縦の帯ごと
// 瞬く。背景を塗る頭のセルは、どのフレームでも光 1 本につき 2 つまで (経路が縦棒の桁で曲がる角の、親の行と子の行。縦棒を
// 光らせると、その間の行の数だけ増える)。
func TestPulseDoesNotLightVerticalBars(t *testing.T) {
	m := newTest(t)
	m.HandleKey("G") // root の直下の先頭から末尾へ: 縦に 8 段動く
	at := fixedNow
	sawGlow := false
	for range 30 {
		advBy(m, &at, 16*time.Millisecond)
		keep := m.pulses
		m.pulses = nil
		was := append([]cell(nil), m.draw().cells...)
		m.pulses = keep
		c := m.draw()
		heads := 0
		for i := range c.cells {
			if c.cells[i].bg != was[i].bg {
				heads++
			}
			sawGlow = sawGlow || c.cells[i] != was[i]
		}
		if heads > 2*len(m.pulses) {
			t.Fatalf("背景を塗ったセルが %d (光は %d 本)", heads, len(m.pulses))
		}
	}
	if !sawGlow {
		t.Fatal("前提: 光が一度も描かれていない")
	}
}
