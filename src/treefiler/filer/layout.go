package filer

import "sort"

// layout.go は木の配置 (spec §3) と線の格子 (spec §2.1)。配置は「目標位置」で、アニメはそこへ向かう (anim.go)。

type place struct {
	x, y   int
	active bool // 親が spine 上 (spec §1.4 の Placed.active)
}

// kids は表示する子 (dotfile は隠す。ただしカーソルへの経路にあるものは見せる。spec §5.5)。
func (m *Model) kids(n *node) []*node {
	if m.showHidden {
		return n.kids
	}
	out := make([]*node, 0, len(n.kids))
	for _, k := range n.kids {
		if !k.hidden() || m.onPath(k) {
			out = append(out, k)
		}
	}
	return out
}

func (m *Model) onPath(n *node) bool {
	for p := m.cur; p != nil; p = p.parent {
		if p == n {
			return true
		}
	}
	return false
}

// spine は root からカーソルまでと、カーソルが開いたフォルダなら「最後にいた子 / 先頭の子」を辿った続き。
// spine は全部 y=0 に並ぶ (固定の選択線。spec §3.1)。
func (m *Model) spine() map[*node]bool {
	s := map[*node]bool{}
	for n := m.cur; n != nil; n = n.parent {
		s[n] = true
	}
	n := m.cur
	for n.dir && n.expanded {
		ks := m.kids(n)
		if len(ks) == 0 {
			break
		}
		next := n.last
		if next == nil || !contains(ks, next) {
			next = ks[0]
		}
		s[next] = true
		n = next
	}
	return s
}

func contains(ns []*node, n *node) bool {
	for _, k := range ns {
		if k == n {
			return true
		}
	}
	return false
}

// layout は表示する全ノードの目標位置を返す (列ごとに、親ごとの子のブロックを縦に詰める。spec §3.1・§3.3 の capped)。
func (m *Model) layout() map[*node]place {
	sp := m.spine()
	pos := map[*node]place{m.root: {0, 0, true}}
	colX := map[int]int{0: 0}
	cols := map[int][]*node{0: {m.root}}
	for depth := 0; ; depth++ {
		var parents []*node
		w := 0
		for _, n := range cols[depth] {
			if lw := labelWidth(n); lw > w {
				w = lw
			}
			if n.dir && n.expanded && len(m.kids(n)) > 0 {
				parents = append(parents, n)
			}
		}
		if len(parents) == 0 {
			break
		}
		sort.Slice(parents, func(i, j int) bool { return pos[parents[i]].y < pos[parents[j]].y })
		y0 := make([]int, len(parents))
		pin := -1
		for j, p := range parents {
			ks := m.kids(p)
			y0[j] = pos[p].y - (len(ks)-1)/2
			if sp[p] {
				for i, k := range ks {
					if sp[k] {
						pin, y0[j] = j, -i
					}
				}
			}
		}
		blen := func(j int) int { return len(m.kids(parents[j])) }
		if pin >= 0 {
			for j := pin - 1; j >= 0; j-- {
				y0[j] = min(y0[j], y0[j+1]-1-blen(j))
			}
			for j := pin + 1; j < len(parents); j++ {
				y0[j] = max(y0[j], y0[j-1]+blen(j-1)+1)
			}
		} else {
			for j := 1; j < len(parents); j++ {
				y0[j] = max(y0[j], y0[j-1]+blen(j-1)+1)
			}
		}
		ups, downs := 0, 0
		for j, p := range parents {
			py := pos[p].y
			if y0[j]+blen(j)-1 < py {
				ups++
			} else if y0[j] > py {
				downs++
			}
		}
		lanes := max(1, min(max(w/3, 2), max(ups, downs)))
		// 次の列の x = 列の x + 列の幅 + lanes + gap (3) + branch (1) (spec §3.2)
		nx := colX[depth] + w + lanes + 3 + 1
		colX[depth+1] = nx
		for j, p := range parents {
			for i, k := range m.kids(p) {
				pos[k] = place{nx, y0[j] + i, sp[p]}
				cols[depth+1] = append(cols[depth+1], k)
			}
		}
	}
	return pos
}

