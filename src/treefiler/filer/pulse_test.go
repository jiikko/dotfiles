package filer

import (
	"math"
	"slices"
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
	// root の右から出る線の始まり (画面の桁)
	rootLine := int(math.Round(m.anims[m.root].x.v)) + m.labelWidth(m.root) + 1 - int(math.Round(m.camX.v))
	at := fixedNow
	var heads []int
	gaps := 0
	for range 30 {
		changed, cur := pulseDiff(t, m)
		if len(changed) == 0 && len(m.pulses) > 0 {
			gaps++
		}
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
	if gaps > 0 {
		t.Fatalf("走っている途中で光が途切れたフレームが %d (頭の列=%v)", gaps, heads)
	}
	if heads[len(heads)-1] > rootLine {
		t.Fatalf("光が root の線 (桁 %d) まで届かない: 頭の列=%v", rootLine, heads)
	}
	// 一定の速さ: root の線に着くのは走る時間 (pulseDur*pulseRun ≒ 24 フレーム) の終わりの方。ease-out だと 10 フレーム前後で着く
	if i := slices.Index(heads, rootLine); i < 18 {
		t.Fatalf("光が %d フレーム目で root の線に着いた (一定の速さなら 18 フレーム目より後): 頭の列=%v", i, heads)
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
	if m.pulses[0].el < 0.03 {
		t.Fatalf("次のキーで前の光が出直した (進み %v 秒)", m.pulses[0].el)
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
	if el := m.pulses[len(m.pulses)-1].el; el > 0.0015 {
		t.Fatalf("上限で今の光を足さなかった (最後の光の進み %v 秒。古い光から捨てるはず)", el)
	}
	if el := m.pulses[0].el; el < 0.0055 || el > 0.0065 {
		t.Fatalf("残った最初の光の進みが %v 秒 (古い 2 本を捨てて、6 打ち前の光が残るはず)", el)
	}
}

// 光は出発点 (カーソルの名前の 1 桁左) より右を光らせない: 開いたフォルダの上では経路がカーソルの右へ続き、尾がそこまで届く。
// 頭より先 (root 側) も光らせない。
func TestPulseLightsOnlyBetweenStartAndHead(t *testing.T) {
	ps := pulseHeads{
		track: []int{20, 18, 16, 14, 12, 10},
		hs:    []pulseHead{{head: 4, tail: 5, a: 1, start: 2}},
	}
	c := &cell{bg: cBg}
	for _, x := range ps.track {
		lit := ps.light(c, x, cAccRoute) != cAccRoute
		if lit != (x >= 12 && x <= 16) {
			t.Fatalf("桁 %d: 光った=%v (出発点 16 から頭 12 までだけ光る)", x, lit)
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

// trackSpan は出発点から root の右の線までの番号の範囲。root の行の線が格子に無いフレーム (rx より右のセルしか無い) でも、
// 存在しない番号を終点にしない。
func TestTrackSpan(t *testing.T) {
	track := []int{20, 18, 16, 14, 12}
	for _, c := range []struct {
		from, rx, start, end int
		ok                   bool
	}{
		{16, 12, 2, 4, true},  // rx ちょうどのセルがある
		{16, 13, 2, 3, true},  // rx が無く、その左にもセルがある
		{16, 11, 2, 4, true},  // 全部 rx より右 (root の行が格子の外)
		{21, 20, 0, 0, true},  // 出発点と終点が同じセル
		{10, 5, 0, 0, false},  // 出発点より左にセルが無い
		{25, 30, 0, 0, false}, // 全部 rx より左
	} {
		s, e, ok := trackSpan(track, c.from, c.rx)
		if ok != c.ok || (ok && (s != c.start || e != c.end)) {
			t.Errorf("trackSpan(from=%d, rx=%d) = (%d, %d, %v), want (%d, %d, %v)", c.from, c.rx, s, e, ok, c.start, c.end, c.ok)
		}
	}
}

// 一定の速さ: 走る時間の半ばで、出発点から root までの (線のセルの数で) 半ばに居る。ease-in / ease-out ではずれる。
func TestPulseRunsAtConstantSpeed(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	settle(m)
	at := fixedNow
	advBy(m, &at, time.Duration(pulseDur*pulseRun/2*float64(time.Second)))
	m.draw()
	h := m.pulseBuf[0]
	_, end, ok := trackSpan(m.trackBuf, m.trackBuf[h.start], int(math.Round(m.anims[m.root].x.v))+m.labelWidth(m.root)+1)
	if !ok {
		t.Fatal("前提: 経路の線の範囲が取れない")
	}
	if f := (h.head - float64(h.start)) / float64(end-h.start); f < 0.35 || f > 0.65 {
		t.Fatalf("走る時間の半ばで %.2f まで進んだ (半ばのはず)", f)
	}
}

// 親へ上がっても (h)、走っている光の頭は跳ばない: 経路は前にいた子まで続いているので、出発点を動かさない。
func TestPulseDoesNotJumpOnParent(t *testing.T) {
	m := newTest(t)
	cdTo(t, m, "b/deep/x.txt")
	settle(m)
	at := fixedNow
	advBy(m, &at, 16*time.Millisecond)
	m.draw()
	before := m.trackBuf[int(math.Round(m.pulseBuf[0].head))]
	m.HandleKey("h")
	if m.cur.raw != "deep" {
		t.Fatalf("前提: 親へ上がっていない (%s)", m.cur.raw)
	}
	advBy(m, &at, 16*time.Millisecond)
	m.draw()
	after := m.trackBuf[int(math.Round(m.pulseBuf[0].head))]
	if d := before - after; d < 0 || d > 2 {
		t.Fatalf("親へ上がった次のフレームで光の頭が %d 桁動いた (%d → %d。1 フレームの進みは 1〜2 桁)", d, before, after)
	}
}
