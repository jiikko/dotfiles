package filer

import "math"

// pulse は経路の線を、出したときのカーソルの深さから root へ走る光 (2026-10-29 ユーザー依頼「カーソルから root に向かって
// 光が走る感じ」)。起点は毎フレーム、今の経路のその深さの項目の位置から取る (列のスクロール・カメラの途中でも線に乗る)。
// 出したときの項目そのものを起点にしない: カーソルが離れると、その項目は仮想化したブロックでは配置されず (virt.go)、
// 仮想化の有無で画面が変わる (virt_test.go)。経路の上の項目は必ず配置される。
// snapAll では打ち切らない: 押しっぱなしで押すたびに打ち切ると、新しい光がカーソルの近くで出直すだけで root まで届かない。
type pulse struct {
	depth int
	t     tween // 0 → 1 の進み
}

const maxPulses = 6 // 押しっぱなしで同時に走る光の上限 (古いものから捨てる)

// stepPulses はカーソルが変わったら光を 1 つ出し、走り終えたものを捨てる。
func (m *Model) stepPulses(dt float64) bool {
	if m.pulseFor != m.cur {
		m.pulseFor = m.cur
		if m.cur != m.root { // root の上では走る線が無い
			if len(m.pulses) == maxPulses {
				m.pulses = append(m.pulses[:0], m.pulses[1:]...)
			}
			m.pulses = append(m.pulses, pulse{depth: m.cur.depth()})
		}
	}
	kept := m.pulses[:0]
	for _, p := range m.pulses {
		if p.t.step(1, pulseDur, dt) {
			kept = append(kept, p)
		}
	}
	m.pulses = kept
	return len(m.pulses) > 0
}

// pulseHead は 1 フレームの 1 つの光: 頭の桁 (木の座標)・尾の長さ・強さ。
type pulseHead struct {
	x, tail, a float64
}

type pulseHeads []pulseHead

// pulseHeads は今のフレームの光の頭。起点か root が描かれていない光は出さない。
func (m *Model) pulseHeads(now map[*node][2]int) pulseHeads {
	if len(m.pulses) == 0 {
		return nil
	}
	rp, ok := now[m.root]
	if !ok {
		return nil
	}
	rx := float64(rp[0] + m.labelWidth(m.root) + 1) // root の右から出る線の始まり
	hs := m.pulseBuf[:0]
	for _, p := range m.pulses {
		from := m.cur
		for from.parent != nil && from.depth() > p.depth { // 今のカーソルより深い光はカーソルから走る
			from = from.parent
		}
		fp, ok := now[from]
		if !ok {
			continue
		}
		fx := float64(fp[0] - 1) // カーソルの名前の 1 桁左 (線の先端)
		if fx <= rx {
			continue
		}
		el := math.Min(1, p.t.el/pulseDur)
		a := 1.0
		if el > 0.7 {
			a = (1 - el) / 0.3 // 終わりの 3 割で消える
		}
		hs = append(hs, pulseHead{
			x:    fx + (rx-fx)*p.t.v,
			tail: math.Max(3, math.Min(12, (fx-rx)*0.15)),
			a:    a,
		})
	}
	m.pulseBuf = hs
	return hs
}

// light は経路の線のセル (木の座標の桁 x) を光で明るくする。尾は進む向きの後ろ (カーソル側) に伸びる。頭のセルは背景も塗る。
// 背景は地の色のセルだけ: 出たばかりの頭はカーソルの名前の 1 桁左に居て、そこはカーソルの背景で塗ってある。
func (hs pulseHeads) light(c *cell, x int, col rgb) rgb {
	g, head := 0.0, false
	for _, h := range hs {
		d := float64(x) - math.Round(h.x)
		if d < 0 || d > h.tail {
			continue
		}
		if v := h.a * (1 - d/(h.tail+1)); v > g {
			g, head = v, d == 0
		}
	}
	if g <= 0 {
		return col
	}
	if head && c.bg == cBg {
		c.bg = mix(cBg, cAccRoute, 0.35*g)
	}
	return mix(col, cFlash, g)
}