func labelWidth(n *node) int { return widthOf(n.label()) }

// ---------- 線 (spec §2.1 の double) ----------

const (
	dirUp    = 1
	dirDown  = 2
	dirLeft  = 4
	dirRight = 8
)

// doubleGlyph は mask (上下左右のビット) から二重線の文字 (spec §2.1 の表の double)。
var doubleGlyph = []string{"·", "║", "║", "║", "═", "╝", "╗", "╣", "═", "╚", "╔", "╠", "═", "╩", "╦", "╬"}

type emphasis int

const (
	emDim emphasis = iota
	emActive
	emRoute
)

type lineCell struct {
	mask  int
	emph  emphasis
	owner *node // その枝が生える親 (線の色の元。spec §1.4 の sap)
}

type lineGrid map[[2]int]*lineCell

func (g lineGrid) add(x, y, bits int, emph emphasis, owner *node) {
	k := [2]int{x, y}
	c := g[k]
	if c == nil {
		c = &lineCell{}
		g[k] = c
	}
	c.mask |= bits
	if emph >= c.emph {
		c.emph, c.owner = emph, owner
	}
}

func (g lineGrid) hseg(a, b, y int, emph emphasis, owner *node) {
	if a > b {
		a, b = b, a
	}
	for x := a; x <= b; x++ {
		bits := 0
		if x > a {
			bits |= dirLeft
		}
		if x < b {
			bits |= dirRight
		}
		if bits != 0 {
			g.add(x, y, bits, emph, owner)
		}
	}
}

func (g lineGrid) vseg(x, a, b int, emph emphasis, owner *node) {
	if a > b {
		a, b = b, a
	}
	for y := a; y <= b; y++ {
		bits := 0
		if y > a {
			bits |= dirUp
		}
		if y < b {
			bits |= dirDown
		}
		if bits != 0 {
			g.add(x, y, bits, emph, owner)
		}
	}
}

// lines は「今描いている位置」(アニメ中の位置) から線を引く (spec §4.2: 線はアニメ中の位置から毎フレーム引く)。
func (m *Model) lines(now map[*node][2]int) lineGrid {
	sp := m.spine()
	g := lineGrid{}
	for _, pn := range m.order {
		pp, ok := now[pn]
		if !ok || !pn.dir || !pn.expanded {
			continue
		}
		var kp [][2]int
		var routeKid *node
		for _, k := range m.kids(pn) {
			if q, ok := now[k]; ok {
				kp = append(kp, q)
				if sp[k] && sp[pn] {
					routeKid = k
				}
			}
		}
		if len(kp) == 0 {
			continue
		}
		emph := emDim
		if sp[pn] {
			emph = emActive
		}
		minX, y0, y1 := kp[0][0], kp[0][1], kp[0][1]
		for _, q := range kp {
			minX, y0, y1 = min(minX, q[0]), min(y0, q[1]), max(y1, q[1])
		}
		start := pp[0] + labelWidth(pn) + 1
		bar := minX - 2
		if bar < start {
			continue // まだ展開の途中
		}
		g.vseg(bar, y0, y1, emph, pn)
		for _, k := range m.kids(pn) {
			q, ok := now[k]
			if !ok {
				continue
			}
			e := emph
			if k == routeKid {
				e = emRoute
			}
			g.hseg(bar, q[0]-1, q[1], e, pn)
		}
		py := pp[1]
		je := emph
		if routeKid != nil {
			je = emRoute
		}
		level := py >= y0 && py <= y1
		if level {
			g.hseg(start, bar, py, je, pn)
		} else {
			tx := max(bar-1, start)
			g.hseg(start, tx, py, je, pn)
			g.vseg(tx, py, y0, je, pn)
			g.hseg(tx, bar, y0, je, pn)
		}
		if routeKid != nil {
			ty := y0
			if level {
				ty = py
			}
			g.vseg(bar, ty, now[routeKid][1], emRoute, pn)
		}
	}
	return g
}
