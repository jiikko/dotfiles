package filer

import (
	"math"
	"slices"
)

// pulse は経路の線を、出したときのカーソルの深さから root へ走る光 (2026-10-29 ユーザー依頼「カーソルから root に向かって
// 光が走る感じ」)。起点は毎フレーム、今の経路のその深さの項目の位置から取る (列のスクロール・カメラの途中でも線に乗る)。
// 出したときの項目そのものを起点にしない: カーソルが離れると、その項目は仮想化したブロックでは配置されず (virt.go)、
// 仮想化の有無で画面が変わる (virt_test.go)。経路の上の項目は必ず配置される。
// snapAll では打ち切らない: 押しっぱなしで押すたびに打ち切ると、新しい光がカーソルの近くで出直すだけで root まで届かない。
type pulse struct {
	depth int
	el    float64 // 出てからの秒数
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
		if p.el += dt; p.el < pulseDur {
			kept = append(kept, p)
		}
	}
	m.pulses = kept
	return len(m.pulses) > 0
}

// pulseHead は 1 フレームの 1 つの光。位置は桁ではなく、経路の横の線のセルを右 (カーソル側) から並べた列 (track) の
// 番号で持つ: 桁で進めると、頭が途中の名前の上にある間は光るセルが無く、光が途切れて見える (名前は光らせない)。
// start は出発点 (カーソルの名前の 1 桁左) の番号で、それより手前 (カーソルの右へ続く経路) は光らせない。
type pulseHead struct {
	head, tail, a float64
	start         int
}

type pulseHeads struct {
	track []int // 経路の横の線が通る桁 (大きい順)
	hs    []pulseHead
}

// pulseHeads は今のフレームの光。grid は今のフレームの線の格子。起点か root が描かれていない光は出さない。
// 光は出発点から root の右の線の始まりまで、一定の速さで走る (2026-10-30 ユーザー依頼「root まで光がぴーっと走って欲しい」。
// ease-out では経路の大半を 0.1 秒で過ぎ、残りを root の手前で消えかけながら留まっていた)。
func (m *Model) pulseHeads(grid lineGrid, now map[*node][2]int) pulseHeads {
	if len(m.pulses) == 0 {
		return pulseHeads{}
	}
	rp, ok := now[m.root]
	if !ok {
		return pulseHeads{}
	}
	rx := rp[0] + m.labelWidth(m.root) + 1 // root の右から出る線の始まり
	track := m.trackBuf[:0]
	for k, lc := range grid {
		if lc.routeRun {
			track = append(track, k[0])
		}
	}
	slices.Sort(track)
	track = slices.Compact(track)
	slices.Reverse(track)
	m.trackBuf = track
	hs := m.pulseBuf[:0]
	for _, p := range m.pulses {
		fp, ok := now[m.spineAt(p.depth)]
		if !ok {
			continue
		}
		start, end, ok := trackSpan(track, fp[0]-1, rx) // 出発点はその項目の名前の 1 桁左 (線の先端)
		if !ok {
			continue
		}
		el := p.el / pulseDur
		a := 1.0
		if el > pulseRun {
			a = math.Max(0, (1-el)/(1-pulseRun)) // root に着いてから消える
		}
		n := end - start
		hs = append(hs, pulseHead{
			head:  float64(start) + float64(n)*math.Min(1, el/pulseRun),
			tail:  math.Max(3, math.Min(10, float64(n)*0.2)),
			a:     a,
			start: start,
		})
	}
	m.pulseBuf = hs
	return pulseHeads{track: track, hs: hs}
}

// pulseRun は光が root に着くまでの割合 (残りで消える)。
const pulseRun = 0.85

// spineAt は今の経路 (spine) の深さ depth の項目。カーソルより深ければ、カーソルより先の続き (spineNext) を辿る:
// 親へ上がった後も経路は前にいた子まで続いているので、そこから出た光は出発点を動かさずに走り続ける
// (出発点をカーソルに切り詰めると、走っている光の頭が 1 フレームで root 側へ跳ぶ)。続きが途切れたら最後の項目。
func (m *Model) spineAt(depth int) *node {
	n := m.cur
	for n.parent != nil && n.depth() > depth {
		n = n.parent
	}
	for n.depth() < depth {
		next := m.spineNext(n)
		if next == nil {
			break
		}
		n = next
	}
	return n
}

// trackSpan は track (大きい順) のうち、桁 from 以下・rx 以上の番号の範囲 [start, end]。無ければ ok=false。
// rx ちょうどのセルが無いとき (root の行の線が画面の行の外で格子に無いフレーム) は、rx より右の最後のセルまで。
func trackSpan(track []int, from, rx int) (start, end int, ok bool) {
	start = trackIndex(track, from)
	end = trackIndex(track, rx)
	if end == len(track) || track[end] < rx {
		end--
	}
	return start, end, start < len(track) && start <= end
}

// trackIndex は track (大きい順) で x 以下になる最初の番号。
func trackIndex(track []int, x int) int {
	i, _ := slices.BinarySearchFunc(track, x, func(t, x int) int { return x - t })
	return i
}

// light は経路の横の線のセル (木の座標の桁 x) を光で明るくする。尾は進む向きの後ろ (カーソル側) に伸びる。頭のセルは背景も塗る。
// 背景は地の色のセルだけ: 出たばかりの頭はカーソルの名前の 1 桁左に居て、そこはカーソルの背景で塗ってある。
func (ps pulseHeads) light(c *cell, x int, col rgb) rgb {
	if len(ps.hs) == 0 {
		return col
	}
	i := trackIndex(ps.track, x)
	if i >= len(ps.track) || ps.track[i] != x {
		return col
	}
	g, head := 0.0, false
	for _, h := range ps.hs {
		d := math.Round(h.head) - float64(i)
		if i < h.start || d < 0 || d > h.tail {
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
